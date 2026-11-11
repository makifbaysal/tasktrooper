package board

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/runtrace"
	"github.com/makifbaysal/tasktrooper/server/internal/application/runtrace/runtracetest"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type inspectingGit struct {
	port.GitClient
	head  string
	files []port.RunGitFile
}

func (g inspectingGit) HeadSHA(context.Context, string) (string, error) { return g.head, nil }
func (g inspectingGit) ChangesSince(context.Context, string, string) ([]port.RunGitFile, error) {
	return g.files, nil
}
func (g inspectingGit) TrackedFileCount(context.Context, string, string) (int, error) { return 5, nil }

func TestRunTraceStartsAtTheTaskWorkspaceHeadAndClosesWithTheGitComparison(t *testing.T) {
	store := runtracetest.NewMemStore()
	r := &Runner{traces: store, git: inspectingGit{head: "abc123", files: []port.RunGitFile{{Path: "shell.out", Status: "?"}}}}
	job := RunJob{Run: domain.TaskAgentRun{ID: uuid.New(), AgentID: uuid.New()}, Task: domain.BoardTask{ID: uuid.New()}}

	rec := r.startRunTrace(context.Background(), job, "/ws", "/ws", "tt/branch", domain.TaskColumn("todo"))
	require.NotNil(t, rec)
	h, err := store.Header(context.Background(), job.Run.ID)
	require.NoError(t, err)
	require.Equal(t, "abc123", h.BaseSHA)
	require.Equal(t, "todo", h.ColumnBefore)
	require.Equal(t, "tt/branch", h.Branch)

	rec.Open(runtrace.CallStart{CallID: "x", Tool: "run_terminal", Arguments: `{"command":"make"}`})
	r.finishRunTrace(context.Background(), rec)
	require.Equal(t, domain.TraceStatusUnfinished, store.BySeq(1).Status)
	require.Equal(t, []string{"shell.out"}, store.Finished.Unrecorded)
	require.Equal(t, "abc123", store.Head)
}

func TestRunTraceIsOffWithoutAStore(t *testing.T) {
	r := &Runner{}
	require.Nil(t, r.startRunTrace(context.Background(), RunJob{}, "", "", "", ""))
	r.finishRunTrace(context.Background(), nil)
}
