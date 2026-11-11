package telemetry

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

func testClient() *Client { return New(urlguard.HumanConfigured()) }

func TestGetSendsBearerKeyAndPathUnderAPIRoot(t *testing.T) {
	var gotPath, gotQuery, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery, gotAuth = r.URL.Path, r.URL.RawQuery, r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"name":"orders-api"}]`))
	}))
	defer srv.Close()

	resp, err := testClient().Get(context.Background(), srv.URL+"/", "tt_key_1", "services", url.Values{"range": {"1h"}})
	require.NoError(t, err)
	assert.Equal(t, 200, resp.Status)
	assert.Equal(t, "/api/v1/services", gotPath)
	assert.Equal(t, "range=1h", gotQuery)
	assert.Equal(t, "Bearer tt_key_1", gotAuth)
	assert.JSONEq(t, `[{"name":"orders-api"}]`, string(resp.Body))
}

func TestGetKeepsABasePathPrefix(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { gotPath = r.URL.Path }))
	defer srv.Close()

	_, err := testClient().Get(context.Background(), srv.URL+"/telemetry", "k", "incidents", nil)
	require.NoError(t, err)
	assert.Equal(t, "/telemetry/api/v1/incidents", gotPath)
}

func TestGetPassesUpstreamStatusThrough(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"bad key"}`))
	}))
	defer srv.Close()

	resp, err := testClient().Get(context.Background(), srv.URL, "wrong", "services", nil)
	require.NoError(t, err)
	assert.Equal(t, 401, resp.Status)
}

func TestGetRefusesADestinationThePolicyForbids(t *testing.T) {
	c := New(urlguard.PublicOnly())
	_, err := c.Get(context.Background(), "http://169.254.169.254", "k", "services", nil)
	require.Error(t, err)
	assert.True(t, errors.Is(err, port.ErrTelemetryUnreachable))
}

func TestGetOnADeadHostIsUnreachableWithoutLeakingTheKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()

	_, err := testClient().Get(context.Background(), url, "super-secret-key", "services", nil)
	require.Error(t, err)
	assert.True(t, errors.Is(err, port.ErrTelemetryUnreachable))
	assert.NotContains(t, err.Error(), "super-secret-key")
}

func TestPingUsesTheHealthEndpointWithoutAKey(t *testing.T) {
	var gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
	}))
	defer srv.Close()

	require.NoError(t, testClient().Ping(context.Background(), srv.URL))
	assert.Equal(t, "/healthz", gotPath)
	assert.Empty(t, gotAuth)
}

func TestPingFailsOnAServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer srv.Close()
	assert.Error(t, testClient().Ping(context.Background(), srv.URL))
}
