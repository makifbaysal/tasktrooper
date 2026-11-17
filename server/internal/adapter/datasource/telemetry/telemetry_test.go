package telemetry_test

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/datasource/telemetry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type call struct {
	path  string
	query url.Values
}

type fakeReader struct {
	answers map[string]port.TelemetryResponse
	err     error
	calls   []call
}

func (f *fakeReader) Get(_ context.Context, relPath string, query url.Values) (port.TelemetryResponse, error) {
	f.calls = append(f.calls, call{path: relPath, query: query})
	if f.err != nil {
		return port.TelemetryResponse{}, f.err
	}
	if r, ok := f.answers[relPath]; ok {
		return r, nil
	}
	return port.TelemetryResponse{Status: 404, Body: []byte(`{"error":"not found"}`)}, nil
}

func ok(body string) port.TelemetryResponse {
	return port.TelemetryResponse{Status: 200, ContentType: "application/json", Body: []byte(body)}
}

var now = time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)

func adapter(r *fakeReader) *telemetry.Adapter {
	a := telemetry.New()
	a.SetTelemetry(r)
	a.SetClock(func() time.Time { return now })
	return a
}

func TestSeriesReadsREDAndPicksTheShortestCoveringRange(t *testing.T) {
	t0 := now.Add(-2 * time.Minute).UnixMilli()
	t1 := now.Add(-time.Minute).UnixMilli()
	t2 := now.UnixMilli()
	r := &fakeReader{answers: map[string]port.TelemetryResponse{
		"services/orders%20api/series": ok(`{"timestamps":[` + itoa(t0) + `,` + itoa(t1) + `,` + itoa(t2) + `],"series":[
			{"id":"rpm","label":"","data":[120,0,60]},
			{"id":"errorRate","label":"","data":[0.5,0,0.1]},
			{"id":"p50","label":"","data":[10,0,11]},
			{"id":"p95","label":"","data":[40,0,41]},
			{"id":"p99","label":"","data":[90,0,91]}]}`),
	}}
	a := adapter(r)
	read := func(query string) domain.Series {
		res, err := a.Series(context.Background(), domain.DataSource{}, nil, domain.SeriesQuery{
			Query: query, From: now.Add(-10 * time.Minute), To: now, Step: time.Minute,
		})
		require.NoError(t, err)
		require.Len(t, res.Series, 1)
		assert.Equal(t, time.Minute, res.Step)
		return res.Series[0]
	}

	req := read(`{"service":"orders api","env":"production"}`)
	assert.Equal(t, "15m", r.calls[0].query.Get("range"))
	assert.Equal(t, "production", r.calls[0].query.Get("env"))
	assert.Equal(t, []float64{2, 0, 1}, values(req.Points))

	ratio := read(`{"service":"orders api","env":"*","metric":"error_ratio"}`)
	assert.Empty(t, r.calls[1].query.Get("env"), "* reads every environment")
	assert.Equal(t, []any{50.0, nil, 10.0}, nullable(ratio.Points), "no traffic is no ratio, not 0%")

	errs := read(`{"service":"orders api","metric":"errors"}`)
	assert.Equal(t, []float64{1, 0, 0.1}, values(errs.Points))

	p99 := read(`{"service":"orders api","metric":"latency","quantile":"0.99"}`)
	assert.Equal(t, []any{90.0, nil, 91.0}, nullable(p99.Points))
	p50 := read(`{"service":"orders api","metric":"latency","quantile":"0.5"}`)
	assert.Equal(t, []any{10.0, nil, 11.0}, nullable(p50.Points))
}

func TestSeriesOfAnEndpointAndOfAnUnknownService(t *testing.T) {
	r := &fakeReader{answers: map[string]port.TelemetryResponse{
		"services/orders/endpoints/get-orders-id/series": ok(`{"timestamps":[],"series":[]}`),
	}}
	a := adapter(r)
	res, err := a.Series(context.Background(), domain.DataSource{}, nil, domain.SeriesQuery{
		Query: `{"service":"orders","endpoint":"get-orders-id"}`, From: now.Add(-3 * time.Hour), To: now,
	})
	require.NoError(t, err)
	assert.Empty(t, res.Series)
	assert.Equal(t, "6h", r.calls[0].query.Get("range"))

	res, err = a.Series(context.Background(), domain.DataSource{}, nil, domain.SeriesQuery{
		Query: `{"service":"gone"}`, From: now.Add(-30 * 24 * time.Hour), To: now,
	})
	require.NoError(t, err, "a service telemetry has not seen is no data, not a failure")
	assert.Empty(t, res.Series)
	assert.Equal(t, "7d", r.calls[1].query.Get("range"))
	assert.NotEmpty(t, res.Warnings)
}

func TestAMalformedQueryIsTheCallersMistake(t *testing.T) {
	a := adapter(&fakeReader{})
	_, err := a.Series(context.Background(), domain.DataSource{}, nil, domain.SeriesQuery{Query: `{"metric":"cpu"}`, From: now.Add(-time.Hour), To: now})
	assert.ErrorIs(t, err, port.ErrDataSourceQuery)

	r := &fakeReader{answers: map[string]port.TelemetryResponse{
		"logs": {Status: 400, Body: []byte(`{"error":"level must be debug|info|warn|error"}`)},
	}}
	_, err = adapter(r).Logs(context.Background(), domain.DataSource{}, nil, domain.LogQuery{Query: `{"service":"x"}`, Since: now.Add(-time.Hour), Until: now})
	assert.ErrorIs(t, err, port.ErrDataSourceQuery)
	assert.Contains(t, err.Error(), "level must be")
}

func TestAnUnsavedConnectionSaysSo(t *testing.T) {
	a := adapter(&fakeReader{err: errors.New("telemetry is not configured")})
	_, err := a.Verify(context.Background(), domain.DataSource{}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not configured")

	_, err = telemetry.New().Verify(context.Background(), domain.DataSource{}, nil)
	assert.Error(t, err)
}

func TestVerifyDiscoverAndLabelValues(t *testing.T) {
	r := &fakeReader{answers: map[string]port.TelemetryResponse{
		"services":     ok(`[{"name":"orders"},{"name":"billing"},{"name":"orders"}]`),
		"environments": ok(`["staging","production"]`),
	}}
	a := adapter(r)
	info, err := a.Verify(context.Background(), domain.DataSource{}, nil)
	require.NoError(t, err)
	assert.Contains(t, info.Detail, "3 services")

	inv, err := a.Discover(context.Background(), domain.DataSource{}, nil)
	require.NoError(t, err)
	assert.Contains(t, inv.Metrics, domain.TelemetryLabelService)

	services, err := a.LabelValues(context.Background(), domain.DataSource{}, nil, domain.TelemetryLabelService)
	require.NoError(t, err)
	assert.Equal(t, []string{"billing", "orders"}, services)
	envs, err := a.LabelValues(context.Background(), domain.DataSource{}, nil, domain.TelemetryLabelEnvironment)
	require.NoError(t, err)
	assert.Equal(t, []string{"production", "staging"}, envs)
	_, err = a.LabelValues(context.Background(), domain.DataSource{}, nil, "k8s.pod.name")
	assert.ErrorIs(t, err, port.ErrDataSourceQuery)
}

func TestLogsFilterSeverityAndPage(t *testing.T) {
	r := &fakeReader{answers: map[string]port.TelemetryResponse{
		"logs": ok(`{"lines":[
			{"id":"1","ts":` + itoa(now.Add(-time.Minute).UnixMilli()) + `,"level":"error","service":"orders","message":"boom","traceId":"abc","route":"/x"},
			{"id":"2","ts":` + itoa(now.Add(-2*time.Minute).UnixMilli()) + `,"level":"warn","service":"orders","message":"slow"}],
			"nextBefore":` + itoa(now.Add(-2*time.Minute).UnixMilli()) + `,"levels":{},"services":{}}`),
	}}
	a := adapter(r)
	page, err := a.Logs(context.Background(), domain.DataSource{}, nil, domain.LogQuery{
		Query: `{"service":"orders","env":"production"}`, Since: now.Add(-30 * time.Minute), Until: now,
		MinSeverity: domain.LogWarning, Text: "boom", Limit: 2,
	})
	require.NoError(t, err)
	q := r.calls[0].query
	assert.Equal(t, "orders", q.Get("service"))
	assert.Equal(t, "warn,error", q.Get("level"))
	assert.Equal(t, "boom", q.Get("q"))
	assert.Equal(t, "2", q.Get("limit"))
	assert.Equal(t, "30m", q.Get("range"))
	require.Len(t, page.Entries, 2)
	assert.Equal(t, domain.LogError, page.Entries[0].Severity)
	assert.Equal(t, "abc", page.Entries[0].TraceID)
	require.NotEmpty(t, page.NextCursor)

	_, err = a.Logs(context.Background(), domain.DataSource{}, nil, domain.LogQuery{
		Query: `{"service":"orders"}`, Since: now.Add(-30 * time.Minute), Until: now, Cursor: page.NextCursor,
	})
	require.NoError(t, err)
	assert.Equal(t, page.NextCursor, r.calls[1].query.Get("before"))
}

func TestErrorsAreOpenIssuesMostFrequentFirst(t *testing.T) {
	since := now.Add(-time.Hour)
	r := &fakeReader{answers: map[string]port.TelemetryResponse{
		"errors": ok(`[
			{"fingerprint":"1","service":"orders","type":"TypeError","message":"x is undefined","route":"/a","firstSeen":` + itoa(now.Add(-48*time.Hour).UnixMilli()) + `,"lastSeen":` + itoa(now.Add(-time.Minute).UnixMilli()) + `,"count":3,"status":"open"},
			{"fingerprint":"2","service":"orders","type":"","message":"db down","route":"/b","firstSeen":` + itoa(now.Add(-10*time.Minute).UnixMilli()) + `,"lastSeen":` + itoa(now.Add(-time.Minute).UnixMilli()) + `,"count":9,"sampleTraceId":"t1","status":"open"},
			{"fingerprint":"3","service":"orders","type":"","message":"old","route":"/c","firstSeen":` + itoa(now.Add(-72*time.Hour).UnixMilli()) + `,"lastSeen":` + itoa(now.Add(-48*time.Hour).UnixMilli()) + `,"count":50,"status":"open"}]`),
	}}
	groups, err := adapter(r).Errors(context.Background(), domain.DataSource{}, nil, domain.ErrorsQuery{
		Query: `{"service":"orders"}`, Since: since, Until: now, Limit: 10,
	})
	require.NoError(t, err)
	assert.Equal(t, "open", r.calls[0].query.Get("status"))
	require.Len(t, groups, 2, "an issue last seen before the window is not in it")
	assert.Equal(t, "2", groups[0].Fingerprint)
	assert.True(t, groups[0].New)
	assert.Equal(t, "TypeError: x is undefined", groups[1].Message)
	assert.False(t, groups[1].New)
}

func TestTracesAndOneTrace(t *testing.T) {
	id := "0123456789abcdef0123456789abcdef"
	r := &fakeReader{answers: map[string]port.TelemetryResponse{
		"traces": ok(`[{"id":"` + id + `","ts":` + itoa(now.Add(-time.Minute).UnixMilli()) + `,"dur":12.5,"status":500,"service":"orders","method":"GET","route":"/orders","isError":true}]`),
		"traces/" + id: ok(`{"id":"` + id + `","ts":` + itoa(now.UnixMilli()) + `,"dur":12.5,"services":["orders"],"errors":1,"spans":[
			{"spanId":"b","parentSpanId":"a","service":"orders","name":"db","kind":"client","category":"db","start":2,"dur":5,"status":0,"failed":false,"resource":{"host.name":"h1"}},
			{"spanId":"a","service":"orders","name":"GET /orders","kind":"server","category":"app","start":0,"dur":12.5,"status":500,"failed":true,"errorType":"Boom","attrs":{"http.route":"/orders"}}]}`),
	}}
	a := adapter(r)
	list, err := a.SearchTraces(context.Background(), domain.DataSource{}, nil, domain.TraceQuery{
		Query: `{"service":"orders"}`, Since: now.Add(-time.Hour), Until: now, Limit: 20, MinDuration: 10 * time.Millisecond,
	})
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "GET /orders", list[0].RootName)
	assert.True(t, list[0].Error)
	assert.Equal(t, "10", r.calls[0].query.Get("minMs"))

	tr, err := a.Trace(context.Background(), domain.DataSource{}, nil, id)
	require.NoError(t, err)
	require.Len(t, tr.Spans, 2)
	assert.Equal(t, "a", tr.Spans[0].ID)
	assert.Equal(t, domain.SpanStatusError, tr.Spans[0].Status)
	assert.Equal(t, "h1", tr.Spans[1].Attributes["resource.host.name"])
	assert.Equal(t, now.Add(2*time.Millisecond), tr.Spans[1].Start)

	_, err = a.Trace(context.Background(), domain.DataSource{}, nil, "ffffffffffffffffffffffffffffffff")
	assert.ErrorIs(t, err, port.ErrNotFound)
}

func TestEndpointsTable(t *testing.T) {
	r := &fakeReader{answers: map[string]port.TelemetryResponse{
		"services/orders/endpoints": ok(`[
			{"id":"a","serviceId":"orders","method":"GET","route":"/a","rpm":5,"errorRate":0,"p50":1,"p95":2,"p99":3,"monitored":false,"criticality":""},
			{"id":"b","serviceId":"orders","method":"POST","route":"/b","rpm":50,"errorRate":0.02,"p50":1,"p95":20,"p99":30,"monitored":false,"criticality":""}]`),
	}}
	res, err := adapter(r).Table(context.Background(), domain.DataSource{}, nil, domain.TableQuery{Query: `{"service":"orders"}`, At: now})
	require.NoError(t, err)
	require.Len(t, res.Rows, 2)
	assert.Equal(t, "POST /b", res.Rows[0]["endpoint"])
	assert.InDelta(t, 2.0, res.Rows[0]["error_pct"], 1e-9)
	assert.Equal(t, []string{"endpoint"}, res.KeyColumns)
	assert.Equal(t, "1h", r.calls[0].query.Get("range"))
}

func itoa(v int64) string { return strconv.FormatInt(v, 10) }

func values(points []domain.SeriesPoint) []float64 {
	out := make([]float64, 0, len(points))
	for _, p := range points {
		if p.Value == nil {
			out = append(out, -1)
			continue
		}
		out = append(out, *p.Value)
	}
	return out
}

func nullable(points []domain.SeriesPoint) []any {
	out := make([]any, 0, len(points))
	for _, p := range points {
		if p.Value == nil {
			out = append(out, nil)
			continue
		}
		out = append(out, *p.Value)
	}
	return out
}
