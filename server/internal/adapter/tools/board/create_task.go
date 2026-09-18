package board

import (
	"context"
	"encoding/json"
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
	// Repository and Project accept a name or a UUID. RepositoryID and
	// InitiativeProjectID are the original UUID-only spellings, kept so existing
	// callers and stored plans keep working.
	Repository          string `json:"repository"`
	Project             string `json:"project"`
	RepositoryID        string `json:"repository_id"`
	InitiativeProjectID string `json:"initiative_project_id"`
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
			Name:        createBoardTaskToolName,
			Description: "Create a new task on the board. Always set repository (which codebase the work touches) and project (which initiative it belongs to) when you can tell — call list_repositories / list_projects to find them rather than asking the user. Leaving repository unset falls back to the active repository context, or to the default repository, which may well be the wrong one. When you are splitting work that has an order, put it in the ARGUMENTS and not only in prose — blocked_by (nobody starts this task until those are done) and deploy_depends_on (this task ships AFTER those) are both enforced, a sentence in the description is not. When you open this task out of an analysis, set derived_from to that analiz task: the agent that picks this up is then handed that analysis's documents to work from. Inside a task run, the task you create here is linked back to the task you are working on as discovered_from automatically — there is no argument for it, and you do not need to (or need to try to) set it yourself. Anything that has to happen around the deploy belongs in before_deploy / after_deploy / rollback_plan, not in a comment: those fields are posted automatically when the release is dispatched and when it lands.",
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"title": map[string]interface{}{
						"type":        "string",
						"description": "Task title",
					},
					"task_type": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"task", "analiz", "bug", "technical"},
						"description": "Task type. \"technical\" is for pure backend/infra work with no UI-facing behaviour: it skips pm_uat and goes straight from QA to human_uat.",
					},
					"description": map[string]interface{}{
						"type":        "string",
						"description": "Product-level description (markdown): the user story, context and out-of-scope notes. Do NOT paste acceptance criteria or technical detail here — they have their own fields (acceptance_criteria, technical_description) and repeating them makes the task drift out of sync.",
					},
					"technical_description": map[string]interface{}{
						"type":        "string",
						"description": "Technical detail (markdown): affected endpoints/files/schema, approach, constraints. This is the ONLY place technical detail belongs — do not repeat it in description.",
					},
					"acceptance_criteria": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Acceptance criteria, one observable Given/When/Then per array item, in order. This is the ONLY place criteria belong — they become the checklist QA executes and pm_uat verifies. Writing them into description instead leaves the task with an empty checklist.",
					},
					"column": map[string]interface{}{
						"type":        "string",
						"description": "Board column (default: backlog)",
					},
					"priority": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"low", "medium", "high", "critical"},
						"description": "Task priority",
					},
					"assignee": map[string]interface{}{
						"type":        "string",
						"description": "Who handles this task: the responsible agent's name (e.g. \"system-architect\", \"backend-developer\") or its UUID. Set this so the assignee is dispatched automatically. Use list_team to see valid names.",
					},
					"repository": map[string]interface{}{
						"type":        "string",
						"description": "Which codebase this task touches: repository name (e.g. \"acme-web\") or its UUID. Use list_repositories to see valid names. Omit only when the task genuinely touches no repository.",
					},
					"project": map[string]interface{}{
						"type":        "string",
						"description": "Which initiative this task belongs to: project name or its UUID. Use list_projects to see valid names, or create_project when the initiative does not exist yet.",
					},
					"repository_id": map[string]interface{}{
						"type":        "string",
						"description": "Deprecated alias for repository (UUID only).",
					},
					"initiative_project_id": map[string]interface{}{
						"type":        "string",
						"description": "Deprecated alias for project (UUID only).",
					},
					"allow_duplicate": map[string]interface{}{
						"type":        "boolean",
						"description": "Set true only when an open task with an almost identical title is genuinely different work. Creation is otherwise refused and the existing task is returned, so parallel agents cannot open the same task twice.",
					},
					"before_deploy": map[string]interface{}{
						"type":        "string",
						"description": "Pre-deploy checklist (markdown): what must be true or done before this ships. Posted on the task automatically when the release is dispatched — do not write it as a comment.",
					},
					"after_deploy": map[string]interface{}{
						"type":        "string",
						"description": "Post-deploy steps (markdown): cache warms, flag flips, smoke checks. Posted on the task automatically when the production deploy succeeds.",
					},
					"rollback_plan": map[string]interface{}{
						"type":        "string",
						"description": "How to undo this change if production breaks (markdown). Posted alongside the pre-deploy checklist when the release is dispatched.",
					},
					"deploy_depends_on": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Deploy ordering: task UUIDs or board keys (e.g. [\"T-1\"]) that must be LIVE IN PRODUCTION before this task may be released. THIS task ships AFTER the ones you list. Enforced — releasing this task is refused while any of them is unreleased, and the ordering is written into this task's before_deploy runbook automatically. Use it for real shipping order (the API before the client that calls it); use blocked_by for who writes the code first.",
					},
					"blocked_by": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Work ordering: task UUIDs or board keys (e.g. [\"T-1\"]) that must be FINISHED (done or released) before anyone starts this task. THIS task waits for the ones you list — same direction as deploy_depends_on. Enforced: while any of them is open this task is parked in `blocked` instead of being dispatched, and it is picked up automatically the moment the last one lands. Use it when one task's code has to exist before the next can be written; use deploy_depends_on when the order is about shipping, and both when it is both. A cycle is refused at creation.",
					},
					"derived_from": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "The analiz task this implementation task was opened out of (board key, e.g. [\"A-12\"], or UUID). Set it on EVERY task you create from an approved analysis. It is how the developer reaches the spec: the analiz task's documents are read into that task's run context automatically, and list_task_documents re-reads them at any time. The analysis lives on that task as documents — never as a file committed to the repository — so a task without this reference is a task whose specification the implementer cannot find.",
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
				"hint":    "Use this task instead of creating another: move_board_task / update_board_task / add_task_comment. Pass allow_duplicate=true only if it is genuinely different work.",
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
		Description:          args.Description,
		TechnicalDescription: args.TechnicalDescription,
		AcceptanceCriteria:   criteria,
		Column:               col,
		CreatedBy:            "agent",
	}
	if args.TaskType != "" {
		req.TaskType = domain.TaskType(args.TaskType)
	}
	if args.Priority != "" {
		req.Priority = domain.TaskPriority(args.Priority)
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
		req.AssigneeAgentID = &assigneeID
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
