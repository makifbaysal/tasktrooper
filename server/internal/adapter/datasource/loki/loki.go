// Package loki is the data source kind for Grafana Loki, self-hosted or
// Grafana Cloud Logs: log lines through query_range, LogQL metric queries as
// series (an error rate counted from lines), and its label names and values
// for the detector.
package loki

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/datasource/httpapi"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	apiName = "Loki"

	verifyTimeout = 15 * time.Second
	queryTimeout  = 30 * time.Second

	maxSmallBody = 1 << 20
	maxLabelBody = 8 << 20
	maxQueryBody = 32 << 20

	// maxEntries is Loki's own default max_entries_limit_per_query.
	maxEntries   = 5000
	defaultLimit = 100
	maxLabels    = 5000
	maxFields    = 64
	// labelWindow keeps label listings to what was written lately, which is
	// also what Loki answers cheaply from its ingesters.
	labelWindow = time.Hour
)

var labelPattern = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// levelLabels are where a line's level is read from, in order: Loki 3's own
// detection, then what shippers and OTLP set. The severity filter excludes
// the lower levels on the first three.
var (
	levelLabels   = []string{"detected_level", "level", "severity", "lvl", "log_level", "loglevel", "severity_text"}
	filterLabels  = levelLabels[:3]
	sourceLabels  = []string{"service_name", "app", "application", "job", "container", "component"}
	traceIDLabels = []string{"trace_id", "traceID", "traceId", "traceid"}
)

type Adapter struct {
	policy urlguard.Policy
	now    func() time.Time
}

var (
	_ port.DataSourceKindAdapter = (*Adapter)(nil)
	_ port.LogsQuerier           = (*Adapter)(nil)
	_ port.MetricsQuerier        = (*Adapter)(nil)
	_ port.Discoverer            = (*Adapter)(nil)
	_ port.LabelValuesLister     = (*Adapter)(nil)
)

// New answers an adapter that refuses every destination (the zero
// urlguard.Policy allows no scheme) until SetURLPolicy.
func New() *Adapter { return &Adapter{now: time.Now} }

func (a *Adapter) SetURLPolicy(p urlguard.Policy) { a.policy = p }

func (a *Adapter) Kind() domain.DataSourceKind { return domain.DataSourceKindLoki }

func (a *Adapter) Capabilities() []domain.Capability {
	return []domain.Capability{domain.CapabilityLogs, domain.CapabilityMetrics}
}

func (a *Adapter) Schema() domain.DataSourceSchema {
	fields := []domain.DataSourceField{{
		Key: httpapi.KeyURL, Label: "URL", Type: domain.FieldURL, Required: true,
		Placeholder: "http://localhost:3100",
		Help:        "The Loki base URL, without /loki/api/v1. For Grafana Cloud, the Logs URL (https://logs-prod-<n>.grafana.net).",
	}}
	fields = append(fields, httpapi.AuthFields(
		[]string{httpapi.AuthNone, httpapi.AuthBearer, httpapi.AuthBasic},
		"For Grafana Cloud, the Logs user ID.",
		"For Grafana Cloud, an access policy token with logs:read.",
		"X-Scope-OrgID: tenant-1",
	)...)
	return domain.DataSourceSchema{
		Label:       "Loki",
		Description: "Grafana Loki or Grafana Cloud Logs: a component's log lines, and error rates counted from them with LogQL.",
		Fields:      fields,
	}
}

var errNotLoki = errors.New("the URL answered, but not as the Loki API — is it the Loki base URL?")

func (a *Adapter) open(ctx context.Context, ds domain.DataSource, secrets map[string]string, timeout time.Duration) (*httpapi.Conn, error) {
	ep, err := httpapi.NewEndpoint(apiName, ds, secrets)
	if err != nil {
		return nil, err
	}
	return httpapi.Open(ctx, a.policy, ep, timeout)
}

// Verify reads build info, falling back to /ready on anything but success
// or an auth refusal: a gateway in front of Loki may not route it.
func (a *Adapter) Verify(ctx context.Context, ds domain.DataSource, secrets map[string]string) (domain.DataSourceInfo, error) {
	c, err := a.open(ctx, ds, secrets, verifyTimeout)
	if err != nil {
		return domain.DataSourceInfo{}, err
	}
	defer c.Close()
	r, err := c.Do(ctx, httpapi.Request{Path: "/loki/api/v1/status/buildinfo", Limit: maxSmallBody})
	if err != nil {
		return domain.DataSourceInfo{}, err
	}
	switch {
	case r.OK():
		var bi struct {
			Version string `json:"version"`
		}
		if json.Unmarshal(r.Body, &bi) != nil || bi.Version == "" {
			return domain.DataSourceInfo{}, errNotLoki
		}
		return domain.DataSourceInfo{Version: bi.Version, Detail: "Loki"}, nil
	case r.Status == http.StatusUnauthorized || r.Status == http.StatusForbidden:
		return domain.DataSourceInfo{}, c.StatusError("build info", r)
	}
	r, err = c.Do(ctx, httpapi.Request{Path: "/ready", Limit: maxSmallBody})
	if err != nil {
		return domain.DataSourceInfo{}, err
	}
	if !r.OK() {
		return domain.DataSourceInfo{}, c.StatusError("readiness", r)
	}
	if strings.TrimSpace(string(r.Body)) != "ready" {
		return domain.DataSourceInfo{}, errNotLoki
	}
	return domain.DataSourceInfo{Detail: "Loki (no build info)"}, nil
}

type envelope struct {
	Status string          `json:"status"`
	Data   json.RawMessage `json:"data"`
}

type queryData struct {
	ResultType string          `json:"resultType"`
	Result     json.RawMessage `json:"result"`
}

func (a *Adapter) queryRange(ctx context.Context, c *httpapi.Conn, op string, params url.Values) (queryData, error) {
	r, err := c.Do(ctx, httpapi.Request{Path: "/loki/api/v1/query_range", Query: params, Limit: maxQueryBody})
	if err != nil {
		return queryData{}, err
	}
	if !r.OK() {
		return queryData{}, refusal(c, op, r)
	}
	var env envelope
	var data queryData
	if json.Unmarshal(r.Body, &env) != nil || env.Status != "success" || json.Unmarshal(env.Data, &data) != nil {
		return queryData{}, errNotLoki
	}
	return data, nil
}

// refusal: Loki answers 400 for a query it cannot parse or will not run,
// with the reason as plain text.
func refusal(c *httpapi.Conn, op string, r httpapi.Response) error {
	if r.Status == http.StatusBadRequest {
		if d := c.Detail(r); d != "" {
			return fmt.Errorf("%w: %s", port.ErrDataSourceQuery, d)
		}
		return port.ErrDataSourceQuery
	}
	return c.StatusError(op, r)
}

// Logs reads the newest q.Limit lines of the window (Loki's limit counts
// across streams). The console's text filter becomes a line filter and its
// severity one label filter per level label, dropping lines whose level is
// known to be lower; a line with no level label passes that and is judged
// here by its text, so a page can come back shorter than a full one.
func (a *Adapter) Logs(ctx context.Context, ds domain.DataSource, secrets map[string]string, q domain.LogQuery) (domain.RuntimeLogPage, error) {
	query, err := logQuery(q)
	if err != nil {
		return domain.RuntimeLogPage{}, err
	}
	limit := q.Limit
	if limit <= 0 {
		limit = defaultLimit
	}
	limit = min(limit, maxEntries)
	c, err := a.open(ctx, ds, secrets, queryTimeout)
	if err != nil {
		return domain.RuntimeLogPage{}, err
	}
	defer c.Close()
	// Loki's end is exclusive; the console's until is not.
	data, err := a.queryRange(ctx, c, "logs", url.Values{
		"query":     {query},
		"start":     {nanos(q.Since)},
		"end":       {nanos(q.Until.Add(time.Nanosecond))},
		"limit":     {strconv.Itoa(limit)},
		"direction": {"backward"},
	})
	if err != nil {
		return domain.RuntimeLogPage{}, err
	}
	if data.ResultType != "streams" {
		return domain.RuntimeLogPage{}, fmt.Errorf("%w: a logs read needs a log query, not a metric one", port.ErrDataSourceQuery)
	}
	var streams []logStream
	if err := json.Unmarshal(data.Result, &streams); err != nil {
		return domain.RuntimeLogPage{}, errNotLoki
	}
	entries := streamEntries(streams)
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].Timestamp.After(entries[j].Timestamp) })
	page := domain.RuntimeLogPage{}
	if len(entries) >= limit {
		entries, page.Truncated = entries[:limit], true
	}
	if q.MinSeverity != "" {
		kept := entries[:0]
		for _, e := range entries {
			if e.Severity.Rank() >= q.MinSeverity.Rank() {
				kept = append(kept, e)
			}
		}
		entries = kept
	}
	page.Entries = entries
	return page, nil
}

// logQuery appends the console's filters to a rendered log query, on a line
// of their own: a # comment closing the query runs to the end of its line
// and would swallow them. Text is quoted as a Go string literal, which is how
// LogQL reads its strings.
func logQuery(q domain.LogQuery) (string, error) {
	base := strings.TrimSpace(q.Query)
	if !strings.HasPrefix(base, "{") {
		return "", fmt.Errorf("%w: a logs read needs a LogQL log query ({…} | …), not a metric query", port.ErrDataSourceQuery)
	}
	var b strings.Builder
	b.WriteString(base)
	words := domain.SeverityWordsBelow(q.MinSeverity)
	if q.Text != "" || len(words) > 0 {
		b.WriteString("\n")
	}
	if q.Text != "" {
		b.WriteString("|= ")
		b.WriteString(strconv.Quote(q.Text))
	}
	if len(words) > 0 {
		re := strconv.Quote("(?i)^(" + strings.Join(words, "|") + ")$")
		for i, label := range filterLabels {
			if i > 0 || q.Text != "" {
				b.WriteString(" ")
			}
			b.WriteString("| ")
			b.WriteString(label)
			b.WriteString("!~")
			b.WriteString(re)
		}
	}
	return b.String(), nil
}

type logStream struct {
	Stream map[string]string   `json:"stream"`
	Values [][]json.RawMessage `json:"values"`
}

func streamEntries(streams []logStream) []domain.RuntimeLogEntry {
	var out []domain.RuntimeLogEntry
	for _, s := range streams {
		for _, v := range s.Values {
			if len(v) < 2 {
				continue
			}
			var rawTS, line string
			if json.Unmarshal(v[0], &rawTS) != nil || json.Unmarshal(v[1], &line) != nil {
				continue
			}
			ns, err := strconv.ParseInt(rawTS, 10, 64)
			if err != nil {
				continue
			}
			labels := make(map[string]string, len(s.Stream))
			for k, val := range s.Stream {
				labels[k] = val
			}
			if len(v) > 2 {
				mergeMetadata(labels, v[2])
			}
			out = append(out, entry(time.Unix(0, ns).UTC(), line, labels))
		}
	}
	return out
}

// mergeMetadata reads a value's third element: structured metadata as a flat
// object, or, with Loki's categorize-labels encoding, {"structuredMetadata",
// "parsed"}.
func mergeMetadata(labels map[string]string, raw json.RawMessage) {
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil {
		return
	}
	for k, v := range obj {
		var s string
		if json.Unmarshal(v, &s) == nil {
			labels[k] = s
			continue
		}
		if k == "structuredMetadata" || k == "parsed" {
			var inner map[string]string
			if json.Unmarshal(v, &inner) == nil {
				for ik, iv := range inner {
					labels[ik] = iv
				}
			}
		}
	}
}

func entry(ts time.Time, line string, labels map[string]string) domain.RuntimeLogEntry {
	e := domain.RuntimeLogEntry{Timestamp: ts, Message: line, Severity: severity(labels, line)}
	e.Source = first(labels, sourceLabels)
	e.TraceID = first(labels, traceIDLabels)
	if len(labels) > 0 {
		e.Fields = make(map[string]string, min(len(labels), maxFields))
		keys := make([]string, 0, len(labels))
		for k := range labels {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys[:min(len(keys), maxFields)] {
			e.Fields[k] = labels[k]
		}
	}
	return e
}

// severity: a level label first, then the line's own level word; info when
// neither says.
func severity(labels map[string]string, line string) domain.LogSeverity {
	for _, k := range levelLabels {
		if s, ok := domain.SeverityFromText(labels[k]); ok {
			return s
		}
	}
	if s, ok := domain.SeverityFromLine(line); ok {
		return s
	}
	return domain.LogInfo
}

func first(labels map[string]string, keys []string) string {
	for _, k := range keys {
		if v := labels[k]; v != "" {
			return v
		}
	}
	return ""
}

func nanos(t time.Time) string { return strconv.FormatInt(t.UnixNano(), 10) }

type matrixSeries struct {
	Metric map[string]string    `json:"metric"`
	Values [][2]json.RawMessage `json:"values"`
}

// Series runs a LogQL metric query (sum(rate({…} |= "error" [$__rate])))
// over the range; a log query answers ErrDataSourceQuery.
func (a *Adapter) Series(ctx context.Context, ds domain.DataSource, secrets map[string]string, q domain.SeriesQuery) (domain.SeriesResult, error) {
	c, err := a.open(ctx, ds, secrets, queryTimeout)
	if err != nil {
		return domain.SeriesResult{}, err
	}
	defer c.Close()
	data, err := a.queryRange(ctx, c, "query", url.Values{
		"query": {q.Query},
		"start": {nanos(q.From)},
		"end":   {nanos(q.To)},
		"step":  {strconv.FormatFloat(q.Step.Seconds(), 'f', -1, 64)},
	})
	if err != nil {
		return domain.SeriesResult{}, err
	}
	if data.ResultType != "matrix" {
		return domain.SeriesResult{}, fmt.Errorf("%w: the query selects log lines; a series needs a metric query such as sum(rate({…}[$__rate]))", port.ErrDataSourceQuery)
	}
	var result []matrixSeries
	if err := json.Unmarshal(data.Result, &result); err != nil {
		return domain.SeriesResult{}, errNotLoki
	}
	out := domain.SeriesResult{Series: make([]domain.Series, 0, len(result))}
	for _, ms := range result {
		s := domain.Series{Labels: map[string]string{}, Points: make([]domain.SeriesPoint, 0, len(ms.Values))}
		for k, v := range ms.Metric {
			s.Labels[k] = v
		}
		s.Name = seriesName(s.Labels, q.Query)
		for _, p := range ms.Values {
			var at float64
			var raw string
			if json.Unmarshal(p[0], &at) != nil || json.Unmarshal(p[1], &raw) != nil {
				continue
			}
			v, err := strconv.ParseFloat(raw, 64)
			if err != nil {
				v = math.NaN()
			}
			s.Points = append(s.Points, domain.FinitePoint(time.UnixMilli(int64(math.Round(at*1000))).UTC(), v))
		}
		out.Series = append(out.Series, s)
	}
	return out, nil
}

func seriesName(labels map[string]string, query string) string {
	if len(labels) == 0 {
		return query
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+strconv.Quote(labels[k]))
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

// Discover lists the label names written over the last hour: a recipe's
// detect metric for this kind is a label it selects on (service_name).
func (a *Adapter) Discover(ctx context.Context, ds domain.DataSource, secrets map[string]string) (domain.Inventory, error) {
	names, err := a.labelList(ctx, ds, secrets, "/loki/api/v1/labels", "labels")
	if err != nil {
		return domain.Inventory{}, err
	}
	inv := domain.Inventory{Metrics: names}
	if len(names) > maxLabels {
		inv.Metrics, inv.Truncated = names[:maxLabels], true
	}
	return inv, nil
}

func (a *Adapter) LabelValues(ctx context.Context, ds domain.DataSource, secrets map[string]string, label string) ([]string, error) {
	if !labelPattern.MatchString(label) {
		return nil, fmt.Errorf("%w: %q is not a Loki label name", port.ErrDataSourceQuery, label)
	}
	values, err := a.labelList(ctx, ds, secrets, "/loki/api/v1/label/"+url.PathEscape(label)+"/values", "label values")
	if err != nil {
		return nil, err
	}
	return values[:min(len(values), maxLabels)], nil
}

func (a *Adapter) labelList(ctx context.Context, ds domain.DataSource, secrets map[string]string, path, op string) ([]string, error) {
	c, err := a.open(ctx, ds, secrets, queryTimeout)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	now := a.now()
	r, err := c.Do(ctx, httpapi.Request{
		Path:  path,
		Query: url.Values{"start": {nanos(now.Add(-labelWindow))}, "end": {nanos(now)}},
		Limit: maxLabelBody,
	})
	if err != nil {
		return nil, err
	}
	if !r.OK() {
		return nil, refusal(c, op, r)
	}
	var env envelope
	var names []string
	if json.Unmarshal(r.Body, &env) != nil || env.Status != "success" || json.Unmarshal(env.Data, &names) != nil {
		return nil, errNotLoki
	}
	sort.Strings(names)
	if names == nil {
		names = []string{}
	}
	return names, nil
}
