package loki_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/datasource/loki"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type recorded struct {
	mu      sync.Mutex
	queries []url.Values
	paths   []string
	headers []http.Header
}

func (r *recorded) last() (string, url.Values, http.Header) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.paths[len(r.paths)-1], r.queries[len(r.queries)-1], r.headers[len(r.headers)-1]
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
			rec.headers = append(rec.headers, r.Header.Clone())
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

func newAdapter() *loki.Adapter {
	a := loki.New()
	a.SetURLPolicy(urlguard.HumanConfigured())
	return a
}

func source(u string, config map[string]string) domain.DataSource {
	c := map[string]string{"url": u}
	for k, v := range config {
		c[k] = v
	}
	return domain.DataSource{Kind: domain.DataSourceKindLoki, Config: c}
}

var (
	since = time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)
	until = time.Date(2026, 10, 17, 10, 0, 0, 0, time.UTC)
)

const streams = `{"status":"success","data":{"resultType":"streams","result":[
 {"stream":{"service_name":"api","detected_level":"error","pod":"api-1"},
  "values":[["1792227600000000000","upstream timeout",{"trace_id":"4bf92f3577b34da6a3ce929d0e0e4736"}]]},
 {"stream":{"app":"api"},
  "values":[["1792227660000000000","WARN slow query took 2s"],["1792227540000000000","plain line"]]},
 {"stream":{"service_name":"api"},
  "values":[["1792227630000000000","level=info msg=ok",{"structuredMetadata":{"traceID":"abc"},"parsed":{"user":"u1"}}]]}
]}}`

func TestLogsMapsStreamsToEntriesNewestFirst(t *testing.T) {
	srv, rec := serve(t, map[string]http.HandlerFunc{"/loki/api/v1/query_range": reply(200, streams)})

	page, err := newAdapter().Logs(context.Background(), source(srv.URL, map[string]string{"auth": "bearer"}),
		map[string]string{"token": "tok", "headers": "X-Scope-OrgID: tenant-1"},
		domain.LogQuery{Query: `{service_name="api"}`, Since: since, Until: until, Limit: 10})

	require.NoError(t, err)
	require.Len(t, page.Entries, 4)
	assert.False(t, page.Truncated)
	first := page.Entries[0]
	assert.Equal(t, time.Unix(0, 1792227660000000000).UTC(), first.Timestamp)
	assert.Equal(t, domain.LogWarning, first.Severity, "a leading level word")
	assert.Equal(t, "api", first.Source)

	timeout := page.Entries[2]
	assert.Equal(t, "upstream timeout", timeout.Message)
	assert.Equal(t, domain.LogError, timeout.Severity, "detected_level")
	assert.Equal(t, "4bf92f3577b34da6a3ce929d0e0e4736", timeout.TraceID, "structured metadata")
	assert.Equal(t, map[string]string{"service_name": "api", "detected_level": "error", "pod": "api-1", "trace_id": "4bf92f3577b34da6a3ce929d0e0e4736"}, timeout.Fields)

	categorized := page.Entries[1]
	assert.Equal(t, domain.LogInfo, categorized.Severity, "level=info in the line")
	assert.Equal(t, "abc", categorized.TraceID)
	assert.Equal(t, "u1", categorized.Fields["user"])
	assert.Equal(t, domain.LogInfo, page.Entries[3].Severity, "no level anywhere reads as info")

	path, q, h := rec.last()
	assert.Equal(t, "/loki/api/v1/query_range", path)
	assert.Equal(t, `{service_name="api"}`, q.Get("query"))
	assert.Equal(t, "backward", q.Get("direction"))
	assert.Equal(t, "10", q.Get("limit"))
	assert.Equal(t, fmt.Sprint(since.UnixNano()), q.Get("start"))
	assert.Equal(t, fmt.Sprint(until.UnixNano()+1), q.Get("end"), "until is inclusive, Loki's end is not")
	assert.Equal(t, "Bearer tok", h.Get("Authorization"))
	assert.Equal(t, "tenant-1", h.Get("X-Scope-OrgID"))
}

func TestLogsAppendsTheConsolesFiltersSafely(t *testing.T) {
	srv, rec := serve(t, map[string]http.HandlerFunc{"/loki/api/v1/query_range": reply(200, streams)})

	page, err := newAdapter().Logs(context.Background(), source(srv.URL, nil), nil, domain.LogQuery{
		Query: `{service_name="api"} | json`, Since: since, Until: until, Limit: 4,
		Text: `say "hi" \ now`, MinSeverity: domain.LogWarning,
	})

	require.NoError(t, err)
	_, q, _ := rec.last()
	below := `"(?i)^(trace|debug|dbug|dbg|verbose|info|information|informational|notice|inf)$"`
	assert.Equal(t, `{service_name="api"} | json`+"\n"+`|= "say \"hi\" \\ now"`+
		` | detected_level!~`+below+` | level!~`+below+` | severity!~`+below, q.Get("query"))
	assert.True(t, page.Truncated, "a full page may hide older lines")
	for _, e := range page.Entries {
		assert.GreaterOrEqual(t, e.Severity.Rank(), domain.LogWarning.Rank(), e.Message)
	}
	assert.Len(t, page.Entries, 2, "lines with no level label are judged by their text here")
}

func TestATrailingCommentCannotSwallowTheConsolesFilters(t *testing.T) {
	srv, rec := serve(t, map[string]http.HandlerFunc{"/loki/api/v1/query_range": reply(200, streams)})

	_, err := newAdapter().Logs(context.Background(), source(srv.URL, nil), nil, domain.LogQuery{
		Query: `{app="x"} # only the api`, Since: since, Until: until, Limit: 4, Text: "boom", MinSeverity: domain.LogError,
	})

	require.NoError(t, err)
	_, q, _ := rec.last()
	lines := strings.Split(q.Get("query"), "\n")
	require.Len(t, lines, 2)
	assert.Equal(t, `{app="x"} # only the api`, lines[0])
	assert.True(t, strings.HasPrefix(lines[1], `|= "boom" | detected_level!~`), lines[1])

	_, err = newAdapter().Logs(context.Background(), source(srv.URL, nil), nil, domain.LogQuery{
		Query: `{app="x"} # note`, Since: since, Until: until, Limit: 4, MinSeverity: domain.LogError,
	})
	require.NoError(t, err)
	_, q, _ = rec.last()
	assert.True(t, strings.HasPrefix(q.Get("query"), "{app=\"x\"} # note\n| detected_level!~"), q.Get("query"))
}

func TestLogsRefusesAMetricQuery(t *testing.T) {
	srv, rec := serve(t, map[string]http.HandlerFunc{"/loki/api/v1/query_range": reply(200, streams)})

	_, err := newAdapter().Logs(context.Background(), source(srv.URL, nil), nil,
		domain.LogQuery{Query: `sum(rate({app="x"}[5m]))`, Since: since, Until: until})

	assert.True(t, errors.Is(err, port.ErrDataSourceQuery), "%v", err)
	assert.Empty(t, rec.paths)
}

func TestARejectedQueryIsTheCallersMistake(t *testing.T) {
	srv, _ := serve(t, map[string]http.HandlerFunc{
		"/loki/api/v1/query_range": reply(400, "parse error at line 1, col 14: syntax error: unexpected IDENTIFIER"),
	})

	_, err := newAdapter().Logs(context.Background(), source(srv.URL, nil), nil,
		domain.LogQuery{Query: `{app="x"} | nope`, Since: since, Until: until})

	assert.True(t, errors.Is(err, port.ErrDataSourceQuery), "%v", err)
	assert.Contains(t, err.Error(), "unexpected IDENTIFIER")
}

func TestSeriesReadsALogQLMetricQuery(t *testing.T) {
	srv, rec := serve(t, map[string]http.HandlerFunc{"/loki/api/v1/query_range": reply(200, `{"status":"success","data":{"resultType":"matrix","result":[
		{"metric":{"service_name":"api"},"values":[[1792227600,"0.5"],[1792227660,"NaN"]]}]}}`)})

	res, err := newAdapter().Series(context.Background(), source(srv.URL, nil), nil, domain.SeriesQuery{
		Query: `sum by (service_name) (rate({service_name="api"} |= "error" [2m]))`, From: since, To: until, Step: time.Minute,
	})

	require.NoError(t, err)
	require.Len(t, res.Series, 1)
	assert.Equal(t, map[string]string{"service_name": "api"}, res.Series[0].Labels)
	require.Len(t, res.Series[0].Points, 2)
	assert.Equal(t, 0.5, *res.Series[0].Points[0].Value)
	assert.Nil(t, res.Series[0].Points[1].Value)
	_, q, _ := rec.last()
	assert.Equal(t, "60", q.Get("step"))
}

func TestSeriesRefusesALogQuery(t *testing.T) {
	srv, _ := serve(t, map[string]http.HandlerFunc{"/loki/api/v1/query_range": reply(200, streams)})

	_, err := newAdapter().Series(context.Background(), source(srv.URL, nil), nil,
		domain.SeriesQuery{Query: `{app="x"}`, From: since, To: until, Step: time.Minute})

	assert.True(t, errors.Is(err, port.ErrDataSourceQuery), "%v", err)
}

func TestVerifyReadsBuildInfo(t *testing.T) {
	srv, _ := serve(t, map[string]http.HandlerFunc{
		"/loki/api/v1/status/buildinfo": reply(200, `{"version":"3.2.1","revision":"abc","branch":"HEAD"}`),
	})

	info, err := newAdapter().Verify(context.Background(), source(srv.URL, nil), nil)

	require.NoError(t, err)
	assert.Equal(t, domain.DataSourceInfo{Version: "3.2.1", Detail: "Loki"}, info)
}

func TestVerifyFallsBackToReady(t *testing.T) {
	tests := []struct {
		name    string
		ready   http.HandlerFunc
		wantErr string
	}{
		{name: "ready", ready: reply(200, "ready\n")},
		{name: "starting", ready: reply(503, "Ingester not ready: waiting for 15s after being ready"), wantErr: "HTTP 503: Ingester not ready"},
		{name: "not loki", ready: reply(200, "<html>Grafana</html>"), wantErr: "not as the Loki API"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, _ := serve(t, map[string]http.HandlerFunc{
				"/loki/api/v1/status/buildinfo": http.NotFound,
				"/ready":                        tt.ready,
			})
			info, err := newAdapter().Verify(context.Background(), source(srv.URL, nil), nil)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "Loki (no build info)", info.Detail)
		})
	}
}

func TestVerifyStopsAtARefusedCredentialWithoutEchoingIt(t *testing.T) {
	srv, rec := serve(t, map[string]http.HandlerFunc{
		"/loki/api/v1/status/buildinfo": reply(401, "invalid token s3cret-token"),
		"/ready":                        reply(200, "ready"),
	})

	_, err := newAdapter().Verify(context.Background(), source(srv.URL, map[string]string{"auth": "bearer"}), map[string]string{"token": "s3cret-token"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "authentication failed (HTTP 401)")
	assert.NotContains(t, err.Error(), "s3cret-token")
	assert.Len(t, rec.paths, 1)
}

func TestDiscoverAndLabelValuesReadTheLastHour(t *testing.T) {
	srv, rec := serve(t, map[string]http.HandlerFunc{
		"/loki/api/v1/labels":                    reply(200, `{"status":"success","data":["service_name","app","__stream_shard__"]}`),
		"/loki/api/v1/label/service_name/values": reply(200, `{"status":"success","data":["billing","api"]}`),
	})
	a := newAdapter()

	inv, err := a.Discover(context.Background(), source(srv.URL, nil), nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"__stream_shard__", "app", "service_name"}, inv.Metrics)

	values, err := a.LabelValues(context.Background(), source(srv.URL, nil), nil, "service_name")
	require.NoError(t, err)
	assert.Equal(t, []string{"api", "billing"}, values)
	_, q, _ := rec.last()
	assert.NotEmpty(t, q.Get("start"))
	assert.NotEmpty(t, q.Get("end"))

	_, err = a.LabelValues(context.Background(), source(srv.URL, nil), nil, "../admin")
	assert.True(t, errors.Is(err, port.ErrDataSourceQuery), "%v", err)
}

func TestWithoutAPolicyNothingIsDialled(t *testing.T) {
	srv, rec := serve(t, map[string]http.HandlerFunc{"/loki/api/v1/status/buildinfo": reply(200, `{"version":"3"}`)})

	_, err := loki.New().Verify(context.Background(), source(srv.URL, nil), nil)

	assert.True(t, errors.Is(err, urlguard.ErrBlocked), "%v", err)
	assert.Empty(t, rec.paths)
}

func TestSchemaMarksEverySecretField(t *testing.T) {
	secret := map[string]bool{}
	for _, f := range loki.New().Schema().Fields {
		secret[f.Key] = f.IsSecret()
	}
	assert.Equal(t, map[string]bool{"url": false, "auth": false, "token": true, "username": false, "password": true, "headers": true}, secret)
}
