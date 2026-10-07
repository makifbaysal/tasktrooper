package llm

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// anthropicSystemText reads the system prompt out of whichever wire shape the
// builder chose — a plain string when nothing caches it, the blocks
// concatenated when a breakpoint lands on the first one.
func anthropicSystemText(t *testing.T, req anthropicRequest) string {
	t.Helper()
	switch v := req.System.(type) {
	case nil:
		return ""
	case string:
		return v
	case []anthropicSystemBlock:
		var text strings.Builder
		for _, b := range v {
			text.WriteString(b.Text)
		}
		return text.String()
	default:
		t.Fatalf("system is %T, want string or []anthropicSystemBlock", req.System)
		return ""
	}
}

// countBreakpoints walks the marshalled request the way the API does, so the
// count reflects what actually goes over the wire rather than what the structs
// look like in memory.
func countBreakpoints(t *testing.T, req anthropicRequest) int {
	t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	var probe struct {
		System json.RawMessage `json:"system"`
		Tools  []struct {
			CacheControl *anthropicCacheControl `json:"cache_control"`
		} `json:"tools"`
		Messages []struct {
			Content []struct {
				CacheControl *anthropicCacheControl `json:"cache_control"`
			} `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}

	n := 0
	var sysBlocks []anthropicSystemBlock
	if len(probe.System) > 0 && json.Unmarshal(probe.System, &sysBlocks) == nil {
		for _, b := range sysBlocks {
			if b.CacheControl != nil {
				n++
			}
		}
	}
	for _, tool := range probe.Tools {
		if tool.CacheControl != nil {
			n++
		}
	}
	for _, m := range probe.Messages {
		for _, c := range m.Content {
			if c.CacheControl != nil {
				n++
			}
		}
	}
	return n
}

func twoTools() []domain.ToolDefinition {
	return []domain.ToolDefinition{
		{Type: "function", Function: domain.FunctionDefinition{Name: "read_file", Description: "read"}},
		{Type: "function", Function: domain.FunctionDefinition{Name: "write_file", Description: "write"}},
	}
}

// The full-house case: tools, a system prompt, a stable head and a live tail.
// All four breakpoints must be spent, each on the boundary it is meant for.
func TestAnthropicRequestPlacesAllFourBreakpoints(t *testing.T) {
	msgs := []domain.Message{
		{Role: domain.RoleSystem, Content: "You are backend-developer."},
		{Role: domain.RoleUser, Content: "Fix the failing build."},
		{Role: domain.RoleAssistant, Content: "Looking."},
		{Role: domain.RoleUser, Content: "Any luck?"},
	}
	// The head is the first three domain messages; one of them is a system turn
	// that never becomes an Anthropic message, so the anchor must land on the
	// assistant turn, not on index 2 of the Anthropic array.
	req := buildAnthropicRequest("claude-sonnet-4", msgs, twoTools(), false, nil, 3)

	if got := countBreakpoints(t, req); got != 4 {
		t.Fatalf("breakpoints = %d, want 4", got)
	}

	if req.Tools[0].CacheControl != nil {
		t.Error("first tool must not carry a breakpoint; only the last one caches the array")
	}
	if req.Tools[1].CacheControl == nil {
		t.Error("last tool definition is missing its breakpoint")
	}

	blocks, ok := req.System.([]anthropicSystemBlock)
	if !ok {
		t.Fatalf("system is %T, want the block form so it can carry cache_control", req.System)
	}
	if blocks[0].CacheControl == nil {
		t.Error("system block is missing its breakpoint")
	}
	if blocks[0].Type != "text" {
		t.Errorf("system block type = %q, want \"text\"", blocks[0].Type)
	}

	// Anthropic messages: [0] user "Fix the failing build.", [1] assistant
	// "Looking.", [2] user "Any luck?". The anchor covers domain[:3] → index 1.
	if len(req.Messages) != 3 {
		t.Fatalf("messages = %d, want 3", len(req.Messages))
	}
	if req.Messages[0].Content[0].CacheControl != nil {
		t.Error("message 0 is inside the head, not its boundary — it must not carry a breakpoint")
	}
	if req.Messages[1].Content[0].CacheControl == nil {
		t.Error("anchor breakpoint missing from the last message of the stable head")
	}
	if req.Messages[2].Content[0].CacheControl == nil {
		t.Error("rolling breakpoint missing from the final message")
	}
}

// Without tools and without an anchor there are only two candidates left, and
// spending a breakpoint on a boundary that does not exist would waste it.
func TestAnthropicRequestPlacesFewerBreakpointsWhenTargetsAreMissing(t *testing.T) {
	msgs := []domain.Message{
		{Role: domain.RoleSystem, Content: "You are backend-developer."},
		{Role: domain.RoleUser, Content: "hi"},
	}
	req := buildAnthropicRequest("claude-sonnet-4", msgs, nil, false, nil, 0)

	if got := countBreakpoints(t, req); got != 2 {
		t.Fatalf("breakpoints = %d, want 2 (system + rolling)", got)
	}
	if len(req.Tools) != 0 {
		t.Fatalf("tools = %d, want none", len(req.Tools))
	}
	if req.Messages[0].Content[0].CacheControl == nil {
		t.Error("rolling breakpoint missing from the only message")
	}
}

// With no system prompt the field must stay a plain string (here: absent), not
// become an empty block that the API would reject.
func TestAnthropicRequestWithoutSystemKeepsTheFieldEmpty(t *testing.T) {
	req := buildAnthropicRequest("claude-sonnet-4", []domain.Message{
		{Role: domain.RoleUser, Content: "hi"},
	}, nil, false, nil, 0)

	if req.System != nil {
		t.Errorf("system = %#v, want nil so omitempty drops it", req.System)
	}
	if got := countBreakpoints(t, req); got != 1 {
		t.Errorf("breakpoints = %d, want 1 (rolling only)", got)
	}
}

// An anchor pointing at the very end of the conversation is the same message
// the rolling breakpoint wants. Two cache_control markers on one block would be
// a wasted breakpoint at best; the anchor must yield.
func TestAnthropicRequestDoesNotDoubleMarkTheFinalMessage(t *testing.T) {
	msgs := []domain.Message{
		{Role: domain.RoleUser, Content: "one"},
		{Role: domain.RoleUser, Content: "two"},
	}
	req := buildAnthropicRequest("claude-sonnet-4", msgs, nil, false, nil, len(msgs))

	if got := countBreakpoints(t, req); got != 1 {
		t.Fatalf("breakpoints = %d, want 1 — the anchor and the rolling mark are the same block", got)
	}
	if req.Messages[0].Content[0].CacheControl != nil {
		t.Error("breakpoint landed on the wrong message")
	}
}

// The hard ceiling: a fifth cache_control is a 400, so no combination of inputs
// may produce one.
func TestAnthropicRequestNeverExceedsFourBreakpoints(t *testing.T) {
	var msgs []domain.Message
	msgs = append(msgs, domain.Message{Role: domain.RoleSystem, Content: "persona"})
	for i := range 20 {
		role := domain.RoleUser
		if i%2 == 1 {
			role = domain.RoleAssistant
		}
		msgs = append(msgs, domain.Message{Role: role, Content: "turn"})
	}

	for anchor := 0; anchor <= len(msgs); anchor++ {
		req := buildAnthropicRequest("claude-sonnet-4", msgs, twoTools(), false, nil, anchor)
		if got := countBreakpoints(t, req); got > maxAnthropicBreakpoints {
			t.Fatalf("anchor %d: breakpoints = %d, want <= %d", anchor, got, maxAnthropicBreakpoints)
		}
	}
}

// An out-of-range anchor is a caller bug the builder has to absorb: a trim can
// leave fewer messages than the head claimed, and panicking on it would take
// down the run.
func TestAnthropicRequestClampsAnOutOfRangeAnchor(t *testing.T) {
	msgs := []domain.Message{{Role: domain.RoleUser, Content: "hi"}}
	for _, anchor := range []int{-5, 99} {
		req := buildAnthropicRequest("claude-sonnet-4", msgs, nil, false, nil, anchor)
		if got := countBreakpoints(t, req); got != 1 {
			t.Errorf("anchor %d: breakpoints = %d, want 1", anchor, got)
		}
	}
}

// A breakpoint on an empty block anchors nothing, so it must be skipped rather
// than burned.
func TestAnthropicRequestSkipsBreakpointsOnEmptyContent(t *testing.T) {
	req := buildAnthropicRequest("claude-sonnet-4", []domain.Message{
		{Role: domain.RoleUser, Content: ""},
	}, nil, false, nil, 0)

	if got := countBreakpoints(t, req); got != 0 {
		t.Errorf("breakpoints = %d, want 0 — an empty text block cannot anchor a cache entry", got)
	}
}

// The streamed path bills the same tokens as the buffered one, so it must send
// the same breakpoints; a stream that skipped them would quietly pay full price
// for every turn.
func TestAnthropicStreamRequestCarriesTheSameBreakpoints(t *testing.T) {
	msgs := []domain.Message{
		{Role: domain.RoleSystem, Content: "persona"},
		{Role: domain.RoleUser, Content: "one"},
		{Role: domain.RoleAssistant, Content: "two"},
		{Role: domain.RoleUser, Content: "three"},
	}
	buffered := buildAnthropicRequest("claude-sonnet-4", msgs, twoTools(), false, nil, 3)
	streamed := buildAnthropicRequest("claude-sonnet-4", msgs, twoTools(), true, nil, 3)

	if got, want := countBreakpoints(t, streamed), countBreakpoints(t, buffered); got != want {
		t.Fatalf("streamed breakpoints = %d, buffered = %d", got, want)
	}
	if !streamed.Stream {
		t.Error("stream flag lost")
	}
}

// A tool_result block is a legal cache boundary, and after a tool chain it is
// exactly where the rolling breakpoint belongs.
func TestAnthropicRequestMarksToolResultBlocks(t *testing.T) {
	req := buildAnthropicRequest("claude-sonnet-4", []domain.Message{
		{Role: domain.RoleUser, Content: "read the file"},
		{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{
			ID:       "call_1",
			Type:     "function",
			Function: domain.FunctionCall{Name: "read_file", Arguments: `{"path":"a.go"}`},
		}}},
		{Role: domain.RoleTool, ToolCallID: "call_1", Content: "package main"},
	}, nil, false, nil, 0)

	last := req.Messages[len(req.Messages)-1]
	block := last.Content[len(last.Content)-1]
	if block.Type != "tool_result" {
		t.Fatalf("last block type = %q, want tool_result", block.Type)
	}
	if block.CacheControl == nil {
		t.Error("rolling breakpoint missing from the tool_result block")
	}
}

// Anthropic reports input_tokens EXCLUDING cache; domain.Usage promises the
// total. Getting this backwards under-reports a cached turn's prompt by orders
// of magnitude and hands billing a number it will happily charge nothing for.
func TestAnthropicUsageNormalizesCacheTokensIntoPromptTotal(t *testing.T) {
	got := anthropicUsage{
		InputTokens:              100,
		OutputTokens:             25,
		CacheReadInputTokens:     900,
		CacheCreationInputTokens: 50,
	}.toDomain()

	want := domain.Usage{
		PromptTokens:     1050,
		CompletionTokens: 25,
		TotalTokens:      1075,
		CacheReadTokens:  900,
		CacheWriteTokens: 50,
	}
	if got != want {
		t.Errorf("usage = %+v, want %+v", got, want)
	}
}

func TestAnthropicUsageWithoutCacheIsUnchanged(t *testing.T) {
	got := anthropicUsage{InputTokens: 300, OutputTokens: 40}.toDomain()
	want := domain.Usage{PromptTokens: 300, CompletionTokens: 40, TotalTokens: 340}
	if got != want {
		t.Errorf("usage = %+v, want %+v", got, want)
	}
}

// The persona is the same bytes for every task the agent runs; the notes after
// it (workspace path, score, memories) are not. Only the persona carries the
// system breakpoint, so a new task still reads it from the cache.
func TestAnthropicRequestCachesThePersonaApartFromThePerTaskNotes(t *testing.T) {
	build := func(workspace string) anthropicRequest {
		return buildAnthropicRequest("claude-sonnet-4", []domain.Message{
			{Role: domain.RoleSystem, Content: "You are backend-developer."},
			{Role: domain.RoleSystem, Content: "Workspace: " + workspace},
			{Role: domain.RoleSystem, Content: "Score: 91"},
			{Role: domain.RoleUser, Content: "Fix the build."},
			{Role: domain.RoleAssistant, Content: "Looking."},
			{Role: domain.RoleUser, Content: "Any luck?"},
		}, twoTools(), false, nil, 5)
	}
	first, second := build("/ws/task-1"), build("/ws/task-2")

	blocks, ok := first.System.([]anthropicSystemBlock)
	if !ok || len(blocks) != 2 {
		t.Fatalf("system = %#v, want two blocks", first.System)
	}
	if blocks[0].Text != "You are backend-developer." || blocks[0].CacheControl == nil {
		t.Errorf("first block = %+v, want the persona with the breakpoint", blocks[0])
	}
	if blocks[1].CacheControl != nil {
		t.Error("the per-task notes must not carry a breakpoint")
	}
	if got := anthropicSystemText(t, first); got != "You are backend-developer.\n\nWorkspace: /ws/task-1\n\nScore: 91" {
		t.Errorf("concatenated system = %q, want the old joined text", got)
	}
	if got := countBreakpoints(t, first); got != 4 {
		t.Errorf("breakpoints = %d, want 4 (tools, persona, anchor, rolling)", got)
	}

	persona := func(req anthropicRequest) string {
		raw, err := json.Marshal(req.System.([]anthropicSystemBlock)[0])
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		return string(raw)
	}
	if persona(first) != persona(second) {
		t.Error("the cached persona block changed with the task")
	}
}

func TestProviderStopReasonsAreNormalized(t *testing.T) {
	for raw, want := range map[string]string{
		"max_tokens": domain.StopReasonMaxTokens,
		"length":     domain.StopReasonMaxTokens,
		"MAX_TOKENS": domain.StopReasonMaxTokens,
		"tool_use":   domain.StopReasonToolUse,
		"tool_calls": domain.StopReasonToolUse,
		"end_turn":   domain.StopReasonEnd,
		"STOP":       domain.StopReasonEnd,
		"":           "",
	} {
		if got := domain.NormalizeStopReason(raw); got != want {
			t.Errorf("%q → %q, want %q", raw, got, want)
		}
	}
}
