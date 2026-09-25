package release

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type finishFixture struct {
	svc    *Service
	store  *fakeReleaseStore
	tasks  *fakeTasks
	parked *fakeParked
}

func newFinishFixture() *finishFixture {
	store := newFakeReleaseStore()
	tasks := newFakeTasks()
	parked := newFakeParked()
	svc := New(Deps{
		Store:       store,
		Tasks:       tasks,
		ParkedTasks: parked,
		Clock:       func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) },
	})
	return &finishFixture{svc: svc, store: store, tasks: tasks, parked: parked}
}

func TestFinishByAgentMovesTasksToReleased(t *testing.T) {
	f := newFinishFixture()
	repositoryID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), RepositoryID: repositoryID, Column: domain.TaskColumnDone, Key: "T-1"}
	f.tasks.tasks[task.ID] = task
	f.parked.parked[task.ID] = task
	created, err := f.store.Create(context.Background(), domain.Release{
		RepositoryID: repositoryID, Status: domain.ReleaseAwaitingVerdict,
	}, []uuid.UUID{task.ID})
	require.NoError(t, err)

	updated, err := f.svc.Finish(context.Background(), created.ID, domain.ReleaseActorAgent, "logs clean, no new errors")
	require.NoError(t, err)

	assert.Equal(t, domain.ReleaseReleased, updated.Status)
	assert.Equal(t, "logs clean, no new errors", updated.Verdict)
	require.NotNil(t, updated.FinishedAt)
	require.Len(t, f.tasks.updates, 1)
	assert.Equal(t, domain.TaskColumnReleased, *f.tasks.updates[0].Column)
	assert.Equal(t, domain.MoveReasonReleaseVerified, f.tasks.updates[0].SystemReason)
	assert.Contains(t, f.parked.taken, task.ID, "a parked card must be claimed before it is moved")
}

func TestFinishRefusesAnAgentOnAFailedRelease(t *testing.T) {
	f := newFinishFixture()
	repositoryID := uuid.New()
	created, err := f.store.Create(context.Background(), domain.Release{
		RepositoryID: repositoryID, Status: domain.ReleaseFailed,
	}, nil)
	require.NoError(t, err)

	_, err = f.svc.Finish(context.Background(), created.ID, domain.ReleaseActorAgent, "ship it anyway")
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrReleaseWrongStatus)
}

func TestFinishAllowsAHumanToShipAFailedRelease(t *testing.T) {
	f := newFinishFixture()
	repositoryID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), RepositoryID: repositoryID, Column: domain.TaskColumnDone}
	f.tasks.tasks[task.ID] = task
	created, err := f.store.Create(context.Background(), domain.Release{
		RepositoryID: repositoryID, Status: domain.ReleaseFailed,
	}, []uuid.UUID{task.ID})
	require.NoError(t, err)

	updated, err := f.svc.Finish(context.Background(), created.ID, domain.ReleaseActorHuman, "ship it anyway")
	require.NoError(t, err)
	assert.Equal(t, domain.ReleaseReleased, updated.Status)
}

func TestFinishRefusesAReleaseStillDeploying(t *testing.T) {
	f := newFinishFixture()
	repositoryID := uuid.New()
	created, err := f.store.Create(context.Background(), domain.Release{
		RepositoryID: repositoryID, Status: domain.ReleaseDeploying,
	}, nil)
	require.NoError(t, err)

	_, err = f.svc.Finish(context.Background(), created.ID, domain.ReleaseActorAgent, "note")
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrReleaseWrongStatus)
}
