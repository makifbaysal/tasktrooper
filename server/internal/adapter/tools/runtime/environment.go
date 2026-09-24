package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const getEnvironmentToolName = "get_environment"

type getEnvironmentTool struct {
	kit *ToolKit
}

func (t *getEnvironmentTool) Name() string { return getEnvironmentToolName }

func (t *getEnvironmentTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: getEnvironmentToolName,
			Description: "Read where a component runs: provider, resource, URL, health and binding status for one or every " +
				"environment. Call this before query_runtime_logs/list_runtime_errors/list_deployments to see whether an " +
				"environment is actually bound, or to find its URL for a request of your own.",
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"component":          componentSchema(),
					"environment":        environmentSchema("every environment of the component"),
					repositoryIDProperty: repositoryIDSchema(),
				},
			},
		},
	}
}

type getEnvironmentArgs struct {
	Component    string `json:"component"`
	Environment  string `json:"environment"`
	RepositoryID string `json:"repository_id"`
}

type environmentView struct {
	Environment string                    `json:"environment"`
	Provider    string                    `json:"provider,omitempty"`
	Resource    string                    `json:"resource,omitempty"`
	URL         string                    `json:"url,omitempty"`
	HealthURL   string                    `json:"health_url,omitempty"`
	Status      string                    `json:"status"`
	Bound       bool                      `json:"bound"`
	Health      *domain.EnvironmentHealth `json:"health,omitempty"`
}

func environmentViewOf(e domain.ComponentEnvironment) environmentView {
	v := environmentView{
		Environment: string(e.Environment),
		Provider:    string(e.Provider),
		URL:         e.URL,
		HealthURL:   e.HealthURL,
		Status:      string(e.Status),
		Bound:       e.Bound(),
		Health:      e.Health,
	}
	if e.Resource != nil {
		v.Resource = e.Resource.Name
		if v.Resource == "" {
			v.Resource = e.Resource.ID
		}
	}
	return v
}

func (t *getEnvironmentTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	name := t.Name()
	var args getEnvironmentArgs
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(name, fmt.Sprintf("invalid arguments: %v", err))
	}

	repositoryID, err := resolveRepositoryID(ctx, args.RepositoryID, name)
	if err != nil {
		return toolError(name, err.Error())
	}
	comp, err := resolveComponent(ctx, t.kit.Components, repositoryID, args.Component)
	if err != nil {
		return toolError(name, err.Error())
	}

	envs, err := t.kit.Cloud.ListEnvironments(ctx, repositoryID)
	if err != nil {
		return toolError(name, err.Error())
	}

	wantEnv := strings.TrimSpace(args.Environment)
	views := make([]environmentView, 0, len(envs))
	for _, e := range envs {
		if e.ComponentID != comp.ID {
			continue
		}
		if wantEnv != "" && string(e.Environment) != wantEnv {
			continue
		}
		views = append(views, environmentViewOf(e))
	}

	return toolJSON(name, map[string]any{
		"component":    comp.Path,
		"environments": views,
	})
}
