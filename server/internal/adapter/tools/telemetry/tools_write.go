package telemetry

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	telemetryapp "github.com/makifbaysal/tasktrooper/server/internal/application/telemetry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const (
	proposeMonitorToolName   = "telemetry_propose_monitor"
	proposeSLOToolName       = "telemetry_propose_slo"
	acceptSuggestionToolName = "telemetry_accept_suggestion"
	runLoadTestToolName      = "telemetry_run_load_test"
	stopLoadTestToolName     = "telemetry_stop_load_test"
)

type approvalInput struct{ Action string }

type loadTestHostInput struct{ Host string }

var (
	needsApprovalKey     = prompt.Define("tool_results.telemetry_needs_approval", approvalInput{Action: proposeMonitorToolName})
	domainUnverifiedKey  = prompt.Define("guard.telemetry_load_test_unverified", loadTestHostInput{Host: "api.example.com"})
	productionTargetKey  = prompt.Define("guard.telemetry_load_test_production", loadTestHostInput{Host: "api.example.com"})
	scenarioDraftKey     = prompt.Define[struct{}]("guard.telemetry_load_test_draft", struct{}{})
	loadTestStartedKey   = prompt.Define[struct{}]("tool_results.telemetry_load_test_started", struct{}{})
	defaultLoadTestGate  = map[string]any{"abortErrorRate": 0.5, "abortP95Ms": 10000}
	severityValues       = []string{"critical", "warning", "info"}
	monitorWindowValues  = []string{"1m", "5m", "10m", "15m", "30m", "1h"}
	monitorKindValues    = []string{"latency", "error_rate", "throughput", "apdex", "metric_threshold", "anomaly", "error_new", "log_count", "log_pattern", "job_missed", "consumer_lag", "dlq", "rum_vital", "crash_rate", "composite", "infra", "faas", "llm"}
	comparisonOpValues   = []string{">", ">=", "<", "<="}
	previewTailBuckets   = 60
	loadTestEnvironments = []string{"staging", "production"}
)

// dryRun is what a write answers without user_approved: exactly what it
// would send, plus whatever it read to judge it, and how to get the yes.
func dryRun(action, method, path string, body any, extra map[string]any) map[string]any {
	out := map[string]any{
		"dry_run":         true,
		"would_send":      map[string]any{"method": method, "path": path, "body": body},
		"approval_needed": needsApprovalKey.Render(approvalInput{Action: action}),
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

func enumSchema(values []string) map[string]interface{} {
	return map[string]interface{}{"type": "string", "enum": values}
}

func numberSchema() map[string]interface{} { return map[string]interface{}{"type": "number"} }

func freeObjectSchema() map[string]interface{} { return map[string]interface{}{"type": "object"} }

func stringListSchema() map[string]interface{} {
	return map[string]interface{}{"type": "array", "items": stringSchema()}
}

func isOneOf(v string, values []string) bool {
	for _, x := range values {
		if v == x {
			return true
		}
	}
	return false
}

// owned refuses, before any approval is asked for, a change to a record an
// agent did not create; the write path refuses it again on send.
func owned(ctx context.Context, k *ToolKit, rel string) string {
	rec, msg := fetch(ctx, k, rel, nil)
	if msg != "" {
		return msg
	}
	m, _ := rec.(map[string]any)
	if src, _ := m["source"].(string); src != telemetryapp.AgentSource {
		return prompt.Text(notOwnedKey)
	}
	return ""
}

// ---- telemetry_propose_monitor

type monitorTarget struct {
	Service  string `json:"service,omitempty"`
	Endpoint string `json:"endpoint,omitempty"`
	Process  string `json:"process,omitempty"`
}

type monitorCondition struct {
	Op          string   `json:"op"`
	Threshold   float64  `json:"threshold"`
	ForMin      *int     `json:"forMin,omitempty"`
	MinRequests *float64 `json:"minRequests,omitempty"`
	Window      string   `json:"window,omitempty"`
	RenotifyMin *int     `json:"renotifyMin,omitempty"`
}

type monitorBody struct {
	Name      string           `json:"name"`
	Kind      string           `json:"kind"`
	Target    monitorTarget    `json:"target"`
	Query     map[string]any   `json:"query,omitempty"`
	Condition monitorCondition `json:"condition"`
	Severity  string           `json:"severity"`
	Channels  []string         `json:"channels"`
	Status    string           `json:"status,omitempty"`
	Queue     map[string]any   `json:"queue,omitempty"`
}

func (b *monitorBody) check() string {
	b.Name = strings.TrimSpace(b.Name)
	switch {
	case b.Name == "":
		return "monitor.name is required"
	case !isOneOf(b.Kind, monitorKindValues):
		return "monitor.kind must be one of " + strings.Join(monitorKindValues, ", ")
	case !isOneOf(b.Severity, severityValues):
		return "monitor.severity must be critical, warning or info"
	case !isOneOf(b.Condition.Op, comparisonOpValues):
		return "monitor.condition.op must be >, >=, < or <="
	case b.Status != "" && b.Status != "active" && b.Status != "draft":
		return "monitor.status must be active or draft"
	case b.Condition.Window != "" && !isOneOf(b.Condition.Window, monitorWindowValues):
		return "monitor.condition.window must be one of " + strings.Join(monitorWindowValues, ", ")
	}
	if b.Channels == nil {
		b.Channels = []string{}
	}
	// The window is stated explicitly, as the telemetry editor does.
	if b.Condition.Window == "" && windowed(b.Kind) {
		b.Condition.Window = "5m"
		if b.Kind == "anomaly" {
			b.Condition.Window = "15m"
		}
	}
	return ""
}

func windowed(kind string) bool {
	switch kind {
	case "job_missed", "error_new", "composite", "consumer_lag":
		return false
	}
	return true
}

func monitorSchema() map[string]interface{} {
	return objectSchema(map[string]interface{}{
		"name": stringSchema(),
		"kind": enumSchema(monitorKindValues),
		"target": objectSchema(map[string]interface{}{
			"service":  stringSchema(),
			"endpoint": stringSchema(),
			"process":  stringSchema(),
		}),
		"query": freeObjectSchema(),
		"condition": objectSchema(map[string]interface{}{
			"op":          enumSchema(comparisonOpValues),
			"threshold":   numberSchema(),
			"forMin":      intSchema(),
			"minRequests": numberSchema(),
			"window":      enumSchema(monitorWindowValues),
			"renotifyMin": intSchema(),
		}, "op", "threshold"),
		"severity": enumSchema(severityValues),
		"channels": stringListSchema(),
		"status":   enumSchema([]string{"active", "draft"}),
		"queue":    freeObjectSchema(),
	}, "name", "kind", "condition", "severity")
}

type proposeMonitorTool struct{ kit *ToolKit }

func (t *proposeMonitorTool) Name() string { return proposeMonitorToolName }

func (t *proposeMonitorTool) Definition() domain.ToolDefinition {
	return definition(proposeMonitorToolName, objectSchema(map[string]interface{}{
		"monitor":       monitorSchema(),
		"preview_range": rangeSchema(),
		"replace_id":    stringSchema(),
		"user_approved": boolSchema(),
	}, "monitor"))
}

func (t *proposeMonitorTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args struct {
		Monitor      monitorBody `json:"monitor"`
		PreviewRange string      `json:"preview_range"`
		ReplaceID    string      `json:"replace_id"`
		UserApproved bool        `json:"user_approved"`
	}
	if bad := decode(t.Name(), arguments, &args); bad != nil {
		return *bad
	}
	if msg := args.Monitor.check(); msg != "" {
		return toolError(t.Name(), msg)
	}
	method, path := http.MethodPost, "monitors"
	if id := strings.TrimSpace(args.ReplaceID); id != "" {
		method, path = http.MethodPut, "monitors/"+segment(id)
		if msg := owned(ctx, t.kit, path); msg != "" {
			return toolError(t.Name(), msg)
		}
	}
	if !args.UserApproved {
		extra := map[string]any{}
		rng := strings.TrimSpace(args.PreviewRange)
		if rng == "" {
			rng = "24h"
		}
		if preview, msg := send(ctx, t.kit, http.MethodPost, "monitors/preview", url.Values{"range": {rng}}, args.Monitor); msg == "" {
			extra["preview"] = trimPreview(preview)
		} else {
			extra["preview_error"] = msg
		}
		return toolJSON(t.Name(), fit(dryRun(t.Name(), method, path, args.Monitor, extra)))
	}
	out, msg := send(ctx, t.kit, method, path, nil, args.Monitor)
	if msg != "" {
		return toolError(t.Name(), msg)
	}
	key := "created"
	if method == http.MethodPut {
		key = "updated"
	}
	return toolJSON(t.Name(), fit(map[string]any{key: out}))
}

// trimPreview keeps the verdict (episodes, firing now) and a short tail of
// the per-bucket series.
func trimPreview(v any) any {
	doc, ok := v.(map[string]any)
	if !ok {
		return v
	}
	data, ok := doc["data"].([]any)
	if !ok || len(data) <= previewTailBuckets {
		return v
	}
	cp := make(map[string]any, len(doc)+1)
	for k, val := range doc {
		cp[k] = val
	}
	cp["data"] = data[len(data)-previewTailBuckets:]
	if ts, ok := doc["timestamps"].([]any); ok && len(ts) > previewTailBuckets {
		cp["timestamps"] = ts[len(ts)-previewTailBuckets:]
	}
	cp["data_trimmed_to_last"] = previewTailBuckets
	return cp
}

// ---- telemetry_propose_slo

type sloTarget struct {
	Kind      string `json:"kind"`
	ServiceID string `json:"serviceId"`
	Ref       string `json:"ref"`
}

type sloGood struct {
	ThresholdMs float64 `json:"thresholdMs,omitempty"`
	StatusBelow int     `json:"statusBelow,omitempty"`
	NoError     bool    `json:"noError,omitempty"`
	Metric      string  `json:"metric,omitempty"`
	Description string  `json:"description,omitempty"`
}

type sloAlert struct {
	Kind        string   `json:"kind"`
	LongWindow  string   `json:"longWindow"`
	ShortWindow string   `json:"shortWindow,omitempty"`
	Threshold   float64  `json:"threshold"`
	Severity    string   `json:"severity"`
	Channels    []string `json:"channels"`
}

type sloBody struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Owner       string     `json:"owner"`
	Target      sloTarget  `json:"target"`
	Sli         string     `json:"sli"`
	Good        sloGood    `json:"good"`
	Objective   float64    `json:"objective"`
	Window      string     `json:"window"`
	Alerts      []sloAlert `json:"alerts"`
}

var (
	sloKinds      = []string{"endpoint", "page", "journey", "app_start"}
	sloSLIs       = []string{"latency", "availability", "success_rate"}
	sloWindows    = []string{"7d", "28d"}
	sloAlertKinds = []string{"burn_rate", "sli_below"}
)

func (k *sloBody) check() string {
	k.Name = strings.TrimSpace(k.Name)
	switch {
	case k.Name == "":
		return "slo.name is required"
	case !isOneOf(k.Sli, sloSLIs):
		return "slo.sli must be latency, availability or success_rate"
	case k.Objective <= 0 || k.Objective >= 1:
		return "slo.objective must be between 0 and 1 (exclusive), e.g. 0.99"
	case !isOneOf(k.Window, sloWindows):
		return "slo.window must be 7d or 28d"
	case !isOneOf(k.Target.Kind, sloKinds):
		return "slo.target.kind must be endpoint, page, journey or app_start"
	case strings.TrimSpace(k.Target.ServiceID) == "" || strings.TrimSpace(k.Target.Ref) == "":
		return "slo.target.serviceId and slo.target.ref are required"
	case k.Target.Kind == "endpoint" && !strings.Contains(strings.TrimSpace(k.Target.Ref), " "):
		return `an endpoint slo.target.ref is "METHOD /route", not the id slug`
	case k.Sli == "latency" && k.Good.ThresholdMs <= 0:
		return "a latency SLO needs slo.good.thresholdMs"
	}
	if k.Alerts == nil {
		k.Alerts = []sloAlert{}
	}
	for i := range k.Alerts {
		a := &k.Alerts[i]
		if !isOneOf(a.Kind, sloAlertKinds) || !isOneOf(a.Severity, severityValues) || strings.TrimSpace(a.LongWindow) == "" {
			return "each slo.alerts entry needs kind (burn_rate or sli_below), longWindow and severity (critical, warning or info)"
		}
		if a.Channels == nil {
			a.Channels = []string{}
		}
	}
	return ""
}

func sloSchema() map[string]interface{} {
	return objectSchema(map[string]interface{}{
		"name":        stringSchema(),
		"description": stringSchema(),
		"owner":       stringSchema(),
		"target": objectSchema(map[string]interface{}{
			"kind":      enumSchema(sloKinds),
			"serviceId": stringSchema(),
			"ref":       stringSchema(),
		}, "kind", "serviceId", "ref"),
		"sli": enumSchema(sloSLIs),
		"good": objectSchema(map[string]interface{}{
			"thresholdMs": numberSchema(),
			"statusBelow": intSchema(),
			"noError":     boolSchema(),
			"metric":      stringSchema(),
			"description": stringSchema(),
		}),
		"objective": numberSchema(),
		"window":    enumSchema(sloWindows),
		"alerts": map[string]interface{}{"type": "array", "items": objectSchema(map[string]interface{}{
			"kind":        enumSchema(sloAlertKinds),
			"longWindow":  stringSchema(),
			"shortWindow": stringSchema(),
			"threshold":   numberSchema(),
			"severity":    enumSchema(severityValues),
			"channels":    stringListSchema(),
		}, "kind", "longWindow", "threshold", "severity")},
	}, "name", "target", "sli", "objective", "window")
}

type proposeSLOTool struct{ kit *ToolKit }

func (t *proposeSLOTool) Name() string { return proposeSLOToolName }

func (t *proposeSLOTool) Definition() domain.ToolDefinition {
	return definition(proposeSLOToolName, objectSchema(map[string]interface{}{
		"slo":           sloSchema(),
		"replace_id":    stringSchema(),
		"user_approved": boolSchema(),
	}, "slo"))
}

func (t *proposeSLOTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args struct {
		SLO          sloBody `json:"slo"`
		ReplaceID    string  `json:"replace_id"`
		UserApproved bool    `json:"user_approved"`
	}
	if bad := decode(t.Name(), arguments, &args); bad != nil {
		return *bad
	}
	if msg := args.SLO.check(); msg != "" {
		return toolError(t.Name(), msg)
	}
	method, path := http.MethodPost, "kpis"
	if id := strings.TrimSpace(args.ReplaceID); id != "" {
		method, path = http.MethodPut, "kpis/"+segment(id)
		if msg := owned(ctx, t.kit, path); msg != "" {
			return toolError(t.Name(), msg)
		}
	}
	if !args.UserApproved {
		return toolJSON(t.Name(), dryRun(t.Name(), method, path, args.SLO, nil))
	}
	out, msg := send(ctx, t.kit, method, path, nil, args.SLO)
	if msg != "" {
		return toolError(t.Name(), msg)
	}
	key := "created"
	if method == http.MethodPut {
		key = "updated"
	}
	return toolJSON(t.Name(), fit(map[string]any{key: out}))
}

// ---- telemetry_accept_suggestion

type acceptSuggestionTool struct{ kit *ToolKit }

func (t *acceptSuggestionTool) Name() string { return acceptSuggestionToolName }

func (t *acceptSuggestionTool) Definition() domain.ToolDefinition {
	return definition(acceptSuggestionToolName, objectSchema(map[string]interface{}{
		"suggestion_id": stringSchema(),
		"threshold":     numberSchema(),
		"severity":      enumSchema(severityValues),
		"channels":      stringListSchema(),
		"user_approved": boolSchema(),
	}, "suggestion_id"))
}

func (t *acceptSuggestionTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args struct {
		SuggestionID string   `json:"suggestion_id"`
		Threshold    *float64 `json:"threshold"`
		Severity     string   `json:"severity"`
		Channels     []string `json:"channels"`
		UserApproved bool     `json:"user_approved"`
	}
	if bad := decode(t.Name(), arguments, &args); bad != nil {
		return *bad
	}
	id := strings.TrimSpace(args.SuggestionID)
	if id == "" {
		return toolError(t.Name(), "suggestion_id is required (from telemetry_list_monitors)")
	}
	if args.Severity != "" && !isOneOf(args.Severity, severityValues) {
		return toolError(t.Name(), "severity must be critical, warning or info")
	}
	body := map[string]any{}
	if args.Threshold != nil {
		body["threshold"] = *args.Threshold
	}
	if args.Severity != "" {
		body["severity"] = args.Severity
	}
	if args.Channels != nil {
		body["channels"] = args.Channels
	}
	path := "suggestions/" + segment(id) + "/accept"
	if !args.UserApproved {
		extra := map[string]any{}
		if s := findSuggestion(ctx, t.kit, id); s != nil {
			extra["suggestion"] = s
		}
		return toolJSON(t.Name(), fit(dryRun(t.Name(), http.MethodPost, path, body, extra)))
	}
	out, msg := send(ctx, t.kit, http.MethodPost, path, nil, body)
	if msg != "" {
		return toolError(t.Name(), msg)
	}
	return toolJSON(t.Name(), fit(map[string]any{"accepted": out}))
}

func findSuggestion(ctx context.Context, k *ToolKit, id string) any {
	list, msg := fetch(ctx, k, "suggestions", url.Values{"state": {"all"}})
	if msg != "" {
		return nil
	}
	items, _ := list.([]any)
	for _, raw := range items {
		if s, ok := raw.(map[string]any); ok && s["id"] == id {
			return s
		}
	}
	return nil
}

// ---- telemetry_run_load_test

type runLoadTestTool struct{ kit *ToolKit }

func (t *runLoadTestTool) Name() string { return runLoadTestToolName }

func (t *runLoadTestTool) Definition() domain.ToolDefinition {
	return definition(runLoadTestToolName, objectSchema(map[string]interface{}{
		"scenario_id": stringSchema(),
		"scenario": objectSchema(map[string]interface{}{
			"name": stringSchema(),
			"spec": freeObjectSchema(),
		}, "name", "spec"),
		"environment":   enumSchema(loadTestEnvironments),
		"user_approved": boolSchema(),
	}))
}

func (t *runLoadTestTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args struct {
		ScenarioID string `json:"scenario_id"`
		Scenario   *struct {
			Name string         `json:"name"`
			Spec map[string]any `json:"spec"`
		} `json:"scenario"`
		Environment  string `json:"environment"`
		UserApproved bool   `json:"user_approved"`
	}
	if bad := decode(t.Name(), arguments, &args); bad != nil {
		return *bad
	}
	env := strings.TrimSpace(args.Environment)
	if env == "" {
		env = "staging"
	}
	if !isOneOf(env, loadTestEnvironments) {
		return toolError(t.Name(), "environment must be staging or production")
	}
	id := strings.TrimSpace(args.ScenarioID)
	if (id == "") == (args.Scenario == nil) {
		return toolError(t.Name(), "pass exactly one of scenario_id (an existing ready scenario) or scenario (a new one)")
	}

	var target string
	var summary map[string]any
	var create map[string]any
	if args.Scenario != nil {
		name := strings.TrimSpace(args.Scenario.Name)
		spec := args.Scenario.Spec
		if msg := checkSpec(name, spec); msg != "" {
			return toolError(t.Name(), msg)
		}
		// Stop on its own when the target is clearly falling over.
		if _, ok := spec["gate"]; !ok {
			spec["gate"] = defaultLoadTestGate
		}
		target, _ = spec["target"].(string)
		create = map[string]any{"name": name, "state": "ready", "spec": spec}
		summary = map[string]any{"name": name, "target": target, "steps": spec["steps"], "levels": spec["levels"], "gate": spec["gate"]}
	} else {
		sc, msg := fetch(ctx, t.kit, "loadtests/scenarios/"+segment(id), nil)
		if msg != "" {
			return toolError(t.Name(), msg)
		}
		m, _ := sc.(map[string]any)
		if state, _ := m["state"].(string); state == "draft" {
			return toolError(t.Name(), prompt.Text(scenarioDraftKey))
		}
		target, _ = m["target"].(string)
		summary = map[string]any{"name": m["name"], "target": target, "peakVus": m["peakVus"], "durationS": m["duration"], "steps": m["steps"]}
	}

	host := hostOf(target)
	if host == "" {
		return toolError(t.Name(), "the scenario has no target URL")
	}
	verified, msg := t.verified(ctx, host)
	if msg != "" {
		return toolError(t.Name(), msg)
	}
	if !verified {
		return toolError(t.Name(), domainUnverifiedKey.Render(loadTestHostInput{Host: host}))
	}
	if env != "production" && t.isProduction(ctx, host) {
		return toolError(t.Name(), productionTargetKey.Render(loadTestHostInput{Host: host}))
	}

	if !args.UserApproved {
		path := "loadtests/scenarios/" + id + "/runs"
		var body any
		if create != nil {
			path, body = "loadtests/scenarios", create
		}
		return toolJSON(t.Name(), fit(dryRun(t.Name(), http.MethodPost, path, body, map[string]any{"scenario": summary, "environment": env})))
	}
	if create != nil {
		created, msg := send(ctx, t.kit, http.MethodPost, "loadtests/scenarios", nil, create)
		if msg != "" {
			return toolError(t.Name(), msg)
		}
		m, _ := created.(map[string]any)
		if id, _ = m["id"].(string); id == "" {
			return toolError(t.Name(), "telemetry created the scenario but answered no id")
		}
	}
	run, msg := send(ctx, t.kit, http.MethodPost, "loadtests/scenarios/"+segment(id)+"/runs", nil, nil)
	if msg != "" {
		return toolError(t.Name(), msg)
	}
	return toolJSON(t.Name(), fit(map[string]any{"scenario_id": id, "started": run, "next": prompt.Text(loadTestStartedKey)}))
}

func checkSpec(name string, spec map[string]any) string {
	if name == "" {
		return "scenario.name is required"
	}
	if spec == nil {
		return "scenario.spec is required"
	}
	target, _ := spec["target"].(string)
	if u, err := url.Parse(target); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "scenario.spec.target must be the base URL, e.g. https://api.example.com (a verified domain from telemetry_list_load_tests)"
	}
	if _, ok := spec["script"]; ok {
		return ""
	}
	if steps, _ := spec["steps"].([]any); len(steps) == 0 {
		return `scenario.spec.steps is required: ["METHOD /route", ...]`
	}
	_, levels := spec["levels"]
	_, stages := spec["stages"]
	if !levels && !stages {
		return "scenario.spec.levels is required: [{name, ramp, hold, target}] (seconds, VUs or RPS)"
	}
	return ""
}

func hostOf(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

// verified mirrors the telemetry runner's own rule: the host itself is a
// verified domain, or a subdomain of one verified by DNS.
func (t *runLoadTestTool) verified(ctx context.Context, host string) (bool, string) {
	list, msg := fetch(ctx, t.kit, "loadtests/domains", nil)
	if msg != "" {
		return false, msg
	}
	items, _ := list.([]any)
	for _, raw := range items {
		d, _ := raw.(map[string]any)
		if ok, _ := d["verified"].(bool); !ok {
			continue
		}
		h, _ := d["host"].(string)
		h = strings.ToLower(strings.TrimSpace(h))
		method, _ := d["method"].(string)
		if h != "" && (host == h || (method == "dns" && strings.HasSuffix(host, "."+h))) {
			return true, ""
		}
	}
	return false, ""
}

// isProduction reports whether host is where the run's repository's
// production answers, by its recorded deploy targets.
func (t *runLoadTestTool) isProduction(ctx context.Context, host string) bool {
	if t.kit.Targets == nil {
		return false
	}
	repo := registry.RepositoryIDFromContext(ctx)
	if repo == uuid.Nil {
		return false
	}
	targets, err := t.kit.Targets.Targets(ctx, repo)
	if err != nil {
		return false
	}
	for _, tg := range targets {
		if tg.Env != domain.DeployEnvProd {
			continue
		}
		for _, u := range []string{tg.BaseURL, tg.HealthURL} {
			if h := hostOf(u); h != "" && h == host {
				return true
			}
		}
	}
	return false
}

// ---- telemetry_stop_load_test

type stopLoadTestTool struct{ kit *ToolKit }

func (t *stopLoadTestTool) Name() string { return stopLoadTestToolName }

func (t *stopLoadTestTool) Definition() domain.ToolDefinition {
	return definition(stopLoadTestToolName, objectSchema(map[string]interface{}{"run_id": stringSchema()}, "run_id"))
}

// Execute needs no approval: stopping only ever takes load away.
func (t *stopLoadTestTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args struct {
		RunID string `json:"run_id"`
	}
	if bad := decode(t.Name(), arguments, &args); bad != nil {
		return *bad
	}
	id := strings.TrimSpace(args.RunID)
	if id == "" {
		return toolError(t.Name(), "run_id is required")
	}
	out, msg := send(ctx, t.kit, http.MethodPost, "loadtests/runs/"+segment(id)+"/stop", nil, nil)
	if msg != "" {
		return toolError(t.Name(), msg)
	}
	return toolJSON(t.Name(), fit(map[string]any{"stopped": out}))
}
