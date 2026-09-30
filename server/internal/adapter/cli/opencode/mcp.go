package opencode

import (
	"encoding/json"
	"fmt"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/cli/core"
)

const mcpServerName = "tasktrooper"

type MCPConfig = core.MCPConfig

type MCPRun = core.MCPRun

type MCPProvider = core.MCPProvider

// mcpConfigContentEnv renders the run's MCP server as opencode 1.x's inline
// config env var. Nothing is written into the workspace, so there is no
// cleanup.
func mcpConfigContentEnv(cfg core.MCPConfig) (string, error) {
	if !cfg.Set() {
		return "", nil
	}
	body, err := json.Marshal(map[string]any{
		"mcp": map[string]any{
			mcpServerName: map[string]any{
				"type":    "remote",
				"url":     cfg.URL,
				"headers": mcpHeaders(cfg),
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("render opencode mcp config: %w", err)
	}
	return string(body), nil
}

// mcpServersConfigContent is the same server in opencode 2.x's shape. The run
// token is a plain bearer, so OAuth discovery is switched off, and code mode
// is off so the tools stay direct tasktrooper_* tools as they are on 1.x.
func mcpServersConfigContent(cfg core.MCPConfig) (string, error) {
	if !cfg.Set() {
		return "", nil
	}
	body, err := json.Marshal(map[string]any{
		"mcp": map[string]any{
			"servers": map[string]any{
				mcpServerName: map[string]any{
					"type":     "remote",
					"url":      cfg.URL,
					"headers":  mcpHeaders(cfg),
					"oauth":    false,
					"codemode": false,
				},
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("render opencode mcp config: %w", err)
	}
	return string(body), nil
}

func mcpHeaders(cfg core.MCPConfig) map[string]string {
	headers := map[string]string{}
	if cfg.Token != "" {
		headers["Authorization"] = "Bearer " + cfg.Token
	}
	return headers
}
