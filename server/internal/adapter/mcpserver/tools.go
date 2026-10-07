package mcpserver

import (
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

func ServedToolNames(registry port.ToolRegistry, run Run) []string {
	tools := servedTools(registry, run)
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	return names
}

func servedTools(registry port.ToolRegistry, run Run) []toolInfo {
	if registry == nil {
		return nil
	}
	return toolsFrom(registry.DefinitionsForPolicy(run.Policy), run)
}

var nativelyCovered = map[string]struct{}{
	"run_terminal":  {}, // Bash
	"read_file":     {}, // Read
	"write_file":    {}, // Write
	"edit_file":     {}, // Edit
	"edit_lines":    {}, // Edit
	"delete_file":   {}, // Bash
	"move_file":     {}, // Bash
	"grep_code":     {}, // Grep
	"get_repo_tree": {}, // Glob
}

const skillLoadTool = "load_skill"

func exposed(name string, run Run) bool {
	if name == domain.AskUserToolName {
		return run.canAsk()
	}
	if run.SkillsOnDisk && name == skillLoadTool {
		return false
	}
	_, native := nativelyCovered[name]
	return !native
}

type toolInfo struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

func emptyObjectSchema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}

func toolsFrom(defs []domain.ToolDefinition, run Run) []toolInfo {
	tools := make([]toolInfo, 0, len(defs))
	for _, def := range defs {
		name := def.Function.Name
		if name == "" || !exposed(name, run) {
			continue
		}
		schema := def.Function.Parameters
		if len(schema) == 0 {
			schema = emptyObjectSchema()
		}
		tools = append(tools, toolInfo{
			Name:        name,
			Description: def.Function.Description,
			InputSchema: schema,
		})
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	return tools
}

type content struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
}

type callToolResult struct {
	Content []content `json:"content"`
	IsError bool `json:"isError,omitempty"`
}

func textResult(text string, isError bool) callToolResult {
	return callToolResult{Content: []content{{Type: "text", Text: text}}, IsError: isError}
}

func resultToMCP(name string, result domain.ToolResult) callToolResult {
	if result.Clarification != nil {
		return textResult(clarificationWaitKey.Render(toolNameInput{Name: name}), true)
	}
	if result.ResourceBlock != nil {
		detail := strings.TrimSpace(result.ResourceBlock.Detail)
		if detail == "" {
			detail = "the resource is held by another run"
		}
		return textResult(resourceBlockKey.Render(resourceBlockInput{
			Name: name, Resource: result.ResourceBlock.Resource, Detail: detail,
		}), true)
	}

	blocks := make([]content, 0, len(result.Images)+1)
	text := result.Content
	if strings.TrimSpace(text) == "" && len(result.Images) == 0 {
		text = emptyResultKey.Render(toolNameInput{Name: name})
	}
	if text != "" {
		blocks = append(blocks, content{Type: "text", Text: text})
	}
	for _, img := range result.Images {
		if img.Data == "" {
			continue
		}
		mime := img.MediaType
		if mime == "" {
			mime = "image/png"
		}
		blocks = append(blocks, content{Type: "image", Data: img.Data, MimeType: mime})
	}
	return callToolResult{Content: blocks, IsError: result.IsError}
}

func callID() string {
	return "mcp-" + uuid.NewString()
}
