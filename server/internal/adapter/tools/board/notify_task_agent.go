package board

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/board"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const notifyTaskAgentToolName = "notify_task_agent"

// ChangeDeliverer is application/board.ChangeRequests: it records a change
// request on the task and gets it to the run that holds the task, if any.
type ChangeDeliverer interface {
	Deliver(ctx context.Context, repositoryID, taskID, authorAgentID uuid.UUID, summary string, interrupt bool) (board.ChangeDelivery, error)
}

type notifyTaskAgentTool struct{ kit *ToolKit }

func newNotifyTaskAgentTool(kit *ToolKit) port.ToolExecutor { return &notifyTaskAgentTool{kit: kit} }

func (t *notifyTaskAgentTool) Name() string { return notifyTaskAgentToolName }

func (t *notifyTaskAgentTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: notifyTaskAgentToolName,
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"task_id":       taskRefProperty,
					"summary":       map[string]interface{}{"type": "string"},
					"interrupt":     map[string]interface{}{"type": "boolean"},
					"user_approved": map[string]interface{}{"type": "boolean"},
				},
				"required": []string{"task_id", "summary"},
			},
		},
	}
}

type notifyTaskAgentArgs struct {
	TaskID       string `json:"task_id"`
	Summary      string `json:"summary"`
	Interrupt    bool   `json:"interrupt"`
	UserApproved bool   `json:"user_approved"`
}

type notifyTaskAgentResult struct {
	Outcome string `json:"outcome"`
	RunID   string `json:"run_id,omitempty"`
	Note    string `json:"note"`
}

func (t *notifyTaskAgentTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args notifyTaskAgentArgs
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(notifyTaskAgentToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	if t.kit.ChangeRequests == nil {
		return toolError(notifyTaskAgentToolName, "change requests are not configured on this deployment")
	}
	if args.Interrupt && !args.UserApproved {
		return toolError(notifyTaskAgentToolName, changeInterruptNeedsApprovalKey.Render(struct{}{}))
	}
	taskID, err := t.kit.resolveTaskRef(ctx, args.TaskID)
	if err != nil {
		return toolError(notifyTaskAgentToolName, err.Error())
	}
	agentID, err := resolveAgentID(ctx)
	if err != nil {
		return toolError(notifyTaskAgentToolName, err.Error())
	}
	repositoryID, err := t.kit.resolveTaskRepositoryID(ctx, taskID)
	if err != nil {
		return toolError(notifyTaskAgentToolName, err.Error())
	}
	delivery, err := t.kit.ChangeRequests.Deliver(ctx, repositoryID, taskID, agentID, args.Summary, args.Interrupt)
	if err != nil {
		return toolError(notifyTaskAgentToolName, err.Error())
	}
	out := notifyTaskAgentResult{Outcome: delivery.Outcome}
	if delivery.RunID != nil {
		out.RunID = delivery.RunID.String()
	}
	switch delivery.Outcome {
	case board.ChangeDeliveryInterrupted:
		out.Note = changeInterruptedNoteKey.Render(struct{}{})
	case board.ChangeDeliveryNotified:
		out.Note = changeNotifiedNoteKey.Render(struct{}{})
	default:
		out.Note = changeStoredNoteKey.Render(struct{}{})
	}
	return toolJSON(notifyTaskAgentToolName, out)
}
