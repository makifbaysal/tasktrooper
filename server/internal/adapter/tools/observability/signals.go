package observability

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/alerting"
	"github.com/makifbaysal/tasktrooper/server/internal/application/signals"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const (
	defaultMetricsLimit = 200
	maxMetricsLimit     = 1000
)

type listDataSourceMetricsTool struct{ kit *ToolKit }

func (t *listDataSourceMetricsTool) Name() string { return listDataSourceMetricsToolName }

func (t *listDataSourceMetricsTool) Definition() domain.ToolDefinition {
	return fn(listDataSourceMetricsToolName, map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"data_source_id": stringSchema(),
			"match":          stringSchema(),
			"limit":          map[string]any{"type": "integer", "minimum": 1, "maximum": maxMetricsLimit},
		},
	})
}

type dataSourceView struct {
	ID     uuid.UUID `json:"id"`
	Name   string    `json:"name"`
	Kind   string    `json:"kind"`
	Status string    `json:"status"`
}

type recipeView struct {
	ID      string   `json:"id"`
	Signals []string `json:"signals"`
	Params  []string `json:"params"`
	Detect  string   `json:"detect_metric,omitempty"`
}

func (t *listDataSourceMetricsTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	name := t.Name()
	var args struct {
		DataSourceID string `json:"data_source_id"`
		Match        string `json:"match"`
		Limit        int    `json:"limit"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(name, fmt.Sprintf("invalid arguments: %v", err))
	}
	listed, err := t.kit.DataSources.List(ctx)
	if err != nil {
		return toolError(name, err.Error())
	}
	job := t.kit.sessionJob(ctx)
	allowed, err := t.kit.jobSources(ctx, job)
	if err != nil {
		return toolError(name, err.Error())
	}
	sources := make([]domain.DataSource, 0, len(listed))
	for _, ds := range listed {
		if allowed == nil || allowed[ds.ID] {
			sources = append(sources, ds)
		}
	}
	if strings.TrimSpace(args.DataSourceID) == "" {
		views := make([]dataSourceView, 0, len(sources))
		for _, ds := range sources {
			views = append(views, dataSourceView{ID: ds.ID, Name: ds.Name, Kind: string(ds.Kind), Status: string(ds.Status)})
		}
		return toolJSON(name, map[string]any{"data_sources": views})
	}
	id, err := uuid.Parse(strings.TrimSpace(args.DataSourceID))
	if err != nil {
		return toolError(name, fmt.Sprintf("invalid data_source_id: %v", err))
	}
	var source *domain.DataSource
	for i := range sources {
		if sources[i].ID == id {
			source = &sources[i]
		}
	}
	if source == nil && job != nil && slices.ContainsFunc(listed, func(ds domain.DataSource) bool { return ds.ID == id }) {
		return toolError(name, t.kit.outsideJob(ctx, name, outsideSource, *job).Error())
	}
	if source == nil {
		return toolError(name, fmt.Sprintf("unknown data_source_id %s", id))
	}
	limit := args.Limit
	if limit <= 0 {
		limit = defaultMetricsLimit
	}
	disc, err := t.kit.DataSources.Discover(ctx, id, args.Match, min(limit, maxMetricsLimit))
	if err != nil {
		return toolError(name, err.Error())
	}
	recipes, err := t.kit.DataSources.Recipes(ctx)
	if err != nil {
		return toolError(name, err.Error())
	}
	views := []recipeView{}
	for _, r := range recipes {
		if r.SourceKind != source.Kind {
			continue
		}
		sigs := make([]string, 0, len(r.Signals))
		for s := range r.Signals {
			sigs = append(sigs, s)
		}
		sort.Strings(sigs)
		views = append(views, recipeView{ID: r.ID, Signals: sigs, Params: r.Params, Detect: r.Detect.Metric})
	}
	return toolJSON(name, map[string]any{
		"data_source":      dataSourceView{ID: source.ID, Name: source.Name, Kind: string(source.Kind), Status: string(source.Status)},
		"metrics":          disc.Metrics,
		"truncated":        disc.Truncated,
		"matching_recipes": disc.Recipes,
		"recipes":          views,
	})
}

type proposeSignalBindingTool struct{ kit *ToolKit }

func (t *proposeSignalBindingTool) Name() string { return proposeSignalBindingToolName }

func (t *proposeSignalBindingTool) Definition() domain.ToolDefinition {
	return fn(proposeSignalBindingToolName, map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"signal", "data_source_id"},
		"properties": map[string]any{
			"component":      stringSchema(),
			"environment":    environmentSchema(),
			"signal":         stringSchema(),
			"data_source_id": stringSchema(),
			"recipe_id":      stringSchema(),
			"params":         stringMapSchema(),
			"query":          stringSchema(),
			"rationale":      stringSchema(),
			"repository_id":  stringSchema(),
		},
	})
}

func (t *proposeSignalBindingTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	name := t.Name()
	var args struct {
		Component    string            `json:"component"`
		Environment  string            `json:"environment"`
		Signal       string            `json:"signal"`
		DataSourceID string            `json:"data_source_id"`
		RecipeID     string            `json:"recipe_id"`
		Params       map[string]string `json:"params"`
		Query        string            `json:"query"`
		Rationale    string            `json:"rationale"`
		RepositoryID string            `json:"repository_id"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(name, fmt.Sprintf("invalid arguments: %v", err))
	}
	sc, err := t.kit.resolve(ctx, name, args.RepositoryID, args.Component, args.Environment, true)
	if err != nil {
		return toolError(name, err.Error())
	}
	if err := t.kit.checkSource(ctx, name, sc.job, args.DataSourceID); err != nil {
		return toolError(name, err.Error())
	}
	signal := strings.TrimSpace(args.Signal)
	view, err := t.kit.Bindings.Propose(ctx, sc.component.ID, sc.environment, signal, signals.BindingRequest{
		DataSourceID: args.DataSourceID,
		RecipeID:     args.RecipeID,
		Params:       args.Params,
		Query:        args.Query,
	})
	if err != nil {
		return toolError(name, err.Error())
	}
	// A proposal matching a suggestion that predates the job answers that
	// one: it is not something this job stored.
	fresh := sc.job == nil || !view.CreatedAt.Before(sc.job.CreatedAt)
	if view.Source == domain.SignalBindingAI && view.Status == domain.SignalBindingSuggested && fresh {
		t.kit.record(ctx, domain.AIJobItem{Kind: domain.AIJobItemSignalBinding, ID: view.ID, Name: signal, Rationale: strings.TrimSpace(args.Rationale)})
	}
	return toolJSON(name, map[string]any{
		"binding": map[string]any{
			"id":             view.ID,
			"component":      sc.component.Path,
			"environment":    view.Environment,
			"signal":         view.Signal,
			"status":         view.Status,
			"source":         view.Source,
			"rendered_query": view.RenderedQuery,
			"needs_params":   view.NeedsParams,
		},
	})
}

type proposeAlertRuleTool struct{ kit *ToolKit }

func (t *proposeAlertRuleTool) Name() string { return proposeAlertRuleToolName }

func (t *proposeAlertRuleTool) Definition() domain.ToolDefinition {
	return fn(proposeAlertRuleToolName, map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"name", "condition"},
		"properties": map[string]any{
			"component":      stringSchema(),
			"environment":    environmentSchema(),
			"name":           stringSchema(),
			"signal":         stringSchema(),
			"data_source_id": stringSchema(),
			"query":          stringSchema(),
			"condition": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"op"},
				"properties": map[string]any{
					"op":             map[string]any{"type": "string", "enum": []string{"gt", "lt", "change_pct_gt", "change_pct_lt", "rising", "no_data"}},
					"threshold":      map[string]any{"type": "number"},
					"for_seconds":    map[string]any{"type": "integer", "minimum": 0},
					"window_seconds": map[string]any{"type": "integer", "minimum": 60},
				},
			},
			"severity":      severitySchema(),
			"open_incident": map[string]any{"type": "boolean"},
			"rationale":     stringSchema(),
			"repository_id": stringSchema(),
		},
	})
}

func (t *proposeAlertRuleTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	name := t.Name()
	var args struct {
		Component    string                     `json:"component"`
		Environment  string                     `json:"environment"`
		Name         string                     `json:"name"`
		Signal       string                     `json:"signal"`
		DataSourceID string                     `json:"data_source_id"`
		Query        string                     `json:"query"`
		Condition    *alerting.ConditionRequest `json:"condition"`
		Severity     string                     `json:"severity"`
		OpenIncident *bool                      `json:"open_incident"`
		Rationale    string                     `json:"rationale"`
		RepositoryID string                     `json:"repository_id"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(name, fmt.Sprintf("invalid arguments: %v", err))
	}
	sc, err := t.kit.resolve(ctx, name, args.RepositoryID, args.Component, args.Environment, true)
	if err != nil {
		return toolError(name, err.Error())
	}
	if err := t.kit.checkSource(ctx, name, sc.job, args.DataSourceID); err != nil {
		return toolError(name, err.Error())
	}
	compID := sc.component.ID
	existing, err := t.kit.Alerts.List(ctx, domain.AlertRuleFilter{ComponentID: &compID, Environment: sc.environment})
	if err != nil {
		return toolError(name, err.Error())
	}
	signal, query := strings.TrimSpace(args.Signal), strings.TrimSpace(args.Query)
	for _, r := range existing {
		if (signal != "" && r.Signal == signal) || (query != "" && r.Query == query) {
			return toolJSON(name, map[string]any{"existing_rule": map[string]any{
				"id": r.ID, "name": r.Name, "signal": r.Signal, "enabled": r.Enabled, "created_by": r.CreatedBy,
			}})
		}
	}
	comp, env := compID.String(), string(sc.environment)
	req := alerting.RuleRequest{
		Name:         &args.Name,
		ComponentID:  &comp,
		Environment:  &env,
		Condition:    args.Condition,
		OpenIncident: args.OpenIncident,
	}
	if signal != "" {
		req.Signal = &signal
	}
	if query != "" {
		req.Query = &query
	}
	if strings.TrimSpace(args.DataSourceID) != "" {
		req.DataSourceID = &args.DataSourceID
	}
	if strings.TrimSpace(args.Severity) != "" {
		req.Severity = &args.Severity
	}
	rule, err := t.kit.Alerts.CreateAIDraft(ctx, req)
	if err != nil {
		return toolError(name, err.Error())
	}
	t.kit.record(ctx, domain.AIJobItem{Kind: domain.AIJobItemAlertRule, ID: rule.ID, Name: rule.Name, Rationale: strings.TrimSpace(args.Rationale)})
	return toolJSON(name, map[string]any{"rule": map[string]any{
		"id": rule.ID, "name": rule.Name, "component": sc.component.Path, "environment": rule.Environment,
		"signal": rule.Signal, "condition": rule.Condition, "severity": rule.Severity, "enabled": rule.Enabled,
		"created_by": rule.CreatedBy,
	}})
}
