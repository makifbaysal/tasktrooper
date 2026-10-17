package sentry_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/datasource/sentry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const token = "sntrys_secret-token-value"

var secrets = map[string]string{"token": token}

type fixture struct {
	srv      *httptest.Server
	mu       sync.Mutex
	requests []*http.Request
	routes   map[string]http.HandlerFunc
	adapter  *sentry.Adapter
}

func newFixture(t *testing.T) *fixture {
	f := &fixture{routes: map[string]http.HandlerFunc{}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.requests = append(f.requests, r)
		h, ok := f.routes[r.URL.Path]
		f.mu.Unlock()
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = fmt.Fprint(w, `{"detail":"The requested resource does not exist"}`)
			return
		}
		h(w, r)
	}))
	t.Cleanup(f.srv.Close)
	f.adapter = sentry.New()
	f.adapter.SetURLPolicy(urlguard.HumanConfigured())
	return f
}

func (f *fixture) source() domain.DataSource {
	return domain.DataSource{Kind: domain.DataSourceKindSentry, Config: map[string]string{"url": f.srv.URL, "org": "acme"}}
}

func (f *fixture) route(path string, status int, body string) {
	f.routes[path] = func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = fmt.Fprint(w, body)
	}
}

func (f *fixture) project() {
	f.route("/api/0/projects/acme/ios-app/", 200, `{"id":"42","slug":"ios-app"}`)
}

func (f *fixture) last() *http.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests[len(f.requests)-1]
}

func link(cursor string, results bool) string {
	return fmt.Sprintf(`<https://sentry.io/x?cursor=prev>; rel="previous"; results="false"; cursor="prev", <https://evil.example/steal?cursor=%s>; rel="next"; results="%t"; cursor="%s"`, cursor, results, cursor)
}

func TestVerifyReadsTheOrganizationWithTheBearerToken(t *testing.T) {
	f := newFixture(t)
	f.route("/api/0/organizations/acme/", 200, `{"name":"Acme Inc","slug":"acme"}`)
	info, err := f.adapter.Verify(context.Background(), f.source(), secrets)
	require.NoError(t, err)
	assert.Equal(t, "Sentry · Acme Inc", info.Detail)
	assert.Equal(t, "Bearer "+token, f.last().Header.Get("Authorization"))
}

func TestDiscoverPagesTheProjectsByCursorAndNeverFollowsTheLinkURL(t *testing.T) {
	f := newFixture(t)
	page := 0
	f.routes["/api/0/organizations/acme/projects/"] = func(w http.ResponseWriter, r *http.Request) {
		page++
		if page == 1 {
			w.Header().Set("Link", link("0:100:0", true))
			_, _ = fmt.Fprint(w, `[{"slug":"ios-app"},{"slug":"web"}]`)
			return
		}
		assert.Equal(t, "0:100:0", r.URL.Query().Get("cursor"))
		w.Header().Set("Link", link("0:200:0", false))
		_, _ = fmt.Fprint(w, `[{"slug":"api"}]`)
	}
	inv, err := f.adapter.Discover(context.Background(), f.source(), secrets)
	require.NoError(t, err)
	assert.Equal(t, []string{"project/ios-app", "project/web", "project/api"}, inv.Metrics)
	assert.False(t, inv.Truncated)
	for _, r := range f.requests {
		assert.Equal(t, "/api/0/organizations/acme/projects/", r.URL.Path)
	}
}

func TestSeriesReadsCrashFreeUsersAsPercentAtAWholeHourInterval(t *testing.T) {
	f := newFixture(t)
	f.project()
	f.routes["/api/0/organizations/acme/sessions/"] = func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		assert.Equal(t, "crash_free_rate(user)", q.Get("field"))
		assert.Equal(t, "42", q.Get("project"))
		assert.Equal(t, "2h", q.Get("interval"))
		assert.Equal(t, "production", q.Get("environment"))
		assert.Equal(t, `release:"1.4.0"`, q.Get("query"))
		_, _ = fmt.Fprint(w, `{"intervals":["2026-10-17T00:00:00Z","2026-10-17T02:00:00Z","2026-10-17T04:00:00Z"],
			"groups":[{"by":{},"series":{"crash_free_rate(user)":[0.995,null,0.98]}}]}`)
	}
	from := time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC)
	res, err := f.adapter.Series(context.Background(), f.source(), secrets, domain.SeriesQuery{
		Query: `{"project":"ios-app","metric":"crash_free_users","environment":"production","release":"1.4.0"}`,
		From:  from, To: from.Add(6 * time.Hour), Step: 90 * time.Minute,
	})
	require.NoError(t, err)
	assert.Equal(t, 2*time.Hour, res.Step)
	require.Len(t, res.Series, 1)
	pts := res.Series[0].Points
	require.Len(t, pts, 3)
	assert.InDelta(t, 99.5, *pts[0].Value, 1e-9)
	assert.Nil(t, pts[1].Value)
	assert.Equal(t, from.Add(4*time.Hour), pts[2].Time)
}

func TestSeriesReadsErrorEventsAsARatePerSecond(t *testing.T) {
	f := newFixture(t)
	f.routes["/api/0/organizations/acme/events-stats/"] = func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		assert.Equal(t, "7", q.Get("project"), "a numeric project is used as given")
		assert.Equal(t, "5m", q.Get("interval"))
		assert.Equal(t, "errors", q.Get("dataset"))
		assert.Equal(t, "count()", q.Get("yAxis"))
		_, _ = fmt.Fprint(w, `{"data":[[1760659200,[{"count":60}]],[1760659500,[{"count":0}]]]}`)
	}
	from := time.Unix(1760659200, 0)
	res, err := f.adapter.Series(context.Background(), f.source(), secrets, domain.SeriesQuery{
		Query: `{"project":"7","metric":"event_rate"}`, From: from, To: from.Add(10 * time.Minute), Step: 5 * time.Minute,
	})
	require.NoError(t, err)
	require.Len(t, res.Series[0].Points, 2)
	assert.InDelta(t, 0.2, *res.Series[0].Points[0].Value, 1e-9)
	assert.Equal(t, 5*time.Minute, res.Step)
}

const issuesPage = `[{"id":"101","shortId":"IOS-1A","title":"NSInvalidArgumentException","culprit":"-[Feed reload]","permalink":"https://acme.sentry.io/issues/101/",
	"count":"250","userCount":30,"firstSeen":"2026-10-16T20:00:00Z","lastSeen":"2026-10-17T08:00:00Z","level":"fatal","isUnhandled":true,
	"metadata":{"value":"unrecognized selector"}},
	{"id":"102","shortId":"IOS-1B","title":"Timeout","culprit":"api.fetch","permalink":"https://acme.sentry.io/issues/102/",
	"count":"4","userCount":2,"firstSeen":"2026-09-01T00:00:00Z","lastSeen":"2026-10-17T07:00:00Z","level":"error","isUnhandled":false}]`

// issuesPage links its issues on the fixture's own host, as a Sentry links
// to itself.
func (f *fixture) issuesPage() string {
	return strings.ReplaceAll(issuesPage, "https://acme.sentry.io", f.srv.URL)
}

func TestErrorsMapsIssuesToErrorGroupsAndMarksTheNewOnes(t *testing.T) {
	f := newFixture(t)
	f.project()
	page := 0
	f.routes["/api/0/organizations/acme/issues/"] = func(w http.ResponseWriter, r *http.Request) {
		page++
		q := r.URL.Query()
		assert.Equal(t, "42", q.Get("project"))
		assert.Equal(t, "is:unresolved", q.Get("query"))
		assert.Equal(t, "freq", q.Get("sort"))
		assert.Equal(t, "2026-10-16T09:00:00Z", q.Get("start"))
		if page == 1 {
			w.Header().Set("Link", link("c2", true))
			_, _ = fmt.Fprint(w, f.issuesPage())
			return
		}
		assert.Equal(t, "c2", q.Get("cursor"))
		_, _ = fmt.Fprint(w, `[]`)
	}
	since := time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)
	groups, err := f.adapter.Errors(context.Background(), f.source(), secrets, domain.ErrorsQuery{
		Query: `{"project":"ios-app"}`, Since: since, Until: since.Add(24 * time.Hour), Limit: 50,
	})
	require.NoError(t, err)
	require.Len(t, groups, 2)
	assert.Equal(t, domain.RuntimeErrorGroup{
		Fingerprint: "101", Message: "NSInvalidArgumentException", Count: 250, Source: "-[Feed reload]",
		Sample: "unrecognized selector", ExternalURL: f.srv.URL + "/issues/101/", New: true,
		FirstSeen: time.Date(2026, 10, 16, 20, 0, 0, 0, time.UTC), LastSeen: time.Date(2026, 10, 17, 8, 0, 0, 0, time.UTC),
	}, groups[0])
	assert.False(t, groups[1].New)
	assert.Equal(t, 2, page)
}

func TestTableAnswersTheCrashesColumns(t *testing.T) {
	f := newFixture(t)
	f.project()
	f.routes["/api/0/organizations/acme/issues/"] = func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "is:unresolved error.unhandled:true", r.URL.Query().Get("query"))
		assert.Equal(t, "2026-10-10T09:00:00Z", r.URL.Query().Get("start"))
		_, _ = fmt.Fprint(w, f.issuesPage())
	}
	res, err := f.adapter.Table(context.Background(), f.source(), secrets, domain.TableQuery{
		Query: `{"project":"ios-app","search":"is:unresolved error.unhandled:true","days":7}`,
		At:    time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)
	require.Len(t, res.Rows, 2)
	assert.Equal(t, map[string]any{
		"issue": "IOS-1A", "type": "crash", "cause": "NSInvalidArgumentException", "location": "-[Feed reload]",
		"users": 30.0, "reports": 250.0, "last_seen": "2026-10-17T08:00:00Z", "link": f.srv.URL + "/issues/101/",
	}, res.Rows[0])
	assert.Equal(t, "error", res.Rows[1]["type"])
}

func TestRefusalsAreClassifiedAndNeverCarryTheToken(t *testing.T) {
	cases := []struct {
		name     string
		status   int
		badQuery bool
		want     string
	}{
		{"a rate limit", 429, false, "rate limited (HTTP 429)"},
		{"a bad token", 401, false, "authentication failed (HTTP 401): Invalid token [redacted]"},
		{"a bad search", 400, true, "HTTP 400: Invalid token [redacted]"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			f.project()
			f.route("/api/0/organizations/acme/sessions/", tt.status, `{"detail":"Invalid token `+token+`"}`)
			_, err := f.adapter.Series(context.Background(), f.source(), secrets, domain.SeriesQuery{
				Query: `{"project":"ios-app","metric":"crash_free_sessions"}`, From: time.Now().Add(-time.Hour), To: time.Now(), Step: time.Hour,
			})
			require.Error(t, err)
			assert.Equal(t, tt.badQuery, errors.Is(err, port.ErrDataSourceQuery))
			assert.Contains(t, err.Error(), tt.want)
			assert.NotContains(t, err.Error(), token)
		})
	}
}

func TestAnOrganizationSlugThatCouldClimbThePathIsRefused(t *testing.T) {
	f := newFixture(t)
	src := f.source()
	src.Config["org"] = "../admin"
	_, err := f.adapter.Verify(context.Background(), src, secrets)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a Sentry organization slug")
	assert.Empty(t, f.requests)
}

func TestErrorsGiveAFifthOfTheLimitToTheNewestIssuesTheMostFrequentLeaveOut(t *testing.T) {
	f := newFixture(t)
	f.project()
	since := time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)
	issueJSON := func(id, firstSeen string) string {
		return fmt.Sprintf(`{"id":%q,"title":"t%s","count":"9","firstSeen":%q}`, id, id, firstSeen)
	}
	var sorts []string
	f.routes["/api/0/organizations/acme/issues/"] = func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		sorts = append(sorts, q.Get("sort")+" "+q.Get("limit"))
		if q.Get("sort") == "new" {
			_, _ = fmt.Fprint(w, "["+issueJSON("f1", "2026-10-16T10:00:00Z")+","+issueJSON("n1", "2026-10-16T11:00:00Z")+","+issueJSON("old", "2026-01-01T00:00:00Z")+"]")
			return
		}
		w.Header().Set("Link", link("c2", true))
		var page []string
		for i := range 10 {
			page = append(page, issueJSON(fmt.Sprintf("f%d", i), "2026-09-01T00:00:00Z"))
		}
		_, _ = fmt.Fprint(w, "["+strings.Join(page, ",")+"]")
	}

	groups, err := f.adapter.Errors(context.Background(), f.source(), secrets, domain.ErrorsQuery{
		Query: `{"project":"ios-app"}`, Since: since, Until: since.Add(24 * time.Hour), Limit: 10,
	})

	require.NoError(t, err)
	assert.Equal(t, []string{"freq 10", "new 2"}, sorts)
	var ids []string
	for _, g := range groups {
		ids = append(ids, g.Fingerprint)
	}
	assert.Equal(t, []string{"f0", "f1", "f2", "f3", "f4", "f5", "f6", "f7", "f8", "n1"}, ids,
		"a new rare issue takes the last place; one already held, or first seen before the window, does not")
}

func TestIssueCountsAreTheSearchsWhenSentryFilteredThem(t *testing.T) {
	f := newFixture(t)
	f.project()
	f.route("/api/0/organizations/acme/issues/", 200, `[{"id":"1","count":"900","userCount":90,"filtered":{"count":"12","userCount":3},"lifetime":{"count":"5000"}},
		{"id":"2","count":"40","userCount":4,"filtered":null}]`)

	groups, err := f.adapter.Errors(context.Background(), f.source(), secrets, domain.ErrorsQuery{
		Query: `{"project":"ios-app","search":"is:unresolved level:error"}`, Since: time.Now().Add(-time.Hour), Until: time.Now(), Limit: 10,
	})

	require.NoError(t, err)
	require.Len(t, groups, 2)
	assert.Equal(t, 12, groups[0].Count, "the window's events the search matches, never the lifetime count")
	assert.Equal(t, 40, groups[1].Count)
}

func TestAnIssueLinkIsKeptOnlyWhenItPointsAtThisSentry(t *testing.T) {
	f := newFixture(t)
	f.project()
	for _, tc := range []struct {
		name, base, permalink, want string
	}{
		{"this host", "", f.srv.URL + "/organizations/acme/issues/1/", f.srv.URL + "/organizations/acme/issues/1/"},
		{"another host", "", "https://evil.example/issues/1/", ""},
		{"javascript", "", "javascript:alert(1)", ""},
		{"credentials", "", strings.Replace(f.srv.URL, "://", "://u:p@", 1) + "/issues/1/", ""},
		{"an org subdomain of sentry.io", "https://sentry.io", "https://acme.sentry.io/issues/1/", "https://acme.sentry.io/issues/1/"},
		{"sentry.io over http", "https://sentry.io", "http://acme.sentry.io/issues/1/", ""},
		{"a lookalike of sentry.io", "https://sentry.io", "https://acme.sentry.io.evil.example/issues/1/", ""},
		{"sentry.io from a self-hosted Sentry", "", "https://acme.sentry.io/issues/1/", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f.route("/api/0/organizations/acme/issues/", 200, fmt.Sprintf(`[{"id":"1","permalink":%q}]`, tc.permalink))
			if tc.base != "" {
				assert.Equal(t, tc.want, sentry.IssueLinkFor(tc.base, tc.permalink))
				return
			}
			groups, err := f.adapter.Errors(context.Background(), f.source(), secrets, domain.ErrorsQuery{
				Query: `{"project":"ios-app"}`, Since: time.Now().Add(-time.Hour), Until: time.Now(), Limit: 10,
			})
			require.NoError(t, err)
			require.Len(t, groups, 1)
			assert.Equal(t, tc.want, groups[0].ExternalURL)
		})
	}
}
