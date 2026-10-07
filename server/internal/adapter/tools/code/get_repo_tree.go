package code

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/makifbaysal/tasktrooper/server/internal/application/workspace"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const getRepoTreeToolName = "get_repo_tree"

type getRepoTreeArgs struct {
	Prefix   string `json:"prefix"`
	MaxDepth int    `json:"max_depth"`
}

type getRepoTreeResponse struct {
	Tree string `json:"tree"`
}

type getRepoTreeTool struct {
	kit *ToolKit
}

func newGetRepoTreeTool(kit *ToolKit) port.ToolExecutor {
	return &getRepoTreeTool{kit: kit}
}

func (t *getRepoTreeTool) Name() string {
	return getRepoTreeToolName
}

func (t *getRepoTreeTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: getRepoTreeToolName,
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"prefix": map[string]interface{}{
						"type": "string",
					},
					"max_depth": map[string]interface{}{
						"type": "integer",
					},
				},
			},
		},
	}
}

func (t *getRepoTreeTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args getRepoTreeArgs
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(getRepoTreeToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	if t.kit.Mapper == nil {
		return toolError(getRepoTreeToolName, "mapper not configured")
	}

	root, err := resolveProjectRoot(ctx)
	if err != nil {
		return toolError(getRepoTreeToolName, err.Error())
	}

	tree, err := t.kit.Mapper.ExpandTree(root, workspace.IndexPath(root, args.Prefix), args.MaxDepth)
	if err != nil {
		return toolError(getRepoTreeToolName, fmt.Sprintf("expand tree: %v", err))
	}

	return toolJSON(getRepoTreeToolName, getRepoTreeResponse{Tree: tree})
}
