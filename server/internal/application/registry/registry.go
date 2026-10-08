package registry

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/rs/zerolog/log"
)

type toolRegistry struct {
	mu    sync.RWMutex
	tools map[string]port.ToolExecutor
	mcp   MCPSources
}

func New() port.ToolRegistry {
	return &toolRegistry{
		tools: make(map[string]port.ToolExecutor),
		mcp:   make(MCPSources),
	}
}

func (r *toolRegistry) Register(executor port.ToolExecutor) {
	r.mu.Lock()
	defer r.mu.Unlock()
	name := executor.Name()
	r.tools[name] = withCatalogDocs(executor)
	if src, ok := executor.(port.MCPServerTool); ok {
		r.mcp[name] = src.MCPSource()
	} else {
		delete(r.mcp, name)
	}
	log.Debug().Str("tool", name).Msg("tool registered")
}

func (r *toolRegistry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.tools, name)
	delete(r.mcp, name)
}

func (r *toolRegistry) AllToolNames() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Definitions is every registered tool, MCP access modes included — the
// admin listing; an agent's tool list always goes through DefinitionsForPolicy.
func (r *toolRegistry) Definitions() []domain.ToolDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	return r.definitionsLocked(names)
}

func (r *toolRegistry) DefinitionsForPolicy(policy domain.ToolPolicy) []domain.ToolDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		if IsToolAllowed(name, policy, r.mcp) {
			names = append(names, name)
		}
	}
	return r.definitionsLocked(names)
}

func (r *toolRegistry) definitionsLocked(names []string) []domain.ToolDefinition {
	// Map order is random per call; the tool list is the first thing in every
	// request, so an unsorted one rewrites the prompt-cache prefix every run.
	sort.Strings(names)
	defs := make([]domain.ToolDefinition, 0, len(names))
	for _, name := range names {
		defs = append(defs, r.tools[name].Definition())
	}
	return defs
}

func (r *toolRegistry) Execute(ctx context.Context, call domain.ToolCall) domain.ToolResult {
	return r.ExecuteWithPolicy(ctx, call, domain.ToolPolicy{})
}

func (r *toolRegistry) ExecuteWithPolicy(ctx context.Context, call domain.ToolCall, policy domain.ToolPolicy) domain.ToolResult {
	r.mu.RLock()
	executor, ok := r.tools[call.Function.Name]
	allowed := IsToolAllowed(call.Function.Name, policy, r.mcp)
	r.mu.RUnlock()

	if !allowed {
		return domain.ToolResult{
			ToolCallID: call.ID,
			Name:       call.Function.Name,
			Content:    fmt.Sprintf("tool not allowed by policy: %s", call.Function.Name),
			IsError:    true,
		}
	}

	if !ok {
		return domain.ToolResult{
			ToolCallID: call.ID,
			Name:       call.Function.Name,
			Content:    unknownToolMessage(call.Function.Name, r.allowedNames(policy)),
			IsError:    true,
		}
	}

	result := executor.Execute(ctx, call.Function.Arguments)
	result.ToolCallID = call.ID
	if result.IsError {
		ToolUsageFromContext(ctx).RecordError(call.Function.Name)
	} else {
		ToolUsageFromContext(ctx).Record(call.Function.Name)
	}
	return result
}

func (r *toolRegistry) allowedNames(policy domain.ToolPolicy) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	sort.Strings(names)
	return FilterToolNames(names, policy, r.mcp)
}
