package designsystem

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const proposeToolName = "propose_design_system"

type proposeTool struct {
	kit *ToolKit
}

func (t *proposeTool) Name() string { return proposeToolName }

func (t *proposeTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: proposeToolName,
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
					"design_md":     map[string]interface{}{"type": "string"},
					"tokens":        map[string]interface{}{"type": "object"},
					"inventory_md":  map[string]interface{}{"type": "string"},
					"rationale":     map[string]interface{}{"type": "string"},
				},
				"required": []string{"scope"},
			},
		},
	}
}

type proposeArgs struct {
	Scope        string          `json:"scope"`
	ProjectID    string          `json:"project_id"`
	RepositoryID string          `json:"repository_id"`
	DesignMD     string          `json:"design_md"`
	Tokens       json.RawMessage `json:"tokens"`
	InventoryMD  string          `json:"inventory_md"`
	Rationale    string          `json:"rationale"`
}

func (t *proposeTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args proposeArgs
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(proposeToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	taskID := registry.TaskIDFromContext(ctx)
	if taskID == uuid.Nil || registry.TaskTypeFromContext(ctx) != string(domain.TaskTypeDesign) {
		return toolError(proposeToolName, needsDesignTaskKey.Render(needsDesignTaskInput{ToolName: proposeToolName}))
	}
	projectID, err := parseOptionalUUID(strings.TrimSpace(args.ProjectID), "project_id")
	if err != nil {
		return toolError(proposeToolName, err.Error())
	}
	repositoryID, err := parseOptionalUUID(strings.TrimSpace(args.RepositoryID), "repository_id")
	if err != nil {
		return toolError(proposeToolName, err.Error())
	}
	proposal := domain.DesignSystemProposal{
		Scope:        domain.DesignSystemScope(strings.TrimSpace(args.Scope)),
		ProjectID:    projectID,
		RepositoryID: repositoryID,
		DesignMD:     args.DesignMD,
		Tokens:       args.Tokens,
		InventoryMD:  args.InventoryMD,
		Rationale:    args.Rationale,
		TaskID:       taskID,
		CreatedBy:    "agent",
	}
	if agentID := registry.AgentIDFromContext(ctx); agentID != uuid.Nil {
		proposal.CreatedBy = "agent:" + agentID.String()
	}
	if t.kit.Tasks != nil {
		if repoID := registry.RepositoryIDFromContext(ctx); repoID != uuid.Nil {
			if task, err := t.kit.Tasks.GetTask(ctx, repoID, taskID); err == nil {
				proposal.TaskKey = task.Key
			}
		}
	}
	ds, err := t.kit.Service.Propose(ctx, proposal)
	if err != nil {
		return toolError(proposeToolName, err.Error())
	}
	lint := ds.Lint
	if lint == nil {
		lint = []domain.DesignLintFinding{}
	}
	return toolJSON(proposeToolName, map[string]any{
		"id":      ds.ID.String(),
		"scope":   string(ds.Scope),
		"version": ds.Version,
		"status":  string(ds.Status),
		"lint":    lint,
	})
}
