package tempo_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/datasource/tempo"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type recorded struct {
	mu      sync.Mutex
	paths   []string
	queries []url.Values
}

func (r *recorded) all() ([]string, []url.Values) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.paths...), append([]url.Values(nil), r.queries...)
}

func serve(t *testing.T, routes map[string]http.HandlerFunc) (*httptest.Server, *recorded) {
	t.Helper()
	rec := &recorded{}
	mux := http.NewServeMux()
	for path, h := range routes {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			rec.mu.Lock()
			rec.paths = append(rec.paths, r.URL.Path)
			rec.queries = append(rec.queries, r.URL.Query())
			rec.mu.Unlock()
			h(w, r)
		})
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, rec
}

func reply(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = fmt.Fprint(w, body)
	}
}

func newAdapter() *tempo.Adapter {
	a := tempo.New()
	a.SetURLPolicy(urlguard.HumanConfigured())
	return a
}

func source(u string) domain.DataSource {
	return domain.DataSource{Kind: domain.DataSourceKindTempo, Config: map[string]string{"url": u}}
}

const traceID = "4bf92f3577b34da6a3ce929d0e0e4736"

const otlp = `{"batches":[
 {"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"api"}},{"key":"k8s.pod.name","value":{"stringValue":"api-1"}}]},
  "scopeSpans":[{"scope":{"name":"otel"},"spans":[
   {"traceId":"S/kvNXezTaajzpKdDg5HNg==","spanId":"AAAAAAAAAAE=","name":"GET /v1/x","kind":"SPAN_KIND_SERVER",
    "startTimeUnixNano":"1792227600000000000","endTimeUnixNano":"1792227600250000000",
    "attributes":[{"key":"http.status_code","value":{"intValue":"500"}},{"key":"retry","value":{"boolValue":true}}],
    "status":{"code":"STATUS_CODE_ERROR","message":"boom"}}]}]},
 {"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"db"}}]},
  "instrumentationLibrarySpans":[{"spans":[
   {"spanId":"00000000000000f2","parentSpanId":"AAAAAAAAAAE=","name":"SELECT","kind":3,
    "startTimeUnixNano":"1792227600010000000","endTimeUnixNano":"1792227600010500000",
    "attributes":[{"key":"db.args","value":{"arrayValue":{"values":[{"stringValue":"a"},{"intValue":2}]}}}],
    "status":{"code":1}}]}]}
]}`

func TestTraceMapsOTLPJSONToSpans(t *testing.T) {
	srv, rec := serve(t, map[string]http.HandlerFunc{"/tempo/api/traces/" + traceID: reply(200, otlp)})

	tr, err := newAdapter().Trace(context.Background(), source(srv.URL+"/tempo"), nil, "4BF92F3577B34DA6A3CE929D0E0E4736")

	require.NoError(t, err)
	assert.Equal(t, traceID, tr.ID)
	require.Len(t, tr.Spans, 2)
	root, child := tr.Spans[0], tr.Spans[1]
	assert.Equal(t, "0000000000000001", root.ID, "base64 ids read as hex")
	assert.Empty(t, root.ParentID)
	assert.Equal(t, "api", root.Service)
	assert.Equal(t, "GET /v1/x", root.Name)
	assert.Equal(t, "server", root.Kind)
	assert.Equal(t, time.Unix(0, 1792227600000000000).UTC(), root.Start)
	assert.InDelta(t, 250.0, root.DurationMS, 1e-9)
	assert.Equal(t, domain.SpanStatusError, root.Status)
	assert.Equal(t, "boom", root.StatusMessage)
	assert.Equal(t, map[string]string{
		"http.status_code": "500", "retry": "true", "resource.service.name": "api", "resource.k8s.pod.name": "api-1",
	}, root.Attributes)

	assert.Equal(t, "00000000000000f2", child.ID)
	assert.Equal(t, "0000000000000001", child.ParentID)
	assert.Equal(t, "client", child.Kind)
	assert.Equal(t, domain.SpanStatusOK, child.Status)
	assert.InDelta(t, 0.5, child.DurationMS, 1e-9)
	assert.Equal(t, "[a, 2]", child.Attributes["db.args"])
	paths, _ := rec.all()
	assert.Equal(t, []string{"/tempo/api/traces/" + traceID}, paths)
}

func TestAHugeTraceKeepsItsEarliestSpansAndBoundsTheAttributesCopied(t *testing.T) {
	const spans = 20005
	var resource []map[string]any
	resource = append(resource, map[string]any{"key": "service.name", "value": map[string]any{"stringValue": "api"}})
	for i := range 100 {
		resource = append(resource, map[string]any{"key": fmt.Sprintf("r%03d", i), "value": map[string]any{"stringValue": "v"}})
	}
	list := make([]map[string]any, 0, spans)
	for i := range spans {
		list = append(list, map[string]any{"spanId": fmt.Sprintf("%016x", i+1), "name": "op", "startTimeUnixNano": fmt.Sprint(1792227600000000000 + int64(spans-i))})
	}
	body, err := json.Marshal(map[string]any{"batches": []any{map[string]any{
		"resource": map[string]any{"attributes": resource}, "scopeSpans": []any{map[string]any{"spans": list}},
	}}})
	require.NoError(t, err)
	srv, _ := serve(t, map[string]http.HandlerFunc{"/api/traces/" + traceID: reply(200, string(body))})

	tr, err := newAdapter().Trace(context.Background(), source(srv.URL), nil, traceID)

	require.NoError(t, err)
	assert.True(t, tr.Truncated)
	require.Len(t, tr.Spans, 20000)
	assert.Equal(t, fmt.Sprintf("%016x", spans), tr.Spans[0].ID, "the earliest span leads")
	assert.Equal(t, "api", tr.Spans[0].Service, "service.name survives the resource cap")
	assert.Len(t, tr.Spans[0].Attributes, 32, "a resource's attributes are copied into each span up to their cap")
	for _, sp := range tr.Spans {
		assert.NotEqual(t, fmt.Sprintf("%016x", 1), sp.ID, "the latest spans are what is left out")
	}
}

func TestASpansOwnAttributesAreBounded(t *testing.T) {
	var own []map[string]any
	for i := range 300 {
		own = append(own, map[string]any{"key": fmt.Sprintf("a%03d", i), "value": map[string]any{"stringValue": "v"}})
	}
	body, err := json.Marshal(map[string]any{"batches": []any{map[string]any{
		"scopeSpans": []any{map[string]any{"spans": []any{map[string]any{"spanId": "0000000000000001", "name": "op", "attributes": own}}}},
	}}})
	require.NoError(t, err)
	srv, _ := serve(t, map[string]http.HandlerFunc{"/api/traces/" + traceID: reply(200, string(body))})

	tr, err := newAdapter().Trace(context.Background(), source(srv.URL), nil, traceID)

	require.NoError(t, err)
	require.Len(t, tr.Spans, 1)
	assert.Len(t, tr.Spans[0].Attributes, 128)
	assert.False(t, tr.Truncated)
}

func TestAMissingTraceIsNotFound(t *testing.T) {
	srv, _ := serve(t, map[string]http.HandlerFunc{"/api/traces/" + traceID: reply(404, "trace not found")})

	_, err := newAdapter().Trace(context.Background(), source(srv.URL), nil, traceID)

	assert.True(t, errors.Is(err, port.ErrNotFound), "%v", err)
}

func TestAMalformedTraceIDIsNeverRequested(t *testing.T) {
	srv, rec := serve(t, map[string]http.HandlerFunc{"/": reply(200, otlp)})
	for _, id := range []string{"../status", "xyz", "", "4bf92f3577b34da6a3ce929d0e0e47361"} {
		_, err := newAdapter().Trace(context.Background(), source(srv.URL), nil, id)
		assert.True(t, errors.Is(err, port.ErrDataSourceQuery), "%q: %v", id, err)
	}
	paths, _ := rec.all()
	assert.Empty(t, paths)
}

const search = `{"traces":[
 {"traceID":"2f3e0cee77ae5dc9c17ade3689eb2e54","rootServiceName":"api","rootTraceName":"GET /a","startTimeUnixNano":"1792227600000000000","durationMs":120,
  "spanSet":{"spans":[],"matched":3},"spanSets":[{"spans":[],"matched":3},{"spans":[],"matched":2}]},
 {"traceID":"9a1e","rootServiceName":"api","rootTraceName":"GET /b","startTimeUnixNano":"1792227700000000000","durationMs":900,"spanSet":{"matched":1}},
 {"traceID":"77","rootServiceName":"api","rootTraceName":"GET /fast","startTimeUnixNano":"1792227650000000000"}
]}`

func TestSearchTracesMapsSummariesAndMarksErrors(t *testing.T) {
	var asked []string
	srv, rec := serve(t, map[string]http.HandlerFunc{"/api/search": func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		asked = append(asked, q)
		if q == `({ resource.service.name = "api" }) && { status = error }` {
			reply(200, `{"traces":[{"traceID":"9a1e"}]}`)(w, r)
			return
		}
		reply(200, search)(w, r)
	}})
	since := time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)
	until := since.Add(time.Hour).Add(500 * time.Millisecond)

	got, err := newAdapter().SearchTraces(context.Background(), source(srv.URL), nil, domain.TraceQuery{
		Query: `{ resource.service.name = "api" }`, Since: since, Until: until, Limit: 5, MinDuration: 100 * time.Millisecond,
	})

	require.NoError(t, err)
	require.Len(t, got, 2, "a trace shorter than min_duration is left out")
	assert.Equal(t, domain.TraceSummary{
		ID: "00000000000000000000000000009a1e", RootService: "api", RootName: "GET /b",
		Start: time.Unix(0, 1792227700000000000).UTC(), DurationMS: 900, SpanCount: 1, Error: true,
	}, got[0])
	assert.Equal(t, "2f3e0cee77ae5dc9c17ade3689eb2e54", got[1].ID)
	assert.Equal(t, 5, got[1].SpanCount, "matched spans across span sets")
	assert.False(t, got[1].Error)
	_, queries := rec.all()
	assert.Equal(t, fmt.Sprint(since.Unix()), queries[0].Get("start"))
	assert.Equal(t, fmt.Sprint(until.Unix()+1), queries[0].Get("end"), "until rounded up to the second")
	assert.Equal(t, "5", queries[0].Get("limit"))
	assert.Equal(t, "100ms", queries[0].Get("minDuration"))
	assert.Len(t, asked, 2)
}

func TestAFailedErrorSearchLeavesTracesUnmarked(t *testing.T) {
	calls := 0
	srv, _ := serve(t, map[string]http.HandlerFunc{"/api/search": func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 2 {
			reply(400, "invalid TraceQL query: parse error")(w, r)
			return
		}
		reply(200, search)(w, r)
	}})

	got, err := newAdapter().SearchTraces(context.Background(), source(srv.URL), nil, domain.TraceQuery{
		Query: `{ } | count() > 2`, Since: time.Now().Add(-time.Hour), Until: time.Now(),
	})

	require.NoError(t, err)
	require.Len(t, got, 3)
	for _, s := range got {
		assert.False(t, s.Error)
	}
}

func TestARejectedSearchIsTheCallersMistake(t *testing.T) {
	srv, _ := serve(t, map[string]http.HandlerFunc{"/api/search": reply(400, "invalid TraceQL query: parse error at line 1, col 3")})

	_, err := newAdapter().SearchTraces(context.Background(), source(srv.URL), nil, domain.TraceQuery{
		Query: `{ nope`, Since: time.Now().Add(-time.Hour), Until: time.Now(),
	})

	assert.True(t, errors.Is(err, port.ErrDataSourceQuery), "%v", err)
	assert.Contains(t, err.Error(), "parse error")
}

func TestVerifyReadsBuildInfoOrFallsBackToReady(t *testing.T) {
	srv, _ := serve(t, map[string]http.HandlerFunc{"/api/status/buildinfo": reply(200, `{"version":"2.6.1","revision":"x"}`)})
	info, err := newAdapter().Verify(context.Background(), source(srv.URL), nil)
	require.NoError(t, err)
	assert.Equal(t, domain.DataSourceInfo{Version: "2.6.1", Detail: "Tempo"}, info)

	srv, _ = serve(t, map[string]http.HandlerFunc{"/api/status/buildinfo": http.NotFound, "/ready": reply(200, "ready")})
	info, err = newAdapter().Verify(context.Background(), source(srv.URL), nil)
	require.NoError(t, err)
	assert.Equal(t, "Tempo (no build info)", info.Detail)
}

func TestDiscoverListsScopedTagsAndFallsBackToV1(t *testing.T) {
	srv, _ := serve(t, map[string]http.HandlerFunc{"/api/v2/search/tags": reply(200,
		`{"scopes":[{"name":"resource","tags":["service.name"]},{"name":"span","tags":["http.route"]},{"name":"intrinsic","tags":["duration"]}]}`)})
	inv, err := newAdapter().Discover(context.Background(), source(srv.URL), nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"resource.service.name", "span.http.route"}, inv.Metrics)

	srv, _ = serve(t, map[string]http.HandlerFunc{
		"/api/v2/search/tags": http.NotFound,
		"/api/search/tags":    reply(200, `{"tagNames":["service.name"]}`),
	})
	inv, err = newAdapter().Discover(context.Background(), source(srv.URL), nil)
	require.NoError(t, err)
	assert.Equal(t, []string{".service.name"}, inv.Metrics)
}

func TestLabelValuesReadsATagsValues(t *testing.T) {
	srv, rec := serve(t, map[string]http.HandlerFunc{
		"/api/v2/search/tag/resource.service.name/values": reply(200, `{"tagValues":[{"type":"string","value":"billing"},{"type":"string","value":"api"}]}`),
	})

	values, err := newAdapter().LabelValues(context.Background(), source(srv.URL), nil, "resource.service.name")

	require.NoError(t, err)
	assert.Equal(t, []string{"api", "billing"}, values)
	_, queries := rec.all()
	assert.NotEmpty(t, queries[0].Get("start"))

	_, err = newAdapter().LabelValues(context.Background(), source(srv.URL), nil, "a/../b")
	assert.True(t, errors.Is(err, port.ErrDataSourceQuery), "%v", err)
}

func TestWithoutAPolicyNothingIsDialled(t *testing.T) {
	srv, rec := serve(t, map[string]http.HandlerFunc{"/api/status/buildinfo": reply(200, `{"version":"2"}`)})

	_, err := tempo.New().Verify(context.Background(), source(srv.URL), nil)

	assert.True(t, errors.Is(err, urlguard.ErrBlocked), "%v", err)
	paths, _ := rec.all()
	assert.Empty(t, paths)
}
