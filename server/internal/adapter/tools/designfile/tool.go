// Package designfile gives the designer get_design_file: the FigCraft file
// its design task draws in.
package designfile

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	appdesignfile "github.com/makifbaysal/tasktrooper/server/internal/application/designfile"
	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const ToolName = "get_design_file"

func init() {
	prompt.Define[struct{}]("tool."+ToolName, struct{}{})
}

type Files interface {
	PrepareTask(ctx context.Context, repositoryID, taskID uuid.UUID) (appdesignfile.TaskFiles, error)
}

type tool struct {
	files Files
}

func NewExecutors(files Files) []port.ToolExecutor {
	if files == nil {
		return nil
	}
	return []port.ToolExecutor{&tool{files: files}}
}

func (t *tool) Name() string { return ToolName }

func (t *tool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: ToolName,
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"task_id":       map[string]interface{}{"type": "string"},
					"repository_id": map[string]interface{}{"type": "string"},
				},
			},
		},
	}
}

type args struct {
	TaskID       string `json:"task_id"`
	RepositoryID string `json:"repository_id"`
}

func parseOr(raw string, fallback uuid.UUID, field string) (uuid.UUID, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		if fallback == uuid.Nil {
			return uuid.Nil, fmt.Errorf("%s is required", field)
		}
		return fallback, nil
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, fmt.Errorf("invalid %s: pass the UUID", field)
	}
	return id, nil
}

func (t *tool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var a args
	if strings.TrimSpace(arguments) != "" {
		if err := json.Unmarshal([]byte(arguments), &a); err != nil {
			return toolError(fmt.Sprintf("invalid arguments: %v", err))
		}
	}
	taskID, err := parseOr(a.TaskID, registry.TaskIDFromContext(ctx), "task_id")
	if err != nil {
		return toolError(err.Error())
	}
	repositoryID, err := parseOr(a.RepositoryID, registry.RepositoryIDFromContext(ctx), "repository_id")
	if err != nil {
		return toolError(err.Error())
	}
	files, err := t.files.PrepareTask(ctx, repositoryID, taskID)
	if err != nil {
		return toolError(err.Error())
	}
	raw, err := json.Marshal(files)
	if err != nil {
		return toolError(err.Error())
	}
	return domain.ToolResult{Name: ToolName, Content: string(raw)}
}

func toolError(message string) domain.ToolResult {
	return domain.ToolResult{Name: ToolName, Content: message, IsError: true}
}
