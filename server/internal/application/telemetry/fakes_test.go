package telemetry

import (
	"context"
	"net/url"
	"sync"

	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type fakeStore struct {
	baseURL, apiKey string
}

func (f *fakeStore) TelemetrySettings(context.Context) (string, string, error) {
	return f.baseURL, f.apiKey, nil
}

func (f *fakeStore) SetTelemetrySettings(_ context.Context, baseURL string, apiKey *string) error {
	f.baseURL = baseURL
	if apiKey != nil {
		f.apiKey = *apiKey
	}
	return nil
}

type call struct {
	baseURL, apiKey, path string
	query                 url.Values
}

type fakeClient struct {
	mu      sync.Mutex
	calls   []call
	pings   int
	pingErr error
	respond func(c call) (port.TelemetryResponse, error)
}

func (f *fakeClient) Get(_ context.Context, baseURL, apiKey, relPath string, query url.Values) (port.TelemetryResponse, error) {
	f.mu.Lock()
	c := call{baseURL, apiKey, relPath, query}
	f.calls = append(f.calls, c)
	f.mu.Unlock()
	if f.respond != nil {
		return f.respond(c)
	}
	return port.TelemetryResponse{Status: 200, ContentType: "application/json", Body: []byte(`[]`)}, nil
}

func (f *fakeClient) Ping(context.Context, string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pings++
	return f.pingErr
}
