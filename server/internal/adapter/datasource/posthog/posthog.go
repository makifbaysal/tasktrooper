// Package posthog is the data source kind for PostHog (PostHog Cloud US or
// EU, or self-hosted), read through its Query API with a personal API key:
// page views, sessions, visitors, exceptions and web vitals as series, and
// the top pages, referrers, countries and devices as tables. Its query
// language is domain.WebQuery, which the kind compiles to HogQL itself, so a
// value from a param only ever lands in a quoted string literal.
package posthog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/datasource/httpapi"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	keyURL     = "url"
	keyProject = "project_id"
	keyAPIKey  = "personal_api_key"

	DefaultURL = "https://us.posthog.com"
	EUURL      = "https://eu.posthog.com"
	api        = "PostHog"
	authHint   = "the personal API key needs the Query Read scope on this project"

	defaultTimeout = 30 * time.Second
	maxBody        = 32 << 20
	// maxBuckets keeps a series under the 50 000 rows one query may return.
	maxBuckets   = 10000
	maxEvents    = 2000
	discoverDays = 7
)

var projectPattern = regexp.MustCompile(`^[0-9]{1,20}$`)

// vitalProperty is the property posthog-js sets on a $web_vitals event for
// each vital, in ms (CLS a score).
var vitalProperty = map[string]string{
	domain.WebMetricLCP: "$web_vitals_LCP_value",
	domain.WebMetricINP: "$web_vitals_INP_value",
	domain.WebMetricCLS: "$web_vitals_CLS_value",
	domain.WebMetricFCP: "$web_vitals_FCP_value",
}

// tableKeys are the properties each table groups $pageview events by, as
// the columns the kind answers them under.
var tableKeys = map[string][]struct{ column, label, property string }{
	domain.RecipeTablePages:     {{"path", "Path", "$pathname"}},
	domain.RecipeTableReferrers: {{"referrer", "Referrer", "$referring_domain"}},
	domain.RecipeTableCountries: {{"country", "Country", "$geoip_country_code"}},
	domain.RecipeTableDevices:   {{"device", "Device", "$device_type"}, {"browser", "Browser", "$browser"}, {"os", "OS", "$os"}},
}

type Adapter struct {
	policy  urlguard.Policy
	timeout time.Duration
}

var (
	_ port.DataSourceKindAdapter = (*Adapter)(nil)
	_ port.MetricsQuerier        = (*Adapter)(nil)
	_ port.TableQuerier          = (*Adapter)(nil)
	_ port.Discoverer            = (*Adapter)(nil)
)

// New answers an adapter that refuses every destination until SetURLPolicy;
// datasource.NewService sets it.
func New() *Adapter { return &Adapter{timeout: defaultTimeout} }

func (a *Adapter) SetURLPolicy(p urlguard.Policy) { a.policy = p }

func (a *Adapter) Kind() domain.DataSourceKind { return domain.DataSourceKindPostHog }

func (a *Adapter) Capabilities() []domain.Capability {
	return []domain.Capability{domain.CapabilityMetrics, domain.CapabilityRUM}
}

func (a *Adapter) Schema() domain.DataSourceSchema {
	return domain.DataSourceSchema{
		Label:       "PostHog",
		Description: "Page views, sessions, visitors, JavaScript exceptions and web vitals from PostHog, with the top pages, referrers, countries and devices. Nothing to install beyond posthog-js.",
		Fields: []domain.DataSourceField{
			{
				Key: keyURL, Label: "PostHog URL", Type: domain.FieldURL, Required: true, Default: DefaultURL,
				Placeholder: DefaultURL,
				Help:        "https://us.posthog.com or https://eu.posthog.com for PostHog Cloud, or your self-hosted PostHog's address.",
			},
			{
				Key: keyProject, Label: "Project ID", Type: domain.FieldText, Required: true,
				Placeholder: "12345",
				Help:        "The number in the project's URL (/project/<id>) or under Project settings.",
			},
			{
				Key: keyAPIKey, Label: "Personal API key", Type: domain.FieldSecret, Required: true, Secret: true,
				Help: "A personal API key (phx_…) with the Query Read scope on this project.",
			},
		},
	}
}

type conn struct {
	*httpapi.Conn
	project string
}

func (a *Adapter) open(ctx context.Context, ds domain.DataSource, secrets map[string]string) (*conn, error) {
	project := strings.TrimSpace(ds.Config[keyProject])
	if !projectPattern.MatchString(project) {
		return nil, fmt.Errorf("%q is not a PostHog project id", project)
	}
	key := secrets[keyAPIKey]
	if key == "" {
		return nil, errors.New("no personal API key is set")
	}
	base := strings.TrimSpace(ds.Config[keyURL])
	if base == "" {
		base = DefaultURL
	}
	c, err := httpapi.Open(ctx, a.policy, httpapi.Endpoint{
		Base:    base,
		Headers: []domain.HeaderLine{{Name: "Authorization", Value: "Bearer " + key}},
		Masks:   []string{key},
		API:     api,
	}, a.timeout)
	if err != nil {
		return nil, err
	}
	return &conn{Conn: c, project: project}, nil
}

// hogQL runs one HogQL query and answers its rows.
func (a *Adapter) hogQL(ctx context.Context, c *conn, op, query string) ([][]any, error) {
	resp, err := c.Do(ctx, httpapi.Request{
		Method: http.MethodPost,
		Path:   "/api/projects/" + c.project + "/query/",
		JSON:   map[string]any{"query": map[string]any{"kind": "HogQLQuery", "query": query}},
		Limit:  maxBody,
	})
	if err != nil {
		return nil, err
	}
	if !resp.OK() {
		return nil, c.Refusal(op, resp, detail(resp.Body), authHint)
	}
	var out struct {
		Results [][]any `json:"results"`
		Error   string  `json:"error"`
	}
	dec := json.NewDecoder(bytes.NewReader(resp.Body))
	dec.UseNumber()
	if err := dec.Decode(&out); err != nil {
		return nil, fmt.Errorf("%s %s: the answer is not what PostHog sends — is the URL right?", api, op)
	}
	if out.Error != "" {
		return nil, fmt.Errorf("%w: %s %s: %s", port.ErrDataSourceQuery, api, op, c.Scrub(out.Error))
	}
	return out.Results, nil
}

// detail is PostHog's {"detail": "…"} (a DRF error) or {"error": "…"}.
func detail(raw []byte) string {
	var body struct {
		Detail string `json:"detail"`
		Error  string `json:"error"`
	}
	if json.Unmarshal(raw, &body) != nil {
		return ""
	}
	if body.Detail != "" {
		return body.Detail
	}
	return body.Error
}

func (a *Adapter) Verify(ctx context.Context, ds domain.DataSource, secrets map[string]string) (domain.DataSourceInfo, error) {
	c, err := a.open(ctx, ds, secrets)
	if err != nil {
		return domain.DataSourceInfo{}, err
	}
	defer c.Close()
	rows, err := a.hogQL(ctx, c, "read the project", "SELECT count() FROM events WHERE timestamp >= now() - INTERVAL 1 DAY")
	if err != nil {
		return domain.DataSourceInfo{}, err
	}
	n := 0.0
	if len(rows) > 0 && len(rows[0]) > 0 {
		n, _ = number(rows[0][0])
	}
	return domain.DataSourceInfo{Detail: fmt.Sprintf("PostHog · project %s · %s events in the last day", c.project, strconv.FormatFloat(n, 'f', -1, 64))}, nil
}

// Discover lists the event names the project recorded in the last week, the
// most frequent first.
func (a *Adapter) Discover(ctx context.Context, ds domain.DataSource, secrets map[string]string) (domain.Inventory, error) {
	c, err := a.open(ctx, ds, secrets)
	if err != nil {
		return domain.Inventory{}, err
	}
	defer c.Close()
	q := fmt.Sprintf("SELECT event, count() AS c FROM events WHERE timestamp >= now() - INTERVAL %d DAY GROUP BY event ORDER BY c DESC LIMIT %d", discoverDays, maxEvents+1)
	rows, err := a.hogQL(ctx, c, "list events", q)
	if err != nil {
		return domain.Inventory{}, err
	}
	inv := domain.Inventory{Metrics: []string{}}
	for _, r := range rows {
		if len(inv.Metrics) == maxEvents {
			inv.Truncated = true
			break
		}
		if len(r) > 0 {
			if name, ok := r[0].(string); ok && name != "" {
				inv.Metrics = append(inv.Metrics, name)
			}
		}
	}
	return inv, nil
}

func parse(raw string) (domain.WebQuery, error) {
	q, err := domain.ParseWebQuery(domain.DataSourceKindPostHog, raw)
	if err != nil {
		return q, fmt.Errorf("%w: %s", port.ErrDataSourceQuery, err.Error())
	}
	return q, nil
}

// literal quotes a value for HogQL. HogQL's escaping inside a string is
// ClickHouse's, which this does not rely on: a value holding a quote, a
// backslash or a control character is refused.
func literal(field, v string) (string, error) {
	if strings.ContainsAny(v, `'\`) || strings.ContainsFunc(v, unicode.IsControl) {
		return "", fmt.Errorf("%w: %s %q holds a quote, a backslash or a control character", port.ErrDataSourceQuery, field, v)
	}
	return "'" + v + "'", nil
}

func window(from, to time.Time) string {
	return fmt.Sprintf("timestamp >= fromUnixTimestamp(%d) AND timestamp < fromUnixTimestamp(%d)", from.Unix(), to.Unix())
}

func hostFilter(host string) (string, error) {
	if host == "" {
		return "", nil
	}
	lit, err := literal("host", host)
	if err != nil {
		return "", err
	}
	return " AND properties.$host = " + lit, nil
}

// seriesSQL is the HogQL of one series: an aggregate per bucket of iv
// seconds since the epoch.
func seriesSQL(q domain.WebQuery, from, to time.Time, iv int64) (string, error) {
	event, agg := "$pageview", "count()"
	switch q.Metric {
	case domain.WebMetricSessions:
		agg = "uniq(properties.$session_id)"
	case domain.WebMetricVisitors:
		agg = "uniq(person_id)"
	case domain.WebMetricJSErrors:
		event = "$exception"
	case domain.WebMetricEvents:
		event = q.Event
	default:
		if prop, ok := vitalProperty[q.Metric]; ok {
			event = "$web_vitals"
			agg = fmt.Sprintf("quantile(%s)(toFloat(properties.%s))", strconv.FormatFloat(q.Quantile, 'f', -1, 64), prop)
		}
	}
	ev, err := literal("event", event)
	if err != nil {
		return "", err
	}
	host, err := hostFilter(q.Host)
	if err != nil {
		return "", err
	}
	extra := ""
	if prop, ok := vitalProperty[q.Metric]; ok {
		extra = " AND properties." + prop + " IS NOT NULL"
	}
	return fmt.Sprintf("SELECT intDiv(toUnixTimestamp(timestamp), %d) * %d AS t, %s AS v FROM events WHERE event = %s AND %s%s%s GROUP BY t ORDER BY t LIMIT %d",
		iv, iv, agg, ev, window(from, to), host, extra, maxBuckets+1), nil
}

func bucket(sq domain.SeriesQuery) int64 {
	iv := max(int64(math.Ceil(sq.Step.Seconds())), 60)
	if span := int64(sq.To.Sub(sq.From).Seconds()); span/iv > maxBuckets {
		iv = span/maxBuckets + 1
	}
	return iv
}

// Series reads one metric per bucket of the step (at least a minute). A
// bucket with no events is 0 for a count and a gap for a vital; page_views
// and js_errors are per second.
func (a *Adapter) Series(ctx context.Context, ds domain.DataSource, secrets map[string]string, sq domain.SeriesQuery) (domain.SeriesResult, error) {
	q, err := parse(sq.Query)
	if err != nil {
		return domain.SeriesResult{}, err
	}
	if q.IsTable() {
		return domain.SeriesResult{}, fmt.Errorf("%w: a table query (table %q) is no series", port.ErrDataSourceQuery, q.Table)
	}
	iv := bucket(sq)
	start := sq.From.Unix() / iv * iv
	sql, err := seriesSQL(q, time.Unix(start, 0), sq.To, iv)
	if err != nil {
		return domain.SeriesResult{}, err
	}
	c, err := a.open(ctx, ds, secrets)
	if err != nil {
		return domain.SeriesResult{}, err
	}
	defer c.Close()
	rows, err := a.hogQL(ctx, c, "read "+q.Metric, sql)
	if err != nil {
		return domain.SeriesResult{}, err
	}
	values := map[int64]*float64{}
	for _, r := range rows {
		if len(r) < 2 {
			continue
		}
		t, ok := number(r[0])
		if !ok {
			continue
		}
		if v, ok := number(r[1]); ok {
			values[int64(t)] = &v
		}
	}
	perSecond := q.Metric == domain.WebMetricPageViews || q.Metric == domain.WebMetricJSErrors
	vital := domain.IsWebVital(q.Metric)
	s := domain.Series{Name: q.Metric, Labels: map[string]string{}, Points: []domain.SeriesPoint{}}
	if q.Host != "" {
		s.Labels["host"] = q.Host
	}
	for t := start; t < sq.To.Unix(); t += iv {
		at := time.Unix(t, 0).UTC()
		v, ok := values[t]
		switch {
		case !ok && vital:
			s.Points = append(s.Points, domain.SeriesPoint{Time: at})
		case !ok:
			s.Points = append(s.Points, domain.FinitePoint(at, 0))
		case perSecond:
			s.Points = append(s.Points, domain.FinitePoint(at, *v/float64(iv)))
		default:
			s.Points = append(s.Points, domain.FinitePoint(at, *v))
		}
	}
	return domain.SeriesResult{Series: []domain.Series{s}, Warnings: []string{}, Step: time.Duration(iv) * time.Second}, nil
}

// TableColumns are the columns each table answers, in the order the recipes
// show them.
func TableColumns(table string) []domain.TableColumn {
	var cols []domain.TableColumn
	for _, k := range tableKeys[table] {
		cols = append(cols, domain.TableColumn{Key: k.column, Label: k.label})
	}
	cols = append(cols,
		domain.TableColumn{Key: "views", Label: "Views", Unit: "views"},
		domain.TableColumn{Key: "visitors", Label: "Visitors", Unit: "visitors"})
	if table == domain.RecipeTablePages {
		cols = append(cols, domain.TableColumn{Key: "lcp_p75", Label: "LCP p75", Unit: "ms"})
	}
	return cols
}

func tableSQL(q domain.WebQuery, at time.Time) (string, error) {
	keys := tableKeys[q.Table]
	var selects, groups []string
	for _, k := range keys {
		selects = append(selects, fmt.Sprintf("properties.%s AS %s", k.property, k.column))
		groups = append(groups, k.column)
	}
	host, err := hostFilter(q.Host)
	if err != nil {
		return "", err
	}
	events := "event = '$pageview'"
	selects = append(selects, "countIf(event = '$pageview') AS views", "uniq(if(event = '$pageview', person_id, NULL)) AS visitors")
	if q.Table == domain.RecipeTablePages {
		events = "event IN ('$pageview', '$web_vitals')"
		selects = append(selects, fmt.Sprintf("quantile(%s)(if(event = '$web_vitals', toFloat(properties.%s), NULL)) AS lcp_p75",
			strconv.FormatFloat(domain.DefaultWebVitalQuantile, 'f', -1, 64), vitalProperty[domain.WebMetricLCP]))
	}
	from := at.Add(-time.Duration(q.Days) * 24 * time.Hour)
	return fmt.Sprintf("SELECT %s FROM events WHERE %s AND %s%s GROUP BY %s HAVING views > 0 ORDER BY views DESC LIMIT %d",
		strings.Join(selects, ", "), events, window(from, at), host, strings.Join(groups, ", "), q.Limit+1), nil
}

// Table answers a web table over the last Days before At, the most viewed
// first: its key columns, views ($pageview events), visitors (distinct
// persons) and, for pages, LCP p75 in ms from $web_vitals events.
func (a *Adapter) Table(ctx context.Context, ds domain.DataSource, secrets map[string]string, tq domain.TableQuery) (domain.TableResult, error) {
	q, err := parse(tq.Query)
	if err != nil {
		return domain.TableResult{}, err
	}
	if !q.IsTable() {
		return domain.TableResult{}, fmt.Errorf("%w: a series query (metric %q) is no table; name a table", port.ErrDataSourceQuery, q.Metric)
	}
	sql, err := tableSQL(q, tq.At)
	if err != nil {
		return domain.TableResult{}, err
	}
	c, err := a.open(ctx, ds, secrets)
	if err != nil {
		return domain.TableResult{}, err
	}
	defer c.Close()
	rows, err := a.hogQL(ctx, c, "read the "+q.Table+" table", sql)
	if err != nil {
		return domain.TableResult{}, err
	}
	cols := TableColumns(q.Table)
	res := domain.TableResult{Columns: cols, Rows: make([]map[string]any, 0, len(rows))}
	for _, r := range rows {
		if len(res.Rows) == q.Limit {
			res.Truncated = true
			break
		}
		row := map[string]any{}
		for i, col := range cols {
			if i >= len(r) {
				break
			}
			if i < len(tableKeys[q.Table]) {
				row[col.Key] = text(r[i])
				continue
			}
			if v, ok := number(r[i]); ok {
				row[col.Key] = v
			} else {
				row[col.Key] = nil
			}
		}
		res.Rows = append(res.Rows, row)
	}
	return res, nil
}

func number(v any) (float64, bool) {
	switch n := v.(type) {
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	case float64:
		return n, true
	case string:
		f, err := strconv.ParseFloat(n, 64)
		return f, err == nil
	}
	return 0, false
}

func text(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case json.Number:
		return t.String()
	}
	return fmt.Sprint(v)
}
