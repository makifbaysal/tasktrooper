// Package projectmodel exposes the read/write tools agents use against the
// structured project model: get_project_brief, list_component_checks,
// list_links and record_project_note. It replaces repoprofile now that the
// judgment layer lives on components instead of a markdown profile.
package projectmodel

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"

	appprojectmodel "github.com/makifbaysal/tasktrooper/server/internal/application/projectmodel"
)

// ToolKit is the write-through into the project model service.
type ToolKit struct {
	Model *appprojectmodel.Service
}

func NewExecutors(k *ToolKit) []port.ToolExecutor {
	if k == nil || k.Model == nil {
		return nil
	}
	return []port.ToolExecutor{
		&briefTool{kit: k},
		&checksTool{kit: k},
		&linksTool{kit: k},
		&recordNoteTool{kit: k},
	}
}

func toolError(name, message string) domain.ToolResult {
	return domain.ToolResult{Name: name, Content: message, IsError: true}
}

func toolJSON(name string, payload any) domain.ToolResult {
	raw, err := json.Marshal(payload)
	if err != nil {
		return toolError(name, fmt.Sprintf("marshal response: %v", err))
	}
	return domain.ToolResult{Name: name, Content: string(raw), IsError: false}
}

const repositoryIDProperty = "repository_id"

func repositoryIDSchema() map[string]interface{} {
	return map[string]interface{}{
		"type":        "string",
		"description": "Repository UUID. Defaults to the run's own repository; pass this only to look at a different one.",
	}
}

// resolveRepositoryID mirrors repoprofile's context fallback: an explicit
// repository_id overrides the run's own repository, and a call with neither
// gets an error naming both ways out.
func resolveRepositoryID(ctx context.Context, raw, toolName string) (uuid.UUID, error) {
	if trimmed := strings.TrimSpace(raw); trimmed != "" {
		id, err := uuid.Parse(trimmed)
		if err != nil {
			return uuid.Nil, fmt.Errorf("invalid repository_id: %w", err)
		}
		return id, nil
	}
	if id := registry.RepositoryIDFromContext(ctx); id != uuid.Nil {
		return id, nil
	}
	return uuid.Nil, fmt.Errorf("%s needs a repository in context or repository_id; this run has none", toolName)
}

// resolveComponentID turns a component's own path (as ComponentsForPaths
// resolves it — the deepest active component that owns the path) into its
// id, the way the board runner resolves a task's changed files.
func resolveComponentID(ctx context.Context, model *appprojectmodel.Service, repositoryID uuid.UUID, path string) (uuid.UUID, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return uuid.Nil, nil
	}
	ids, err := model.ComponentsForPaths(ctx, repositoryID, []string{trimmed})
	if err != nil {
		return uuid.Nil, err
	}
	if len(ids) == 0 {
		return uuid.Nil, fmt.Errorf("no active component at path %q", trimmed)
	}
	return ids[0], nil
}
