package registry

import (
	"context"
	"sync"
)

const toolUsageKey contextKey = "tool_usage"

// ToolUsage counts the tools one run actually executed successfully.
//
// It exists so a check can be based on what an agent DID rather than on what
// it says it did: an analiz run once produced a full technical spec after
// eight tool calls, none of which read the repository. The model's own summary
// ("I have created the spec file") cannot distinguish that from real work —
// the tool ledger can.
//
// The tracker is carried in the context rather than returned from the agent
// loop because a board run fans out into orchestrator subtasks, each with its
// own loop; they all execute through the same registry and therefore share one
// tracker.
type ToolUsage struct {
	mu sync.Mutex
	// counts holds successes only. Every gate built on this tracker asks "did
	// the agent actually do X", and a failed call is not evidence that it did,
	// so failures are counted separately rather than mixed in here.
	counts map[string]int
	errors map[string]int
}

// NewToolUsage returns an empty tracker.
func NewToolUsage() *ToolUsage {
	return &ToolUsage{counts: map[string]int{}, errors: map[string]int{}}
}

// Record counts one successful execution of the named tool. Failed calls are
// not evidence of anything, so callers pass only successful ones.
func (u *ToolUsage) Record(name string) {
	if u == nil || name == "" {
		return
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.counts == nil {
		u.counts = map[string]int{}
	}
	u.counts[name]++
}

// RecordError counts one failed execution of the named tool.
//
// Failures used to be dropped on the floor: they existed in the agent loop's
// own memory for the length of the run and nowhere else. The run row now stores
// them, which is what lets the next run be told which tools kept rejecting this
// one and lets the tool_error_rate KPI measure it.
func (u *ToolUsage) RecordError(name string) {
	if u == nil || name == "" {
		return
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.errors == nil {
		u.errors = map[string]int{}
	}
	u.errors[name]++
}

// Failures copies the per-tool failure counts.
func (u *ToolUsage) Failures() map[string]int {
	if u == nil {
		return nil
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	out := make(map[string]int, len(u.errors))
	for name, n := range u.errors {
		out[name] = n
	}
	return out
}

// Totals returns the run's total tool calls and how many of them failed.
func (u *ToolUsage) Totals() (calls, failures int) {
	if u == nil {
		return 0, 0
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	for _, n := range u.counts {
		calls += n
	}
	for _, n := range u.errors {
		failures += n
		calls += n
	}
	return calls, failures
}

// Count returns how many times the named tool succeeded.
func (u *ToolUsage) Count(name string) int {
	if u == nil {
		return 0
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.counts[name]
}

// Snapshot copies the current counts. Two snapshots around one agent loop give
// that loop's own tool calls even though the tracker is shared by every subtask
// of the run — which is how a per-subtask check reads a per-run ledger.
func (u *ToolUsage) Snapshot() map[string]int {
	if u == nil {
		return nil
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	out := make(map[string]int, len(u.counts))
	for name, n := range u.counts {
		out[name] = n
	}
	return out
}

// UsageDelta returns what happened between two snapshots.
func UsageDelta(before, after map[string]int) map[string]int {
	delta := make(map[string]int, len(after))
	for name, n := range after {
		if diff := n - before[name]; diff > 0 {
			delta[name] = diff
		}
	}
	return delta
}

// UsedAny reports whether any of the named tools succeeded at least once.
func (u *ToolUsage) UsedAny(names ...string) bool {
	if u == nil {
		return false
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	for _, name := range names {
		if u.counts[name] > 0 {
			return true
		}
	}
	return false
}

// ContextWithToolUsage installs a fresh tracker for one run.
func ContextWithToolUsage(ctx context.Context) (context.Context, *ToolUsage) {
	usage := NewToolUsage()
	return context.WithValue(ctx, toolUsageKey, usage), usage
}

// ToolUsageFromContext returns the run's tracker, or nil when the caller did
// not install one (chat sessions, tests). A nil tracker answers every query
// with "unknown", and every gate built on it must treat that as "do not
// block" — an unmeasured run is not a failed run.
func ToolUsageFromContext(ctx context.Context) *ToolUsage {
	if v := ctx.Value(toolUsageKey); v != nil {
		if usage, ok := v.(*ToolUsage); ok {
			return usage
		}
	}
	return nil
}

const traceSourceKey contextKey = "trace_source"

// ContextWithTraceSource says which door a tool call came through (the MCP
// endpoint an agent CLI calls back on) for the run trace; the in-process loop
// is the default.
func ContextWithTraceSource(ctx context.Context, source string) context.Context {
	return context.WithValue(ctx, traceSourceKey, source)
}

func traceSource(ctx context.Context) string {
	if s, ok := ctx.Value(traceSourceKey).(string); ok {
		return s
	}
	return ""
}
