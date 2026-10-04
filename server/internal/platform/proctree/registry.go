package proctree

import (
	"context"
	"sync"
	"time"
)

// Registry keeps trees that must outlive the call that started them — a server
// an agent backgrounded with `&` to curl a moment later — until the scope that
// owns them ends: the agent run, or the whole server.
type Registry struct {
	mu    sync.Mutex
	trees map[string][]*Tree
}

// Default is the process-wide registry: agent tools track into it and
// Server.Shutdown empties it.
var Default = NewRegistry()

func NewRegistry() *Registry {
	return &Registry{trees: make(map[string][]*Tree)}
}

// Track hands t to scope; KillScope or KillAll ends it.
func (r *Registry) Track(scope string, t *Tree) {
	if t == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.trees[scope] = append(r.trees[scope], t)
}

// KillScope terminates and releases every tree tracked under scope.
func (r *Registry) KillScope(scope string, grace time.Duration) {
	r.mu.Lock()
	trees := r.trees[scope]
	delete(r.trees, scope)
	r.mu.Unlock()
	closeAll(trees, grace)
}

// KillAll terminates and releases every tracked tree, whatever its scope.
func (r *Registry) KillAll(grace time.Duration) {
	r.mu.Lock()
	var trees []*Tree
	for _, ts := range r.trees {
		trees = append(trees, ts...)
	}
	r.trees = make(map[string][]*Tree)
	r.mu.Unlock()
	closeAll(trees, grace)
}

func closeAll(trees []*Tree, grace time.Duration) {
	var wg sync.WaitGroup
	for _, t := range trees {
		wg.Add(1)
		go func(t *Tree) {
			defer wg.Done()
			t.Terminate(grace)
			t.Close()
		}(t)
	}
	wg.Wait()
}

type scopeKey struct{}

// UnscopedScope collects trees started with no run scope in the context; only
// KillAll (server shutdown) ends them.
const UnscopedScope = "unscoped"

// WithScope marks ctx as belonging to scope (e.g. "run:<id>"); the code that
// owns the scope calls Default.KillScope(scope, …) when it ends.
func WithScope(ctx context.Context, scope string) context.Context {
	return context.WithValue(ctx, scopeKey{}, scope)
}

// ScopeFrom returns the scope set by WithScope, or UnscopedScope.
func ScopeFrom(ctx context.Context) string {
	if s, ok := ctx.Value(scopeKey{}).(string); ok && s != "" {
		return s
	}
	return UnscopedScope
}
