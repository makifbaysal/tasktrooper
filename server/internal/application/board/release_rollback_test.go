package board_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/board"
	"github.com/makifbaysal/tasktrooper/server/internal/application/workflow/workflowtest"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type releaseRollbackTaskReader struct {
	task domain.BoardTask
}

func (r *releaseRollbackTaskReader) GetTask(_ context.Context, _, _ uuid.UUID) (domain.BoardTask, error) {
	return r.task, nil
}

type releaseRollbackCommenter struct {
	*releaseRollbackTaskReader
	comments []string
}

func (c *releaseRollbackCommenter) AddComment(_ context.Context, _, _ uuid.UUID, req domain.CreateTaskCommentRequest) (domain.TaskComment, error) {
	c.comments = append(c.comments, req.Content)
	return domain.TaskComment{}, nil
}

// TestReleaseRollbackRunbookNamesTheCurrentToolNotTheRemovedOne guards M12: the
// incident wake runbook used to tell the agent to call `rollback_task_release`
// with a `trigger` argument — a tool that no longer exists on the release
// engineer's allowlist (the surviving tool is `rollback_release`, reason
// `health_incident`). Without the fix this comment sends the agent looking for
// a tool the policy layer will refuse.
func TestReleaseRollbackRunbookNamesTheCurrentToolNotTheRemovedOne(t *testing.T) {
	agentA := uuid.New()
	repositoryID := uuid.New()
	task := domain.BoardTask{
		ID:           uuid.New(),
		RepositoryID: repositoryID,
		TaskType:     "task",
		Column:       domain.TaskColumnDone,
	}

	boardStore := &fakeBoardConfigStore{agentsByColumn: map[string][]uuid.UUID{"done": {agentA}}}
	events := &fakeEventStore{}
	runs := &fakeRunStore{}
	runner := &fakeRunner{}
	disp := board.NewDispatcher(boardStore, events, runs, runner, true)
	disp.SetWorkflows(workflowtest.Default().Reader())

	commenter := &releaseRollbackCommenter{releaseRollbackTaskReader: &releaseRollbackTaskReader{task: task}}
	rrd := board.NewReleaseRollbackDispatcher(disp, commenter)

	incident := domain.Incident{ID: uuid.New(), RepositoryID: repositoryID, Env: "prod", Title: "high error rate", Severity: "high"}

	err := rrd.DispatchReleaseRollback(context.Background(), domain.ReleaseAttribution{TaskID: task.ID}, incident, true)
	require.NoError(t, err)
	require.Len(t, commenter.comments, 1)

	comment := commenter.comments[0]
	require.Contains(t, comment, "rollback_release", "must direct the agent at the tool that still exists")
	require.Contains(t, comment, "reason=health_incident", "the surviving tool takes reason, not trigger")
	require.NotContains(t, comment, "rollback_task_release", "must not name the removed tool")
	require.NotContains(t, comment, "trigger=health_incident", "must not use the removed tool's argument name")
}

// TestReleaseRollbackRunbookAutoRollbackWordingMatchesTheSourceOfTruth guards
// the other half of M12: the ON/OFF sentence must read as the release's own
// delivery profile deciding, not "this environment" — the legacy deploy
// target is no longer where auto_rollback is read from.
func TestReleaseRollbackRunbookAutoRollbackWordingMatchesTheSourceOfTruth(t *testing.T) {
	for _, autoRollback := range []bool{true, false} {
		task := domain.BoardTask{ID: uuid.New(), RepositoryID: uuid.New(), TaskType: "task", Column: domain.TaskColumnDone}
		agentA := uuid.New()
		boardStore := &fakeBoardConfigStore{agentsByColumn: map[string][]uuid.UUID{"done": {agentA}}}
		disp := board.NewDispatcher(boardStore, &fakeEventStore{}, &fakeRunStore{}, &fakeRunner{}, true)
		disp.SetWorkflows(workflowtest.Default().Reader())

		commenter := &releaseRollbackCommenter{releaseRollbackTaskReader: &releaseRollbackTaskReader{task: task}}
		rrd := board.NewReleaseRollbackDispatcher(disp, commenter)

		incident := domain.Incident{ID: uuid.New(), RepositoryID: task.RepositoryID, Env: "prod", Title: "incident"}
		err := rrd.DispatchReleaseRollback(context.Background(), domain.ReleaseAttribution{TaskID: task.ID}, incident, autoRollback)
		require.NoError(t, err)

		comment := commenter.comments[0]
		require.Contains(t, comment, "this release's delivery profile")
		if autoRollback {
			require.True(t, strings.Contains(comment, "auto_rollback is ON"))
		} else {
			require.True(t, strings.Contains(comment, "auto_rollback is OFF"))
		}
	}
}
