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

// SourceUse is the signal binding store: which data sources the components
// read through, to keep an AI job to its own repository's.
type SourceUse interface {
	ListConfirmedSignalBindings(ctx context.Context, env domain.DeployEnvironment) ([]domain.SignalBinding, error)
}

// ToolKit: a tool is offered only when what it reads or writes is wired.
// Jobs is optional everywhere. Without SourceUse a job reads no data source.
type ToolKit struct {
	Components  Components
	Errors      ErrorsReader
	Definitions ErrorDefinitions
	Jobs        Jobs
	DataSources DataSources
	Bindings    SignalBindings
	Alerts      AlertRules
	SourceUse   SourceUse
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
// arguments name, else its AI job's, else the run's own repository. Inside a
// job the arguments may name only the job's repository and component;
// outside one, a run bound to a repository may name only that one.
type scope struct {
	repositoryID uuid.UUID
	component    *domain.Component
	environment  domain.DeployEnvironment
	job          *domain.AIJob
}

const (
	outsideRepository = "repository"
	outsideComponent  = "component"
	outsideSource     = "data_source"
)

func (k *ToolKit) sessionJob(ctx context.Context) *domain.AIJob {
	if k.Jobs == nil {
		return nil
	}
	if job, ok := k.Jobs.SessionJob(ctx); ok {
		return &job
	}
	return nil
}

func (k *ToolKit) resolve(ctx context.Context, toolName, repositoryArg, componentArg, envArg string, needComponent bool) (scope, error) {
	sc := scope{job: k.sessionJob(ctx)}
	bound := registry.RepositoryIDFromContext(ctx)
	switch raw := strings.TrimSpace(repositoryArg); {
	case raw != "":
		id, err := uuid.Parse(raw)
		if err != nil {
			return scope{}, fmt.Errorf("invalid repository_id: %w", err)
		}
		switch {
		case sc.job != nil && id != sc.job.RepositoryID:
			return scope{}, k.outsideJob(ctx, toolName, outsideRepository, *sc.job)
		case sc.job == nil && bound != uuid.Nil && id != bound:
			return scope{}, errors.New(outsideRunRepositoryKey.Render(outsideRunRepositoryInput{ToolName: toolName, RepositoryID: bound.String()}))
		}
		sc.repositoryID = id
	case sc.job != nil:
		sc.repositoryID = sc.job.RepositoryID
	default:
		sc.repositoryID = bound
	}
	if sc.repositoryID == uuid.Nil {
		return scope{}, errors.New(prompt.ToolRepositoryRequiredText(toolName))
	}
	sc.environment = domain.DeployEnvironment(strings.TrimSpace(envArg))
	if sc.environment == "" {
		sc.environment = domain.EnvironmentProduction
		if sc.job != nil && sc.job.Environment != "" {
			sc.environment = sc.job.Environment
		}
	}
	if !sc.environment.Valid() {
		return scope{}, fmt.Errorf("unknown environment %q", sc.environment)
	}
	comp, err := k.component(ctx, toolName, sc, componentArg, needComponent)
	if err != nil {
		return scope{}, err
	}
	sc.component = comp
	return sc, nil
}

// outsideJob is the refusal of a call that reaches past its job's scope.
func (k *ToolKit) outsideJob(ctx context.Context, toolName, what string, job domain.AIJob) error {
	in := outsideJobInput{ToolName: toolName, Case: what, RepositoryID: job.RepositoryID.String()}
	if job.ComponentID != nil {
		if c, err := k.Components.GetComponent(ctx, *job.ComponentID); err == nil {
			in.Component = c.Path
		}
	}
	return errors.New(outsideJobKey.Render(in))
}

// component resolves a path exactly among the repository's active
// components. With no path it is the job's component, else (when one is
// needed) the repository's only or root component. A job scoped to one
// component refuses any other.
func (k *ToolKit) component(ctx context.Context, toolName string, sc scope, path string, need bool) (*domain.Component, error) {
	path = strings.TrimSpace(path)
	var jobComponent *uuid.UUID
	if sc.job != nil && sc.job.ComponentID != nil {
		jobComponent = sc.job.ComponentID
	}
	if path == "" && jobComponent != nil {
		c, err := k.Components.GetComponent(ctx, *jobComponent)
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
	pick, err := pickComponent(active, path)
	if err != nil {
		return nil, err
	}
	if jobComponent != nil && pick.ID != *jobComponent {
		return nil, k.outsideJob(ctx, toolName, outsideComponent, *sc.job)
	}
	return pick, nil
}

func pickComponent(active []domain.Component, path string) (*domain.Component, error) {
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

// jobSources is which data sources a job may read and propose through: the
// ones with a confirmed binding on a component of its repository in its
// environment, and the ones no component reads through yet. Nil outside a
// job, where every source is open.
func (k *ToolKit) jobSources(ctx context.Context, job *domain.AIJob) (map[uuid.UUID]bool, error) {
	if job == nil {
		return nil, nil
	}
	allowed := map[uuid.UUID]bool{}
	if k.SourceUse == nil {
		return allowed, nil
	}
	comps, err := k.Components.ListComponents(ctx, job.RepositoryID)
	if err != nil {
		return nil, err
	}
	own := make(map[uuid.UUID]bool, len(comps))
	for _, c := range comps {
		own[c.ID] = true
	}
	env := job.Environment
	if env == "" {
		env = domain.EnvironmentProduction
	}
	bindings, err := k.SourceUse.ListConfirmedSignalBindings(ctx, env)
	if err != nil {
		return nil, err
	}
	foreign := map[uuid.UUID]bool{}
	for _, b := range bindings {
		if own[b.ComponentID] {
			allowed[b.DataSourceID] = true
		} else {
			foreign[b.DataSourceID] = true
		}
	}
	if k.DataSources != nil {
		sources, err := k.DataSources.List(ctx)
		if err != nil {
			return nil, err
		}
		for _, ds := range sources {
			if !foreign[ds.ID] {
				allowed[ds.ID] = true
			}
		}
	}
	return allowed, nil
}

// checkSource refuses, inside a job, a data_source_id the job may not use.
func (k *ToolKit) checkSource(ctx context.Context, toolName string, job *domain.AIJob, raw string) error {
	raw = strings.TrimSpace(raw)
	if job == nil || raw == "" {
		return nil
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid data_source_id: %w", err)
	}
	allowed, err := k.jobSources(ctx, job)
	if err != nil {
		return err
	}
	if !allowed[id] {
		return k.outsideJob(ctx, toolName, outsideSource, *job)
	}
	return nil
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
