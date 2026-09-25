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

type stubDeployParkStore struct {
	parked     []domain.BoardTask
	takenIDs   []uuid.UUID
	listErr    error
	takeMisses map[uuid.UUID]bool
}

func (s *stubDeployParkStore) ListBlockedByResource(context.Context, string, int) ([]domain.BoardTask, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.parked, nil
}

func (s *stubDeployParkStore) TakeBlockedResourceTask(_ context.Context, _ string, taskID uuid.UUID) (domain.BoardTask, bool, error) {
	if s.takeMisses[taskID] {
		return domain.BoardTask{}, false, nil
	}
	s.takenIDs = append(s.takenIDs, taskID)
	for _, t := range s.parked {
		if t.ID == taskID {
			return t, true, nil
		}
	}
	return domain.BoardTask{}, false, nil
}

type stubDeployStatus struct {
	byTask map[uuid.UUID]domain.DeployWatchState
	err    error
	calls  int
}

func (s *stubDeployStatus) StatusForTask(_ context.Context, task domain.BoardTask) (domain.DeployWatchStatus, error) {
	s.calls++
	if s.err != nil {
		return domain.DeployWatchStatus{}, s.err
	}
	return domain.DeployWatchStatus{TaskID: task.ID, State: s.byTask[task.ID]}, nil
}

func deployParkedTask(col domain.TaskColumn) domain.BoardTask {
	return domain.BoardTask{
		ID:             uuid.New(),
		RepositoryID:   uuid.New(),
		Column:         col,
		TaskType:       "task",
		MergeCommitSHA: "abc123def456789012345678901234567890abcd",
	}
}

func TestDeploySweepLeavesARunningDeployParked(t *testing.T) {
	task := deployParkedTask(domain.TaskColumnDone)
	store := &stubDeployParkStore{parked: []domain.BoardTask{task}}
	watch := &stubDeployStatus{byTask: map[uuid.UUID]domain.DeployWatchState{task.ID: domain.DeployWatchPending}}
	s := NewDeploySweeper(store, watch, &Dispatcher{})

	s.sweep(context.Background())

	assert.Equal(t, 1, watch.calls, "the sweep should ask once per parked task")
	assert.Empty(t, store.takenIDs, "a task whose deploy is still running must stay parked")
}

func TestDeploySweepClaimsOnlyTheTasksWhoseDeploySettled(t *testing.T) {
	stillRunning := deployParkedTask(domain.TaskColumnDone)
	finished := deployParkedTask(domain.TaskColumnReleased)
	store := &stubDeployParkStore{parked: []domain.BoardTask{stillRunning, finished}}
	watch := &stubDeployStatus{byTask: map[uuid.UUID]domain.DeployWatchState{
		stillRunning.ID: domain.DeployWatchPending,
		finished.ID:     domain.DeployWatchFailure,
	}}
	s := NewDeploySweeper(store, watch, &Dispatcher{})

	s.sweep(context.Background())

	require.Len(t, store.takenIDs, 1)
	assert.Equal(t, finished.ID, store.takenIDs[0],
		"the newer task whose deploy finished must be resumed, not the older one still waiting")
}

func TestDeploySweepLeavesTaskParkedWhenTheWatchFails(t *testing.T) {
	task := deployParkedTask(domain.TaskColumnDone)
	store := &stubDeployParkStore{parked: []domain.BoardTask{task}}
	watch := &stubDeployStatus{err: errors.New("github timeout")}
	s := NewDeploySweeper(store, watch, &Dispatcher{})

	s.sweep(context.Background())

	assert.Empty(t, store.takenIDs)
}

func TestDeploySweepToleratesLosingTheClaim(t *testing.T) {
	task := deployParkedTask(domain.TaskColumnDone)
	store := &stubDeployParkStore{
		parked:     []domain.BoardTask{task},
		takeMisses: map[uuid.UUID]bool{task.ID: true},
	}
	watch := &stubDeployStatus{byTask: map[uuid.UUID]domain.DeployWatchState{task.ID: domain.DeployWatchSuccess}}
	s := NewDeploySweeper(store, watch, &Dispatcher{})

	s.sweep(context.Background())
}

func TestDeployWatchWakeOnlyFiresForTheSweepersResumePayload(t *testing.T) {
	resume := map[string]interface{}{domain.EventPayloadResumedResource: domain.ResourceDeployWatch}

	cases := []struct {
		name    string
		task    domain.BoardTask
		event   domain.BoardEventType
		payload map[string]interface{}
		want    bool
	}{
		{"sweeper resume in done", deployParkedTask(domain.TaskColumnDone), domain.BoardEventTaskMoved, resume, true},
		{"sweeper resume in released", deployParkedTask(domain.TaskColumnReleased), domain.BoardEventTaskMoved, resume, true},
		{"release sweeper resume in done", deployParkedTask(domain.TaskColumnDone), domain.BoardEventTaskMoved,
			map[string]interface{}{domain.EventPayloadResumedResource: domain.ResourceReleaseWatch}, true},
		{"release sweeper resume in released", deployParkedTask(domain.TaskColumnReleased), domain.BoardEventTaskMoved,
			map[string]interface{}{domain.EventPayloadResumedResource: domain.ResourceReleaseWatch}, true},
		{"an ordinary move into done", deployParkedTask(domain.TaskColumnDone), domain.BoardEventTaskMoved, nil, false},
		{"a comment on a done card", deployParkedTask(domain.TaskColumnDone), domain.BoardEventTaskCommented, resume, false},
		{"a resume payload in a working column", deployParkedTask(domain.TaskColumnInProgress), domain.BoardEventTaskMoved, resume, false},
		{"the device park's resume", deployParkedTask(domain.TaskColumnDone), domain.BoardEventTaskMoved,
			map[string]interface{}{domain.EventPayloadResumedResource: domain.ResourceMobileDevice}, false},
	}
	taskWF := workflowtest.Default().Workflows["task"]
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := deployWatchWake(taskWF, true, DispatchInput{Task: tc.task, EventType: tc.event, Payload: tc.payload})
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestDeployWatchWakeSkipsNonCodeTasks(t *testing.T) {
	task := deployParkedTask(domain.TaskColumnDone)
	task.TaskType = "analiz"
	analizWF := workflowtest.Default().Workflows["analiz"]
	got := deployWatchWake(analizWF, true, DispatchInput{
		Task:      task,
		EventType: domain.BoardEventTaskMoved,
		Payload:   map[string]interface{}{domain.EventPayloadResumedResource: domain.ResourceDeployWatch},
	})
	assert.False(t, got)
}
