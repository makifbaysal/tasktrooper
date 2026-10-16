// Package ops exposes production operations to agents: reading the incident
// they were dispatched for, writing back the remedy they diagnosed, and loading
// the deploy recipe for an environment they have to ship.
package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/deploy"
	"github.com/makifbaysal/tasktrooper/server/internal/application/deployops"
	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// Incidents is the incident surface an agent is allowed to touch.
type Incidents interface {
	List(ctx context.Context, filter domain.IncidentFilter) ([]domain.Incident, error)
	Get(ctx context.Context, id uuid.UUID) (domain.Incident, error)
	ProposeRemedy(ctx context.Context, id uuid.UUID, remedy domain.Remedy, author string) (domain.Incident, error)
	Resolve(ctx context.Context, id uuid.UUID, note string) (domain.Incident, error)
}

// Deploys is the deploy-definition surface an agent is allowed to touch. It is
// read-only except for RecordTargetURLs, which can move nothing but the two
// address fields — how a repo ships stays a human decision.
type Deploys interface {
	Targets(ctx context.Context, repositoryID uuid.UUID) ([]domain.DeployTarget, error)
	Instructions(ctx context.Context, repositoryID uuid.UUID, env string) (string, error)
	RecordTargetURLs(ctx context.Context, repositoryID uuid.UUID, env string, in deploy.RecordTargetURLsInput) (domain.DeployTarget, error)
}

// LocalDeploys is the break-glass surface: recording a deploy that was driven
// from a machine instead of GitHub Actions, so the operations console still
// knows what is live. It records; it deploys nothing.
type LocalDeploys interface {
	RecordLocal(ctx context.Context, in deployops.LocalRunInput) (domain.DeploymentRun, error)
}

type ToolKit struct {
	Incidents    Incidents
	Deploys      Deploys
	LocalDeploys LocalDeploys
}

// NewExecutors returns the ops tools that the wired dependencies can serve.
// Deploy template tools need no service at all — the catalog is embedded.
func NewExecutors(kit *ToolKit) []port.ToolExecutor {
	if kit == nil {
		return nil
	}
	out := []port.ToolExecutor{
		&listDeployTemplatesTool{},
		&loadDeployTemplateTool{},
	}
	if kit.Incidents != nil {
		out = append(out,
			&listIncidentsTool{kit: kit},
			&getIncidentTool{kit: kit},
			&proposeRemedyTool{kit: kit},
			&resolveIncidentTool{kit: kit},
		)
	}
	if kit.Deploys != nil {
		out = append(out, &deployTargetTool{kit: kit}, &updateDeployTargetTool{kit: kit})
	}
	if kit.LocalDeploys != nil {
		out = append(out, &recordLocalDeployTool{kit: kit})
	}
	return out
}

// NewLocalDeployExecutors returns just the break-glass recorder. It exists
// because the deploy console service is wired later in runtime than the rest
// of the ops kit (it needs a resolvable GitHub token), and calling
// NewExecutors a second time would re-register the deploy template tools.
func NewLocalDeployExecutors(local LocalDeploys) []port.ToolExecutor {
	if local == nil {
		return nil
	}
	return []port.ToolExecutor{&recordLocalDeployTool{kit: &ToolKit{LocalDeploys: local}}}
}

const (
	listIncidentsToolName       = "list_incidents"
	getIncidentToolName         = "get_incident"
	proposeRemedyToolName       = "propose_incident_remedy"
	resolveIncidentToolName     = "resolve_incident"
	listDeployTemplatesToolName = "list_deploy_templates"
	loadDeployTemplateToolName  = "load_deploy_template"
	deployTargetToolName        = "get_deploy_target"
	updateDeployTargetToolName  = "update_deploy_target"
	recordLocalDeployToolName   = "record_local_deploy"
)

// ---------------------------------------------------------------- incidents

type listIncidentsTool struct{ kit *ToolKit }

func (t *listIncidentsTool) Name() string { return listIncidentsToolName }

func (t *listIncidentsTool) Definition() domain.ToolDefinition {
	return fn(listIncidentsToolName, "", map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"repository_id": map[string]any{"type": "string"},
			"env":           map[string]any{"type": "string"},
			"status": map[string]any{
				"type": "string",
			},
		},
	})
}

func (t *listIncidentsTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args struct {
		RepositoryID string `json:"repository_id"`
		Env          string `json:"env"`
		Status       string `json:"status"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return errResult(listIncidentsToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	filter := domain.IncidentFilter{Env: strings.TrimSpace(args.Env), Limit: 50}
	// Listing is the one ops tool that can answer without a repository at all,
	// and omitting the argument has always meant "every repository" — scoping
	// an omission to the run's repository would hide the cross-repo view an
	// operator asks for by leaving it out. Only a value the model DID pass and
	// we could not parse (a repository name) is read as "the one I am working
	// on", and even that narrows nothing when the run has no repository.
	if strings.TrimSpace(args.RepositoryID) != "" {
		if id, ok := resolveRepositoryID(ctx, args.RepositoryID, false); ok {
			filter.RepositoryID = &id
		}
	}
	if strings.TrimSpace(args.Status) == "" {
		filter.Statuses = []domain.IncidentStatus{
			domain.IncidentStatusOpen, domain.IncidentStatusTriaging,
			domain.IncidentStatusProposed, domain.IncidentStatusFixing,
		}
	} else {
		for _, s := range strings.Split(args.Status, ",") {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			if !domain.ValidIncidentStatus(domain.IncidentStatus(s)) {
				return errResult(listIncidentsToolName, "invalid status: "+s)
			}
			filter.Statuses = append(filter.Statuses, domain.IncidentStatus(s))
		}
	}
	incidents, err := t.kit.Incidents.List(ctx, filter)
	if err != nil {
		return errResult(listIncidentsToolName, err.Error())
	}
	summaries := make([]map[string]any, 0, len(incidents))
	for _, inc := range incidents {
		summaries = append(summaries, map[string]any{
			"id":          inc.ID,
			"env":         inc.Env,
			"title":       inc.Title,
			"severity":    inc.Severity,
			"status":      inc.Status,
			"occurrences": inc.Occurrences,
			"remedy_kind": inc.RemedyKind,
			// Whose proposal this is: auto_triage means nobody has diagnosed it
			// yet, only the rules engine.
			"remedy_author": inc.RemedyAuthor,
			"last_seen":     inc.LastSeenAt,
		})
	}
	return jsonResult(listIncidentsToolName, map[string]any{"incidents": summaries, "count": len(summaries)})
}

type getIncidentTool struct{ kit *ToolKit }

func (t *getIncidentTool) Name() string { return getIncidentToolName }

func (t *getIncidentTool) Definition() domain.ToolDefinition {
	return fn(getIncidentToolName, "", map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"incident_id"},
		"properties": map[string]any{
			"incident_id": map[string]any{"type": "string"},
		},
	})
}

func (t *getIncidentTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	id, res := parseID(getIncidentToolName, arguments)
	if res != nil {
		return *res
	}
	incident, err := t.kit.Incidents.Get(ctx, id)
	if err != nil {
		return errResult(getIncidentToolName, err.Error())
	}
	return jsonResult(getIncidentToolName, incident)
}

type proposeRemedyTool struct{ kit *ToolKit }

func (t *proposeRemedyTool) Name() string { return proposeRemedyToolName }

func (t *proposeRemedyTool) Definition() domain.ToolDefinition {
	return fn(proposeRemedyToolName, "", map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"incident_id", "kind", "summary"},
		"properties": map[string]any{
			"incident_id": map[string]any{"type": "string"},
			"kind": map[string]any{
				"type": "string",
				"enum": []string{domain.RemedyKindRollback, domain.RemedyKindCodeFix, domain.RemedyKindConfig, domain.RemedyKindDependency, domain.RemedyKindCapacity, domain.RemedyKindUnknown},
			},
			"summary": map[string]any{"type": "string"},
			"steps": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string"},
			},
			"evidence": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string"},
			},
			"confidence": map[string]any{"type": "integer"},
			"rollback":   map[string]any{"type": "boolean"},
		},
	})
}

func (t *proposeRemedyTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args struct {
		IncidentID string   `json:"incident_id"`
		Kind       string   `json:"kind"`
		Summary    string   `json:"summary"`
		Steps      []string `json:"steps"`
		Evidence   []string `json:"evidence"`
		Confidence int      `json:"confidence"`
		Rollback   bool     `json:"rollback"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return errResult(proposeRemedyToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	id, err := uuid.Parse(strings.TrimSpace(args.IncidentID))
	if err != nil {
		return errResult(proposeRemedyToolName, "invalid incident_id")
	}
	if strings.TrimSpace(args.Summary) == "" {
		return errResult(proposeRemedyToolName, "summary is required — state the root cause and the fix")
	}
	if args.Confidence < 0 || args.Confidence > 100 {
		return errResult(proposeRemedyToolName, "confidence must be between 0 and 100")
	}
	incident, err := t.kit.Incidents.ProposeRemedy(ctx, id, domain.Remedy{
		Kind:       strings.TrimSpace(args.Kind),
		Summary:    strings.TrimSpace(args.Summary),
		Steps:      args.Steps,
		Evidence:   args.Evidence,
		Confidence: args.Confidence,
		Rollback:   args.Rollback,
	}, domain.RemedyAuthorAgent)
	if err != nil {
		return errResult(proposeRemedyToolName, err.Error())
	}
	return jsonResult(proposeRemedyToolName, map[string]any{
		"incident_id": incident.ID,
		"status":      incident.Status,
		"recorded":    true,
	})
}

type resolveIncidentTool struct{ kit *ToolKit }

func (t *resolveIncidentTool) Name() string { return resolveIncidentToolName }

func (t *resolveIncidentTool) Definition() domain.ToolDefinition {
	return fn(resolveIncidentToolName, "", map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"incident_id", "note"},
		"properties": map[string]any{
			"incident_id": map[string]any{"type": "string"},
			"note":        map[string]any{"type": "string"},
		},
	})
}

func (t *resolveIncidentTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args struct {
		IncidentID string `json:"incident_id"`
		Note       string `json:"note"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return errResult(resolveIncidentToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	id, err := uuid.Parse(strings.TrimSpace(args.IncidentID))
	if err != nil {
		return errResult(resolveIncidentToolName, "invalid incident_id")
	}
	if strings.TrimSpace(args.Note) == "" {
		return errResult(resolveIncidentToolName, "note is required — say what fixed it and how you verified recovery")
	}
	incident, err := t.kit.Incidents.Resolve(ctx, id, args.Note)
	if err != nil {
		return errResult(resolveIncidentToolName, err.Error())
	}
	return jsonResult(resolveIncidentToolName, map[string]any{"incident_id": incident.ID, "status": incident.Status})
}

// ------------------------------------------------------------------- deploy

type listDeployTemplatesTool struct{}

func (t *listDeployTemplatesTool) Name() string { return listDeployTemplatesToolName }

func (t *listDeployTemplatesTool) Definition() domain.ToolDefinition {
	return fn(listDeployTemplatesToolName, "", map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"kind": map[string]any{"type": "string"},
		},
	})
}

func (t *listDeployTemplatesTool) Execute(_ context.Context, arguments string) domain.ToolResult {
	var args struct {
		Kind string `json:"kind"`
	}
	_ = json.Unmarshal([]byte(arguments), &args)
	templates := deploy.Templates()
	if strings.TrimSpace(args.Kind) != "" {
		templates = deploy.TemplatesForKind(strings.TrimSpace(args.Kind))
	}
	out := make([]map[string]any, 0, len(templates))
	for _, tpl := range templates {
		out = append(out, map[string]any{
			"id":            tpl.ID,
			"provider":      tpl.Provider,
			"name":          tpl.Name,
			"summary":       tpl.Summary,
			"workflow_file": tpl.WorkflowFile,
			"required_vars": tpl.RequiredVars,
		})
	}
	return jsonResult(listDeployTemplatesToolName, map[string]any{"templates": out, "count": len(out)})
}

type loadDeployTemplateTool struct{}

func (t *loadDeployTemplateTool) Name() string { return loadDeployTemplateToolName }

func (t *loadDeployTemplateTool) Definition() domain.ToolDefinition {
	return fn(loadDeployTemplateToolName, "", map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"template"},
		"properties": map[string]any{
			"template": map[string]any{"type": "string"},
		},
	})
}

func (t *loadDeployTemplateTool) Execute(_ context.Context, arguments string) domain.ToolResult {
	var args struct {
		Template string `json:"template"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return errResult(loadDeployTemplateToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	tpl, ok := deploy.Template(args.Template)
	if !ok {
		var ids []string
		for _, candidate := range deploy.Templates() {
			ids = append(ids, candidate.ID)
		}
		return errResult(loadDeployTemplateToolName, "unknown template: "+args.Template+". Available: "+strings.Join(ids, ", "))
	}
	return jsonResult(loadDeployTemplateToolName, tpl)
}

type deployTargetTool struct{ kit *ToolKit }

func (t *deployTargetTool) Name() string { return deployTargetToolName }

func (t *deployTargetTool) Definition() domain.ToolDefinition {
	return fn(deployTargetToolName, "", map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"repository_id": map[string]any{"type": "string"},
			"env":           map[string]any{"type": "string"},
		},
	})
}

func (t *deployTargetTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args struct {
		RepositoryID string `json:"repository_id"`
		Env          string `json:"env"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return errResult(deployTargetToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	// Reading is lenient: the worst a guess costs here is one tool call spent
	// on the wrong repository's target, which the model can see and correct.
	repoID, ok := resolveRepositoryID(ctx, args.RepositoryID, false)
	if !ok {
		return errResult(deployTargetToolName, repositoryIDHelp)
	}
	targets, err := t.kit.Deploys.Targets(ctx, repoID)
	if err != nil {
		return errResult(deployTargetToolName, err.Error())
	}
	env := strings.TrimSpace(args.Env)
	if env == "" {
		return jsonResult(deployTargetToolName, map[string]any{"targets": targets, "count": len(targets)})
	}
	for _, target := range targets {
		if target.Env != env {
			continue
		}
		instructions, err := t.kit.Deploys.Instructions(ctx, repoID, env)
		if err != nil {
			instructions = "no template rendered: " + err.Error()
		}
		return jsonResult(deployTargetToolName, map[string]any{
			"target":       target,
			"instructions": instructions,
		})
	}
	return errResult(deployTargetToolName, "no deploy target configured for env "+env)
}

type updateDeployTargetTool struct{ kit *ToolKit }

func (t *updateDeployTargetTool) Name() string { return updateDeployTargetToolName }

func (t *updateDeployTargetTool) Definition() domain.ToolDefinition {
	return fn(updateDeployTargetToolName, "", map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"env"},
		"properties": map[string]any{
			"repository_id": map[string]any{"type": "string"},
			"env":           map[string]any{"type": "string", "enum": domain.DeployEnvs()},
			"base_url": map[string]any{
				"type": "string",
			},
			"health_url": map[string]any{
				"type": "string",
			},
			"logs_url": map[string]any{
				"type": "string",
			},
			"app_url": map[string]any{
				"type": "string",
			},
		},
	})
}

func (t *updateDeployTargetTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	// Pointers, not strings: an omitted field must leave the stored value
	// alone, while an explicit "" is a deliberate clear.
	var args struct {
		RepositoryID string  `json:"repository_id"`
		Env          string  `json:"env"`
		BaseURL      *string `json:"base_url"`
		HealthURL    *string `json:"health_url"`
		LogsURL      *string `json:"logs_url"`
		AppURL       *string `json:"app_url"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return errResult(updateDeployTargetToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	// Writing is strict: a present-but-unparseable id names a repository the
	// agent meant, and falling back would record this URL against a different
	// one silently. Only an omitted id may mean "the one I am working in".
	repoID, ok := resolveRepositoryID(ctx, args.RepositoryID, true)
	if !ok {
		return errResult(updateDeployTargetToolName, repositoryIDHelp)
	}
	env := strings.TrimSpace(args.Env)
	if !domain.ValidDeployEnv(env) {
		return errResult(updateDeployTargetToolName, "env must be one of stage, preprod, prod")
	}
	if args.BaseURL == nil && args.HealthURL == nil && args.LogsURL == nil && args.AppURL == nil {
		return errResult(updateDeployTargetToolName, "nothing to record: pass base_url, health_url, logs_url or app_url")
	}
	target, err := t.kit.Deploys.RecordTargetURLs(ctx, repoID, env, deploy.RecordTargetURLsInput{
		BaseURL:   args.BaseURL,
		HealthURL: args.HealthURL,
		AppURL:    args.AppURL,
		LogsURL:   args.LogsURL,
	})
	if err != nil {
		return errResult(updateDeployTargetToolName, err.Error())
	}
	return jsonResult(updateDeployTargetToolName, map[string]any{
		"env":        target.Env,
		"base_url":   target.BaseURL,
		"health_url": target.HealthURL,
		"logs_url":   target.LogsURL,
		"app_url":    target.AppURL,
		"recorded":   true,
	})
}

// ------------------------------------------------------------------ helpers

// The three repository_id params' wording (lenient-reader, strict-writer, and
// list_incidents' own "omit means every repository") now lives in each
// tool's own catalog/system/tools/<name>.md — this file no longer holds any
// of it, in Go string constants or otherwise.

// ------------------------------------------------------- local (break-glass)

type recordLocalDeployTool struct{ kit *ToolKit }

func (t *recordLocalDeployTool) Name() string { return recordLocalDeployToolName }

func (t *recordLocalDeployTool) Definition() domain.ToolDefinition {
	return fn(recordLocalDeployToolName, "", map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"env", "status"},
		"properties": map[string]any{
			"repository_id": map[string]any{
				"type": "string",
			},
			"env":     map[string]any{"type": "string"},
			"status":  map[string]any{"type": "string"},
			"run_id":  map[string]any{"type": "integer"},
			"command": map[string]any{"type": "string"},
			"head_sha": map[string]any{
				"type": "string",
			},
			"head_ref": map[string]any{"type": "string"},
			"conclusion": map[string]any{
				"type": "string",
			},
		},
	})
}

func (t *recordLocalDeployTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args struct {
		RepositoryID string `json:"repository_id"`
		Env          string `json:"env"`
		Status       string `json:"status"`
		RunID        int64  `json:"run_id"`
		Command      string `json:"command"`
		HeadSHA      string `json:"head_sha"`
		HeadRef      string `json:"head_ref"`
		Conclusion   string `json:"conclusion"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return errResult(recordLocalDeployToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	// A writer: a repository_id that was passed but is not a UUID is refused
	// rather than silently redirected at the run's own repository, for the
	// same reason update_deploy_target refuses it — this writes a row that
	// says "repo X is live at this commit".
	repositoryID, ok := resolveRepositoryID(ctx, args.RepositoryID, true)
	if !ok {
		return errResult(recordLocalDeployToolName, repositoryIDHelp)
	}
	run, err := t.kit.LocalDeploys.RecordLocal(ctx, deployops.LocalRunInput{
		RepositoryID: repositoryID,
		Env:          strings.TrimSpace(args.Env),
		RunID:        args.RunID,
		Command:      args.Command,
		HeadSHA:      args.HeadSHA,
		HeadRef:      args.HeadRef,
		Status:       strings.TrimSpace(args.Status),
		Conclusion:   strings.TrimSpace(args.Conclusion),
		Actor:        localDeployActor(ctx),
	})
	if err != nil {
		return errResult(recordLocalDeployToolName, err.Error())
	}
	// run_id is echoed first because the completed call has to pass it back.
	return jsonResult(recordLocalDeployToolName, map[string]any{
		"run_id":     run.RunID,
		"env":        run.Env,
		"status":     run.Status,
		"conclusion": run.Conclusion,
		"head_sha":   run.HeadSHA,
		"recorded":   "visible in Operations → Changes as a local deploy",
	})
}

// localDeployActor names who a recorded local deploy is attributed to. The
// signed human actor is used when the gateway resolved one (the SPA drove
// this run); otherwise this is an agent run with no human attached, and
// "agent" is the same label board comments and tasks use for that case.
func localDeployActor(ctx context.Context) string {
	if uid := strings.TrimSpace(registry.ActorUserIDFromContext(ctx)); uid != "" {
		return uid
	}
	return "agent"
}

// repositoryIDHelp is what a call that named no resolvable repository gets
// back: both ways out, because the model that got here either passed nothing
// or passed something that was not a UUID.
var repositoryIDHelp = prompt.OpsRepositoryIDInvalidText()

// resolveRepositoryID turns whatever the model put in repository_id into the
// repository the call should act on.
//
// Agents pass the repository NAME ("agent-server") about as often as the UUID,
// and the run already knows which repository it is working in — so for a READER
// a value that is not a usable UUID is not a reason to fail, it is a reason to
// fall back to the run's own repository. Reading the wrong repository's deploy
// target costs one wasted tool call.
//
// strict is for the WRITERS, where that fallback is the bug: "web-frontend" is
// a repository the agent meant, so quietly writing to the run's repository
// instead stamps repo B's URL onto repo A's target and nothing in the transcript
// says so. There, only an ABSENT argument may fall back; a present one must be
// a UUID or be refused.
//
// Matching a name against the real repositories would be the step in between,
// but the ops ToolKit holds incidents and deploys and no repository lister, so
// there is nothing here to match a name against.
func resolveRepositoryID(ctx context.Context, raw string, strict bool) (uuid.UUID, bool) {
	if trimmed := strings.TrimSpace(raw); trimmed != "" {
		id, err := uuid.Parse(trimmed)
		if err == nil && id != uuid.Nil {
			return id, true
		}
		if strict {
			return uuid.Nil, false
		}
	}
	if id := registry.RepositoryIDFromContext(ctx); id != uuid.Nil {
		return id, true
	}
	return uuid.Nil, false
}

func fn(name, description string, parameters map[string]any) domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name:        name,
			Description: description,
			Parameters:  parameters,
		},
	}
}

func parseID(tool, arguments string) (uuid.UUID, *domain.ToolResult) {
	var args struct {
		IncidentID string `json:"incident_id"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		res := errResult(tool, fmt.Sprintf("invalid arguments: %v", err))
		return uuid.Nil, &res
	}
	id, err := uuid.Parse(strings.TrimSpace(args.IncidentID))
	if err != nil {
		res := errResult(tool, "invalid incident_id")
		return uuid.Nil, &res
	}
	return id, nil
}

func jsonResult(tool string, payload any) domain.ToolResult {
	body, err := json.Marshal(payload)
	if err != nil {
		return errResult(tool, fmt.Sprintf("marshal response: %v", err))
	}
	return domain.ToolResult{Name: tool, Content: string(body)}
}

func errResult(tool, message string) domain.ToolResult {
	return domain.ToolResult{Name: tool, Content: message, IsError: true}
}
