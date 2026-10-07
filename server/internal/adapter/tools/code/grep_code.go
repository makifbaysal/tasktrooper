package code

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/application/mapper"
	"github.com/makifbaysal/tasktrooper/server/internal/application/workspace"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const grepCodeToolName = "grep_code"

type grepCodeArgs struct {
	Pattern       string `json:"pattern"`
	Path          string `json:"path"`
	Glob          string `json:"glob"`
	MaxResults    int    `json:"max_results"`
	CaseSensitive bool   `json:"case_sensitive"`
}

type grepMatch struct {
	FilePath string `json:"file_path"`
	Line     int    `json:"line"`
	Content  string `json:"content"`
}

type grepCodeResponse struct {
	Matches []grepMatch `json:"matches"`
}

type grepCodeTool struct {
	kit      *ToolKit
	lookPath func(string) (string, error)
}

func newGrepCodeTool(kit *ToolKit) port.ToolExecutor {
	return &grepCodeTool{kit: kit, lookPath: exec.LookPath}
}

func (t *grepCodeTool) Name() string {
	return grepCodeToolName
}

func (t *grepCodeTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: grepCodeToolName,
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"pattern": map[string]interface{}{
						"type": "string",
					},
					"path": map[string]interface{}{
						"type": "string",
					},
					"glob": map[string]interface{}{
						"type": "string",
					},
					"max_results": map[string]interface{}{
						"type": "integer",
					},
					"case_sensitive": map[string]interface{}{
						"type": "boolean",
					},
				},
				"required": []string{"pattern"},
			},
		},
	}
}

func (t *grepCodeTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args grepCodeArgs
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(grepCodeToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	if args.Pattern == "" {
		return toolError(grepCodeToolName, "pattern is required")
	}

	root, err := resolveProjectRoot(ctx)
	if err != nil {
		return toolError(grepCodeToolName, err.Error())
	}

	// `path` arrives as model output and used to be joined onto the root with
	// nothing checking the result. filepath.Join Cleans instead of confining, so
	// "../../../../etc" became a valid absolute path and ripgrep dumped it —
	// including the pod's secrets on disk. grep_code is one of the tools the
	// deliberately restricted "cursor" API key is allowed to call precisely
	// because it was supposed to stay inside the workspace.
	searchPath := root
	if args.Path != "" {
		resolved, err := workspace.ResolveWithinRoot(root, args.Path)
		if err != nil {
			return toolError(grepCodeToolName, err.Error())
		}
		searchPath = resolved
	}

	maxResults := args.MaxResults
	if maxResults <= 0 {
		maxResults = 100
	}

	rg, err := t.lookPath("rg")
	if err != nil {
		matches, err := searchWithoutRipgrep(ctx, root, searchPath, args, maxResults)
		if err != nil {
			return toolError(grepCodeToolName, err.Error())
		}
		return toolJSON(grepCodeToolName, grepCodeResponse{Matches: matches})
	}

	// --null ends the path with NUL instead of ':' — a Windows path's drive
	// colon otherwise split every line in the wrong place and every match was
	// dropped. --with-filename because rg omits the path when `path` names a
	// single file, and a line without one cannot be parsed.
	rgArgs := []string{
		"--line-number",
		"--no-heading",
		"--null",
		"--with-filename",
		"--color=never",
		"--max-count", strconv.Itoa(maxResults),
	}

	// A model searching for UI copy or a symbol it half-remembers types the
	// casing it saw in the ticket, not the casing in the file, and a
	// case-sensitive miss reads back as "this does not exist in the repo".
	// Default to ignoring case and let a caller that needs exact casing opt in.
	// The same applies to `glob`: a "*.TSX" filter that silently matches nothing
	// is the same dead end one layer up, so the file filter follows the pattern.
	if args.CaseSensitive {
		rgArgs = append(rgArgs, "--case-sensitive")
	} else {
		rgArgs = append(rgArgs, "--ignore-case", "--glob-case-insensitive")
	}

	if args.Glob != "" {
		rgArgs = append(rgArgs, "--glob", args.Glob)
	}

	patterns, err := mapper.GitignorePatterns(root)
	if err != nil {
		return toolError(grepCodeToolName, fmt.Sprintf("load gitignore: %v", err))
	}
	for _, pat := range patterns {
		rgArgs = append(rgArgs, "--glob", "!"+pat)
	}

	rgArgs = append(rgArgs, args.Pattern, searchPath)

	cmd := exec.CommandContext(ctx, rg, rgArgs...)
	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return toolJSON(grepCodeToolName, grepCodeResponse{Matches: []grepMatch{}})
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return toolError(grepCodeToolName, msg)
	}

	matches := parseRipgrepOutput(out.String(), root)
	if len(matches) > maxResults {
		matches = matches[:maxResults]
	}

	return toolJSON(grepCodeToolName, grepCodeResponse{Matches: matches})
}

func parseRipgrepOutput(output, root string) []grepMatch {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	matches := make([]grepMatch, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			continue
		}
		filePath, rest, ok := strings.Cut(line, "\x00")
		if !ok {
			continue
		}
		num, content, ok := strings.Cut(rest, ":")
		if !ok {
			continue
		}
		lineNum, err := strconv.Atoi(num)
		if err != nil {
			continue
		}
		if rel, err := filepath.Rel(root, filePath); err == nil {
			filePath = filepath.ToSlash(rel)
		} else {
			filePath = filepath.ToSlash(filePath)
		}
		matches = append(matches, grepMatch{
			FilePath: filePath,
			Line:     lineNum,
			Content:  strings.TrimSuffix(content, "\r"),
		})
	}
	return matches
}
