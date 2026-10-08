package designsystem

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	appdesignsystem "github.com/makifbaysal/tasktrooper/server/internal/application/designsystem"
	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const requestToolName = "request_design_system"

type requestTool struct {
	kit *ToolKit
}

func (t *requestTool) Name() string { return requestToolName }

func (t *requestTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: requestToolName,
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"scope": map[string]interface{}{
						"type": "string",
						"enum": []string{string(domain.DesignSystemScopeProject), string(domain.DesignSystemScopeRepository)},
					},
					"project_id":    map[string]interface{}{"type": "string"},
					"repository_id": map[string]interface{}{"type": "string"},
					"notes":         map[string]interface{}{"type": "string"},
				},
				"required": []string{"scope"},
			},
		},
	}
}

type requestArgs struct {
	Scope        string `json:"scope"`
	ProjectID    string `json:"project_id"`
	RepositoryID string `json:"repository_id"`
	Notes        string `json:"notes"`
}

func (t *requestTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args requestArgs
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(requestToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	var (
		res appdesignsystem.RequestResult
		err error
	)
	switch domain.DesignSystemScope(strings.TrimSpace(args.Scope)) {
	case domain.DesignSystemScopeProject:
		projectID, perr := parseOptionalUUID(strings.TrimSpace(args.ProjectID), "project_id")
		if perr != nil {
			return toolError(requestToolName, perr.Error())
		}
		if projectID == nil {
			return toolError(requestToolName, "project_id is required for scope project")
		}
		res, err = t.kit.Service.RequestForProject(ctx, *projectID, args.Notes)
	case domain.DesignSystemScopeRepository:
		repositoryID, perr := parseOptionalUUID(strings.TrimSpace(args.RepositoryID), "repository_id")
		if perr != nil {
			return toolError(requestToolName, perr.Error())
		}
		if repositoryID == nil {
			if id := registry.RepositoryIDFromContext(ctx); id != uuid.Nil {
				repositoryID = &id
			}
		}
		if repositoryID == nil {
			return toolError(requestToolName, prompt.ToolRepositoryRequiredText(requestToolName))
		}
		res, err = t.kit.Service.RequestForRepository(ctx, *repositoryID, args.Notes)
	default:
		return toolError(requestToolName, fmt.Sprintf("scope must be %q or %q", domain.DesignSystemScopeProject, domain.DesignSystemScopeRepository))
	}
	if err != nil {
		return toolError(requestToolName, err.Error())
	}
	return toolJSON(requestToolName, map[string]any{
		"task_id":       res.Task.ID.String(),
		"key":           res.Task.Key,
		"title":         res.Task.Title,
		"column":        string(res.Task.Column),
		"repository_id": res.Task.RepositoryID.String(),
		"created":       res.Created,
	})
}
