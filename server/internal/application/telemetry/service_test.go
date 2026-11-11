package telemetry

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

func newTestService() (*Service, *fakeStore, *fakeClient) {
	store, client := &fakeStore{}, &fakeClient{}
	return NewService(Deps{Store: store, Client: client}), store, client
}

func ptr(s string) *string { return &s }

func TestSettingsConfiguredNeedsURLAndKey(t *testing.T) {
	svc, store, _ := newTestService()
	got, err := svc.Settings(context.Background())
	require.NoError(t, err)
	assert.Equal(t, Settings{}, got)

	store.baseURL = "http://t.example"
	got, _ = svc.Settings(context.Background())
	assert.False(t, got.Configured)
	assert.Equal(t, "http://t.example", got.BaseURL)

	store.apiKey = "k"
	got, _ = svc.Settings(context.Background())
	assert.True(t, got.Configured)
}

func TestUpdateKeepsReplacesAndClearsTheKey(t *testing.T) {
	svc, store, _ := newTestService()
	ctx := context.Background()

	got, err := svc.Update(ctx, "https://t.example/", ptr(" key-1 "))
	require.NoError(t, err)
	assert.Equal(t, Settings{BaseURL: "https://t.example", Configured: true}, got)
	assert.Equal(t, "key-1", store.apiKey)

	_, err = svc.Update(ctx, "https://t2.example", nil)
	require.NoError(t, err)
	assert.Equal(t, "key-1", store.apiKey, "an omitted key is kept")
	assert.Equal(t, "https://t2.example", store.baseURL)

	got, err = svc.Update(ctx, "https://t2.example", ptr(""))
	require.NoError(t, err)
	assert.Equal(t, "", store.apiKey, "an empty key clears")
	assert.False(t, got.Configured)
}

func TestUpdateRejectsABadURL(t *testing.T) {
	svc, _, _ := newTestService()
	for _, bad := range []string{"ftp://x", "t.example", "http://", "http://user:pw@t.example", "javascript:alert(1)"} {
		_, err := svc.Update(context.Background(), bad, nil)
		assert.ErrorIs(t, err, ErrInvalidURL, bad)
	}
}

func TestGetInjectsTheKeyAndNeverReturnsIt(t *testing.T) {
	svc, store, client := newTestService()
	store.baseURL, store.apiKey = "http://t.example", "secret-key"

	resp, err := svc.Proxy(context.Background(), "/services/orders-api/", "range=1h&env=prod")
	require.NoError(t, err)
	assert.Equal(t, 200, resp.Status)
	require.Len(t, client.calls, 1)
	assert.Equal(t, "secret-key", client.calls[0].apiKey)
	assert.Equal(t, "services/orders-api", client.calls[0].path)
	assert.Equal(t, "1h", client.calls[0].query.Get("range"))
}

func TestGetWhenNotConfigured(t *testing.T) {
	svc, store, client := newTestService()
	store.baseURL = "http://t.example"
	_, err := svc.Get(context.Background(), "services", nil)
	assert.ErrorIs(t, err, ErrNotConfigured)
	assert.Empty(t, client.calls)
}

func TestGetMapsUpstream401ToUnauthorized(t *testing.T) {
	svc, store, client := newTestService()
	store.baseURL, store.apiKey = "http://t.example", "k"
	client.respond = func(call) (port.TelemetryResponse, error) { return port.TelemetryResponse{Status: 401}, nil }
	_, err := svc.Get(context.Background(), "services", nil)
	assert.ErrorIs(t, err, ErrUnauthorized)
}

func TestGetPassesOtherStatusesThrough(t *testing.T) {
	svc, store, client := newTestService()
	store.baseURL, store.apiKey = "http://t.example", "k"
	client.respond = func(call) (port.TelemetryResponse, error) { return port.TelemetryResponse{Status: 404}, nil }
	resp, err := svc.Get(context.Background(), "incidents/9", nil)
	require.NoError(t, err)
	assert.Equal(t, 404, resp.Status)
}

func TestProxyRefusesPathsOutsideTheAllowlist(t *testing.T) {
	svc, store, client := newTestService()
	store.baseURL, store.apiKey = "http://t.example", "k"
	for _, p := range []string{
		"", "/", "auth/config", "me", "api-keys", "orgs/1/members", "integrations", "notification-channels",
		"assistant/threads", "internal/collector-config", "repos", "../auth/config", "services/../me",
		"services//x", "services/%2e%2e/me", "services/a\\b", "services/a?b", "./services",
	} {
		_, err := svc.Proxy(context.Background(), p, "")
		assert.ErrorIs(t, err, ErrPathNotAllowed, "path %q", p)
	}
	assert.Empty(t, client.calls)
}

func TestValidatePathAllowsTheScreenPaths(t *testing.T) {
	for _, p := range []string{
		"services", "services/orders-api/endpoints/GET-orders/rca", "overview/series", "traces/abc", "errors/ff00", "logs/patterns",
		"incidents", "incidents/7/events", "kpis/3/series", "infra/k8s/pods/uid-1", "loadtests/runs/2/series",
	} {
		got, err := ValidatePath(p)
		assert.NoError(t, err, p)
		assert.Equal(t, p, got)
	}
	got, err := ValidatePath("services/orders%20api/endpoints/get-x")
	assert.NoError(t, err)
	assert.Equal(t, "services/orders api/endpoints/get-x", got, "escapes are decoded once")
	for _, bad := range []string{"services/a%2Fb", "services/%2e%2e/me", "services/a%5Cb", "services/%00", "services/%zz"} {
		_, err = ValidatePath(bad)
		assert.ErrorIs(t, err, ErrPathNotAllowed, bad)
	}
}

func TestTestUsesTheFormValuesBeforeTheSavedOnes(t *testing.T) {
	svc, store, client := newTestService()
	store.baseURL, store.apiKey = "http://saved.example", "saved-key"

	res, err := svc.Test(context.Background(), TestInput{BaseURL: "http://typed.example/", APIKey: "typed-key"})
	require.NoError(t, err)
	assert.Equal(t, TestResult{OK: true}, res)
	require.Len(t, client.calls, 1)
	assert.Equal(t, "http://typed.example", client.calls[0].baseURL)
	assert.Equal(t, "typed-key", client.calls[0].apiKey)

	client.calls = nil
	res, _ = svc.Test(context.Background(), TestInput{})
	assert.True(t, res.OK)
	assert.Equal(t, "saved-key", client.calls[0].apiKey)
}

func TestTestReportsWhatIsWrong(t *testing.T) {
	svc, store, client := newTestService()

	res, _ := svc.Test(context.Background(), TestInput{})
	assert.False(t, res.OK)
	assert.Contains(t, res.Error, "URL is not set")

	store.baseURL = "http://t.example"
	res, _ = svc.Test(context.Background(), TestInput{})
	assert.False(t, res.OK)
	assert.Contains(t, res.Error, "key is not set")

	store.apiKey = "k"
	client.pingErr = fmt.Errorf("%w: connection refused", port.ErrTelemetryUnreachable)
	res, _ = svc.Test(context.Background(), TestInput{})
	assert.Equal(t, TestResult{Error: "connection refused"}, res)

	client.pingErr = nil
	client.respond = func(call) (port.TelemetryResponse, error) { return port.TelemetryResponse{Status: 401}, nil }
	res, _ = svc.Test(context.Background(), TestInput{})
	assert.Contains(t, res.Error, "rejected the key")

	res, _ = svc.Test(context.Background(), TestInput{BaseURL: "nope"})
	assert.Equal(t, ErrInvalidURL.Error(), res.Error)
}

func TestStatusIsCheapAndCached(t *testing.T) {
	svc, store, client := newTestService()
	now := time.Now()
	svc.now = func() time.Time { return now }

	got, err := svc.Status(context.Background())
	require.NoError(t, err)
	assert.Equal(t, Status{}, got)
	assert.Zero(t, client.pings, "an unconfigured install is never probed")

	store.baseURL, store.apiKey = "http://t.example", "k"
	got, _ = svc.Status(context.Background())
	assert.Equal(t, Status{Configured: true, Reachable: true}, got)
	_, _ = svc.Status(context.Background())
	assert.Equal(t, 1, client.pings, "second call inside the ttl is cached")

	now = now.Add(statusTTL + time.Second)
	client.pingErr = errors.New("down")
	got, _ = svc.Status(context.Background())
	assert.Equal(t, Status{Configured: true, Reachable: false}, got)
	assert.Equal(t, 2, client.pings)
}

func TestUpdateDropsTheCachedStatus(t *testing.T) {
	svc, store, client := newTestService()
	store.baseURL, store.apiKey = "http://t.example", "k"
	_, _ = svc.Status(context.Background())
	_, err := svc.Update(context.Background(), "http://t.example", nil)
	require.NoError(t, err)
	_, _ = svc.Status(context.Background())
	assert.Equal(t, 2, client.pings)
}

func TestNormalizeBaseURL(t *testing.T) {
	got, err := NormalizeBaseURL("  https://t.example/base/?x=1#f ")
	require.NoError(t, err)
	assert.Equal(t, "https://t.example/base", got)
	got, err = NormalizeBaseURL("")
	require.NoError(t, err)
	assert.Equal(t, "", got)
}

type scopeKey struct{}

func TestStatusCacheIsKeptPerScope(t *testing.T) {
	svc, store, client := newTestService()
	svc.SetScope(func(ctx context.Context) string {
		s, _ := ctx.Value(scopeKey{}).(string)
		return s
	})
	store.baseURL, store.apiKey = "http://t.example", "k"
	a := context.WithValue(context.Background(), scopeKey{}, "tenant-a")
	b := context.WithValue(context.Background(), scopeKey{}, "tenant-b")

	got, _ := svc.Status(a)
	assert.True(t, got.Reachable)
	client.pingErr = errors.New("down")
	got, _ = svc.Status(b)
	assert.False(t, got.Reachable, "tenant B is not answered from tenant A's probe")
	got, _ = svc.Status(a)
	assert.True(t, got.Reachable, "tenant A keeps its own cached answer")
	assert.Equal(t, 2, client.pings)
}
