package telemetry

import (
	"context"
	"net/url"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const (
	explainEndpointToolName = "telemetry_explain_endpoint"
	listSLOsToolName        = "telemetry_list_slos"
	listMonitorsToolName    = "telemetry_list_monitors"
	listLoadTestsToolName   = "telemetry_list_load_tests"
)

// ---- telemetry_explain_endpoint

type explainEndpointTool struct{ kit *ToolKit }

func (t *explainEndpointTool) Name() string { return explainEndpointToolName }

func (t *explainEndpointTool) Definition() domain.ToolDefinition {
	return definition(explainEndpointToolName, objectSchema(map[string]interface{}{
		"service":  stringSchema(),
		"endpoint": stringSchema(),
		"range":    rangeSchema(),
		"env":      stringSchema(),
		"baseline": map[string]interface{}{"type": "string", "enum": []string{"prev", "1d", "7d"}},
	}, "service", "endpoint"))
}

func (t *explainEndpointTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args struct {
		Service  string `json:"service"`
		Endpoint string `json:"endpoint"`
		Range    string `json:"range"`
		Env      string `json:"env"`
		Baseline string `json:"baseline"`
	}
	if bad := decode(t.Name(), arguments, &args); bad != nil {
		return *bad
	}
	svc, endpoint := strings.TrimSpace(args.Service), strings.TrimSpace(args.Endpoint)
	if svc == "" || endpoint == "" {
		return toolError(t.Name(), "service and endpoint are required")
	}
	q := url.Values{}
	setIf(q, "range", args.Range)
	setIf(q, "env", args.Env)
	base := "services/" + segment(svc) + "/endpoints"
	id := endpoint
	// "METHOD /route" is how traces and incidents name an endpoint; the
	// comparison is keyed by the slug, so look it up.
	if strings.Contains(endpoint, " ") {
		list, msg := fetch(ctx, t.kit, base, q)
		if msg != "" {
			return toolError(t.Name(), msg)
		}
		id = endpointSlug(list, endpoint)
		if id == "" {
			return toolError(t.Name(), "no endpoint "+endpoint+" in "+svc+" over this range; pass the id slug from telemetry_get_service's endpoints")
		}
	}
	rq := url.Values{}
	setIf(rq, "range", args.Range)
	setIf(rq, "env", args.Env)
	setIf(rq, "baseline", args.Baseline)
	out, msg := fetch(ctx, t.kit, base+"/"+segment(id)+"/rca", rq)
	if msg != "" {
		return toolError(t.Name(), msg)
	}
	return toolJSON(t.Name(), fit(map[string]any{"endpoint_id": id, "rca": out}))
}

func endpointSlug(list any, endpoint string) string {
	items, _ := list.([]any)
	want := strings.ToUpper(strings.Join(strings.Fields(endpoint), " "))
	for _, raw := range items {
		e, _ := raw.(map[string]any)
		method, _ := e["method"].(string)
		route, _ := e["route"].(string)
		id, _ := e["id"].(string)
		if id == "" {
			continue
		}
		if strings.ToUpper(strings.TrimSpace(method)+" "+strings.TrimSpace(route)) == want || strings.ToUpper(route) == want {
			return id
		}
	}
	return ""
}

// ---- telemetry_list_slos

type listSLOsTool struct{ kit *ToolKit }

func (t *listSLOsTool) Name() string { return listSLOsToolName }

func (t *listSLOsTool) Definition() domain.ToolDefinition {
	return definition(listSLOsToolName, objectSchema(map[string]interface{}{
		"service": stringSchema(),
		"slo_id":  stringSchema(),
	}))
}

func (t *listSLOsTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args struct {
		Service string `json:"service"`
		SLOID   string `json:"slo_id"`
	}
	if bad := decode(t.Name(), arguments, &args); bad != nil {
		return *bad
	}
	if id := strings.TrimSpace(args.SLOID); id != "" {
		out, msg := fetch(ctx, t.kit, "kpis/"+segment(id), nil)
		if msg != "" {
			return toolError(t.Name(), msg)
		}
		return toolJSON(t.Name(), fit(out))
	}
	q := url.Values{}
	setIf(q, "serviceId", args.Service)
	out, msg := fetch(ctx, t.kit, "kpis", q)
	if msg != "" {
		return toolError(t.Name(), msg)
	}
	return toolJSON(t.Name(), fit(out))
}

// ---- telemetry_list_monitors

type listMonitorsTool struct{ kit *ToolKit }

func (t *listMonitorsTool) Name() string { return listMonitorsToolName }

func (t *listMonitorsTool) Definition() domain.ToolDefinition {
	return definition(listMonitorsToolName, objectSchema(map[string]interface{}{
		"service":     stringSchema(),
		"endpoint":    stringSchema(),
		"kind":        stringSchema(),
		"suggestions": map[string]interface{}{"type": "string", "enum": []string{"none", "proposed", "all"}},
	}))
}

func (t *listMonitorsTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args struct {
		Service     string `json:"service"`
		Endpoint    string `json:"endpoint"`
		Kind        string `json:"kind"`
		Suggestions string `json:"suggestions"`
	}
	if bad := decode(t.Name(), arguments, &args); bad != nil {
		return *bad
	}
	q := url.Values{}
	setIf(q, "service", args.Service)
	setIf(q, "endpoint", args.Endpoint)
	setIf(q, "kind", args.Kind)
	monitors, msg := fetch(ctx, t.kit, "monitors", q)
	if msg != "" {
		return toolError(t.Name(), msg)
	}
	out := map[string]any{"monitors": monitors}
	state := strings.TrimSpace(args.Suggestions)
	if state == "" {
		state = "proposed"
	}
	if state != "none" {
		if v, msg := fetch(ctx, t.kit, "suggestions", url.Values{"state": {state}}); msg == "" {
			out["suggestions"] = v
		} else {
			out["suggestions_error"] = msg
		}
	}
	return toolJSON(t.Name(), fit(out))
}

// ---- telemetry_list_load_tests

type listLoadTestsTool struct{ kit *ToolKit }

func (t *listLoadTestsTool) Name() string { return listLoadTestsToolName }

func (t *listLoadTestsTool) Definition() domain.ToolDefinition {
	return definition(listLoadTestsToolName, objectSchema(map[string]interface{}{
		"run_id":      stringSchema(),
		"scenario_id": stringSchema(),
	}))
}

func (t *listLoadTestsTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args struct {
		RunID      string `json:"run_id"`
		ScenarioID string `json:"scenario_id"`
	}
	if bad := decode(t.Name(), arguments, &args); bad != nil {
		return *bad
	}
	if id := strings.TrimSpace(args.RunID); id != "" {
		out, msg := fetch(ctx, t.kit, "loadtests/runs/"+segment(id), nil)
		if msg != "" {
			return toolError(t.Name(), msg)
		}
		return toolJSON(t.Name(), fit(out))
	}
	q := url.Values{}
	setIf(q, "scenarioId", args.ScenarioID)
	runs, msg := fetch(ctx, t.kit, "loadtests/runs", q)
	if msg != "" {
		return toolError(t.Name(), msg)
	}
	out := map[string]any{"runs": runs}
	if v, msg := fetch(ctx, t.kit, "loadtests/scenarios", nil); msg == "" {
		out["scenarios"] = withoutSpecs(v)
	} else {
		out["scenarios_error"] = msg
	}
	if v, msg := fetch(ctx, t.kit, "loadtests/domains", nil); msg == "" {
		out["domains"] = domainSummaries(v)
	} else {
		out["domains_error"] = msg
	}
	return toolJSON(t.Name(), fit(out))
}

// withoutSpecs drops each scenario's spec, which is long and only matters
// when writing one.
func withoutSpecs(v any) any {
	items, ok := v.([]any)
	if !ok {
		return v
	}
	out := make([]any, 0, len(items))
	for _, raw := range items {
		sc, ok := raw.(map[string]any)
		if !ok {
			out = append(out, raw)
			continue
		}
		cp := make(map[string]any, len(sc))
		for k, val := range sc {
			if k != "spec" {
				cp[k] = val
			}
		}
		out = append(out, cp)
	}
	return out
}

// domainSummaries keeps a target domain's host and verification, never its
// verification token.
func domainSummaries(v any) []map[string]any {
	items, _ := v.([]any)
	out := make([]map[string]any, 0, len(items))
	for _, raw := range items {
		d, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, map[string]any{"host": d["host"], "verified": d["verified"], "method": d["method"]})
	}
	return out
}
