package board_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/board"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type controlRunStore struct {
	byID    map[uuid.UUID]domain.TaskAgentRun
	created []domain.TaskAgentRun
}

func newControlRunStore(runs ...domain.TaskAgentRun) *controlRunStore {
	s := &controlRunStore{byID: map[uuid.UUID]domain.TaskAgentRun{}}
	for _, r := range runs {
		s.byID[r.ID] = r
	}
	return s
}

func (f *controlRunStore) Create(_ context.Context, run domain.TaskAgentRun) (domain.TaskAgentRun, error) {
	run.ID = uuid.New()
	f.byID[run.ID] = run
	f.created = append(f.created, run)
	return run, nil
}

func (f *controlRunStore) Update(_ context.Context, run domain.TaskAgentRun) (domain.TaskAgentRun, error) {
	f.byID[run.ID] = run
	return run, nil
}

func (f *controlRunStore) GetByID(_ context.Context, id uuid.UUID) (domain.TaskAgentRun, error) {
	run, ok := f.byID[id]
	if !ok {
		return domain.TaskAgentRun{}, domain.ErrTaskAgentRunNotFound
	}
	return run, nil
}

func (f *controlRunStore) CancelIfLive(_ context.Context, id uuid.UUID, reason string) (domain.TaskAgentRun, bool, error) {
	run, ok := f.byID[id]
	if !ok {
		return domain.TaskAgentRun{}, false, domain.ErrTaskAgentRunNotFound
	}
	if run.Status != domain.TaskAgentRunStatusPending && run.Status != domain.TaskAgentRunStatusRunning {
		return run, false, nil
	}
	run.Status = domain.TaskAgentRunStatusCancelled
	run.Summary = reason
	f.byID[id] = run
	return run, true, nil
}

func (f *controlRunStore) ListByTask(context.Context, uuid.UUID, int) ([]domain.TaskAgentRun, error) {
	return nil, nil
}
func (f *controlRunStore) ListRecent(context.Context, int) ([]domain.TaskAgentRun, error) {
	return nil, nil
}
func (f *controlRunStore) HasPendingForEvent(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return false, nil
}
func (f *controlRunStore) HasPendingForTask(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return false, nil
}
func (f *controlRunStore) HasLiveForTask(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return false, nil
}
func (f *controlRunStore) ListStale(context.Context, time.Time) ([]domain.TaskAgentRun, error) {
	return nil, nil
}
func (f *controlRunStore) Touch(context.Context, uuid.UUID) (string, error) {
	return domain.TaskAgentRunStatusRunning, nil
}
func (f *controlRunStore) ClaimRun(context.Context, port.RunClaim) (port.RunClaimResult, error) {
	return port.RunClaimResult{Claimed: true}, nil
}
func (f *controlRunStore) FailIfStale(context.Context, uuid.UUID, time.Time, string) (bool, error) {
	return true, nil
}
func (f *controlRunStore) HasLiveRunForTask(context.Context, uuid.UUID, time.Duration) (bool, error) {
	return false, nil
}

type controlBlockCall struct {
	taskID uuid.UUID
	reason string
}

type controlTaskStore struct {
	task    domain.BoardTask
	blocked []controlBlockCall
}

func (f *controlTaskStore) Get(context.Context, uuid.UUID, uuid.UUID) (domain.BoardTask, error) {
	return f.task, nil
}

func (f *controlTaskStore) BlockOnCancel(_ context.Context, _, taskID uuid.UUID, reason string) error {
	f.blocked = append(f.blocked, controlBlockCall{taskID: taskID, reason: reason})
	return nil
}

func (f *controlTaskStore) Create(context.Context, domain.BoardTask) (domain.BoardTask, error) {
	return domain.BoardTask{}, nil
}
func (f *controlTaskStore) GetByNumber(context.Context, int) (domain.BoardTask, error) {
	return domain.BoardTask{}, nil
}
func (f *controlTaskStore) LookupByKey(context.Context, string, int) (domain.BoardTask, error) {
	return domain.BoardTask{}, nil
}
func (f *controlTaskStore) ListByRepository(context.Context, uuid.UUID) ([]domain.BoardTask, error) {
	return nil, nil
}
func (f *controlTaskStore) ListAll(context.Context) ([]domain.BoardTask, error) { return nil, nil }
func (f *controlTaskStore) ListBoardVisible(context.Context, time.Time) ([]domain.BoardTask, error) {
	return nil, nil
}
func (f *controlTaskStore) ListReleasedArchive(context.Context, string, int) ([]domain.BoardTask, error) {
	return nil, nil
}
func (f *controlTaskStore) Update(_ context.Context, task domain.BoardTask) (domain.BoardTask, error) {
	return task, nil
}
func (f *controlTaskStore) Delete(context.Context, uuid.UUID, uuid.UUID) error { return nil }
func (f *controlTaskStore) ClaimAssignee(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (domain.BoardTask, error) {
	return domain.BoardTask{}, nil
}
func (f *controlTaskStore) MarkCompleted(context.Context, uuid.UUID, bool, time.Time) error {
	return nil
}
func (f *controlTaskStore) SetMigrationFlag(context.Context, uuid.UUID, bool) error { return nil }
func (f *controlTaskStore) SetTaskPullRequest(context.Context, uuid.UUID, string, int) error {
	return nil
}
func (f *controlTaskStore) SetTaskMergeCommit(context.Context, uuid.UUID, string) error {
	return nil
}
func (f *controlTaskStore) MarkStageVerified(context.Context, uuid.UUID, time.Time) error { return nil }
func (f *controlTaskStore) ClearStageVerification(context.Context, uuid.UUID) error       { return nil }
func (f *controlTaskStore) NextTaskNumber(context.Context, domain.TaskType) (int, error) {
	return 1, nil
}
func (f *controlTaskStore) BlockOnResource(context.Context, uuid.UUID, uuid.UUID, string, string) (domain.TaskColumn, error) {
	return "", nil
}

func (f *controlTaskStore) MarkWorkOrderWaiting(context.Context, uuid.UUID, uuid.UUID, string) error {
	return nil
}

func (f *controlTaskStore) ClearWorkOrderWaiting(context.Context, uuid.UUID) (domain.BoardTask, bool, error) {
	return domain.BoardTask{}, false, nil
}

func (f *controlTaskStore) TakeBlockedByResource(context.Context, string) (domain.BoardTask, bool, error) {
	return domain.BoardTask{}, false, nil
}

func (f *controlTaskStore) TakeQuotaResumable(context.Context, time.Time) (domain.BoardTask, bool, error) {
	return domain.BoardTask{}, false, nil
}

func (f *controlTaskStore) BlockOnQuestion(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string) error {
	return nil
}
func (f *controlTaskStore) TakeBlockedBySession(context.Context, uuid.UUID) (domain.BoardTask, bool, error) {
	return domain.BoardTask{}, false, nil
}

type controlEventStore struct {
	events []domain.BoardEvent
}

func (f *controlEventStore) Create(_ context.Context, event domain.BoardEvent) (domain.BoardEvent, error) {
	event.ID = uuid.New()
	f.events = append(f.events, event)
	return event, nil
}
func (f *controlEventStore) ListRecent(context.Context, int) ([]domain.BoardEvent, error) {
	return f.events, nil
}
func (f *controlEventStore) ListByTask(context.Context, uuid.UUID, int) ([]domain.BoardEvent, error) {
	return f.events, nil
}

type controlRunner struct {
	cancelled []uuid.UUID
	jobs      []board.RunJob
}

func (f *controlRunner) Cancel(runID uuid.UUID) bool {
	f.cancelled = append(f.cancelled, runID)
	return true
}

func (f *controlRunner) Enqueue(job board.RunJob) { f.jobs = append(f.jobs, job) }

type ControlSuite struct {
	suite.Suite
	runs   *controlRunStore
	events *controlEventStore
	tasks  *controlTaskStore
	runner *controlRunner
	repoID uuid.UUID
	taskID uuid.UUID
}

func TestControlSuite(t *testing.T) {
	suite.Run(t, new(ControlSuite))
}

func (s *ControlSuite) SetupTest() {
	s.repoID = uuid.New()
	s.taskID = uuid.New()
	s.events = &controlEventStore{}
	s.runner = &controlRunner{}
	s.tasks = &controlTaskStore{task: domain.BoardTask{
		ID:           s.taskID,
		RepositoryID: s.repoID,
		Column:       domain.TaskColumnInProgress,
	}}
}

func (s *ControlSuite) controller() *board.Controller {
	return board.NewController(s.runs, s.events, s.tasks, s.runner)
}

func (s *ControlSuite) TestCancelStopsRunAndBlocksTask() {
	runID := uuid.New()
	agentID := uuid.New()
	s.runs = newControlRunStore(domain.TaskAgentRun{
		ID: runID, TaskID: s.taskID, AgentID: agentID,
		Status: domain.TaskAgentRunStatusRunning,
	})

	run, err := s.controller().CancelRun(context.Background(), s.repoID, s.taskID, runID, "changed my mind")

	s.Require().NoError(err)
	s.Equal(domain.TaskAgentRunStatusCancelled, run.Status)
	s.Require().Len(s.tasks.blocked, 1, "a cancelled run leaves nobody on the task; it must be parked")
	s.Equal("changed my mind", s.tasks.blocked[0].reason)
	s.Equal([]uuid.UUID{runID}, s.runner.cancelled)
	s.Require().Len(s.events.events, 1)
	s.Equal(domain.BoardEventTaskRunCancelled, s.events.events[0].EventType)
}

func (s *ControlSuite) TestCancelDefaultsReasonWhenEmpty() {
	runID := uuid.New()
	s.runs = newControlRunStore(domain.TaskAgentRun{
		ID: runID, TaskID: s.taskID, AgentID: uuid.New(),
		Status: domain.TaskAgentRunStatusPending,
	})

	_, err := s.controller().CancelRun(context.Background(), s.repoID, s.taskID, runID, "")

	s.Require().NoError(err)
	s.Require().Len(s.tasks.blocked, 1)
	s.NotEmpty(s.tasks.blocked[0].reason, "the blocked card renders the reason; it must never be empty")
}

func (s *ControlSuite) TestCancelRefusesFinishedRun() {
	runID := uuid.New()
	s.runs = newControlRunStore(domain.TaskAgentRun{
		ID: runID, TaskID: s.taskID, AgentID: uuid.New(),
		Status: domain.TaskAgentRunStatusCompleted,
	})

	_, err := s.controller().CancelRun(context.Background(), s.repoID, s.taskID, runID, "")

	s.Require().ErrorIs(err, domain.ErrRunNotLive)
	s.Empty(s.tasks.blocked, "losing the race must not park a task whose run finished on its own")
	s.Empty(s.events.events)
	s.Empty(s.runner.cancelled)
}

func (s *ControlSuite) TestCancelRecordsActorUserIDFromContext() {
	runID := uuid.New()
	s.runs = newControlRunStore(domain.TaskAgentRun{
		ID: runID, TaskID: s.taskID, AgentID: uuid.New(),
		Status: domain.TaskAgentRunStatusRunning,
	})
	ctx := registry.ContextWithActorUserID(context.Background(), "firebase-uid-cancel")

	_, err := s.controller().CancelRun(ctx, s.repoID, s.taskID, runID, "changed my mind")

	s.Require().NoError(err)
	s.Require().Len(s.events.events, 1)
	s.Require().NotNil(s.events.events[0].ActorUserID)
	s.Equal("firebase-uid-cancel", *s.events.events[0].ActorUserID)
}

func (s *ControlSuite) TestCancelLeavesActorUserIDNilWithoutContext() {
	runID := uuid.New()
	s.runs = newControlRunStore(domain.TaskAgentRun{
		ID: runID, TaskID: s.taskID, AgentID: uuid.New(),
		Status: domain.TaskAgentRunStatusRunning,
	})

	_, err := s.controller().CancelRun(context.Background(), s.repoID, s.taskID, runID, "changed my mind")

	s.Require().NoError(err)
	s.Require().Len(s.events.events, 1)
	s.Nil(s.events.events[0].ActorUserID)
}

func (s *ControlSuite) TestCancelRefusesRunFromAnotherTask() {
	runID := uuid.New()
	s.runs = newControlRunStore(domain.TaskAgentRun{
		ID: runID, TaskID: uuid.New(), AgentID: uuid.New(),
		Status: domain.TaskAgentRunStatusRunning,
	})

	_, err := s.controller().CancelRun(context.Background(), s.repoID, s.taskID, runID, "")

	s.Require().ErrorIs(err, domain.ErrTaskAgentRunNotFound)
	s.Empty(s.tasks.blocked)
	s.Empty(s.events.events)
}

func (s *ControlSuite) TestRerunQueuesFreshRunForSameAgent() {
	runID := uuid.New()
	agentID := uuid.New()
	s.runs = newControlRunStore(domain.TaskAgentRun{
		ID: runID, TaskID: s.taskID, AgentID: agentID,
		Status: domain.TaskAgentRunStatusFailed, Summary: "boom",
	})

	fresh, err := s.controller().RerunRun(context.Background(), s.repoID, s.taskID, runID)

	s.Require().NoError(err)
	s.Equal(agentID, fresh.AgentID, "a re-run repeats one agent, not the whole column")
	s.Equal(domain.TaskAgentRunStatusPending, fresh.Status)
	s.NotEqual(runID, fresh.ID)
	s.Require().Len(s.events.events, 1)
	s.Equal(domain.BoardEventTaskRerunRequested, s.events.events[0].EventType)
	s.Equal(s.events.events[0].ID, fresh.BoardEventID)
	s.Require().Len(s.runner.jobs, 1)
	s.Equal(fresh.ID, s.runner.jobs[0].Run.ID)
	s.Equal(s.repoID, s.runner.jobs[0].RepositoryID)

	original := s.runs.byID[runID]
	s.Equal(domain.TaskAgentRunStatusFailed, original.Status, "the old run is history and stays untouched")
	s.Equal("boom", original.Summary)
	s.Empty(s.tasks.blocked, "a re-run leaves the task where it is")
}

func (s *ControlSuite) TestRerunRecordsActorUserIDFromContext() {
	runID := uuid.New()
	s.runs = newControlRunStore(domain.TaskAgentRun{
		ID: runID, TaskID: s.taskID, AgentID: uuid.New(),
		Status: domain.TaskAgentRunStatusFailed,
	})
	ctx := registry.ContextWithActorUserID(context.Background(), "firebase-uid-rerun")

	_, err := s.controller().RerunRun(ctx, s.repoID, s.taskID, runID)

	s.Require().NoError(err)
	s.Require().Len(s.events.events, 1)
	s.Require().NotNil(s.events.events[0].ActorUserID)
	s.Equal("firebase-uid-rerun", *s.events.events[0].ActorUserID)
}

func (s *ControlSuite) TestRerunLeavesActorUserIDNilWithoutContext() {
	runID := uuid.New()
	s.runs = newControlRunStore(domain.TaskAgentRun{
		ID: runID, TaskID: s.taskID, AgentID: uuid.New(),
		Status: domain.TaskAgentRunStatusFailed,
	})

	_, err := s.controller().RerunRun(context.Background(), s.repoID, s.taskID, runID)

	s.Require().NoError(err)
	s.Require().Len(s.events.events, 1)
	s.Nil(s.events.events[0].ActorUserID)
}

func (s *ControlSuite) TestRerunRefusesLiveRun() {
	runID := uuid.New()
	s.runs = newControlRunStore(domain.TaskAgentRun{
		ID: runID, TaskID: s.taskID, AgentID: uuid.New(),
		Status: domain.TaskAgentRunStatusRunning,
	})

	_, err := s.controller().RerunRun(context.Background(), s.repoID, s.taskID, runID)

	s.Require().ErrorIs(err, domain.ErrRunNotTerminal)
	s.Empty(s.runner.jobs)
	s.Empty(s.events.events)
}

func (s *ControlSuite) TestRerunRefusesBlockedTask() {
	runID := uuid.New()
	s.runs = newControlRunStore(domain.TaskAgentRun{
		ID: runID, TaskID: s.taskID, AgentID: uuid.New(),
		Status: domain.TaskAgentRunStatusCancelled,
	})
	blockedAt := time.Now()
	s.tasks.task.Column = domain.TaskColumnBlocked
	s.tasks.task.BlockedAt = &blockedAt

	_, err := s.controller().RerunRun(context.Background(), s.repoID, s.taskID, runID)

	s.Require().ErrorIs(err, domain.ErrTaskBlockedForRerun)
	s.Empty(s.runner.jobs, "recovery for a blocked task is a human moving it, not a re-run in place")
	s.Empty(s.events.events)
	s.Empty(s.runs.created)
}

func (f *controlTaskStore) FindTaskByMergeCommit(context.Context, uuid.UUID, string) (domain.BoardTask, error) {
	return domain.BoardTask{}, errors.New("not found")
}

func (f *controlTaskStore) ListBlockedByResource(context.Context, string, int) ([]domain.BoardTask, error) {
	return nil, nil
}

func (f *controlTaskStore) TakeBlockedResourceTask(context.Context, string, uuid.UUID) (domain.BoardTask, bool, error) {
	return domain.BoardTask{}, false, nil
}
