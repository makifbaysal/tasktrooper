package vercel_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/datasource/vercel"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/makifbaysal/tasktrooper/server/internal/port/mocks"
)

const token = "vercel-access-token"

type call struct {
	method string
	path   string
	query  url.Values
	auth   string
	body   map[string]any
}

type fixture struct {
	t        *testing.T
	srv      *httptest.Server
	adapter  *vercel.Adapter
	accounts *mocks.CloudAccountReader
	mu       sync.Mutex
	calls    []call
}

func newFixture(t *testing.T, routes map[string]http.HandlerFunc) *fixture {
	t.Helper()
	f := &fixture{t: t}
	mux := http.NewServeMux()
	for path, h := range routes {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			c := call{method: r.Method, path: r.URL.Path, query: r.URL.Query(), auth: r.Header.Get("Authorization")}
			if raw, _ := io.ReadAll(r.Body); len(raw) > 0 {
				require.NoError(t, json.Unmarshal(raw, &c.body))
			}
			f.mu.Lock()
			f.calls = append(f.calls, c)
			f.mu.Unlock()
			h(w, r)
		})
	}
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	f.accounts = &mocks.CloudAccountReader{}
	t.Cleanup(func() { f.accounts.AssertExpectations(t) })
	f.adapter = vercel.New()
	f.adapter.SetURLPolicy(urlguard.HumanConfigured())
	f.adapter.SetCloudAccounts(f.accounts)
	f.adapter.SetBaseURL(f.srv.URL)
	return f
}

func (f *fixture) source(provider domain.CloudProviderKind, fields map[string]string) domain.DataSource {
	id := uuid.New()
	f.accounts.On("CloudCredential", mock.Anything, id).Return(domain.CloudCredential{
		AccountID: id, Provider: provider, Fields: fields,
	}, nil)
	return domain.DataSource{Kind: domain.DataSourceKindVercel, Config: map[string]string{"cloud_account_id": id.String()}}
}

func (f *fixture) team() domain.DataSource {
	return f.source(domain.CloudVercel, map[string]string{"token": token, "team_id": "team_1"})
}

func (f *fixture) last() call {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[len(f.calls)-1]
}

func reply(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = fmt.Fprint(w, body)
	}
}

const (
	visits   = "/v1/query/web-analytics/visits/aggregate"
	observe  = "/v2/observability/query"
	userPath = "/v2/user"
)

func TestVerifyNamesTheTeamAndSendsTheBearerToken(t *testing.T) {
	f := newFixture(t, map[string]http.HandlerFunc{userPath: reply(200, `{"user":{"username":"akif"}}`)})
	info, err := f.adapter.Verify(context.Background(), f.team(), nil)
	require.NoError(t, err)
	assert.Equal(t, "Vercel · team team_1", info.Detail)
	assert.Equal(t, "Bearer "+token, f.last().auth)
}

func TestAnAccountOfAnotherProviderIsRefused(t *testing.T) {
	f := newFixture(t, nil)
	_, err := f.adapter.Verify(context.Background(), f.source(domain.CloudGCP, map[string]string{"service_account_json": "{}"}), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a Vercel account")
}

func TestARefusedTokenSaysSoWithoutTheToken(t *testing.T) {
	f := newFixture(t, map[string]http.HandlerFunc{userPath: reply(403, `{"error":{"message":"bad `+token+`"}}`)})
	_, err := f.adapter.Verify(context.Background(), f.team(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "authentication failed")
	assert.NotContains(t, err.Error(), token)
}

func TestSeriesReadsPageViewsPerSecondByTheHourAndFillsGaps(t *testing.T) {
	f := newFixture(t, map[string]http.HandlerFunc{visits: reply(200, `{"data":[
		{"timestamp":"2026-10-16T10:00:00.000Z","visitors":3,"pageviews":7200},
		{"timestamp":"2026-10-16T12:00:00.000Z","visitors":1,"pageviews":3600}]}`)})
	from := time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC)
	res, err := f.adapter.Series(context.Background(), f.team(), nil, domain.SeriesQuery{
		Query: `{"metric": "page_views", "project": "prj_1"}`, From: from, To: from.Add(3 * time.Hour), Step: 5 * time.Minute,
	})
	require.NoError(t, err)
	assert.Equal(t, time.Hour, res.Step)
	pts := res.Series[0].Points
	require.Len(t, pts, 3)
	assert.Equal(t, 2.0, *pts[0].Value)
	assert.Equal(t, 0.0, *pts[1].Value)
	assert.Equal(t, 1.0, *pts[2].Value)
	c := f.last()
	assert.Equal(t, "prj_1", c.query.Get("projectId"))
	assert.Equal(t, "team_1", c.query.Get("teamId"))
	assert.Equal(t, "hour", c.query.Get("by"))
	assert.Equal(t, "2026-10-16T10:00:00Z", c.query.Get("since"))
}

func TestSeriesByTheDayReadsVisitorsAsCounts(t *testing.T) {
	f := newFixture(t, map[string]http.HandlerFunc{visits: reply(200, `{"data":[{"timestamp":"2026-10-16T00:00:00.000Z","visitors":5,"pageviews":9}]}`)})
	from := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	res, err := f.adapter.Series(context.Background(), f.team(), nil, domain.SeriesQuery{
		Query: `{"metric": "visitors", "project": "prj_1"}`, From: from, To: from.Add(48 * time.Hour), Step: 24 * time.Hour,
	})
	require.NoError(t, err)
	assert.Equal(t, 24*time.Hour, res.Step)
	assert.Equal(t, "day", f.last().query.Get("by"))
	require.Len(t, res.Series[0].Points, 2)
	assert.Equal(t, 5.0, *res.Series[0].Points[0].Value)
	assert.Equal(t, 0.0, *res.Series[0].Points[1].Value)
}

func TestSeriesReadsAVitalAsAProjectScopedPercentile(t *testing.T) {
	f := newFixture(t, map[string]http.HandlerFunc{observe: reply(200, `{"data":[
		{"timestamp":"2026-10-16T10:00:00.000Z","vercel_speed_insights_lcp_ms_p75":2400.5},
		{"timestamp":"2026-10-16T11:00:00.000Z","vercel_speed_insights_lcp_ms_p75":null}]}`)})
	from := time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC)
	res, err := f.adapter.Series(context.Background(), f.team(), nil, domain.SeriesQuery{
		Query: `{"metric": "lcp", "quantile": 0.75, "project": "prj_1"}`, From: from, To: from.Add(3 * time.Hour), Step: time.Minute,
	})
	require.NoError(t, err)
	pts := res.Series[0].Points
	require.Len(t, pts, 3)
	assert.Equal(t, 2400.5, *pts[0].Value)
	assert.Nil(t, pts[1].Value, "a null hour is a gap")
	assert.Nil(t, pts[2].Value, "an hour with no row is a gap")
	c := f.last()
	assert.Equal(t, http.MethodPost, c.method)
	assert.Equal(t, "team_1", c.query.Get("teamId"))
	assert.Equal(t, "vercel.speed_insights.lcp_ms", c.body["metric"])
	assert.Equal(t, "p75", c.body["aggregation"])
	assert.Equal(t, map[string]any{"type": "project", "ownerId": "team_1", "projectIds": []any{"prj_1"}}, c.body["scope"])
	assert.Equal(t, map[string]any{"hours": float64(1)}, c.body["granularity"])
}

func TestAVitalOfAPersonalAccountIsScopedToTheUser(t *testing.T) {
	f := newFixture(t, map[string]http.HandlerFunc{
		userPath: reply(200, `{"user":{"id":"usr_9","username":"akif"}}`),
		observe:  reply(200, `{"data":[]}`),
	})
	ds := f.source(domain.CloudVercel, map[string]string{"token": token})
	from := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	_, err := f.adapter.Series(context.Background(), ds, nil, domain.SeriesQuery{
		Query: `{"metric": "cls", "quantile": 0.9, "project": "prj_1"}`, From: from, To: from.Add(72 * time.Hour), Step: 24 * time.Hour,
	})
	require.NoError(t, err)
	c := f.last()
	assert.Empty(t, c.query.Get("teamId"))
	assert.Equal(t, "p90", c.body["aggregation"])
	assert.Equal(t, "usr_9", c.body["scope"].(map[string]any)["ownerId"])
	assert.Equal(t, map[string]any{"days": float64(1)}, c.body["granularity"])
}

func TestSeriesRefusesAQueryWithoutAProjectOrWithAHost(t *testing.T) {
	f := newFixture(t, nil)
	ds := domain.DataSource{Kind: domain.DataSourceKindVercel}
	for _, q := range []string{`{"metric": "page_views"}`, `{"metric": "page_views", "project": "prj_1", "host": "a.com"}`, `{"table": "pages", "project": "prj_1"}`} {
		_, err := f.adapter.Series(context.Background(), ds, nil, domain.SeriesQuery{Query: q, Step: time.Hour})
		require.ErrorIs(t, err, port.ErrDataSourceQuery, q)
	}
}

func TestAPlanLimitIsAQueryErrorWithVercelsOwnWords(t *testing.T) {
	f := newFixture(t, map[string]http.HandlerFunc{visits: reply(400, `{"error":{"message":"the hobby plan only grants access to the latest 7 days of data."}}`)})
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	_, err := f.adapter.Series(context.Background(), f.team(), nil, domain.SeriesQuery{
		Query: `{"metric": "page_views", "project": "prj_1"}`, From: from, To: from.Add(24 * time.Hour), Step: 24 * time.Hour,
	})
	require.ErrorIs(t, err, port.ErrDataSourceQuery)
	assert.Contains(t, err.Error(), "hobby plan")
}

func TestTableListsTheTopPathsAndDropsOthers(t *testing.T) {
	f := newFixture(t, map[string]http.HandlerFunc{visits: reply(200, `{"data":[
		{"requestPath":"/a","visitors":3,"pageviews":6},
		{"requestPath":"/b","visitors":2,"pageviews":4},
		{"requestPath":"Others","visitors":2,"pageviews":7}]}`)})
	res, err := f.adapter.Table(context.Background(), f.team(), nil, domain.TableQuery{
		Query: `{"table": "pages", "project": "prj_1", "limit": 5, "days": 3}`, At: time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)
	require.Len(t, res.Rows, 2)
	assert.Equal(t, map[string]any{"path": "/a", "views": 6.0, "visitors": 3.0}, res.Rows[0])
	assert.True(t, res.Truncated)
	c := f.last()
	assert.Equal(t, "requestPath", c.query.Get("by"))
	assert.Equal(t, "6", c.query.Get("limit"))
	assert.Equal(t, "2026-10-13T12:00:00Z", c.query.Get("since"))
	assert.Equal(t, []string{"path", "views", "visitors"}, columnKeys(res.Columns))
}

func columnKeys(cols []domain.TableColumn) []string {
	var out []string
	for _, c := range cols {
		out = append(out, c.Key)
	}
	return out
}

func TestDiscoverListsWhatTheAccountCanBeReadFor(t *testing.T) {
	f := newFixture(t, map[string]http.HandlerFunc{userPath: reply(200, `{"user":{"username":"akif"}}`)})
	inv, err := f.adapter.Discover(context.Background(), f.team(), nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"pageview", "speed_insights"}, inv.Metrics)
}

func TestTheSchemaTakesAVercelAccountOnly(t *testing.T) {
	s := vercel.New().Schema()
	require.Len(t, s.Fields, 1)
	assert.Equal(t, domain.FieldCloudAccount, s.Fields[0].Type)
	assert.Equal(t, domain.CloudVercel, s.Fields[0].Provider)
}

const (
	projectOn  = `{"id":"prj_1","webAnalytics":{"id":"w","enabledAt":1783521344822},"speedInsights":{"id":"s","enabledAt":1783549664004}}`
	projectOff = `{"id":"prj_1","webAnalytics":{"id":"w"},"speedInsights":{"id":"s","hasData":false}}`
)

func hourFrom() (time.Time, domain.SeriesQuery) {
	from := time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC)
	return from, domain.SeriesQuery{From: from, To: from.Add(2 * time.Hour), Step: time.Hour}
}

func TestAProjectWithWebAnalyticsOffIsNotEnabledNotAFailure(t *testing.T) {
	f := newFixture(t, map[string]http.HandlerFunc{
		"/v9/projects/prj_1": reply(200, projectOff),
		visits:               reply(200, `{"data":[]}`),
	})
	_, q := hourFrom()
	for _, metric := range []string{"page_views", "visitors"} {
		q.Query = fmt.Sprintf(`{"metric": %q, "project": "prj_1"}`, metric)
		_, err := f.adapter.Series(context.Background(), f.team(), nil, q)
		require.ErrorIs(t, err, port.ErrSignalNotEnabled, metric)
		assert.Contains(t, err.Error(), "Web Analytics is not enabled for this project in Vercel")
		assert.Contains(t, err.Error(), "Hobby plan")
	}
	_, err := f.adapter.Table(context.Background(), f.team(), nil, domain.TableQuery{
		Query: `{"table": "pages", "project": "prj_1"}`, At: time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC),
	})
	require.ErrorIs(t, err, port.ErrSignalNotEnabled)
	assert.NotEqual(t, visits, f.last().path, "the data endpoint is never asked for a project that is off")
}

func TestAProjectWithSpeedInsightsOffIsNotEnabledForTheVitalsOnly(t *testing.T) {
	f := newFixture(t, map[string]http.HandlerFunc{
		"/v9/projects/prj_1": reply(200, `{"webAnalytics":{"enabledAt":1},"speedInsights":{"id":"s"}}`),
		visits:               reply(200, `{"data":[{"timestamp":"2026-10-16T10:00:00.000Z","visitors":1,"pageviews":3600}]}`),
		observe:              reply(200, `{"data":[]}`),
	})
	_, q := hourFrom()

	q.Query = `{"metric": "lcp", "project": "prj_1"}`
	_, err := f.adapter.Series(context.Background(), f.team(), nil, q)
	require.ErrorIs(t, err, port.ErrSignalNotEnabled)
	assert.Contains(t, err.Error(), "Speed Insights is not enabled for this project in Vercel")

	q.Query = `{"metric": "page_views", "project": "prj_1"}`
	res, err := f.adapter.Series(context.Background(), f.team(), nil, q)
	require.NoError(t, err, "traffic still reads: only the vitals are off")
	assert.Equal(t, 1.0, *res.Series[0].Points[0].Value)
}

func TestAProjectWithBothOnIsRead(t *testing.T) {
	f := newFixture(t, map[string]http.HandlerFunc{
		"/v9/projects/prj_1": reply(200, projectOn),
		observe:              reply(200, `{"data":[{"timestamp":"2026-10-16T10:00:00.000Z","vercel_speed_insights_cls_p75":0.05}]}`),
	})
	_, q := hourFrom()
	q.Query = `{"metric": "cls", "project": "prj_1"}`
	res, err := f.adapter.Series(context.Background(), f.team(), nil, q)
	require.NoError(t, err)
	assert.Equal(t, 0.05, *res.Series[0].Points[0].Value)
}

func TestAProjectVercelWillNotDescribeIsReadAnyway(t *testing.T) {
	f := newFixture(t, map[string]http.HandlerFunc{
		"/v9/projects/prj_1": reply(404, `{"error":{"message":"not found"}}`),
		visits:               reply(200, `{"data":[{"timestamp":"2026-10-16T10:00:00.000Z","visitors":2,"pageviews":7200}]}`),
	})
	_, q := hourFrom()
	q.Query = `{"metric": "page_views", "project": "prj_1"}`
	res, err := f.adapter.Series(context.Background(), f.team(), nil, q)
	require.NoError(t, err)
	assert.Equal(t, 2.0, *res.Series[0].Points[0].Value)
}

func TestVercelsOwnNotEnabledAnswerIsTheSameState(t *testing.T) {
	f := newFixture(t, map[string]http.HandlerFunc{
		visits: reply(400, `{"error":{"message":"Web Analytics is not enabled for this project"}}`),
	})
	_, q := hourFrom()
	q.Query = `{"metric": "visitors", "project": "prj_1"}`
	_, err := f.adapter.Series(context.Background(), f.team(), nil, q)
	require.ErrorIs(t, err, port.ErrSignalNotEnabled)
	assert.NotErrorIs(t, err, port.ErrDataSourceQuery)
}

func TestVerifyAndDiscoverPassWithNoAnalyticsOnAnywhere(t *testing.T) {
	f := newFixture(t, map[string]http.HandlerFunc{
		userPath:             reply(200, `{"user":{"username":"akif"}}`),
		"/v9/projects/prj_1": reply(200, projectOff),
	})
	_, err := f.adapter.Verify(context.Background(), f.team(), nil)
	require.NoError(t, err)
	_, err = f.adapter.Discover(context.Background(), f.team(), nil)
	require.NoError(t, err)
}

func TestTheSchemaSaysAnalyticsIsPerProjectAndLimitedOnTheFreePlan(t *testing.T) {
	help := vercel.New().Schema().Fields[0].Help
	assert.Contains(t, help, "Hobby")
	assert.Contains(t, help, "one project")
}
