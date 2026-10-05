package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/activity"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// traceStore keeps the run's steps in the order they were written.
type traceStore struct {
	mu    sync.Mutex
	steps []domain.SessionStep
}

func (s *traceStore) CreateRun(context.Context, *uuid.UUID, string, string) (domain.SessionRun, error) {
	return domain.SessionRun{ID: uuid.New()}, nil
}
func (s *traceStore) CompleteRun(context.Context, uuid.UUID, string) error { return nil }
func (s *traceStore) CancelRun(context.Context, uuid.UUID) (bool, error)   { return false, nil }
func (s *traceStore) RunStatus(context.Context, uuid.UUID) (string, error) { return "", nil }
func (s *traceStore) AppendStep(_ context.Context, runID uuid.UUID, stepType string, payload []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.steps = append(s.steps, domain.SessionStep{
		ID: uuid.New(), RunID: runID, StepType: stepType, Payload: json.RawMessage(payload),
	})
	return nil
}
func (s *traceStore) ListRunsBySession(context.Context, uuid.UUID, int) ([]domain.SessionRun, error) {
	return nil, nil
}
func (s *traceStore) ListStepsByRun(context.Context, uuid.UUID) ([]domain.SessionStep, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]domain.SessionStep(nil), s.steps...), nil
}
func (s *traceStore) ListActiveRuns(context.Context) ([]domain.SessionRun, error) { return nil, nil }

func (s *traceStore) recorded() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]map[string]any, 0, len(s.steps))
	for _, st := range s.steps {
		var payload map[string]any
		_ = json.Unmarshal(st.Payload, &payload)
		out = append(out, map[string]any{"type": st.StepType, "payload": payload})
	}
	return out
}

// tracedRun mints a run whose context carries an activity recorder, exactly as
// the board runner's runCtx does.
func tracedRun(t *testing.T, policy domain.ToolPolicy) (Run, *traceStore) {
	t.Helper()
	store := &traceStore{}
	ctx, rec, err := activity.StartRun(context.Background(), store, nil, "req-mcp", "claude-opus-5")
	require.NoError(t, err)
	require.NotNil(t, rec)
	return Run{Ctx: ctx, Policy: policy, TaskKey: "tt-9"}, store
}

// A TaskTrooper tool a Claude Code session calls over MCP has to appear in the
// run's trace. It is deliberately NOT recorded on the CLI side (see
// recordedElsewhere in adapter/agentcli/claudecode) so it cannot be
// double-counted — which left it recorded nowhere at all until this endpoint
// wrote it. A session that moved its card showed no step for having done so.
func TestCallToolWritesTheRunTrace(t *testing.T) {
	reg := &fakeRegistry{
		defs:    fullCatalog(),
		results: map[string]domain.ToolResult{"move_board_task": {Name: "move_board_task", Content: "moved to code_review"}},
	}
	run, store := tracedRun(t, domain.ToolPolicy{})
	app, _, token := newTestServer(t, reg, run)

	resp, body := call(t, app, token,
		`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"move_board_task","arguments":{"column":"code_review"}}}`)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	_, isError := callResultOf(t, body)
	require.False(t, isError)

	steps := store.recorded()
	require.Len(t, steps, 2, "exactly one start and one result — never zero, never a duplicated pair")

	assert.Equal(t, "tool_call_start", steps[0]["type"])
	start := steps[0]["payload"].(map[string]any)
	assert.Equal(t, "move_board_task", start["tool"], "the plain tool name, not the mcp__tasktrooper__ spelling")
	assert.Contains(t, start["arguments"], "code_review")

	assert.Equal(t, "tool_call_result", steps[1]["type"])
	result := steps[1]["payload"].(map[string]any)
	assert.Equal(t, "move_board_task", result["tool"])
	assert.Equal(t, "moved to code_review", result["content"])
	assert.Equal(t, false, result["is_error"])
	assert.Equal(t, start["call_id"], result["call_id"], "the pair has to share a call id or the UI cannot join them")
}

// A failing tool is recorded as a failure, with the text the model was given.
func TestCallToolWritesAFailedResultToTheTrace(t *testing.T) {
	reg := &fakeRegistry{
		defs: fullCatalog(),
		results: map[string]domain.ToolResult{
			"move_board_task": {Name: "move_board_task", Content: "column does not exist", IsError: true},
		},
	}
	run, store := tracedRun(t, domain.ToolPolicy{})
	app, _, token := newTestServer(t, reg, run)

	_, body := call(t, app, token,
		`{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"move_board_task","arguments":{"column":"nope"}}}`)
	_, isError := callResultOf(t, body)
	require.True(t, isError)

	steps := store.recorded()
	require.Len(t, steps, 2)
	result := steps[1]["payload"].(map[string]any)
	assert.Equal(t, true, result["is_error"])
	assert.Equal(t, "column does not exist", result["content"])
}

// A call this endpoint refuses is a turn the session spent, so it belongs in the
// trace with the reason it was refused. A silent refusal reads on the board as a
// turn that did nothing at all.
func TestCallToolTracesARefusedCall(t *testing.T) {
	reg := &fakeRegistry{defs: fullCatalog()}
	run, store := tracedRun(t, domain.ToolPolicy{AllowTools: []string{"read_file"}})
	app, _, token := newTestServer(t, reg, run)

	_, body := call(t, app, token,
		`{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"move_board_task","arguments":{}}}`)
	_, isError := callResultOf(t, body)
	require.True(t, isError)

	assert.Empty(t, reg.calls(), "a refused call never reaches the registry")
	steps := store.recorded()
	require.Len(t, steps, 2)
	result := steps[1]["payload"].(map[string]any)
	assert.Equal(t, true, result["is_error"])
	assert.Contains(t, result["content"], "tool policy")
}

// A run with no recorder (a chat session, a test) must keep working: the trace
// is a nicety and a tool call is not.
func TestCallToolWithoutARecorder(t *testing.T) {
	reg := &fakeRegistry{defs: fullCatalog()}
	app, _, token := newTestServer(t, reg, Run{})

	_, body := call(t, app, token,
		`{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"move_board_task","arguments":{}}}`)
	_, isError := callResultOf(t, body)
	assert.False(t, isError)
	assert.Len(t, reg.calls(), 1)
}

func (s *traceStore) ListStepsByRunSince(ctx context.Context, runID uuid.UUID, _ time.Time) ([]domain.SessionStep, error) {
	return s.ListStepsByRun(ctx, runID)
}
