// Package plausible is the data source kind for Plausible Analytics
// (plausible.io or self-hosted), read through its Stats API v2 with a Stats
// API key: page views, visits and visitors as series by hour or day, and the
// top pages, sources, countries and devices as tables. Its query language is
// domain.WebQuery.
package plausible

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/datasource/httpapi"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	keyURL    = "url"
	keySite   = "site_id"
	keyAPIKey = "api_key"

	DefaultURL = "https://plausible.io"
	api        = "Plausible"
	authHint   = "the key must be a Stats API key of the team that owns the site"

	defaultTimeout = 30 * time.Second
	maxBody        = 32 << 20
	queryPath      = "/api/v2/query"
	hourLayout     = "2006-01-02 15:04:05"
)

var metricNames = map[string]string{
	domain.WebMetricPageViews: "pageviews",
	domain.WebMetricSessions:  "visits",
	domain.WebMetricVisitors:  "visitors",
}

var tableDims = map[string][]struct{ column, label, dimension string }{
	domain.RecipeTablePages:     {{"path", "Path", "event:page"}},
	domain.RecipeTableReferrers: {{"referrer", "Source", "visit:source"}},
	domain.RecipeTableCountries: {{"country", "Country", "visit:country"}},
	domain.RecipeTableDevices:   {{"device", "Device", "visit:device"}, {"browser", "Browser", "visit:browser"}, {"os", "OS", "visit:os"}},
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

func (a *Adapter) Kind() domain.DataSourceKind { return domain.DataSourceKindPlausible }

func (a *Adapter) Capabilities() []domain.Capability {
	return []domain.Capability{domain.CapabilityMetrics, domain.CapabilityRUM}
}

func (a *Adapter) Schema() domain.DataSourceSchema {
	return domain.DataSourceSchema{
		Label:       "Plausible",
		Description: "Page views, visits and visitors from Plausible Analytics, with the top pages, sources, countries and devices.",
		Fields: []domain.DataSourceField{
			{
				Key: keyURL, Label: "Plausible URL", Type: domain.FieldURL, Required: true, Default: DefaultURL,
				Placeholder: DefaultURL,
				Help:        "https://plausible.io, or your self-hosted Plausible's address.",
			},
			{
				Key: keySite, Label: "Site", Type: domain.FieldText, Required: true,
				Placeholder: "example.com",
				Help:        "The site's domain as it is named in Plausible.",
			},
			{
				Key: keyAPIKey, Label: "Stats API key", Type: domain.FieldSecret, Required: true, Secret: true,
				Help: "A Stats API key (Account settings → API keys) of the team that owns the site.",
			},
		},
	}
}

func (a *Adapter) open(ctx context.Context, ds domain.DataSource, secrets map[string]string) (*httpapi.Conn, error) {
	key := secrets[keyAPIKey]
	if key == "" {
		return nil, errors.New("no Stats API key is set")
	}
	base := strings.TrimSpace(ds.Config[keyURL])
	if base == "" {
		base = DefaultURL
	}
	return httpapi.Open(ctx, a.policy, httpapi.Endpoint{
		Base:    base,
		Headers: []domain.HeaderLine{{Name: "Authorization", Value: "Bearer " + key}},
		Masks:   []string{key},
		API:     api,
	}, a.timeout)
}

func site(ds domain.DataSource, override string) (string, error) {
	s := override
	if s == "" {
		s = strings.TrimSpace(ds.Config[keySite])
	}
	if s == "" || strings.ContainsFunc(s, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
		return "", fmt.Errorf("%q is not a Plausible site", s)
	}
	return s, nil
}

type result struct {
	Metrics    []*float64 `json:"metrics"`
	Dimensions []string   `json:"dimensions"`
}

type answer struct {
	Results []result `json:"results"`
	Meta    struct {
		TotalRows *int `json:"total_rows"`
	} `json:"meta"`
	Query struct {
		DateRange []string `json:"date_range"`
	} `json:"query"`
}

func (a *Adapter) query(ctx context.Context, c *httpapi.Conn, op string, body map[string]any) (answer, error) {
	resp, err := c.Do(ctx, httpapi.Request{Method: http.MethodPost, Path: queryPath, JSON: body, Limit: maxBody})
	if err != nil {
		return answer{}, err
	}
	if !resp.OK() {
		return answer{}, c.Refusal(op, resp, c.Detail(resp), authHint)
	}
	var out answer
	if err := json.NewDecoder(bytes.NewReader(resp.Body)).Decode(&out); err != nil {
		return answer{}, fmt.Errorf("%s %s: the answer is not what Plausible sends — is the URL right?", api, op)
	}
	return out, nil
}

func (a *Adapter) Verify(ctx context.Context, ds domain.DataSource, secrets map[string]string) (domain.DataSourceInfo, error) {
	s, err := site(ds, "")
	if err != nil {
		return domain.DataSourceInfo{}, err
	}
	c, err := a.open(ctx, ds, secrets)
	if err != nil {
		return domain.DataSourceInfo{}, err
	}
	defer c.Close()
	out, err := a.query(ctx, c, "read the site", map[string]any{"site_id": s, "metrics": []string{"visitors"}, "date_range": "7d"})
	if err != nil {
		return domain.DataSourceInfo{}, err
	}
	visitors := 0.0
	if len(out.Results) > 0 && len(out.Results[0].Metrics) > 0 && out.Results[0].Metrics[0] != nil {
		visitors = *out.Results[0].Metrics[0]
	}
	return domain.DataSourceInfo{Detail: fmt.Sprintf("Plausible · %s · %.0f visitors in the last 7 days", s, visitors)}, nil
}

// Discover answers "pageview" once the site has recorded one in the last
// 28 days: Plausible lists no events beyond the goals a person set up.
func (a *Adapter) Discover(ctx context.Context, ds domain.DataSource, secrets map[string]string) (domain.Inventory, error) {
	s, err := site(ds, "")
	if err != nil {
		return domain.Inventory{}, err
	}
	c, err := a.open(ctx, ds, secrets)
	if err != nil {
		return domain.Inventory{}, err
	}
	defer c.Close()
	out, err := a.query(ctx, c, "read page views", map[string]any{"site_id": s, "metrics": []string{"pageviews"}, "date_range": "28d"})
	if err != nil {
		return domain.Inventory{}, err
	}
	inv := domain.Inventory{Metrics: []string{}}
	if len(out.Results) > 0 && len(out.Results[0].Metrics) > 0 && out.Results[0].Metrics[0] != nil && *out.Results[0].Metrics[0] > 0 {
		inv.Metrics = append(inv.Metrics, "pageview")
	}
	return inv, nil
}

func parse(raw string) (domain.WebQuery, error) {
	q, err := domain.ParseWebQuery(domain.DataSourceKindPlausible, raw)
	if err != nil {
		return q, fmt.Errorf("%w: %s", port.ErrDataSourceQuery, err.Error())
	}
	return q, nil
}

func filters(q domain.WebQuery) []any {
	if q.Host == "" {
		return []any{}
	}
	return []any{[]any{"is", "event:hostname", []string{q.Host}}}
}

// zoneOf is the offset Plausible echoes the date range in: its time labels
// are in the site's reporting time zone, which the answer names no other
// way.
func zoneOf(out answer) (*time.Location, bool) {
	if len(out.Query.DateRange) == 0 {
		return time.UTC, false
	}
	t, err := time.Parse(time.RFC3339, out.Query.DateRange[0])
	if err != nil {
		return time.UTC, false
	}
	_, offset := t.Zone()
	return time.FixedZone("", offset), true
}

// Series reads one metric per hour (a step under a day; time:hour) or per day
// (time:day) of the site's time zone. A bucket with no row is 0; page_views
// is per second of its bucket.
func (a *Adapter) Series(ctx context.Context, ds domain.DataSource, secrets map[string]string, sq domain.SeriesQuery) (domain.SeriesResult, error) {
	q, err := parse(sq.Query)
	if err != nil {
		return domain.SeriesResult{}, err
	}
	if q.IsTable() {
		return domain.SeriesResult{}, fmt.Errorf("%w: a table query (table %q) is no series", port.ErrDataSourceQuery, q.Table)
	}
	s, err := site(ds, q.Site)
	if err != nil {
		return domain.SeriesResult{}, err
	}
	hourly := sq.Step < 24*time.Hour
	dim, step := "time:day", 24*time.Hour
	if hourly {
		dim, step = "time:hour", time.Hour
	}
	c, err := a.open(ctx, ds, secrets)
	if err != nil {
		return domain.SeriesResult{}, err
	}
	defer c.Close()
	out, err := a.query(ctx, c, "read "+q.Metric, map[string]any{
		"site_id":    s,
		"metrics":    []string{metricNames[q.Metric]},
		"date_range": []string{sq.From.UTC().Format(time.RFC3339), sq.To.UTC().Format(time.RFC3339)},
		"dimensions": []string{dim},
		"filters":    filters(q),
	})
	if err != nil {
		return domain.SeriesResult{}, err
	}
	res := domain.SeriesResult{Series: []domain.Series{}, Warnings: []string{}, Step: step}
	loc, known := zoneOf(out)
	if !known {
		res.Warnings = append(res.Warnings, "Plausible did not say which time zone its hours are in; they are read as UTC")
	}
	values := map[int64]float64{}
	for _, r := range out.Results {
		if len(r.Dimensions) == 0 || len(r.Metrics) == 0 || r.Metrics[0] == nil {
			continue
		}
		t, err := time.ParseInLocation(hourLayout, r.Dimensions[0], loc)
		if err != nil {
			t, err = time.ParseInLocation(time.DateOnly, r.Dimensions[0], loc)
		}
		if err == nil {
			values[t.Unix()] = *r.Metrics[0]
		}
	}
	series := domain.Series{Name: q.Metric, Labels: map[string]string{"site": s}, Points: []domain.SeriesPoint{}}
	if q.Host != "" {
		series.Labels["host"] = q.Host
	}
	from := sq.From.In(loc)
	t := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, loc)
	if hourly {
		t = time.Date(from.Year(), from.Month(), from.Day(), from.Hour(), 0, 0, 0, loc)
	}
	for ; t.Before(sq.To); t = t.Add(step) {
		v := values[t.Unix()]
		if q.Metric == domain.WebMetricPageViews {
			v /= step.Seconds()
		}
		series.Points = append(series.Points, domain.FinitePoint(t.UTC(), v))
	}
	res.Series = append(res.Series, series)
	return res, nil
}

// TableColumns are the columns each table answers, in the order the recipes
// show them.
func TableColumns(table string) []domain.TableColumn {
	var cols []domain.TableColumn
	for _, d := range tableDims[table] {
		cols = append(cols, domain.TableColumn{Key: d.column, Label: d.label})
	}
	return append(cols,
		domain.TableColumn{Key: "views", Label: "Views", Unit: "views"},
		domain.TableColumn{Key: "visitors", Label: "Visitors", Unit: "visitors"})
}

// Table answers a web table over the last Days before At, the most visited
// first: its dimensions, views (pageviews) and visitors.
func (a *Adapter) Table(ctx context.Context, ds domain.DataSource, secrets map[string]string, tq domain.TableQuery) (domain.TableResult, error) {
	q, err := parse(tq.Query)
	if err != nil {
		return domain.TableResult{}, err
	}
	if !q.IsTable() {
		return domain.TableResult{}, fmt.Errorf("%w: a series query (metric %q) is no table; name a table", port.ErrDataSourceQuery, q.Metric)
	}
	s, err := site(ds, q.Site)
	if err != nil {
		return domain.TableResult{}, err
	}
	c, err := a.open(ctx, ds, secrets)
	if err != nil {
		return domain.TableResult{}, err
	}
	defer c.Close()
	dims := tableDims[q.Table]
	var dimensions []string
	for _, d := range dims {
		dimensions = append(dimensions, d.dimension)
	}
	from := tq.At.Add(-time.Duration(q.Days) * 24 * time.Hour)
	out, err := a.query(ctx, c, "read the "+q.Table+" table", map[string]any{
		"site_id":    s,
		"metrics":    []string{"pageviews", "visitors"},
		"date_range": []string{from.UTC().Format(time.RFC3339), tq.At.UTC().Format(time.RFC3339)},
		"dimensions": dimensions,
		"filters":    filters(q),
		"order_by":   [][]string{{"visitors", "desc"}},
		"pagination": map[string]int{"limit": q.Limit + 1},
	})
	if err != nil {
		return domain.TableResult{}, err
	}
	res := domain.TableResult{Columns: TableColumns(q.Table), Rows: make([]map[string]any, 0, len(out.Results))}
	for _, r := range out.Results {
		if len(res.Rows) == q.Limit {
			res.Truncated = true
			break
		}
		row := map[string]any{"views": nil, "visitors": nil}
		for i, d := range dims {
			if i < len(r.Dimensions) {
				row[d.column] = r.Dimensions[i]
			}
		}
		for i, key := range []string{"views", "visitors"} {
			if i < len(r.Metrics) && r.Metrics[i] != nil {
				row[key] = *r.Metrics[i]
			}
		}
		res.Rows = append(res.Rows, row)
	}
	return res, nil
}
