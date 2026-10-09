package executor

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const maxSurfaceCalls = 4096

// SurfaceCall is one call a CLI made on its run's tool surface to a tool this
// computer serves. N counts from 1 and never repeats within a surface.
type SurfaceCall struct {
	N          int    `json:"n"`
	Name       string `json:"name"`
	IsError    bool   `json:"is_error"`
	DurationMS int64  `json:"duration_ms"`
}

// MCPCalls answers the calls after a cursor; Next is the cursor to ask with
// next time. Dropped says how many the bounded log forgot before After.
type MCPCalls struct {
	V       int           `json:"v"`
	RunID   string        `json:"run_id"`
	Calls   []SurfaceCall `json:"calls"`
	Next    int           `json:"next"`
	Dropped int           `json:"dropped,omitempty"`
	Closed  bool          `json:"closed,omitempty"`
}

type callLog struct {
	mu    sync.Mutex
	calls []SurfaceCall
	total int
}

func (l *callLog) add(name string, isError bool, took time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.total++
	l.calls = append(l.calls, SurfaceCall{N: l.total, Name: name, IsError: isError, DurationMS: took.Milliseconds()})
	if len(l.calls) > maxSurfaceCalls {
		l.calls = append([]SurfaceCall(nil), l.calls[len(l.calls)-maxSurfaceCalls:]...)
	}
}

func (l *callLog) since(after int) ([]SurfaceCall, int, int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []SurfaceCall
	dropped := 0
	for _, c := range l.calls {
		if c.N > after {
			out = append(out, c)
		}
	}
	if oldest := l.total - len(l.calls); after < oldest {
		dropped = oldest - after
	}
	return out, l.total, dropped
}

// recordingRegistry notes every call to a tool this computer serves; the
// coordination endpoint's own tools are the cloud's to count.
type recordingRegistry struct {
	port.ToolRegistry
	sources map[string]string
	log     *callLog
}

func (r *recordingRegistry) Execute(ctx context.Context, call domain.ToolCall) domain.ToolResult {
	return r.ExecuteWithPolicy(ctx, call, domain.ToolPolicy{})
}

func (r *recordingRegistry) ExecuteWithPolicy(ctx context.Context, call domain.ToolCall, policy domain.ToolPolicy) domain.ToolResult {
	started := time.Now()
	result := r.ToolRegistry.ExecuteWithPolicy(ctx, call, policy)
	if r.sources[call.Function.Name] == SourceLocal {
		r.log.add(call.Function.Name, result.IsError, time.Since(started))
	}
	return result
}

// MCPCallsSince reports the local tool calls a run's surface served after
// cursor `after`. A surface that is not open answers closed, with nothing.
func (s *Service) MCPCallsSince(runID string, after int) *MCPCalls {
	runID = strings.TrimSpace(runID)
	s.mu.Lock()
	surface, ok := s.surfaces[runID]
	s.mu.Unlock()
	if !ok {
		return &MCPCalls{V: ProtocolVersion, RunID: runID, Calls: []SurfaceCall{}, Next: max(after, 0), Closed: true}
	}
	calls, next, dropped := surface.calls.since(max(after, 0))
	if calls == nil {
		calls = []SurfaceCall{}
	}
	return &MCPCalls{V: ProtocolVersion, RunID: runID, Calls: calls, Next: next, Dropped: dropped}
}
