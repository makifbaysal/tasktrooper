package code

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const writeFileToolName = "write_file"

type writeFileArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type writeFileTool struct{}

// NewWriteFileTool builds the workspace file writer.
//
// Creating a file meant a shell heredoc, and source code is the worst possible
// heredoc payload: backticks and $( ) are command substitution, quotes have to
// survive two levels of escaping, and the sandbox's allowlist mode rejects the
// whole command for containing them — which is how an agent ended up reporting
// "I do not have permission to create files" while holding the shell.
func NewWriteFileTool() port.ToolExecutor {
	return &writeFileTool{}
}

func (t *writeFileTool) Name() string {
	return writeFileToolName
}

func (t *writeFileTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: writeFileToolName,
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type": "string",
					},
					"content": map[string]interface{}{
						"type": "string",
					},
				},
				"required": []string{"path", "content"},
			},
		},
	}
}

func (t *writeFileTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args writeFileArgs
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(writeFileToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	if strings.TrimSpace(args.Path) == "" {
		return toolError(writeFileToolName, "path is required")
	}

	root, err := resolveProjectRoot(ctx)
	if err != nil {
		return toolError(writeFileToolName, err.Error())
	}
	abs, err := resolveEditablePath(root, args.Path, writeFileToolName)
	if err != nil {
		return toolError(writeFileToolName, err.Error())
	}

	existed := false
	mode := os.FileMode(0o644)
	content := args.Content
	eol := dominantEOL(content)
	if info, statErr := os.Stat(abs); statErr == nil {
		if info.IsDir() {
			return toolError(writeFileToolName, fmt.Sprintf("%s is a directory, not a file.", args.Path))
		}
		existed = true
		mode = info.Mode().Perm()
		if existing := fileEOL(abs); existing != "" {
			eol = existing
			content = withEOL(content, eol)
		}
	}

	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return toolError(writeFileToolName, fmt.Sprintf("create parent directory for %s: %v", args.Path, err))
	}

	// A source file without a trailing newline trips linters, diff tools and
	// "\ No newline at end of file" noise in every later review.
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += eol
	}
	if err := os.WriteFile(abs, []byte(content), mode); err != nil {
		return toolError(writeFileToolName, fmt.Sprintf("write %s: %v", args.Path, err))
	}

	verb := "created"
	if existed {
		verb = "overwritten"
	}
	return domain.ToolResult{
		Name: writeFileToolName,
		Content: fmt.Sprintf("%s: %s, %d lines, %d bytes. %s",
			args.Path, verb, len(splitLines(content)), len(content), prompt.CodeWriteDiskNoteText()),
	}
}
