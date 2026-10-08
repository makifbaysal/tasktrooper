package designsystem

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const getToolName = "get_design_system"

type getTool struct {
	kit *ToolKit
}

func (t *getTool) Name() string { return getToolName }

func (t *getTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: getToolName,
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"repository_id": map[string]interface{}{"type": "string"},
					"project_id":    map[string]interface{}{"type": "string"},
					"files":         map[string]interface{}{"type": "boolean"},
				},
			},
		},
	}
}

type getArgs struct {
	RepositoryID string `json:"repository_id"`
	ProjectID    string `json:"project_id"`
	Files        bool   `json:"files"`
}

type versionOut struct {
	ID            string                     `json:"id"`
	Version       int                        `json:"version"`
	Status        string                     `json:"status"`
	DesignMD      string                     `json:"design_md,omitempty"`
	InventoryMD   string                     `json:"inventory_md,omitempty"`
	Rationale     string                     `json:"rationale,omitempty"`
	Tokens        json.RawMessage            `json:"tokens,omitempty"`
	SourceTaskKey string                     `json:"source_task_key,omitempty"`
	Lint          []domain.DesignLintFinding `json:"lint,omitempty"`
}

func versionOf(d *domain.DesignSystem) *versionOut {
	if d == nil {
		return nil
	}
	return &versionOut{
		ID:            d.ID.String(),
		Version:       d.Version,
		Status:        string(d.Status),
		DesignMD:      d.DesignMD,
		InventoryMD:   d.InventoryMD,
		Rationale:     d.Rationale,
		Tokens:        d.Tokens,
		SourceTaskKey: d.SourceTaskKey,
		Lint:          d.Lint,
	}
}

func (t *getTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args getArgs
	if strings.TrimSpace(arguments) != "" {
		if err := json.Unmarshal([]byte(arguments), &args); err != nil {
			return toolError(getToolName, fmt.Sprintf("invalid arguments: %v", err))
		}
	}
	projectID, err := parseOptionalUUID(strings.TrimSpace(args.ProjectID), "project_id")
	if err != nil {
		return toolError(getToolName, err.Error())
	}
	if projectID != nil {
		return t.project(ctx, *projectID)
	}
	repositoryID, err := parseOptionalUUID(strings.TrimSpace(args.RepositoryID), "repository_id")
	if err != nil {
		return toolError(getToolName, err.Error())
	}
	if repositoryID == nil {
		if id := registry.RepositoryIDFromContext(ctx); id != uuid.Nil {
			repositoryID = &id
		}
	}
	if repositoryID == nil {
		return toolError(getToolName, prompt.ToolRepositoryRequiredText(getToolName))
	}
	eff, err := t.kit.Service.Effective(ctx, *repositoryID)
	if err != nil {
		return toolError(getToolName, err.Error())
	}
	out := map[string]any{
		"repository_id": repositoryID.String(),
		"base":          versionOf(eff.Base),
		"layer":         versionOf(eff.Layer),
		"tokens":        eff.Tokens,
	}
	if eff.Project != nil {
		out["project"] = map[string]any{"id": eff.Project.ID.String(), "name": eff.Project.Name}
	}
	if eff.Ambiguous {
		out["ambiguous"] = true
	}
	if args.Files {
		files, err := t.kit.Service.Files(ctx, *repositoryID)
		if err != nil {
			return toolError(getToolName, err.Error())
		}
		out["files"] = files
	}
	return toolJSON(getToolName, out)
}

func (t *getTool) project(ctx context.Context, projectID uuid.UUID) domain.ToolResult {
	view, err := t.kit.Service.ProjectView(ctx, projectID)
	if err != nil {
		return toolError(getToolName, err.Error())
	}
	pending := make([]*versionOut, 0, len(view.Pending))
	for i := range view.Pending {
		pending = append(pending, versionOf(&view.Pending[i]))
	}
	repos := make([]map[string]any, 0, len(view.Repositories))
	for _, r := range view.Repositories {
		entry := map[string]any{
			"id":                     r.ID.String(),
			"name":                   r.Name,
			"kind":                   r.Kind,
			"builds_on_this_project": r.BuildsOnThisProject,
			"pending_layer":          r.PendingLayer,
		}
		if r.Layer != nil {
			entry["layer_version"] = r.Layer.Version
			entry["layer_rationale"] = r.Layer.Rationale
		}
		repos = append(repos, entry)
	}
	return toolJSON(getToolName, map[string]any{
		"project":      map[string]any{"id": view.Project.ID.String(), "name": view.Project.Name},
		"base":         versionOf(view.Current),
		"pending":      pending,
		"repositories": repos,
	})
}
