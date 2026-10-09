package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type recordingCreatedObserver struct {
	tasks    []domain.BoardTask
	sessions []uuid.UUID
}

func (r *recordingCreatedObserver) TaskCreated(ctx context.Context, task domain.BoardTask) {
	r.tasks = append(r.tasks, task)
	r.sessions = append(r.sessions, registry.SessionIDFromContext(ctx))
}

// Issue sync links the tasks a product manager opens in an import's
// conversion chat by the chat on the caller's context, so the observer must
// get that context, not a fresh one.
func TestCreateTaskTellsTheObserverWithTheCallersContext(t *testing.T) {
	f := newAssigneeFixture()
	observer := &recordingCreatedObserver{}
	f.svc.SetTaskCreatedObserver(observer)
	sessionID := uuid.New()

	task, err := f.svc.CreateTask(registry.ContextWithSessionID(context.Background(), sessionID), f.repoID,
		domain.CreateBoardTaskRequest{Title: "Sign in with email"})

	require.NoError(t, err)
	require.Len(t, observer.tasks, 1)
	require.Equal(t, task.ID, observer.tasks[0].ID)
	require.Equal(t, sessionID, observer.sessions[0])
}
