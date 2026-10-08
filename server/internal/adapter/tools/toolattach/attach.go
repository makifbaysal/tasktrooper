// Package toolattach saves an image a screenshot tool returned onto the board
// task the run is working, when the caller asked for it.
package toolattach

import (
	"context"
	"encoding/base64"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type attachedInput struct {
	Filename     string
	AttachmentID string
}

var attachedKey = prompt.Define("tools.screenshot_attached", attachedInput{Filename: "home-375.png", AttachmentID: "a"})

type failedInput struct{ Reason string }

var failedKey = prompt.Define("tools.screenshot_attach_failed", failedInput{Reason: "no task"})

var noTaskKey = prompt.Define("tools.screenshot_attach_no_task", struct{}{})

var unsafeName = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

// Note attaches img to the run's task and returns the line to append to the
// tool's result. It never fails the tool: a screenshot the model can see is
// still worth having when the copy for the human could not be stored.
func Note(ctx context.Context, a port.TaskImageAttacher, toolName, title string, img domain.ToolResultImage) string {
	taskID := registry.TaskIDFromContext(ctx)
	repositoryID := registry.RepositoryIDFromContext(ctx)
	if a == nil || taskID == uuid.Nil || repositoryID == uuid.Nil {
		return "\n" + prompt.Text(noTaskKey)
	}
	data, err := base64.StdEncoding.DecodeString(img.Data)
	if err != nil {
		return "\n" + failedKey.Render(failedInput{Reason: err.Error()})
	}
	name := filename(toolName, title, img.MediaType)
	meta, err := a.AttachToTask(ctx, repositoryID, taskID, name, img.MediaType, data)
	if err != nil {
		return "\n" + failedKey.Render(failedInput{Reason: err.Error()})
	}
	return "\n" + attachedKey.Render(attachedInput{Filename: meta.Filename, AttachmentID: meta.ID.String()})
}

func filename(toolName, title, mediaType string) string {
	base := strings.Trim(unsafeName.ReplaceAllString(strings.TrimSpace(title), "-"), "-.")
	if base == "" {
		base = toolName
	}
	if len(base) > 80 {
		base = base[:80]
	}
	ext := ".png"
	if mediaType == "image/jpeg" {
		ext = ".jpg"
	}
	return base + ext
}
