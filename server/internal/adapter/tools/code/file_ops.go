package code

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/application/workspace"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	deleteFileToolName = "delete_file"
	moveFileToolName   = "move_file"
)

type deleteFileArgs struct {
	Path      string `json:"path"`
	Recursive bool   `json:"recursive"`
}

type moveFileArgs struct {
	From      string `json:"from"`
	To        string `json:"to"`
	Overwrite bool   `json:"overwrite"`
}

type deleteFileTool struct{}
type moveFileTool struct{}

// NewDeleteFileTool and NewMoveFileTool complete the set an agent needs to
// change a repository without shelling out: read_file, write_file, edit_file,
// edit_lines, and these two. Removing and renaming were the last operations
// that still required `rm`/`mv` through run_terminal, where they run against
// the whole filesystem rather than the workspace and leave nothing in the trace
// but a silent exit status.
func NewDeleteFileTool() port.ToolExecutor { return &deleteFileTool{} }

// NewMoveFileTool builds the move/rename tool.
func NewMoveFileTool() port.ToolExecutor { return &moveFileTool{} }

func (t *deleteFileTool) Name() string { return deleteFileToolName }

func (t *deleteFileTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: deleteFileToolName,
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type": "string",
					},
					"recursive": map[string]interface{}{
						"type": "boolean",
					},
				},
				"required": []string{"path"},
			},
		},
	}
}

func (t *deleteFileTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args deleteFileArgs
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(deleteFileToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	if strings.TrimSpace(args.Path) == "" {
		return toolError(deleteFileToolName, "path is required")
	}

	root, err := resolveProjectRoot(ctx)
	if err != nil {
		return toolError(deleteFileToolName, err.Error())
	}
	abs, err := resolveEditablePath(root, args.Path, deleteFileToolName)
	if err != nil {
		return toolError(deleteFileToolName, err.Error())
	}

	info, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return toolError(deleteFileToolName, prompt.CodeDeleteNoSuchPathText(args.Path))
		}
		return toolError(deleteFileToolName, fmt.Sprintf("stat %s: %v", args.Path, err))
	}

	if info.IsDir() {
		entries, readErr := os.ReadDir(abs)
		if readErr != nil {
			return toolError(deleteFileToolName, fmt.Sprintf("read %s: %v", args.Path, readErr))
		}
		if len(entries) > 0 && !args.Recursive {
			return toolError(deleteFileToolName, prompt.CodeDeleteNeedsRecursiveText(args.Path, len(entries)))
		}
		if err := os.RemoveAll(abs); err != nil {
			return toolError(deleteFileToolName, fmt.Sprintf("delete %s: %v", args.Path, err))
		}
		return domain.ToolResult{
			Name:    deleteFileToolName,
			Content: fmt.Sprintf("%s: directory deleted (%d entries). %s", args.Path, len(entries), prompt.CodeDeleteDiskNoteText()),
		}
	}

	if err := os.Remove(abs); err != nil {
		return toolError(deleteFileToolName, fmt.Sprintf("delete %s: %v", args.Path, err))
	}
	return domain.ToolResult{
		Name:    deleteFileToolName,
		Content: fmt.Sprintf("%s: deleted (%d bytes). %s", args.Path, info.Size(), prompt.CodeDeleteDiskNoteText()),
	}
}

func (t *moveFileTool) Name() string { return moveFileToolName }

func (t *moveFileTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: moveFileToolName,
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"from": map[string]interface{}{
						"type": "string",
					},
					"to": map[string]interface{}{
						"type": "string",
					},
					"overwrite": map[string]interface{}{
						"type": "boolean",
					},
				},
				"required": []string{"from", "to"},
			},
		},
	}
}

func (t *moveFileTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args moveFileArgs
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(moveFileToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	if strings.TrimSpace(args.From) == "" || strings.TrimSpace(args.To) == "" {
		return toolError(moveFileToolName, "from and to are both required")
	}

	root, err := resolveProjectRoot(ctx)
	if err != nil {
		return toolError(moveFileToolName, err.Error())
	}
	src, err := resolveEditablePath(root, args.From, moveFileToolName)
	if err != nil {
		return toolError(moveFileToolName, err.Error())
	}
	dst, err := resolveEditablePath(root, args.To, moveFileToolName)
	if err != nil {
		return toolError(moveFileToolName, err.Error())
	}
	if src == dst {
		return toolError(moveFileToolName, "from and to are the same path")
	}

	if _, err := os.Stat(src); err != nil {
		if os.IsNotExist(err) {
			return toolError(moveFileToolName, prompt.CodeMoveNoSuchPathText(args.From))
		}
		return toolError(moveFileToolName, fmt.Sprintf("stat %s: %v", args.From, err))
	}

	switch caseOnly, sameEntry := sameDirectoryEntry(src, dst); {
	case caseOnly:
		// Button.tsx → button.tsx on a case-insensitive filesystem: dst "exists"
		// because it IS src, and removing it to make room deletes the file being
		// moved. Rename alone changes the case.
	case sameEntry:
		return toolError(moveFileToolName, "from and to are the same path")
	default:
		if _, err := os.Stat(dst); err == nil {
			if !args.Overwrite {
				return toolError(moveFileToolName, prompt.CodeMoveDestExistsText(args.To))
			}
			if err := os.RemoveAll(dst); err != nil {
				return toolError(moveFileToolName, fmt.Sprintf("replace %s: %v", args.To, err))
			}
		}
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return toolError(moveFileToolName, fmt.Sprintf("create parent directory for %s: %v", args.To, err))
	}
	if err := os.Rename(src, dst); err != nil {
		return toolError(moveFileToolName, fmt.Sprintf("move %s to %s: %v", args.From, args.To, err))
	}

	return domain.ToolResult{
		Name:    moveFileToolName,
		Content: prompt.CodeMoveSuccessText(args.From, args.To),
	}
}

// sameDirectoryEntry reports whether src and dst already name one entry, and
// whether they differ only in case — the one shape of that a rename resolves.
// Lstat, so a symlink at dst is its own entry and not the file it points at.
func sameDirectoryEntry(src, dst string) (caseOnly, same bool) {
	srcInfo, err := os.Lstat(src)
	if err != nil {
		return false, false
	}
	dstInfo, err := os.Lstat(dst)
	if err != nil || !os.SameFile(srcInfo, dstInfo) {
		return false, false
	}
	return strings.EqualFold(src, dst), true
}

// resolveEditablePath confines a path to the workspace and refuses the ones no
// edit may touch: the root itself and anything that reaches .git, under every
// spelling the host filesystem accepts (see workspace.ResolveEditableWithinRoot).
func resolveEditablePath(root, rel, tool string) (string, error) {
	abs, err := workspace.ResolveEditableWithinRoot(root, rel)
	var protected *workspace.ProtectedPathError
	switch {
	case err == nil:
		return abs, nil
	case errors.Is(err, workspace.ErrWorkspaceRoot):
		return "", fmt.Errorf("%s cannot operate on the workspace root itself", tool)
	case errors.As(err, &protected):
		return "", fmt.Errorf("%q is protected: %s may not touch it", protected.Segment, tool)
	}
	return "", err
}
