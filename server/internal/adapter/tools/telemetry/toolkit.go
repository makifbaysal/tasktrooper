// Package telemetry exposes the TaskTrooper Telemetry data to agents as
// ordinary tools — incidents, service health, logs, traces, errors and
// deploys — through the same allowlisted, key-injecting service the desktop UI
// reads through. They only read; nothing here can change telemetry.
package telemetry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	telemetryapp "github.com/makifbaysal/tasktrooper/server/internal/application/telemetry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// Reader is application/telemetry.Service.
type Reader interface {
	Get(ctx context.Context, relPath string, query url.Values) (port.TelemetryResponse, error)
}

type ToolKit struct {
	Reader Reader
}

func NewExecutors(k *ToolKit) []port.ToolExecutor {
	if k == nil || k.Reader == nil {
		return nil
	}
	return []port.ToolExecutor{
		&getIncidentTool{kit: k},
		&listIncidentsTool{kit: k},
		&getServiceTool{kit: k},
		&searchLogsTool{kit: k},
		&searchTracesTool{kit: k},
		&listErrorsTool{kit: k},
		&listDeploysTool{kit: k},
		&apiGetTool{kit: k},
	}
}

// ToolNames are the names NewExecutors registers.
var ToolNames = []string{
	getIncidentToolName, listIncidentsToolName, getServiceToolName, searchLogsToolName,
	searchTracesToolName, listErrorsToolName, listDeploysToolName, apiGetToolName,
}

// maxResultBytes keeps one answer inside a context budget.
const maxResultBytes = 48 << 10

type unreachableInput struct{ Detail string }

var (
	notConfiguredKey = prompt.Define[struct{}]("guard.telemetry_not_configured", struct{}{})
	unauthorizedKey  = prompt.Define[struct{}]("guard.telemetry_unauthorized", struct{}{})
	unreachableKey   = prompt.Define("guard.telemetry_unreachable", unreachableInput{Detail: "connection refused"})
	pathKey          = prompt.Define[struct{}]("guard.telemetry_path_not_allowed", struct{}{})
	truncatedKey     = prompt.Define[struct{}]("tool_results.telemetry_truncated", struct{}{})
)

func toolError(name, message string) domain.ToolResult {
	return domain.ToolResult{Name: name, Content: message, IsError: true}
}

func toolJSON(name string, payload any) domain.ToolResult {
	raw, err := json.Marshal(payload)
	if err != nil {
		return toolError(name, fmt.Sprintf("marshal response: %v", err))
	}
	return domain.ToolResult{Name: name, Content: string(raw)}
}

func decode(name, arguments string, into any) *domain.ToolResult {
	if strings.TrimSpace(arguments) == "" {
		arguments = "{}"
	}
	if err := json.Unmarshal([]byte(arguments), into); err != nil {
		r := toolError(name, fmt.Sprintf("invalid arguments: %v", err))
		return &r
	}
	return nil
}

// fetch is one GET, returned as decoded JSON. Failures come back as the text
// a tool call answers with.
func fetch(ctx context.Context, k *ToolKit, rel string, q url.Values) (any, string) {
	resp, err := k.Reader.Get(ctx, rel, q)
	switch {
	case errors.Is(err, telemetryapp.ErrNotConfigured):
		return nil, prompt.Text(notConfiguredKey)
	case errors.Is(err, telemetryapp.ErrUnauthorized):
		return nil, prompt.Text(unauthorizedKey)
	case errors.Is(err, telemetryapp.ErrPathNotAllowed):
		return nil, prompt.Text(pathKey)
	case errors.Is(err, port.ErrTelemetryUnreachable):
		return nil, unreachableKey.Render(unreachableInput{Detail: strings.TrimPrefix(err.Error(), port.ErrTelemetryUnreachable.Error()+": ")})
	case err != nil:
		return nil, err.Error()
	}
	if resp.Status >= 400 {
		return nil, fmt.Sprintf("telemetry answered %d: %s", resp.Status, snippet(resp.Body))
	}
	var out any
	if err := json.Unmarshal(resp.Body, &out); err != nil {
		return nil, fmt.Sprintf("telemetry answered something that is not JSON: %s", snippet(resp.Body))
	}
	return out, ""
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return s
}

type cut struct {
	Field string `json:"field,omitempty"`
	Total int    `json:"total"`
	Shown int    `json:"shown"`
}

// fit halves the largest list in v until its JSON fits maxResultBytes. What
// was cut is named under "truncated", so a partial list is never read as a
// complete one.
func fit(v any) any {
	size := func(x any) int { b, _ := json.Marshal(x); return len(b) }
	if size(v) <= maxResultBytes {
		return v
	}
	cuts := map[string]*cut{}
	note := func(field string, total, shown int) {
		c := cuts[field]
		if c == nil {
			c = &cut{Field: field, Total: total}
			cuts[field] = c
		}
		c.Shown = shown
	}
loop:
	for i := 0; i < 64 && size(v)+512 > maxResultBytes; i++ {
		switch x := v.(type) {
		case []any:
			if len(x) <= 1 {
				break loop
			}
			note("", len(x), len(x)/2)
			v = x[:len(x)/2]
		case map[string]any:
			field, list := largestList(x)
			if len(list) <= 1 {
				break loop
			}
			note(field, len(list), len(list)/2)
			next := make(map[string]any, len(x))
			for k, val := range x {
				next[k] = val
			}
			next[field] = list[:len(list)/2]
			v = next
		default:
			break loop
		}
	}
	fields := make([]string, 0, len(cuts))
	for f := range cuts {
		fields = append(fields, f)
	}
	sort.Strings(fields)
	list := make([]cut, 0, len(fields))
	for _, f := range fields {
		list = append(list, *cuts[f])
	}
	return map[string]any{"truncated": list, "hint": prompt.Text(truncatedKey), "data": v}
}

func largestList(m map[string]any) (string, []any) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	best, bestSize := "", -1
	var list []any
	for _, k := range keys {
		l, ok := m[k].([]any)
		if !ok {
			continue
		}
		if s := len(l); s > bestSize {
			best, bestSize, list = k, s, l
		}
	}
	return best, list
}

func setIf(q url.Values, key, value string) {
	if v := strings.TrimSpace(value); v != "" {
		q.Set(key, v)
	}
}

func setInt(q url.Values, key string, value, max int) {
	if value <= 0 {
		return
	}
	if max > 0 && value > max {
		value = max
	}
	q.Set(key, strconv.Itoa(value))
}

var rangeValues = []string{"15m", "30m", "1h", "6h", "24h", "7d"}

func rangeSchema() map[string]interface{} {
	return map[string]interface{}{"type": "string", "enum": rangeValues}
}

func stringSchema() map[string]interface{} { return map[string]interface{}{"type": "string"} }

func intSchema() map[string]interface{} { return map[string]interface{}{"type": "integer"} }

func boolSchema() map[string]interface{} { return map[string]interface{}{"type": "boolean"} }

func objectSchema(props map[string]interface{}, required ...string) map[string]interface{} {
	out := map[string]interface{}{
		"type":                 "object",
		"additionalProperties": false,
		"properties":           props,
	}
	if len(required) > 0 {
		out["required"] = required
	}
	return out
}

func formatInt(v int64) string { return strconv.FormatInt(v, 10) }
