package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// AgentSource is the source every record an agent creates in telemetry
// carries, and the only source an agent may change or delete.
const AgentSource = "agent"

// stampedRoots are the creates whose body Write stamps with AgentSource, so a
// record an agent made is always marked as such whatever the tool sent.
var stampedRoots = map[string]bool{
	"monitors":            true,
	"kpis":                true,
	"loadtests/scenarios": true,
}

// ownedRoots are the records an agent may change or delete only when an
// agent created them.
var ownedRoots = map[string]bool{"monitors": true, "kpis": true}

// Write is one allowlisted write for the agents' tools (never the UI proxy).
// Creates under stampedRoots are stamped source=agent; a change or delete of
// a monitor or KPI first reads it and is refused with ErrNotOwned unless an
// agent created it. body is marshalled to JSON; nil sends none.
func (s *Service) Write(ctx context.Context, method, relPath string, query url.Values, body any) (port.TelemetryResponse, error) {
	clean, err := ValidateWrite(method, relPath)
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
	if method == http.MethodPost && stampedRoots[clean] {
		if body, err = stamp(body); err != nil {
			return port.TelemetryResponse{}, err
		}
	}
	if root, _, ok := ownedRecord(clean); ok && method != http.MethodPost {
		if err := s.requireOwned(ctx, c, clean); err != nil {
			return port.TelemetryResponse{}, err
		}
		if method == http.MethodPut && ownedRoots[root] {
			if body, err = stamp(body); err != nil {
				return port.TelemetryResponse{}, err
			}
		}
	}
	var raw []byte
	if body != nil {
		if raw, err = json.Marshal(body); err != nil {
			return port.TelemetryResponse{}, fmt.Errorf("encode the telemetry request: %w", err)
		}
	}
	resp, err := s.client.Send(ctx, c.baseURL, c.apiKey, method, clean, query, raw)
	if err != nil {
		return port.TelemetryResponse{}, err
	}
	if resp.Status == 401 {
		return port.TelemetryResponse{}, ErrUnauthorized
	}
	return resp, nil
}

// ownedRecord splits "monitors/<id>" or "kpis/<id>".
func ownedRecord(clean string) (root, id string, ok bool) {
	root, id, found := strings.Cut(clean, "/")
	if !found || !ownedRoots[root] || id == "" || strings.Contains(id, "/") || id == "preview" {
		return "", "", false
	}
	return root, id, true
}

func (s *Service) requireOwned(ctx context.Context, c connection, clean string) error {
	resp, err := s.client.Get(ctx, c.baseURL, c.apiKey, clean, nil)
	if err != nil {
		return err
	}
	switch {
	case resp.Status == 401:
		return ErrUnauthorized
	case resp.Status >= 400:
		return fmt.Errorf("telemetry answered %d reading %s", resp.Status, clean)
	}
	var rec struct {
		Source string `json:"source"`
	}
	if json.Unmarshal(resp.Body, &rec) != nil || rec.Source != AgentSource {
		return fmt.Errorf("%w (source %q)", ErrNotOwned, rec.Source)
	}
	return nil
}

// stamp sets source=agent on a JSON object body.
func stamp(body any) (map[string]any, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encode the telemetry request: %w", err)
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return nil, fmt.Errorf("a telemetry create takes a JSON object body")
	}
	obj["source"] = AgentSource
	return obj, nil
}
