package board

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const updateBoardTaskToolName = "update_board_task"

type updateTaskArgs struct {
	TaskID               string `json:"task_id"`
	Title                string `json:"title"`
	Description          string `json:"description"`
	TechnicalDescription string `json:"technical_description"`
	// AcceptanceCriteria replaces the whole checklist when present. It is a
	// pointer to a slice so an omitted argument leaves existing criteria (and
	// their completion state) alone, while an explicit [] clears them.
	AcceptanceCriteria *[]string `json:"acceptance_criteria"`
	Column             string    `json:"column"`
	Priority           string    `json:"priority"`
	// Project tags an existing task with an initiative (name or UUID). Without
	// it a task created before the initiative existed could never be filed.
	Project string `json:"project"`
	// Component re-scopes the task to a different component of a monorepo, by
	// its repository-relative path ("." for the root). Empty leaves it alone —
	// there is no spelling to clear it back to "whole repository" from here.
	Component string `json:"component"`
	// Deploy runbook. Empty string is "leave alone" here, matching every other
	// string argument on this tool; the fields are cleared from the UI, not by
	// an agent that happened to send "".
	BeforeDeploy string `json:"before_deploy"`
	AfterDeploy  string `json:"after_deploy"`
	RollbackPlan string `json:"rollback_plan"`
	// DeployDependsOn replaces the task's deploy ordering wholesale. Pointer to
	// a slice for the same reason acceptance_criteria is one: an omitted
	// argument must leave the existing dependencies alone, while an explicit []
	// clears them.
	DeployDependsOn *[]string `json:"deploy_depends_on"`
	// BlockedBy ADDS work-order blockers rather than replacing them, so it is a
	// plain slice: there is no "clear them all" spelling to reserve nil for.
	BlockedBy []string `json:"blocked_by"`
}

type updateTaskTool struct {
	kit *ToolKit
}

func newUpdateTaskTool(kit *ToolKit) port.ToolExecutor {
	return &updateTaskTool{kit: kit}
}

func (t *updateTaskTool) Name() string {
	return updateBoardTaskToolName
}

func (t *updateTaskTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name:        updateBoardTaskToolName,
			Description: "Update a board task. Deploy ordering goes in deploy_depends_on (this task ships AFTER those), and anything that has to happen around the deploy goes in before_deploy / after_deploy / rollback_plan — not in a comment. Those fields are read back and posted automatically when the release is dispatched and when it lands, so a checklist written as a comment is one nobody will see at deploy time.",
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"task_id": map[string]interface{}{
						"type":        "string",
						"description": "Board task UUID or its board key (e.g. \"T-1\" for a task, \"B-1\" for a bug, \"A-1\" for an analysis).",
					},
					"title": map[string]interface{}{
						"type": "string",
					},
					"description": map[string]interface{}{
						"type":        "string",
						"description": "Product-level description (markdown): user story, context, out-of-scope. Acceptance criteria and technical detail have their own fields — do not paste them here.",
					},
					"technical_description": map[string]interface{}{
						"type":        "string",
						"description": "Technical detail (markdown): affected endpoints/files/schema, approach, constraints. The only place technical detail belongs.",
					},
					"acceptance_criteria": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Replaces the task's acceptance-criteria checklist with this list, one observable Given/When/Then per item. Send the FULL list — anything omitted is deleted and completion state is reset. Omit the argument entirely to leave criteria untouched.",
					},
					"column": map[string]interface{}{
						"type": "string",
					},
					"priority": map[string]interface{}{
						"type": "string",
						"enum": []string{"low", "medium", "high", "critical"},
					},
					"project": map[string]interface{}{
						"type":        "string",
						"description": "File this task under an initiative: project name or UUID. Use list_projects for valid names.",
					},
					"component": map[string]interface{}{
						"type":        "string",
						"description": "For a monorepo: re-scope this task to a different component, by its repository-relative path (e.g. \"services/api\"); \".\" means the repository root. Omit to leave the task's current component alone.",
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
						"description": "Deploy ordering: task UUIDs or board keys (e.g. [\"T-1\"]) that must be LIVE IN PRODUCTION before this task may be released — THIS task ships AFTER them. Replaces the task's existing deploy dependencies — send the FULL list; [] clears them, omitting the argument leaves them untouched. This is enforced: releasing this task is refused while any of them is unreleased, and the ordering is regenerated into this task's before_deploy runbook. Use it for real shipping order (the API before the client that calls it), not for who codes first — that is blocked_by. A cycle is refused.",
					},
					"blocked_by": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Work ordering: task UUIDs or board keys that must be FINISHED (done or released) before anyone starts this task — THIS task waits for them. ADDS to the task's existing blockers rather than replacing them, so send only the ones you are adding. Enforced: while any blocker is open the task is parked in `blocked` instead of being dispatched, and it is picked up automatically when the last one lands. A cycle is refused.",
					},
				},
				"required": []string{"task_id"},
			},
		},
	}
}

func (t *updateTaskTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args updateTaskArgs
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(updateBoardTaskToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	taskID, err := t.kit.resolveTaskRef(ctx, args.TaskID)
	if err != nil {
		return toolError(updateBoardTaskToolName, err.Error())
	}
	repositoryID, err := t.kit.resolveTaskRepositoryID(ctx, taskID)
	if err != nil {
		return toolError(updateBoardTaskToolName, err.Error())
	}
	req := domain.UpdateBoardTaskRequest{}
	if args.Title != "" {
		req.Title = &args.Title
	}
	if args.Description != "" {
		req.Description = &args.Description
	}
	if args.TechnicalDescription != "" {
		req.TechnicalDescription = &args.TechnicalDescription
	}
	if args.Column != "" {
		col := domain.TaskColumn(args.Column)
		req.Column = &col
	}
	if args.Priority != "" {
		p := domain.TaskPriority(args.Priority)
		req.Priority = &p
	}
	if strings.TrimSpace(args.Project) != "" {
		projectID, projErr := t.kit.resolveProjectRef(ctx, args.Project)
		if projErr != nil {
			return toolError(updateBoardTaskToolName, projErr.Error())
		}
		req.InitiativeProjectID = &projectID
	}
	if strings.TrimSpace(args.Component) != "" {
		componentID, cerr := t.kit.resolveComponentRef(ctx, repositoryID, args.Component)
		if cerr != nil {
			return toolError(updateBoardTaskToolName, cerr.Error())
		}
		if componentID != nil {
			req.ComponentID = domain.Nullable[uuid.UUID]{Present: true, Value: componentID}
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
	if args.DeployDependsOn != nil {
		// Resolved here rather than in the service so an unknown board key
		// fails the tool call with the "pass a UUID or a board key" message the
		// model can act on, instead of a bare not-found from the store.
		deps, depErr := t.kit.resolveDeployDependencies(ctx, *args.DeployDependsOn)
		if depErr != nil {
			return toolError(updateBoardTaskToolName, depErr.Error())
		}
		req.DeployDependsOn = &deps
	}
	if len(args.BlockedBy) > 0 {
		blockers, blockErr := t.kit.resolveRelationRefs(ctx, args.BlockedBy, domain.TaskRelationBlocks, "blocked_by")
		if blockErr != nil {
			return toolError(updateBoardTaskToolName, blockErr.Error())
		}
		req.BlockedBy = blockers
	}
	req.Actor = domain.TaskActorAgent
	if actorID, actorErr := resolveAgentID(ctx); actorErr == nil {
		req.ActorAgentID = &actorID
	}
	task, err := t.kit.Tasks.UpdateTask(ctx, repositoryID, taskID, req)
	if err != nil {
		return toolError(updateBoardTaskToolName, err.Error())
	}
	var droppedCriteria []droppedCriterion
	if args.AcceptanceCriteria != nil {
		items, dropped := criteriaInputs(*args.AcceptanceCriteria)
		droppedCriteria = dropped
		criteria, criteriaErr := t.kit.Tasks.ReplaceAcceptanceCriteria(ctx, repositoryID, taskID, items)
		if criteriaErr != nil {
			return toolError(updateBoardTaskToolName, criteriaErr.Error())
		}
		task.AcceptanceCriteria = criteria
	}
	if len(droppedCriteria) > 0 {
		return toolJSON(updateBoardTaskToolName, map[string]any{
			"task":             task,
			"dropped_criteria": droppedCriteria,
			"hint":             criteriaHint(droppedCriteria),
		})
	}
	return toolJSON(updateBoardTaskToolName, task)
}
