package board_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/board"
	"github.com/makifbaysal/tasktrooper/server/internal/application/workflow/workflowtest"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// TestReleaseWakerWakesTheParkedCard exercises the same wiring
// board/deploy_sweeper.go relies on for its own resume, but keyed on
// release_watch: Wake must dispatch a task.moved event carrying
// resumed_resource=release_watch so dispatcher.go's deployWatchWake carve-out
// (not the ordinary done-column suspension) fires and a run is enqueued.
func TestReleaseWakerWakesTheParkedCard(t *testing.T) {
	agentA := uuid.New()
	boardStore := &fakeBoardConfigStore{agentsByColumn: map[string][]uuid.UUID{"done": {agentA}}}
	events := &fakeEventStore{}
	runs := &fakeRunStore{}
	runner := &fakeRunner{}
	disp := board.NewDispatcher(boardStore, events, runs, runner, true)
	disp.SetWorkflows(workflowtest.Default().Reader())

	waker := board.NewReleaseWaker(disp)

	repositoryID := uuid.New()
	task := domain.BoardTask{
		ID:           uuid.New(),
		RepositoryID: repositoryID,
		TaskType:     "task",
		Column:       domain.TaskColumnDone,
	}

	err := waker.Wake(context.Background(), repositoryID, task, domain.ReleaseAwaitingVerdict)
	require.NoError(t, err)

	require.Len(t, events.events, 1)
	require.Len(t, runs.runs, 1)
	require.Len(t, runner.jobs, 1)
	require.Equal(t, agentA, runner.jobs[0].Run.AgentID)
}

func TestReleaseWakerIsNilSafe(t *testing.T) {
	var waker *board.ReleaseWaker
	err := waker.Wake(context.Background(), uuid.New(), domain.BoardTask{}, domain.ReleaseFailed)
	require.NoError(t, err)
}
