// Package telemetry is the HTTP client for the TaskTrooper Telemetry API
// (/api/v1, bearer project key). It is the only place the key leaves memory.
package telemetry

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	requestTimeout = 20 * time.Second
	// maxBody bounds one answer: replay segments and profiles are the big ones.
	maxBody = 8 << 20
)

type Client struct {
	policy urlguard.Policy
	http   *http.Client
}

var _ port.TelemetryClient = (*Client)(nil)

// New answers a client that dials only what policy admits.
func New(policy urlguard.Policy) *Client {
	return &Client{policy: policy, http: policy.Client(requestTimeout)}
}

func (c *Client) endpoint(baseURL, relPath string, query url.Values) (string, error) {
	u, err := c.policy.Precheck(baseURL)
	if err != nil {
		return "", fmt.Errorf("%w: %v", port.ErrTelemetryUnreachable, err)
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/api/v1/" + strings.TrimLeft(relPath, "/")
	u.RawQuery = query.Encode()
	u.Fragment = ""
	return u.String(), nil
}

func (c *Client) Get(ctx context.Context, baseURL, apiKey, relPath string, query url.Values) (port.TelemetryResponse, error) {
	target, err := c.endpoint(baseURL, relPath, query)
	if err != nil {
		return port.TelemetryResponse{}, err
	}
	return c.do(ctx, http.MethodGet, target, apiKey, nil)
}

func (c *Client) Send(ctx context.Context, baseURL, apiKey, method, relPath string, query url.Values, body []byte) (port.TelemetryResponse, error) {
	target, err := c.endpoint(baseURL, relPath, query)
	if err != nil {
		return port.TelemetryResponse{}, err
	}
	return c.do(ctx, method, target, apiKey, body)
}

func (c *Client) Ping(ctx context.Context, baseURL string) error {
	u, err := c.policy.Precheck(baseURL)
	if err != nil {
		return fmt.Errorf("%w: %v", port.ErrTelemetryUnreachable, err)
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/healthz"
	u.RawQuery = ""
	resp, err := c.do(ctx, http.MethodGet, u.String(), "", nil)
	if err != nil {
		return err
	}
	if resp.Status >= 400 {
		return fmt.Errorf("%w: health check answered %d", port.ErrTelemetryUnreachable, resp.Status)
	}
	return nil
}

func (c *Client) do(ctx context.Context, method, target, apiKey string, payload []byte) (port.TelemetryResponse, error) {
	var reader io.Reader
	if payload != nil {
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return port.TelemetryResponse{}, fmt.Errorf("%w: %v", port.ErrTelemetryUnreachable, err)
	}
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return port.TelemetryResponse{}, err
		}
		return port.TelemetryResponse{}, fmt.Errorf("%w: %s", port.ErrTelemetryUnreachable, redactErr(err))
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return port.TelemetryResponse{}, fmt.Errorf("%w: reading the answer: %s", port.ErrTelemetryUnreachable, redactErr(err))
	}
	out := port.TelemetryResponse{Status: resp.StatusCode, ContentType: resp.Header.Get("Content-Type"), Body: body}
	if len(body) > maxBody {
		out.Body = body[:maxBody]
		out.Truncated = true
	}
	return out, nil
}

// redactErr drops the URL net/http embeds in its errors: it names the host,
// which is enough, and never carries the key (a header) anyway.
func redactErr(err error) string {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err.Error()
	}
	return err.Error()
}
