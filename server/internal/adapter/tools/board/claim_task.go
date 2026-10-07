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

const claimBoardTaskToolName = "claim_board_task"

type claimTaskArgs struct {
	TaskID string `json:"task_id"`
}

type claimTaskTool struct {
	kit *ToolKit
}

func newClaimTaskTool(kit *ToolKit) port.ToolExecutor {
	return &claimTaskTool{kit: kit}
}

func (t *claimTaskTool) Name() string {
	return claimBoardTaskToolName
}

func (t *claimTaskTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: claimBoardTaskToolName,
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"task_id": map[string]interface{}{
						"type": "string",
					},
				},
				"required": []string{"task_id"},
			},
		},
	}
}

func (t *claimTaskTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args claimTaskArgs
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(claimBoardTaskToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	taskID, err := t.kit.resolveTaskRef(ctx, args.TaskID)
	if err != nil {
		return toolError(claimBoardTaskToolName, err.Error())
	}
	agentID, err := resolveAgentID(ctx)
	if err != nil {
		return toolError(claimBoardTaskToolName, err.Error())
	}
	repositoryID, err := t.kit.resolveTaskRepositoryID(ctx, taskID)
	if err != nil {
		return toolError(claimBoardTaskToolName, err.Error())
	}
	task, err := t.kit.Tasks.ClaimTask(ctx, repositoryID, taskID, agentID)
	if err != nil {
		return toolError(claimBoardTaskToolName, claimRefusal(args.TaskID, err))
	}
	return toolJSON(claimBoardTaskToolName, task)
}

// claimRefusal says why a claim failed in words the caller can act on.
//
// The claim update matches only rows that are unassigned or already ours, so
// its "no rows" answer covers two different situations and the driver error it
// used to surface verbatim — "claim assignee: no rows in result set" — named
// neither. An agent reading that could not tell a task missing from this
// repository from one a colleague had just taken, and did the only thing left:
// claimed again, in a loop. The store now types both cases; this turns them
// into the sentence.
//
// The task is named the way the caller referred to it, the board key it passed
// rather than an id it never saw.
func claimRefusal(ref string, err error) string {
	ref = strings.TrimSpace(ref)
	switch {
	case errors.Is(err, domain.ErrTaskAlreadyClaimed):
		return claimAlreadyAssignedKey.Render(refInput{Ref: ref})
	case errors.Is(err, domain.ErrBoardTaskNotFound):
		return claimTaskNotFoundKey.Render(refInput{Ref: ref})
	case errors.Is(err, domain.ErrAssigneeNotAssignable):
		return assigneeNotAssignableKey.Render(struct{}{})
	default:
		return err.Error()
	}
}

func resolveAgentID(ctx context.Context) (uuid.UUID, error) {
	id := registry.AgentIDFromContext(ctx)
	if id == uuid.Nil {
		return uuid.Nil, fmt.Errorf("agent id not set in context")
	}
	return id, nil
}
