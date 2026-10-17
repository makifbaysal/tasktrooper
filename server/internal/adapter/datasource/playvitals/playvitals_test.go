package playvitals_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/datasource/playvitals"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/makifbaysal/tasktrooper/server/internal/port/mocks"
)

const serviceAccount = `{"client_email":"vitals@acme.iam.gserviceaccount.com","private_key":"-----BEGIN PRIVATE KEY-----\nAAAA\n-----END PRIVATE KEY-----\n"}`

type minted struct {
	mu     sync.Mutex
	calls  int
	scopes []string
}

func (m *minted) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

type fixture struct {
	t        *testing.T
	now      time.Time
	mint     *minted
	creds    *mocks.StoreCredentialReader
	adapter  *playvitals.Adapter
	mu       sync.Mutex
	requests []*http.Request
	bodies   []map[string]any
	routes   map[string]http.HandlerFunc
}

func newFixture(t *testing.T) *fixture {
	f := &fixture{t: t, now: time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC), mint: &minted{}, routes: map[string]http.HandlerFunc{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		f.mu.Lock()
		f.requests = append(f.requests, r)
		f.bodies = append(f.bodies, body)
		h, ok := f.routes[r.Method+" "+r.URL.Path]
		f.mu.Unlock()
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = fmt.Fprint(w, `{"error":{"code":404,"message":"not here"}}`)
			return
		}
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	f.creds = mocks.NewStoreCredentialReader(t)
	f.adapter = playvitals.New()
	f.adapter.SetURLPolicy(urlguard.HumanConfigured())
	f.adapter.SetStoreCredentials(f.creds)
	f.adapter.SetBaseURL(srv.URL)
	f.adapter.SetClock(func() time.Time { return f.now })
	f.adapter.SetMinter(func(_ context.Context, client *http.Client, key, scope string) (string, time.Time, error) {
		f.mint.mu.Lock()
		defer f.mint.mu.Unlock()
		if client == nil || key != serviceAccount {
			return "", time.Time{}, errors.New("unexpected key")
		}
		f.mint.calls++
		f.mint.scopes = append(f.mint.scopes, scope)
		return fmt.Sprintf("ya29.token-%d", f.mint.calls), f.now.Add(time.Hour), nil
	})
	return f
}

func (f *fixture) credential() {
	f.creds.On("StoreCredential", mock.Anything, domain.StoreCredentialGooglePlay).
		Return(domain.StoreCredential{Provider: domain.StoreCredentialGooglePlay, Data: map[string]string{"service_account_json": serviceAccount}}, nil)
}

func (f *fixture) route(key string, status int, body string) {
	f.routes[key] = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = fmt.Fprint(w, body)
	}
}

func (f *fixture) last() (*http.Request, map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests[len(f.requests)-1], f.bodies[len(f.bodies)-1]
}

func source(config map[string]string) domain.DataSource {
	c := map[string]string{"store_credential": "google_play"}
	for k, v := range config {
		c[k] = v
	}
	return domain.DataSource{Kind: domain.DataSourceKindGooglePlayVitals, Config: c}
}

const freshDaily = `{"name":"apps/com.acme.app/crashRateMetricSet","freshnessInfo":{"freshnesses":[
	{"aggregationPeriod":"DAILY","latestEndTime":{"year":2026,"month":10,"day":16,"timeZone":{"id":"America/Los_Angeles"}}},
	{"aggregationPeriod":"HOURLY","latestEndTime":{"year":2026,"month":10,"day":17,"hours":3,"timeZone":{"id":"UTC"}}}]}}`

func TestSeriesReadsDailyRowsAsLosAngelesDaysScaledAndCutAtTheLatestData(t *testing.T) {
	f := newFixture(t)
	f.credential()
	f.route("GET /v1beta1/apps/com.acme.app/crashRateMetricSet", 200, freshDaily)
	f.route("POST /v1beta1/apps/com.acme.app/crashRateMetricSet:query", 200, `{"rows":[
		{"aggregationPeriod":"DAILY","startTime":{"year":2026,"month":10,"day":15,"timeZone":{"id":"America/Los_Angeles"}},
		 "metrics":[{"metric":"crashRate","decimalValue":{"value":"0.0125"}}]},
		{"aggregationPeriod":"DAILY","startTime":{"year":2026,"month":10,"day":14,"timeZone":{"id":"America/Los_Angeles"}},
		 "metrics":[{"metric":"crashRate","decimalValue":{"value":"0.01"}}]}]}`)

	res, err := f.adapter.Series(context.Background(), source(nil), nil, domain.SeriesQuery{
		Query: `{"package":"com.acme.app","metric_set":"crashRateMetricSet","metric":"crashRate","as":"free_percent"}`,
		From:  f.now.Add(-72 * time.Hour), To: f.now, Step: time.Hour,
	})
	require.NoError(t, err)
	assert.Equal(t, 24*time.Hour, res.Step)
	require.Len(t, res.Series, 1)
	s := res.Series[0]
	assert.Equal(t, "crashRate", s.Name)
	require.Len(t, s.Points, 2)
	la, _ := time.LoadLocation("America/Los_Angeles")
	assert.Equal(t, time.Date(2026, 10, 14, 0, 0, 0, 0, la).UTC(), s.Points[0].Time)
	assert.InDelta(t, 99.0, *s.Points[0].Value, 1e-9)
	assert.InDelta(t, 98.75, *s.Points[1].Value, 1e-9)
	require.Len(t, res.Warnings, 1)
	assert.Contains(t, res.Warnings[0], "publishes vitals late")

	_, body := f.last()
	spec := body["timelineSpec"].(map[string]any)
	assert.Equal(t, "DAILY", spec["aggregationPeriod"])
	assert.Equal(t, map[string]any{"year": 2026.0, "month": 10.0, "day": 14.0, "timeZone": map[string]any{"id": "America/Los_Angeles"}}, spec["startTime"])
	assert.Equal(t, map[string]any{"year": 2026.0, "month": 10.0, "day": 16.0, "timeZone": map[string]any{"id": "America/Los_Angeles"}}, spec["endTime"],
		"the end is clamped to the freshness Play reports")
	assert.Equal(t, []any{"crashRate"}, body["metrics"])
	assert.Equal(t, []string{playvitals.ReportingScope}, f.mint.scopes)
}

func TestSeriesBreaksDownByADimensionAndFollowsThePageToken(t *testing.T) {
	f := newFixture(t)
	f.credential()
	f.route("GET /v1beta1/apps/com.acme.app/anrRateMetricSet", 200, freshDaily)
	page := 0
	f.routes["POST /v1beta1/apps/com.acme.app/anrRateMetricSet:query"] = func(w http.ResponseWriter, _ *http.Request) {
		page++
		row := func(day int, version, value string) string {
			return `{"startTime":{"year":2026,"month":10,"day":` + fmt.Sprint(day) + `,"timeZone":{"id":"America/Los_Angeles"}},
				"dimensions":[{"dimension":"versionCode","int64Value":"` + version + `"}],
				"metrics":[{"metric":"userPerceivedAnrRate","decimalValue":{"value":"` + value + `"}}]}`
		}
		if page == 1 {
			_, _ = fmt.Fprint(w, `{"rows":[`+row(14, "41", "0.002")+`],"nextPageToken":"p2"}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"rows":[`+row(14, "42", "0.004")+`,`+row(15, "42", "0.003")+`]}`)
	}

	res, err := f.adapter.Series(context.Background(), source(map[string]string{"package_name": "com.acme.app"}), nil, domain.SeriesQuery{
		Query: `{"metric_set":"anrRateMetricSet","metric":"userPerceivedAnrRate","dimension":"versionCode","as":"percent"}`,
		From:  f.now.Add(-72 * time.Hour), To: f.now.Add(-50 * time.Hour), Step: time.Hour,
	})
	require.NoError(t, err)
	require.Len(t, res.Series, 2)
	assert.Equal(t, map[string]string{"versionCode": "41"}, res.Series[0].Labels)
	assert.Equal(t, "42", res.Series[1].Name)
	require.Len(t, res.Series[1].Points, 2)
	assert.InDelta(t, 0.4, *res.Series[1].Points[0].Value, 1e-9)
	assert.Empty(t, res.Warnings, "a window ending before the latest data is not cut")
	_, body := f.last()
	assert.Equal(t, "p2", body["pageToken"])
	assert.Equal(t, []any{"versionCode"}, body["dimensions"])
}

func TestTheAccessTokenIsMintedOnceAndReusedUntilItNearlyExpires(t *testing.T) {
	f := newFixture(t)
	f.credential()
	f.route("GET /v1beta1/apps:search", 200, `{"apps":[{"packageName":"com.acme.app"}]}`)

	for range 3 {
		info, err := f.adapter.Verify(context.Background(), source(nil), nil)
		require.NoError(t, err)
		assert.Equal(t, "Google Play vitals · 1 app(s)", info.Detail)
	}
	assert.Equal(t, 1, f.mint.count())
	req, _ := f.last()
	assert.Equal(t, "Bearer ya29.token-1", req.Header.Get("Authorization"))

	f.now = f.now.Add(59*time.Minute + 30*time.Second)
	_, err := f.adapter.Verify(context.Background(), source(nil), nil)
	require.NoError(t, err)
	assert.Equal(t, 2, f.mint.count())
}

func TestRefusalsAreClassifiedAndNeverCarryTheToken(t *testing.T) {
	cases := []struct {
		name     string
		status   int
		body     string
		badQuery bool
		want     string
	}{
		{"a malformed query", 400, `{"error":{"code":400,"message":"Invalid metric ya29.token-1"}}`, true, "Invalid metric [redacted]"},
		{"a rate limit", 429, `{"error":{"code":429,"message":"Quota exceeded"}}`, false, "rate limited (HTTP 429)"},
		{"a missing permission", 403, `{"error":{"code":403,"message":"The caller does not have permission"}}`, false, "permission to view app information"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			f.credential()
			f.route("GET /v1beta1/apps/com.acme.app/crashRateMetricSet", 200, freshDaily)
			f.route("POST /v1beta1/apps/com.acme.app/crashRateMetricSet:query", tt.status, tt.body)
			_, err := f.adapter.Series(context.Background(), source(nil), nil, domain.SeriesQuery{
				Query: `{"package":"com.acme.app","metric_set":"crashRateMetricSet","metric":"crashRate"}`,
				From:  f.now.Add(-72 * time.Hour), To: f.now, Step: time.Hour,
			})
			require.Error(t, err)
			assert.Equal(t, tt.badQuery, errors.Is(err, port.ErrDataSourceQuery))
			assert.Contains(t, err.Error(), tt.want)
			assert.NotContains(t, err.Error(), "ya29.token-1")
		})
	}
}

func TestQueriesThatDoNotFitAreRefusedBeforeAnyRequest(t *testing.T) {
	f := newFixture(t)
	cases := []struct{ name, query, want string }{
		{"no package anywhere", `{"metric_set":"crashRateMetricSet","metric":"crashRate"}`, "names no package"},
		{"not a package name", `{"package":"not a package","metric_set":"crashRateMetricSet","metric":"crashRate"}`, "not an Android package name"},
		{"hourly slow starts", `{"package":"com.acme.app","metric_set":"slowStartRateMetricSet","metric":"slowStartRate","period":"hourly"}`, "daily data only"},
		{"an issues query", `{"package":"com.acme.app","issues":"crash"}`, "reads a table"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			_, err := f.adapter.Series(context.Background(), source(nil), nil, domain.SeriesQuery{Query: tt.query, From: f.now.Add(-time.Hour), To: f.now, Step: time.Hour})
			require.Error(t, err)
			assert.ErrorIs(t, err, port.ErrDataSourceQuery)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
	assert.Empty(t, f.requests)
}

func TestAMissingCredentialSaysWhereToAddIt(t *testing.T) {
	f := newFixture(t)
	f.creds.On("StoreCredential", mock.Anything, domain.StoreCredentialGooglePlay).Return(domain.StoreCredential{}, port.ErrNotFound)
	_, err := f.adapter.Verify(context.Background(), source(nil), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Settings → Integrations")
	assert.Zero(t, f.mint.count())
}

func TestTableListsTheTopErrorIssuesOfTheLastDays(t *testing.T) {
	f := newFixture(t)
	f.credential()
	page := 0
	f.routes["GET /v1beta1/apps/com.acme.app/errorIssues:search"] = func(w http.ResponseWriter, _ *http.Request) {
		page++
		if page == 1 {
			_, _ = fmt.Fprint(w, `{"errorIssues":[{"name":"apps/com.acme.app/errorIssues/abc","type":"CRASH","cause":"java.lang.NullPointerException",
				"location":"com.acme.Main.onCreate","errorReportCount":"120","distinctUsers":"40","lastErrorReportTime":"2026-10-16T08:00:00Z",
				"issueUri":"https://play.google.com/console/x","lastAppVersion":{"versionCode":"42"}}],"nextPageToken":"n2"}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"errorIssues":[{"name":"apps/com.acme.app/errorIssues/def","type":"APPLICATION_NOT_RESPONDING","cause":"Input dispatching timed out",
			"errorReportCount":"5","distinctUsers":"3"}]}`)
	}

	res, err := f.adapter.Table(context.Background(), source(nil), nil, domain.TableQuery{
		Query: `{"package":"com.acme.app","issues":"crash","days":2}`, At: f.now.Add(30 * time.Minute),
	})
	require.NoError(t, err)
	require.Len(t, res.Rows, 2)
	assert.Equal(t, map[string]any{
		"issue": "abc", "type": "crash", "cause": "java.lang.NullPointerException", "location": "com.acme.Main.onCreate",
		"users": 40.0, "reports": 120.0, "last_version": "42", "last_seen": "2026-10-16T08:00:00Z", "link": "https://play.google.com/console/x",
	}, res.Rows[0])
	assert.Equal(t, "anr", res.Rows[1]["type"])
	assert.False(t, res.Truncated)

	req, _ := f.last()
	q := req.URL.Query()
	assert.Equal(t, "n2", q.Get("pageToken"))
	assert.Equal(t, "errorIssueType = CRASH", q.Get("filter"))
	assert.Equal(t, "distinctUsers desc", q.Get("orderBy"))
	assert.Equal(t, "17", q.Get("interval.endTime.day"))
	assert.Equal(t, "9", q.Get("interval.endTime.hours"))
	assert.Equal(t, "15", q.Get("interval.startTime.day"))
	assert.Equal(t, "UTC", q.Get("interval.startTime.timeZone.id"))
}

func TestTheIssuesTableStopsAtItsPageCapAndSaysSo(t *testing.T) {
	f := newFixture(t)
	f.credential()
	pages := 0
	f.routes["GET /v1beta1/apps/com.acme.app/errorIssues:search"] = func(w http.ResponseWriter, _ *http.Request) {
		pages++
		_, _ = fmt.Fprint(w, `{"errorIssues":[],"nextPageToken":"again"}`)
	}

	res, err := f.adapter.Table(context.Background(), source(nil), nil, domain.TableQuery{
		Query: `{"package":"com.acme.app","issues":"all","days":2}`, At: f.now,
	})

	require.NoError(t, err)
	assert.True(t, res.Truncated)
	assert.Equal(t, 20, pages, "a server handing out page tokens forever is not read forever")
}

func TestAnIssueLinkIsKeptOnlyWhenItIsPlayConsolesOwnHTTPSPage(t *testing.T) {
	for uri, want := range map[string]string{
		"https://play.google.com/console/u/0/developers/1/app/2/vitals/crashes/abc": "https://play.google.com/console/u/0/developers/1/app/2/vitals/crashes/abc",
		"javascript:alert(1)":                      "",
		"http://play.google.com/console/x":         "",
		"https://play.google.com.evil.example/x":   "",
		"https://user@play.google.com/console/x":   "",
		"data:text/html,<script>alert(1)</script>": "",
	} {
		t.Run(uri, func(t *testing.T) {
			f := newFixture(t)
			f.credential()
			f.routes["GET /v1beta1/apps/com.acme.app/errorIssues:search"] = func(w http.ResponseWriter, _ *http.Request) {
				raw, _ := json.Marshal(map[string]any{"errorIssues": []map[string]string{{"name": "apps/com.acme.app/errorIssues/abc", "type": "CRASH", "issueUri": uri}}})
				_, _ = w.Write(raw)
			}
			res, err := f.adapter.Table(context.Background(), source(nil), nil, domain.TableQuery{
				Query: `{"package":"com.acme.app","issues":"all","days":2}`, At: f.now,
			})
			require.NoError(t, err)
			require.Len(t, res.Rows, 1)
			assert.Equal(t, want, res.Rows[0]["link"])
		})
	}
}
