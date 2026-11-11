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
	// Assignee (agent name or UUID) or AssigneeRole (role key, resolved for the
	// task's repository area) sets or changes who works the task. Assignee
	// wins when both are given. Empty leaves the assignee alone; "none"
	// unassigns.
	Assignee     string `json:"assignee"`
	AssigneeRole string `json:"assignee_role"`
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
			Name: updateBoardTaskToolName,
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"task_id": map[string]interface{}{
						"type": "string",
					},
					"title": map[string]interface{}{
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
					"project": map[string]interface{}{
						"type": "string",
					},
					"component": map[string]interface{}{
						"type": "string",
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
					"assignee": map[string]interface{}{
						"type": "string",
					},
					"assignee_role": map[string]interface{}{
						"type": "string",
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
	if strings.TrimSpace(args.Assignee) != "" {
		if strings.EqualFold(strings.TrimSpace(args.Assignee), "none") {
			req.AssigneeAgentID = domain.Nullable[uuid.UUID]{Present: true}
		} else {
			assigneeID, aerr := t.kit.resolveAssignee(ctx, strings.TrimSpace(args.Assignee))
			if aerr != nil {
				return toolError(updateBoardTaskToolName, aerr.Error())
			}
			if refusal := t.kit.disabledAssigneeRefusal(ctx, assigneeID); refusal != "" {
				return toolError(updateBoardTaskToolName, refusal)
			}
			req.AssigneeAgentID = domain.Nullable[uuid.UUID]{Present: true, Value: &assigneeID}
		}
	} else if strings.TrimSpace(args.AssigneeRole) != "" {
		roleAssignee, rerr := t.kit.resolveAssigneeRole(ctx, strings.TrimSpace(args.AssigneeRole), repositoryID)
		if rerr != nil {
			return toolError(updateBoardTaskToolName, rerr.Error())
		}
		if roleAssignee == nil {
			return toolError(updateBoardTaskToolName, fmt.Sprintf("no agent holds role %q for this repository", args.AssigneeRole))
		}
		if refusal := t.kit.disabledRoleRefusal(ctx, args.AssigneeRole, *roleAssignee); refusal != "" {
			return toolError(updateBoardTaskToolName, refusal)
		}
		req.AssigneeAgentID = domain.Nullable[uuid.UUID]{Present: true, Value: roleAssignee}
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
