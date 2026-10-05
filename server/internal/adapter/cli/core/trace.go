package core

import (
	"context"
	"strings"
	"sync"

	"github.com/makifbaysal/tasktrooper/server/internal/application/activity"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// Sink is what a stream parser reports session events into.
type Sink interface {
	OnSession(sessionID, model string)
	OnTurn()
	OnAssistantText(text string)
	OnToolUse(callID, name, arguments string)
	OnToolResult(callID, name, content string, isError bool)
}

// ParentScoper is implemented by sinks that can attribute the steps they
// record to a parent tool call (a sub-agent's work under its Task call). A
// parser calls it before dispatching an event's blocks; "" is the main agent.
type ParentScoper interface {
	SetParentCall(callID string)
}

// NewStreamingSink wraps an inner sink with a ChatStream so a caller watching
// the turn sees assistant text and turn boundaries as they are produced. With
// no watcher it is the inner sink itself. (Claude Code's stream wiring echoes
// segment separators into the watcher itself and so keeps its own copy.)
func NewStreamingSink(inner Sink, out port.ChatStream) Sink {
	if out.OnText == nil && out.OnSegmentBreak == nil {
		return inner
	}
	return &streamingSink{inner: inner, out: out}
}

type streamingSink struct {
	inner    Sink
	out      port.ChatStream
	streamed bool
}

func (s *streamingSink) SetParentCall(callID string) {
	if ps, ok := s.inner.(ParentScoper); ok {
		ps.SetParentCall(callID)
	}
}

func (s *streamingSink) OnSession(sessionID, model string) { s.inner.OnSession(sessionID, model) }

func (s *streamingSink) OnTurn() { s.inner.OnTurn() }

func (s *streamingSink) OnAssistantText(text string) {
	s.inner.OnAssistantText(text)
	if text != "" {
		s.out.Text(text)
		s.streamed = true
	}
}

func (s *streamingSink) OnToolUse(callID, name, arguments string) {
	s.inner.OnToolUse(callID, name, arguments)
	if s.streamed {
		s.out.SegmentBreak()
		s.streamed = false
	}
}

func (s *streamingSink) OnToolResult(callID, name, content string, isError bool) {
	s.inner.OnToolResult(callID, name, content, isError)
}

const tracePreviewMax = 500

// Trace records a CLI session into the activity ledger and tool-usage registry
// under one flavor's step name and native-tool vocabulary.
type Trace struct {
	ctx     context.Context
	taskKey string
	step    string
	sinceOwnTool func(string) bool
	ledgerTool    func(string) string

	mu        sync.Mutex
	sessionID string
	model     string
	turns     int
	parent    string
}

func (t *Trace) SetParentCall(callID string) {
	t.mu.Lock()
	t.parent = callID
	t.mu.Unlock()
}

func (t *Trace) parentCall() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.parent
}

func (t *Trace) withParent(payload map[string]any) map[string]any {
	if p := t.parentCall(); p != "" {
		payload["parent_call_id"] = p
	}
	return payload
}

func NewTrace(ctx context.Context, taskKey, step string, sinceOwnTool func(string) bool, ledgerTool func(string) string) *Trace {
	return &Trace{ctx: ctx, taskKey: taskKey, step: step, sinceOwnTool: sinceOwnTool, ledgerTool: ledgerTool}
}

func (t *Trace) OnSession(sessionID, model string) {
	t.mu.Lock()
	t.sessionID, t.model = sessionID, model
	t.mu.Unlock()
	if rec := activity.FromContext(t.ctx); rec != nil {
		rec.Step(t.step, map[string]string{
			"cli_session_id": sessionID,
			"model":          model,
			"task_key":       t.taskKey,
		})
	}
}

func (t *Trace) OnTurn() {
	t.mu.Lock()
	t.turns++
	turn := t.turns
	t.mu.Unlock()
	if rec := activity.FromContext(t.ctx); rec != nil {
		rec.Step("iteration_start", t.withParent(map[string]any{"iteration": turn}))
	}
}

func (t *Trace) Turns() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.turns
}

func (t *Trace) Model() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.model
}

func (t *Trace) SessionID() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.sessionID
}

func (t *Trace) OnAssistantText(text string) {
	if rec := activity.FromContext(t.ctx); rec != nil {
		rec.Step("assistant_message", t.withParent(map[string]any{"content": text}))
	}
}

func (t *Trace) OnToolUse(callID, name, arguments string) {
	if t.sinceOwnTool(name) {
		return
	}
	if rec := activity.FromContext(t.ctx); rec != nil {
		rec.Step("tool_call_start", t.withParent(map[string]any{
			"tool": t.ledgerTool(name), "call_id": callID, "arguments": arguments,
		}))
	}
}

func (t *Trace) OnToolResult(callID, name, content string, isError bool) {
	if t.sinceOwnTool(name) {
		return
	}
	ledgerName := t.ledgerTool(name)
	if isError {
		registry.ToolUsageFromContext(t.ctx).RecordError(ledgerName)
	} else {
		registry.ToolUsageFromContext(t.ctx).Record(ledgerName)
	}
	if rec := activity.FromContext(t.ctx); rec != nil {
		rec.Step("tool_call_result", t.withParent(map[string]any{
			"tool": ledgerName, "call_id": callID,
			"content":  domain.TruncateHead(content, tracePreviewMax),
			"is_error": isError,
		}))
	}
}

// HiddenScriptedFilter returns a sinceOwnTool predicate for flavors whose own
// tools carry the tasktrooper marker anywhere in their name.
func HiddenScriptedFilter(marker string) func(string) bool {
	return func(name string) bool {
		return strings.Contains(strings.ToLower(name), marker)
	}
}

// PrefixOwnToolFilter returns a sinceOwnTool predicate for flavors whose own
// tools arrive under the mcp__server__ name.
func PrefixOwnToolFilter(serverName string) func(string) bool {
	prefix := "mcp__" + serverName + "__"
	return func(name string) bool {
		return strings.HasPrefix(name, prefix)
	}
}