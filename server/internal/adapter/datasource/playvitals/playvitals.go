// Package playvitals is the data source kind for Google Play's Android
// vitals, read through the Play Developer Reporting API with the
// workspace's Google Play service account (the store credential the release
// flow already uses), so a mobile app's crash rate, ANR rate, slow starts and
// top crash issues need nothing installed. Its query language is
// domain.PlayVitalsQuery.
package playvitals

import (
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
	_ "time/tzdata" // Play's daily rows are America/Los_Angeles days, on every OS.

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/cloud/gcloud"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/datasource/httpapi"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/datasource/tokencache"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	keyCredential = "store_credential"
	keyPackage    = "package_name"

	credentialField = "service_account_json"
	// ReportingScope is the only scope the Reporting API takes.
	ReportingScope = "https://www.googleapis.com/auth/playdeveloperreporting"
	defaultBaseURL = "https://playdeveloperreporting.googleapis.com"
	api            = "the Play Developer Reporting API"

	// dailyZone is the only time zone Play aggregates days in; hourly rows
	// are UTC.
	dailyZone = "America/Los_Angeles"

	defaultTimeout = 30 * time.Second
	tokenTimeout   = 15 * time.Second
	maxBody        = 32 << 20
	maxErrorText   = 300

	rowsPerPage   = 10000
	maxPages      = 20
	issuesPerPage = 100
	maxIssueRows  = 500
)

// Minter exchanges a service account key for an access token of scope;
// gcloud.MintAccessToken unless a test replaces it.
type Minter func(ctx context.Context, client *http.Client, serviceAccountJSON, scope string) (token string, expiresAt time.Time, err error)

type Adapter struct {
	policy  urlguard.Policy
	creds   port.StoreCredentialReader
	baseURL string
	mint    Minter
	now     func() time.Time
	timeout time.Duration
	tokens  *tokencache.Cache
	daily   *time.Location
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
	loc, err := time.LoadLocation(dailyZone)
	if err != nil {
		loc = time.FixedZone(dailyZone, -8*3600)
	}
	return &Adapter{
		baseURL: defaultBaseURL,
		mint:    gcloud.MintAccessToken,
		now:     time.Now,
		timeout: defaultTimeout,
		tokens:  tokencache.New(),
		daily:   loc,
	}
}

// SetURLPolicy applies to the token exchange and the API alike: the API's
// address is fixed, but it is dialled like any other.
func (a *Adapter) SetURLPolicy(p urlguard.Policy) { a.policy = p }

func (a *Adapter) SetStoreCredentials(r port.StoreCredentialReader) { a.creds = r }

func (a *Adapter) SetBaseURL(u string) { a.baseURL = strings.TrimRight(u, "/") }

func (a *Adapter) SetMinter(m Minter) { a.mint = m }

func (a *Adapter) SetClock(now func() time.Time) { a.now = now }

func (a *Adapter) Kind() domain.DataSourceKind { return domain.DataSourceKindGooglePlayVitals }

func (a *Adapter) Capabilities() []domain.Capability {
	return []domain.Capability{domain.CapabilityMetrics, domain.CapabilityCrashes}
}

func (a *Adapter) Schema() domain.DataSourceSchema {
	return domain.DataSourceSchema{
		Label:       "Google Play vitals",
		Description: "Android vitals from Google Play — crash and ANR rates, slow starts, top crash issues — read with the workspace's Google Play credential. Nothing to install; Play publishes them about a day late.",
		Fields: []domain.DataSourceField{
			{
				Key: keyCredential, Label: "Google Play credential", Type: domain.FieldStoreCredential,
				Provider: domain.CloudProviderKind(domain.StoreCredentialGooglePlay), Required: true,
				Default: domain.StoreCredentialGooglePlay,
				Help:    "The service account saved under Settings → Integrations → Google Play. It needs the Play Developer Reporting API enabled in its Google Cloud project and, in Play Console, permission to view app information.",
			},
			{
				Key: keyPackage, Label: "Package name", Type: domain.FieldText,
				Placeholder: "com.example.app",
				Help:        "The app read when a query names none. A component's binding fills it from its repository's linked Android app.",
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
	if pkg := strings.TrimSpace(ds.Config[keyPackage]); pkg != "" {
		if err := checkPackage(pkg); err != nil {
			return domain.DataSourceInfo{}, err
		}
		latest, err := a.freshness(ctx, c, pkg, domain.PlayCrashRateMetricSet, "DAILY")
		if err != nil {
			return domain.DataSourceInfo{}, err
		}
		detail := "Google Play vitals · " + pkg
		if !latest.IsZero() {
			detail += " · data through " + latest.In(a.daily).AddDate(0, 0, -1).Format("2006-01-02")
		}
		return domain.DataSourceInfo{Detail: detail}, nil
	}
	var page struct {
		Apps []struct {
			PackageName string `json:"packageName"`
		} `json:"apps"`
		NextPageToken string `json:"nextPageToken"`
	}
	if err := a.get(ctx, c, "list apps", "/v1beta1/apps:search", url.Values{"pageSize": {"100"}}, &page); err != nil {
		return domain.DataSourceInfo{}, err
	}
	count := strconv.Itoa(len(page.Apps))
	if page.NextPageToken != "" {
		count += "+"
	}
	return domain.DataSourceInfo{Detail: "Google Play vitals · " + count + " app(s)"}, nil
}

func (a *Adapter) open(ctx context.Context, ds domain.DataSource) (*httpapi.Conn, error) {
	cred, err := a.credential(ctx, ds)
	if err != nil {
		return nil, err
	}
	tok, err := a.token(ctx, cred.Data[credentialField])
	if err != nil {
		return nil, err
	}
	return httpapi.Open(ctx, a.policy, httpapi.Endpoint{
		Base:    a.baseURL,
		Headers: []domain.HeaderLine{{Name: "Authorization", Value: "Bearer " + tok}},
		Masks:   []string{tok},
		API:     api,
	}, a.timeout)
}

func (a *Adapter) credential(ctx context.Context, ds domain.DataSource) (domain.StoreCredential, error) {
	if a.creds == nil {
		return domain.StoreCredential{}, errors.New("store credentials are not available here")
	}
	provider := ds.Config[keyCredential]
	if provider == "" {
		provider = domain.StoreCredentialGooglePlay
	}
	if provider != domain.StoreCredentialGooglePlay {
		return domain.StoreCredential{}, errors.New("the selected store credential is not Google Play's")
	}
	cred, err := a.creds.StoreCredential(ctx, provider)
	switch {
	case errors.Is(err, port.ErrNotFound):
		return domain.StoreCredential{}, errors.New("no Google Play credential is saved: add it under Settings → Integrations")
	case err != nil:
		return domain.StoreCredential{}, fmt.Errorf("reading the Google Play credential: %w", err)
	case cred.Data[credentialField] == "":
		return domain.StoreCredential{}, errors.New("the Google Play credential has no service account key")
	}
	return cred, nil
}

func (a *Adapter) token(ctx context.Context, serviceAccountJSON string) (string, error) {
	key := tokencache.Key(serviceAccountJSON, ReportingScope)
	if tok, ok := a.tokens.Get(key, a.now()); ok {
		return tok, nil
	}
	client := a.policy.Client(tokenTimeout)
	defer client.CloseIdleConnections()
	tok, exp, err := a.mint(ctx, client, serviceAccountJSON, ReportingScope)
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
	return fmt.Errorf("google sign-in with the Google Play credential failed: %s", msg)
}

func checkPackage(pkg string) error {
	if !domain.ValidPlayPackage(pkg) {
		return fmt.Errorf("%q is not an Android package name", pkg)
	}
	return nil
}

func (a *Adapter) parse(ds domain.DataSource, raw string) (domain.PlayVitalsQuery, string, error) {
	q, err := domain.ParsePlayVitalsQuery(raw)
	if err != nil {
		return q, "", fmt.Errorf("%w: %s", port.ErrDataSourceQuery, err.Error())
	}
	pkg := q.Package
	if pkg == "" {
		pkg = strings.TrimSpace(ds.Config[keyPackage])
	}
	if pkg == "" {
		return q, "", fmt.Errorf("%w: the query names no package and the source has no package name", port.ErrDataSourceQuery)
	}
	if err := checkPackage(pkg); err != nil {
		return q, "", fmt.Errorf("%w: %s", port.ErrDataSourceQuery, err.Error())
	}
	return q, pkg, nil
}

// dateTime is google.type.DateTime as the API writes it: a civil time plus
// a time zone id or a UTC offset ("-25200s").
type dateTime struct {
	Year     int       `json:"year"`
	Month    int       `json:"month"`
	Day      int       `json:"day"`
	Hours    int       `json:"hours,omitempty"`
	Minutes  int       `json:"minutes,omitempty"`
	Seconds  int       `json:"seconds,omitempty"`
	TimeZone *timeZone `json:"timeZone,omitempty"`
	Offset   string    `json:"utcOffset,omitempty"`
}

type timeZone struct {
	ID string `json:"id"`
}

func (a *Adapter) civil(t time.Time, period string) dateTime {
	if period == domain.PlayPeriodHourly {
		t = t.UTC()
		return dateTime{Year: t.Year(), Month: int(t.Month()), Day: t.Day(), Hours: t.Hour(), TimeZone: &timeZone{ID: "UTC"}}
	}
	t = t.In(a.daily)
	return dateTime{Year: t.Year(), Month: int(t.Month()), Day: t.Day(), TimeZone: &timeZone{ID: dailyZone}}
}

func (a *Adapter) instant(d dateTime) time.Time {
	loc := time.UTC
	switch {
	case d.TimeZone != nil && d.TimeZone.ID == dailyZone:
		loc = a.daily
	case d.TimeZone != nil && d.TimeZone.ID != "" && d.TimeZone.ID != "UTC":
		if l, err := time.LoadLocation(d.TimeZone.ID); err == nil {
			loc = l
		}
	case d.Offset != "":
		if off, err := time.ParseDuration(d.Offset); err == nil {
			loc = time.FixedZone("", int(off/time.Second))
		}
	}
	return time.Date(d.Year, time.Month(d.Month), d.Day, d.Hours, d.Minutes, d.Seconds, 0, loc)
}

func periodName(period string) string {
	if period == domain.PlayPeriodHourly {
		return "HOURLY"
	}
	return "DAILY"
}

// freshness is the end (exclusive) of the latest data the metric set holds
// at a period; zero when Play reports none.
func (a *Adapter) freshness(ctx context.Context, c *httpapi.Conn, pkg, set, period string) (time.Time, error) {
	var out struct {
		FreshnessInfo struct {
			Freshnesses []struct {
				AggregationPeriod string   `json:"aggregationPeriod"`
				LatestEndTime     dateTime `json:"latestEndTime"`
			} `json:"freshnesses"`
		} `json:"freshnessInfo"`
	}
	if err := a.get(ctx, c, "read "+set, "/v1beta1/apps/"+url.PathEscape(pkg)+"/"+set, nil, &out); err != nil {
		return time.Time{}, err
	}
	for _, f := range out.FreshnessInfo.Freshnesses {
		if f.AggregationPeriod == period && f.LatestEndTime.Year > 0 {
			return a.instant(f.LatestEndTime), nil
		}
	}
	return time.Time{}, nil
}

type metricsRow struct {
	StartTime  dateTime `json:"startTime"`
	Dimensions []struct {
		Dimension   string `json:"dimension"`
		StringValue string `json:"stringValue"`
		Int64Value  string `json:"int64Value"`
		ValueLabel  string `json:"valueLabel"`
	} `json:"dimensions"`
	Metrics []struct {
		Metric       string `json:"metric"`
		DecimalValue *struct {
			Value string `json:"value"`
		} `json:"decimalValue"`
	} `json:"metrics"`
}

// Series reads one metric of one metric set over the window, at Play's own
// daily or hourly period. The window is cut at the latest data Play holds,
// which for daily vitals is usually a day or two back, and a warning says so.
func (a *Adapter) Series(ctx context.Context, ds domain.DataSource, _ map[string]string, sq domain.SeriesQuery) (domain.SeriesResult, error) {
	q, pkg, err := a.parse(ds, sq.Query)
	if err != nil {
		return domain.SeriesResult{}, err
	}
	if q.IsTable() {
		return domain.SeriesResult{}, fmt.Errorf("%w: an issues query reads a table, not a series; name a metric_set", port.ErrDataSourceQuery)
	}
	c, err := a.open(ctx, ds)
	if err != nil {
		return domain.SeriesResult{}, err
	}
	defer c.Close()

	period := periodName(q.Period)
	step := 24 * time.Hour
	start := a.civil(sq.From, q.Period)
	if q.Period == domain.PlayPeriodHourly {
		step = time.Hour
	}
	end := sq.To
	latest, err := a.freshness(ctx, c, pkg, q.MetricSet, period)
	if err != nil {
		return domain.SeriesResult{}, err
	}
	res := domain.SeriesResult{Series: []domain.Series{}, Warnings: []string{}, Step: step}
	if !latest.IsZero() && latest.Before(end) {
		end = latest
		res.Warnings = append(res.Warnings, fmt.Sprintf("Google Play publishes vitals late: this metric has data until %s, so the rest of the window is empty", latest.UTC().Format(time.RFC3339)))
	}
	startAt := a.instant(start)
	if !end.After(startAt) {
		return res, nil
	}
	endCivil := a.civil(end, q.Period)
	if boundary := a.instant(endCivil); boundary.Before(end) {
		next := boundary.Add(time.Hour)
		if q.Period != domain.PlayPeriodHourly {
			next = boundary.AddDate(0, 0, 1)
		}
		endCivil = a.civil(next, q.Period)
	}
	if !latest.IsZero() && a.instant(endCivil).After(latest) {
		endCivil = a.civil(latest, q.Period)
	}

	body := map[string]any{
		"timelineSpec": map[string]any{"aggregationPeriod": period, "startTime": start, "endTime": endCivil},
		"metrics":      []string{q.Metric},
		"pageSize":     rowsPerPage,
	}
	if q.Dimension != "" {
		body["dimensions"] = []string{q.Dimension}
	}
	if q.Filter != "" {
		body["filter"] = q.Filter
	}
	bySeries := map[string]*domain.Series{}
	var order []string
	path := "/v1beta1/apps/" + url.PathEscape(pkg) + "/" + q.MetricSet + ":query"
	for page := 0; ; page++ {
		var out struct {
			Rows          []metricsRow `json:"rows"`
			NextPageToken string       `json:"nextPageToken"`
		}
		if err := a.post(ctx, c, "query "+q.MetricSet, path, body, &out); err != nil {
			return domain.SeriesResult{}, err
		}
		for _, row := range out.Rows {
			key, name, labels := seriesKey(row, q)
			s, ok := bySeries[key]
			if !ok {
				s = &domain.Series{Name: name, Labels: labels}
				bySeries[key] = s
				order = append(order, key)
			}
			s.Points = append(s.Points, a.point(row, q))
		}
		if out.NextPageToken == "" {
			break
		}
		if page+1 >= maxPages {
			res.Warnings = append(res.Warnings, fmt.Sprintf("Google Play answered more than %d pages; the rest is left out", maxPages))
			break
		}
		body["pageToken"] = out.NextPageToken
	}
	for _, key := range order {
		s := bySeries[key]
		sort.Slice(s.Points, func(i, j int) bool { return s.Points[i].Time.Before(s.Points[j].Time) })
		res.Series = append(res.Series, *s)
	}
	return res, nil
}

func seriesKey(row metricsRow, q domain.PlayVitalsQuery) (key, name string, labels map[string]string) {
	labels = map[string]string{}
	name = q.Metric
	for _, d := range row.Dimensions {
		if d.Dimension != q.Dimension {
			continue
		}
		v := d.StringValue
		if v == "" {
			v = d.Int64Value
		}
		labels[d.Dimension] = v
		name = v
		if d.ValueLabel != "" && d.ValueLabel != v {
			labels[d.Dimension+"_label"] = d.ValueLabel
			name = d.ValueLabel
		}
		return d.Dimension + "=" + v, name, labels
	}
	return "", name, labels
}

func (a *Adapter) point(row metricsRow, q domain.PlayVitalsQuery) domain.SeriesPoint {
	t := a.instant(row.StartTime).UTC()
	for _, m := range row.Metrics {
		if m.Metric != q.Metric || m.DecimalValue == nil {
			continue
		}
		v, err := strconv.ParseFloat(m.DecimalValue.Value, 64)
		if err != nil {
			return domain.SeriesPoint{Time: t}
		}
		switch q.As {
		case domain.PlayAsPercent:
			v *= 100
		case domain.PlayAsFreePercent:
			v = 100 - v*100
		}
		return domain.FinitePoint(t, v)
	}
	return domain.SeriesPoint{Time: t}
}

type errorIssue struct {
	Name                string `json:"name"`
	Type                string `json:"type"`
	Cause               string `json:"cause"`
	Location            string `json:"location"`
	ErrorReportCount    string `json:"errorReportCount"`
	DistinctUsers       string `json:"distinctUsers"`
	LastErrorReportTime string `json:"lastErrorReportTime"`
	IssueURI            string `json:"issueUri"`
	LastAppVersion      *struct {
		VersionCode string `json:"versionCode"`
	} `json:"lastAppVersion"`
}

var issueTypeFilter = map[string]string{
	domain.PlayIssuesCrash:    "CRASH",
	domain.PlayIssuesANR:      "APPLICATION_NOT_RESPONDING",
	domain.PlayIssuesNonFatal: "NON_FATAL",
}

var issueTypeName = map[string]string{
	"CRASH": domain.PlayIssuesCrash, "APPLICATION_NOT_RESPONDING": domain.PlayIssuesANR, "NON_FATAL": domain.PlayIssuesNonFatal,
}

// Table answers an issues query: the app's error issues of the last Days
// before At, most affected users first. Columns: issue, type, cause,
// location, users, reports, last_version, last_seen, link.
func (a *Adapter) Table(ctx context.Context, ds domain.DataSource, _ map[string]string, tq domain.TableQuery) (domain.TableResult, error) {
	q, pkg, err := a.parse(ds, tq.Query)
	if err != nil {
		return domain.TableResult{}, err
	}
	if !q.IsTable() {
		return domain.TableResult{}, fmt.Errorf("%w: a table reads issues; give issues (all, crash, anr or non_fatal)", port.ErrDataSourceQuery)
	}
	c, err := a.open(ctx, ds)
	if err != nil {
		return domain.TableResult{}, err
	}
	defer c.Close()

	end := tq.At.UTC().Truncate(time.Hour)
	start := end.Add(-time.Duration(q.Days) * 24 * time.Hour)
	params := url.Values{"pageSize": {strconv.Itoa(issuesPerPage)}, "orderBy": {"distinctUsers desc"}}
	for prefix, t := range map[string]time.Time{"interval.startTime.": start, "interval.endTime.": end} {
		params.Set(prefix+"year", strconv.Itoa(t.Year()))
		params.Set(prefix+"month", strconv.Itoa(int(t.Month())))
		params.Set(prefix+"day", strconv.Itoa(t.Day()))
		params.Set(prefix+"hours", strconv.Itoa(t.Hour()))
		params.Set(prefix+"timeZone.id", "UTC")
	}
	if f, ok := issueTypeFilter[q.Issues]; ok {
		params.Set("filter", "errorIssueType = "+f)
	}
	res := domain.TableResult{Columns: IssueColumns(), Rows: []map[string]any{}}
	path := "/v1beta1/apps/" + url.PathEscape(pkg) + "/errorIssues:search"
	for range maxPages {
		var out struct {
			ErrorIssues   []errorIssue `json:"errorIssues"`
			NextPageToken string       `json:"nextPageToken"`
		}
		if err := a.get(ctx, c, "search error issues", path, params, &out); err != nil {
			return domain.TableResult{}, err
		}
		for _, is := range out.ErrorIssues {
			if len(res.Rows) == maxIssueRows {
				res.Truncated = true
				return res, nil
			}
			res.Rows = append(res.Rows, issueRow(is))
		}
		if out.NextPageToken == "" {
			return res, nil
		}
		params.Set("pageToken", out.NextPageToken)
	}
	res.Truncated = true
	return res, nil
}

// IssueColumns are the crashes table's columns, in the order the recipe
// lists them.
func IssueColumns() []domain.TableColumn {
	return []domain.TableColumn{
		{Key: "issue", Label: "Issue"}, {Key: "type", Label: "Type"}, {Key: "cause", Label: "Cause"},
		{Key: "location", Label: "Location"}, {Key: "users", Label: "Users", Unit: "users"},
		{Key: "reports", Label: "Reports", Unit: "reports"}, {Key: "last_version", Label: "Last version"},
		{Key: "last_seen", Label: "Last seen"}, {Key: "link", Label: "Link"},
	}
}

func issueRow(is errorIssue) map[string]any {
	id := is.Name
	if i := strings.LastIndex(id, "/"); i >= 0 {
		id = id[i+1:]
	}
	row := map[string]any{
		"issue": id, "type": issueTypeName[is.Type], "cause": is.Cause, "location": is.Location,
		"users": count(is.DistinctUsers), "reports": count(is.ErrorReportCount),
		"last_seen": is.LastErrorReportTime, "link": playConsoleLink(is.IssueURI),
	}
	if row["type"] == "" {
		row["type"] = strings.ToLower(is.Type)
	}
	if is.LastAppVersion != nil {
		row["last_version"] = is.LastAppVersion.VersionCode
	}
	return row
}

// playConsoleLink keeps an issue's link only when it is Play Console's own
// https page: it is shown as a link, and the answer is not this server's to
// trust.
func playConsoleLink(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || !strings.EqualFold(u.Host, "play.google.com") {
		return ""
	}
	return u.String()
}

func count(s string) any {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return nil
	}
	return float64(n)
}

func (a *Adapter) get(ctx context.Context, c *httpapi.Conn, op, path string, query url.Values, out any) error {
	return a.call(ctx, c, op, httpapi.Request{Method: http.MethodGet, Path: path, Query: query, Limit: maxBody}, out)
}

func (a *Adapter) post(ctx context.Context, c *httpapi.Conn, op, path string, body, out any) error {
	return a.call(ctx, c, op, httpapi.Request{Method: http.MethodPost, Path: path, JSON: body, Limit: maxBody}, out)
}

func (a *Adapter) call(ctx context.Context, c *httpapi.Conn, op string, r httpapi.Request, out any) error {
	resp, err := c.Do(ctx, r)
	if err != nil {
		return err
	}
	switch {
	case resp.OK():
		if err := json.Unmarshal(resp.Body, out); err != nil {
			return fmt.Errorf("%s %s: the answer is not JSON", api, op)
		}
		return nil
	case resp.Status == http.StatusBadRequest:
		return fmt.Errorf("%w: %s", port.ErrDataSourceQuery, c.StatusError(op, resp).Error())
	case resp.Status == http.StatusForbidden:
		return fmt.Errorf("%s; the service account needs the Play Developer Reporting API enabled in its Google Cloud project and permission to view app information in Play Console", c.StatusError(op, resp))
	case resp.Status == http.StatusNotFound:
		return fmt.Errorf("%s %s: not found (HTTP 404) — is the package name right, and is the app on this Play Console account?", api, op)
	case resp.Status == http.StatusTooManyRequests:
		return fmt.Errorf("%s %s: rate limited (HTTP 429); try again in a minute", api, op)
	}
	return c.StatusError(op, resp)
}
