package http

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	telemetryadapter "github.com/makifbaysal/tasktrooper/server/internal/adapter/telemetry"
	telemetryapp "github.com/makifbaysal/tasktrooper/server/internal/application/telemetry"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
)

type memTelemetryStore struct {
	mu              sync.Mutex
	baseURL, apiKey string
}

func (m *memTelemetryStore) TelemetrySettings(context.Context) (string, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.baseURL, m.apiKey, nil
}

func (m *memTelemetryStore) SetTelemetrySettings(_ context.Context, baseURL string, apiKey *string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.baseURL = baseURL
	if apiKey != nil {
		m.apiKey = *apiKey
	}
	return nil
}

type telemetryFixture struct {
	app      *fiber.App
	store    *memTelemetryStore
	upstream *httptest.Server
	seen     chan *http.Request
}

func newTelemetryFixture(t *testing.T, upstream http.HandlerFunc) *telemetryFixture {
	t.Helper()
	f := &telemetryFixture{store: &memTelemetryStore{}, seen: make(chan *http.Request, 16)}
	f.upstream = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case f.seen <- r.Clone(context.Background()):
		default:
		}
		upstream(w, r)
	}))
	t.Cleanup(f.upstream.Close)
	svc := telemetryapp.NewService(telemetryapp.Deps{Store: f.store, Client: telemetryadapter.New(urlguard.HumanConfigured())})
	h := &Handler{telemetrySvc: svc}
	f.app = fiber.New()
	h.registerTelemetryRoutes(f.app)
	return f
}

func (f *telemetryFixture) do(t *testing.T, method, path string, body any) (int, []byte, http.Header) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		reader = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := f.app.Test(req, 5000)
	require.NoError(t, err)
	out, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, out, resp.Header
}

func jsonOK(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(body))
}

func TestTelemetrySettingsNeverReturnTheKey(t *testing.T) {
	f := newTelemetryFixture(t, func(w http.ResponseWriter, r *http.Request) { jsonOK(w, `[]`) })

	status, body, _ := f.do(t, "GET", "/v1/telemetry/settings", nil)
	assert.Equal(t, 200, status)
	assert.JSONEq(t, `{"base_url":"","configured":false}`, string(body))

	status, body, _ = f.do(t, "PUT", "/v1/telemetry/settings", map[string]any{"base_url": f.upstream.URL + "/", "api_key": "tt_secret"})
	assert.Equal(t, 200, status)
	assert.JSONEq(t, `{"base_url":"`+f.upstream.URL+`","configured":true}`, string(body))
	assert.NotContains(t, string(body), "tt_secret")

	status, body, _ = f.do(t, "GET", "/v1/telemetry/settings", nil)
	assert.Equal(t, 200, status)
	assert.NotContains(t, string(body), "tt_secret")

	status, _, _ = f.do(t, "PUT", "/v1/telemetry/settings", map[string]any{"base_url": f.upstream.URL})
	assert.Equal(t, 200, status)
	assert.Equal(t, "tt_secret", f.store.apiKey, "an omitted key is kept")

	_, body, _ = f.do(t, "PUT", "/v1/telemetry/settings", map[string]any{"base_url": f.upstream.URL, "api_key": ""})
	assert.JSONEq(t, `{"base_url":"`+f.upstream.URL+`","configured":false}`, string(body))
	assert.Equal(t, "", f.store.apiKey, "an empty key clears")
}

func TestTelemetrySettingsRejectABadURL(t *testing.T) {
	f := newTelemetryFixture(t, func(w http.ResponseWriter, r *http.Request) {})
	status, _, _ := f.do(t, "PUT", "/v1/telemetry/settings", map[string]any{"base_url": "ftp://nope"})
	assert.Equal(t, 400, status)
}

func TestTelemetryTestEndpoint(t *testing.T) {
	f := newTelemetryFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			jsonOK(w, `{"ok":true}`)
			return
		}
		if r.Header.Get("Authorization") != "Bearer good" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		jsonOK(w, `["prod"]`)
	})

	status, body, _ := f.do(t, "POST", "/v1/telemetry/settings/test", nil)
	assert.Equal(t, 200, status)
	assert.Contains(t, string(body), `"ok":false`)

	status, body, _ = f.do(t, "POST", "/v1/telemetry/settings/test", map[string]any{"base_url": f.upstream.URL, "api_key": "bad"})
	assert.Equal(t, 200, status)
	assert.Contains(t, string(body), `"ok":false`)
	assert.Contains(t, string(body), "rejected")

	status, body, _ = f.do(t, "POST", "/v1/telemetry/settings/test", map[string]any{"base_url": f.upstream.URL, "api_key": "good"})
	assert.Equal(t, 200, status)
	assert.JSONEq(t, `{"ok":true}`, string(body))
}

func TestTelemetryStatus(t *testing.T) {
	f := newTelemetryFixture(t, func(w http.ResponseWriter, r *http.Request) { jsonOK(w, `{}`) })
	_, body, _ := f.do(t, "GET", "/v1/telemetry/status", nil)
	assert.JSONEq(t, `{"configured":false,"reachable":false}`, string(body))

	f.store.baseURL, f.store.apiKey = f.upstream.URL, "k"
	_, body, _ = f.do(t, "GET", "/v1/telemetry/status", nil)
	assert.JSONEq(t, `{"configured":true,"reachable":true}`, string(body))
}

func TestTelemetryProxyIsConflictWhenNotConfigured(t *testing.T) {
	f := newTelemetryFixture(t, func(w http.ResponseWriter, r *http.Request) { jsonOK(w, `[]`) })
	status, body, _ := f.do(t, "GET", "/v1/telemetry/proxy/services", nil)
	assert.Equal(t, 409, status)
	assert.Contains(t, string(body), `"code":"telemetry_not_configured"`)
	assert.Empty(t, f.seen)
}

func TestTelemetryProxyForwardsWithTheKeyInjected(t *testing.T) {
	f := newTelemetryFixture(t, func(w http.ResponseWriter, r *http.Request) { jsonOK(w, `[{"name":"orders-api"}]`) })
	f.store.baseURL, f.store.apiKey = f.upstream.URL, "tt_secret"

	status, body, hdr := f.do(t, "GET", "/v1/telemetry/proxy/services?range=6h&env=prod", nil)
	assert.Equal(t, 200, status)
	assert.JSONEq(t, `[{"name":"orders-api"}]`, string(body))
	assert.Contains(t, hdr.Get("Content-Type"), "application/json")

	r := <-f.seen
	assert.Equal(t, "/api/v1/services", r.URL.Path)
	assert.Equal(t, "6h", r.URL.Query().Get("range"))
	assert.Equal(t, "prod", r.URL.Query().Get("env"))
	assert.Equal(t, "Bearer tt_secret", r.Header.Get("Authorization"))
}

func TestTelemetryProxyIgnoresACallerSuppliedAuthorization(t *testing.T) {
	f := newTelemetryFixture(t, func(w http.ResponseWriter, r *http.Request) { jsonOK(w, `[]`) })
	f.store.baseURL, f.store.apiKey = f.upstream.URL, "tt_secret"

	req := httptest.NewRequest("GET", "/v1/telemetry/proxy/errors", nil)
	req.Header.Set("Authorization", "Bearer attacker")
	req.Header.Set("Cookie", "tt_session=stolen")
	resp, err := f.app.Test(req, 5000)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	r := <-f.seen
	assert.Equal(t, "Bearer tt_secret", r.Header.Get("Authorization"))
	assert.Empty(t, r.Header.Get("Cookie"))
}

func TestTelemetryProxyDecodesEscapedSegmentsOnce(t *testing.T) {
	f := newTelemetryFixture(t, func(w http.ResponseWriter, r *http.Request) { jsonOK(w, `{}`) })
	f.store.baseURL, f.store.apiKey = f.upstream.URL, "k"

	status, _, _ := f.do(t, "GET", "/v1/telemetry/proxy/services/orders%20api/endpoints/get-orders/rca", nil)
	assert.Equal(t, 200, status)
	r := <-f.seen
	assert.Equal(t, "/api/v1/services/orders api/endpoints/get-orders/rca", r.URL.Path)
}

func TestTelemetryProxyRefusesPathsOutsideTheAllowlist(t *testing.T) {
	f := newTelemetryFixture(t, func(w http.ResponseWriter, r *http.Request) { jsonOK(w, `{}`) })
	f.store.baseURL, f.store.apiKey = f.upstream.URL, "k"

	for _, p := range []string{"auth/config", "me", "api-keys", "integrations", "notification-channels", "internal/collector-config", "services/%2e%2e/me", "services/a%2Fb"} {
		status, body, _ := f.do(t, "GET", "/v1/telemetry/proxy/"+p, nil)
		assert.Equal(t, 403, status, p)
		assert.Contains(t, string(body), "telemetry_path_not_allowed", p)
	}
	assert.Empty(t, f.seen, "nothing forbidden reached the upstream")
}

func TestTelemetryProxyIsReadOnly(t *testing.T) {
	f := newTelemetryFixture(t, func(w http.ResponseWriter, r *http.Request) { jsonOK(w, `{}`) })
	f.store.baseURL, f.store.apiKey = f.upstream.URL, "k"
	for _, m := range []string{"POST", "PUT", "DELETE", "PATCH"} {
		status, _, _ := f.do(t, m, "/v1/telemetry/proxy/incidents/1/resolve", nil)
		assert.Equal(t, 405, status, m)
	}
	assert.Empty(t, f.seen)
}

func TestTelemetryProxyPassesUpstreamErrorsThrough(t *testing.T) {
	f := newTelemetryFixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"no such incident"}`))
	})
	f.store.baseURL, f.store.apiKey = f.upstream.URL, "k"
	status, body, _ := f.do(t, "GET", "/v1/telemetry/proxy/incidents/999", nil)
	assert.Equal(t, 404, status)
	assert.JSONEq(t, `{"error":"no such incident"}`, string(body))
}

func TestTelemetryProxyKeepsAnUpstream401AwayFromTheSessionLayer(t *testing.T) {
	f := newTelemetryFixture(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusUnauthorized) })
	f.store.baseURL, f.store.apiKey = f.upstream.URL, "revoked"
	status, body, _ := f.do(t, "GET", "/v1/telemetry/proxy/services", nil)
	assert.Equal(t, 502, status)
	assert.Contains(t, string(body), "telemetry_unauthorized")
}

func TestTelemetryProxyUnreachable(t *testing.T) {
	f := newTelemetryFixture(t, func(w http.ResponseWriter, r *http.Request) {})
	f.store.baseURL, f.store.apiKey = f.upstream.URL, "k"
	f.upstream.Close()
	status, body, _ := f.do(t, "GET", "/v1/telemetry/proxy/services", nil)
	assert.Equal(t, 502, status)
	assert.Contains(t, string(body), "telemetry_unreachable")
}
