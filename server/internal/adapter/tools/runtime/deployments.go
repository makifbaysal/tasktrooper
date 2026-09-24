package runtime

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const listDeploymentsToolName = "list_deployments"

const defaultDeploymentsLimit = 10

type listDeploymentsTool struct {
	kit *ToolKit
}

func (t *listDeploymentsTool) Name() string { return listDeploymentsToolName }

func (t *listDeploymentsTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: listDeploymentsToolName,
			Description: "List an environment's recent deployments (status, commit, branch, URL, timestamps) as the provider " +
				"reports them, newest first.",
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"component":   componentSchema(),
					"environment": environmentSchema("\"production\""),
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum deployments to return. Defaults to 10.",
					},
					repositoryIDProperty: repositoryIDSchema(),
				},
			},
		},
	}
}

type listDeploymentsArgs struct {
	Component    string `json:"component"`
	Environment  string `json:"environment"`
	Limit        int    `json:"limit"`
	RepositoryID string `json:"repository_id"`
}

func (t *listDeploymentsTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	name := t.Name()
	var args listDeploymentsArgs
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(name, fmt.Sprintf("invalid arguments: %v", err))
	}
	limit := args.Limit
	if limit <= 0 {
		limit = defaultDeploymentsLimit
	}

	repositoryID, err := resolveRepositoryID(ctx, args.RepositoryID, name)
	if err != nil {
		return toolError(name, err.Error())
	}
	env, comp, err := resolveBoundEnvironment(ctx, t.kit, repositoryID, args.Component, args.Environment)
	if err != nil {
		return toolError(name, err.Error())
	}

	deployments, err := t.kit.Cloud.Deployments(ctx, env.ID, limit)
	if err != nil {
		return toolError(name, err.Error())
	}
	if deployments == nil {
		deployments = []domain.CloudDeployment{}
	}

	return toolJSON(name, map[string]any{
		"component":   comp.Path,
		"environment": string(env.Environment),
		"deployments": deployments,
		"count":       len(deployments),
	})
}
