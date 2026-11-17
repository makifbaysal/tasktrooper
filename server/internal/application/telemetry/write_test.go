package telemetry

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

func configured() (*Service, *fakeClient) {
	svc, store, client := newTestService()
	store.baseURL, store.apiKey = "http://t.example", "k"
	return svc, client
}

func TestValidateWriteAllowsOnlyTheAgentWrites(t *testing.T) {
	ok := []struct{ method, path string }{
		{http.MethodPost, "monitors"},
		{http.MethodPost, "/monitors/preview/"},
		{http.MethodPatch, "monitors/m1"},
		{http.MethodDelete, "kpis/k1"},
		{http.MethodPost, "loadtests/scenarios/s1/runs"},
		{http.MethodPost, "loadtests/runs/r1/stop"},
		{http.MethodPost, "suggestions/s1/accept"},
		{http.MethodPost, "deploys"},
	}
	for _, c := range ok {
		_, err := ValidateWrite(c.method, c.path)
		assert.NoErrorf(t, err, "%s %s", c.method, c.path)
	}
	refused := []struct{ method, path string }{
		{http.MethodGet, "monitors"},
		{http.MethodDelete, "monitors"},
		{http.MethodPost, "api-keys"},
		{http.MethodPost, "integrations/slack/test"},
		{http.MethodDelete, "loadtests/scenarios/s1"},
		{http.MethodPost, "loadtests/domains"},
		{http.MethodPost, "monitors/%2e%2e/x"},
		{http.MethodPost, "suggestions/s1/accept/extra"},
		{http.MethodPut, "monitors/a%2Fb"},
	}
	for _, c := range refused {
		_, err := ValidateWrite(c.method, c.path)
		assert.ErrorIsf(t, err, ErrPathNotAllowed, "%s %s", c.method, c.path)
	}
}

func TestTheReadAllowlistNeverAdmitsAWrite(t *testing.T) {
	svc, client := configured()
	_, err := svc.Proxy(context.Background(), "api-keys", "")
	assert.ErrorIs(t, err, ErrPathNotAllowed)
	for _, c := range client.calls {
		assert.Equal(t, "GET", c.method)
	}
}

func TestWriteStampsCreatesWithTheAgentSource(t *testing.T) {
	svc, client := configured()
	_, err := svc.Write(context.Background(), http.MethodPost, "monitors", nil, map[string]any{"name": "p95", "source": "manual"})
	require.NoError(t, err)
	require.Len(t, client.calls, 1)
	var body map[string]any
	require.NoError(t, json.Unmarshal([]byte(client.calls[0].body), &body))
	assert.Equal(t, "agent", body["source"])
	assert.Equal(t, http.MethodPost, client.calls[0].method)
}

func TestWriteRefusesChangingARecordAnAgentDidNotCreate(t *testing.T) {
	svc, client := configured()
	client.respond = func(c call) (port.TelemetryResponse, error) {
		if c.method == "GET" {
			return port.TelemetryResponse{Status: 200, Body: []byte(`{"id":"m1","source":"manual"}`)}, nil
		}
		return port.TelemetryResponse{Status: 200, Body: []byte(`{}`)}, nil
	}
	_, err := svc.Write(context.Background(), http.MethodDelete, "monitors/m1", nil, nil)
	assert.ErrorIs(t, err, ErrNotOwned)
	require.Len(t, client.calls, 1, "nothing is sent after the ownership read refuses")
}

func TestWriteChangesARecordAnAgentCreated(t *testing.T) {
	svc, client := configured()
	client.respond = func(c call) (port.TelemetryResponse, error) {
		if c.method == "GET" {
			return port.TelemetryResponse{Status: 200, Body: []byte(`{"id":"k1","source":"agent"}`)}, nil
		}
		return port.TelemetryResponse{Status: 200, Body: []byte(`{}`)}, nil
	}
	_, err := svc.Write(context.Background(), http.MethodPut, "kpis/k1", nil, map[string]any{"name": "x"})
	require.NoError(t, err)
	require.Len(t, client.calls, 2)
	assert.Equal(t, http.MethodPut, client.calls[1].method)
	assert.Contains(t, client.calls[1].body, `"source":"agent"`)
}

func TestWriteNeedsAConnectionAndMaps401(t *testing.T) {
	svc, _, _ := newTestService()
	_, err := svc.Write(context.Background(), http.MethodPost, "deploys", nil, map[string]any{})
	assert.ErrorIs(t, err, ErrNotConfigured)

	svc, client := configured()
	client.respond = func(call) (port.TelemetryResponse, error) { return port.TelemetryResponse{Status: 401}, nil }
	_, err = svc.Write(context.Background(), http.MethodPost, "loadtests/runs/r1/stop", nil, nil)
	assert.ErrorIs(t, err, ErrUnauthorized)
}

