package board

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func analizRunner(updater TaskUpdater) *Runner {
	return &Runner{taskUpdater: updater}
}

func documentedUsage() *registry.ToolUsage {
	u := registry.NewToolUsage()
	u.Record("add_task_document")
	return u
}

func TestAdvanceToAnalizReviewMovesAFinishedAnalysis(t *testing.T) {
	agentID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnInProgress, TaskType: domain.TaskTypeAnaliz, AssigneeAgentID: &agentID}
	updater := &fakeTaskUpdater{task: task}
	r := analizRunner(updater)

	r.advanceToAnalizReview(context.Background(), runJobFor(task, agentID), documentedUsage())

	require.Len(t, updater.calls, 1)
	require.NotNil(t, updater.calls[0].Column)
	assert.Equal(t, domain.TaskColumnAnalizReview, *updater.calls[0].Column)
	assert.Equal(t, domain.TaskActorAgent, updater.calls[0].Actor)
	require.NotNil(t, updater.calls[0].ActorAgentID)
	assert.Equal(t, agentID, *updater.calls[0].ActorAgentID, "the dispatcher skips the agent that made the move; the system actor would re-dispatch this run")
}

func TestAdvanceToAnalizReviewHandsBackARevision(t *testing.T) {
	agentID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnNeedRevision, TaskType: domain.TaskTypeAnaliz}
	updater := &fakeTaskUpdater{task: task}
	r := analizRunner(updater)

	r.advanceToAnalizReview(context.Background(), runJobFor(task, agentID), documentedUsage())

	require.Len(t, updater.calls, 1)
	assert.Equal(t, domain.TaskColumnAnalizReview, *updater.calls[0].Column)
}

func TestAdvanceToAnalizReviewHandsBackARevisionRewrittenWithUpdateTaskDocument(t *testing.T) {
	agentID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnNeedRevision, TaskType: domain.TaskTypeAnaliz}
	updater := &fakeTaskUpdater{task: task}
	r := analizRunner(updater)

	usage := registry.NewToolUsage()
	usage.Record("update_task_document")

	r.advanceToAnalizReview(context.Background(), runJobFor(task, agentID), usage)

	require.Len(t, updater.calls, 1, "the analiz prompt tells a need_revision run to call update_task_document, not add_task_document, so that call alone must be enough evidence to hand off")
	assert.Equal(t, domain.TaskColumnAnalizReview, *updater.calls[0].Column)
}

func TestAdvanceToAnalizReviewHoldsARunWithNoDocument(t *testing.T) {
	agentID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnInProgress, TaskType: domain.TaskTypeAnaliz}
	updater := &fakeTaskUpdater{task: task}
	r := analizRunner(updater)

	usage := registry.NewToolUsage()
	usage.Record("read_file")

	r.advanceToAnalizReview(context.Background(), runJobFor(task, agentID), usage)

	assert.Empty(t, updater.calls, "a run that never attached a document has not finished the analysis")
}

func TestAdvanceToAnalizReviewSkipsNonAnalizTasks(t *testing.T) {
	agentID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnInProgress, TaskType: domain.TaskTypeTask}
	updater := &fakeTaskUpdater{task: task}
	r := analizRunner(updater)

	r.advanceToAnalizReview(context.Background(), runJobFor(task, agentID), documentedUsage())

	assert.Empty(t, updater.calls)
}

func TestAdvanceToAnalizReviewOnlyActsOnWorkingColumns(t *testing.T) {
	agentID := uuid.New()
	for _, column := range []domain.TaskColumn{
		domain.TaskColumnTodo, domain.TaskColumnAnalizReview,
		domain.TaskColumnDone, domain.TaskColumnReleased,
	} {
		task := domain.BoardTask{ID: uuid.New(), Column: column, TaskType: domain.TaskTypeAnaliz}
		updater := &fakeTaskUpdater{task: task}
		r := analizRunner(updater)

		r.advanceToAnalizReview(context.Background(), runJobFor(task, agentID), documentedUsage())

		assert.Empty(t, updater.calls, "column %s must be left alone", column)
	}
}

func TestAdvanceToAnalizReviewRespectsAColumnChangedDuringTheRun(t *testing.T) {
	agentID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnInProgress, TaskType: domain.TaskTypeAnaliz}
	updater := &readableUpdater{
		fakeTaskUpdater: fakeTaskUpdater{task: task},
		fresh:           domain.BoardTask{ID: task.ID, Column: domain.TaskColumnDone},
	}
	r := analizRunner(updater)

	r.advanceToAnalizReview(context.Background(), runJobFor(task, agentID), documentedUsage())

	assert.Empty(t, updater.calls)
}

func TestAdvanceToAnalizReviewCommentsWhenTheBoardRefuses(t *testing.T) {
	agentID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnInProgress, TaskType: domain.TaskTypeAnaliz}
	updater := &commentingUpdater{fakeTaskUpdater: fakeTaskUpdater{task: task, err: errors.New("board refuses the move")}}
	r := analizRunner(updater)

	r.advanceToAnalizReview(context.Background(), runJobFor(task, agentID), documentedUsage())

	require.Len(t, updater.comments, 1)
	assert.Contains(t, updater.comments[0].Content, "board refuses the move")
}

func TestAdvanceToAnalizReviewIsNilSafe(t *testing.T) {
	agentID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnInProgress, TaskType: domain.TaskTypeAnaliz}

	assert.NotPanics(t, func() {
		(&Runner{}).advanceToAnalizReview(context.Background(), runJobFor(task, agentID), documentedUsage())
	})
}
