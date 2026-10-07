package registry

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/application/workspace"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const shellToolName = "run_terminal"

type WorkspaceRegistry struct {
	inner    port.ToolRegistry
	scopedFS port.ScopedFilesystemExecutor
}

func NewWorkspaceRegistry(inner port.ToolRegistry, scopedFS port.ScopedFilesystemExecutor) port.ToolRegistry {
	return &WorkspaceRegistry{inner: inner, scopedFS: scopedFS}
}

func (w *WorkspaceRegistry) Register(executor port.ToolExecutor) {
	w.inner.Register(executor)
}

func (w *WorkspaceRegistry) Definitions() []domain.ToolDefinition {
	return w.inner.Definitions()
}

func (w *WorkspaceRegistry) DefinitionsForPolicy(policy domain.ToolPolicy) []domain.ToolDefinition {
	return w.inner.DefinitionsForPolicy(policy)
}

func (w *WorkspaceRegistry) AllToolNames() []string {
	return w.inner.AllToolNames()
}

func (w *WorkspaceRegistry) Execute(ctx context.Context, call domain.ToolCall) domain.ToolResult {
	return w.ExecuteWithPolicy(ctx, call, domain.ToolPolicy{})
}

func (w *WorkspaceRegistry) ExecuteWithPolicy(ctx context.Context, call domain.ToolCall, policy domain.ToolPolicy) domain.ToolResult {
	if strings.HasPrefix(call.Function.Name, "mcp_filesystem_") {
		if root := EffectiveWorkspaceDir(ctx); root != "" && w.scopedFS != nil {
			return w.scopedFS.ExecuteScoped(ctx, root, call)
		}
	}
	if call.Function.Name == shellToolName {
		call = w.injectWorkingDir(ctx, call)
	}
	return w.inner.ExecuteWithPolicy(ctx, call, policy)
}

func (w *WorkspaceRegistry) injectWorkingDir(ctx context.Context, call domain.ToolCall) domain.ToolCall {
	scoped := EffectiveWorkspaceDir(ctx)
	if scoped == "" {
		return call
	}

	var args map[string]any
	if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
		args = map[string]any{}
	}
	requested, _ := args["working_dir"].(string)
	resolved, err := workspace.ResolveScopedWorkDir(requested, scoped)
	if err != nil {
		// Left as asked, so run_terminal refuses it by name. Swapping in the
		// scope root would run a command meant for another directory — an
		// `rm -rf *`, say — in the repository root instead.
		return call
	}
	args["working_dir"] = resolved
	patched, err := json.Marshal(args)
	if err != nil {
		return call
	}
	call.Function.Arguments = string(patched)
	return call
}
