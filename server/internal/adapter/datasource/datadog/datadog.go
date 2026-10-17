// Package datadog is the data source kind for Datadog, read through its
// metrics API with an API key and an application key: any metric query
// (APM trace metrics, system and container metrics) as series, and its
// latest value per tag group as a table. The site is picked from Datadog's
// own list, so the keys only ever go to a Datadog API host. Its query
// language is a Datadog metric query (avg:system.cpu.user{service:web} by
// {host}).
package datadog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/datasource/httpapi"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	keySite   = "site"
	keyAPIKey = "api_key"
	keyAppKey = "app_key"

	DefaultSite = "datadoghq.com"
	api         = "Datadog"
	authHint    = "check the site, and that the application key has the timeseries_query and metrics_read scopes"

	defaultTimeout = 30 * time.Second
	maxBody        = 32 << 20
	maxMetrics     = 10000
	discoverWindow = 24 * time.Hour
	// tableWindow is how far back a table looks for each group's latest
	// point: a few of Datadog's own rollup intervals.
	tableWindow = 10 * time.Minute
)

// Sites are Datadog's sites; each one's API is at api.<site>.
func Sites() []string {
	return []string{"datadoghq.com", "us3.datadoghq.com", "us5.datadoghq.com", "datadoghq.eu", "ap1.datadoghq.com", "ddog-gov.com"}
}

type Adapter struct {
	policy  urlguard.Policy
	baseURL string
	now     func() time.Time
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
func New() *Adapter { return &Adapter{now: time.Now, timeout: defaultTimeout} }

func (a *Adapter) SetURLPolicy(p urlguard.Policy) { a.policy = p }

// SetBaseURL sends every site's requests to u instead (a test server).
func (a *Adapter) SetBaseURL(u string) { a.baseURL = strings.TrimRight(u, "/") }

func (a *Adapter) SetClock(now func() time.Time) { a.now = now }

func (a *Adapter) Kind() domain.DataSourceKind { return domain.DataSourceKindDatadog }

func (a *Adapter) Capabilities() []domain.Capability {
	return []domain.Capability{domain.CapabilityMetrics}
}

func (a *Adapter) Schema() domain.DataSourceSchema {
	return domain.DataSourceSchema{
		Label:       "Datadog",
		Description: "APM request rates, errors and latency, and host or container metrics, from Datadog's metrics API.",
		Fields: []domain.DataSourceField{
			{
				Key: keySite, Label: "Site", Type: domain.FieldSelect, Required: true,
				Options: Sites(), Default: DefaultSite, Placeholder: DefaultSite,
				Help: "The site in your Datadog URL: app.datadoghq.com is datadoghq.com, app.datadoghq.eu is datadoghq.eu, us5.datadoghq.com is us5.datadoghq.com.",
			},
			{
				Key: keyAPIKey, Label: "API key", Type: domain.FieldSecret, Required: true, Secret: true,
				Help: "Organization settings → API keys.",
			},
			{
				Key: keyAppKey, Label: "Application key", Type: domain.FieldSecret, Required: true, Secret: true,
				Help: "Organization settings → Application keys; scoped keys need timeseries_query and metrics_read.",
			},
		},
	}
}

func (a *Adapter) open(ctx context.Context, ds domain.DataSource, secrets map[string]string) (*httpapi.Conn, string, error) {
	site := strings.TrimSpace(ds.Config[keySite])
	if site == "" {
		site = DefaultSite
	}
	if !slices.Contains(Sites(), site) {
		return nil, "", fmt.Errorf("%q is not a Datadog site (%s)", site, strings.Join(Sites(), ", "))
	}
	apiKey, appKey := secrets[keyAPIKey], secrets[keyAppKey]
	if apiKey == "" || appKey == "" {
		return nil, "", errors.New("both the API key and the application key are needed")
	}
	base := "https://api." + site
	if a.baseURL != "" {
		base = a.baseURL
	}
	c, err := httpapi.Open(ctx, a.policy, httpapi.Endpoint{
		Base: base,
		Headers: []domain.HeaderLine{
			{Name: "DD-API-KEY", Value: apiKey},
			{Name: "DD-APPLICATION-KEY", Value: appKey},
		},
		Masks: []string{apiKey, appKey},
		API:   api,
	}, a.timeout)
	if err != nil {
		return nil, "", err
	}
	return c, site, nil
}

func (a *Adapter) get(ctx context.Context, c *httpapi.Conn, op, path string, query url.Values, out any) error {
	resp, err := c.Do(ctx, httpapi.Request{Method: http.MethodGet, Path: path, Query: query, Limit: maxBody})
	if err != nil {
		return err
	}
	if !resp.OK() {
		return c.Refusal(op, resp, detail(resp.Body), authHint)
	}
	if err := json.NewDecoder(bytes.NewReader(resp.Body)).Decode(out); err != nil {
		return fmt.Errorf("%s %s: the answer is not what Datadog sends", api, op)
	}
	return nil
}

// detail is Datadog's {"errors": ["…"]}.
func detail(raw []byte) string {
	var body struct {
		Errors []string `json:"errors"`
	}
	if json.Unmarshal(raw, &body) != nil {
		return ""
	}
	return strings.Join(body.Errors, "; ")
}

func (a *Adapter) Verify(ctx context.Context, ds domain.DataSource, secrets map[string]string) (domain.DataSourceInfo, error) {
	c, site, err := a.open(ctx, ds, secrets)
	if err != nil {
		return domain.DataSourceInfo{}, err
	}
	defer c.Close()
	var valid struct {
		Valid bool `json:"valid"`
	}
	if err := a.get(ctx, c, "check the API key", "/api/v1/validate", nil, &valid); err != nil {
		return domain.DataSourceInfo{}, err
	}
	if !valid.Valid {
		return domain.DataSourceInfo{}, fmt.Errorf("%s check the API key: Datadog says it is not valid on %s", api, site)
	}
	metrics, err := a.activeMetrics(ctx, c, time.Hour)
	if err != nil {
		return domain.DataSourceInfo{}, err
	}
	return domain.DataSourceInfo{Detail: fmt.Sprintf("Datadog · %s · %d metrics active in the last hour", site, len(metrics))}, nil
}

func (a *Adapter) activeMetrics(ctx context.Context, c *httpapi.Conn, within time.Duration) ([]string, error) {
	var out struct {
		Metrics []string `json:"metrics"`
	}
	from := strconv.FormatInt(a.now().Add(-within).Unix(), 10)
	if err := a.get(ctx, c, "list active metrics", "/api/v1/metrics", url.Values{"from": {from}}, &out); err != nil {
		return nil, err
	}
	return out.Metrics, nil
}

// Discover lists the metrics active in the last day, at most 10 000.
func (a *Adapter) Discover(ctx context.Context, ds domain.DataSource, secrets map[string]string) (domain.Inventory, error) {
	c, _, err := a.open(ctx, ds, secrets)
	if err != nil {
		return domain.Inventory{}, err
	}
	defer c.Close()
	metrics, err := a.activeMetrics(ctx, c, discoverWindow)
	if err != nil {
		return domain.Inventory{}, err
	}
	slices.Sort(metrics)
	inv := domain.Inventory{Metrics: metrics}
	if len(inv.Metrics) > maxMetrics {
		inv.Metrics, inv.Truncated = inv.Metrics[:maxMetrics], true
	}
	if inv.Metrics == nil {
		inv.Metrics = []string{}
	}
	return inv, nil
}

type queryAnswer struct {
	Status string `json:"status"`
	Error  string `json:"error"`
	Series []struct {
		Metric      string           `json:"metric"`
		DisplayName string           `json:"display_name"`
		Expression  string           `json:"expression"`
		Scope       string           `json:"scope"`
		TagSet      []string         `json:"tag_set"`
		Interval    int64            `json:"interval"`
		Pointlist   [][2]*float64    `json:"pointlist"`
		Unit        []map[string]any `json:"unit"`
	} `json:"series"`
}

func (a *Adapter) query(ctx context.Context, c *httpapi.Conn, query string, from, to time.Time) (queryAnswer, error) {
	var out queryAnswer
	params := url.Values{
		"from":  {strconv.FormatInt(from.Unix(), 10)},
		"to":    {strconv.FormatInt(to.Unix(), 10)},
		"query": {query},
	}
	if err := a.get(ctx, c, "query metrics", "/api/v1/query", params, &out); err != nil {
		return queryAnswer{}, err
	}
	if out.Status == "error" || out.Error != "" {
		return queryAnswer{}, fmt.Errorf("%w: %s query metrics: %s", port.ErrDataSourceQuery, api, c.Scrub(out.Error))
	}
	return out, nil
}

// labels reads a series' tag set ("service:web" → service=web; a bare tag
// is its own key with no value), or its scope where the set is empty.
func labels(tags []string, scope string) map[string]string {
	out := map[string]string{}
	if len(tags) == 0 && scope != "" && scope != "*" {
		tags = strings.Split(scope, ",")
	}
	for _, t := range tags {
		k, v, _ := strings.Cut(strings.TrimSpace(t), ":")
		if k != "" {
			out[k] = v
		}
	}
	return out
}

// Series runs the query over the window; Datadog picks the rollup interval
// from the window's length (a .rollup() in the query sets its own), so Step
// is the interval it answered at.
func (a *Adapter) Series(ctx context.Context, ds domain.DataSource, secrets map[string]string, sq domain.SeriesQuery) (domain.SeriesResult, error) {
	c, _, err := a.open(ctx, ds, secrets)
	if err != nil {
		return domain.SeriesResult{}, err
	}
	defer c.Close()
	out, err := a.query(ctx, c, sq.Query, sq.From, sq.To)
	if err != nil {
		return domain.SeriesResult{}, err
	}
	res := domain.SeriesResult{Series: []domain.Series{}, Warnings: []string{}}
	for _, s := range out.Series {
		name := s.DisplayName
		if name == "" {
			name = s.Metric
		}
		series := domain.Series{Name: name, Labels: labels(s.TagSet, s.Scope), Points: make([]domain.SeriesPoint, 0, len(s.Pointlist))}
		for _, p := range s.Pointlist {
			if p[0] == nil {
				continue
			}
			t := time.UnixMilli(int64(*p[0])).UTC()
			if p[1] == nil {
				series.Points = append(series.Points, domain.SeriesPoint{Time: t})
				continue
			}
			series.Points = append(series.Points, domain.FinitePoint(t, *p[1]))
		}
		res.Series = append(res.Series, series)
		if iv := time.Duration(s.Interval) * time.Millisecond; iv > res.Step {
			res.Step = iv
		}
	}
	return res, nil
}

// Table runs the query over the 10 minutes before At and answers each
// series' latest point as a row of its tags plus value, so a query grouped
// "by {resource_name}" is one row per resource.
func (a *Adapter) Table(ctx context.Context, ds domain.DataSource, secrets map[string]string, tq domain.TableQuery) (domain.TableResult, error) {
	c, _, err := a.open(ctx, ds, secrets)
	if err != nil {
		return domain.TableResult{}, err
	}
	defer c.Close()
	out, err := a.query(ctx, c, tq.Query, tq.At.Add(-tableWindow), tq.At)
	if err != nil {
		return domain.TableResult{}, err
	}
	res := domain.TableResult{Columns: []domain.TableColumn{}, Rows: make([]map[string]any, 0, len(out.Series))}
	keys := map[string]bool{}
	for _, s := range out.Series {
		row := map[string]any{domain.TableValueKey: nil}
		for k, v := range labels(s.TagSet, s.Scope) {
			row[k] = v
			keys[k] = true
		}
		for i := len(s.Pointlist) - 1; i >= 0; i-- {
			if v := s.Pointlist[i][1]; v != nil {
				row[domain.TableValueKey] = *v
				break
			}
		}
		res.Rows = append(res.Rows, row)
	}
	for _, k := range slices.Sorted(maps.Keys(keys)) {
		res.Columns = append(res.Columns, domain.TableColumn{Key: k, Label: k})
	}
	res.Columns = append(res.Columns, domain.TableColumn{Key: domain.TableValueKey, Label: "Value"})
	return res, nil
}
