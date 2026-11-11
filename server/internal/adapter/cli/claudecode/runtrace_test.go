package claudecode

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/runtrace"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type runTraceSink struct {
	mu     sync.Mutex
	events []domain.RunTraceEvent
}

func (s *runTraceSink) Begin(context.Context, domain.RunTrace) error { return nil }
func (s *runTraceSink) Header(context.Context, uuid.UUID) (domain.RunTrace, error) {
	return domain.RunTrace{}, nil
}
func (s *runTraceSink) NextSeq(context.Context, uuid.UUID) (int, error) { return 1, nil }
func (s *runTraceSink) Append(_ context.Context, evs []domain.RunTraceEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, evs...)
	return nil
}
func (s *runTraceSink) Finish(context.Context, uuid.UUID, string, int, *domain.RunTraceVerification) error {
	return nil
}
func (s *runTraceSink) List(context.Context, uuid.UUID, port.RunTraceFilter) ([]domain.RunTraceEvent, error) {
	return nil, nil
}
func (s *runTraceSink) Counts(context.Context, uuid.UUID) (domain.RunTraceCounts, error) {
	return domain.RunTraceCounts{}, nil
}
func (s *runTraceSink) Effects(context.Context, uuid.UUID) ([]domain.RunTraceEvent, error) {
	return nil, nil
}
func (s *runTraceSink) Changes(context.Context, uuid.UUID) ([]domain.RunTraceEvent, error) {
	return nil, nil
}
func (s *runTraceSink) DeleteBefore(context.Context, time.Time, int) (int, error) { return 0, nil }

func TestExecuteRecordsTheRunTrace(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{}, "trace_multistep.jsonl")
	sink := &runTraceSink{}
	rec := runtrace.Start(context.Background(), sink, runtrace.Meta{RunID: uuid.New(), TaskID: uuid.New(), AgentID: uuid.New(), WorkDir: workDir})
	require.NotNil(t, rec)
	ctx := runtrace.WithRecorder(context.Background(), rec)

	_, err := ex.Execute(ctx, taskExecution(workDir))
	require.NoError(t, err)

	got := map[string]domain.RunTraceEvent{}
	for _, ev := range sink.events {
		got[ev.NativeTool] = ev
	}
	require.Len(t, sink.events, 4, "the Read, Grep, Edit and Bash calls; the MCP endpoint records its own tool")
	require.Equal(t, domain.TraceCategoryRead, got["Read"].Category)
	require.Equal(t, "internal/application/board/runner.go", got["Read"].Target)
	require.Equal(t, domain.TraceCategoryChange, got["Edit"].Category)
	require.Equal(t, domain.TraceCategoryExec, got["Bash"].Category)
	require.Equal(t, domain.TraceStatusError, got["Bash"].Status)
	require.Equal(t, domain.TraceSourceCLI, got["Bash"].Source)
	var d map[string]any
	require.NoError(t, json.Unmarshal(got["Bash"].Detail, &d))
	require.Equal(t, "go build ./...", d["command"])
	require.NotContains(t, string(got["Edit"].Detail), `"old_string"`)
}
