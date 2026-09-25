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

type handBackFixture struct {
	svc    *Service
	store  *fakeReleaseStore
	tasks  *fakeTasks
	parked *fakeParked
	waker  *fakeWaker
}

func newHandBackFixture() *handBackFixture {
	store := newFakeReleaseStore()
	tasks := newFakeTasks()
	parked := newFakeParked()
	waker := &fakeWaker{}
	svc := New(Deps{
		Store: store, Tasks: tasks, ParkedTasks: parked, Waker: waker,
		Clock: func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) },
	})
	return &handBackFixture{svc: svc, store: store, tasks: tasks, parked: parked, waker: waker}
}

// L4: the no-parked-card fallback must walk back to the newest task that is
// still in done (or released), skipping one a human already moved elsewhere
// — a stale release verdict must not wake a card that left done for a
// reason.
func TestHandBackFallbackSkipsATaskAHumanMovedElsewhere(t *testing.T) {
	f := newHandBackFixture()
	repositoryID := uuid.New()
	stillDone := domain.BoardTask{ID: uuid.New(), RepositoryID: repositoryID, Column: domain.TaskColumnDone, Key: "T-1"}
	moved := domain.BoardTask{ID: uuid.New(), RepositoryID: repositoryID, Column: domain.TaskColumnNeedRevision, Key: "T-2"}
	f.tasks.tasks[stillDone.ID] = stillDone
	f.tasks.tasks[moved.ID] = moved
	created, err := f.store.Create(context.Background(), domain.Release{
		RepositoryID: repositoryID, Status: domain.ReleaseFailed,
	}, []uuid.UUID{stillDone.ID, moved.ID})
	require.NoError(t, err)

	f.svc.handBack(context.Background(), created)

	require.Len(t, f.waker.calls, 1)
	assert.Equal(t, stillDone.ID, f.waker.calls[0].task.ID, "the newest task (moved away) must be skipped in favour of the older one still in done")
}

func TestHandBackFallbackWakesNothingWhenEveryTaskLeftDone(t *testing.T) {
	f := newHandBackFixture()
	repositoryID := uuid.New()
	moved := domain.BoardTask{ID: uuid.New(), RepositoryID: repositoryID, Column: domain.TaskColumnNeedRevision, Key: "T-1"}
	f.tasks.tasks[moved.ID] = moved
	created, err := f.store.Create(context.Background(), domain.Release{
		RepositoryID: repositoryID, Status: domain.ReleaseFailed,
	}, []uuid.UUID{moved.ID})
	require.NoError(t, err)

	f.svc.handBack(context.Background(), created)

	assert.Empty(t, f.waker.calls)
}
