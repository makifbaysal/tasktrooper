// Package newrelic is the data source kind for New Relic, read through
// NerdGraph (GraphQL) with a user API key: NRQL queries of APM, browser and
// mobile events as series (the kind appends SINCE, UNTIL and TIMESERIES) and
// as tables (a FACET query, one row per facet). The region picks the API
// host, so the key only ever goes to New Relic. Its query language is NRQL.
package newrelic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"net/http"
	"regexp"
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
	keyRegion  = "region"
	keyAccount = "account_id"
	keyAPIKey  = "api_key"

	RegionUS = "us"
	RegionEU = "eu"

	api      = "New Relic"
	authHint = "the key must be a user key (NRAK-…) of a user who can query this account"

	defaultTimeout = 45 * time.Second
	maxBody        = 32 << 20
	// maxBuckets is NRQL's own ceiling for TIMESERIES.
	maxBuckets  = 366
	tableWindow = time.Hour
	maxEvents   = 2000
)

var (
	hosts = map[string]string{RegionUS: "https://api.newrelic.com", RegionEU: "https://api.eu.newrelic.com"}

	accountPattern = regexp.MustCompile(`^[0-9]{1,12}$`)
	// windowClauses are the clauses the kind writes itself.
	windowClauses = []string{"SINCE", "UNTIL", "TIMESERIES", "COMPARE"}
)

const (
	accountQuery = `query($id: Int!) { actor { account(id: $id) { id name } } }`
	nrqlQuery    = `query($id: Int!, $nrql: Nrql!) { actor { account(id: $id) { nrql(query: $nrql, timeout: 30) { results } } } }`
)

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

// SetBaseURL sends both regions' requests to u instead (a test server).
func (a *Adapter) SetBaseURL(u string) { a.baseURL = strings.TrimRight(u, "/") }

func (a *Adapter) SetClock(now func() time.Time) { a.now = now }

func (a *Adapter) Kind() domain.DataSourceKind { return domain.DataSourceKindNewRelic }

func (a *Adapter) Capabilities() []domain.Capability {
	return []domain.Capability{domain.CapabilityMetrics, domain.CapabilityRUM, domain.CapabilityCrashes}
}

func (a *Adapter) Schema() domain.DataSourceSchema {
	return domain.DataSourceSchema{
		Label:       "New Relic",
		Description: "APM throughput, errors and latency, browser page views and web vitals, and mobile crash rates, from New Relic with NRQL.",
		Fields: []domain.DataSourceField{
			{
				Key: keyRegion, Label: "Region", Type: domain.FieldSelect, Required: true,
				Options: []string{RegionUS, RegionEU}, Default: RegionUS, Placeholder: RegionUS,
				Help: "Where the account's data lives: eu for one.eu.newrelic.com, else us.",
			},
			{
				Key: keyAccount, Label: "Account ID", Type: domain.FieldText, Required: true,
				Placeholder: "1234567",
				Help:        "The account's number, shown under the account name in the account picker.",
			},
			{
				Key: keyAPIKey, Label: "User key", Type: domain.FieldSecret, Required: true, Secret: true,
				Help: "A user key (NRAK-…) from API keys, of a user who can query the account.",
			},
		},
	}
}

type conn struct {
	*httpapi.Conn
	account int64
}

func (a *Adapter) open(ctx context.Context, ds domain.DataSource, secrets map[string]string) (*conn, error) {
	region := strings.TrimSpace(ds.Config[keyRegion])
	if region == "" {
		region = RegionUS
	}
	base, ok := hosts[region]
	if !ok {
		return nil, fmt.Errorf("%q is not a New Relic region (us or eu)", region)
	}
	id := strings.TrimSpace(ds.Config[keyAccount])
	if !accountPattern.MatchString(id) {
		return nil, fmt.Errorf("%q is not a New Relic account id", id)
	}
	account, err := strconv.ParseInt(id, 10, 64)
	if err != nil || account > math.MaxInt32 {
		return nil, fmt.Errorf("%q is not a New Relic account id", id)
	}
	key := secrets[keyAPIKey]
	if key == "" {
		return nil, errors.New("no user key is set")
	}
	if a.baseURL != "" {
		base = a.baseURL
	}
	c, err := httpapi.Open(ctx, a.policy, httpapi.Endpoint{
		Base:    base,
		Headers: []domain.HeaderLine{{Name: "API-Key", Value: key}},
		Masks:   []string{key},
		API:     api,
	}, a.timeout)
	if err != nil {
		return nil, err
	}
	return &conn{Conn: c, account: account}, nil
}

type graphQLError struct {
	Message string `json:"message"`
}

// graphQL posts one query with its variables; the query text is fixed and
// every value travels as a variable, so NRQL never lands in GraphQL syntax.
func (a *Adapter) graphQL(ctx context.Context, c *conn, op, query string, vars map[string]any, data any) error {
	resp, err := c.Do(ctx, httpapi.Request{
		Method: http.MethodPost, Path: "/graphql",
		JSON:  map[string]any{"query": query, "variables": vars},
		Limit: maxBody,
	})
	if err != nil {
		return err
	}
	var out struct {
		Data   json.RawMessage `json:"data"`
		Errors []graphQLError  `json:"errors"`
	}
	decodeErr := json.NewDecoder(bytes.NewReader(resp.Body)).Decode(&out)
	if !resp.OK() {
		d := ""
		if decodeErr == nil && len(out.Errors) > 0 {
			d = out.Errors[0].Message
		}
		return c.Refusal(op, resp, d, authHint)
	}
	if decodeErr != nil {
		return fmt.Errorf("%s %s: the answer is not what NerdGraph sends", api, op)
	}
	if len(out.Errors) > 0 {
		return c.graphQLRefusal(op, out.Errors)
	}
	dec := json.NewDecoder(bytes.NewReader(out.Data))
	dec.UseNumber()
	if err := dec.Decode(data); err != nil {
		return fmt.Errorf("%s %s: the answer is not what NerdGraph sends", api, op)
	}
	return nil
}

// graphQLRefusal: NerdGraph answers 200 with errors for a query it refuses
// and for a key it does not accept, so the message tells them apart.
func (c *conn) graphQLRefusal(op string, errs []graphQLError) error {
	msg := c.Scrub(errs[0].Message)
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "api key") || strings.Contains(lower, "unauthorized") || strings.Contains(lower, "forbidden") || strings.Contains(lower, "not authorized"):
		return fmt.Errorf("%s %s: authentication failed: %s; %s", api, op, msg, authHint)
	case strings.Contains(lower, "syntax") || strings.Contains(lower, "invalid") || strings.Contains(lower, "nrql"):
		return fmt.Errorf("%w: %s %s: %s", port.ErrDataSourceQuery, api, op, msg)
	}
	return fmt.Errorf("%s %s: %s", api, op, msg)
}

func (a *Adapter) nrql(ctx context.Context, c *conn, op, query string) ([]map[string]any, error) {
	var data struct {
		Actor struct {
			Account *struct {
				NRQL *struct {
					Results []map[string]any `json:"results"`
				} `json:"nrql"`
			} `json:"account"`
		} `json:"actor"`
	}
	if err := a.graphQL(ctx, c, op, nrqlQuery, map[string]any{"id": c.account, "nrql": query}, &data); err != nil {
		return nil, err
	}
	if data.Actor.Account == nil || data.Actor.Account.NRQL == nil {
		return nil, fmt.Errorf("%s %s: no such account, or the key cannot read it", api, op)
	}
	return data.Actor.Account.NRQL.Results, nil
}

func (a *Adapter) Verify(ctx context.Context, ds domain.DataSource, secrets map[string]string) (domain.DataSourceInfo, error) {
	c, err := a.open(ctx, ds, secrets)
	if err != nil {
		return domain.DataSourceInfo{}, err
	}
	defer c.Close()
	var data struct {
		Actor struct {
			Account *struct {
				Name string `json:"name"`
			} `json:"account"`
		} `json:"actor"`
	}
	if err := a.graphQL(ctx, c, "read the account", accountQuery, map[string]any{"id": c.account}, &data); err != nil {
		return domain.DataSourceInfo{}, err
	}
	if data.Actor.Account == nil {
		return domain.DataSourceInfo{}, fmt.Errorf("%s read the account: no account %d, or the key cannot read it", api, c.account)
	}
	return domain.DataSourceInfo{Detail: "New Relic · " + data.Actor.Account.Name}, nil
}

// Discover lists the event types the account holds data for in the last
// day (Transaction, PageView, MobileSession, …).
func (a *Adapter) Discover(ctx context.Context, ds domain.DataSource, secrets map[string]string) (domain.Inventory, error) {
	c, err := a.open(ctx, ds, secrets)
	if err != nil {
		return domain.Inventory{}, err
	}
	defer c.Close()
	rows, err := a.nrql(ctx, c, "list event types", "SHOW EVENT TYPES SINCE 1 day ago")
	if err != nil {
		return domain.Inventory{}, err
	}
	inv := domain.Inventory{Metrics: []string{}}
	for _, r := range rows {
		types, _ := r["eventTypes"].([]any)
		for _, t := range types {
			if name, ok := t.(string); ok && name != "" {
				if len(inv.Metrics) == maxEvents {
					inv.Truncated = true
					return inv, nil
				}
				inv.Metrics = append(inv.Metrics, name)
			}
		}
	}
	return inv, nil
}

// clauses are the keywords of q outside its strings and backquoted names,
// upper-cased; a comment there (--, // or /* */) is the word "--".
func clauses(q string) map[string]bool {
	out := map[string]bool{}
	for i := 0; i < len(q); i++ {
		c := q[i]
		switch {
		case (c == '-' || c == '/') && i+1 < len(q) && q[i+1] == c, c == '/' && i+1 < len(q) && q[i+1] == '*':
			out[commentWord] = true
			return out
		case c == '\'' || c == '"' || c == '`':
			j := i + 1
			for ; j < len(q) && q[j] != c; j++ {
				if q[j] == '\\' && c != '`' {
					j++
				}
			}
			i = j
		case isWordByte(c):
			j := i
			for j < len(q) && isWordByte(q[j]) {
				j++
			}
			out[strings.ToUpper(q[i:j])] = true
			i = j - 1
		}
	}
	return out
}

func isWordByte(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// commentWord stands for a comment in clauses: the kind appends its clauses
// after the query, which a comment there would swallow (-- and // run to the
// end of the line), and a keyword inside one is no clause of the query.
const commentWord = "--"

func refuseComment(words map[string]bool) error {
	if words[commentWord] {
		return fmt.Errorf("%w: leave out comments (--, // or /* */): the kind appends SINCE, UNTIL and TIMESERIES after the query", port.ErrDataSourceQuery)
	}
	return nil
}

func refuseWindow(q string) error {
	words := clauses(q)
	if err := refuseComment(words); err != nil {
		return err
	}
	for _, w := range windowClauses {
		if words[w] {
			return fmt.Errorf("%w: leave out %s: the kind adds SINCE, UNTIL and TIMESERIES from the window", port.ErrDataSourceQuery, w)
		}
	}
	return nil
}

// bucketMinutes is the step in whole minutes, raised until the window fits
// NRQL's 366 buckets.
func bucketMinutes(sq domain.SeriesQuery) int64 {
	m := max(int64(math.Ceil(sq.Step.Minutes())), 1)
	if span := sq.To.Sub(sq.From).Minutes(); span/float64(m) > maxBuckets {
		m = int64(math.Ceil(span / maxBuckets))
	}
	return m
}

// Series runs the NRQL with SINCE and UNTIL the window (epoch ms) and
// TIMESERIES the step in whole minutes (coarser where the window would need
// more than 366 buckets). Every numeric value a bucket answers is a series,
// named by its key (an alias, rate.count, percentile.duration.95), labelled
// by the bucket's facet.
func (a *Adapter) Series(ctx context.Context, ds domain.DataSource, secrets map[string]string, sq domain.SeriesQuery) (domain.SeriesResult, error) {
	if err := refuseWindow(sq.Query); err != nil {
		return domain.SeriesResult{}, err
	}
	minutes := bucketMinutes(sq)
	query := fmt.Sprintf("%s SINCE %d UNTIL %d TIMESERIES %d minutes", strings.TrimSpace(sq.Query), sq.From.UnixMilli(), sq.To.UnixMilli(), minutes)
	c, err := a.open(ctx, ds, secrets)
	if err != nil {
		return domain.SeriesResult{}, err
	}
	defer c.Close()
	rows, err := a.nrql(ctx, c, "run the query", query)
	if err != nil {
		return domain.SeriesResult{}, err
	}
	byKey := map[string]*domain.Series{}
	var order []string
	for _, r := range rows {
		begin, ok := num(r["beginTimeSeconds"])
		if !ok {
			continue
		}
		t := time.Unix(int64(begin), 0).UTC()
		facet := facetOf(r["facet"])
		for name, v := range values(r) {
			key := facet + "\x00" + name
			s, ok := byKey[key]
			if !ok {
				s = &domain.Series{Name: name, Labels: map[string]string{}, Points: []domain.SeriesPoint{}}
				if facet != "" {
					s.Labels["facet"] = facet
				}
				byKey[key] = s
				order = append(order, key)
			}
			if v == nil {
				s.Points = append(s.Points, domain.SeriesPoint{Time: t})
			} else {
				s.Points = append(s.Points, domain.FinitePoint(t, *v))
			}
		}
	}
	slices.Sort(order)
	res := domain.SeriesResult{Series: make([]domain.Series, 0, len(order)), Warnings: []string{}, Step: time.Duration(minutes) * time.Minute}
	for _, k := range order {
		s := byKey[k]
		slices.SortFunc(s.Points, func(a, b domain.SeriesPoint) int { return a.Time.Compare(b.Time) })
		res.Series = append(res.Series, *s)
	}
	return res, nil
}

// values are a result row's numbers by name: a number under its key, a map
// (percentile(x, 50, 95)) under key.subkey, null as nil. Strings (a facet
// attribute echoed back) and the bucket bounds are not values.
func values(r map[string]any) map[string]*float64 {
	out := map[string]*float64{}
	for k, v := range r {
		switch k {
		case "beginTimeSeconds", "endTimeSeconds", "facet":
			continue
		}
		switch t := v.(type) {
		case nil:
			out[k] = nil
		case json.Number:
			if f, err := t.Float64(); err == nil {
				out[k] = &f
			}
		case map[string]any:
			for sub, sv := range t {
				if f, ok := num(sv); ok {
					out[k+"."+sub] = &f
				} else if sv == nil {
					out[k+"."+sub] = nil
				}
			}
		}
	}
	return out
}

func facetOf(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []any:
		parts := make([]string, 0, len(t))
		for _, p := range t {
			parts = append(parts, fmt.Sprint(p))
		}
		return strings.Join(parts, ", ")
	case nil:
		return ""
	}
	return fmt.Sprint(v)
}

func num(v any) (float64, bool) {
	switch t := v.(type) {
	case json.Number:
		f, err := t.Float64()
		return f, err == nil
	case float64:
		return t, true
	}
	return 0, false
}

// Table runs the NRQL over the hour before At (or its own SINCE) and
// answers each result row: a FACET query's facet under "facet" (a
// multi-attribute facet joined with ", "), every other value by its key.
func (a *Adapter) Table(ctx context.Context, ds domain.DataSource, secrets map[string]string, tq domain.TableQuery) (domain.TableResult, error) {
	words := clauses(tq.Query)
	if err := refuseComment(words); err != nil {
		return domain.TableResult{}, err
	}
	if words["TIMESERIES"] || words["COMPARE"] {
		return domain.TableResult{}, fmt.Errorf("%w: a table query takes no TIMESERIES or COMPARE WITH", port.ErrDataSourceQuery)
	}
	query := strings.TrimSpace(tq.Query)
	if !words["SINCE"] {
		query = fmt.Sprintf("%s SINCE %d UNTIL %d", query, tq.At.Add(-tableWindow).UnixMilli(), tq.At.UnixMilli())
	}
	c, err := a.open(ctx, ds, secrets)
	if err != nil {
		return domain.TableResult{}, err
	}
	defer c.Close()
	rows, err := a.nrql(ctx, c, "run the query", query)
	if err != nil {
		return domain.TableResult{}, err
	}
	res := domain.TableResult{Columns: []domain.TableColumn{}, Rows: make([]map[string]any, 0, len(rows))}
	keys := map[string]bool{}
	for _, r := range rows {
		row := map[string]any{}
		if f, ok := r["facet"]; ok {
			row["facet"] = facetOf(f)
			keys["facet"] = true
		}
		for k, v := range r {
			if s, ok := v.(string); ok && k != "facet" {
				row[k] = s
				keys[k] = true
			}
		}
		for k, v := range values(r) {
			if v == nil {
				row[k] = nil
			} else {
				row[k] = *v
			}
			keys[k] = true
		}
		res.Rows = append(res.Rows, row)
	}
	for _, k := range slices.Sorted(maps.Keys(keys)) {
		res.Columns = append(res.Columns, domain.TableColumn{Key: k, Label: k})
	}
	return res, nil
}
