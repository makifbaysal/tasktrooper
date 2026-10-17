package newrelic_test

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

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/datasource/newrelic"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const key = "NRAK-user-key-secret"

var secrets = map[string]string{"api_key": key}

type request struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables"`
}

type fixture struct {
	srv      *httptest.Server
	mu       sync.Mutex
	requests []request
	keys     []string
	answer   func(r request) (int, string)
	adapter  *newrelic.Adapter
	now      time.Time
}

func newFixture(t *testing.T) *fixture {
	f := &fixture{now: time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/graphql" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var req request
		require.NoError(t, json.Unmarshal(raw, &req))
		f.mu.Lock()
		f.requests = append(f.requests, req)
		f.keys = append(f.keys, r.Header.Get("API-Key"))
		f.mu.Unlock()
		status, out := f.answer(req)
		w.WriteHeader(status)
		_, _ = fmt.Fprint(w, out)
	}))
	t.Cleanup(f.srv.Close)
	f.adapter = newrelic.New()
	f.adapter.SetURLPolicy(urlguard.HumanConfigured())
	f.adapter.SetBaseURL(f.srv.URL)
	f.adapter.SetClock(func() time.Time { return f.now })
	return f
}

func source() domain.DataSource {
	return domain.DataSource{Kind: domain.DataSourceKindNewRelic, Config: map[string]string{"region": "eu", "account_id": "1234567"}}
}

func (f *fixture) last() request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests[len(f.requests)-1]
}

func results(rows string) string {
	return `{"data": {"actor": {"account": {"nrql": {"results": ` + rows + `}}}}}`
}

func TestVerifyReadsTheAccountNameWithTheUserKey(t *testing.T) {
	f := newFixture(t)
	f.answer = func(request) (int, string) {
		return 200, `{"data": {"actor": {"account": {"id": 1234567, "name": "Acme prod"}}}}`
	}
	info, err := f.adapter.Verify(context.Background(), source(), secrets)
	require.NoError(t, err)
	assert.Equal(t, "New Relic · Acme prod", info.Detail)
	assert.Equal(t, key, f.keys[0])
	assert.Equal(t, 1234567.0, f.last().Variables["id"])
}

func TestSeriesAppendsTheWindowAndSplitsValuesAndFacetsIntoSeries(t *testing.T) {
	f := newFixture(t)
	from := f.now.Add(-time.Hour)
	f.answer = func(request) (int, string) {
		return 200, results(fmt.Sprintf(`[
			{"beginTimeSeconds": %[1]d, "endTimeSeconds": %[2]d, "facet": "checkout", "appName": "checkout", "latency": 120.5, "percentile.duration": {"95": 0.3}},
			{"beginTimeSeconds": %[2]d, "endTimeSeconds": %[3]d, "facet": "checkout", "appName": "checkout", "latency": null, "percentile.duration": {"95": 0.4}}
		]`, from.Unix(), from.Add(30*time.Minute).Unix(), f.now.Unix()))
	}
	res, err := f.adapter.Series(context.Background(), source(), secrets, domain.SeriesQuery{
		Query: "SELECT percentile(duration, 95) * 1000 AS 'latency' FROM Transaction WHERE appName = 'checkout' FACET appName",
		From:  from, To: f.now, Step: 30 * time.Minute,
	})
	require.NoError(t, err)
	assert.Equal(t, 30*time.Minute, res.Step)
	nrql := f.last().Variables["nrql"].(string)
	assert.Equal(t, fmt.Sprintf("SELECT percentile(duration, 95) * 1000 AS 'latency' FROM Transaction WHERE appName = 'checkout' FACET appName SINCE %d UNTIL %d TIMESERIES 30 minutes",
		from.UnixMilli(), f.now.UnixMilli()), nrql)
	assert.NotContains(t, f.last().Query, "checkout", "the NRQL travels as a variable, never in the GraphQL text")
	require.Len(t, res.Series, 2)
	byName := map[string]domain.Series{}
	for _, s := range res.Series {
		byName[s.Name] = s
		assert.Equal(t, map[string]string{"facet": "checkout"}, s.Labels)
	}
	latency := byName["latency"]
	require.Len(t, latency.Points, 2)
	assert.Equal(t, 120.5, *latency.Points[0].Value)
	assert.Nil(t, latency.Points[1].Value)
	assert.Equal(t, 0.4, *byName["percentile.duration.95"].Points[1].Value)
}

func TestSeriesRaisesTheBucketToFitNRQLsCeiling(t *testing.T) {
	f := newFixture(t)
	f.answer = func(request) (int, string) { return 200, results(`[]`) }
	_, err := f.adapter.Series(context.Background(), source(), secrets, domain.SeriesQuery{
		Query: "SELECT count(*) FROM Transaction", From: f.now.Add(-7 * 24 * time.Hour), To: f.now, Step: time.Minute,
	})
	require.NoError(t, err)
	assert.Contains(t, f.last().Variables["nrql"], "TIMESERIES 28 minutes")
}

func TestAQueryWithItsOwnWindowIsRefusedBeforeAnyRequest(t *testing.T) {
	f := newFixture(t)
	f.answer = func(request) (int, string) { return 200, results(`[]`) }
	for _, q := range []string{
		"SELECT count(*) FROM Transaction SINCE 1 day ago",
		"SELECT count(*) FROM Transaction timeseries",
		"SELECT count(*) FROM Transaction COMPARE WITH 1 week ago",
	} {
		_, err := f.adapter.Series(context.Background(), source(), secrets, domain.SeriesQuery{Query: q, From: f.now.Add(-time.Hour), To: f.now, Step: time.Minute})
		assert.ErrorIs(t, err, port.ErrDataSourceQuery, q)
	}
	_, err := f.adapter.Series(context.Background(), source(), secrets, domain.SeriesQuery{
		Query: "SELECT count(*) FROM Transaction WHERE name = 'since then'", From: f.now.Add(-time.Hour), To: f.now, Step: time.Minute,
	})
	assert.NoError(t, err, "a keyword inside a string is no clause")
	assert.Len(t, f.requests, 1)
}

func TestACommentThatWouldSwallowTheAppendedClausesIsRefused(t *testing.T) {
	f := newFixture(t)
	f.answer = func(request) (int, string) { return 200, results(`[]`) }
	for _, q := range []string{
		"SELECT count(*) FROM Transaction -- the api",
		"SELECT count(*) FROM Transaction // the api",
		"SELECT count(*) FROM Transaction /* the api */",
		"SELECT count(*) FROM Transaction -- SINCE 1 day ago",
	} {
		_, err := f.adapter.Series(context.Background(), source(), secrets, domain.SeriesQuery{Query: q, From: f.now.Add(-time.Hour), To: f.now, Step: time.Minute})
		require.ErrorIs(t, err, port.ErrDataSourceQuery, q)
		assert.Contains(t, err.Error(), "comments", q)
		_, err = f.adapter.Table(context.Background(), source(), secrets, domain.TableQuery{Query: q, At: f.now})
		require.ErrorIs(t, err, port.ErrDataSourceQuery, q)
	}
	assert.Empty(t, f.requests)

	_, err := f.adapter.Series(context.Background(), source(), secrets, domain.SeriesQuery{
		Query: "SELECT count(*) FROM Transaction WHERE name = 'a -- b' AND request.uri = 'http://x/y'", From: f.now.Add(-time.Hour), To: f.now, Step: time.Minute,
	})
	assert.NoError(t, err, "comment marks inside a string are text")
}

func TestTableAnswersOneRowPerFacetOverTheLastHour(t *testing.T) {
	f := newFixture(t)
	f.answer = func(request) (int, string) {
		return 200, results(`[
			{"facet": "WebTransaction/Go/orders", "name": "WebTransaction/Go/orders", "rpm": 120, "error_pct": 1.5, "p95_ms": 210},
			{"facet": ["GET", "/users"], "rpm": 3, "error_pct": 0, "p95_ms": null}
		]`)
	}
	q := "SELECT rate(count(*), 1 minute) AS 'rpm' FROM Transaction FACET name LIMIT 100"
	res, err := f.adapter.Table(context.Background(), source(), secrets, domain.TableQuery{Query: q, At: f.now})
	require.NoError(t, err)
	assert.Equal(t, fmt.Sprintf("%s SINCE %d UNTIL %d", q, f.now.Add(-time.Hour).UnixMilli(), f.now.UnixMilli()), f.last().Variables["nrql"])
	assert.Equal(t, []map[string]any{
		{"facet": "WebTransaction/Go/orders", "name": "WebTransaction/Go/orders", "rpm": 120.0, "error_pct": 1.5, "p95_ms": 210.0},
		{"facet": "GET, /users", "rpm": 3.0, "error_pct": 0.0, "p95_ms": nil},
	}, res.Rows)

	own := "SELECT count(*) AS 'views' FROM PageView FACET pageUrl SINCE 7 days ago"
	_, err = f.adapter.Table(context.Background(), source(), secrets, domain.TableQuery{Query: own, At: f.now})
	require.NoError(t, err)
	assert.Equal(t, own, f.last().Variables["nrql"], "a table keeps its own SINCE")
}

func TestDiscoverListsEventTypes(t *testing.T) {
	f := newFixture(t)
	f.answer = func(request) (int, string) { return 200, results(`[{"eventTypes": ["Transaction", "PageView"]}]`) }
	inv, err := f.adapter.Discover(context.Background(), source(), secrets)
	require.NoError(t, err)
	assert.Equal(t, []string{"Transaction", "PageView"}, inv.Metrics)
	assert.Equal(t, "SHOW EVENT TYPES SINCE 1 day ago", f.last().Variables["nrql"])
}

func TestErrorsMaskTheKeyAndTellARateLimitFromABadQuery(t *testing.T) {
	f := newFixture(t)
	f.answer = func(request) (int, string) {
		return 200, `{"data": {"actor": {"account": {"nrql": null}}}, "errors": [{"message": "NRQL Syntax Error: Error at line 1 near ` + key + `"}]}`
	}
	_, err := f.adapter.Discover(context.Background(), source(), secrets)
	require.Error(t, err)
	assert.ErrorIs(t, err, port.ErrDataSourceQuery)
	assert.NotContains(t, err.Error(), key)
	assert.Contains(t, err.Error(), "[redacted]")

	f.answer = func(request) (int, string) { return 429, `{"errors": [{"message": "Too many requests"}]}` }
	_, err = f.adapter.Discover(context.Background(), source(), secrets)
	require.Error(t, err)
	assert.NotErrorIs(t, err, port.ErrDataSourceQuery)
	assert.Contains(t, err.Error(), "rate limited (HTTP 429)")

	f.answer = func(request) (int, string) { return 200, `{"errors": [{"message": "Invalid API key"}]}` }
	_, err = f.adapter.Verify(context.Background(), source(), secrets)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "authentication failed")
	assert.NotErrorIs(t, err, port.ErrDataSourceQuery)
}

func TestARegionOrAccountOffTheListIsRefused(t *testing.T) {
	f := newFixture(t)
	ds := source()
	ds.Config["region"] = "mars"
	_, err := f.adapter.Verify(context.Background(), ds, secrets)
	assert.ErrorContains(t, err, "is not a New Relic region")
	ds = source()
	ds.Config["account_id"] = "12ab"
	_, err = f.adapter.Verify(context.Background(), ds, secrets)
	assert.ErrorContains(t, err, "is not a New Relic account id")
	assert.Empty(t, f.requests)
}
