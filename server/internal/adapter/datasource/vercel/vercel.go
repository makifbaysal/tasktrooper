// Package vercel is the data source kind for a Vercel team's own analytics,
// read with the token of a connected Vercel account: page views and visitors
// from Web Analytics (GET /v1/query/web-analytics/visits/aggregate) and the
// Core Web Vitals from Speed Insights (POST /v2/observability/query), per
// project. Nothing is installed in the site: Vercel already measures it once
// Web Analytics and Speed Insights are switched on for the project. Its query
// language is domain.WebQuery with the project's id in "project".
//
// Runtime logs and deployments of the same account are read by the cloud
// provider (adapter/cloud/vercel), not here.
package vercel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	cloudvercel "github.com/makifbaysal/tasktrooper/server/internal/adapter/cloud/vercel"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/datasource/httpapi"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	keyAccount = "cloud_account_id"

	DefaultURL = "https://api.vercel.com"
	api        = "Vercel"
	authHint   = "the token must be allowed to read the project's analytics (a team token with access to the project)"

	defaultTimeout = 30 * time.Second
	maxBody        = 16 << 20

	visitsPath  = "/v1/query/web-analytics/visits/aggregate"
	observePath = "/v2/observability/query"
	userPath    = "/v2/user"
	projectPath = "/v9/projects/"

	// maxGroupLimit is Web Analytics' largest page of grouped rows.
	maxGroupLimit = 100
	// othersRow is what Web Analytics calls the groups beyond the limit.
	othersRow = "Others"
)

var vitalMetrics = map[string]string{
	domain.WebMetricLCP: "vercel.speed_insights.lcp_ms",
	domain.WebMetricINP: "vercel.speed_insights.inp_ms",
	domain.WebMetricCLS: "vercel.speed_insights.cls",
	domain.WebMetricFCP: "vercel.speed_insights.fcp_ms",
}

// percentiles are the ones Speed Insights aggregates.
var percentiles = []float64{0.5, 0.75, 0.9, 0.95, 0.99}

var tableDims = map[string]struct{ dimension, column, label string }{
	domain.RecipeTablePages:     {"requestPath", "path", "Path"},
	domain.RecipeTableReferrers: {"referrerHostname", "referrer", "Source"},
	domain.RecipeTableCountries: {"country", "country", "Country"},
	domain.RecipeTableDevices:   {"deviceType", "device", "Device"},
}

type Adapter struct {
	policy   urlguard.Policy
	accounts port.CloudAccountReader
	baseURL  string
	timeout  time.Duration
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
func New() *Adapter { return &Adapter{baseURL: DefaultURL, timeout: defaultTimeout} }

func (a *Adapter) SetURLPolicy(p urlguard.Policy) { a.policy = p }

func (a *Adapter) SetCloudAccounts(r port.CloudAccountReader) { a.accounts = r }

func (a *Adapter) SetBaseURL(u string) { a.baseURL = strings.TrimRight(u, "/") }

func (a *Adapter) Kind() domain.DataSourceKind { return domain.DataSourceKindVercel }

func (a *Adapter) Capabilities() []domain.Capability {
	return []domain.Capability{domain.CapabilityMetrics, domain.CapabilityRUM}
}

func (a *Adapter) Schema() domain.DataSourceSchema {
	return domain.DataSourceSchema{
		Label:       "Vercel",
		Description: "Page views, visitors and Core Web Vitals of your Vercel projects, from Web Analytics and Speed Insights, read with a connected Vercel account. Nothing to install.",
		Fields: []domain.DataSourceField{
			{
				Key: keyAccount, Label: "Vercel account", Type: domain.FieldCloudAccount,
				Provider: domain.CloudVercel, Required: true,
				Help: "Web Analytics and Speed Insights are switched on per project in Vercel, and the free Hobby plan allows each on one project only; a project without them shows as not enabled, and its logs and deployments are read as usual.",
			},
		},
	}
}

// access is what one call needs of the connected account.
type access struct {
	token string
	team  string
	conn  *httpapi.Conn
}

func (a *Adapter) open(ctx context.Context, ds domain.DataSource) (*access, error) {
	cred, err := a.credential(ctx, ds)
	if err != nil {
		return nil, err
	}
	token := strings.TrimSpace(cred.Fields["token"])
	conn, err := httpapi.Open(ctx, a.policy, httpapi.Endpoint{
		Base:    a.baseURL,
		Headers: []domain.HeaderLine{{Name: "Authorization", Value: "Bearer " + token}},
		Masks:   []string{token},
		API:     api,
	}, a.timeout)
	if err != nil {
		return nil, err
	}
	return &access{token: token, team: strings.TrimSpace(cred.Fields["team_id"]), conn: conn}, nil
}

func (a *Adapter) credential(ctx context.Context, ds domain.DataSource) (domain.CloudCredential, error) {
	if a.accounts == nil {
		return domain.CloudCredential{}, errors.New("cloud accounts are not available here")
	}
	id, err := uuid.Parse(ds.Config[keyAccount])
	if err != nil {
		return domain.CloudCredential{}, errors.New("no Vercel account is selected")
	}
	cred, err := a.accounts.CloudCredential(ctx, id)
	switch {
	case errors.Is(err, port.ErrNotFound):
		return domain.CloudCredential{}, errors.New("the Vercel account is no longer connected")
	case err != nil:
		return domain.CloudCredential{}, fmt.Errorf("reading the Vercel account: %w", err)
	case cred.Provider != domain.CloudVercel:
		return domain.CloudCredential{}, errors.New("the selected cloud account is not a Vercel account")
	case strings.TrimSpace(cred.Fields["token"]) == "":
		return domain.CloudCredential{}, errors.New("the Vercel account has no token")
	}
	return cred, nil
}

func (x *access) teamQuery(q url.Values) url.Values {
	if q == nil {
		q = url.Values{}
	}
	if x.team != "" {
		q.Set("teamId", x.team)
	}
	return q
}

func (a *Adapter) get(ctx context.Context, x *access, op, path string, q url.Values, out any) error {
	resp, err := x.conn.Do(ctx, httpapi.Request{Method: http.MethodGet, Path: path, Query: q, Limit: maxBody})
	if err != nil {
		return err
	}
	return decode(x.conn, op, resp, out, false)
}

const (
	webAnalyticsOff  = "Web Analytics is not enabled for this project in Vercel. Turn it on in the project's Analytics tab (the free Hobby plan allows it on one project only)."
	speedInsightsOff = "Speed Insights is not enabled for this project in Vercel. Turn it on in the project's Speed Insights tab (the free Hobby plan allows it on one project only)."
)

// notEnabled is a project Vercel is not measuring: a state to show with its
// hint, not a failure of the source.
type notEnabled string

func (e notEnabled) Error() string { return string(e) }

func (notEnabled) Is(target error) bool { return target == port.ErrSignalNotEnabled }

func switchedOffMessage(vital bool) notEnabled {
	if vital {
		return speedInsightsOff
	}
	return webAnalyticsOff
}

// checkEnabled refuses a read of a feature the project has switched off. A
// project Vercel will not describe (no access to it, an unknown id) is read
// anyway: the read's own answer says what is wrong.
func (a *Adapter) checkEnabled(ctx context.Context, x *access, project string, vital bool) error {
	resp, err := x.conn.Do(ctx, httpapi.Request{
		Method: http.MethodGet, Path: projectPath + url.PathEscape(project), Query: x.teamQuery(nil), Limit: maxBody,
	})
	if err != nil || !resp.OK() {
		return nil
	}
	state, err := cloudvercel.ProjectAnalytics(resp.Body)
	if err != nil {
		return nil
	}
	feature := state.WebAnalytics
	if vital {
		feature = state.SpeedInsights
	}
	if feature == domain.AnalyticsNotEnabled {
		return switchedOffMessage(vital)
	}
	return nil
}

func decode(c *httpapi.Conn, op string, resp httpapi.Response, out any, vital bool) error {
	if !resp.OK() {
		if resp.Status == http.StatusBadRequest && strings.Contains(strings.ToLower(c.Detail(resp)), "not enabled") {
			return switchedOffMessage(vital)
		}
		return c.Refusal(op, resp, c.Detail(resp), authHint)
	}
	if err := json.Unmarshal(resp.Body, out); err != nil {
		return fmt.Errorf("%s %s: the answer is not what Vercel sends", api, op)
	}
	return nil
}

// owner is the id an observability scope names: the team's, or for a
// personal account the user's own.
func (a *Adapter) owner(ctx context.Context, x *access) (string, error) {
	if x.team != "" {
		return x.team, nil
	}
	var out struct {
		User struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	if err := a.get(ctx, x, "read the account", userPath, nil, &out); err != nil {
		return "", err
	}
	if out.User.ID == "" {
		return "", fmt.Errorf("%s: the account has no user id", api)
	}
	return out.User.ID, nil
}

func (a *Adapter) Verify(ctx context.Context, ds domain.DataSource, _ map[string]string) (domain.DataSourceInfo, error) {
	x, err := a.open(ctx, ds)
	if err != nil {
		return domain.DataSourceInfo{}, err
	}
	defer x.conn.Close()
	var out struct {
		User struct {
			Username string `json:"username"`
		} `json:"user"`
	}
	if err := a.get(ctx, x, "read the account", userPath, nil, &out); err != nil {
		return domain.DataSourceInfo{}, err
	}
	who := out.User.Username
	if x.team != "" {
		who = "team " + x.team
	}
	return domain.DataSourceInfo{Detail: "Vercel · " + who}, nil
}

// Discover names what the account can be read for: Vercel answers per
// project, so which project has Web Analytics or Speed Insights on is learnt
// when its signals are read, and the recipes for a bound Vercel project are
// offered from that binding.
func (a *Adapter) Discover(ctx context.Context, ds domain.DataSource, secrets map[string]string) (domain.Inventory, error) {
	if _, err := a.Verify(ctx, ds, secrets); err != nil {
		return domain.Inventory{}, err
	}
	return domain.Inventory{Metrics: []string{"pageview", "speed_insights"}}, nil
}

func parse(raw string) (domain.WebQuery, error) {
	q, err := domain.ParseWebQuery(domain.DataSourceKindVercel, raw)
	if err != nil {
		return q, fmt.Errorf("%w: %s", port.ErrDataSourceQuery, err.Error())
	}
	if q.Host != "" {
		return q, fmt.Errorf("%w: Vercel analytics cannot be narrowed to a host; leave host out", port.ErrDataSourceQuery)
	}
	if q.Project == "" {
		return q, fmt.Errorf("%w: name the Vercel project (its id, prj_…) in \"project\"", port.ErrDataSourceQuery)
	}
	return q, nil
}

type visitRow struct {
	Timestamp string   `json:"timestamp"`
	Pageviews *float64 `json:"pageviews"`
	Visitors  *float64 `json:"visitors"`
}

func (r visitRow) value(metric string) (float64, bool) {
	v := r.Pageviews
	if metric == domain.WebMetricVisitors {
		v = r.Visitors
	}
	if v == nil {
		return 0, false
	}
	return *v, true
}

// Series reads one metric per hour (a step under a day) or per day (UTC). A
// bucket Vercel has no row for is 0 for the counts and a gap for a vital;
// page_views is per second of its bucket.
func (a *Adapter) Series(ctx context.Context, ds domain.DataSource, _ map[string]string, sq domain.SeriesQuery) (domain.SeriesResult, error) {
	q, err := parse(sq.Query)
	if err != nil {
		return domain.SeriesResult{}, err
	}
	if q.IsTable() {
		return domain.SeriesResult{}, fmt.Errorf("%w: a table query (table %q) is no series", port.ErrDataSourceQuery, q.Table)
	}
	hourly := sq.Step < 24*time.Hour
	step := 24 * time.Hour
	if hourly {
		step = time.Hour
	}
	x, err := a.open(ctx, ds)
	if err != nil {
		return domain.SeriesResult{}, err
	}
	defer x.conn.Close()
	if err := a.checkEnabled(ctx, x, q.Project, domain.IsWebVital(q.Metric)); err != nil {
		return domain.SeriesResult{}, err
	}

	var values map[int64]float64
	if domain.IsWebVital(q.Metric) {
		values, err = a.vital(ctx, x, q, sq, hourly)
	} else {
		values, err = a.visits(ctx, x, q, sq, hourly)
	}
	if err != nil {
		return domain.SeriesResult{}, err
	}

	counts := !domain.IsWebVital(q.Metric)
	series := domain.Series{Name: q.Metric, Labels: map[string]string{"project": q.Project}, Points: []domain.SeriesPoint{}}
	from := sq.From.UTC()
	t := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.UTC)
	if hourly {
		t = time.Date(from.Year(), from.Month(), from.Day(), from.Hour(), 0, 0, 0, time.UTC)
	}
	for ; t.Before(sq.To); t = t.Add(step) {
		v, ok := values[t.Unix()]
		switch {
		case !ok && counts:
			series.Points = append(series.Points, domain.FinitePoint(t, 0))
		case !ok:
			series.Points = append(series.Points, domain.SeriesPoint{Time: t})
		default:
			if q.Metric == domain.WebMetricPageViews {
				v /= step.Seconds()
			}
			series.Points = append(series.Points, domain.FinitePoint(t, v))
		}
	}
	return domain.SeriesResult{Series: []domain.Series{series}, Warnings: []string{}, Step: step}, nil
}

func (a *Adapter) visits(ctx context.Context, x *access, q domain.WebQuery, sq domain.SeriesQuery, hourly bool) (map[int64]float64, error) {
	by := "day"
	if hourly {
		by = "hour"
	}
	query := x.teamQuery(url.Values{})
	query.Set("projectId", q.Project)
	query.Set("since", sq.From.UTC().Format(time.RFC3339))
	query.Set("until", sq.To.UTC().Format(time.RFC3339))
	query.Set("by", by)
	var out struct {
		Data []visitRow `json:"data"`
	}
	if err := a.get(ctx, x, "read "+q.Metric, visitsPath, query, &out); err != nil {
		return nil, err
	}
	values := map[int64]float64{}
	for _, r := range out.Data {
		t, err := time.Parse(time.RFC3339, r.Timestamp)
		if err != nil {
			continue
		}
		if v, ok := r.value(q.Metric); ok {
			values[t.UTC().Unix()] = v
		}
	}
	return values, nil
}

// aggregation is the Speed Insights percentile nearest to quantile, as
// Vercel names it (p75).
func aggregation(quantile float64) string {
	best := percentiles[0]
	for _, p := range percentiles {
		if math.Abs(p-quantile) < math.Abs(best-quantile) {
			best = p
		}
	}
	return fmt.Sprintf("p%d", int(math.Round(best*100)))
}

func (a *Adapter) vital(ctx context.Context, x *access, q domain.WebQuery, sq domain.SeriesQuery, hourly bool) (map[int64]float64, error) {
	owner, err := a.owner(ctx, x)
	if err != nil {
		return nil, err
	}
	metric, agg := vitalMetrics[q.Metric], aggregation(q.Quantile)
	granularity := map[string]int{"days": 1}
	if hourly {
		granularity = map[string]int{"hours": 1}
	}
	body := map[string]any{
		"metric":      metric,
		"aggregation": agg,
		"scope":       map[string]any{"type": "project", "ownerId": owner, "projectIds": []string{q.Project}},
		"startTime":   sq.From.UTC().Format(time.RFC3339),
		"endTime":     sq.To.UTC().Format(time.RFC3339),
		"granularity": granularity,
	}
	resp, err := x.conn.Do(ctx, httpapi.Request{
		Method: http.MethodPost, Path: observePath, Query: x.teamQuery(nil), JSON: body, Limit: maxBody,
	})
	if err != nil {
		return nil, err
	}
	var out struct {
		Data []map[string]any `json:"data"`
	}
	if err := decode(x.conn, "read "+q.Metric, resp, &out, true); err != nil {
		return nil, err
	}
	// A row is {"timestamp": …, "<metric with _ for .>_<aggregation>": value}.
	key := strings.ReplaceAll(metric, ".", "_") + "_" + agg
	values := map[int64]float64{}
	for _, row := range out.Data {
		ts, _ := row["timestamp"].(string)
		t, err := time.Parse(time.RFC3339, ts)
		if err != nil {
			continue
		}
		if v, ok := row[key].(float64); ok {
			values[t.UTC().Unix()] = v
		}
	}
	return values, nil
}

// TableColumns are the columns each table answers, in the order the recipes
// show them. Vercel groups devices by device type only.
func TableColumns(table string) []domain.TableColumn {
	d := tableDims[table]
	return []domain.TableColumn{
		{Key: d.column, Label: d.label},
		{Key: "views", Label: "Views", Unit: "views"},
		{Key: "visitors", Label: "Visitors", Unit: "visitors"},
	}
}

// Table answers a web table over the last Days before At, the most visited
// first: its dimension, views (pageviews) and visitors. The rows Web
// Analytics folds into "Others" past the limit are left out.
func (a *Adapter) Table(ctx context.Context, ds domain.DataSource, _ map[string]string, tq domain.TableQuery) (domain.TableResult, error) {
	q, err := parse(tq.Query)
	if err != nil {
		return domain.TableResult{}, err
	}
	if !q.IsTable() {
		return domain.TableResult{}, fmt.Errorf("%w: a series query (metric %q) is no table; name a table", port.ErrDataSourceQuery, q.Metric)
	}
	dim := tableDims[q.Table]
	x, err := a.open(ctx, ds)
	if err != nil {
		return domain.TableResult{}, err
	}
	defer x.conn.Close()
	if err := a.checkEnabled(ctx, x, q.Project, false); err != nil {
		return domain.TableResult{}, err
	}
	from := tq.At.Add(-time.Duration(q.Days) * 24 * time.Hour)
	query := x.teamQuery(url.Values{})
	query.Set("projectId", q.Project)
	query.Set("since", from.UTC().Format(time.RFC3339))
	query.Set("until", tq.At.UTC().Format(time.RFC3339))
	query.Set("by", dim.dimension)
	query.Set("limit", fmt.Sprint(min(q.Limit+1, maxGroupLimit)))
	var out struct {
		Data []map[string]any `json:"data"`
	}
	if err := a.get(ctx, x, "read the "+q.Table+" table", visitsPath, query, &out); err != nil {
		return domain.TableResult{}, err
	}
	res := domain.TableResult{Columns: TableColumns(q.Table), Rows: make([]map[string]any, 0, len(out.Data))}
	limit := min(q.Limit, maxGroupLimit)
	for _, r := range out.Data {
		name, _ := r[dim.dimension].(string)
		if name == othersRow {
			res.Truncated = true
			continue
		}
		if len(res.Rows) == limit {
			res.Truncated = true
			break
		}
		row := map[string]any{dim.column: name, "views": nil, "visitors": nil}
		if v, ok := r["pageviews"].(float64); ok {
			row["views"] = v
		}
		if v, ok := r["visitors"].(float64); ok {
			row["visitors"] = v
		}
		res.Rows = append(res.Rows, row)
	}
	return res, nil
}
