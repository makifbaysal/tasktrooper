package board

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const deleteBoardTaskToolName = "delete_board_task"

// deletableColumns are the columns a task may be removed from without asking
// twice. Both are planning columns: nothing has been built for the task yet, so
// deleting it destroys a plan and nothing else. Past them the task carries a
// branch, comments, criteria verdicts and a pipeline history — the record of
// work that actually happened — and the right move is almost always to move it
// somewhere terminal instead of erasing it.
var deletableColumns = map[domain.TaskColumn]bool{
	domain.TaskColumnBacklog: true,
	domain.TaskColumnTodo:    true,
}

type deleteTaskArgs struct {
	TaskID string `json:"task_id"`
	Reason string `json:"reason"`
	// Force removes a task that has already left the planning columns. It is a
	// deliberate second step, not a default.
	Force bool `json:"force"`
}

type deleteTaskTool struct {
	kit *ToolKit
}

func newDeleteTaskTool(kit *ToolKit) port.ToolExecutor {
	return &deleteTaskTool{kit: kit}
}

func (t *deleteTaskTool) Name() string {
	return deleteBoardTaskToolName
}

func (t *deleteTaskTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: deleteBoardTaskToolName,
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"task_id": map[string]interface{}{
						"type": "string",
					},
					"reason": map[string]interface{}{
						"type": "string",
					},
					"force": map[string]interface{}{
						"type": "boolean",
					},
				},
				"required": []string{"task_id"},
			},
		},
	}
}

func (t *deleteTaskTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args deleteTaskArgs
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(deleteBoardTaskToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	taskID, err := t.kit.resolveTaskRef(ctx, args.TaskID)
	if err != nil {
		return toolError(deleteBoardTaskToolName, err.Error())
	}
	repositoryID, err := t.kit.resolveTaskRepositoryID(ctx, taskID)
	if err != nil {
		return toolError(deleteBoardTaskToolName, err.Error())
	}
	// The record is read before it is removed: what comes back is the only
	// description of the task that will exist afterwards, and the ledger entry
	// the chat renders is built from it.
	task, found := t.kit.findTask(ctx, taskID)
	if !found {
		return toolError(deleteBoardTaskToolName, fmt.Sprintf("task %s not found on the board", args.TaskID))
	}
	if !deletableColumns[task.Column] && !args.Force {
		// The board-list shape the refusal always carried: the single-task
		// read also loads documents in full, criteria and test rounds, which
		// would only inflate the tool result.
		listed := task
		listed.AcceptanceCriteria, listed.TestCases, listed.Relations = nil, nil, nil
		listed.BlockedBy, listed.Documents, listed.Attachments = nil, nil, nil
		return toolJSON(deleteBoardTaskToolName, map[string]any{
			"deleted": false,
			"reason":  fmt.Sprintf("task is in %s, not a planning column", task.Column),
			"task":    listed,
			"hint":    deleteTaskBlockedHintKey.Render(struct{}{}),
		})
	}
	if err := t.kit.Tasks.DeleteTask(ctx, repositoryID, taskID); err != nil {
		return toolError(deleteBoardTaskToolName, err.Error())
	}
	return toolJSON(deleteBoardTaskToolName, map[string]any{
		"deleted":       true,
		"id":            task.ID,
		"key":           task.Key,
		"title":         task.Title,
		"column":        task.Column,
		"priority":      task.Priority,
		"repository_id": task.RepositoryID,
		"delete_reason": args.Reason,
	})
}
