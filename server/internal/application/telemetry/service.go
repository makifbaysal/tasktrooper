// Package telemetry is TaskTrooper's native client of the TaskTrooper
// Telemetry product: the connection settings (URL + write-only API key), a
// read-only allowlisted passthrough the desktop UI and the agents' tools read
// through, and the incident sync that feeds telemetry alerts to the release
// engineer. The key is injected here, on the server, and never returned.
package telemetry

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

var (
	ErrNotConfigured  = errors.New("telemetry is not configured")
	ErrPathNotAllowed = errors.New("telemetry path is not allowed")
	ErrUnauthorized   = errors.New("telemetry rejected the API key")
	ErrInvalidURL     = errors.New("telemetry URL must be an http(s) URL")
)

const statusTTL = 15 * time.Second

type Deps struct {
	Store  port.TelemetrySettingsStore
	Client port.TelemetryClient
}

type Service struct {
	store  port.TelemetrySettingsStore
	client port.TelemetryClient

	scope func(ctx context.Context) string
	now   func() time.Time

	mu     sync.Mutex
	status map[string]cachedStatus
}

type cachedStatus struct {
	at        time.Time
	reachable bool
}

func NewService(d Deps) *Service {
	return &Service{store: d.Store, client: d.Client, now: time.Now, status: map[string]cachedStatus{}}
}

// SetScope keys the status cache; a deployment serving many tenants from one
// process must tell them apart.
func (s *Service) SetScope(scope func(ctx context.Context) string) { s.scope = scope }

func (s *Service) scopeOf(ctx context.Context) string {
	if s.scope == nil {
		return ""
	}
	return s.scope(ctx)
}

type Settings struct {
	BaseURL    string `json:"base_url"`
	Configured bool   `json:"configured"`
}

type TestResult struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

type Status struct {
	Configured bool `json:"configured"`
	Reachable  bool `json:"reachable"`
}

func (s *Service) Settings(ctx context.Context) (Settings, error) {
	baseURL, apiKey, err := s.store.TelemetrySettings(ctx)
	if err != nil {
		return Settings{}, err
	}
	return Settings{BaseURL: baseURL, Configured: baseURL != "" && apiKey != ""}, nil
}

// Update saves the connection. A nil apiKey keeps the stored key, a pointer to
// "" clears it. An empty baseURL clears the URL.
func (s *Service) Update(ctx context.Context, baseURL string, apiKey *string) (Settings, error) {
	baseURL, err := NormalizeBaseURL(baseURL)
	if err != nil {
		return Settings{}, err
	}
	if apiKey != nil {
		trimmed := strings.TrimSpace(*apiKey)
		apiKey = &trimmed
	}
	if err := s.store.SetTelemetrySettings(ctx, baseURL, apiKey); err != nil {
		return Settings{}, err
	}
	s.forgetStatus(ctx)
	return s.Settings(ctx)
}

// NormalizeBaseURL trims, drops a trailing slash and requires http(s) with a
// host. "" is valid and means "clear".
func NormalizeBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
		return "", ErrInvalidURL
	}
	u.RawQuery, u.Fragment = "", ""
	return strings.TrimRight(u.String(), "/"), nil
}

type connection struct{ baseURL, apiKey string }

func (c connection) ok() bool { return c.baseURL != "" && c.apiKey != "" }

func (s *Service) connection(ctx context.Context) (connection, error) {
	baseURL, apiKey, err := s.store.TelemetrySettings(ctx)
	if err != nil {
		return connection{}, err
	}
	return connection{baseURL: baseURL, apiKey: apiKey}, nil
}

// Configured reports whether both halves of the connection are saved.
func (s *Service) Configured(ctx context.Context) (bool, error) {
	c, err := s.connection(ctx)
	return c.ok(), err
}

// Get is one allowlisted GET against the API. A 401 from the upstream is
// ErrUnauthorized: it is about the key, never about the caller's own session.
func (s *Service) Get(ctx context.Context, relPath string, query url.Values) (port.TelemetryResponse, error) {
	clean, err := ValidatePath(relPath)
	if err != nil {
		return port.TelemetryResponse{}, err
	}
	c, err := s.connection(ctx)
	if err != nil {
		return port.TelemetryResponse{}, err
	}
	if !c.ok() {
		return port.TelemetryResponse{}, ErrNotConfigured
	}
	resp, err := s.client.Get(ctx, c.baseURL, c.apiKey, clean, query)
	if err != nil {
		return port.TelemetryResponse{}, err
	}
	if resp.Status == 401 {
		return port.TelemetryResponse{}, ErrUnauthorized
	}
	return resp, nil
}

// Proxy is Get for a raw query string, as the HTTP route receives it.
func (s *Service) Proxy(ctx context.Context, relPath, rawQuery string) (port.TelemetryResponse, error) {
	q, err := url.ParseQuery(rawQuery)
	if err != nil {
		return port.TelemetryResponse{}, ErrPathNotAllowed
	}
	return s.Get(ctx, relPath, q)
}

// TestInput overrides the saved connection so a form can be tried before it is
// saved; an empty field falls back to the saved one.
type TestInput struct {
	BaseURL string
	APIKey  string
}

func (s *Service) Test(ctx context.Context, in TestInput) (TestResult, error) {
	c, err := s.connection(ctx)
	if err != nil {
		return TestResult{}, err
	}
	if v := strings.TrimSpace(in.BaseURL); v != "" {
		norm, nerr := NormalizeBaseURL(v)
		if nerr != nil {
			return TestResult{Error: nerr.Error()}, nil
		}
		c.baseURL = norm
	}
	if v := strings.TrimSpace(in.APIKey); v != "" {
		c.apiKey = v
	}
	if c.baseURL == "" {
		return TestResult{Error: "the telemetry API URL is not set"}, nil
	}
	if err := s.client.Ping(ctx, c.baseURL); err != nil {
		return TestResult{Error: unreachableText(err)}, nil
	}
	if c.apiKey == "" {
		return TestResult{Error: "the telemetry API key is not set"}, nil
	}
	resp, err := s.client.Get(ctx, c.baseURL, c.apiKey, "environments", nil)
	if err != nil {
		return TestResult{Error: unreachableText(err)}, nil
	}
	switch {
	case resp.Status == 401 || resp.Status == 403:
		return TestResult{Error: "the telemetry API rejected the key"}, nil
	case resp.Status >= 400:
		return TestResult{Error: "the telemetry API answered an error"}, nil
	}
	return TestResult{OK: true}, nil
}

func unreachableText(err error) string {
	if errors.Is(err, port.ErrTelemetryUnreachable) {
		return strings.TrimPrefix(err.Error(), port.ErrTelemetryUnreachable.Error()+": ")
	}
	return err.Error()
}

// Status is cheap enough for a header chip: not configured costs nothing and a
// configured connection is probed at most once per statusTTL.
func (s *Service) Status(ctx context.Context) (Status, error) {
	c, err := s.connection(ctx)
	if err != nil {
		return Status{}, err
	}
	if !c.ok() {
		return Status{}, nil
	}
	key := s.scopeOf(ctx) + "|" + c.baseURL
	s.mu.Lock()
	hit, ok := s.status[key]
	s.mu.Unlock()
	if ok && s.now().Sub(hit.at) < statusTTL {
		return Status{Configured: true, Reachable: hit.reachable}, nil
	}
	reachable := s.client.Ping(ctx, c.baseURL) == nil
	s.mu.Lock()
	s.status[key] = cachedStatus{at: s.now(), reachable: reachable}
	s.mu.Unlock()
	return Status{Configured: true, Reachable: reachable}, nil
}

func (s *Service) forgetStatus(ctx context.Context) {
	prefix := s.scopeOf(ctx) + "|"
	s.mu.Lock()
	defer s.mu.Unlock()
	for k := range s.status {
		if strings.HasPrefix(k, prefix) {
			delete(s.status, k)
		}
	}
}
