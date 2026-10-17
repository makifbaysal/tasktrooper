package posthog_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/datasource/posthog"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const key = "phx_personal-secret-key"

var secrets = map[string]string{"personal_api_key": key}

type fixture struct {
	srv     *httptest.Server
	mu      sync.Mutex
	queries []string
	auth    []string
	answer  func(query string) (int, string)
	adapter *posthog.Adapter
}

func newFixture(t *testing.T) *fixture {
	f := &fixture{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/base/api/projects/42/query/" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			Query struct {
				Kind  string `json:"kind"`
				Query string `json:"query"`
			} `json:"query"`
		}
		require.NoError(t, json.Unmarshal(raw, &body))
		assert.Equal(t, "HogQLQuery", body.Query.Kind)
		f.mu.Lock()
		f.queries = append(f.queries, body.Query.Query)
		f.auth = append(f.auth, r.Header.Get("Authorization"))
		f.mu.Unlock()
		status, out := f.answer(body.Query.Query)
		w.WriteHeader(status)
		_, _ = fmt.Fprint(w, out)
	}))
	t.Cleanup(f.srv.Close)
	f.adapter = posthog.New()
	f.adapter.SetURLPolicy(urlguard.HumanConfigured())
	return f
}

func (f *fixture) source() domain.DataSource {
	return domain.DataSource{Kind: domain.DataSourceKindPostHog, Config: map[string]string{"url": f.srv.URL + "/base", "project_id": "42"}}
}

func (f *fixture) last() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.queries[len(f.queries)-1]
}

func TestVerifyRunsAHogQLCountWithTheBearerKey(t *testing.T) {
	f := newFixture(t)
	f.answer = func(string) (int, string) { return 200, `{"results": [[4812]], "columns": ["count()"]}` }
	info, err := f.adapter.Verify(context.Background(), f.source(), secrets)
	require.NoError(t, err)
	assert.Equal(t, "PostHog · project 42 · 4812 events in the last day", info.Detail)
	assert.Equal(t, "Bearer "+key, f.auth[0])
	assert.Contains(t, f.last(), "FROM events")
}

func TestDiscoverListsEventNamesMostFrequentFirst(t *testing.T) {
	f := newFixture(t)
	f.answer = func(string) (int, string) {
		return 200, `{"results": [["$pageview", 900], ["$web_vitals", 40], ["signup", 3]]}`
	}
	inv, err := f.adapter.Discover(context.Background(), f.source(), secrets)
	require.NoError(t, err)
	assert.Equal(t, []string{"$pageview", "$web_vitals", "signup"}, inv.Metrics)
	assert.False(t, inv.Truncated)
	assert.Contains(t, f.last(), "GROUP BY event ORDER BY c DESC LIMIT 2001")
}

func TestSeriesCountsPageViewsPerSecondAndFillsEmptyBucketsWithZero(t *testing.T) {
	f := newFixture(t)
	from := time.Unix(1_700_000_000/300*300, 0).UTC()
	f.answer = func(string) (int, string) {
		return 200, fmt.Sprintf(`{"results": [[%d, 600], [%d, 30]]}`, from.Unix(), from.Unix()+600)
	}
	res, err := f.adapter.Series(context.Background(), f.source(), secrets, domain.SeriesQuery{
		Query: `{"metric": "page_views", "host": "shop.example.com"}`,
		From:  from, To: from.Add(15 * time.Minute), Step: 5 * time.Minute,
	})
	require.NoError(t, err)
	assert.Equal(t, 5*time.Minute, res.Step)
	require.Len(t, res.Series, 1)
	s := res.Series[0]
	assert.Equal(t, map[string]string{"host": "shop.example.com"}, s.Labels)
	require.Len(t, s.Points, 3)
	assert.Equal(t, 2.0, *s.Points[0].Value)
	assert.Equal(t, 0.0, *s.Points[1].Value, "a bucket with no page views is zero")
	assert.Equal(t, 0.1, *s.Points[2].Value)
	q := f.last()
	assert.Contains(t, q, "intDiv(toUnixTimestamp(timestamp), 300) * 300 AS t")
	assert.Contains(t, q, "event = '$pageview'")
	assert.Contains(t, q, "properties.$host = 'shop.example.com'")
	assert.Contains(t, q, fmt.Sprintf("timestamp >= fromUnixTimestamp(%d)", from.Unix()))
}

func TestSeriesReadsAVitalAtItsQuantileAndLeavesEmptyBucketsAsGaps(t *testing.T) {
	f := newFixture(t)
	from := time.Unix(1_700_000_000/60*60, 0).UTC()
	f.answer = func(string) (int, string) { return 200, fmt.Sprintf(`{"results": [[%d, 2450.5]]}`, from.Unix()) }
	res, err := f.adapter.Series(context.Background(), f.source(), secrets, domain.SeriesQuery{
		Query: `{"metric": "lcp"}`, From: from, To: from.Add(2 * time.Minute), Step: time.Minute,
	})
	require.NoError(t, err)
	require.Len(t, res.Series[0].Points, 2)
	assert.Equal(t, 2450.5, *res.Series[0].Points[0].Value)
	assert.Nil(t, res.Series[0].Points[1].Value)
	q := f.last()
	assert.Contains(t, q, "quantile(0.75)(toFloat(properties.$web_vitals_LCP_value))")
	assert.Contains(t, q, "event = '$web_vitals'")
}

func TestAValueThatCouldLeaveItsStringIsRefusedBeforeAnyRequest(t *testing.T) {
	f := newFixture(t)
	f.answer = func(string) (int, string) { return 200, `{"results": []}` }
	for _, host := range []string{`x' OR 1=1 --`, `a\b`} {
		raw, _ := json.Marshal(map[string]string{"metric": "page_views", "host": host})
		_, err := f.adapter.Series(context.Background(), f.source(), secrets, domain.SeriesQuery{
			Query: string(raw), From: time.Unix(0, 0), To: time.Unix(3600, 0), Step: time.Minute,
		})
		assert.ErrorIs(t, err, port.ErrDataSourceQuery, host)
	}
	assert.Empty(t, f.queries)
}

func TestTableReadsTopPagesWithTheirLCPAndSaysWhenMoreRemain(t *testing.T) {
	f := newFixture(t)
	f.answer = func(string) (int, string) {
		return 200, `{"results": [["/", 300, 120, 1800.0], ["/checkout", 90, 40, null], ["/about", 10, 9, 900]]}`
	}
	at := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	res, err := f.adapter.Table(context.Background(), f.source(), secrets, domain.TableQuery{Query: `{"table": "pages", "days": 1, "limit": 2}`, At: at})
	require.NoError(t, err)
	assert.Equal(t, posthog.TableColumns(domain.RecipeTablePages), res.Columns)
	assert.Equal(t, []map[string]any{
		{"path": "/", "views": 300.0, "visitors": 120.0, "lcp_p75": 1800.0},
		{"path": "/checkout", "views": 90.0, "visitors": 40.0, "lcp_p75": nil},
	}, res.Rows)
	assert.True(t, res.Truncated)
	q := f.last()
	assert.Contains(t, q, "properties.$pathname AS path")
	assert.Contains(t, q, "event IN ('$pageview', '$web_vitals')")
	assert.Contains(t, q, fmt.Sprintf("fromUnixTimestamp(%d)", at.Add(-24*time.Hour).Unix()))
	assert.Contains(t, q, "ORDER BY views DESC LIMIT 3")
}

func TestErrorsMaskTheKeyAndTellARateLimitFromABadQuery(t *testing.T) {
	f := newFixture(t)
	f.answer = func(string) (int, string) {
		return 400, `{"type": "validation_error", "detail": "bad token ` + key + ` in query"}`
	}
	_, err := f.adapter.Discover(context.Background(), f.source(), secrets)
	require.Error(t, err)
	assert.ErrorIs(t, err, port.ErrDataSourceQuery)
	assert.NotContains(t, err.Error(), key)
	assert.Contains(t, err.Error(), "[redacted]")

	f.answer = func(string) (int, string) { return 429, `{"detail": "api_queries_budget_exceeded"}` }
	_, err = f.adapter.Discover(context.Background(), f.source(), secrets)
	require.Error(t, err)
	assert.NotErrorIs(t, err, port.ErrDataSourceQuery, "a rate limit is the source failing, not the query")
	assert.Contains(t, err.Error(), "rate limited (HTTP 429)")

	f.answer = func(string) (int, string) { return 403, `{"detail": "missing scope"}` }
	_, err = f.adapter.Verify(context.Background(), f.source(), secrets)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "authentication failed (HTTP 403)")
	assert.Contains(t, err.Error(), "Query Read")
}

func TestOpenRefusesABadProjectAndAMissingKey(t *testing.T) {
	f := newFixture(t)
	ds := f.source()
	ds.Config["project_id"] = "../1"
	_, err := f.adapter.Verify(context.Background(), ds, secrets)
	assert.ErrorContains(t, err, "is not a PostHog project id")
	_, err = f.adapter.Verify(context.Background(), f.source(), map[string]string{})
	assert.ErrorContains(t, err, "no personal API key")
}

func TestTheZeroPolicyDialsNothing(t *testing.T) {
	f := newFixture(t)
	f.adapter.SetURLPolicy(urlguard.Policy{})
	_, err := f.adapter.Verify(context.Background(), f.source(), secrets)
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "not an allowed destination"), err.Error())
}
