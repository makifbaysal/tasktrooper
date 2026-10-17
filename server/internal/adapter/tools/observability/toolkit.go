// Package observability exposes the release engineer's observability tools:
// reading error groups and data source inventories, and proposing error
// definitions, signal bindings and alert rules. Every proposal is stored as
// a suggestion (or a disabled rule) for a person to accept; inside an AI job
// it is also recorded on the job, found through the chat session on the
// tool call's context.
package observability

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/alerting"
	"github.com/makifbaysal/tasktrooper/server/internal/application/errordef"
	"github.com/makifbaysal/tasktrooper/server/internal/application/opsquery"
	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/application/signals"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	listErrorGroupsToolName         = "list_error_groups"
	proposeErrorDefinitionsToolName = "propose_error_definitions"
	listDataSourceMetricsToolName   = "list_data_source_metrics"
	proposeSignalBindingToolName    = "propose_signal_binding"
	proposeAlertRuleToolName        = "propose_alert_rule"
)

type ErrorsReader interface {
	Errors(ctx context.Context, q opsquery.ErrorsQuery) (domain.OpsErrors, error)
}

type ErrorDefinitions interface {
	Annotate(ctx context.Context, in domain.OpsErrors, includeIgnored bool) domain.OpsErrors
	Propose(ctx context.Context, repositoryID uuid.UUID, proposals []errordef.Proposal) ([]errordef.ProposalOutcome, error)
}

// Jobs is opsai.Service: the job a tool call's chat belongs to.
type Jobs interface {
	SessionJob(ctx context.Context) (domain.AIJob, bool)
	Record(ctx context.Context, item domain.AIJobItem)
}

type Components interface {
	ListComponents(ctx context.Context, repositoryID uuid.UUID) ([]domain.Component, error)
	GetComponent(ctx context.Context, id uuid.UUID) (domain.Component, error)
}

type DataSources interface {
	List(ctx context.Context) ([]domain.DataSource, error)
	Discover(ctx context.Context, id uuid.UUID, match string, limit int) (domain.DataSourceDiscovery, error)
	Recipes(ctx context.Context) ([]domain.Recipe, error)
}

type SignalBindings interface {
	Propose(ctx context.Context, componentID uuid.UUID, env domain.DeployEnvironment, signal string, req signals.BindingRequest) (domain.SignalBindingView, error)
}

type AlertRules interface {
	List(ctx context.Context, filter domain.AlertRuleFilter) ([]domain.AlertRule, error)
	CreateAIDraft(ctx context.Context, req alerting.RuleRequest) (domain.AlertRule, error)
}

// ToolKit: a tool is offered only when what it reads or writes is wired.
// Jobs is optional everywhere.
type ToolKit struct {
	Components  Components
	Errors      ErrorsReader
	Definitions ErrorDefinitions
	Jobs        Jobs
	DataSources DataSources
	Bindings    SignalBindings
	Alerts      AlertRules
}

func NewExecutors(k *ToolKit) []port.ToolExecutor {
	if k == nil || k.Components == nil {
		return nil
	}
	var out []port.ToolExecutor
	if k.Errors != nil {
		out = append(out, &listErrorGroupsTool{kit: k})
	}
	if k.Definitions != nil {
		out = append(out, &proposeErrorDefinitionsTool{kit: k})
	}
	if k.DataSources != nil {
		out = append(out, &listDataSourceMetricsTool{kit: k})
	}
	if k.Bindings != nil {
		out = append(out, &proposeSignalBindingTool{kit: k})
	}
	if k.Alerts != nil {
		out = append(out, &proposeAlertRuleTool{kit: k})
	}
	return out
}

func fn(name string, params map[string]any) domain.ToolDefinition {
	return domain.ToolDefinition{Type: "function", Function: domain.FunctionDefinition{Name: name, Parameters: params}}
}

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

func stringSchema() map[string]any { return map[string]any{"type": "string"} }

func environmentSchema() map[string]any {
	return map[string]any{"type": "string", "enum": []string{"production", "staging", "preview", "development"}}
}

func stringMapSchema() map[string]any {
	return map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}}
}

func severitySchema() map[string]any {
	return map[string]any{"type": "string", "enum": []string{"critical", "high", "medium", "low"}}
}

// scope is where a call reads or writes: the repository and component its
// arguments name, else its AI job's, else the run's own repository.
type scope struct {
	repositoryID uuid.UUID
	component    *domain.Component
	environment  domain.DeployEnvironment
	job          *domain.AIJob
}

func (k *ToolKit) resolve(ctx context.Context, toolName, repositoryArg, componentArg, envArg string, needComponent bool) (scope, error) {
	var sc scope
	if k.Jobs != nil {
		if job, ok := k.Jobs.SessionJob(ctx); ok {
			sc.job = &job
		}
	}
	switch raw := strings.TrimSpace(repositoryArg); {
	case raw != "":
		id, err := uuid.Parse(raw)
		if err != nil {
			return scope{}, fmt.Errorf("invalid repository_id: %w", err)
		}
		sc.repositoryID = id
	case sc.job != nil:
		sc.repositoryID = sc.job.RepositoryID
	default:
		sc.repositoryID = registry.RepositoryIDFromContext(ctx)
	}
	if sc.repositoryID == uuid.Nil {
		return scope{}, errors.New(prompt.ToolRepositoryRequiredText(toolName))
	}
	sc.environment = domain.DeployEnvironment(strings.TrimSpace(envArg))
	if sc.environment == "" {
		sc.environment = domain.EnvironmentProduction
		if sc.job != nil && sc.job.RepositoryID == sc.repositoryID && sc.job.Environment != "" {
			sc.environment = sc.job.Environment
		}
	}
	if !sc.environment.Valid() {
		return scope{}, fmt.Errorf("unknown environment %q", sc.environment)
	}
	comp, err := k.component(ctx, sc, componentArg, needComponent)
	if err != nil {
		return scope{}, err
	}
	sc.component = comp
	return sc, nil
}

// component resolves a path exactly among the repository's active
// components. With no path it is the job's component, else (when one is
// needed) the repository's only or root component.
func (k *ToolKit) component(ctx context.Context, sc scope, path string, need bool) (*domain.Component, error) {
	path = strings.TrimSpace(path)
	if path == "" && sc.job != nil && sc.job.ComponentID != nil && sc.job.RepositoryID == sc.repositoryID {
		c, err := k.Components.GetComponent(ctx, *sc.job.ComponentID)
		if err != nil {
			return nil, err
		}
		return &c, nil
	}
	if path == "" && !need {
		return nil, nil
	}
	all, err := k.Components.ListComponents(ctx, sc.repositoryID)
	if err != nil {
		return nil, err
	}
	var active []domain.Component
	for _, c := range all {
		if c.Status == domain.ComponentStatusActive {
			active = append(active, c)
		}
	}
	if len(active) == 0 {
		return nil, errors.New("this repository has no active components")
	}
	if path != "" && path != "." {
		for i := range active {
			if active[i].Path == path {
				return &active[i], nil
			}
		}
		return nil, fmt.Errorf("no active component at path %q", path)
	}
	if len(active) == 1 {
		return &active[0], nil
	}
	for i := range active {
		if active[i].Path == "." {
			return &active[i], nil
		}
	}
	paths := make([]string, 0, len(active))
	for _, c := range active {
		paths = append(paths, c.Path)
	}
	sort.Strings(paths)
	return nil, errors.New(prompt.RuntimeComponentsAmbiguousText(len(active), strings.Join(paths, ", ")))
}

func (k *ToolKit) record(ctx context.Context, item domain.AIJobItem) {
	if k.Jobs != nil {
		k.Jobs.Record(ctx, item)
	}
}

// parseSince takes a duration ("30m", "24h", "7d") or an RFC3339 instant.
func parseSince(raw string, now time.Time, def time.Duration) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return now.Add(-def), nil
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t, nil
	}
	if days, ok := strings.CutSuffix(raw, "d"); ok {
		n, err := strconv.ParseFloat(days, 64)
		if err != nil || n <= 0 {
			return time.Time{}, fmt.Errorf("invalid since %q", raw)
		}
		return now.Add(-time.Duration(n * float64(24*time.Hour))), nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return time.Time{}, fmt.Errorf("invalid since %q", raw)
	}
	return now.Add(-d), nil
}
