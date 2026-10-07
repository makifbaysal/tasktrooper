package code

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

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
		"--max-columns", strconv.Itoa(grepMaxColumns),
		"--max-columns-preview",
		// rg reads .gitignore only inside a git repository unless told
		// otherwise; a workspace that is a plain directory must still keep
		// its ignored trees out, as Walk does for the fallback.
		"--no-require-git",
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

	rgArgs = append(rgArgs, args.Pattern, searchPath)

	matches, err := runRipgrep(ctx, rg, rgArgs, root, maxResults)
	if err != nil {
		return toolError(grepCodeToolName, err.Error())
	}
	return toolJSON(grepCodeToolName, grepCodeResponse{Matches: matches})
}

// runRipgrep reads matches as rg prints them and stops it once maxResults are
// in: --max-count only caps matches per file, so a common pattern over a big
// tree used to be searched to the end and then thrown away.
func runRipgrep(ctx context.Context, rg string, rgArgs []string, root string, maxResults int) ([]grepMatch, error) {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(runCtx, rg, rgArgs...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	matches := []grepMatch{}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for len(matches) < maxResults && scanner.Scan() {
		if m, ok := parseRipgrepLine(scanner.Text(), root); ok {
			matches = append(matches, m)
		}
	}
	capped := len(matches) >= maxResults
	if capped {
		cancel()
	}
	scanErr := scanner.Err()
	waitErr := cmd.Wait()
	if capped {
		return matches, nil
	}
	if scanErr != nil {
		return nil, fmt.Errorf("read ripgrep output: %w", scanErr)
	}
	if waitErr != nil {
		if exitErr, ok := waitErr.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return []grepMatch{}, nil
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = waitErr.Error()
		}
		return nil, errors.New(msg)
	}
	return matches, nil
}

func parseRipgrepOutput(output, root string) []grepMatch {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	matches := make([]grepMatch, 0, len(lines))
	for _, line := range lines {
		if m, ok := parseRipgrepLine(line, root); ok {
			matches = append(matches, m)
		}
	}
	return matches
}

func parseRipgrepLine(line, root string) (grepMatch, bool) {
	if line == "" {
		return grepMatch{}, false
	}
	filePath, rest, ok := strings.Cut(line, "\x00")
	if !ok {
		return grepMatch{}, false
	}
	num, content, ok := strings.Cut(rest, ":")
	if !ok {
		return grepMatch{}, false
	}
	lineNum, err := strconv.Atoi(num)
	if err != nil {
		return grepMatch{}, false
	}
	if rel, err := filepath.Rel(root, filePath); err == nil {
		filePath = filepath.ToSlash(rel)
	} else {
		filePath = filepath.ToSlash(filePath)
	}
	return grepMatch{
		FilePath: filePath,
		Line:     lineNum,
		Content:  strings.TrimSuffix(content, "\r"),
	}, true
}
