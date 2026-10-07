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
}

func New() port.ToolRegistry {
	return &toolRegistry{
		tools: make(map[string]port.ToolExecutor),
	}
}

func (r *toolRegistry) Register(executor port.ToolExecutor) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[executor.Name()] = withCatalogDocs(executor)
	log.Debug().Str("tool", executor.Name()).Msg("tool registered")
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

func (r *toolRegistry) Definitions() []domain.ToolDefinition {
	return r.DefinitionsForPolicy(domain.ToolPolicy{})
}

func (r *toolRegistry) DefinitionsForPolicy(policy domain.ToolPolicy) []domain.ToolDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()

	allNames := make([]string, 0, len(r.tools))
	for name := range r.tools {
		allNames = append(allNames, name)
	}

	allowed := FilterToolNames(allNames, policy)
	allowedSet := make(map[string]bool, len(allowed))
	for _, n := range allowed {
		allowedSet[n] = true
	}

	names := make([]string, 0, len(allowedSet))
	for name := range allowedSet {
		if _, ok := r.tools[name]; ok {
			names = append(names, name)
		}
	}
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
	if !policy.IsZero() && !IsToolAllowed(call.Function.Name, policy, r.AllToolNames()) {
		return domain.ToolResult{
			ToolCallID: call.ID,
			Name:       call.Function.Name,
			Content:    fmt.Sprintf("tool not allowed by policy: %s", call.Function.Name),
			IsError:    true,
		}
	}

	r.mu.RLock()
	executor, ok := r.tools[call.Function.Name]
	r.mu.RUnlock()

	if !ok {
		return domain.ToolResult{
			ToolCallID: call.ID,
			Name:       call.Function.Name,
			Content:    unknownToolMessage(call.Function.Name, FilterToolNames(r.AllToolNames(), policy)),
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
