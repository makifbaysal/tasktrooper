package datadog_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/datasource/datadog"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	apiKey = "dd-api-key-secret"
	appKey = "dd-app-key-secret"
)

var secrets = map[string]string{"api_key": apiKey, "app_key": appKey}

type fixture struct {
	srv     *httptest.Server
	mu      sync.Mutex
	queries []url.Values
	headers []http.Header
	routes  map[string]func(q url.Values) (int, string)
	adapter *datadog.Adapter
	now     time.Time
}

func newFixture(t *testing.T) *fixture {
	f := &fixture{routes: map[string]func(url.Values) (int, string){}, now: time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.queries = append(f.queries, r.URL.Query())
		f.headers = append(f.headers, r.Header.Clone())
		h, ok := f.routes[r.URL.Path]
		f.mu.Unlock()
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = fmt.Fprint(w, `{"errors": ["Not found"]}`)
			return
		}
		status, body := h(r.URL.Query())
		w.WriteHeader(status)
		_, _ = fmt.Fprint(w, body)
	}))
	t.Cleanup(f.srv.Close)
	f.adapter = datadog.New()
	f.adapter.SetURLPolicy(urlguard.HumanConfigured())
	f.adapter.SetBaseURL(f.srv.URL)
	f.adapter.SetClock(func() time.Time { return f.now })
	return f
}

func source(site string) domain.DataSource {
	return domain.DataSource{Kind: domain.DataSourceKindDatadog, Config: map[string]string{"site": site}}
}

func (f *fixture) last() url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.queries[len(f.queries)-1]
}

func TestVerifyChecksBothKeysOnTheSite(t *testing.T) {
	f := newFixture(t)
	f.routes["/api/v1/validate"] = func(url.Values) (int, string) { return 200, `{"valid": true}` }
	f.routes["/api/v1/metrics"] = func(q url.Values) (int, string) {
		assert.Equal(t, fmt.Sprint(f.now.Add(-time.Hour).Unix()), q.Get("from"))
		return 200, `{"metrics": ["system.cpu.user", "trace.http.request.hits"]}`
	}
	info, err := f.adapter.Verify(context.Background(), source("datadoghq.eu"), secrets)
	require.NoError(t, err)
	assert.Equal(t, "Datadog · datadoghq.eu · 2 metrics active in the last hour", info.Detail)
	for _, h := range f.headers {
		assert.Equal(t, apiKey, h.Get("DD-API-KEY"))
		assert.Equal(t, appKey, h.Get("DD-APPLICATION-KEY"))
	}
}

func TestASiteOffDatadogsListIsRefusedBeforeAnyRequest(t *testing.T) {
	f := newFixture(t)
	_, err := f.adapter.Verify(context.Background(), source("evil.example.com"), secrets)
	assert.ErrorContains(t, err, "is not a Datadog site")
	_, err = f.adapter.Verify(context.Background(), source("datadoghq.com"), map[string]string{"api_key": apiKey})
	assert.ErrorContains(t, err, "both the API key and the application key")
	assert.Empty(t, f.queries)
}

func TestDiscoverListsTheMetricsActiveInTheLastDaySorted(t *testing.T) {
	f := newFixture(t)
	f.routes["/api/v1/metrics"] = func(q url.Values) (int, string) {
		assert.Equal(t, fmt.Sprint(f.now.Add(-24*time.Hour).Unix()), q.Get("from"))
		return 200, `{"metrics": ["trace.http.request.hits", "system.cpu.user"]}`
	}
	inv, err := f.adapter.Discover(context.Background(), source(""), secrets)
	require.NoError(t, err)
	assert.Equal(t, []string{"system.cpu.user", "trace.http.request.hits"}, inv.Metrics)
}

func TestSeriesLabelsEachSeriesByItsTagsAndKeepsDatadogsInterval(t *testing.T) {
	f := newFixture(t)
	from := f.now.Add(-time.Hour)
	f.routes["/api/v1/query"] = func(q url.Values) (int, string) {
		assert.Equal(t, fmt.Sprint(from.Unix()), q.Get("from"))
		assert.Equal(t, fmt.Sprint(f.now.Unix()), q.Get("to"))
		assert.Equal(t, "sum:trace.http.request.hits{service:api} by {resource_name}.as_rate()", q.Get("query"))
		return 200, fmt.Sprintf(`{"status": "ok", "series": [
			{"metric": "trace.http.request.hits", "display_name": "trace.http.request.hits", "scope": "resource_name:get_/users,service:api",
			 "tag_set": ["resource_name:get_/users"], "interval": 20000, "pointlist": [[%d, 1.5], [%d, null]]}
		]}`, from.UnixMilli(), from.Add(20*time.Second).UnixMilli())
	}
	res, err := f.adapter.Series(context.Background(), source(""), secrets, domain.SeriesQuery{
		Query: "sum:trace.http.request.hits{service:api} by {resource_name}.as_rate()", From: from, To: f.now, Step: time.Minute,
	})
	require.NoError(t, err)
	assert.Equal(t, 20*time.Second, res.Step)
	require.Len(t, res.Series, 1)
	s := res.Series[0]
	assert.Equal(t, map[string]string{"resource_name": "get_/users"}, s.Labels)
	require.Len(t, s.Points, 2)
	assert.Equal(t, 1.5, *s.Points[0].Value)
	assert.Nil(t, s.Points[1].Value)
}

func TestTableAnswersEachGroupsLatestPoint(t *testing.T) {
	f := newFixture(t)
	f.routes["/api/v1/query"] = func(q url.Values) (int, string) {
		assert.Equal(t, fmt.Sprint(f.now.Add(-10*time.Minute).Unix()), q.Get("from"))
		return 200, `{"status": "ok", "series": [
			{"tag_set": ["resource_name:get_/a"], "pointlist": [[1, 4], [2, 6], [3, null]]},
			{"tag_set": ["resource_name:get_/b"], "pointlist": [[1, null]]}
		]}`
	}
	res, err := f.adapter.Table(context.Background(), source(""), secrets, domain.TableQuery{Query: "p95:trace.http.request{service:api} by {resource_name}", At: f.now})
	require.NoError(t, err)
	assert.Equal(t, []domain.TableColumn{{Key: "resource_name", Label: "resource_name"}, {Key: "value", Label: "Value"}}, res.Columns)
	assert.Equal(t, []map[string]any{
		{"resource_name": "get_/a", "value": 6.0},
		{"resource_name": "get_/b", "value": nil},
	}, res.Rows)
}

func TestErrorsMaskTheKeysAndTellARateLimitFromABadQuery(t *testing.T) {
	f := newFixture(t)
	f.routes["/api/v1/query"] = func(url.Values) (int, string) {
		return 400, `{"errors": ["Error parsing query: unexpected '}' near key ` + appKey + `"]}`
	}
	_, err := f.adapter.Series(context.Background(), source(""), secrets, domain.SeriesQuery{Query: "avg:x{", From: f.now.Add(-time.Hour), To: f.now})
	require.Error(t, err)
	assert.ErrorIs(t, err, port.ErrDataSourceQuery)
	assert.NotContains(t, err.Error(), appKey)
	assert.Contains(t, err.Error(), "[redacted]")

	f.routes["/api/v1/query"] = func(url.Values) (int, string) { return 200, `{"status": "error", "error": "Rule failed"}` }
	_, err = f.adapter.Series(context.Background(), source(""), secrets, domain.SeriesQuery{Query: "avg:x{*}", From: f.now.Add(-time.Hour), To: f.now})
	assert.ErrorIs(t, err, port.ErrDataSourceQuery)

	f.routes["/api/v1/query"] = func(url.Values) (int, string) { return 429, `{"errors": ["Rate limit exceeded"]}` }
	_, err = f.adapter.Series(context.Background(), source(""), secrets, domain.SeriesQuery{Query: "avg:x{*}", From: f.now.Add(-time.Hour), To: f.now})
	require.Error(t, err)
	assert.NotErrorIs(t, err, port.ErrDataSourceQuery)
	assert.Contains(t, err.Error(), "rate limited (HTTP 429)")

	f.routes["/api/v1/validate"] = func(url.Values) (int, string) { return 403, `{"errors": ["Forbidden"]}` }
	_, err = f.adapter.Verify(context.Background(), source(""), secrets)
	assert.ErrorContains(t, err, "authentication failed (HTTP 403)")
}
