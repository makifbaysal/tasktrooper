package board_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/board"
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

// The board view drops long-released tasks; the reconciler works off the
// same set as ListAll here, so nothing in these tests depends on the cutoff.
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

// fakePlanSettler answers the plan lookup for a dead run and records what the
// reconciler closed out.
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

// DE-1 kept a spinning subtask on the board while the pod that ran it had
// already been replaced. Failing the run row alone was not enough: the plan and
// its subtasks carry their own status, and the UI reads the plan to decide the
// run is still live.
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
	// No stale runs and no run history at all — this is the DE2-1 case:
	// dispatch never fired for this task (e.g. config gap at creation time).

	s.rec.Run(context.Background())

	s.Empty(s.runs.updated, "nothing to recover, there was never a run")
	s.Require().Len(s.runner.jobs, 1, "assigned task with zero run history must be dispatched")
	s.Equal(assignee, s.runner.jobs[0].Run.AgentID)
}

// A task parked on work_order stays in todo/in_progress with zero run
// history (Park returns before any run is ever created), so without the
// BlockedResource check this would look identical to a never-started task
// and get re-dispatched — re-triggering the park's comment — on every sweep.
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
	// A completed run exists and the task is not stale — reconciler must
	// leave it alone (already picked up once; not this sweep's job).
	s.runs.runs = []domain.TaskAgentRun{{
		ID:      uuid.New(),
		TaskID:  taskID,
		AgentID: assignee,
		Status:  domain.TaskAgentRunStatusCompleted,
	}}

	s.rec.Run(context.Background())

	s.Empty(s.runner.jobs, "task with existing run history must not be re-dispatched by the never-started path")
}

// With the self-dispatch guard in place, a failed run's task gets no follow-up
// event — the reconciler is now the only retry path, bounded at three
// consecutive failures.
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

// A success between failures resets the consecutive count: latest failed run
// after a completed one is retried.
func (s *ReconcilerSuite) TestSuccessResetsFailureCount() {
	assignee := uuid.New()
	taskID := uuid.New()
	s.tasks.tasks = []domain.BoardTask{{
		ID:              taskID,
		Column:          domain.TaskColumnInProgress,
		AssigneeAgentID: &assignee,
	}}
	// Insertion order oldest→newest; ListByTask returns newest first.
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
		// no AssigneeAgentID — out of scope: reconciler only recovers work
		// already assigned to a specific agent, never auto-assigns idle work.
	}}

	s.rec.Run(context.Background())

	s.Empty(s.runner.jobs, "unassigned tasks are not auto-dispatched by the reconciler")
}

// The reconciler is the second path into a run: it revives assigned tasks with
// no live run. Backlog tasks are not on the board, so reviving one there would
// start work nobody scheduled — the same DE-1 failure by another route.
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

// Failing a run that is still executing does not stop it; it only re-dispatches
// the task, putting a second agent on the same branch. There is no in-process
// "is this mine" oracle to consult any more — it answered for one replica's
// memory and reported every other replica's live run as abandoned — so the
// guard is the heartbeat, re-read inside the write.
//
// This is the cross-replica interleaving stated as a test: the run is LISTED as
// stale (its row was old when the sweep started) and then its owner — a
// different pod, as far as this one is concerned — heartbeats before the write
// lands. The list is advisory; the write is authoritative; the run survives.
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
	// What the sweep SAW: a row whose heartbeat had stopped an hour ago.
	s.runs.stale = []domain.TaskAgentRun{{
		ID:        runID,
		TaskID:    taskID,
		AgentID:   assignee,
		Status:    domain.TaskAgentRunStatusRunning,
		UpdatedAt: time.Now().Add(-time.Hour),
	}}
	// What the DATABASE holds by the time the write runs: the owner, on another
	// replica, touched it.
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

// The other half of the same rule: a run whose heartbeat really has stopped IS
// recovered, whichever replica notices. Without this the previous test would
// pass just as well against a reconciler that never recovers anything.
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

// A pod terminated mid-run (a deploy, an eviction, a drain that ran out of
// budget) leaves a 'running' row behind whose heartbeat has stopped. It used to
// take half an hour to recover, because the periodic sweep only looked at rows
// older than a configured 30 minutes; a startup sweep with a cutoff of NOW made
// up the difference by assuming that anything unfinished at boot was dead.
//
// That assumption is fatal with more than one replica, so it is gone — and it
// is not missed, because the configured window is now clamped to eighteen
// missed heartbeats (board.maxRunStale). The ordinary sweep recovers the dead
// run in minutes and can never touch a live one, whichever pod is running it.
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
	// Asking for half an hour of patience: ten minutes of silence would not be
	// enough, and the card would spin for another twenty.
	rec := board.NewReconciler(s.runs, s.tasks, s.disp, 30*time.Minute)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rec.Start(ctx, time.Hour)

	s.Eventually(func() bool { return len(s.runner.jobs) > 0 }, 2*time.Second, 10*time.Millisecond,
		"a run silent for ten minutes is recovered whatever the configured window says")
	s.Require().Len(s.runs.updated, 1)
	s.Equal(domain.TaskAgentRunStatusFailed, s.runs.updated[0].Status)
}

// And the boot sweep must not do what it used to: a run that another replica is
// executing right now, heartbeating normally, has to survive a fresh pod
// starting up beside it. This is the deploy case — the reason replicas: 1 with
// Recreate was the only safe setting.
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
	// Deliberately listed as stale: this models the worst case, where the
	// sweep's own read is behind. The write still refuses.
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

// The startup cutoff is what keeps the sweep from failing a run that a request
// arriving during the sweep just created: such a row is newer than the cutoff.
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

// A pending run is not a running one: nothing heartbeats it, so the 30-minute
// window that protects a long healthy run only hid an orphan. This is the row a
// control-plane handover leaves behind — created by the process that went away,
// never started by the one that took over.
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

// The other half of the same rule: a run queued seconds ago is normal, and
// failing it would race the worker that is about to start it.
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
	// Pass 2 reads the same row: a task whose latest run is queued is not a task
	// that was never dispatched.
	s.runs.runs = s.runs.stale

	s.rec.Run(context.Background())

	s.Empty(s.runs.updated, "a freshly queued run is not an orphan")
	s.Empty(s.runner.jobs, "and nothing may be dispatched beside it")
}

// Age beats ownership now, and that is the deliberate change.
//
// A pending row used to be protected by asking THIS process whether it still
// held the job in its queue. That answer is worthless on a shared deployment —
// every other replica's queued job reads as orphaned — and it is no longer
// needed: a re-dispatch produces a new pending row, and the losing worker's
// claim then fails with not_pending, so the worst case is a redundant dispatch
// rather than two agents on one branch. Trading a durable park for a two-minute
// wait is the whole point: the old in-memory park died with its pod.
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
