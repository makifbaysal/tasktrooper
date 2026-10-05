package claudecode

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/activity"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	usageapp "github.com/makifbaysal/tasktrooper/server/internal/application/usage"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// traceStore is an in-memory port.ActivityStore that keeps the run's steps in
// the order they were written, which is the only thing these tests assert on.
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

func (s *traceStore) types() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.steps))
	for _, st := range s.steps {
		out = append(out, st.StepType)
	}
	return out
}

// field reads one string field out of the nth step of the given type.
func (s *traceStore) payloads(stepType string) []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []map[string]any
	for _, st := range s.steps {
		if st.StepType != stepType {
			continue
		}
		var m map[string]any
		_ = json.Unmarshal(st.Payload, &m)
		out = append(out, m)
	}
	return out
}

// runTraced drives the executor against the fixture with a run recorder
// installed, exactly as the board runner does, and hands back everything the
// run wrote to its trace.
func runTraced(t *testing.T, fixture string) *traceStore {
	t.Helper()
	ex, workDir := newTestExecutor(t, Config{}, fixture)

	store := &traceStore{}
	ctx, _ := registry.ContextWithToolUsage(context.Background())
	ctx, _ = usageapp.ContextWithTokenUsage(ctx)
	ctx, rec, err := activity.StartRun(ctx, store, nil, "req-trace", "claude-opus-5")
	require.NoError(t, err)
	require.NotNil(t, rec)

	_, err = ex.Execute(ctx, taskExecution(workDir))
	require.NoError(t, err)
	return store
}

// The trace a CLI session leaves has to be a chronological history a human can
// scroll, not a single closing chip. This pins the exact sequence: a turn
// marker per assistant turn, the narration inside it, and every native tool
// call with its arguments and its result.
func TestExecuteRecordsTheWholeSessionAsATrace(t *testing.T) {
	store := runTraced(t, "trace_multistep.jsonl")

	assert.Equal(t, []string{
		"claude_code_session",

		// turn 1: narration, then Read
		"iteration_start",
		"assistant_message",
		"tool_call_start",
		"tool_call_result",

		// turn 2: narration, then Grep
		"iteration_start",
		"assistant_message",
		"tool_call_start",
		"tool_call_result",

		// turn 3: narration, then Edit
		"iteration_start",
		"assistant_message",
		"tool_call_start",
		"tool_call_result",

		// turn 4: no narration, just Bash (which fails)
		"iteration_start",
		"tool_call_start",
		"tool_call_result",

		// turn 5: narration, then the TaskTrooper tool. Its start/result pair is
		// written by the MCP endpoint where the call actually executes, so this
		// half of the pipe stays silent for it — see recordedElsewhere.
		"iteration_start",
		"assistant_message",

		// turn 6: the closing answer
		"iteration_start",
		"assistant_message",

		"claude_code_result",
	}, store.types())
}

// Every native tool call must arrive with the name the board's gates ask for,
// its arguments and a preview of what came back — the same payload shape the
// agent loop writes, so one renderer serves both agent kinds.
func TestExecuteRecordsToolCallArgumentsAndResults(t *testing.T) {
	store := runTraced(t, "trace_multistep.jsonl")

	starts := store.payloads("tool_call_start")
	require.Len(t, starts, 4)
	assert.Equal(t, "read_file", starts[0]["tool"])
	assert.Equal(t, "toolu_read", starts[0]["call_id"])
	assert.Contains(t, starts[0]["arguments"], "runner.go")
	assert.Equal(t, "grep_code", starts[1]["tool"])
	assert.Equal(t, "edit_file", starts[2]["tool"])
	assert.Equal(t, "run_terminal", starts[3]["tool"])
	assert.Contains(t, starts[3]["arguments"], "go build")

	results := store.payloads("tool_call_result")
	require.Len(t, results, 4)
	assert.Equal(t, "read_file", results[0]["tool"])
	assert.Contains(t, results[0]["content"], "package board")
	assert.Equal(t, false, results[0]["is_error"])
	assert.Equal(t, "run_terminal", results[3]["tool"])
	assert.Equal(t, true, results[3]["is_error"], "a failed Bash call is recorded as a failure")
}

// The narration between tool calls is what makes a trace readable. The terminal
// result event carries only the closing answer, so every intermediate text
// block has to be a step of its own.
func TestExecuteRecordsNarrationBetweenToolCalls(t *testing.T) {
	store := runTraced(t, "trace_multistep.jsonl")

	messages := store.payloads("assistant_message")
	require.Len(t, messages, 5)
	assert.Equal(t, "Let me look at the runner first to see how a task is dispatched.", messages[0]["content"])
	assert.Equal(t, "Now I will find every caller of execute.", messages[1]["content"])
	assert.Equal(t, "Applying the fix to the dispatch switch.", messages[2]["content"])
	assert.Equal(t, "Build is green now; moving the card.", messages[3]["content"])
	assert.Equal(t, "Done.", messages[4]["content"])
}

// The session step is the header of the whole trace: which CLI conversation
// this was and on which model.
func TestExecuteRecordsTheSessionHeaderAndFooter(t *testing.T) {
	store := runTraced(t, "trace_multistep.jsonl")

	sessions := store.payloads("claude_code_session")
	require.Len(t, sessions, 1)
	assert.Equal(t, "sess-trace", sessions[0]["cli_session_id"])
	assert.Equal(t, "claude-opus-5", sessions[0]["model"])
	assert.Equal(t, "tt-42", sessions[0]["task_key"])

	results := store.payloads("claude_code_result")
	require.Len(t, results, 1)
	assert.Equal(t, "success", results[0]["subtype"])
	assert.EqualValues(t, 11, results[0]["num_turns"])
	assert.EqualValues(t, 6, results[0]["turns"], "the assistant turns this trace actually brackets")
	assert.EqualValues(t, 5, results[0]["tool_calls"])
	assert.EqualValues(t, 1, results[0]["tool_failures"])
	assert.Equal(t, "sess-trace", results[0]["cli_session_id"])
	assert.Equal(t, "claude-opus-5", results[0]["model"])
}

// A TaskTrooper tool the session called over MCP must appear in the trace
// exactly once, and it is the MCP endpoint that writes it. Counting it here as
// well would double every board tool a session used — in the transcript and in
// the ledger the grounding gates read.
func TestExecuteDoesNotRecordTaskTrooperToolsTwice(t *testing.T) {
	store := runTraced(t, "trace_multistep.jsonl")

	for _, p := range append(store.payloads("tool_call_start"), store.payloads("tool_call_result")...) {
		assert.NotEqual(t, "move_board_task", p["tool"])
		assert.NotContains(t, p["tool"], ownToolPrefix)
	}
}

// A run with no recorder on its context (a chat turn, a test) must still work.
// Every Step call in this path is nil-safe by way of activity.FromContext
// returning nil, and this is the regression that proves nothing added to the
// trace changed that.
func TestExecuteWorksWithoutARecorder(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{}, "trace_multistep.jsonl")
	ctx, _ := registry.ContextWithToolUsage(context.Background())
	ctx, _ = usageapp.ContextWithTokenUsage(ctx)

	resp, err := ex.Execute(ctx, taskExecution(workDir))
	require.NoError(t, err)
	assert.Equal(t, "Fixed the dispatch switch and moved the card to code review.", resp.Message.Content)
}

func (s *traceStore) ListStepsByRunSince(ctx context.Context, runID uuid.UUID, _ time.Time) ([]domain.SessionStep, error) {
	return s.ListStepsByRun(ctx, runID)
}
