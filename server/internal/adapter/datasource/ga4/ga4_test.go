package ga4_test

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

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/datasource/ga4"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/makifbaysal/tasktrooper/server/internal/port/mocks"
)

const (
	keyJSON = `{"type":"service_account","private_key":"-----BEGIN PRIVATE KEY-----secret"}`
	token   = "ya29.access-token-value"
)

type fixture struct {
	srv     *httptest.Server
	mu      sync.Mutex
	bodies  []map[string]any
	auth    []string
	scopes  []string
	answer  func(body map[string]any) (int, string)
	adapter *ga4.Adapter
	account uuid.UUID
	now     time.Time
}

func newFixture(t *testing.T) *fixture {
	f := &fixture{account: uuid.New(), now: time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1beta/properties/123456:runReport" {
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
	accounts := mocks.NewCloudAccountReader(t)
	accounts.On("CloudCredential", mock.Anything, f.account).Return(domain.CloudCredential{
		AccountID: f.account, Provider: domain.CloudGCP, Fields: map[string]string{"service_account_json": keyJSON},
	}, nil).Maybe()
	f.adapter = ga4.New()
	f.adapter.SetURLPolicy(urlguard.HumanConfigured())
	f.adapter.SetCloudAccounts(accounts)
	f.adapter.SetBaseURL(f.srv.URL)
	f.adapter.SetClock(func() time.Time { return f.now })
	f.adapter.SetMinter(func(_ context.Context, _ *http.Client, sa, scope string) (string, time.Time, error) {
		assert.Equal(t, keyJSON, sa)
		f.scopes = append(f.scopes, scope)
		return token, f.now.Add(time.Hour), nil
	})
	return f
}

func (f *fixture) source() domain.DataSource {
	return domain.DataSource{Kind: domain.DataSourceKindGA4, Config: map[string]string{
		"property_id": "123456", "auth": "cloud_account", "cloud_account_id": f.account.String(),
	}}
}

func (f *fixture) last() map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bodies[len(f.bodies)-1]
}

func TestVerifyMintsAReadonlyTokenOnceAndReadsTheProperty(t *testing.T) {
	f := newFixture(t)
	f.answer = func(map[string]any) (int, string) {
		return 200, `{"rows": [{"metricValues": [{"value": "812"}]}], "metadata": {"timeZone": "Europe/Istanbul"}}`
	}
	for range 2 {
		info, err := f.adapter.Verify(context.Background(), f.source(), nil)
		require.NoError(t, err)
		assert.Equal(t, "Google Analytics · property 123456 · 812 active users in the last 7 days · Europe/Istanbul", info.Detail)
	}
	assert.Equal(t, []string{ga4.ReadonlyScope}, f.scopes, "the token is reused while it is valid")
	assert.Equal(t, "Bearer "+token, f.auth[1])
}

func TestAPastedServiceAccountKeyIsUsedWithoutACloudAccount(t *testing.T) {
	f := newFixture(t)
	f.answer = func(map[string]any) (int, string) { return 200, `{"rows": []}` }
	ds := domain.DataSource{Kind: domain.DataSourceKindGA4, Config: map[string]string{"property_id": "123456", "auth": "service_account"}}
	_, err := f.adapter.Verify(context.Background(), ds, map[string]string{"service_account_json": keyJSON})
	require.NoError(t, err)
	_, err = f.adapter.Verify(context.Background(), ds, map[string]string{})
	assert.ErrorContains(t, err, "no service account key")
}

func TestSeriesReadsDaysOfThePropertysZoneAsPerSecondPageViews(t *testing.T) {
	f := newFixture(t)
	f.now = time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	f.answer = func(map[string]any) (int, string) {
		return 200, `{"rows": [
			{"dimensionValues": [{"value": "20261014"}], "metricValues": [{"value": "86400"}]},
			{"dimensionValues": [{"value": "20261015"}], "metricValues": [{"value": "172800"}]}
		], "metadata": {"timeZone": "Europe/Istanbul"}}`
	}
	ist, _ := time.LoadLocation("Europe/Istanbul")
	from := time.Date(2026, 10, 14, 0, 0, 0, 0, ist)
	res, err := f.adapter.Series(context.Background(), f.source(), nil, domain.SeriesQuery{
		Query: `{"metric": "page_views", "host": "www.example.com"}`, From: from, To: from.AddDate(0, 0, 3), Step: 24 * time.Hour,
	})
	require.NoError(t, err)
	assert.Equal(t, 24*time.Hour, res.Step)
	require.Len(t, res.Series, 1)
	pts := res.Series[0].Points
	require.Len(t, pts, 3)
	assert.Equal(t, from.UTC(), pts[0].Time, "a day starts at midnight in the property's zone")
	assert.Equal(t, 1.0, *pts[0].Value)
	assert.Equal(t, 2.0, *pts[1].Value)
	assert.Equal(t, 0.0, *pts[2].Value, "a day with no row is zero")
	assert.Empty(t, res.Warnings, "an old window carries no processing warning")
	body := f.last()
	assert.Equal(t, []any{map[string]any{"name": "date"}}, body["dimensions"])
	assert.Equal(t, []any{map[string]any{"name": "screenPageViews"}}, body["metrics"])
	assert.Equal(t, "hostName", body["dimensionFilter"].(map[string]any)["filter"].(map[string]any)["fieldName"])
}

func TestSeriesBelowADayIsHourlyAndARecentWindowIsWarnedAbout(t *testing.T) {
	f := newFixture(t)
	f.answer = func(map[string]any) (int, string) {
		return 200, `{"rows": [{"dimensionValues": [{"value": "2026101610"}], "metricValues": [{"value": "40"}]}],
			"metadata": {"timeZone": "UTC", "subjectToThresholding": true}}`
	}
	from := time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC)
	res, err := f.adapter.Series(context.Background(), f.source(), nil, domain.SeriesQuery{
		Query: `{"metric": "sessions"}`, From: from, To: from.Add(2 * time.Hour), Step: time.Hour,
	})
	require.NoError(t, err)
	assert.Equal(t, time.Hour, res.Step)
	pts := res.Series[0].Points
	require.Len(t, pts, 2)
	assert.Equal(t, 40.0, *pts[0].Value, "sessions are a count, not a rate")
	assert.Contains(t, res.Warnings, "Google Analytics processes data for up to 48 hours; the newest points may still rise")
	assert.Contains(t, res.Warnings, "Google Analytics withheld rows below its privacy thresholds")
	assert.Equal(t, []any{map[string]any{"name": "dateHour"}}, f.last()["dimensions"])
}

func TestTableReadsTheDevicesOfTheLastDaysMostViewedFirst(t *testing.T) {
	f := newFixture(t)
	f.answer = func(map[string]any) (int, string) {
		return 200, `{"rows": [
			{"dimensionValues": [{"value": "mobile"}, {"value": "Safari"}, {"value": "iOS"}], "metricValues": [{"value": "10"}, {"value": "4"}]},
			{"dimensionValues": [{"value": "desktop"}, {"value": "Chrome"}, {"value": "Windows"}], "metricValues": [{"value": "90"}, {"value": "30"}]}
		], "rowCount": 7}`
	}
	res, err := f.adapter.Table(context.Background(), f.source(), nil, domain.TableQuery{Query: `{"table": "devices", "days": 3}`, At: f.now})
	require.NoError(t, err)
	assert.Equal(t, ga4.TableColumns(domain.RecipeTableDevices), res.Columns)
	assert.Equal(t, []map[string]any{
		{"device": "desktop", "browser": "Chrome", "os": "Windows", "views": 90.0, "visitors": 30.0},
		{"device": "mobile", "browser": "Safari", "os": "iOS", "views": 10.0, "visitors": 4.0},
	}, res.Rows)
	assert.True(t, res.Truncated)
	body := f.last()
	assert.Equal(t, []any{map[string]any{"startDate": "2daysAgo", "endDate": "today"}}, body["dateRanges"])
	assert.Equal(t, "100", body["limit"])
}

func TestDiscoverListsEventNames(t *testing.T) {
	f := newFixture(t)
	f.answer = func(map[string]any) (int, string) {
		return 200, `{"rows": [{"dimensionValues": [{"value": "page_view"}]}, {"dimensionValues": [{"value": "session_start"}]}], "rowCount": 2}`
	}
	inv, err := f.adapter.Discover(context.Background(), f.source(), nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"page_view", "session_start"}, inv.Metrics)
	assert.False(t, inv.Truncated)
}

func TestErrorsMaskTheTokenAndTellARateLimitFromABadQuery(t *testing.T) {
	f := newFixture(t)
	f.answer = func(map[string]any) (int, string) {
		return 400, `{"error": {"code": 400, "message": "Field fooBar is not a valid dimension; token ` + token + `", "status": "INVALID_ARGUMENT"}}`
	}
	_, err := f.adapter.Discover(context.Background(), f.source(), nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, port.ErrDataSourceQuery)
	assert.NotContains(t, err.Error(), token)
	assert.Contains(t, err.Error(), "[redacted]")

	f.answer = func(map[string]any) (int, string) {
		return 429, `{"error": {"code": 429, "message": "Exhausted property tokens", "status": "RESOURCE_EXHAUSTED"}}`
	}
	_, err = f.adapter.Discover(context.Background(), f.source(), nil)
	require.Error(t, err)
	assert.NotErrorIs(t, err, port.ErrDataSourceQuery)
	assert.Contains(t, err.Error(), "rate limited (HTTP 429)")

	f.answer = func(map[string]any) (int, string) {
		return 403, `{"error": {"code": 403, "message": "User does not have sufficient permissions for this property."}}`
	}
	_, err = f.adapter.Verify(context.Background(), f.source(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "authentication failed (HTTP 403)")
	assert.Contains(t, err.Error(), "Viewer")
}

func TestAMeasurementIDIsNotAProperty(t *testing.T) {
	f := newFixture(t)
	ds := f.source()
	ds.Config["property_id"] = "G-ABC123"
	_, err := f.adapter.Verify(context.Background(), ds, nil)
	assert.ErrorContains(t, err, "is not a Google Analytics 4 property id")
}
