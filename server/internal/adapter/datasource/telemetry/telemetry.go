// Package telemetry is the data source kind for TaskTrooper Telemetry, read
// through the one connection saved in Settings → TaskTrooper Telemetry
// (port.TelemetryReader): a source of this kind stores no URL and no key, and
// every request goes to the URL that person typed, under the URL policy the
// telemetry client already enforces. It answers RED series per service or
// endpoint, log lines, error issues, traces and an endpoints table. Its
// query language is domain.TelemetryQuery.
package telemetry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	api = "TaskTrooper Telemetry"

	maxLogLines   = 1000
	maxTraces     = 500
	maxErrors     = 500
	maxLabelNames = 1000
	tableRange    = "1h"
	listRange     = "24h"
)

// telemetryRange is one of the windows the telemetry API answers: it always
// ends now, at a bucket size of its own.
type telemetryRange struct {
	name   string
	span   time.Duration
	bucket time.Duration
}

var ranges = []telemetryRange{
	{"15m", 15 * time.Minute, time.Minute},
	{"30m", 30 * time.Minute, time.Minute},
	{"1h", time.Hour, time.Minute},
	{"6h", 6 * time.Hour, 5 * time.Minute},
	{"24h", 24 * time.Hour, 15 * time.Minute},
	{"7d", 7 * 24 * time.Hour, 2 * time.Hour},
}

type Adapter struct {
	reader port.TelemetryReader
	now    func() time.Time
}

var (
	_ port.DataSourceKindAdapter = (*Adapter)(nil)
	_ port.MetricsQuerier        = (*Adapter)(nil)
	_ port.Discoverer            = (*Adapter)(nil)
	_ port.LabelValuesLister     = (*Adapter)(nil)
	_ port.LogsQuerier           = (*Adapter)(nil)
	_ port.TracesQuerier         = (*Adapter)(nil)
	_ port.ErrorsQuerier         = (*Adapter)(nil)
	_ port.TableQuerier          = (*Adapter)(nil)
)

// New answers an adapter that reads nothing until SetTelemetry; the runtime
// hands it the telemetry service once that exists.
func New() *Adapter { return &Adapter{now: time.Now} }

func (a *Adapter) SetTelemetry(r port.TelemetryReader) { a.reader = r }

func (a *Adapter) SetClock(now func() time.Time) { a.now = now }

func (a *Adapter) Kind() domain.DataSourceKind { return domain.DataSourceKindTaskTrooperTelemetry }

func (a *Adapter) Capabilities() []domain.Capability {
	return []domain.Capability{domain.CapabilityMetrics, domain.CapabilityLogs, domain.CapabilityTraces, domain.CapabilityErrors}
}

func (a *Adapter) Schema() domain.DataSourceSchema {
	return domain.DataSourceSchema{
		Label:       api,
		Description: "Requests, errors and latency per service and endpoint, log lines, error issues and traces, through the connection saved in Settings → TaskTrooper Telemetry.",
		Fields:      []domain.DataSourceField{},
	}
}

// apiError is the telemetry API's own error body.
type apiError struct {
	Error string `json:"error"`
}

func (a *Adapter) get(ctx context.Context, op, path string, query url.Values, out any) error {
	if a.reader == nil {
		return fmt.Errorf("%s %s: the telemetry connection is not available in this build", api, op)
	}
	resp, err := a.reader.Get(ctx, path, query)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return fmt.Errorf("%s %s: %w", api, op, err)
	}
	if resp.Status >= 400 {
		var body apiError
		_ = json.Unmarshal(resp.Body, &body)
		detail := strings.TrimSpace(body.Error)
		if detail == "" {
			detail = fmt.Sprintf("answered %d", resp.Status)
		}
		switch {
		case resp.Status == 404:
			return fmt.Errorf("%s %s: %s: %w", api, op, detail, port.ErrNotFound)
		case resp.Status == 400:
			return fmt.Errorf("%s %s: %s: %w", api, op, detail, port.ErrDataSourceQuery)
		case resp.Status == 403:
			return fmt.Errorf("%s %s: the API key may not read this (%s)", api, op, detail)
		}
		return fmt.Errorf("%s %s: %s", api, op, detail)
	}
	if resp.Truncated {
		return fmt.Errorf("%s %s: the answer is larger than one read may carry", api, op)
	}
	if err := json.Unmarshal(resp.Body, out); err != nil {
		return fmt.Errorf("%s %s: the answer is not what the telemetry API sends", api, op)
	}
	return nil
}

func parse(raw string) (domain.TelemetryQuery, error) {
	q, err := domain.ParseTelemetryQuery(raw)
	if err != nil {
		return q, fmt.Errorf("%w: %v", port.ErrDataSourceQuery, err)
	}
	return q, nil
}

// rangeFor is the shortest window reaching back to since; a since older
// than the longest one answers that one, and false.
func (a *Adapter) rangeFor(since time.Time) (telemetryRange, bool) {
	back := a.now().Sub(since)
	for _, r := range ranges {
		if back <= r.span {
			return r, true
		}
	}
	return ranges[len(ranges)-1], false
}

func scoped(q domain.TelemetryQuery, rangeName string) url.Values {
	v := url.Values{}
	v.Set("range", rangeName)
	if q.Env != "" {
		v.Set("env", q.Env)
	}
	return v
}

func ms(t int64) time.Time { return time.UnixMilli(t).UTC() }

type service struct {
	Name string `json:"name"`
}

func (a *Adapter) services(ctx context.Context, rangeName string) ([]service, error) {
	var out []service
	err := a.get(ctx, "services", "services", url.Values{"range": {rangeName}}, &out)
	return out, err
}

func (a *Adapter) Verify(ctx context.Context, _ domain.DataSource, _ map[string]string) (domain.DataSourceInfo, error) {
	list, err := a.services(ctx, "1h")
	if err != nil {
		return domain.DataSourceInfo{}, err
	}
	return domain.DataSourceInfo{Detail: fmt.Sprintf("%d services reporting in the last hour", len(list))}, nil
}

func (a *Adapter) Discover(ctx context.Context, _ domain.DataSource, _ map[string]string) (domain.Inventory, error) {
	var envs []string
	if err := a.get(ctx, "environments", "environments", nil, &envs); err != nil {
		return domain.Inventory{}, err
	}
	return domain.Inventory{Metrics: []string{domain.TelemetryLabelEnvironment, domain.TelemetryLabelService}}, nil
}

func (a *Adapter) LabelValues(ctx context.Context, _ domain.DataSource, _ map[string]string, label string) ([]string, error) {
	var out []string
	switch label {
	case domain.TelemetryLabelService:
		list, err := a.services(ctx, listRange)
		if err != nil {
			return nil, err
		}
		for _, s := range list {
			if s.Name != "" {
				out = append(out, s.Name)
			}
		}
	case domain.TelemetryLabelEnvironment:
		if err := a.get(ctx, "environments", "environments", nil, &out); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("%w: %s lists only %s and %s", port.ErrDataSourceQuery, api, domain.TelemetryLabelService, domain.TelemetryLabelEnvironment)
	}
	sort.Strings(out)
	out = slices.Compact(out)
	if len(out) > maxLabelNames {
		out = out[:maxLabelNames]
	}
	return out, nil
}

type seriesSet struct {
	Timestamps []int64 `json:"timestamps"`
	Series     []struct {
		ID   string    `json:"id"`
		Data []float64 `json:"data"`
	} `json:"series"`
}

func (s seriesSet) data(id string) []float64 {
	for _, x := range s.Series {
		if x.ID == id {
			return x.Data
		}
	}
	return nil
}

// latencySeries: telemetry keeps p50, p95 and p99; a quantile reads the
// nearest of them at or above it.
func latencySeries(q float64) string {
	switch {
	case q <= 0.5:
		return "p50"
	case q <= 0.95+1e-9:
		return "p95"
	}
	return "p99"
}

func (a *Adapter) Series(ctx context.Context, _ domain.DataSource, _ map[string]string, sq domain.SeriesQuery) (domain.SeriesResult, error) {
	q, err := parse(sq.Query)
	if err != nil {
		return domain.SeriesResult{}, err
	}
	r, covered := a.rangeFor(sq.From)
	path := "services/" + url.PathEscape(q.Service) + "/series"
	if q.Endpoint != "" {
		path = "services/" + url.PathEscape(q.Service) + "/endpoints/" + url.PathEscape(q.Endpoint) + "/series"
	}
	res := domain.SeriesResult{Series: []domain.Series{}, Step: r.bucket}
	if !covered {
		res.Warnings = append(res.Warnings, fmt.Sprintf("%s keeps series for the last 7 days; older points are missing", api))
	}
	var set seriesSet
	if err := a.get(ctx, "series", path, scoped(q, r.name), &set); err != nil {
		if errors.Is(err, port.ErrNotFound) {
			return res, nil
		}
		return domain.SeriesResult{}, err
	}
	rpm := set.data("rpm")
	errRate := set.data("errorRate")
	at := func(xs []float64, i int) (float64, bool) {
		if i < len(xs) {
			return xs[i], true
		}
		return 0, false
	}
	var value func(i int) (float64, bool)
	switch q.Metric {
	case domain.TelemetryMetricRequests:
		value = func(i int) (float64, bool) { v, ok := at(rpm, i); return v / 60, ok }
	case domain.TelemetryMetricErrorRatio:
		value = func(i int) (float64, bool) {
			n, ok := at(rpm, i)
			e, eok := at(errRate, i)
			return e * 100, ok && eok && n > 0
		}
	case domain.TelemetryMetricErrors:
		value = func(i int) (float64, bool) {
			n, ok := at(rpm, i)
			e, eok := at(errRate, i)
			return n * e / 60, ok && eok
		}
	case domain.TelemetryMetricLatency:
		quantile, _ := q.QuantileValue()
		lat := set.data(latencySeries(quantile))
		value = func(i int) (float64, bool) {
			n, ok := at(rpm, i)
			v, vok := at(lat, i)
			return v, ok && vok && n > 0
		}
	case domain.TelemetryMetricRetries, domain.TelemetryMetricDeadLetters:
		id := "retried"
		if q.Metric == domain.TelemetryMetricDeadLetters {
			id = "dlq"
		}
		xs := set.data(id)
		value = func(i int) (float64, bool) { v, ok := at(xs, i); return v / 60, ok }
	}
	if len(rpm) == 0 && q.Metric != domain.TelemetryMetricRetries && q.Metric != domain.TelemetryMetricDeadLetters {
		return res, nil
	}
	labels := map[string]string{"service": q.Service}
	if q.Env != "" {
		labels["env"] = q.Env
	}
	if q.Endpoint != "" {
		labels["endpoint"] = q.Endpoint
	}
	out := domain.Series{Name: q.Metric, Labels: labels, Points: []domain.SeriesPoint{}}
	for i, ts := range set.Timestamps {
		t := ms(ts)
		if t.Before(sq.From.Add(-r.bucket)) || t.After(sq.To) {
			continue
		}
		if v, ok := value(i); ok {
			out.Points = append(out.Points, domain.FinitePoint(t, v))
		} else {
			out.Points = append(out.Points, domain.SeriesPoint{Time: t})
		}
	}
	res.Series = append(res.Series, out)
	return res, nil
}

type logPage struct {
	Lines []struct {
		ID      string `json:"id"`
		Ts      int64  `json:"ts"`
		Level   string `json:"level"`
		Service string `json:"service"`
		Message string `json:"message"`
		TraceID string `json:"traceId"`
		Route   string `json:"route"`
	} `json:"lines"`
	NextBefore int64 `json:"nextBefore"`
}

// levelsFrom are the telemetry levels at or above min; telemetry has no
// level above error, so critical reads error.
func levelsFrom(min domain.LogSeverity) string {
	switch min {
	case domain.LogInfo:
		return "info,warn,error"
	case domain.LogWarning:
		return "warn,error"
	case domain.LogError, domain.LogCritical:
		return "error"
	}
	return ""
}

func (a *Adapter) Logs(ctx context.Context, _ domain.DataSource, _ map[string]string, lq domain.LogQuery) (domain.RuntimeLogPage, error) {
	q, err := parse(lq.Query)
	if err != nil {
		return domain.RuntimeLogPage{}, err
	}
	r, covered := a.rangeFor(lq.Since)
	v := scoped(q, r.name)
	v.Set("service", q.Service)
	if q.Endpoint != "" {
		v.Set("route", q.Endpoint)
	}
	if lv := levelsFrom(lq.MinSeverity); lv != "" {
		v.Set("level", lv)
	}
	if t := strings.TrimSpace(lq.Text); t != "" {
		v.Set("q", t)
	}
	before := lq.Until.UnixMilli() + 1
	if lq.Cursor != "" {
		c, err := strconv.ParseInt(lq.Cursor, 10, 64)
		if err != nil || c <= 0 {
			return domain.RuntimeLogPage{}, fmt.Errorf("%w: the cursor is not one this source gave", port.ErrDataSourceQuery)
		}
		before = min(before, c)
	}
	v.Set("before", strconv.FormatInt(before, 10))
	limit := lq.Limit
	if limit <= 0 || limit > maxLogLines {
		limit = maxLogLines
	}
	v.Set("limit", strconv.Itoa(limit))
	var page logPage
	if err := a.get(ctx, "logs", "logs", v, &page); err != nil {
		return domain.RuntimeLogPage{}, err
	}
	out := domain.RuntimeLogPage{Entries: []domain.RuntimeLogEntry{}, Truncated: !covered}
	reachedSince := false
	for _, l := range page.Lines {
		t := ms(l.Ts)
		if t.Before(lq.Since) {
			reachedSince = true
			continue
		}
		sev, _ := domain.SeverityFromText(l.Level)
		e := domain.RuntimeLogEntry{Timestamp: t, Severity: sev, Message: l.Message, Source: l.Service, Path: l.Route, TraceID: l.TraceID}
		out.Entries = append(out.Entries, e)
	}
	if page.NextBefore > 0 && !reachedSince {
		out.NextCursor = strconv.FormatInt(page.NextBefore, 10)
	}
	return out, nil
}

type errorIssue struct {
	Fingerprint string  `json:"fingerprint"`
	Service     string  `json:"service"`
	Type        string  `json:"type"`
	Message     string  `json:"message"`
	Route       string  `json:"route"`
	FirstSeen   int64   `json:"firstSeen"`
	LastSeen    int64   `json:"lastSeen"`
	Count       float64 `json:"count"`
	SampleTrace string  `json:"sampleTraceId"`
}

func (a *Adapter) Errors(ctx context.Context, _ domain.DataSource, _ map[string]string, eq domain.ErrorsQuery) ([]domain.RuntimeErrorGroup, error) {
	q, err := parse(eq.Query)
	if err != nil {
		return nil, err
	}
	r, _ := a.rangeFor(eq.Since)
	v := scoped(q, r.name)
	v.Set("service", q.Service)
	v.Set("status", "open")
	if q.Endpoint != "" {
		v.Set("route", q.Endpoint)
	}
	limit := eq.Limit
	if limit <= 0 || limit > maxErrors {
		limit = maxErrors
	}
	v.Set("limit", strconv.Itoa(limit))
	var issues []errorIssue
	if err := a.get(ctx, "errors", "errors", v, &issues); err != nil {
		return nil, err
	}
	out := []domain.RuntimeErrorGroup{}
	for _, is := range issues {
		first, last := ms(is.FirstSeen), ms(is.LastSeen)
		if last.Before(eq.Since) || first.After(eq.Until) {
			continue
		}
		msg := is.Message
		if is.Type != "" {
			msg = is.Type + ": " + is.Message
		}
		g := domain.RuntimeErrorGroup{
			Fingerprint: is.Fingerprint, Message: msg, Count: int(is.Count),
			FirstSeen: first, LastSeen: last, Source: is.Route,
			New: !first.Before(eq.Since) && !first.After(eq.Until),
		}
		if is.SampleTrace != "" {
			g.Sample = "trace " + is.SampleTrace
		}
		out = append(out, g)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Count > out[j].Count })
	return out, nil
}

type traceSummary struct {
	ID      string  `json:"id"`
	Ts      int64   `json:"ts"`
	Dur     float64 `json:"dur"`
	Service string  `json:"service"`
	Method  string  `json:"method"`
	Route   string  `json:"route"`
	IsError bool    `json:"isError"`
}

func (a *Adapter) SearchTraces(ctx context.Context, _ domain.DataSource, _ map[string]string, tq domain.TraceQuery) ([]domain.TraceSummary, error) {
	q, err := parse(tq.Query)
	if err != nil {
		return nil, err
	}
	r, _ := a.rangeFor(tq.Since)
	v := scoped(q, r.name)
	v.Set("service", q.Service)
	v.Set("sort", "recent")
	if q.Endpoint != "" {
		v.Set("endpoint", q.Endpoint)
	}
	if tq.MinDuration > 0 {
		v.Set("minMs", strconv.FormatInt(tq.MinDuration.Milliseconds(), 10))
	}
	limit := tq.Limit
	if limit <= 0 || limit > maxTraces {
		limit = maxTraces
	}
	v.Set("limit", strconv.Itoa(limit))
	var list []traceSummary
	if err := a.get(ctx, "traces", "traces", v, &list); err != nil {
		return nil, err
	}
	out := []domain.TraceSummary{}
	for _, t := range list {
		start := ms(t.Ts)
		if start.Before(tq.Since) || (!tq.Until.IsZero() && start.After(tq.Until)) {
			continue
		}
		out = append(out, domain.TraceSummary{
			ID: t.ID, RootService: t.Service, RootName: strings.TrimSpace(t.Method + " " + t.Route),
			Start: start, DurationMS: t.Dur, SpanCount: 1, Error: t.IsError,
		})
	}
	return out, nil
}

type trace struct {
	ID    string `json:"id"`
	Ts    int64  `json:"ts"`
	Spans []struct {
		SpanID       string            `json:"spanId"`
		ParentSpanID string            `json:"parentSpanId"`
		Service      string            `json:"service"`
		Name         string            `json:"name"`
		Kind         string            `json:"kind"`
		Start        float64           `json:"start"`
		Dur          float64           `json:"dur"`
		Failed       bool              `json:"failed"`
		ErrorType    string            `json:"errorType"`
		Attrs        map[string]string `json:"attrs"`
		Resource     map[string]string `json:"resource"`
	} `json:"spans"`
}

func (a *Adapter) Trace(ctx context.Context, _ domain.DataSource, _ map[string]string, traceID string) (domain.Trace, error) {
	id, ok := domain.NormalizeTraceID(traceID)
	if !ok || len(id) != 32 {
		return domain.Trace{}, fmt.Errorf("%q is not a trace id: %w", traceID, port.ErrNotFound)
	}
	var t trace
	if err := a.get(ctx, "trace", "traces/"+id, nil, &t); err != nil {
		return domain.Trace{}, err
	}
	start := ms(t.Ts)
	out := domain.Trace{ID: id, Spans: make([]domain.Span, 0, len(t.Spans))}
	for _, s := range t.Spans {
		attrs := make(map[string]string, len(s.Attrs)+len(s.Resource))
		for k, v := range s.Attrs {
			attrs[k] = v
		}
		for k, v := range s.Resource {
			attrs["resource."+k] = v
		}
		status := domain.SpanStatusOK
		if s.Failed {
			status = domain.SpanStatusError
		}
		out.Spans = append(out.Spans, domain.Span{
			ID: s.SpanID, ParentID: s.ParentSpanID, Service: s.Service, Name: s.Name, Kind: s.Kind,
			Start:      start.Add(time.Duration(s.Start * float64(time.Millisecond))),
			DurationMS: s.Dur, Status: status, StatusMessage: s.ErrorType, Attributes: attrs,
		})
	}
	sort.SliceStable(out.Spans, func(i, j int) bool { return out.Spans[i].Start.Before(out.Spans[j].Start) })
	return out, nil
}

type endpoint struct {
	Method    string  `json:"method"`
	Route     string  `json:"route"`
	RPM       float64 `json:"rpm"`
	ErrorRate float64 `json:"errorRate"`
	P95       float64 `json:"p95"`
}

// Table answers the endpoints table of the query's service over the last
// hour, the busiest first; Metric is ignored.
func (a *Adapter) Table(ctx context.Context, _ domain.DataSource, _ map[string]string, tq domain.TableQuery) (domain.TableResult, error) {
	q, err := parse(tq.Query)
	if err != nil {
		return domain.TableResult{}, err
	}
	res := domain.TableResult{
		Columns: []domain.TableColumn{
			{Key: "endpoint", Label: "Endpoint"},
			{Key: "rpm", Label: "Requests", Unit: "req/min"},
			{Key: "error_pct", Label: "Errors", Unit: "%"},
			{Key: "p95_ms", Label: "p95", Unit: "ms"},
		},
		KeyColumns: []string{"endpoint"},
		Rows:       []map[string]any{},
	}
	var list []endpoint
	if err := a.get(ctx, "endpoints", "services/"+url.PathEscape(q.Service)+"/endpoints", scoped(q, tableRange), &list); err != nil {
		if errors.Is(err, port.ErrNotFound) {
			return res, nil
		}
		return domain.TableResult{}, err
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].RPM > list[j].RPM })
	for _, e := range list {
		res.Rows = append(res.Rows, map[string]any{
			"endpoint":  strings.TrimSpace(e.Method + " " + e.Route),
			"rpm":       e.RPM,
			"error_pct": e.ErrorRate * 100,
			"p95_ms":    e.P95,
		})
	}
	return res, nil
}
