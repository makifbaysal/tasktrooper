package board_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/board"
	"github.com/makifbaysal/tasktrooper/server/internal/application/workflow/workflowtest"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/stretchr/testify/suite"
)

type fakeBoardTaskStore struct {
	tasks []domain.BoardTask
}

func (f *fakeBoardTaskStore) Create(context.Context, domain.BoardTask) (domain.BoardTask, error) {
	return domain.BoardTask{}, nil
}
func (f *fakeBoardTaskStore) Get(context.Context, uuid.UUID, uuid.UUID) (domain.BoardTask, error) {
	return domain.BoardTask{}, nil
}
func (f *fakeBoardTaskStore) GetByNumber(context.Context, int) (domain.BoardTask, error) {
	return domain.BoardTask{}, nil
}
func (f *fakeBoardTaskStore) LookupByKey(context.Context, string, int) (domain.BoardTask, error) {
	return domain.BoardTask{}, nil
}
func (f *fakeBoardTaskStore) ListByRepository(context.Context, uuid.UUID) ([]domain.BoardTask, error) {
	return nil, nil
}
func (f *fakeBoardTaskStore) ListAll(context.Context) ([]domain.BoardTask, error) {
	return f.tasks, nil
}

func (f *fakeBoardTaskStore) ListBoardVisible(context.Context, time.Time) ([]domain.BoardTask, error) {
	return f.tasks, nil
}

func (f *fakeBoardTaskStore) ListReleasedArchive(context.Context, string, int) ([]domain.BoardTask, error) {
	return nil, nil
}
func (f *fakeBoardTaskStore) Update(_ context.Context, task domain.BoardTask) (domain.BoardTask, error) {
	return task, nil
}
func (f *fakeBoardTaskStore) Delete(context.Context, uuid.UUID, uuid.UUID) error { return nil }
func (f *fakeBoardTaskStore) ClaimAssignee(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (domain.BoardTask, error) {
	return domain.BoardTask{}, nil
}
func (f *fakeBoardTaskStore) MarkCompleted(context.Context, uuid.UUID, bool, time.Time) error {
	return nil
}
func (f *fakeBoardTaskStore) SetMigrationFlag(context.Context, uuid.UUID, bool) error { return nil }
func (f *fakeBoardTaskStore) SetTaskPullRequest(context.Context, uuid.UUID, string, int) error {
	return nil
}
func (f *fakeBoardTaskStore) SetTaskMergeCommit(context.Context, uuid.UUID, string) error {
	return nil
}
func (f *fakeBoardTaskStore) ClearStageVerification(context.Context, uuid.UUID) error { return nil }
func (f *fakeBoardTaskStore) MarkStageVerified(context.Context, uuid.UUID, time.Time) error {
	return nil
}
func (f *fakeBoardTaskStore) NextTaskNumber(context.Context, domain.TaskType) (int, error) {
	return 1, nil
}
func (f *fakeBoardTaskStore) BlockOnQuestion(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string) error {
	return nil
}
func (f *fakeBoardTaskStore) TakeBlockedBySession(context.Context, uuid.UUID) (domain.BoardTask, bool, error) {
	return domain.BoardTask{}, false, nil
}
func (f *fakeBoardTaskStore) BlockOnResource(context.Context, uuid.UUID, uuid.UUID, string, string) (domain.TaskColumn, error) {
	return "", nil
}

func (f *fakeBoardTaskStore) MarkWorkOrderWaiting(context.Context, uuid.UUID, uuid.UUID, string) error {
	return nil
}

func (f *fakeBoardTaskStore) ClearWorkOrderWaiting(context.Context, uuid.UUID) (domain.BoardTask, bool, error) {
	return domain.BoardTask{}, false, nil
}

func (f *fakeBoardTaskStore) TakeBlockedByResource(context.Context, string) (domain.BoardTask, bool, error) {
	return domain.BoardTask{}, false, nil
}

func (f *fakeBoardTaskStore) TakeQuotaResumable(context.Context, time.Time) (domain.BoardTask, bool, error) {
	return domain.BoardTask{}, false, nil
}

func (f *fakeBoardTaskStore) BlockOnCancel(context.Context, uuid.UUID, uuid.UUID, string) error {
	return nil
}

func (f *fakeBoardTaskStore) ConfirmBeforeDeploy(context.Context, uuid.UUID, uuid.UUID) (domain.BoardTask, error) {
	return domain.BoardTask{}, nil
}

type ReconcilerSuite struct {
	suite.Suite
	board  *fakeBoardConfigStore
	events *fakeEventStore
	runs   *fakeRunStore
	runner *fakeRunner
	tasks  *fakeBoardTaskStore
	disp   *board.Dispatcher
	rec    *board.Reconciler
}

func TestReconcilerSuite(t *testing.T) {
	suite.Run(t, new(ReconcilerSuite))
}

func (s *ReconcilerSuite) SetupTest() {
	s.board = &fakeBoardConfigStore{agentsByColumn: map[string][]uuid.UUID{}}
	s.events = &fakeEventStore{}
	s.runs = &fakeRunStore{}
	s.runner = &fakeRunner{}
	s.tasks = &fakeBoardTaskStore{}
	s.disp = board.NewDispatcher(s.board, s.events, s.runs, s.runner, true)
	s.disp.SetWorkflows(workflowtest.Default().Reader())
	s.rec = board.NewReconciler(s.runs, s.tasks, s.disp, 0)
}

func (s *ReconcilerSuite) TestRecoversStaleRunAndRedispatchesAssignee() {
	assignee := uuid.New()
	taskID := uuid.New()
	repositoryID := uuid.New()
	s.tasks.tasks = []domain.BoardTask{{
		ID:              taskID,
		RepositoryID:    repositoryID,
		Column:          domain.TaskColumnInProgress,
		AssigneeAgentID: &assignee,
	}}
	s.runs.stale = []domain.TaskAgentRun{{
		ID:      uuid.New(),
		TaskID:  taskID,
		AgentID: assignee,
		Status:  domain.TaskAgentRunStatusRunning,
	}}

	s.rec.Run(context.Background())

	s.Require().Len(s.runs.updated, 1, "stale run must be marked done (failed) so it stops matching ListStale next sweep")
	s.Equal(domain.TaskAgentRunStatusFailed, s.runs.updated[0].Status)
	s.Require().Len(s.runner.jobs, 1, "task must be re-dispatched to its assignee")
	s.Equal(assignee, s.runner.jobs[0].Run.AgentID)
}

type fakePlanSettler struct {
	plan         domain.PlanView
	planStatus   string
	taskStatuses map[uuid.UUID]string
}

func (f *fakePlanSettler) GetPlanByRunID(context.Context, uuid.UUID) (domain.PlanView, error) {
	return f.plan, nil
}

func (f *fakePlanSettler) UpdatePlanStatus(_ context.Context, _ uuid.UUID, status string) error {
	f.planStatus = status
	return nil
}

func (f *fakePlanSettler) UpdateTaskStatus(_ context.Context, taskID uuid.UUID, status, _, _ string) error {
	if f.taskStatuses == nil {
		f.taskStatuses = map[uuid.UUID]string{}
	}
	f.taskStatuses[taskID] = status
	return nil
}

func (s *ReconcilerSuite) TestRecoveredRunAlsoSettlesItsOrchestrationPlan() {
	assignee := uuid.New()
	taskID := uuid.New()
	sessionRunID := uuid.New()
	startedSubtask := uuid.New()
	neverStartedSubtask := uuid.New()

	settler := &fakePlanSettler{plan: domain.PlanView{
		ID:     uuid.New(),
		RunID:  sessionRunID,
		Status: domain.PlanStatusRunning,
		Tasks: []domain.PlanTask{
			{ID: startedSubtask, TaskKey: "t2", Status: domain.TaskStatusRunning},
			{ID: neverStartedSubtask, TaskKey: "t3", Status: domain.TaskStatusPending},
		},
	}}
	s.rec.SetPlanSettler(settler)

	s.tasks.tasks = []domain.BoardTask{{
		ID:              taskID,
		Column:          domain.TaskColumnInProgress,
		AssigneeAgentID: &assignee,
	}}
	s.runs.stale = []domain.TaskAgentRun{{
		ID:           uuid.New(),
		TaskID:       taskID,
		AgentID:      assignee,
		SessionRunID: &sessionRunID,
		Status:       domain.TaskAgentRunStatusRunning,
	}}

	s.rec.Run(context.Background())

	s.Equal(domain.PlanStatusFailed, settler.planStatus, "a plan whose process is gone must stop claiming to run")
	s.Equal(domain.TaskStatusFailed, settler.taskStatuses[startedSubtask])
	s.NotContains(settler.taskStatuses, neverStartedSubtask, "a subtask that never started stays pending")
}

func (s *ReconcilerSuite) TestDoneTaskNotRedispatched() {
	assignee := uuid.New()
	taskID := uuid.New()
	s.tasks.tasks = []domain.BoardTask{{
		ID:              taskID,
		Column:          domain.TaskColumnDone,
		AssigneeAgentID: &assignee,
	}}
	s.runs.stale = []domain.TaskAgentRun{{
		ID:      uuid.New(),
		TaskID:  taskID,
		AgentID: assignee,
		Status:  domain.TaskAgentRunStatusPending,
	}}

	s.rec.Run(context.Background())

	s.Require().Len(s.runs.updated, 1)
	s.Empty(s.runner.jobs, "a task already done must not be re-dispatched")
}

func (s *ReconcilerSuite) TestNoStaleRunsIsNoop() {
	s.rec.Run(context.Background())
	s.Empty(s.runs.updated)
	s.Empty(s.runner.jobs)
}

func (s *ReconcilerSuite) TestNeverStartedAssignedTaskIsDispatched() {
	assignee := uuid.New()
	taskID := uuid.New()
	repositoryID := uuid.New()
	s.tasks.tasks = []domain.BoardTask{{
		ID:              taskID,
		RepositoryID:    repositoryID,
		Column:          domain.TaskColumnTodo,
		AssigneeAgentID: &assignee,
	}}

	s.rec.Run(context.Background())

	s.Empty(s.runs.updated, "nothing to recover, there was never a run")
	s.Require().Len(s.runner.jobs, 1, "assigned task with zero run history must be dispatched")
	s.Equal(assignee, s.runner.jobs[0].Run.AgentID)
}

func (s *ReconcilerSuite) TestWorkOrderParkedTaskNotRedispatched() {
	assignee := uuid.New()
	taskID := uuid.New()
	s.tasks.tasks = []domain.BoardTask{{
		ID:              taskID,
		Column:          domain.TaskColumnTodo,
		AssigneeAgentID: &assignee,
		BlockedResource: domain.ResourceWorkOrder,
	}}

	s.rec.Run(context.Background())

	s.Empty(s.runner.jobs, "a task parked on work_order is resumed by the sweeper, not the reconciler")
}

func (s *ReconcilerSuite) TestTaskWithRunHistoryNotDoubleDispatched() {
	assignee := uuid.New()
	taskID := uuid.New()
	s.tasks.tasks = []domain.BoardTask{{
		ID:              taskID,
		Column:          domain.TaskColumnInProgress,
		AssigneeAgentID: &assignee,
	}}
	s.runs.runs = []domain.TaskAgentRun{{
		ID:      uuid.New(),
		TaskID:  taskID,
		AgentID: assignee,
		Status:  domain.TaskAgentRunStatusCompleted,
	}}

	s.rec.Run(context.Background())

	s.Empty(s.runner.jobs, "task with existing run history must not be re-dispatched by the never-started path")
}

func (s *ReconcilerSuite) TestFailedRunIsRetried() {
	assignee := uuid.New()
	taskID := uuid.New()
	repositoryID := uuid.New()
	s.tasks.tasks = []domain.BoardTask{{
		ID:              taskID,
		RepositoryID:    repositoryID,
		Column:          domain.TaskColumnInProgress,
		AssigneeAgentID: &assignee,
	}}
	s.runs.runs = []domain.TaskAgentRun{{
		ID:      uuid.New(),
		TaskID:  taskID,
		AgentID: assignee,
		Status:  domain.TaskAgentRunStatusFailed,
	}}

	s.rec.Run(context.Background())

	s.Require().Len(s.runner.jobs, 1, "failed run must be retried")
	s.Equal(assignee, s.runner.jobs[0].Run.AgentID)
}

func (s *ReconcilerSuite) TestThreeConsecutiveFailuresParkTask() {
	assignee := uuid.New()
	taskID := uuid.New()
	s.tasks.tasks = []domain.BoardTask{{
		ID:              taskID,
		Column:          domain.TaskColumnInProgress,
		AssigneeAgentID: &assignee,
	}}
	for range 3 {
		s.runs.runs = append(s.runs.runs, domain.TaskAgentRun{
			ID:      uuid.New(),
			TaskID:  taskID,
			AgentID: assignee,
			Status:  domain.TaskAgentRunStatusFailed,
		})
	}

	s.rec.Run(context.Background())

	s.Empty(s.runner.jobs, "a task failing three times in a row waits for a human")
}

func (s *ReconcilerSuite) TestSuccessResetsFailureCount() {
	assignee := uuid.New()
	taskID := uuid.New()
	s.tasks.tasks = []domain.BoardTask{{
		ID:              taskID,
		Column:          domain.TaskColumnInProgress,
		AssigneeAgentID: &assignee,
	}}
	s.runs.runs = []domain.TaskAgentRun{
		{ID: uuid.New(), TaskID: taskID, AgentID: assignee, Status: domain.TaskAgentRunStatusFailed},
		{ID: uuid.New(), TaskID: taskID, AgentID: assignee, Status: domain.TaskAgentRunStatusFailed},
		{ID: uuid.New(), TaskID: taskID, AgentID: assignee, Status: domain.TaskAgentRunStatusCompleted},
		{ID: uuid.New(), TaskID: taskID, AgentID: assignee, Status: domain.TaskAgentRunStatusFailed},
	}

	s.rec.Run(context.Background())

	s.Require().Len(s.runner.jobs, 1, "one failure after a success is retried")
}

func (s *ReconcilerSuite) TestBlockedTaskNotRetried() {
	assignee := uuid.New()
	taskID := uuid.New()
	s.tasks.tasks = []domain.BoardTask{{
		ID:              taskID,
		Column:          domain.TaskColumnBlocked,
		AssigneeAgentID: &assignee,
	}}
	s.runs.runs = []domain.TaskAgentRun{{
		ID:      uuid.New(),
		TaskID:  taskID,
		AgentID: assignee,
		Status:  domain.TaskAgentRunStatusFailed,
	}}

	s.rec.Run(context.Background())

	s.Empty(s.runner.jobs, "blocked tasks wait on the human answer, not a retry")
}

func (s *ReconcilerSuite) TestUnassignedTaskNotDispatched() {
	taskID := uuid.New()
	s.tasks.tasks = []domain.BoardTask{{
		ID:     taskID,
		Column: domain.TaskColumnTodo,
	}}

	s.rec.Run(context.Background())

	s.Empty(s.runner.jobs, "unassigned tasks are not auto-dispatched by the reconciler")
}

func (s *ReconcilerSuite) TestBacklogTaskNotDispatched() {
	assignee := uuid.New()
	s.tasks.tasks = []domain.BoardTask{{
		ID:              uuid.New(),
		RepositoryID:    uuid.New(),
		Column:          domain.TaskColumnBacklog,
		AssigneeAgentID: &assignee,
	}}

	s.rec.Run(context.Background())

	s.Empty(s.runner.jobs, "a backlog task must not be dispatched by the reconciler")
}

func (s *ReconcilerSuite) TestRunThatHeartbeatsDuringTheSweepIsNotRecovered() {
	assignee := uuid.New()
	taskID := uuid.New()
	runID := uuid.New()
	s.tasks.tasks = []domain.BoardTask{{
		ID:              taskID,
		RepositoryID:    uuid.New(),
		Column:          domain.TaskColumnInProgress,
		AssigneeAgentID: &assignee,
	}}
	s.runs.stale = []domain.TaskAgentRun{{
		ID:        runID,
		TaskID:    taskID,
		AgentID:   assignee,
		Status:    domain.TaskAgentRunStatusRunning,
		UpdatedAt: time.Now().Add(-time.Hour),
	}}
	s.runs.runs = []domain.TaskAgentRun{{
		ID:        runID,
		TaskID:    taskID,
		AgentID:   assignee,
		Status:    domain.TaskAgentRunStatusRunning,
		UpdatedAt: time.Now(),
	}}

	s.rec.Run(context.Background())

	s.Empty(s.runs.updated, "a run whose owner heartbeated mid-sweep must not be marked failed")
	s.Empty(s.runner.jobs, "and must not be re-dispatched behind its own back")
}

func (s *ReconcilerSuite) TestRunWithNoHeartbeatIsRecovered() {
	assignee := uuid.New()
	taskID := uuid.New()
	runID := uuid.New()
	s.tasks.tasks = []domain.BoardTask{{
		ID:              taskID,
		RepositoryID:    uuid.New(),
		Column:          domain.TaskColumnInProgress,
		AssigneeAgentID: &assignee,
	}}
	dead := domain.TaskAgentRun{
		ID:        runID,
		TaskID:    taskID,
		AgentID:   assignee,
		Status:    domain.TaskAgentRunStatusRunning,
		UpdatedAt: time.Now().Add(-time.Hour),
	}
	s.runs.stale = []domain.TaskAgentRun{dead}
	s.runs.runs = []domain.TaskAgentRun{dead}

	s.rec.Run(context.Background())

	s.Len(s.runs.updated, 1, "a run whose owner stopped heartbeating is abandoned")
	s.Equal(domain.TaskAgentRunStatusFailed, s.runs.updated[0].Status)
	s.Len(s.runner.jobs, 1, "and its task is re-dispatched")
}

func (s *ReconcilerSuite) TestStaleWindowIsClampedToTheHeartbeat() {
	assignee := uuid.New()
	taskID := uuid.New()
	s.tasks.tasks = []domain.BoardTask{{
		ID:              taskID,
		RepositoryID:    uuid.New(),
		Column:          domain.TaskColumnInProgress,
		AssigneeAgentID: &assignee,
	}}
	dead := domain.TaskAgentRun{
		ID:        uuid.New(),
		TaskID:    taskID,
		AgentID:   assignee,
		Status:    domain.TaskAgentRunStatusRunning,
		UpdatedAt: time.Now().Add(-10 * time.Minute),
	}
	s.runs.stale = []domain.TaskAgentRun{dead}
	s.runs.runs = []domain.TaskAgentRun{dead}
	rec := board.NewReconciler(s.runs, s.tasks, s.disp, 30*time.Minute)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rec.Start(ctx, time.Hour)

	s.Eventually(func() bool { return len(s.runner.jobs) > 0 }, 2*time.Second, 10*time.Millisecond,
		"a run silent for ten minutes is recovered whatever the configured window says")
	s.Require().Len(s.runs.updated, 1)
	s.Equal(domain.TaskAgentRunStatusFailed, s.runs.updated[0].Status)
}

func (s *ReconcilerSuite) TestBootSweepLeavesAnotherReplicasLiveRunAlone() {
	assignee := uuid.New()
	taskID := uuid.New()
	s.tasks.tasks = []domain.BoardTask{{
		ID:              taskID,
		RepositoryID:    uuid.New(),
		Column:          domain.TaskColumnInProgress,
		AssigneeAgentID: &assignee,
	}}
	live := domain.TaskAgentRun{
		ID:        uuid.New(),
		TaskID:    taskID,
		AgentID:   assignee,
		Status:    domain.TaskAgentRunStatusRunning,
		UpdatedAt: time.Now(),
	}
	s.runs.stale = []domain.TaskAgentRun{live}
	s.runs.runs = []domain.TaskAgentRun{live}
	rec := board.NewReconciler(s.runs, s.tasks, s.disp, 0)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rec.Start(ctx, time.Hour)

	s.Never(func() bool { return len(s.runs.updated) > 0 }, 300*time.Millisecond, 20*time.Millisecond,
		"a booting replica must not fail a run its sibling is executing")
	s.Empty(s.runner.jobs, "and must not put a second agent on the same branch")
}

func (s *ReconcilerSuite) TestStartupSweepLeavesRunsCreatedAfterBootAlone() {
	assignee := uuid.New()
	taskID := uuid.New()
	s.tasks.tasks = []domain.BoardTask{{
		ID:              taskID,
		RepositoryID:    uuid.New(),
		Column:          domain.TaskColumnInProgress,
		AssigneeAgentID: &assignee,
	}}
	justStarted := domain.TaskAgentRun{
		ID:        uuid.New(),
		TaskID:    taskID,
		AgentID:   assignee,
		Status:    domain.TaskAgentRunStatusRunning,
		UpdatedAt: time.Now().Add(time.Minute),
	}
	s.runs.stale = []domain.TaskAgentRun{justStarted}
	s.runs.runs = []domain.TaskAgentRun{justStarted}
	rec := board.NewReconciler(s.runs, s.tasks, s.disp, 30*time.Minute)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rec.Start(ctx, time.Hour)

	s.Eventually(func() bool { return len(s.runs.staleCutoffs) > 0 }, 2*time.Second, 10*time.Millisecond,
		"startup sweep never ran")
	s.Empty(s.runs.updated, "a run started after boot must not be failed by the startup sweep")
	s.Empty(s.runner.jobs, "and must not be re-dispatched behind its own back")
}

func (f *fakeBoardTaskStore) FindTaskByMergeCommit(context.Context, uuid.UUID, string) (domain.BoardTask, error) {
	return domain.BoardTask{}, errors.New("not found")
}

func (f *fakeBoardTaskStore) ListBlockedByResource(context.Context, string, int) ([]domain.BoardTask, error) {
	return nil, nil
}

func (f *fakeBoardTaskStore) TakeBlockedResourceTask(context.Context, string, uuid.UUID) (domain.BoardTask, bool, error) {
	return domain.BoardTask{}, false, nil
}

func (s *ReconcilerSuite) TestOrphanedPendingRunIsRecoveredWellBeforeTheStaleWindow() {
	assignee := uuid.New()
	taskID := uuid.New()
	s.tasks.tasks = []domain.BoardTask{{
		ID:              taskID,
		RepositoryID:    uuid.New(),
		Column:          domain.TaskColumnInProgress,
		AssigneeAgentID: &assignee,
	}}
	s.runs.stale = []domain.TaskAgentRun{{
		ID:        uuid.New(),
		TaskID:    taskID,
		AgentID:   assignee,
		Status:    domain.TaskAgentRunStatusPending,
		UpdatedAt: time.Now().Add(-3 * time.Minute),
	}}

	s.rec.Run(context.Background())

	s.Require().Len(s.runs.updated, 1, "a pending run nobody owns must not wait out the running window")
	s.Equal(domain.TaskAgentRunStatusFailed, s.runs.updated[0].Status)
	s.Require().Len(s.runner.jobs, 1, "and the task must be re-dispatched")
}

func (s *ReconcilerSuite) TestFreshPendingRunIsLeftAlone() {
	assignee := uuid.New()
	taskID := uuid.New()
	s.tasks.tasks = []domain.BoardTask{{
		ID:              taskID,
		RepositoryID:    uuid.New(),
		Column:          domain.TaskColumnInProgress,
		AssigneeAgentID: &assignee,
	}}
	s.runs.stale = []domain.TaskAgentRun{{
		ID:        uuid.New(),
		TaskID:    taskID,
		AgentID:   assignee,
		Status:    domain.TaskAgentRunStatusPending,
		UpdatedAt: time.Now().Add(-20 * time.Second),
	}}
	s.runs.runs = s.runs.stale

	s.rec.Run(context.Background())

	s.Empty(s.runs.updated, "a freshly queued run is not an orphan")
	s.Empty(s.runner.jobs, "and nothing may be dispatched beside it")
}

func (s *ReconcilerSuite) TestLongQueuedPendingRunIsRecovered() {
	assignee := uuid.New()
	taskID := uuid.New()
	runID := uuid.New()
	s.tasks.tasks = []domain.BoardTask{{
		ID:              taskID,
		RepositoryID:    uuid.New(),
		Column:          domain.TaskColumnInProgress,
		AssigneeAgentID: &assignee,
	}}
	s.runs.stale = []domain.TaskAgentRun{{
		ID:        runID,
		TaskID:    taskID,
		AgentID:   assignee,
		Status:    domain.TaskAgentRunStatusPending,
		UpdatedAt: time.Now().Add(-45 * time.Minute),
	}}
	s.runs.runs = s.runs.stale

	s.rec.Run(context.Background())

	s.Len(s.runs.updated, 1, "a pending row nobody has claimed in 45 minutes is orphaned")
	s.Len(s.runner.jobs, 1, "and its task is dispatched again")
}
