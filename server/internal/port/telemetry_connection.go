package port

import (
	"context"
	"errors"
	"net/url"
)

// TelemetrySettingsStore keeps the TaskTrooper Telemetry connection: the API
// URL in the clear and the API key encrypted at rest, like github_token.
type TelemetrySettingsStore interface {
	// TelemetrySettings returns ("", "", nil) when nothing is stored.
	TelemetrySettings(ctx context.Context) (baseURL, apiKey string, err error)
	// SetTelemetrySettings replaces the URL; a nil apiKey keeps the stored
	// key, a pointer to "" clears it.
	SetTelemetrySettings(ctx context.Context, baseURL string, apiKey *string) error
}

// TelemetryResponse is what the telemetry API answered, undigested.
type TelemetryResponse struct {
	Status      int
	ContentType string
	Body        []byte
	// Truncated reports that Body was cut at the client's size cap.
	Truncated bool
}

// ErrTelemetryUnreachable wraps every transport-level failure (DNS, refused,
// timeout, a destination the URL policy refuses).
var ErrTelemetryUnreachable = errors.New("telemetry is unreachable")

// TelemetryClient performs one authenticated GET against the telemetry API.
// relPath is relative to the API root (/api/v1/); the key is sent as a bearer
// token and never leaves the client.
type TelemetryClient interface {
	Get(ctx context.Context, baseURL, apiKey, relPath string, query url.Values) (TelemetryResponse, error)
	// Ping reaches the unauthenticated health endpoint.
	Ping(ctx context.Context, baseURL string) error
}
