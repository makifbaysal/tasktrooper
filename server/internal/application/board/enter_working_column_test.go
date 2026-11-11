package board

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/workflow/workflowtest"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type fakeTaskUpdater struct {
	calls    []domain.UpdateBoardTaskRequest
	comments []domain.CreateTaskCommentRequest
	task     domain.BoardTask
	err      error
}

func (f *fakeTaskUpdater) UpdateTask(_ context.Context, _, _ uuid.UUID, req domain.UpdateBoardTaskRequest) (domain.BoardTask, error) {
	f.calls = append(f.calls, req)
	if f.err != nil {
		return domain.BoardTask{}, f.err
	}
	out := f.task
	if req.Column != nil {
		out.Column = *req.Column
	}
	return out, nil
}

func (f *fakeTaskUpdater) AddComment(_ context.Context, _, _ uuid.UUID, req domain.CreateTaskCommentRequest) (domain.TaskComment, error) {
	f.comments = append(f.comments, req)
	return domain.TaskComment{}, nil
}

func (f *fakeTaskUpdater) ListComments(_ context.Context, _, _ uuid.UUID) ([]domain.TaskComment, error) {
	return nil, nil
}

func runJobFor(task domain.BoardTask, agentID uuid.UUID) RunJob {
	return RunJob{
		Task:         task,
		Run:          domain.TaskAgentRun{ID: uuid.New(), TaskID: task.ID, AgentID: agentID},
		RepositoryID: uuid.New(),
	}
}

func TestEnterWorkingColumnMovesTheAssigneesTask(t *testing.T) {
	agentID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnTodo, AssigneeAgentID: &agentID}
	updater := &fakeTaskUpdater{task: task}
	r := &Runner{taskUpdater: updater, workflows: workflowtest.Default().Reader()}

	out := r.enterWorkingColumn(context.Background(), runJobFor(task, agentID))

	assert.Equal(t, domain.TaskColumnInProgress, out.Column)
	require.Len(t, updater.calls, 1)
	require.NotNil(t, updater.calls[0].Column)
	assert.Equal(t, domain.TaskColumnInProgress, *updater.calls[0].Column)
}

func TestEnterWorkingColumnAttributesTheMoveToTheAgent(t *testing.T) {
	agentID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnTodo, AssigneeAgentID: &agentID}
	updater := &fakeTaskUpdater{task: task}
	r := &Runner{taskUpdater: updater, workflows: workflowtest.Default().Reader()}

	r.enterWorkingColumn(context.Background(), runJobFor(task, agentID))

	require.Len(t, updater.calls, 1)
	assert.Equal(t, domain.TaskActorAgent, updater.calls[0].Actor)
	require.NotNil(t, updater.calls[0].ActorAgentID)
	assert.Equal(t, agentID, *updater.calls[0].ActorAgentID)
}

func TestEnterWorkingColumnLeavesUnassignedTasksAlone(t *testing.T) {
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnTodo}
	updater := &fakeTaskUpdater{task: task}
	r := &Runner{taskUpdater: updater, workflows: workflowtest.Default().Reader()}

	out := r.enterWorkingColumn(context.Background(), runJobFor(task, uuid.New()))

	assert.Equal(t, domain.TaskColumnTodo, out.Column)
	assert.Empty(t, updater.calls)
}

func TestEnterWorkingColumnLeavesAnotherAgentsTaskAlone(t *testing.T) {
	assignee := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnTodo, AssigneeAgentID: &assignee}
	updater := &fakeTaskUpdater{task: task}
	r := &Runner{taskUpdater: updater, workflows: workflowtest.Default().Reader()}

	out := r.enterWorkingColumn(context.Background(), runJobFor(task, uuid.New()))

	assert.Equal(t, domain.TaskColumnTodo, out.Column)
	assert.Empty(t, updater.calls)
}

func TestEnterWorkingColumnOnlyActsOnTheQueues(t *testing.T) {
	agentID := uuid.New()
	for _, column := range []domain.TaskColumn{
		domain.TaskColumnInProgress,
		domain.TaskColumnCodeReview,
		domain.TaskColumnInQA,
		domain.TaskColumnHumanUAT,
	} {
		task := domain.BoardTask{ID: uuid.New(), Column: column, AssigneeAgentID: &agentID}
		updater := &fakeTaskUpdater{task: task}
		r := &Runner{taskUpdater: updater, workflows: workflowtest.Default().Reader()}

		out := r.enterWorkingColumn(context.Background(), runJobFor(task, agentID))

		assert.Equal(t, column, out.Column)
		assert.Empty(t, updater.calls, "column %s must be left alone", column)
	}
}

func TestEnterWorkingColumnMovesNeedRevisionIntoProgress(t *testing.T) {
	agentID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnNeedRevision, AssigneeAgentID: &agentID}
	updater := &fakeTaskUpdater{task: task}
	r := &Runner{taskUpdater: updater, workflows: workflowtest.Default().Reader()}

	out := r.enterWorkingColumn(context.Background(), runJobFor(task, agentID))

	assert.Equal(t, domain.TaskColumnInProgress, out.Column)
	assert.Len(t, updater.calls, 1)

	other := uuid.New()
	foreign := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnNeedRevision, AssigneeAgentID: &other}
	foreignUpdater := &fakeTaskUpdater{task: foreign}
	r = &Runner{taskUpdater: foreignUpdater, workflows: workflowtest.Default().Reader()}

	out = r.enterWorkingColumn(context.Background(), runJobFor(foreign, agentID))

	assert.Equal(t, domain.TaskColumnNeedRevision, out.Column)
	assert.Empty(t, foreignUpdater.calls)
}

func TestRunInstructionKeepsTheRevisionFraming(t *testing.T) {
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnInProgress}
	job := RunJob{Task: task, EnteredFrom: domain.TaskColumnNeedRevision}

	assert.Contains(t, runInstruction(taskWF, job), "came back from review")

	job = RunJob{Task: task, EnteredFrom: domain.TaskColumnTodo}
	assert.NotContains(t, runInstruction(taskWF, job), "came back from review")
}

func TestEnterWorkingColumnSurvivesAFailedUpdate(t *testing.T) {
	agentID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnTodo, AssigneeAgentID: &agentID}
	updater := &fakeTaskUpdater{task: task, err: errors.New("pool closed")}
	r := &Runner{taskUpdater: updater, workflows: workflowtest.Default().Reader()}

	out := r.enterWorkingColumn(context.Background(), runJobFor(task, agentID))

	assert.Equal(t, task.ID, out.ID)
	assert.Equal(t, domain.TaskColumnTodo, out.Column)
}

func TestEnterWorkingColumnIsNilSafe(t *testing.T) {
	agentID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnTodo, AssigneeAgentID: &agentID}
	r := &Runner{}

	assert.Equal(t, domain.TaskColumnTodo, r.enterWorkingColumn(context.Background(), runJobFor(task, agentID)).Column)
}

func TestColumnInstructionFollowsTheAutomaticMove(t *testing.T) {
	agentID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnTodo, AssigneeAgentID: &agentID}
	r := &Runner{taskUpdater: &fakeTaskUpdater{task: task}, workflows: workflowtest.Default().Reader()}

	moved := r.enterWorkingColumn(context.Background(), runJobFor(task, agentID))

	assert.Contains(t, columnInstruction(taskWF, moved), "ALREADY in `in_progress`")
	assert.NotContains(t, columnInstruction(taskWF, moved), "move it to in_progress")
}

func TestEnterWorkingColumnTakesAQATaskIntoInQA(t *testing.T) {
	agentID := uuid.New()
	developer := uuid.New()

	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnReadyForQA, AssigneeAgentID: &developer}
	updater := &fakeTaskUpdater{task: task}
	r := &Runner{taskUpdater: updater, workflows: workflowtest.Default().Reader()}

	out := r.enterWorkingColumn(context.Background(), runJobFor(task, agentID))

	assert.Equal(t, domain.TaskColumnInQA, out.Column)
	require.Len(t, updater.calls, 1)
	require.NotNil(t, updater.calls[0].Column)
	assert.Equal(t, domain.TaskColumnInQA, *updater.calls[0].Column)

	assert.Equal(t, domain.TaskActorAgent, updater.calls[0].Actor)
	require.NotNil(t, updater.calls[0].ActorAgentID)
	assert.Equal(t, agentID, *updater.calls[0].ActorAgentID)
}

func TestEnterWorkingColumnLeavesAnalizOutOfInQA(t *testing.T) {
	agentID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnReadyForQA, TaskType: "analiz"}
	updater := &fakeTaskUpdater{task: task}
	r := &Runner{taskUpdater: updater, workflows: workflowtest.Default().Reader()}

	out := r.enterWorkingColumn(context.Background(), runJobFor(task, agentID))

	assert.Equal(t, domain.TaskColumnReadyForQA, out.Column)
	assert.Empty(t, updater.calls)
}

func TestColumnInstructionFollowsTheAutomaticQAMove(t *testing.T) {
	agentID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnReadyForQA}
	r := &Runner{taskUpdater: &fakeTaskUpdater{task: task}, workflows: workflowtest.Default().Reader()}

	moved := r.enterWorkingColumn(context.Background(), runJobFor(task, agentID))

	assert.Contains(t, columnInstruction(taskWF, moved), "ALREADY in `in_qa`")
	assert.NotContains(t, columnInstruction(taskWF, moved), "Move it to in_qa")
}

func TestColumnInstructionAsksForTheMoveWhenItWasRefused(t *testing.T) {
	agentID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnReadyForQA}
	r := &Runner{taskUpdater: &fakeTaskUpdater{task: task, err: errors.New("pool closed")}, workflows: workflowtest.Default().Reader()}

	stuck := r.enterWorkingColumn(context.Background(), runJobFor(task, agentID))

	assert.Equal(t, domain.TaskColumnReadyForQA, stuck.Column)
	// "Move it to in_qa yourself" now lives in qa-agent's own ready_for_qa.md
	// (catalogrepo's TestQACatalogCarriesTheExecutionRule); the engine's own
	// fact line only states that the automatic move did not go through.
	assert.Contains(t, columnInstruction(taskWF, stuck), "the automatic move into `in_qa` did not go through")
}
