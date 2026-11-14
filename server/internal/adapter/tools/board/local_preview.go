package board

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const startTaskPreviewToolName = "start_task_preview"

// LocalPreviewRunner is application/localpreview.Service. It lets a role with
// no shell (the PM) run the task's own branch when there is no stage and no
// per-branch preview.
type LocalPreviewRunner interface {
	Start(ctx context.Context, repositoryID, taskID uuid.UUID, commandOverride string) (domain.LocalPreview, error)
	Status(repositoryID, taskID uuid.UUID) (domain.LocalPreview, bool)
}

// Vars so the unit test can shrink them.
var (
	startTaskPreviewPollInterval = time.Second
	startTaskPreviewPollTimeout  = 60 * time.Second
)

const startTaskPreviewLogTailLines = 15

type startTaskPreviewTool struct{ kit *ToolKit }

func newStartTaskPreviewTool(kit *ToolKit) port.ToolExecutor { return &startTaskPreviewTool{kit: kit} }

func (t *startTaskPreviewTool) Name() string { return startTaskPreviewToolName }

func (t *startTaskPreviewTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: startTaskPreviewToolName,
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"task_id": taskRefProperty,
				},
			},
		},
	}
}

type localPreviewResult struct {
	Status  string   `json:"status"`
	URL     string   `json:"url,omitempty"`
	Branch  string   `json:"branch,omitempty"`
	Command string   `json:"command,omitempty"`
	Detail  string   `json:"detail,omitempty"`
	LogTail []string `json:"log_tail,omitempty"`
	Note    string   `json:"note"`
}

func (t *startTaskPreviewTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(startTaskPreviewToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	if t.kit.LocalPreviews == nil {
		return toolError(startTaskPreviewToolName, "local preview is not configured on this deployment")
	}
	taskID, repositoryID, res := t.kit.resolveDeployTask(ctx, startTaskPreviewToolName, args.TaskID)
	if res != nil {
		return *res
	}

	preview, ok := t.kit.LocalPreviews.Status(repositoryID, taskID)
	if !ok || !isLive(preview.Status) {
		started, err := t.kit.LocalPreviews.Start(ctx, repositoryID, taskID, "")
		if err != nil {
			return toolError(startTaskPreviewToolName, err.Error())
		}
		preview = started
	}

	preview = t.awaitURL(ctx, repositoryID, taskID, preview)
	return toolJSON(startTaskPreviewToolName, localPreviewOutput(preview))
}

func isLive(status domain.LocalPreviewStatus) bool {
	return status == domain.LocalPreviewStarting || status == domain.LocalPreviewRunning
}

func (t *startTaskPreviewTool) awaitURL(ctx context.Context, repositoryID, taskID uuid.UUID, preview domain.LocalPreview) domain.LocalPreview {
	deadline := time.Now().Add(startTaskPreviewPollTimeout)
	for preview.URL == "" && preview.Status != domain.LocalPreviewFailed && time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return preview
		case <-time.After(startTaskPreviewPollInterval):
		}
		if current, ok := t.kit.LocalPreviews.Status(repositoryID, taskID); ok {
			preview = current
		}
	}
	return preview
}

func localPreviewOutput(p domain.LocalPreview) localPreviewResult {
	out := localPreviewResult{
		Status:  string(p.Status),
		URL:     p.URL,
		Branch:  p.Branch,
		Command: p.Command,
		Detail:  p.Detail,
	}
	switch {
	case p.Status == domain.LocalPreviewFailed:
		out.LogTail = tailLines(p.LogTail, startTaskPreviewLogTailLines)
		out.Note = localPreviewFailedKey.Render(struct{}{})
	case p.URL != "":
		out.Note = localPreviewReadyKey.Render(struct{}{})
	default:
		out.LogTail = tailLines(p.LogTail, startTaskPreviewLogTailLines)
		out.Note = localPreviewPendingKey.Render(struct{}{})
	}
	return out
}

func tailLines(lines []string, n int) []string {
	if len(lines) <= n {
		return lines
	}
	return lines[len(lines)-n:]
}
