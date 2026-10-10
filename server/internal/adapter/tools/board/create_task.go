package board

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const createBoardTaskToolName = "create_board_task"

type createTaskArgs struct {
	Title                string `json:"title"`
	TaskType             string `json:"task_type"`
	Description          string `json:"description"`
	TechnicalDescription string `json:"technical_description"`
	// AcceptanceCriteria is the structured checklist QA and pm_uat verify
	// against. It has its own board field, so criteria belong here and not
	// pasted into description.
	AcceptanceCriteria []string `json:"acceptance_criteria"`
	Column             string   `json:"column"`
	Priority           string   `json:"priority"`
	Assignee           string   `json:"assignee"`
	// AssigneeRole is the optional role-key alternative to Assignee: resolved
	// through RoleResolver.AgentForRole against the task's repository area.
	// Ignored when Assignee is also set.
	AssigneeRole string `json:"assignee_role"`
	// Repository and Project accept a name or a UUID. RepositoryID and
	// InitiativeProjectID are the original UUID-only spellings, kept so existing
	// callers and stored plans keep working.
	Repository          string `json:"repository"`
	Project             string `json:"project"`
	RepositoryID        string `json:"repository_id"`
	InitiativeProjectID string `json:"initiative_project_id"`
	// Component narrows the task to one component of a monorepo, by its
	// repository-relative path ("." for the root). Ignored when the deployment
	// has no project model.
	Component string `json:"component"`
	// AllowDuplicate opts out of the same-work guard for the rare case where a
	// near-identically titled open task is genuinely a separate deliverable.
	AllowDuplicate bool `json:"allow_duplicate"`
	// Deploy runbook and deploy ordering. Both belong on the task from the
	// moment it is planned: the agent that knows the API must ship before the
	// client is the one writing these two tasks, and it knows it now.
	BeforeDeploy    string   `json:"before_deploy"`
	AfterDeploy     string   `json:"after_deploy"`
	RollbackPlan    string   `json:"rollback_plan"`
	DeployDependsOn []string `json:"deploy_depends_on"`
	// BlockedBy is the WORK order — who writes code first — where
	// DeployDependsOn is the shipping order. Both point the same way ("this
	// task comes after those") so the agent never has to reason about which end
	// of a relation it is holding.
	BlockedBy []string `json:"blocked_by"`
	// DerivedFrom is the analiz task this implementation task was opened out of.
	// A list because a task can genuinely implement two analyses; in practice it
	// is one.
	DerivedFrom []string `json:"derived_from"`
}

// repositoryRef / projectRef prefer the name-or-UUID argument and fall back to
// the legacy UUID-only one.
func (a createTaskArgs) repositoryRef() string {
	if strings.TrimSpace(a.Repository) != "" {
		return a.Repository
	}
	return a.RepositoryID
}

func (a createTaskArgs) projectRef() string {
	if strings.TrimSpace(a.Project) != "" {
		return a.Project
	}
	return a.InitiativeProjectID
}

type createTaskTool struct {
	kit *ToolKit
}

func newCreateTaskTool(kit *ToolKit) port.ToolExecutor {
	return &createTaskTool{kit: kit}
}

func (t *createTaskTool) Name() string {
	return createBoardTaskToolName
}

func (t *createTaskTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: createBoardTaskToolName,
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"title": map[string]interface{}{
						"type": "string",
					},
					"task_type": map[string]interface{}{
						"type": "string",
					},
					"assignee_role": map[string]interface{}{
						"type": "string",
					},
					"description": map[string]interface{}{
						"type": "string",
					},
					"technical_description": map[string]interface{}{
						"type": "string",
					},
					"acceptance_criteria": map[string]interface{}{
						"type":  "array",
						"items": map[string]interface{}{"type": "string"},
					},
					"column": map[string]interface{}{
						"type": "string",
					},
					"priority": map[string]interface{}{
						"type": "string",
						"enum": []string{"low", "medium", "high", "critical"},
					},
					"assignee": map[string]interface{}{
						"type": "string",
					},
					"repository": map[string]interface{}{
						"type": "string",
					},
					"project": map[string]interface{}{
						"type": "string",
					},
					"repository_id": map[string]interface{}{
						"type": "string",
					},
					"initiative_project_id": map[string]interface{}{
						"type": "string",
					},
					"component": map[string]interface{}{
						"type": "string",
					},
					"allow_duplicate": map[string]interface{}{
						"type": "boolean",
					},
					"before_deploy": map[string]interface{}{
						"type": "string",
					},
					"after_deploy": map[string]interface{}{
						"type": "string",
					},
					"rollback_plan": map[string]interface{}{
						"type": "string",
					},
					"deploy_depends_on": map[string]interface{}{
						"type":  "array",
						"items": map[string]interface{}{"type": "string"},
					},
					"blocked_by": map[string]interface{}{
						"type":  "array",
						"items": map[string]interface{}{"type": "string"},
					},
					"derived_from": map[string]interface{}{
						"type":  "array",
						"items": map[string]interface{}{"type": "string"},
					},
				},
				"required": []string{"title"},
			},
		},
	}
}

func (t *createTaskTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args createTaskArgs
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(createBoardTaskToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	if args.Title == "" {
		return toolError(createBoardTaskToolName, "title is required")
	}
	var explicitRepository *uuid.UUID
	if ref := args.repositoryRef(); strings.TrimSpace(ref) != "" {
		parsed, err := t.kit.resolveRepositoryRef(ctx, ref)
		if err != nil {
			return toolError(createBoardTaskToolName, err.Error())
		}
		explicitRepository = &parsed
	}
	repositoryID, err := t.kit.resolveCreateRepositoryID(ctx, explicitRepository)
	if err != nil {
		return toolError(createBoardTaskToolName, err.Error())
	}
	if !args.AllowDuplicate {
		if existing, found := t.kit.findDuplicateTask(ctx, repositoryID, args.Title); found {
			return toolJSON(createBoardTaskToolName, map[string]any{
				"created": false,
				"reason":  "an open task already covers this work",
				"task":    existing,
				"hint":    duplicateTaskHintKey.Render(struct{}{}),
			})
		}
	}
	col := domain.TaskColumnBacklog
	if args.Column != "" {
		col = domain.TaskColumn(args.Column)
		if !domain.ValidTaskColumn(col) {
			return toolError(createBoardTaskToolName, fmt.Sprintf("invalid column: %s", args.Column))
		}
	}
	criteria, droppedCriteria := criteriaInputs(args.AcceptanceCriteria)
	req := domain.CreateBoardTaskRequest{
		Title:                args.Title,
		TechnicalDescription: args.TechnicalDescription,
		AcceptanceCriteria:   criteria,
		Column:               col,
		CreatedBy:            "agent",
	}
	if args.TaskType != "" {
		taskType := domain.TaskType(args.TaskType)
		if t.kit.Workflows != nil {
			if exists, terr := t.kit.Workflows.TaskTypeExists(ctx, taskType); terr == nil && !exists {
				return toolError(createBoardTaskToolName, fmt.Sprintf("unknown task_type %q", args.TaskType))
			}
		}
		req.TaskType = taskType
	}
	if args.Priority != "" {
		req.Priority = domain.TaskPriority(args.Priority)
	}
	if strings.TrimSpace(args.Component) != "" {
		componentID, cerr := t.kit.resolveComponentRef(ctx, repositoryID, args.Component)
		if cerr != nil {
			return toolError(createBoardTaskToolName, cerr.Error())
		}
		req.ComponentID = componentID
	}
	if ref := args.projectRef(); strings.TrimSpace(ref) != "" {
		parsed, err := t.kit.resolveProjectRef(ctx, ref)
		if err != nil {
			return toolError(createBoardTaskToolName, err.Error())
		}
		req.InitiativeProjectID = &parsed
	}
	if args.Assignee != "" {
		assigneeID, err := t.resolveAssignee(ctx, args.Assignee)
		if err != nil {
			return toolError(createBoardTaskToolName, err.Error())
		}
		if refusal := t.kit.disabledAssigneeRefusal(ctx, assigneeID); refusal != "" {
			return toolError(createBoardTaskToolName, refusal)
		}
		req.AssigneeAgentID = &assigneeID
	} else if args.AssigneeRole != "" {
		roleAssignee, err := t.resolveAssigneeRole(ctx, args.AssigneeRole, repositoryID)
		if err != nil {
			return toolError(createBoardTaskToolName, err.Error())
		}
		if roleAssignee != nil {
			if refusal := t.kit.disabledRoleRefusal(ctx, args.AssigneeRole, *roleAssignee); refusal != "" {
				return toolError(createBoardTaskToolName, refusal)
			}
			req.AssigneeAgentID = roleAssignee
		}
	}
	if args.BeforeDeploy != "" {
		req.BeforeDeploy = &args.BeforeDeploy
	}
	if args.AfterDeploy != "" {
		req.AfterDeploy = &args.AfterDeploy
	}
	if args.RollbackPlan != "" {
		req.RollbackPlan = &args.RollbackPlan
	}
	if len(args.DeployDependsOn) > 0 {
		deps, depErr := t.kit.resolveDeployDependencies(ctx, args.DeployDependsOn)
		if depErr != nil {
			return toolError(createBoardTaskToolName, depErr.Error())
		}
		req.Relations = append(req.Relations, deps...)
	}
	if len(args.DerivedFrom) > 0 {
		refs, refErr := t.kit.resolveRelationRefs(ctx, args.DerivedFrom, domain.TaskRelationDerivedFrom, "derived_from")
		if refErr != nil {
			return toolError(createBoardTaskToolName, refErr.Error())
		}
		req.Relations = append(req.Relations, refs...)
	}
	if origin := registry.TaskIDFromContext(ctx); origin != uuid.Nil {
		// Provenance the agent cannot forget: derived_from already says origin
		// is this task's specification, so writing discovered_from too would
		// assert two different relationships to the same task — the stronger
		// claim wins.
		derivedFromOrigin := false
		for _, rel := range req.Relations {
			if rel.RelationType == domain.TaskRelationDerivedFrom && rel.TargetTaskID == origin {
				derivedFromOrigin = true
				break
			}
		}
		if !derivedFromOrigin {
			req.Relations = append(req.Relations, domain.TaskRelationInput{
				TargetTaskID: origin,
				RelationType: domain.TaskRelationDiscoveredFrom,
			})
		}
	}
	if len(args.BlockedBy) > 0 {
		// Not appended to req.Relations: a blocks row is stored with the BLOCKER
		// as its source, and everything in Relations shares the new task as
		// theirs. See domain.CreateBoardTaskRequest.BlockedBy.
		blockers, blockErr := t.kit.resolveRelationRefs(ctx, args.BlockedBy, domain.TaskRelationBlocks, "blocked_by")
		if blockErr != nil {
			return toolError(createBoardTaskToolName, blockErr.Error())
		}
		req.BlockedBy = blockers
	}
	task, err := t.kit.Tasks.CreateTask(ctx, repositoryID, req)
	if errors.Is(err, domain.ErrAssigneeNotAssignable) {
		return toolError(createBoardTaskToolName, assigneeNotAssignableKey.Render(struct{}{}))
	}
	if err != nil {
		return toolError(createBoardTaskToolName, err.Error())
	}
	if len(droppedCriteria) > 0 {
		// Shape only changes when something was removed: the plain task object
		// is what every caller reading this result already expects.
		return toolJSON(createBoardTaskToolName, map[string]any{
			"task":             task,
			"dropped_criteria": droppedCriteria,
			"hint":             criteriaHint(droppedCriteria),
		})
	}
	return toolJSON(createBoardTaskToolName, task)
}

// resolveAssignee turns an assignee argument — either an agent UUID or an agent
// name like "system-architect" — into an agent id, so the task is dispatched to
// that agent on creation. Name lookup is case-insensitive and needs the Team
// lister; if it is unavailable or the name is unknown, it returns an error
// naming the valid options rather than silently leaving the task unassigned.
func (t *createTaskTool) resolveAssignee(ctx context.Context, assignee string) (uuid.UUID, error) {
	if id, err := uuid.Parse(assignee); err == nil {
		return id, nil
	}
	if t.kit.Team == nil {
		return uuid.Nil, fmt.Errorf("assignee %q is not a UUID and the team roster is unavailable to resolve it", assignee)
	}
	agents, err := t.kit.Team.ListAgents(ctx)
	if err != nil {
		return uuid.Nil, fmt.Errorf("resolve assignee: %v", err)
	}
	names := make([]string, 0, len(agents))
	for _, a := range agents {
		if strings.EqualFold(a.Name, assignee) {
			return a.ID, nil
		}
		names = append(names, a.Name)
	}
	return uuid.Nil, fmt.Errorf("unknown assignee %q; valid agents: %s", assignee, strings.Join(names, ", "))
}

// roleByKeyLookup is the narrow extra surface application/workflow.Service
// offers beyond port.RoleResolver — resolving a role's key (what an agent
// types) to its id (what AgentForRole needs). Asserted against t.kit.Roles
// rather than added to port.RoleResolver, since every other RoleResolver
// caller is the hot dispatch path and has no key to resolve from.
type roleByKeyLookup interface {
	RoleByKey(ctx context.Context, key string) (domain.AgentRole, error)
}

// resolveAssigneeRole resolves the optional assignee_role argument: the named
// role's agent for repositoryID's area, or nil (leave the caller's assignee —
// none, here — alone) when roles are unavailable, the role is unknown, or it
// has nobody assigned for that area.
func (t *createTaskTool) resolveAssigneeRole(ctx context.Context, roleKey string, repositoryID uuid.UUID) (*uuid.UUID, error) {
	if t.kit.Roles == nil {
		return nil, fmt.Errorf("assignee_role %q was given but roles are not available on this deployment", roleKey)
	}
	lookup, ok := t.kit.Roles.(roleByKeyLookup)
	if !ok {
		return nil, fmt.Errorf("assignee_role %q was given but role lookup is not available on this deployment", roleKey)
	}
	role, err := lookup.RoleByKey(ctx, roleKey)
	if err != nil {
		return nil, fmt.Errorf("unknown assignee_role %q", roleKey)
	}
	return t.kit.Roles.AgentForRole(ctx, role.ID, t.kit.repoArea(ctx, repositoryID))
}

// repoArea resolves a repository's backend/frontend/mobile area for role
// routing. "" (any-area fallback) when the workspace lister is unavailable or
// the repository cannot be found in it — a role assignment scoped to a
// specific area then simply does not match, which AgentForRole already
// handles as "no assignment here".
func (kit *ToolKit) repoArea(ctx context.Context, repositoryID uuid.UUID) string {
	if kit.Workspace == nil {
		return ""
	}
	repos, err := kit.Workspace.ListRepositories(ctx)
	if err != nil {
		return ""
	}
	for _, r := range repos {
		if r.ID == repositoryID {
			return domain.RepoArea(r.Kind, r.SubProjects)
		}
	}
	return ""
}
