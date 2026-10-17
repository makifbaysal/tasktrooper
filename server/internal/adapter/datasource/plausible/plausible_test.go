package plausible_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/datasource/plausible"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const key = "plausible-stats-api-key"

var secrets = map[string]string{"api_key": key}

type fixture struct {
	srv     *httptest.Server
	mu      sync.Mutex
	bodies  []map[string]any
	auth    []string
	answer  func(body map[string]any) (int, string)
	adapter *plausible.Adapter
}

func newFixture(t *testing.T) *fixture {
	f := &fixture{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v2/query" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		require.NoError(t, json.Unmarshal(raw, &body))
		f.mu.Lock()
		f.bodies = append(f.bodies, body)
		f.auth = append(f.auth, r.Header.Get("Authorization"))
		f.mu.Unlock()
		status, out := f.answer(body)
		w.WriteHeader(status)
		_, _ = fmt.Fprint(w, out)
	}))
	t.Cleanup(f.srv.Close)
	f.adapter = plausible.New()
	f.adapter.SetURLPolicy(urlguard.HumanConfigured())
	return f
}

func (f *fixture) source() domain.DataSource {
	return domain.DataSource{Kind: domain.DataSourceKindPlausible, Config: map[string]string{"url": f.srv.URL, "site_id": "example.com"}}
}

func (f *fixture) last() map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bodies[len(f.bodies)-1]
}

func TestVerifyReadsTheSitesVisitorsWithTheBearerKey(t *testing.T) {
	f := newFixture(t)
	f.answer = func(map[string]any) (int, string) { return 200, `{"results": [{"metrics": [321], "dimensions": []}]}` }
	info, err := f.adapter.Verify(context.Background(), f.source(), secrets)
	require.NoError(t, err)
	assert.Equal(t, "Plausible · example.com · 321 visitors in the last 7 days", info.Detail)
	assert.Equal(t, "Bearer "+key, f.auth[0])
	assert.Equal(t, "example.com", f.last()["site_id"])
}

func TestDiscoverAnswersPageviewOnceTheSiteHasOne(t *testing.T) {
	f := newFixture(t)
	f.answer = func(map[string]any) (int, string) { return 200, `{"results": [{"metrics": [12], "dimensions": []}]}` }
	inv, err := f.adapter.Discover(context.Background(), f.source(), secrets)
	require.NoError(t, err)
	assert.Equal(t, []string{"pageview"}, inv.Metrics)

	f.answer = func(map[string]any) (int, string) { return 200, `{"results": [{"metrics": [0], "dimensions": []}]}` }
	inv, err = f.adapter.Discover(context.Background(), f.source(), secrets)
	require.NoError(t, err)
	assert.Empty(t, inv.Metrics)
}

func TestSeriesReadsHoursInTheZonePlausibleEchoes(t *testing.T) {
	f := newFixture(t)
	f.answer = func(map[string]any) (int, string) {
		return 200, `{"results": [
			{"metrics": [7200], "dimensions": ["2026-10-16 13:00:00"]},
			{"metrics": [3600], "dimensions": ["2026-10-16 15:00:00"]}
		], "query": {"date_range": ["2026-10-16T13:00:00+03:00", "2026-10-16T15:59:59+03:00"]}}`
	}
	from := time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC)
	res, err := f.adapter.Series(context.Background(), f.source(), secrets, domain.SeriesQuery{
		Query: `{"metric": "page_views", "host": "app.example.com"}`, From: from, To: from.Add(3 * time.Hour), Step: 5 * time.Minute,
	})
	require.NoError(t, err)
	assert.Equal(t, time.Hour, res.Step)
	assert.Empty(t, res.Warnings)
	pts := res.Series[0].Points
	require.Len(t, pts, 3)
	assert.Equal(t, from, pts[0].Time, "13:00 at +03:00 is 10:00 UTC")
	assert.Equal(t, 2.0, *pts[0].Value)
	assert.Equal(t, 0.0, *pts[1].Value)
	assert.Equal(t, 1.0, *pts[2].Value)
	body := f.last()
	assert.Equal(t, []any{"pageviews"}, body["metrics"])
	assert.Equal(t, []any{"time:hour"}, body["dimensions"])
	assert.Equal(t, []any{[]any{"is", "event:hostname", []any{"app.example.com"}}}, body["filters"])
	assert.Equal(t, []any{"2026-10-16T10:00:00Z", "2026-10-16T13:00:00Z"}, body["date_range"])
}

func TestSeriesWithoutAnEchoedZoneReadsUTCAndSaysSo(t *testing.T) {
	f := newFixture(t)
	f.answer = func(map[string]any) (int, string) {
		return 200, `{"results": [{"metrics": [5], "dimensions": ["2026-10-16"]}]}`
	}
	from := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	res, err := f.adapter.Series(context.Background(), f.source(), secrets, domain.SeriesQuery{
		Query: `{"metric": "visitors"}`, From: from, To: from.Add(48 * time.Hour), Step: 24 * time.Hour,
	})
	require.NoError(t, err)
	assert.Equal(t, 24*time.Hour, res.Step)
	require.Len(t, res.Series[0].Points, 2)
	assert.Equal(t, 5.0, *res.Series[0].Points[0].Value)
	assert.NotEmpty(t, res.Warnings)
}

func TestTableReadsTopSourcesAndSaysWhenMoreRemain(t *testing.T) {
	f := newFixture(t)
	f.answer = func(map[string]any) (int, string) {
		return 200, `{"results": [
			{"metrics": [400, 120], "dimensions": ["Google"]},
			{"metrics": [90, 40], "dimensions": ["Direct / None"]}
		]}`
	}
	at := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	res, err := f.adapter.Table(context.Background(), f.source(), secrets, domain.TableQuery{Query: `{"table": "referrers", "limit": 1}`, At: at})
	require.NoError(t, err)
	assert.Equal(t, plausible.TableColumns(domain.RecipeTableReferrers), res.Columns)
	assert.Equal(t, []map[string]any{{"referrer": "Google", "views": 400.0, "visitors": 120.0}}, res.Rows)
	assert.True(t, res.Truncated)
	body := f.last()
	assert.Equal(t, []any{"visit:source"}, body["dimensions"])
	assert.Equal(t, []any{"2026-10-09T12:00:00Z", "2026-10-16T12:00:00Z"}, body["date_range"])
	assert.Equal(t, map[string]any{"limit": 2.0}, body["pagination"])
}

func TestErrorsMaskTheKeyAndTellARateLimitFromABadQuery(t *testing.T) {
	f := newFixture(t)
	f.answer = func(map[string]any) (int, string) { return 400, `{"error": "Invalid filter with key ` + key + `"}` }
	_, err := f.adapter.Discover(context.Background(), f.source(), secrets)
	require.Error(t, err)
	assert.ErrorIs(t, err, port.ErrDataSourceQuery)
	assert.NotContains(t, err.Error(), key)

	f.answer = func(map[string]any) (int, string) { return 429, `{"error": "Too many API requests"}` }
	_, err = f.adapter.Discover(context.Background(), f.source(), secrets)
	require.Error(t, err)
	assert.NotErrorIs(t, err, port.ErrDataSourceQuery)
	assert.Contains(t, err.Error(), "rate limited (HTTP 429)")

	f.answer = func(map[string]any) (int, string) { return 401, `{"error": "Invalid API key"}` }
	_, err = f.adapter.Verify(context.Background(), f.source(), secrets)
	assert.ErrorContains(t, err, "authentication failed (HTTP 401)")
}

func TestASiteOverrideIsReadAndABlankSiteIsRefused(t *testing.T) {
	f := newFixture(t)
	f.answer = func(map[string]any) (int, string) { return 200, `{"results": []}` }
	_, err := f.adapter.Table(context.Background(), f.source(), secrets, domain.TableQuery{Query: `{"table": "pages", "site": "blog.example.com"}`, At: time.Now()})
	require.NoError(t, err)
	assert.Equal(t, "blog.example.com", f.last()["site_id"])

	ds := f.source()
	ds.Config["site_id"] = " "
	_, err = f.adapter.Verify(context.Background(), ds, secrets)
	assert.ErrorContains(t, err, "is not a Plausible site")
}
