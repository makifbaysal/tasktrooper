package appstoremetrics_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/datasource/appstoremetrics"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/makifbaysal/tasktrooper/server/internal/port/mocks"
)

var ascCredential = domain.StoreCredential{Provider: domain.StoreCredentialASC, Data: map[string]string{
	"key_id": "KEY123", "issuer_id": "issuer-1", "p8": "-----BEGIN PRIVATE KEY-----\nAAAA\n-----END PRIVATE KEY-----",
}}

type fixture struct {
	srv      *httptest.Server
	now      time.Time
	mints    int
	creds    *mocks.StoreCredentialReader
	adapter  *appstoremetrics.Adapter
	mu       sync.Mutex
	requests []*http.Request
	routes   map[string]http.HandlerFunc
}

func newFixture(t *testing.T) *fixture {
	f := &fixture{now: time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC), routes: map[string]http.HandlerFunc{}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.requests = append(f.requests, r)
		h, ok := f.routes[r.URL.Path]
		f.mu.Unlock()
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = fmt.Fprint(w, `{"errors":[{"status":"404","code":"NOT_FOUND","title":"The specified resource does not exist"}]}`)
			return
		}
		h(w, r)
	}))
	t.Cleanup(f.srv.Close)
	f.creds = mocks.NewStoreCredentialReader(t)
	f.adapter = appstoremetrics.New()
	f.adapter.SetURLPolicy(urlguard.HumanConfigured())
	f.adapter.SetStoreCredentials(f.creds)
	f.adapter.SetBaseURL(f.srv.URL)
	f.adapter.SetClock(func() time.Time { return f.now })
	f.adapter.SetMinter(func(cred domain.StoreCredential) (string, time.Time, error) {
		if cred.Data["key_id"] != "KEY123" {
			return "", time.Time{}, errors.New("wrong key")
		}
		f.mints++
		return fmt.Sprintf("jwt-%d", f.mints), f.now.Add(15 * time.Minute), nil
	})
	return f
}

func (f *fixture) credential() {
	f.creds.On("StoreCredential", mock.Anything, domain.StoreCredentialASC).Return(ascCredential, nil)
}

func (f *fixture) route(path string, status int, body string) {
	f.routes[path] = func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = fmt.Fprint(w, body)
	}
}

func (f *fixture) last() *http.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests[len(f.requests)-1]
}

func source(config map[string]string) domain.DataSource {
	c := map[string]string{"store_credential": "asc"}
	for k, v := range config {
		c[k] = v
	}
	return domain.DataSource{Kind: domain.DataSourceKindAppStoreMetrics, Config: c}
}

const launchMetrics = `{"productData":[{"platform":"iOS","metricCategories":[{"identifier":"LAUNCH","metrics":[{"identifier":"launchTime",
	"unit":{"identifier":"milliseconds"},"datasets":[
	{"filterCriteria":{"device":"all_iphones","percentile":"percentile.fifty"},"points":[{"version":"1.0","value":700},{"version":"1.1","value":650},{"version":"0.9","value":900}]},
	{"filterCriteria":{"device":"all_iphones","percentile":"percentile.ninety"},"points":[{"version":"1.0","value":1500},{"version":"1.1","value":1300}]}]}]}]}]}`

const versions = `{"data":[
	{"attributes":{"versionString":"1.0","createdDate":"2026-10-10T12:00:00.000-0700"}},
	{"attributes":{"versionString":"1.1","createdDate":"2026-10-15T12:00:00Z"}}]}`

func TestSeriesIsAStaircaseOfTheNewestVersionsValueAtEachStep(t *testing.T) {
	f := newFixture(t)
	f.credential()
	f.route("/v1/apps/1234567890/perfPowerMetrics", 200, launchMetrics)
	f.route("/v1/apps/1234567890/appStoreVersions", 200, versions)

	from := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	res, err := f.adapter.Series(context.Background(), source(nil), nil, domain.SeriesQuery{
		Query: `{"app":"1234567890","metric":"launchTime","percentile":"p90"}`,
		From:  from, To: from.Add(8 * 24 * time.Hour), Step: 24 * time.Hour,
	})
	require.NoError(t, err)
	require.Len(t, res.Series, 1)
	s := res.Series[0]
	assert.Equal(t, "p90", s.Name)
	assert.Equal(t, map[string]string{"metric": "launchTime", "device": "all_iphones", "quantile": "p90", "unit": "milliseconds"}, s.Labels)
	var got []float64
	for _, p := range s.Points {
		got = append(got, *p.Value)
	}
	assert.Equal(t, []float64{1500, 1500, 1500, 1500, 1500, 1300, 1300}, got, "10-11 → 1.0 (created 10-10 19:00 UTC), 10-16 on → 1.1")
	assert.Equal(t, time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC), s.Points[0].Time)
	assert.Contains(t, res.Warnings[0], "per app version")

	req := f.last()
	assert.Equal(t, "/v1/apps/1234567890/appStoreVersions", req.URL.Path)
	assert.Equal(t, "Bearer jwt-1", req.Header.Get("Authorization"))
	assert.Contains(t, req.Header.Get("Accept"), "application/vnd.apple.xcode-metrics+json")
	assert.Equal(t, 1, f.mints, "one token signs every request of the read")
}

func TestSeriesAnswersBothPercentilesAndWarnsAboutVersionsWithoutARecord(t *testing.T) {
	f := newFixture(t)
	f.credential()
	f.routes["/v1/apps/1234567890/perfPowerMetrics"] = func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "LAUNCH", r.URL.Query().Get("filter[metricType]"))
		assert.Equal(t, "IOS", r.URL.Query().Get("filter[platform]"))
		assert.Equal(t, "all_iphones", r.URL.Query().Get("filter[deviceType]"))
		_, _ = fmt.Fprint(w, launchMetrics)
	}
	f.route("/v1/apps/1234567890/appStoreVersions", 200, versions)
	from := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	res, err := f.adapter.Series(context.Background(), source(map[string]string{"app_id": "1234567890"}), nil, domain.SeriesQuery{
		Query: `{"metric":"launchTime"}`, From: from, To: from.Add(time.Hour), Step: time.Hour,
	})
	require.NoError(t, err)
	require.Len(t, res.Series, 2)
	assert.Equal(t, "p50", res.Series[0].Name)
	assert.Equal(t, 650.0, *res.Series[0].Points[0].Value)
	assert.Equal(t, "p90", res.Series[1].Name)
	require.Len(t, res.Warnings, 2)
	assert.Contains(t, res.Warnings[1], "0.9")
}

func TestAnAppWithoutMetricsYetAnswersNoSeries(t *testing.T) {
	f := newFixture(t)
	f.credential()
	res, err := f.adapter.Series(context.Background(), source(nil), nil, domain.SeriesQuery{
		Query: `{"app":"1234567890","metric":"hang"}`, From: f.now.Add(-time.Hour), To: f.now, Step: time.Minute,
	})
	require.NoError(t, err)
	assert.Empty(t, res.Series)
	assert.Contains(t, res.Warnings[len(res.Warnings)-1], "no power and performance metrics")
	assert.Equal(t, "HANG", f.last().URL.Query().Get("filter[metricType]"))
}

func TestTheTokenIsReusedUntilShortlyBeforeItExpires(t *testing.T) {
	f := newFixture(t)
	f.credential()
	f.route("/v1/apps", 200, `{"data":[{"id":"1"},{"id":"2"}],"links":{"next":"https://example/next"}}`)
	for range 2 {
		info, err := f.adapter.Verify(context.Background(), source(nil), nil)
		require.NoError(t, err)
		assert.Equal(t, "App Store Connect · 2+ app(s)", info.Detail)
	}
	assert.Equal(t, 1, f.mints)
	f.now = f.now.Add(14*time.Minute + 30*time.Second)
	_, err := f.adapter.Verify(context.Background(), source(nil), nil)
	require.NoError(t, err)
	assert.Equal(t, 2, f.mints)
}

func TestRefusalsAreClassifiedAndMasked(t *testing.T) {
	cases := []struct {
		name     string
		status   int
		badQuery bool
		want     string
	}{
		{"a bad parameter", 400, true, "HTTP 400: bad [redacted] value [redacted]"},
		{"a rate limit", 429, false, "rate limited (HTTP 429)"},
		{"a forbidden role", 403, false, "authentication failed (HTTP 403)"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			f.credential()
			f.route("/v1/apps/1234567890/perfPowerMetrics", tt.status, `{"errors":[{"title":"t","detail":"bad jwt-1 value jwt-1"}]}`)
			_, err := f.adapter.Series(context.Background(), source(nil), nil, domain.SeriesQuery{
				Query: `{"app":"1234567890","metric":"launchTime"}`, From: f.now.Add(-time.Hour), To: f.now, Step: time.Minute,
			})
			require.Error(t, err)
			assert.Equal(t, tt.badQuery, errors.Is(err, port.ErrDataSourceQuery))
			assert.Contains(t, err.Error(), tt.want)
			assert.NotContains(t, err.Error(), "jwt-1")
		})
	}
}

func TestTableListsTheNewestBuildsDiagnosticSignaturesFollowingOnlyTheAPIsOwnNextLinks(t *testing.T) {
	f := newFixture(t)
	f.credential()
	f.routes["/v1/builds"] = func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "1234567890", r.URL.Query().Get("filter[app]"))
		assert.Equal(t, "-uploadedDate", r.URL.Query().Get("sort"))
		assert.Equal(t, "2.0", r.URL.Query().Get("filter[preReleaseVersion.version]"))
		_, _ = fmt.Fprint(w, `{"data":[{"id":"build-1","attributes":{"version":"57"}}]}`)
	}
	page := 0
	f.routes["/v1/builds/build-1/diagnosticSignatures"] = func(w http.ResponseWriter, r *http.Request) {
		page++
		assert.Equal(t, "HANGS", r.URL.Query().Get("filter[diagnosticType]"))
		if page == 1 {
			_, _ = fmt.Fprintf(w, `{"data":[{"id":"sig-a","attributes":{"diagnosticType":"HANGS","signature":"Main thread: -[Feed reload]","weight":0.85}}],
				"links":{"next":"%s/v1/builds/build-1/diagnosticSignatures?cursor=c2&filter%%5BdiagnosticType%%5D=HANGS"}}`, f.srv.URL)
			return
		}
		assert.Equal(t, "c2", r.URL.Query().Get("cursor"))
		_, _ = fmt.Fprint(w, `{"data":[{"id":"sig-b","attributes":{"diagnosticType":"HANGS","signature":"x","weight":0.1}}],
			"links":{"next":"https://elsewhere.example/v1/steal"}}`)
	}

	res, err := f.adapter.Table(context.Background(), source(nil), nil, domain.TableQuery{
		Query: `{"app":"1234567890","diagnostics":"hangs","version":"2.0"}`, At: f.now,
	})
	require.NoError(t, err)
	require.Len(t, res.Rows, 2)
	assert.Equal(t, map[string]any{"issue": "sig-a", "type": "hangs", "cause": "Main thread: -[Feed reload]", "weight": 85.0, "build": "57"}, res.Rows[0])
	assert.Equal(t, 2, page)
	assert.Equal(t, []string{"issue", "type", "cause", "weight", "build"}, columnKeys(res.Columns))
}

func columnKeys(cols []domain.TableColumn) []string {
	out := make([]string, 0, len(cols))
	for _, c := range cols {
		out = append(out, c.Key)
	}
	return out
}

func TestQueriesThatDoNotFitAreRefusedBeforeAnyRequest(t *testing.T) {
	f := newFixture(t)
	for _, q := range []string{
		`{"metric":"launchTime"}`,
		`{"app":"com.acme.app","metric":"launchTime"}`,
		`{"app":"1234567890","metric":"launchTime","percentile":"p95"}`,
		`{"app":"1234567890","diagnostics":"crashes"}`,
	} {
		_, err := f.adapter.Series(context.Background(), source(nil), nil, domain.SeriesQuery{Query: q, From: f.now.Add(-time.Hour), To: f.now, Step: time.Minute})
		assert.ErrorIs(t, err, port.ErrDataSourceQuery, q)
	}
	assert.Empty(t, f.requests)
}
