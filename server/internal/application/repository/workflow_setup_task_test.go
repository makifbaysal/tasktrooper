package repository

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type workflowSetupTaskStore struct {
	*assigneeTaskStore
}

func (s workflowSetupTaskStore) Get(_ context.Context, _ uuid.UUID, taskID uuid.UUID) (domain.BoardTask, error) {
	task, ok := s.tasks[taskID]
	if !ok {
		return domain.BoardTask{}, fmt.Errorf("%w: %s", domain.ErrBoardTaskNotFound, taskID)
	}
	return task, nil
}

func newWorkflowSetupFixture() (*Service, uuid.UUID, workflowSetupTaskStore) {
	f := newAssigneeFixture()
	store := workflowSetupTaskStore{assigneeTaskStore: f.tasks}
	f.svc.tasks = store
	return f.svc, f.repoID, store
}

func TestCreateWorkflowSetupTaskReturnsTheOpenTaskInsteadOfADuplicate(t *testing.T) {
	svc, repoID, store := newWorkflowSetupFixture()
	ctx := context.Background()

	first, created, err := svc.CreateWorkflowSetupTask(ctx, repoID)
	require.NoError(t, err)
	assert.True(t, created)

	again, created, err := svc.CreateWorkflowSetupTask(ctx, repoID)
	require.NoError(t, err)
	assert.False(t, created)
	assert.Equal(t, first.ID, again.ID)
	assert.Len(t, store.tasks, 1)

	open, ok, err := svc.WorkflowSetupTask(ctx, repoID)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, first.ID, open.ID)
}

func TestCreateWorkflowSetupTaskOpensANewOneOnceTheLastIsGone(t *testing.T) {
	for name, finish := range map[string]func(store workflowSetupTaskStore, id uuid.UUID){
		"deleted": func(store workflowSetupTaskStore, id uuid.UUID) { delete(store.tasks, id) },
		"done": func(store workflowSetupTaskStore, id uuid.UUID) {
			task := store.tasks[id]
			task.Column = domain.TaskColumnDone
			store.tasks[id] = task
		},
		"released": func(store workflowSetupTaskStore, id uuid.UUID) {
			task := store.tasks[id]
			task.Column = domain.TaskColumnReleased
			store.tasks[id] = task
		},
	} {
		t.Run(name, func(t *testing.T) {
			svc, repoID, store := newWorkflowSetupFixture()
			ctx := context.Background()

			first, _, err := svc.CreateWorkflowSetupTask(ctx, repoID)
			require.NoError(t, err)
			finish(store, first.ID)

			_, ok, err := svc.WorkflowSetupTask(ctx, repoID)
			require.NoError(t, err)
			assert.False(t, ok)

			next, created, err := svc.CreateWorkflowSetupTask(ctx, repoID)
			require.NoError(t, err)
			assert.True(t, created)
			assert.NotEqual(t, first.ID, next.ID)
		})
	}
}

func TestCreateWorkflowSetupTaskKeepsBlockingWhileTheTaskIsInFlight(t *testing.T) {
	svc, repoID, store := newWorkflowSetupFixture()
	ctx := context.Background()

	first, _, err := svc.CreateWorkflowSetupTask(ctx, repoID)
	require.NoError(t, err)
	task := store.tasks[first.ID]
	task.Column = domain.TaskColumnCodeReview
	store.tasks[first.ID] = task

	again, created, err := svc.CreateWorkflowSetupTask(ctx, repoID)
	require.NoError(t, err)
	assert.False(t, created)
	assert.Equal(t, first.ID, again.ID)
}
