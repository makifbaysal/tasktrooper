package board

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// deletingTaskManager serves one task and records the deletions asked of it.
type deletingTaskManager struct {
	*fakeTaskManager
	task    domain.BoardTask
	deleted []uuid.UUID
	err     error
}

func (d *deletingTaskManager) ListTasks(ctx context.Context, repositoryID uuid.UUID) ([]domain.BoardTask, error) {
	return []domain.BoardTask{d.task}, nil
}

func (d *deletingTaskManager) GetTask(_ context.Context, repositoryID, taskID uuid.UUID) (domain.BoardTask, error) {
	if repositoryID != d.task.RepositoryID || taskID != d.task.ID {
		return domain.BoardTask{}, domain.ErrBoardTaskNotFound
	}
	return d.task, nil
}

func (d *deletingTaskManager) ListAllTasks(ctx context.Context) ([]domain.BoardTask, error) {
	return []domain.BoardTask{d.task}, nil
}

func (d *deletingTaskManager) DeleteTask(ctx context.Context, repositoryID, taskID uuid.UUID) error {
	if d.err != nil {
		return d.err
	}
	d.deleted = append(d.deleted, taskID)
	return nil
}

func newDeleteKit(column domain.TaskColumn) (*deleteTaskTool, *deletingTaskManager, domain.BoardTask) {
	repoID := uuid.New()
	task := domain.BoardTask{
		ID: uuid.New(), RepositoryID: repoID, Key: "DE-1",
		Title: "Android button", Column: column, Priority: domain.TaskPriorityHigh,
	}
	mgr := &deletingTaskManager{fakeTaskManager: &fakeTaskManager{taskRepoID: repoID}, task: task}
	return &deleteTaskTool{kit: &ToolKit{Tasks: mgr}}, mgr, task
}

// The board key is what the user, the chat and the action ledger all say, so it
// has to be what the tool accepts — a UUID-only delete is a delete the model
// cannot perform from the conversation it was asked in.
func TestDeleteTaskAcceptsABoardKey(t *testing.T) {
	tool, mgr, task := newDeleteKit(domain.TaskColumnBacklog)

	res := tool.Execute(context.Background(), `{"task_id":"DE-1","reason":"merged into DE-4"}`)

	require.False(t, res.IsError, res.Content)
	assert.Equal(t, []uuid.UUID{task.ID}, mgr.deleted)

	var payload map[string]any
	require.NoError(t, json.Unmarshal([]byte(res.Content), &payload))
	assert.Equal(t, true, payload["deleted"])
	// id/key/title are what the session ledger builds its entry from: the board
	// row is gone, so this result is the only description of it left.
	assert.Equal(t, task.ID.String(), payload["id"])
	assert.Equal(t, "DE-1", payload["key"])
	assert.Equal(t, "Android button", payload["title"])
}

// Deleting a task that is already being worked on erases its branch history,
// comments and criteria. It is refused with the alternative named, not with a
// bare error the model will read as "try again with different arguments".
func TestDeleteTaskRefusesWorkThatHasStarted(t *testing.T) {
	tool, mgr, _ := newDeleteKit(domain.TaskColumnInProgress)

	res := tool.Execute(context.Background(), `{"task_id":"DE-1"}`)

	require.False(t, res.IsError, res.Content)
	assert.Empty(t, mgr.deleted)

	var payload map[string]any
	require.NoError(t, json.Unmarshal([]byte(res.Content), &payload))
	assert.Equal(t, false, payload["deleted"])
	assert.Contains(t, payload["hint"], "move_board_task")
	// No top-level id: a refusal must not reach the action ledger as a deletion.
	assert.Nil(t, payload["id"])
}

// force is the second step the refusal names. It exists for the case the user
// asked for that exact task to go.
func TestDeleteTaskForceRemovesStartedWork(t *testing.T) {
	tool, mgr, task := newDeleteKit(domain.TaskColumnCodeReview)

	res := tool.Execute(context.Background(), `{"task_id":"DE-1","force":true}`)

	require.False(t, res.IsError, res.Content)
	assert.Equal(t, []uuid.UUID{task.ID}, mgr.deleted)
}
