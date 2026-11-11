package telemetry

import (
	"context"
	"net/url"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const (
	getIncidentToolName   = "telemetry_get_incident"
	listIncidentsToolName = "telemetry_list_incidents"
	getServiceToolName    = "telemetry_get_service"
	searchLogsToolName    = "telemetry_search_logs"
	searchTracesToolName  = "telemetry_search_traces"
	listErrorsToolName    = "telemetry_list_errors"
	listDeploysToolName   = "telemetry_list_deploys"
	apiGetToolName        = "telemetry_api_get"
)

func definition(name string, params map[string]interface{}) domain.ToolDefinition {
	return domain.ToolDefinition{Type: "function", Function: domain.FunctionDefinition{Name: name, Parameters: params}}
}

func segment(s string) string { return url.PathEscape(strings.TrimSpace(s)) }

// ---- telemetry_get_incident

type getIncidentTool struct{ kit *ToolKit }

func (t *getIncidentTool) Name() string { return getIncidentToolName }

func (t *getIncidentTool) Definition() domain.ToolDefinition {
	return definition(getIncidentToolName, objectSchema(map[string]interface{}{"incident_id": stringSchema()}, "incident_id"))
}

func (t *getIncidentTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args struct {
		IncidentID string `json:"incident_id"`
	}
	if bad := decode(t.Name(), arguments, &args); bad != nil {
		return *bad
	}
	id := strings.TrimSpace(args.IncidentID)
	if id == "" {
		return toolError(t.Name(), "incident_id is required")
	}
	incident, msg := fetch(ctx, t.kit, "incidents/"+segment(id), nil)
	if msg != "" {
		return toolError(t.Name(), msg)
	}
	out := map[string]any{"incident": incident}
	if events, msg := fetch(ctx, t.kit, "incidents/"+segment(id)+"/events", nil); msg == "" {
		out["events"] = events
	}
	return toolJSON(t.Name(), fit(out))
}

// ---- telemetry_list_incidents

type listIncidentsTool struct{ kit *ToolKit }

func (t *listIncidentsTool) Name() string { return listIncidentsToolName }

func (t *listIncidentsTool) Definition() domain.ToolDefinition {
	return definition(listIncidentsToolName, objectSchema(map[string]interface{}{
		"state":    map[string]interface{}{"type": "string", "enum": []string{"open", "pending", "firing", "acknowledged", "resolved"}},
		"service":  stringSchema(),
		"endpoint": stringSchema(),
		"since":    intSchema(),
		"limit":    intSchema(),
	}))
}

func (t *listIncidentsTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args struct {
		State    string `json:"state"`
		Service  string `json:"service"`
		Endpoint string `json:"endpoint"`
		Since    int64  `json:"since"`
		Limit    int    `json:"limit"`
	}
	if bad := decode(t.Name(), arguments, &args); bad != nil {
		return *bad
	}
	q := url.Values{}
	setIf(q, "state", args.State)
	setIf(q, "service", args.Service)
	setIf(q, "endpoint", args.Endpoint)
	if args.Since > 0 {
		q.Set("since", itoa64(args.Since))
	}
	setInt(q, "limit", orDefault(args.Limit, 20), 100)
	out, msg := fetch(ctx, t.kit, "incidents", q)
	if msg != "" {
		return toolError(t.Name(), msg)
	}
	return toolJSON(t.Name(), fit(out))
}

// ---- telemetry_get_service

type getServiceTool struct{ kit *ToolKit }

func (t *getServiceTool) Name() string { return getServiceToolName }

func (t *getServiceTool) Definition() domain.ToolDefinition {
	return definition(getServiceToolName, objectSchema(map[string]interface{}{
		"service":     stringSchema(),
		"range":       rangeSchema(),
		"env":         stringSchema(),
		"endpoint_id": stringSchema(),
		"baseline":    map[string]interface{}{"type": "string", "enum": []string{"prev", "1d", "7d"}},
	}, "service"))
}

func (t *getServiceTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args struct {
		Service    string `json:"service"`
		Range      string `json:"range"`
		Env        string `json:"env"`
		EndpointID string `json:"endpoint_id"`
		Baseline   string `json:"baseline"`
	}
	if bad := decode(t.Name(), arguments, &args); bad != nil {
		return *bad
	}
	svc := strings.TrimSpace(args.Service)
	if svc == "" {
		return toolError(t.Name(), "service is required")
	}
	q := url.Values{}
	setIf(q, "range", args.Range)
	setIf(q, "env", args.Env)

	base := "services/" + segment(svc)
	health, msg := fetch(ctx, t.kit, base, q)
	if msg != "" {
		return toolError(t.Name(), msg)
	}
	out := map[string]any{"service": health}
	for key, rel := range map[string]string{"endpoints": base + "/endpoints", "deploys": base + "/deploys", "dependencies": base + "/dependencies"} {
		if v, msg := fetch(ctx, t.kit, rel, q); msg == "" {
			out[key] = v
		} else {
			out[key+"_error"] = msg
		}
	}
	if id := strings.TrimSpace(args.EndpointID); id != "" {
		rq := url.Values{}
		setIf(rq, "range", args.Range)
		setIf(rq, "env", args.Env)
		setIf(rq, "baseline", args.Baseline)
		if v, msg := fetch(ctx, t.kit, base+"/endpoints/"+segment(id)+"/rca", rq); msg == "" {
			out["rca"] = v
		} else {
			out["rca_error"] = msg
		}
	}
	return toolJSON(t.Name(), fit(out))
}

// ---- telemetry_search_logs

type searchLogsTool struct{ kit *ToolKit }

func (t *searchLogsTool) Name() string { return searchLogsToolName }

func (t *searchLogsTool) Definition() domain.ToolDefinition {
	return definition(searchLogsToolName, objectSchema(map[string]interface{}{
		"service":  stringSchema(),
		"level":    map[string]interface{}{"type": "string", "enum": []string{"error", "warn", "info", "debug"}},
		"q":        stringSchema(),
		"trace_id": stringSchema(),
		"route":    stringSchema(),
		"range":    rangeSchema(),
		"env":      stringSchema(),
		"before":   intSchema(),
		"limit":    intSchema(),
	}))
}

func (t *searchLogsTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args struct {
		Service string `json:"service"`
		Level   string `json:"level"`
		Q       string `json:"q"`
		TraceID string `json:"trace_id"`
		Route   string `json:"route"`
		Range   string `json:"range"`
		Env     string `json:"env"`
		Before  int64  `json:"before"`
		Limit   int    `json:"limit"`
	}
	if bad := decode(t.Name(), arguments, &args); bad != nil {
		return *bad
	}
	q := url.Values{}
	setIf(q, "service", args.Service)
	setIf(q, "level", args.Level)
	setIf(q, "q", args.Q)
	setIf(q, "traceId", args.TraceID)
	setIf(q, "route", args.Route)
	setIf(q, "range", args.Range)
	setIf(q, "env", args.Env)
	if args.Before > 0 {
		q.Set("before", itoa64(args.Before))
	}
	setInt(q, "limit", orDefault(args.Limit, 50), 200)
	out, msg := fetch(ctx, t.kit, "logs", q)
	if msg != "" {
		return toolError(t.Name(), msg)
	}
	return toolJSON(t.Name(), fit(out))
}

// ---- telemetry_search_traces

type searchTracesTool struct{ kit *ToolKit }

func (t *searchTracesTool) Name() string { return searchTracesToolName }

func (t *searchTracesTool) Definition() domain.ToolDefinition {
	return definition(searchTracesToolName, objectSchema(map[string]interface{}{
		"trace_id":    stringSchema(),
		"service":     stringSchema(),
		"endpoint":    stringSchema(),
		"errors_only": boolSchema(),
		"min_ms":      intSchema(),
		"sort":        map[string]interface{}{"type": "string", "enum": []string{"recent", "slowest"}},
		"range":       rangeSchema(),
		"env":         stringSchema(),
		"limit":       intSchema(),
	}))
}

func (t *searchTracesTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args struct {
		TraceID    string `json:"trace_id"`
		Service    string `json:"service"`
		Endpoint   string `json:"endpoint"`
		ErrorsOnly bool   `json:"errors_only"`
		MinMs      int    `json:"min_ms"`
		Sort       string `json:"sort"`
		Range      string `json:"range"`
		Env        string `json:"env"`
		Limit      int    `json:"limit"`
	}
	if bad := decode(t.Name(), arguments, &args); bad != nil {
		return *bad
	}
	if id := strings.TrimSpace(args.TraceID); id != "" {
		out, msg := fetch(ctx, t.kit, "traces/"+segment(id), nil)
		if msg != "" {
			return toolError(t.Name(), msg)
		}
		return toolJSON(t.Name(), fit(out))
	}
	q := url.Values{}
	setIf(q, "service", args.Service)
	setIf(q, "endpoint", args.Endpoint)
	if args.ErrorsOnly {
		q.Set("errors", "true")
	}
	setInt(q, "minMs", args.MinMs, 0)
	setIf(q, "sort", args.Sort)
	setIf(q, "range", args.Range)
	setIf(q, "env", args.Env)
	setInt(q, "limit", orDefault(args.Limit, 10), 100)
	out, msg := fetch(ctx, t.kit, "traces", q)
	if msg != "" {
		return toolError(t.Name(), msg)
	}
	return toolJSON(t.Name(), fit(out))
}

// ---- telemetry_list_errors

type listErrorsTool struct{ kit *ToolKit }

func (t *listErrorsTool) Name() string { return listErrorsToolName }

func (t *listErrorsTool) Definition() domain.ToolDefinition {
	return definition(listErrorsToolName, objectSchema(map[string]interface{}{
		"fingerprint": stringSchema(),
		"service":     stringSchema(),
		"route":       stringSchema(),
		"status":      map[string]interface{}{"type": "string", "enum": []string{"open", "resolved", "ignored"}},
		"range":       rangeSchema(),
		"env":         stringSchema(),
		"limit":       intSchema(),
	}))
}

func (t *listErrorsTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args struct {
		Fingerprint string `json:"fingerprint"`
		Service     string `json:"service"`
		Route       string `json:"route"`
		Status      string `json:"status"`
		Range       string `json:"range"`
		Env         string `json:"env"`
		Limit       int    `json:"limit"`
	}
	if bad := decode(t.Name(), arguments, &args); bad != nil {
		return *bad
	}
	q := url.Values{}
	setIf(q, "range", args.Range)
	if fp := strings.TrimSpace(args.Fingerprint); fp != "" {
		out, msg := fetch(ctx, t.kit, "errors/"+segment(fp), q)
		if msg != "" {
			return toolError(t.Name(), msg)
		}
		return toolJSON(t.Name(), fit(out))
	}
	setIf(q, "service", args.Service)
	setIf(q, "route", args.Route)
	setIf(q, "status", args.Status)
	setIf(q, "env", args.Env)
	setInt(q, "limit", orDefault(args.Limit, 20), 100)
	out, msg := fetch(ctx, t.kit, "errors", q)
	if msg != "" {
		return toolError(t.Name(), msg)
	}
	return toolJSON(t.Name(), fit(out))
}

// ---- telemetry_list_deploys

type listDeploysTool struct{ kit *ToolKit }

func (t *listDeploysTool) Name() string { return listDeploysToolName }

func (t *listDeploysTool) Definition() domain.ToolDefinition {
	return definition(listDeploysToolName, objectSchema(map[string]interface{}{
		"service": stringSchema(),
		"range":   rangeSchema(),
		"env":     stringSchema(),
	}))
}

func (t *listDeploysTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args struct {
		Service string `json:"service"`
		Range   string `json:"range"`
		Env     string `json:"env"`
	}
	if bad := decode(t.Name(), arguments, &args); bad != nil {
		return *bad
	}
	q := url.Values{}
	setIf(q, "service", args.Service)
	setIf(q, "range", args.Range)
	setIf(q, "env", args.Env)
	out, msg := fetch(ctx, t.kit, "deploys", q)
	if msg != "" {
		return toolError(t.Name(), msg)
	}
	return toolJSON(t.Name(), fit(out))
}

// ---- telemetry_api_get

type apiGetTool struct{ kit *ToolKit }

func (t *apiGetTool) Name() string { return apiGetToolName }

func (t *apiGetTool) Definition() domain.ToolDefinition {
	return definition(apiGetToolName, objectSchema(map[string]interface{}{
		"path":  stringSchema(),
		"query": map[string]interface{}{"type": "object", "additionalProperties": stringSchema()},
	}, "path"))
}

func (t *apiGetTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args struct {
		Path  string            `json:"path"`
		Query map[string]string `json:"query"`
	}
	if bad := decode(t.Name(), arguments, &args); bad != nil {
		return *bad
	}
	q := url.Values{}
	for k, v := range args.Query {
		setIf(q, k, v)
	}
	out, msg := fetch(ctx, t.kit, args.Path, q)
	if msg != "" {
		return toolError(t.Name(), msg)
	}
	return toolJSON(t.Name(), fit(out))
}

func orDefault(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}

func itoa64(v int64) string { return formatInt(v) }
