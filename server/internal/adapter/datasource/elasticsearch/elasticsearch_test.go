package elasticsearch_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/datasource/elasticsearch"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type call struct {
	method string
	path   string
	query  url.Values
	body   map[string]any
	auth   string
}

type recorded struct {
	mu    sync.Mutex
	calls []call
}

func (r *recorded) all() []call {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]call(nil), r.calls...)
}

func serve(t *testing.T, routes map[string]http.HandlerFunc) (*httptest.Server, *recorded) {
	t.Helper()
	rec := &recorded{}
	mux := http.NewServeMux()
	for path, h := range routes {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			raw, _ := io.ReadAll(r.Body)
			var body map[string]any
			_ = json.Unmarshal(raw, &body)
			rec.mu.Lock()
			rec.calls = append(rec.calls, call{method: r.Method, path: r.URL.EscapedPath(), query: r.URL.Query(), body: body, auth: r.Header.Get("Authorization")})
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
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = fmt.Fprint(w, body)
	}
}

func newAdapter() *elasticsearch.Adapter {
	a := elasticsearch.New()
	a.SetURLPolicy(urlguard.HumanConfigured())
	return a
}

func source(u string, config map[string]string) domain.DataSource {
	c := map[string]string{"url": u, "index": "logs-*"}
	for k, v := range config {
		c[k] = v
	}
	return domain.DataSource{Kind: domain.DataSourceKindElasticsearch, Config: c}
}

var (
	since = time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)
	until = time.Date(2026, 10, 17, 10, 0, 0, 0, time.UTC)
)

const hits = `{"hits":{"hits":[
 {"_index":"logs-a","_source":{"@timestamp":"2026-10-17T09:59:00.123Z","message":"upstream timeout",
   "log":{"level":"ERROR"},"service":{"name":"api"},"trace":{"id":"4bf92f3577b34da6a3ce929d0e0e4736"},
   "http":{"request":{"method":"GET"},"response":{"status_code":504}},"url":{"path":"/v1/x"},"tags":["a","b"]},
   "sort":[1792231140123]},
 {"_index":"logs-a","_source":{"@timestamp":"2026-10-17T09:58:00Z","message":"WARN disk 91% full","log.level":"","host.name":"node-1"},
   "sort":[1792231080000]}
]}}`

func TestLogsBuildsTheBoolQueryAndMapsHits(t *testing.T) {
	srv, rec := serve(t, map[string]http.HandlerFunc{"/logs-*/_search": reply(200, hits)})

	page, err := newAdapter().Logs(context.Background(), source(srv.URL, map[string]string{"auth": "api_key"}), map[string]string{"api_key": "ZW5jb2RlZA=="},
		domain.LogQuery{Query: `{"filters": {"service.name": "api"}}`, Since: since, Until: until, Limit: 2, Text: "time out", MinSeverity: domain.LogWarning})

	require.NoError(t, err)
	calls := rec.all()
	require.Len(t, calls, 1)
	c := calls[0]
	assert.Equal(t, http.MethodPost, c.method)
	assert.Equal(t, "/logs-*/_search", c.path)
	assert.Equal(t, "true", c.query.Get("ignore_unavailable"))
	assert.Equal(t, "ApiKey ZW5jb2RlZA==", c.auth)
	assert.Equal(t, float64(2), c.body["size"])
	assert.Equal(t, []any{
		map[string]any{"@timestamp": map[string]any{"order": "desc", "unmapped_type": "date"}},
		map[string]any{"_doc": map[string]any{"order": "desc"}},
	}, c.body["sort"], "a tiebreaker keeps lines of one timestamp from straddling a page twice or never")
	boolQuery := c.body["query"].(map[string]any)["bool"].(map[string]any)
	assert.Equal(t, []any{
		map[string]any{"range": map[string]any{"@timestamp": map[string]any{"gte": float64(since.UnixMilli()), "lte": float64(until.UnixMilli()), "format": "epoch_millis"}}},
		map[string]any{"match": map[string]any{"service.name": map[string]any{"query": "api", "operator": "and"}}},
		map[string]any{"match_phrase": map[string]any{"message": "time out"}},
	}, boolQuery["filter"])
	terms := boolQuery["must_not"].([]any)[0].(map[string]any)["terms"].(map[string]any)["log.level"].([]any)
	assert.Contains(t, terms, "debug")
	assert.Contains(t, terms, "INFO")
	assert.Contains(t, terms, "Notice")
	assert.NotContains(t, terms, "warn")

	require.Len(t, page.Entries, 2)
	e := page.Entries[0]
	assert.Equal(t, time.Date(2026, 10, 17, 9, 59, 0, 123000000, time.UTC), e.Timestamp)
	assert.Equal(t, domain.LogError, e.Severity)
	assert.Equal(t, "upstream timeout", e.Message)
	assert.Equal(t, "api", e.Source)
	assert.Equal(t, "4bf92f3577b34da6a3ce929d0e0e4736", e.TraceID)
	assert.Equal(t, "GET", e.Method)
	assert.Equal(t, "/v1/x", e.Path)
	assert.Equal(t, 504, e.StatusCode)
	assert.Equal(t, "a, b", e.Fields["tags"])
	assert.NotContains(t, e.Fields, "message")
	assert.Equal(t, domain.LogWarning, page.Entries[1].Severity, "no level field: the message's own level word")
	assert.Equal(t, "node-1", page.Entries[1].Source)
	assert.True(t, page.Truncated)
	assert.NotEmpty(t, page.NextCursor)
}

func TestTheCursorHandsTheLastSortValuesBackExactly(t *testing.T) {
	nanos := `{"hits":{"hits":[{"_source":{"message":"x"},"sort":[1792231140123456789]}]}}`
	srv, rec := serve(t, map[string]http.HandlerFunc{"/logs-*/_search": reply(200, nanos)})
	a := newAdapter()
	q := domain.LogQuery{Query: `{"filters": {}}`, Since: since, Until: until, Limit: 1}

	first, err := a.Logs(context.Background(), source(srv.URL, nil), nil, q)
	require.NoError(t, err)
	require.NotEmpty(t, first.NextCursor)
	assert.Equal(t, time.Unix(0, 1792231140123456789).UTC(), first.Entries[0].Timestamp, "a date_nanos sort value")

	q.Cursor = first.NextCursor
	_, err = a.Logs(context.Background(), source(srv.URL, nil), nil, q)
	require.NoError(t, err)
	calls := rec.all()
	assert.Nil(t, calls[0].body["search_after"])
	assert.Len(t, calls[1].body["search_after"], 1)

	q.Cursor = "not-a-cursor!"
	_, err = a.Logs(context.Background(), source(srv.URL, nil), nil, q)
	assert.True(t, errors.Is(err, port.ErrDataSourceQuery), "%v", err)
}

func TestTheCursorKeepsLargeSortValuesOnTheWire(t *testing.T) {
	var got []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ = io.ReadAll(r.Body)
		_, _ = fmt.Fprint(w, `{"hits":{"hits":[{"_source":{"message":"x"},"sort":[1792231140123456789]}]}}`)
	}))
	t.Cleanup(srv.Close)
	a := newAdapter()
	q := domain.LogQuery{Query: `{"filters": {}}`, Since: since, Until: until, Limit: 1}
	first, err := a.Logs(context.Background(), source(srv.URL, nil), nil, q)
	require.NoError(t, err)

	q.Cursor = first.NextCursor
	_, err = a.Logs(context.Background(), source(srv.URL, nil), nil, q)

	require.NoError(t, err)
	assert.Contains(t, string(got), `"search_after":[1792231140123456789]`)
}

func TestLogsRefusesAQueryOutsideTheLanguage(t *testing.T) {
	srv, rec := serve(t, map[string]http.HandlerFunc{"/logs-*/_search": reply(200, hits)})
	for _, q := range []string{`{"query": {"match_all": {}}}`, `{"filters": {"a b": "x"}}`, `not json`} {
		_, err := newAdapter().Logs(context.Background(), source(srv.URL, nil), nil, domain.LogQuery{Query: q, Since: since, Until: until})
		assert.True(t, errors.Is(err, port.ErrDataSourceQuery), "%s: %v", q, err)
	}
	assert.Empty(t, rec.all())
}

func TestAnIndexThatIsNotAPatternIsNeverRequested(t *testing.T) {
	srv, rec := serve(t, map[string]http.HandlerFunc{"/": reply(200, `{"version":{"number":"8.15.0"}}`)})
	for _, index := range []string{"_search", "../x", "logs/x", "logs?x", ""} {
		_, err := newAdapter().Verify(context.Background(), source(srv.URL, map[string]string{"index": index}), nil)
		assert.Error(t, err, index)
	}
	assert.Empty(t, rec.all())
}

func TestARefusedSearchIsTheCallersMistake(t *testing.T) {
	srv, _ := serve(t, map[string]http.HandlerFunc{"/logs-*/_search": reply(400,
		`{"error":{"root_cause":[{"type":"query_shard_exception","reason":"No mapping found for [ts] in order to sort on"}],"type":"search_phase_execution_exception","reason":"all shards failed"},"status":400}`)})

	_, err := newAdapter().Logs(context.Background(), source(srv.URL, nil), nil, domain.LogQuery{Query: `{"filters":{}}`, Since: since, Until: until})

	assert.True(t, errors.Is(err, port.ErrDataSourceQuery), "%v", err)
	assert.Contains(t, err.Error(), "all shards failed")
}

func TestVerifyNamesTheDistributionAndChecksTheIndex(t *testing.T) {
	tests := []struct {
		name    string
		root    string
		caps    string
		want    domain.DataSourceInfo
		wantErr string
	}{
		{
			name: "elasticsearch",
			root: `{"cluster_name":"prod","version":{"number":"8.15.0"}}`,
			caps: `{"indices":["logs-a","logs-b"],"fields":{"@timestamp":{"date":{}}}}`,
			want: domain.DataSourceInfo{Version: "8.15.0", Detail: "Elasticsearch · prod · 2 indices match logs-*"},
		},
		{
			name: "opensearch",
			root: `{"cluster_name":"logs","version":{"number":"2.17.0","distribution":"opensearch"}}`,
			caps: `{"indices":["logs-a"],"fields":{"@timestamp":{"date":{}}}}`,
			want: domain.DataSourceInfo{Version: "2.17.0", Detail: "OpenSearch · logs · 1 indices match logs-*"},
		},
		{name: "no index", root: `{"version":{"number":"8.15.0"}}`, caps: `{"indices":[],"fields":{}}`, wantErr: "no index matches logs-*"},
		{name: "no time field", root: `{"version":{"number":"8.15.0"}}`, caps: `{"indices":["logs-a"],"fields":{}}`, wantErr: "has the time field @timestamp"},
		{name: "not elasticsearch", root: `<html></html>`, wantErr: "not as Elasticsearch"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, rec := serve(t, map[string]http.HandlerFunc{
				"/":                   reply(200, tt.root),
				"/logs-*/_field_caps": reply(200, tt.caps),
			})
			info, err := newAdapter().Verify(context.Background(), source(srv.URL, nil), nil)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, info)
			calls := rec.all()
			assert.Equal(t, "@timestamp", calls[len(calls)-1].query.Get("fields"))
		})
	}
}

func TestVerifyDoesNotEchoTheBasicCredential(t *testing.T) {
	srv, _ := serve(t, map[string]http.HandlerFunc{"/": func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		_, _ = fmt.Fprintf(w, `{"error":{"reason":"unable to authenticate with %s"}}`, r.Header.Get("Authorization"))
	}})

	_, err := newAdapter().Verify(context.Background(), source(srv.URL, map[string]string{"auth": "basic", "username": "elastic"}), map[string]string{"password": "changeme-1"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "authentication failed (HTTP 401)")
	assert.Contains(t, err.Error(), "Basic [redacted]")
}

func TestDiscoverListsFieldsAndLabelValuesFallsBackToKeyword(t *testing.T) {
	var searches int
	srv, rec := serve(t, map[string]http.HandlerFunc{
		"/logs-*/_field_caps": reply(200, `{"indices":["logs-a"],"fields":{"_id":{},"service.name":{"text":{}},"service.name.keyword":{"keyword":{}},"@timestamp":{"date":{}}}}`),
		"/logs-*/_search": func(w http.ResponseWriter, r *http.Request) {
			searches++
			if searches == 1 {
				reply(400, `{"error":{"reason":"Fielddata is disabled on [service.name] in [logs-a]"},"status":400}`)(w, r)
				return
			}
			reply(200, `{"aggregations":{"values":{"buckets":[{"key":"api","doc_count":3},{"key":"billing","doc_count":1}]}}}`)(w, r)
		},
	})
	a := newAdapter()

	inv, err := a.Discover(context.Background(), source(srv.URL, nil), nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"@timestamp", "service.name", "service.name.keyword"}, inv.Metrics)

	values, err := a.LabelValues(context.Background(), source(srv.URL, nil), nil, "service.name")
	require.NoError(t, err)
	assert.Equal(t, []string{"api", "billing"}, values)
	calls := rec.all()
	last := calls[len(calls)-1].body["aggs"].(map[string]any)["values"].(map[string]any)["terms"].(map[string]any)
	assert.Equal(t, "service.name.keyword", last["field"])
	assert.Equal(t, float64(0), calls[len(calls)-1].body["size"])
}
