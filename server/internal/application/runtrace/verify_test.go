package runtrace_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/runtrace"
	"github.com/makifbaysal/tasktrooper/server/internal/application/runtrace/runtracetest"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

func changeEvent(seq int, status, detail string) domain.RunTraceEvent {
	return domain.RunTraceEvent{Seq: seq, Category: domain.TraceCategoryChange, Status: status, Detail: json.RawMessage(detail)}
}

func TestCompareFlagsFilesChangedOutsideRecordedCalls(t *testing.T) {
	files := []port.RunGitFile{
		{Path: "a.go", Status: "M"},
		{Path: "shell.txt", Status: "A"},
		{Path: "new/b.go", Status: "R", From: "old/b.go"},
	}
	changes := []domain.RunTraceEvent{
		changeEvent(1, "ok", `{"path":"a.go","changed":true}`),
		changeEvent(2, "ok", `{"path":"ghost.go","changed":true}`),
		changeEvent(3, "error", `{"path":"shell.txt"}`),
		changeEvent(4, "ok", `{"from":"old/b.go","to":"new/b.go","changed":true}`),
		changeEvent(5, "ok", `{"path":"same.go","changed":false}`),
	}
	reads := []domain.RunTraceEvent{
		{Category: domain.TraceCategoryRead, Detail: json.RawMessage(`{"path":"a.go"}`)},
		{Category: domain.TraceCategoryRead, Detail: json.RawMessage(`{"path":"c.go"}`)},
	}
	v := runtrace.Compare(files, changes, reads, 100)
	require.Equal(t, []string{"shell.txt"}, v.Unrecorded)
	require.Equal(t, []string{"ghost.go"}, v.RecordedOnly)
	require.Len(t, v.Files, 3)
	require.True(t, v.Available)
	require.Equal(t, 100-3, v.Untouched, "a.go, the renamed file and c.go are touched")
}

type fakeGit struct {
	files   []port.RunGitFile
	tracked int
	err     error
}

func (f fakeGit) HeadSHA(context.Context, string) (string, error) { return "headsha", nil }
func (f fakeGit) ChangesSince(context.Context, string, string) ([]port.RunGitFile, error) {
	return f.files, f.err
}
func (f fakeGit) TrackedFileCount(context.Context, string, string) (int, error) { return f.tracked, nil }

func TestFinalizeStampsTheComparison(t *testing.T) {
	store := runtracetest.NewMemStore()
	meta := runtrace.Meta{RunID: uuid.New(), TaskID: uuid.New(), AgentID: uuid.New(), WorkspacePath: "/w", BaseSHA: "base"}
	rec := runtrace.Start(context.Background(), store, meta)
	rec.Begin(runtrace.CallStart{Tool: "edit_file", Arguments: `{"path":"a.go"}`}).End(runtrace.CallResult{})
	runtrace.Finalize(context.Background(), store, fakeGit{files: []port.RunGitFile{{Path: "a.go", Status: "M"}, {Path: "x.sh.out", Status: "A"}}, tracked: 10}, meta, 0)
	require.Equal(t, "headsha", store.Head)
	require.NotNil(t, store.Finished)
	require.Equal(t, []string{"x.sh.out"}, store.Finished.Unrecorded)
}

func TestFinalizeExplainsWhyThereIsNoComparison(t *testing.T) {
	store := runtracetest.NewMemStore()
	meta := runtrace.Meta{RunID: uuid.New(), WorkspacePath: "/w", BaseSHA: "base"}
	runtrace.Start(context.Background(), store, meta)
	runtrace.Finalize(context.Background(), store, nil, meta, 2)
	require.Equal(t, "no_checkout", store.Finished.Reason)
	require.False(t, store.Finished.Available)
	require.Equal(t, 2, store.Dropped)

	meta.BaseSHA = ""
	runtrace.Finalize(context.Background(), store, fakeGit{}, meta, 0)
	require.Equal(t, "no_base", store.Finished.Reason)
}

func TestSummaryEffectsSkipsFailedCalls(t *testing.T) {
	ok := domain.RunTraceEvent{Seq: 1, Tool: "http_request", Status: "ok", Target: "api.x", Detail: json.RawMessage(`{"effect":"http_request"}`)}
	bad := domain.RunTraceEvent{Seq: 2, Tool: "http_request", Status: "error", Detail: json.RawMessage(`{"effect":"http_request"}`)}
	none := domain.RunTraceEvent{Seq: 3, Status: "ok", Detail: json.RawMessage(`{}`)}
	got := runtrace.SummaryEffects([]domain.RunTraceEvent{ok, bad, none})
	require.Len(t, got, 1)
	require.Equal(t, "api.x", got[0].Target)
}
