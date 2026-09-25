package projectmodel

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"

	appprojectmodel "github.com/makifbaysal/tasktrooper/server/internal/application/projectmodel"
)

const getBriefToolName = "get_project_brief"

type briefTool struct {
	kit *ToolKit
}

func (t *briefTool) Name() string { return getBriefToolName }

func (t *briefTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: getBriefToolName,
			Description: "Read the repository overview: what it is, its stack, its components and their commands, git conventions, " +
				"reference docs, what runs where, what it talks to, and the CI checks to run before handing off. Not given to you " +
				"automatically — call it when you need this, optionally scoped to a component path or area.",
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"component": map[string]interface{}{
						"type":        "string",
						"description": "A component's own path (e.g. \"apps/web\", or \".\" for a single-purpose repo) to scope the brief to just that component.",
					},
					"area": map[string]interface{}{
						"type":        "string",
						"description": "A role/area (e.g. \"backend\", \"frontend\") to scope the brief to every component in that area. Ignored when component is set.",
					},
					repositoryIDProperty: repositoryIDSchema(),
				},
			},
		},
	}
}

type briefArgs struct {
	Component    string `json:"component"`
	Area         string `json:"area"`
	RepositoryID string `json:"repository_id"`
}

func (t *briefTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	name := t.Name()
	var args briefArgs
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(name, fmt.Sprintf("invalid arguments: %v", err))
	}

	repositoryID, err := resolveRepositoryID(ctx, args.RepositoryID, name)
	if err != nil {
		return toolError(name, err.Error())
	}

	scope := appprojectmodel.BriefScope{Area: strings.TrimSpace(args.Area)}
	if component := strings.TrimSpace(args.Component); component != "" {
		componentID, err := resolveComponentID(ctx, t.kit.Model, repositoryID, component)
		if err != nil {
			return toolError(name, err.Error())
		}
		scope = appprojectmodel.BriefScope{ComponentID: &componentID}
	}

	brief, err := t.kit.Model.Brief(ctx, repositoryID, scope)
	if err != nil {
		return toolError(name, err.Error())
	}
	return domain.ToolResult{Name: name, Content: brief, IsError: false}
}
