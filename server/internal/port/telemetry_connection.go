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

// TelemetryClient performs one authenticated request against the telemetry
// API. relPath is relative to the API root (/api/v1/); the key is sent as a
// bearer token and never leaves the client. Which paths and methods a caller
// may reach is the application's decision, not the client's.
type TelemetryClient interface {
	Get(ctx context.Context, baseURL, apiKey, relPath string, query url.Values) (TelemetryResponse, error)
	// Send is a request with a JSON body (nil sends none).
	Send(ctx context.Context, baseURL, apiKey, method, relPath string, query url.Values, body []byte) (TelemetryResponse, error)
	// Ping reaches the unauthenticated health endpoint.
	Ping(ctx context.Context, baseURL string) error
}

// TelemetryReader is one allowlisted GET against the TaskTrooper Telemetry
// connection saved in Settings, its key injected by the reader (the
// application's telemetry.Service, on top of TelemetryClient). A data source
// of kind tasktrooper_telemetry reads through it, so it holds no URL or key
// of its own. An unsaved connection is an error whose text says so.
type TelemetryReader interface {
	Get(ctx context.Context, relPath string, query url.Values) (TelemetryResponse, error)
}

// TelemetrySourceMarkStore remembers that a person deleted the data source
// made on its own for the telemetry connection, so it is not made again.
type TelemetrySourceMarkStore interface {
	TelemetrySourceDeleted(ctx context.Context) (bool, error)
	SetTelemetrySourceDeleted(ctx context.Context, deleted bool) error
}
