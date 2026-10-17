// Package appstoremetrics is the data source kind for App Store Connect's
// power and performance metrics (what Xcode Organizer shows: launch time,
// hang rate, memory, disk writes, battery, terminations) and a build's
// diagnostic signatures, read with the workspace's App Store Connect API key
// (the store credential the release flow already uses). Apple reports these
// per app version, not over time, and has no crash-free rate; Sentry or
// Crashlytics answer that. Its query language is domain.AppStoreMetricsQuery.
package appstoremetrics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/cloud/appstore"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/datasource/httpapi"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/datasource/tokencache"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	keyCredential = "store_credential"
	keyApp        = "app_id"

	defaultBaseURL = "https://api.appstoreconnect.apple.com"
	api            = "the App Store Connect API"
	// accept: perfPowerMetrics answers Apple's own xcode-metrics type; every
	// other resource answers JSON.
	accept       = "application/vnd.apple.xcode-metrics+json, application/json"
	tokenPurpose = "appstoreconnect-v1"

	defaultTimeout = 30 * time.Second
	maxBody        = 32 << 20
	maxPages       = 5
	pageLimit      = 200
	maxRows        = 500
)

// Minter signs an App Store Connect API token from the store credential;
// appstore.MintToken unless a test replaces it.
type Minter func(cred domain.StoreCredential) (token string, expiresAt time.Time, err error)

// knownCategories are the metric identifiers whose category is certain, so a
// read of one asks Apple for that category only.
var knownCategories = map[string]string{"launchTime": "LAUNCH", "hangRate": "HANG"}

var percentileCriteria = map[string]string{
	"percentile.fifty": domain.ASCPercentileP50, "percentile.ninety": domain.ASCPercentileP90,
}

type Adapter struct {
	policy  urlguard.Policy
	creds   port.StoreCredentialReader
	baseURL string
	mint    Minter
	now     func() time.Time
	timeout time.Duration
	tokens  *tokencache.Cache
}

var (
	_ port.DataSourceKindAdapter = (*Adapter)(nil)
	_ port.MetricsQuerier        = (*Adapter)(nil)
	_ port.TableQuerier          = (*Adapter)(nil)
)

// New answers an adapter that refuses every destination until SetURLPolicy
// and reads no credential until SetStoreCredentials; datasource.NewService
// sets both.
func New() *Adapter {
	return &Adapter{
		baseURL: defaultBaseURL,
		mint:    appstore.MintToken,
		now:     time.Now,
		timeout: defaultTimeout,
		tokens:  tokencache.New(),
	}
}

func (a *Adapter) SetURLPolicy(p urlguard.Policy) { a.policy = p }

func (a *Adapter) SetStoreCredentials(r port.StoreCredentialReader) { a.creds = r }

func (a *Adapter) SetBaseURL(u string) { a.baseURL = strings.TrimRight(u, "/") }

func (a *Adapter) SetMinter(m Minter) { a.mint = m }

func (a *Adapter) SetClock(now func() time.Time) { a.now = now }

func (a *Adapter) Kind() domain.DataSourceKind { return domain.DataSourceKindAppStoreMetrics }

// Capabilities: the signatures table (hang, launch and disk write
// diagnostics) is what crashes stands for on this kind.
func (a *Adapter) Capabilities() []domain.Capability {
	return []domain.Capability{domain.CapabilityMetrics, domain.CapabilityCrashes}
}

func (a *Adapter) Schema() domain.DataSourceSchema {
	return domain.DataSourceSchema{
		Label:       "App Store Connect metrics",
		Description: "An iOS app's launch time, hangs, memory, disk writes and battery use as Xcode Organizer shows them, per app version, plus each build's hang and launch diagnostics — read with the workspace's App Store Connect key. No crash-free rate: Apple does not report one.",
		Fields: []domain.DataSourceField{
			{
				Key: keyCredential, Label: "App Store Connect credential", Type: domain.FieldStoreCredential,
				Provider: domain.CloudProviderKind(domain.StoreCredentialASC), Required: true,
				Default: domain.StoreCredentialASC,
				Help:    "The API key saved under Settings → Integrations → App Store Connect.",
			},
			{
				Key: keyApp, Label: "App id", Type: domain.FieldText,
				Placeholder: "1234567890",
				Help:        "The app read when a query names none: its App Store Connect id. A component's binding fills it from its repository's linked iOS app.",
			},
		},
	}
}

func (a *Adapter) Verify(ctx context.Context, ds domain.DataSource, _ map[string]string) (domain.DataSourceInfo, error) {
	c, err := a.open(ctx, ds)
	if err != nil {
		return domain.DataSourceInfo{}, err
	}
	defer c.Close()
	if app := strings.TrimSpace(ds.Config[keyApp]); app != "" {
		if err := checkApp(app); err != nil {
			return domain.DataSourceInfo{}, err
		}
		var out struct {
			Data struct {
				Attributes struct {
					Name     string `json:"name"`
					BundleID string `json:"bundleId"`
				} `json:"attributes"`
			} `json:"data"`
		}
		if err := a.get(ctx, c, "read the app", "/v1/apps/"+app, url.Values{"fields[apps]": {"name,bundleId"}}, &out); err != nil {
			return domain.DataSourceInfo{}, err
		}
		return domain.DataSourceInfo{Detail: "App Store Connect · " + out.Data.Attributes.Name}, nil
	}
	var out struct {
		Data  []json.RawMessage `json:"data"`
		Links struct {
			Next string `json:"next"`
		} `json:"links"`
	}
	if err := a.get(ctx, c, "list apps", "/v1/apps", url.Values{"limit": {strconv.Itoa(pageLimit)}, "fields[apps]": {"name"}}, &out); err != nil {
		return domain.DataSourceInfo{}, err
	}
	count := strconv.Itoa(len(out.Data))
	if out.Links.Next != "" {
		count += "+"
	}
	return domain.DataSourceInfo{Detail: "App Store Connect · " + count + " app(s)"}, nil
}

func (a *Adapter) open(ctx context.Context, ds domain.DataSource) (*httpapi.Conn, error) {
	tok, err := a.token(ctx, ds)
	if err != nil {
		return nil, err
	}
	return httpapi.Open(ctx, a.policy, httpapi.Endpoint{
		Base: a.baseURL,
		Headers: []domain.HeaderLine{
			{Name: "Authorization", Value: "Bearer " + tok},
			{Name: "Accept", Value: accept},
		},
		Masks: []string{tok},
		API:   api,
	}, a.timeout)
}

// token is a JWT signed with the credential's key, reused until shortly
// before it expires: signing is local, but a chart refresh reads several
// resources.
func (a *Adapter) token(ctx context.Context, ds domain.DataSource) (string, error) {
	if a.creds == nil {
		return "", errors.New("store credentials are not available here")
	}
	provider := ds.Config[keyCredential]
	if provider == "" {
		provider = domain.StoreCredentialASC
	}
	if provider != domain.StoreCredentialASC {
		return "", errors.New("the selected store credential is not App Store Connect's")
	}
	cred, err := a.creds.StoreCredential(ctx, provider)
	switch {
	case errors.Is(err, port.ErrNotFound):
		return "", errors.New("no App Store Connect credential is saved: add it under Settings → Integrations")
	case err != nil:
		return "", fmt.Errorf("reading the App Store Connect credential: %w", err)
	}
	key := tokencache.Key(cred.Data["key_id"]+"\x00"+cred.Data["issuer_id"]+"\x00"+cred.Data["p8"], tokenPurpose)
	if tok, ok := a.tokens.Get(key, a.now()); ok {
		return tok, nil
	}
	tok, exp, err := a.mint(cred)
	if err != nil {
		return "", errors.New("the App Store Connect key cannot sign a token: check its key id, issuer id and .p8 key")
	}
	a.tokens.Put(key, tok, exp, a.now())
	return tok, nil
}

func checkApp(app string) error {
	if !domain.ValidASCAppID(app) {
		return fmt.Errorf("%q is not an App Store Connect app id (the number in its App Store URL)", app)
	}
	return nil
}

func (a *Adapter) parse(ds domain.DataSource, raw string) (domain.AppStoreMetricsQuery, string, error) {
	q, err := domain.ParseAppStoreMetricsQuery(raw)
	if err != nil {
		return q, "", fmt.Errorf("%w: %s", port.ErrDataSourceQuery, err.Error())
	}
	app := q.App
	if app == "" {
		app = strings.TrimSpace(ds.Config[keyApp])
	}
	if app == "" {
		return q, "", fmt.Errorf("%w: the query names no app and the source has no app id", port.ErrDataSourceQuery)
	}
	if err := checkApp(app); err != nil {
		return q, "", fmt.Errorf("%w: %s", port.ErrDataSourceQuery, err.Error())
	}
	return q, app, nil
}

type xcodeMetrics struct {
	ProductData []struct {
		Platform         string `json:"platform"`
		MetricCategories []struct {
			Identifier string `json:"identifier"`
			Metrics    []struct {
				Identifier string `json:"identifier"`
				Unit       struct {
					Identifier string `json:"identifier"`
				} `json:"unit"`
				Datasets []struct {
					FilterCriteria struct {
						Device     string `json:"device"`
						Percentile string `json:"percentile"`
					} `json:"filterCriteria"`
					Points []struct {
						Version string   `json:"version"`
						Value   *float64 `json:"value"`
					} `json:"points"`
				} `json:"datasets"`
			} `json:"metrics"`
		} `json:"metricCategories"`
	} `json:"productData"`
}

type versionValue struct {
	version string
	at      time.Time
	value   *float64
}

type dataset struct {
	metric, percentile, unit string
	values                   []versionValue
}

// Series answers, at every step of the window, the value Apple reports for
// the newest app version created in App Store Connect by then: its metrics
// are per version, so a chart is a staircase that moves when a version
// ships. A version Apple has metrics for but App Store Connect lists no
// version record of is left out, with a warning.
func (a *Adapter) Series(ctx context.Context, ds domain.DataSource, _ map[string]string, sq domain.SeriesQuery) (domain.SeriesResult, error) {
	q, app, err := a.parse(ds, sq.Query)
	if err != nil {
		return domain.SeriesResult{}, err
	}
	if q.IsTable() {
		return domain.SeriesResult{}, fmt.Errorf("%w: a diagnostics query reads a table, not a series; name a metric", port.ErrDataSourceQuery)
	}
	c, err := a.open(ctx, ds)
	if err != nil {
		return domain.SeriesResult{}, err
	}
	defer c.Close()

	res := domain.SeriesResult{
		Series:   []domain.Series{},
		Warnings: []string{"App Store Connect reports these metrics per app version: each point is the value of the newest version created by then"},
	}
	params := url.Values{"filter[platform]": {"IOS"}, "filter[deviceType]": {q.Device}}
	category := q.ASCCategory()
	if category == "" {
		category = knownCategories[q.Metric]
	}
	if category != "" {
		params.Set("filter[metricType]", category)
	}
	var metrics xcodeMetrics
	err = a.get(ctx, c, "read power and performance metrics", "/v1/apps/"+app+"/perfPowerMetrics", params, &metrics)
	if errors.Is(err, errNotFound) {
		res.Warnings = append(res.Warnings, "App Store Connect has no power and performance metrics for this app yet")
		return res, nil
	}
	if err != nil {
		return domain.SeriesResult{}, err
	}
	sets := pickDatasets(metrics, q)
	if len(sets) == 0 {
		return res, nil
	}
	dates, err := a.versionDates(ctx, c, app)
	if err != nil {
		return domain.SeriesResult{}, err
	}
	var undated []string
	for i := range sets {
		kept := sets[i].values[:0]
		for _, v := range sets[i].values {
			at, ok := dates[v.version]
			if !ok {
				if !slices.Contains(undated, v.version) {
					undated = append(undated, v.version)
				}
				continue
			}
			v.at = at
			kept = append(kept, v)
		}
		sets[i].values = kept
		sort.SliceStable(sets[i].values, func(x, y int) bool { return sets[i].values[x].at.Before(sets[i].values[y].at) })
	}
	if len(undated) > 0 {
		res.Warnings = append(res.Warnings, "App Store Connect lists no version record of "+strings.Join(undated, ", ")+", so its metrics are left out")
	}
	single := q.ASCCategory() == ""
	for _, set := range sets {
		name := set.percentile
		if !single || name == "" {
			name = strings.TrimSpace(set.metric + " " + set.percentile)
		}
		labels := map[string]string{"metric": set.metric, "device": q.Device}
		if set.percentile != "" {
			labels["quantile"] = set.percentile
		}
		if set.unit != "" {
			labels["unit"] = set.unit
		}
		res.Series = append(res.Series, domain.Series{Name: name, Labels: labels, Points: staircase(set.values, sq)})
	}
	return res, nil
}

func pickDatasets(m xcodeMetrics, q domain.AppStoreMetricsQuery) []dataset {
	category := q.ASCCategory()
	var out []dataset
	for _, p := range m.ProductData {
		if p.Platform != "" && !strings.EqualFold(p.Platform, "iOS") {
			continue
		}
		for _, cat := range p.MetricCategories {
			if category != "" && cat.Identifier != category {
				continue
			}
			for _, metric := range cat.Metrics {
				if category == "" && metric.Identifier != q.Metric {
					continue
				}
				for _, d := range metric.Datasets {
					if d.FilterCriteria.Device != "" && d.FilterCriteria.Device != q.Device {
						continue
					}
					pct := percentileCriteria[d.FilterCriteria.Percentile]
					if q.Percentile != "" && pct != q.Percentile {
						continue
					}
					set := dataset{metric: metric.Identifier, percentile: pct, unit: metric.Unit.Identifier}
					for _, pt := range d.Points {
						if q.Version != "" && pt.Version != q.Version {
							continue
						}
						set.values = append(set.values, versionValue{version: pt.Version, value: pt.Value})
					}
					out = append(out, set)
				}
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].metric != out[j].metric {
			return out[i].metric < out[j].metric
		}
		return out[i].percentile < out[j].percentile
	})
	return out
}

// staircase samples dated version values at the query's steps: each point is
// the newest version dated at or before it, none before the first.
func staircase(values []versionValue, sq domain.SeriesQuery) []domain.SeriesPoint {
	points := []domain.SeriesPoint{}
	if len(values) == 0 || sq.Step <= 0 {
		return points
	}
	i := -1
	for t := sq.From; !t.After(sq.To); t = t.Add(sq.Step) {
		for i+1 < len(values) && !values[i+1].at.After(t) {
			i++
		}
		if i < 0 {
			continue
		}
		if v := values[i].value; v != nil {
			points = append(points, domain.FinitePoint(t, *v))
		} else {
			points = append(points, domain.SeriesPoint{Time: t})
		}
	}
	return points
}

// versionDates maps each iOS version string to when its App Store Connect
// version record was created, the earliest where one repeats.
func (a *Adapter) versionDates(ctx context.Context, c *httpapi.Conn, app string) (map[string]time.Time, error) {
	out := map[string]time.Time{}
	path := "/v1/apps/" + app + "/appStoreVersions"
	params := url.Values{
		"filter[platform]":         {"IOS"},
		"fields[appStoreVersions]": {"versionString,createdDate"},
		"limit":                    {strconv.Itoa(pageLimit)},
	}
	for page := 0; page < maxPages; page++ {
		var resp struct {
			Data []struct {
				Attributes struct {
					VersionString string `json:"versionString"`
					CreatedDate   string `json:"createdDate"`
				} `json:"attributes"`
			} `json:"data"`
			Links struct {
				Next string `json:"next"`
			} `json:"links"`
		}
		if err := a.get(ctx, c, "list app versions", path, params, &resp); err != nil {
			return nil, err
		}
		for _, v := range resp.Data {
			at, err := time.Parse(time.RFC3339, v.Attributes.CreatedDate)
			if err != nil {
				if at, err = time.Parse("2006-01-02T15:04:05.000-0700", v.Attributes.CreatedDate); err != nil {
					continue
				}
			}
			if prev, ok := out[v.Attributes.VersionString]; !ok || at.Before(prev) {
				out[v.Attributes.VersionString] = at.UTC()
			}
		}
		next, ok := a.nextPage(resp.Links.Next)
		if !ok {
			break
		}
		path, params = next.Path, next.Query
	}
	return out, nil
}

var diagnosticTypes = map[string]string{
	domain.ASCDiagnosticsHangs: "HANGS", domain.ASCDiagnosticsLaunches: "LAUNCHES", domain.ASCDiagnosticsDiskWrites: "DISK_WRITES",
}

// SignatureColumns are the signatures table's columns, in the order the
// recipe lists them.
func SignatureColumns() []domain.TableColumn {
	return []domain.TableColumn{
		{Key: "issue", Label: "Signature id"}, {Key: "type", Label: "Type"}, {Key: "cause", Label: "Signature"},
		{Key: "weight", Label: "Share", Unit: "%"}, {Key: "build", Label: "Build"},
	}
}

// Table answers a diagnostics query: one build's diagnostic signatures by
// weight (the share of that build's hangs, slow launches or disk writes the
// signature accounts for, as a percent).
func (a *Adapter) Table(ctx context.Context, ds domain.DataSource, _ map[string]string, tq domain.TableQuery) (domain.TableResult, error) {
	q, app, err := a.parse(ds, tq.Query)
	if err != nil {
		return domain.TableResult{}, err
	}
	if !q.IsTable() {
		return domain.TableResult{}, fmt.Errorf("%w: a table reads diagnostics; give diagnostics (hangs, launches, disk_writes or all)", port.ErrDataSourceQuery)
	}
	c, err := a.open(ctx, ds)
	if err != nil {
		return domain.TableResult{}, err
	}
	defer c.Close()
	res := domain.TableResult{Columns: SignatureColumns(), Rows: []map[string]any{}}
	buildID, buildNumber, err := a.build(ctx, c, app, q)
	if err != nil || buildID == "" {
		return res, err
	}
	path := "/v1/builds/" + url.PathEscape(buildID) + "/diagnosticSignatures"
	params := url.Values{
		"fields[diagnosticSignatures]": {"diagnosticType,signature,weight"},
		"limit":                        {strconv.Itoa(pageLimit)},
	}
	if t, ok := diagnosticTypes[q.Diagnostics]; ok {
		params.Set("filter[diagnosticType]", t)
	}
	for page := 0; page < maxPages; page++ {
		var resp struct {
			Data []struct {
				ID         string `json:"id"`
				Attributes struct {
					DiagnosticType string   `json:"diagnosticType"`
					Signature      string   `json:"signature"`
					Weight         *float64 `json:"weight"`
				} `json:"attributes"`
			} `json:"data"`
			Links struct {
				Next string `json:"next"`
			} `json:"links"`
		}
		if err := a.get(ctx, c, "list diagnostic signatures", path, params, &resp); err != nil {
			return domain.TableResult{}, err
		}
		for _, d := range resp.Data {
			if len(res.Rows) == maxRows {
				res.Truncated = true
				return res, nil
			}
			row := map[string]any{
				"issue": d.ID, "type": strings.ToLower(d.Attributes.DiagnosticType),
				"cause": d.Attributes.Signature, "build": buildNumber,
			}
			if d.Attributes.Weight != nil {
				row["weight"] = *d.Attributes.Weight * 100
			}
			res.Rows = append(res.Rows, row)
		}
		next, ok := a.nextPage(resp.Links.Next)
		if !ok {
			return res, nil
		}
		path, params = next.Path, next.Query
	}
	res.Truncated = true
	return res, nil
}

// build is the build a diagnostics read covers: the one the query names, else
// the newest upload of its version, else the newest upload.
func (a *Adapter) build(ctx context.Context, c *httpapi.Conn, app string, q domain.AppStoreMetricsQuery) (id, number string, err error) {
	if q.Build != "" {
		return q.Build, q.Build, nil
	}
	params := url.Values{
		"filter[app]":    {app},
		"sort":           {"-uploadedDate"},
		"limit":          {"1"},
		"fields[builds]": {"version,uploadedDate"},
	}
	if q.Version != "" {
		params.Set("filter[preReleaseVersion.version]", q.Version)
	}
	var resp struct {
		Data []struct {
			ID         string `json:"id"`
			Attributes struct {
				Version string `json:"version"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := a.get(ctx, c, "find the newest build", "/v1/builds", params, &resp); err != nil {
		return "", "", err
	}
	if len(resp.Data) == 0 {
		return "", "", nil
	}
	return resp.Data[0].ID, resp.Data[0].Attributes.Version, nil
}

type page struct {
	Path  string
	Query url.Values
}

// nextPage follows a links.next only on the API's own host and scheme.
func (a *Adapter) nextPage(next string) (page, bool) {
	if next == "" {
		return page{}, false
	}
	u, err := url.Parse(next)
	base, berr := url.Parse(a.baseURL)
	if err != nil || berr != nil || u.Scheme != base.Scheme || !strings.EqualFold(u.Host, base.Host) {
		return page{}, false
	}
	rel := strings.TrimPrefix(u.EscapedPath(), strings.TrimRight(base.EscapedPath(), "/"))
	return page{Path: rel, Query: u.Query()}, true
}

var errNotFound = errors.New("not found")

func (a *Adapter) get(ctx context.Context, c *httpapi.Conn, op, path string, query url.Values, out any) error {
	resp, err := c.Do(ctx, httpapi.Request{Method: http.MethodGet, Path: path, Query: query, Limit: maxBody})
	if err != nil {
		return err
	}
	if resp.OK() {
		if err := json.Unmarshal(resp.Body, out); err != nil {
			return fmt.Errorf("%s %s: the answer is not JSON", api, op)
		}
		return nil
	}
	d := c.Scrub(detail(resp.Body))
	suffix := ""
	if d != "" {
		suffix = ": " + d
	}
	switch resp.Status {
	case http.StatusBadRequest:
		return fmt.Errorf("%w: %s %s: HTTP 400%s", port.ErrDataSourceQuery, api, op, suffix)
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("%s %s: authentication failed (HTTP %d)%s; the key's role may not read app metrics", api, op, resp.Status, suffix)
	case http.StatusNotFound:
		return fmt.Errorf("%s %s: %w (HTTP 404)%s — is the app id right?", api, op, errNotFound, suffix)
	case http.StatusTooManyRequests:
		return fmt.Errorf("%s %s: rate limited (HTTP 429); try again in a minute", api, op)
	}
	return fmt.Errorf("%s %s: HTTP %d%s", api, op, resp.Status, suffix)
}

// detail is App Store Connect's {"errors": [{"title", "detail"}]} message.
func detail(raw []byte) string {
	var body struct {
		Errors []struct {
			Title  string `json:"title"`
			Detail string `json:"detail"`
		} `json:"errors"`
	}
	if json.Unmarshal(raw, &body) != nil || len(body.Errors) == 0 {
		return ""
	}
	if body.Errors[0].Detail != "" {
		return body.Errors[0].Detail
	}
	return body.Errors[0].Title
}
