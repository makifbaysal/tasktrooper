package board

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const triggerReleaseToolName = "trigger_release"

type triggerReleaseTool struct {
	kit *ToolKit
}

func newTriggerReleaseTool(kit *ToolKit) port.ToolExecutor {
	return &triggerReleaseTool{kit: kit}
}

func (t *triggerReleaseTool) Name() string { return triggerReleaseToolName }

func (t *triggerReleaseTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name:        triggerReleaseToolName,
			Description: "Dispatch the production deploy (release) GitHub Actions workflow for a task that is in the done column. On success the task moves to released; on failure it moves to need_revision with the deploy log. Do NOT write a pre-deploy checklist comment: the task's before_deploy and rollback_plan fields are posted automatically when the deploy is dispatched, and after_deploy when it succeeds — put the content in those fields (update_board_task) instead. The release is refused if the task's branch has moved since it was verified (or if no verified commit is recorded) — commit nothing after a task reaches done unless you intend it to go back through review — and also if any task in the task's deploy_depends_on list is not live in production yet.",
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"task_id"},
				"properties": map[string]interface{}{
					"task_id": map[string]interface{}{"type": "string", "description": "Board task UUID or its board key (e.g. \"T-1\" for a task, \"B-1\" for a bug, \"A-1\" for an analysis); must be in the done column."},
				},
			},
		},
	}
}

func (t *triggerReleaseTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(triggerReleaseToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	taskID, err := t.kit.resolveTaskRef(ctx, args.TaskID)
	if err != nil {
		return toolError(triggerReleaseToolName, err.Error())
	}
	repositoryID, err := t.kit.resolveTaskRepositoryID(ctx, taskID)
	if err != nil {
		return toolError(triggerReleaseToolName, fmt.Sprintf("resolve repository: %v", err))
	}
	pipeline, err := t.kit.Tasks.TriggerRelease(ctx, repositoryID, taskID)
	if err != nil {
		// The release-identity block is a tool ERROR, not a quiet "not
		// triggered": the code on the branch is not the code that was signed
		// off, and an agent that reads this as a routine no-op will report the
		// task as released. The service error already names both commits and
		// the way forward — retrying the same call cannot change either, so
		// say so instead of letting the model loop on it.
		if errors.Is(err, domain.ErrReleaseTargetMoved) || errors.Is(err, domain.ErrReleaseTargetUnverified) {
			return toolError(triggerReleaseToolName, err.Error()+
				"\n\nNothing was deployed. Do not retry trigger_release: it will block again until the task is re-verified at its current commit.")
		}
		return toolError(triggerReleaseToolName, err.Error())
	}
	return toolJSON(triggerReleaseToolName, map[string]any{
		"triggered":   true,
		"pipeline_id": pipeline.ID.String(),
		"status":      string(pipeline.Status),
		"message":     "Prod deploy triggered; follow it with get_pipeline_status. On success the task moves to released, on failure to need_revision.",
	})
}
