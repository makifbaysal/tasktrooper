// Package ga4 is the data source kind for Google Analytics 4, read through
// the Analytics Data API v1beta (runReport) with a Google service account:
// the one of a connected Google Cloud account, or a key pasted for this
// source alone. Page views, sessions, active users and event counts are
// series by day or hour; the top pages, referrers, countries and devices
// are tables. Its query language is domain.WebQuery.
package ga4

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // the property's reporting time zone, on every OS.

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/cloud/gcloud"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/datasource/httpapi"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/datasource/tokencache"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	keyProperty       = "property_id"
	keyAuth           = "auth"
	keyAccount        = "cloud_account_id"
	keyServiceAccount = "service_account_json"

	AuthCloudAccount   = "cloud_account"
	AuthServiceAccount = "service_account"

	credentialField = "service_account_json"
	// ReadonlyScope is the narrowest scope runReport takes.
	ReadonlyScope  = "https://www.googleapis.com/auth/analytics.readonly"
	defaultBaseURL = "https://analyticsdata.googleapis.com"
	api            = "Google Analytics"
	authHint       = "add the service account's email to the property as a Viewer (Admin → Property access management) and enable the Google Analytics Data API in its project"

	defaultTimeout = 30 * time.Second
	tokenTimeout   = 15 * time.Second
	maxBody        = 32 << 20
	maxErrorText   = 300
	maxRows        = 25000
	maxEvents      = 1000
	// processingDelay is how long Google says a day's data may take to
	// settle.
	processingDelay = 48 * time.Hour
)

// metricNames are the Data API metrics each WebQuery metric reads.
var metricNames = map[string]string{
	domain.WebMetricPageViews: "screenPageViews",
	domain.WebMetricSessions:  "sessions",
	domain.WebMetricVisitors:  "activeUsers",
	domain.WebMetricEvents:    "eventCount",
}

// tableDims are the dimensions each table reads, as the columns the kind
// answers them under.
var tableDims = map[string][]struct{ column, label, dimension string }{
	domain.RecipeTablePages:     {{"path", "Path", "pagePath"}},
	domain.RecipeTableReferrers: {{"referrer", "Referrer", "sessionSourceMedium"}},
	domain.RecipeTableCountries: {{"country", "Country", "countryId"}},
	domain.RecipeTableDevices:   {{"device", "Device", "deviceCategory"}, {"browser", "Browser", "browser"}, {"os", "OS", "operatingSystem"}},
}

// Minter exchanges a service account key for an access token of scope;
// gcloud.MintAccessToken unless a test replaces it.
type Minter func(ctx context.Context, client *http.Client, serviceAccountJSON, scope string) (token string, expiresAt time.Time, err error)

type Adapter struct {
	policy   urlguard.Policy
	accounts port.CloudAccountReader
	baseURL  string
	mint     Minter
	now      func() time.Time
	timeout  time.Duration
	tokens   *tokencache.Cache
}

var (
	_ port.DataSourceKindAdapter = (*Adapter)(nil)
	_ port.MetricsQuerier        = (*Adapter)(nil)
	_ port.TableQuerier          = (*Adapter)(nil)
	_ port.Discoverer            = (*Adapter)(nil)
)

// New answers an adapter that refuses every destination until SetURLPolicy
// and reads no account until SetCloudAccounts; datasource.NewService sets
// both.
func New() *Adapter {
	return &Adapter{
		baseURL: defaultBaseURL, mint: gcloud.MintAccessToken, now: time.Now,
		timeout: defaultTimeout, tokens: tokencache.New(),
	}
}

func (a *Adapter) SetURLPolicy(p urlguard.Policy) { a.policy = p }

func (a *Adapter) SetCloudAccounts(r port.CloudAccountReader) { a.accounts = r }

func (a *Adapter) SetBaseURL(u string) { a.baseURL = strings.TrimRight(u, "/") }

func (a *Adapter) SetMinter(m Minter) { a.mint = m }

func (a *Adapter) SetClock(now func() time.Time) { a.now = now }

func (a *Adapter) Kind() domain.DataSourceKind { return domain.DataSourceKindGA4 }

func (a *Adapter) Capabilities() []domain.Capability {
	return []domain.Capability{domain.CapabilityMetrics, domain.CapabilityRUM}
}

func (a *Adapter) Schema() domain.DataSourceSchema {
	return domain.DataSourceSchema{
		Label:       "Google Analytics 4",
		Description: "Page views, sessions and active users from a Google Analytics 4 property, with the top pages, referrers, countries and devices. GA4 processes data for up to two days, so the newest points may still rise.",
		Fields: []domain.DataSourceField{
			{
				Key: keyProperty, Label: "Property ID", Type: domain.FieldText, Required: true,
				Placeholder: "123456789",
				Help:        "The number under Admin → Property details (not the G-… measurement ID).",
			},
			{
				Key: keyAuth, Label: "Sign in with", Type: domain.FieldSelect,
				Options: []string{AuthCloudAccount, AuthServiceAccount}, Default: AuthCloudAccount, Placeholder: AuthCloudAccount,
			},
			{
				Key: keyAccount, Label: "Google Cloud account", Type: domain.FieldCloudAccount,
				Provider: domain.CloudGCP, Required: true, ShowWhen: map[string]string{keyAuth: AuthCloudAccount},
				Help: "Its service account must be a Viewer of the property (Admin → Property access management).",
			},
			{
				Key: keyServiceAccount, Label: "Service account key (JSON)", Type: domain.FieldSecret, Required: true, Secret: true, Multiline: true,
				ShowWhen: map[string]string{keyAuth: AuthServiceAccount},
				Help:     "A key of a service account that is a Viewer of the property, with the Google Analytics Data API enabled in its project. Stored encrypted.",
			},
		},
	}
}

func (a *Adapter) serviceAccount(ctx context.Context, ds domain.DataSource, secrets map[string]string) (string, error) {
	if ds.Config[keyAuth] == AuthServiceAccount {
		if key := secrets[keyServiceAccount]; key != "" {
			return key, nil
		}
		return "", errors.New("no service account key is set")
	}
	if a.accounts == nil {
		return "", errors.New("cloud accounts are not available here")
	}
	id, err := uuid.Parse(ds.Config[keyAccount])
	if err != nil {
		return "", errors.New("no Google Cloud account is selected")
	}
	cred, err := a.accounts.CloudCredential(ctx, id)
	switch {
	case errors.Is(err, port.ErrNotFound):
		return "", errors.New("the Google Cloud account is no longer connected")
	case err != nil:
		return "", fmt.Errorf("reading the Google Cloud account: %w", err)
	case cred.Provider != domain.CloudGCP:
		return "", errors.New("the selected cloud account is not a Google Cloud account")
	case cred.Fields[credentialField] == "":
		return "", errors.New("the Google Cloud account has no service account key")
	}
	return cred.Fields[credentialField], nil
}

func (a *Adapter) token(ctx context.Context, serviceAccountJSON string) (string, error) {
	key := tokencache.Key(serviceAccountJSON, ReadonlyScope)
	if tok, ok := a.tokens.Get(key, a.now()); ok {
		return tok, nil
	}
	client := a.policy.Client(tokenTimeout)
	defer client.CloseIdleConnections()
	tok, exp, err := a.mint(ctx, client, serviceAccountJSON, ReadonlyScope)
	if err != nil {
		return "", mintError(err)
	}
	a.tokens.Put(key, tok, exp, a.now())
	return tok, nil
}

// mintError keeps the status and OAuth code the minter reports (never the
// token endpoint's own text, which it only logs) and drops the token URL
// net/http adds.
func mintError(err error) error {
	switch {
	case errors.Is(err, gcloud.ErrTokenURINotGoogle):
		return errors.New("google sign-in: the key's token_uri is not Google's token endpoint")
	case errors.Is(err, urlguard.ErrBlocked):
		return errors.New("google sign-in: the key's token_uri is not an allowed destination")
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	msg := err.Error()
	if r := []rune(msg); len(r) > maxErrorText {
		msg = string(r[:maxErrorText]) + "…"
	}
	return fmt.Errorf("google sign-in for Google Analytics failed: %s", msg)
}

type conn struct {
	*httpapi.Conn
	property string
}

func (a *Adapter) open(ctx context.Context, ds domain.DataSource, secrets map[string]string, property string) (*conn, error) {
	if property == "" {
		property = strings.TrimSpace(ds.Config[keyProperty])
	}
	if !domain.ValidGA4Property(property) {
		return nil, fmt.Errorf("%q is not a Google Analytics 4 property id (the number, not a G-… measurement id)", property)
	}
	key, err := a.serviceAccount(ctx, ds, secrets)
	if err != nil {
		return nil, err
	}
	tok, err := a.token(ctx, key)
	if err != nil {
		return nil, err
	}
	c, err := httpapi.Open(ctx, a.policy, httpapi.Endpoint{
		Base:    a.baseURL,
		Headers: []domain.HeaderLine{{Name: "Authorization", Value: "Bearer " + tok}},
		Masks:   []string{tok},
		API:     api,
	}, a.timeout)
	if err != nil {
		return nil, err
	}
	return &conn{Conn: c, property: property}, nil
}

type report struct {
	Rows []struct {
		DimensionValues []struct {
			Value string `json:"value"`
		} `json:"dimensionValues"`
		MetricValues []struct {
			Value string `json:"value"`
		} `json:"metricValues"`
	} `json:"rows"`
	RowCount int `json:"rowCount"`
	Metadata struct {
		TimeZone              string `json:"timeZone"`
		SubjectToThresholding bool   `json:"subjectToThresholding"`
		DataLossFromOtherRow  bool   `json:"dataLossFromOtherRow"`
	} `json:"metadata"`
}

func (a *Adapter) runReport(ctx context.Context, c *conn, op string, body map[string]any) (report, error) {
	resp, err := c.Do(ctx, httpapi.Request{
		Method: http.MethodPost,
		Path:   "/v1beta/properties/" + c.property + ":runReport",
		JSON:   body,
		Limit:  maxBody,
	})
	if err != nil {
		return report{}, err
	}
	if !resp.OK() {
		return report{}, c.Refusal(op, resp, c.Detail(resp), authHint)
	}
	var out report
	if err := json.NewDecoder(bytes.NewReader(resp.Body)).Decode(&out); err != nil {
		return report{}, fmt.Errorf("%s %s: the answer is not what the Data API sends", api, op)
	}
	return out, nil
}

func (a *Adapter) Verify(ctx context.Context, ds domain.DataSource, secrets map[string]string) (domain.DataSourceInfo, error) {
	c, err := a.open(ctx, ds, secrets, "")
	if err != nil {
		return domain.DataSourceInfo{}, err
	}
	defer c.Close()
	rep, err := a.runReport(ctx, c, "read the property", map[string]any{
		"dateRanges": []map[string]string{{"startDate": "6daysAgo", "endDate": "today"}},
		"metrics":    []map[string]string{{"name": "activeUsers"}},
	})
	if err != nil {
		return domain.DataSourceInfo{}, err
	}
	users := "0"
	if len(rep.Rows) > 0 && len(rep.Rows[0].MetricValues) > 0 {
		users = rep.Rows[0].MetricValues[0].Value
	}
	detail := "Google Analytics · property " + c.property + " · " + users + " active users in the last 7 days"
	if rep.Metadata.TimeZone != "" {
		detail += " · " + rep.Metadata.TimeZone
	}
	return domain.DataSourceInfo{Detail: detail}, nil
}

// Discover lists the event names the property recorded in the last 28
// days, the most frequent first (page_view, session_start, …).
func (a *Adapter) Discover(ctx context.Context, ds domain.DataSource, secrets map[string]string) (domain.Inventory, error) {
	c, err := a.open(ctx, ds, secrets, "")
	if err != nil {
		return domain.Inventory{}, err
	}
	defer c.Close()
	rep, err := a.runReport(ctx, c, "list events", map[string]any{
		"dateRanges": []map[string]string{{"startDate": "27daysAgo", "endDate": "today"}},
		"dimensions": []map[string]string{{"name": "eventName"}},
		"metrics":    []map[string]string{{"name": "eventCount"}},
		"orderBys":   []map[string]any{{"metric": map[string]string{"metricName": "eventCount"}, "desc": true}},
		"limit":      strconv.Itoa(maxEvents),
	})
	if err != nil {
		return domain.Inventory{}, err
	}
	inv := domain.Inventory{Metrics: []string{}, Truncated: rep.RowCount > len(rep.Rows)}
	for _, r := range rep.Rows {
		if len(r.DimensionValues) > 0 && r.DimensionValues[0].Value != "" {
			inv.Metrics = append(inv.Metrics, r.DimensionValues[0].Value)
		}
	}
	return inv, nil
}

func parse(raw string) (domain.WebQuery, error) {
	q, err := domain.ParseWebQuery(domain.DataSourceKindGA4, raw)
	if err != nil {
		return q, fmt.Errorf("%w: %s", port.ErrDataSourceQuery, err.Error())
	}
	return q, nil
}

func stringFilter(field, value string) map[string]any {
	return map[string]any{"filter": map[string]any{
		"fieldName":    field,
		"stringFilter": map[string]any{"matchType": "EXACT", "value": value, "caseSensitive": false},
	}}
}

func dimensionFilter(q domain.WebQuery) map[string]any {
	var exprs []map[string]any
	if q.Host != "" {
		exprs = append(exprs, stringFilter("hostName", q.Host))
	}
	if q.Event != "" {
		exprs = append(exprs, stringFilter("eventName", q.Event))
	}
	switch len(exprs) {
	case 0:
		return nil
	case 1:
		return exprs[0]
	}
	return map[string]any{"andGroup": map[string]any{"expressions": exprs}}
}

// Series reads one metric per hour (a step under a day; dateHour) or per day
// (date) of the property's own time zone. The window is widened by a day on
// each side, since dates are the property's and its zone is only known from
// the answer, then cut back. A bucket with no row is 0; page_views is per
// second of its bucket.
func (a *Adapter) Series(ctx context.Context, ds domain.DataSource, secrets map[string]string, sq domain.SeriesQuery) (domain.SeriesResult, error) {
	q, err := parse(sq.Query)
	if err != nil {
		return domain.SeriesResult{}, err
	}
	if q.IsTable() {
		return domain.SeriesResult{}, fmt.Errorf("%w: a table query (table %q) is no series", port.ErrDataSourceQuery, q.Table)
	}
	hourly := sq.Step < 24*time.Hour
	dim, layout := "date", "20060102"
	if hourly {
		dim, layout = "dateHour", "2006010215"
	}
	c, err := a.open(ctx, ds, secrets, q.Property)
	if err != nil {
		return domain.SeriesResult{}, err
	}
	defer c.Close()
	body := map[string]any{
		"dateRanges": []map[string]string{{
			"startDate": sq.From.UTC().AddDate(0, 0, -1).Format(time.DateOnly),
			"endDate":   sq.To.UTC().AddDate(0, 0, 1).Format(time.DateOnly),
		}},
		"dimensions": []map[string]string{{"name": dim}},
		"metrics":    []map[string]string{{"name": metricNames[q.Metric]}},
		"limit":      strconv.Itoa(maxRows),
	}
	if f := dimensionFilter(q); f != nil {
		body["dimensionFilter"] = f
	}
	rep, err := a.runReport(ctx, c, "read "+q.Metric, body)
	if err != nil {
		return domain.SeriesResult{}, err
	}
	res := domain.SeriesResult{Series: []domain.Series{}, Warnings: []string{}, Step: 24 * time.Hour}
	if hourly {
		res.Step = time.Hour
	}
	loc, err := time.LoadLocation(rep.Metadata.TimeZone)
	if rep.Metadata.TimeZone == "" || err != nil {
		loc = time.UTC
		res.Warnings = append(res.Warnings, "Google Analytics named no time zone for the property; its days are read as UTC")
	}
	values := map[int64]float64{}
	for _, r := range rep.Rows {
		if len(r.DimensionValues) == 0 || len(r.MetricValues) == 0 {
			continue
		}
		t, err := time.ParseInLocation(layout, r.DimensionValues[0].Value, loc)
		if err != nil {
			continue
		}
		if v, err := strconv.ParseFloat(r.MetricValues[0].Value, 64); err == nil {
			values[t.Unix()] = v
		}
	}
	s := domain.Series{Name: q.Metric, Labels: map[string]string{"property": c.property}, Points: []domain.SeriesPoint{}}
	if q.Host != "" {
		s.Labels["host"] = q.Host
	}
	for t := startOfBucket(sq.From.In(loc), hourly); t.Before(sq.To); t = nextBucket(t, hourly) {
		v := values[t.Unix()]
		if q.Metric == domain.WebMetricPageViews {
			v /= nextBucket(t, hourly).Sub(t).Seconds()
		}
		s.Points = append(s.Points, domain.FinitePoint(t.UTC(), v))
	}
	res.Series = append(res.Series, s)
	if a.now().Sub(sq.To) < processingDelay {
		res.Warnings = append(res.Warnings, "Google Analytics processes data for up to 48 hours; the newest points may still rise")
	}
	res.Warnings = append(res.Warnings, reportWarnings(rep)...)
	return res, nil
}

func startOfBucket(t time.Time, hourly bool) time.Time {
	if hourly {
		return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, t.Location())
	}
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

func nextBucket(t time.Time, hourly bool) time.Time {
	if hourly {
		return t.Add(time.Hour)
	}
	return t.AddDate(0, 0, 1)
}

func reportWarnings(rep report) []string {
	var out []string
	if rep.Metadata.SubjectToThresholding {
		out = append(out, "Google Analytics withheld rows below its privacy thresholds")
	}
	if rep.Metadata.DataLossFromOtherRow {
		out = append(out, "Google Analytics folded some rows into (other)")
	}
	return out
}

// TableColumns are the columns each table answers, in the order the recipes
// show them.
func TableColumns(table string) []domain.TableColumn {
	var cols []domain.TableColumn
	for _, d := range tableDims[table] {
		cols = append(cols, domain.TableColumn{Key: d.column, Label: d.label})
	}
	cols = append(cols,
		domain.TableColumn{Key: "views", Label: "Views", Unit: "views"},
		domain.TableColumn{Key: "visitors", Label: "Visitors", Unit: "users"})
	if table == domain.RecipeTableReferrers {
		cols = append(cols, domain.TableColumn{Key: "sessions", Label: "Sessions", Unit: "sessions"})
	}
	return cols
}

// Table answers a web table over the last Days of the property's own
// calendar (today included), the most viewed first: its dimensions, views
// (screenPageViews), visitors (activeUsers) and, for referrers, sessions.
func (a *Adapter) Table(ctx context.Context, ds domain.DataSource, secrets map[string]string, tq domain.TableQuery) (domain.TableResult, error) {
	q, err := parse(tq.Query)
	if err != nil {
		return domain.TableResult{}, err
	}
	if !q.IsTable() {
		return domain.TableResult{}, fmt.Errorf("%w: a series query (metric %q) is no table; name a table", port.ErrDataSourceQuery, q.Metric)
	}
	c, err := a.open(ctx, ds, secrets, q.Property)
	if err != nil {
		return domain.TableResult{}, err
	}
	defer c.Close()
	dims := tableDims[q.Table]
	var dimensions []map[string]string
	for _, d := range dims {
		dimensions = append(dimensions, map[string]string{"name": d.dimension})
	}
	metrics := []map[string]string{{"name": "screenPageViews"}, {"name": "activeUsers"}}
	if q.Table == domain.RecipeTableReferrers {
		metrics = append(metrics, map[string]string{"name": "sessions"})
	}
	body := map[string]any{
		"dateRanges": []map[string]string{{"startDate": strconv.Itoa(q.Days-1) + "daysAgo", "endDate": "today"}},
		"dimensions": dimensions,
		"metrics":    metrics,
		"orderBys":   []map[string]any{{"metric": map[string]string{"metricName": "screenPageViews"}, "desc": true}},
		"limit":      strconv.Itoa(q.Limit),
	}
	if f := dimensionFilter(q); f != nil {
		body["dimensionFilter"] = f
	}
	rep, err := a.runReport(ctx, c, "read the "+q.Table+" table", body)
	if err != nil {
		return domain.TableResult{}, err
	}
	cols := TableColumns(q.Table)
	res := domain.TableResult{Columns: cols, Rows: make([]map[string]any, 0, len(rep.Rows)), Truncated: rep.RowCount > len(rep.Rows)}
	for _, r := range rep.Rows {
		row := map[string]any{}
		for i, d := range dims {
			if i < len(r.DimensionValues) {
				row[d.column] = r.DimensionValues[i].Value
			}
		}
		for i, col := range cols[len(dims):] {
			row[col.Key] = nil
			if i < len(r.MetricValues) {
				if v, err := strconv.ParseFloat(r.MetricValues[i].Value, 64); err == nil {
					row[col.Key] = v
				}
			}
		}
		res.Rows = append(res.Rows, row)
	}
	sort.SliceStable(res.Rows, func(i, j int) bool {
		vi, _ := res.Rows[i]["views"].(float64)
		vj, _ := res.Rows[j]["views"].(float64)
		return vi > vj
	})
	return res, nil
}
