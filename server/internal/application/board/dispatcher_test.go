package board_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/board"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/application/workflow/workflowtest"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/stretchr/testify/suite"
)

type fakeBoardConfigStore struct {
	agentsByColumn map[string][]uuid.UUID
	instructions   map[string]string
}

func (f *fakeBoardConfigStore) GetSettings(context.Context) (domain.BoardSettings, error) {
	return domain.BoardSettings{KeyPrefix: "DT"}, nil
}
func (f *fakeBoardConfigStore) UpdateSettings(context.Context, string) (domain.BoardSettings, error) {
	return domain.BoardSettings{KeyPrefix: "DT"}, nil
}
func (f *fakeBoardConfigStore) ListColumns(context.Context) ([]domain.BoardColumn, error) {
	return nil, nil
}
func (f *fakeBoardConfigStore) ReplaceColumns(context.Context, []domain.BoardColumnInput) error {
	return nil
}
func (f *fakeBoardConfigStore) ListMembers(context.Context) ([]domain.BoardMember, error) {
	return nil, nil
}
func (f *fakeBoardConfigStore) SetMembers(context.Context, []uuid.UUID) error { return nil }
func (f *fakeBoardConfigStore) ListSubscriptions(context.Context) ([]domain.BoardSubscription, error) {
	return nil, nil
}
func (f *fakeBoardConfigStore) SetSubscriptions(context.Context, []domain.BoardSubscriptionInput) error {
	return nil
}
func (f *fakeBoardConfigStore) ListAgentSubscriptions(context.Context, uuid.UUID) ([]string, error) {
	return nil, nil
}
func (f *fakeBoardConfigStore) SetAgentSubscriptions(context.Context, uuid.UUID, []string) error {
	return nil
}
func (f *fakeBoardConfigStore) ListAgentSubscriptionsDetailed(context.Context, uuid.UUID) ([]domain.AgentColumnSubscription, error) {
	return nil, nil
}
func (f *fakeBoardConfigStore) SetAgentSubscriptionsDetailed(context.Context, uuid.UUID, []domain.AgentColumnSubscription) error {
	return nil
}
func (f *fakeBoardConfigStore) ListAgentColumnInstructions(context.Context, uuid.UUID) ([]domain.AgentColumnInstruction, error) {
	var out []domain.AgentColumnInstruction
	for slug, text := range f.instructions {
		out = append(out, domain.AgentColumnInstruction{ColumnSlug: slug, Instruction: text})
	}
	return out, nil
}
func (f *fakeBoardConfigStore) SetAgentColumnInstruction(context.Context, uuid.UUID, string, string) error {
	return nil
}
func (f *fakeBoardConfigStore) ListTransitions(context.Context) ([]domain.BoardTransition, error) {
	return nil, nil
}
func (f *fakeBoardConfigStore) SetTransitions(context.Context, []domain.BoardTransition) error {
	return nil
}
func (f *fakeBoardConfigStore) AgentsForColumn(_ context.Context, columnSlug string, _ string) ([]uuid.UUID, error) {
	return f.agentsByColumn[columnSlug], nil
}
func (f *fakeBoardConfigStore) ValidateColumnSlug(context.Context, string) (bool, error) {
	return true, nil
}

type fakeEventStore struct {
	events []domain.BoardEvent
}

func (f *fakeEventStore) Create(_ context.Context, event domain.BoardEvent) (domain.BoardEvent, error) {
	event.ID = uuid.New()
	f.events = append(f.events, event)
	return event, nil
}

func (f *fakeEventStore) ListRecent(context.Context, int) ([]domain.BoardEvent, error) {
	return f.events, nil
}

func (f *fakeEventStore) ListByTask(_ context.Context, taskID uuid.UUID, limit int) ([]domain.BoardEvent, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	var out []domain.BoardEvent
	for _, e := range f.events {
		if e.TaskID != taskID {
			continue
		}
		out = append(out, e)
		if len(out) == limit {
			break
		}
	}
	return out, nil
}

type fakeRunStore struct {
	runs         []domain.TaskAgentRun
	stale        []domain.TaskAgentRun
	updated      []domain.TaskAgentRun
	touched      []uuid.UUID
	staleCutoffs []time.Time
	beforeCreate func()
}

func (f *fakeRunStore) Create(_ context.Context, run domain.TaskAgentRun) (domain.TaskAgentRun, error) {
	if f.beforeCreate != nil {
		f.beforeCreate()
	}
	for _, existing := range f.runs {
		if existing.TaskID == run.TaskID && existing.AgentID == run.AgentID &&
			existing.Status == domain.TaskAgentRunStatusPending {
			return existing, nil
		}
	}
	run.ID = uuid.New()
	f.runs = append(f.runs, run)
	return run, nil
}

func (f *fakeRunStore) Update(_ context.Context, run domain.TaskAgentRun) (domain.TaskAgentRun, error) {
	f.updated = append(f.updated, run)
	return run, nil
}

func (f *fakeRunStore) ListByTask(_ context.Context, taskID uuid.UUID, _ int) ([]domain.TaskAgentRun, error) {
	var out []domain.TaskAgentRun
	for i := len(f.runs) - 1; i >= 0; i-- {
		if f.runs[i].TaskID == taskID {
			out = append(out, f.runs[i])
		}
	}
	return out, nil
}

func (f *fakeRunStore) ListRecent(context.Context, int) ([]domain.TaskAgentRun, error) {
	return f.runs, nil
}

func (f *fakeRunStore) HasPendingForEvent(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return false, nil
}

func (f *fakeRunStore) HasPendingForTask(_ context.Context, taskID, agentID uuid.UUID) (bool, error) {
	for _, r := range f.runs {
		if r.TaskID == taskID && r.AgentID == agentID && r.Status == domain.TaskAgentRunStatusPending {
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeRunStore) HasLiveForTask(_ context.Context, taskID, agentID uuid.UUID) (bool, error) {
	for _, r := range f.runs {
		if r.TaskID != taskID || r.AgentID != agentID {
			continue
		}
		if r.Status == domain.TaskAgentRunStatusPending || r.Status == domain.TaskAgentRunStatusRunning {
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeRunStore) ListStale(_ context.Context, cutoff time.Time) ([]domain.TaskAgentRun, error) {
	f.staleCutoffs = append(f.staleCutoffs, cutoff)
	var out []domain.TaskAgentRun
	for _, run := range f.stale {
		if run.UpdatedAt.IsZero() || run.UpdatedAt.Before(cutoff) {
			out = append(out, run)
		}
	}
	return out, nil
}

func (f *fakeRunStore) Touch(_ context.Context, id uuid.UUID) (string, error) {
	f.touched = append(f.touched, id)
	return domain.TaskAgentRunStatusRunning, nil
}

func (f *fakeRunStore) ClaimRun(context.Context, port.RunClaim) (port.RunClaimResult, error) {
	return port.RunClaimResult{Claimed: true}, nil
}

func (f *fakeRunStore) FailIfStale(_ context.Context, id uuid.UUID, cutoff time.Time, summary string) (bool, error) {
	rows := f.runs
	if !containsRun(rows, id) {
		rows = f.stale
	}
	for i := range rows {
		if rows[i].ID != id {
			continue
		}
		f.runs = rows
		if rows[i].Status != domain.TaskAgentRunStatusPending && rows[i].Status != domain.TaskAgentRunStatusRunning {
			return false, nil
		}
		if !rows[i].UpdatedAt.Before(cutoff) {
			return false, nil
		}
		rows[i].Status = domain.TaskAgentRunStatusFailed
		rows[i].Summary = summary
		f.updated = append(f.updated, rows[i])
		return true, nil
	}
	return false, nil
}

func containsRun(runs []domain.TaskAgentRun, id uuid.UUID) bool {
	for _, r := range runs {
		if r.ID == id {
			return true
		}
	}
	return false
}

func (f *fakeRunStore) HasLiveRunForTask(context.Context, uuid.UUID, time.Duration) (bool, error) {
	return false, nil
}

func (f *fakeRunStore) GetByID(_ context.Context, id uuid.UUID) (domain.TaskAgentRun, error) {
	for _, run := range f.runs {
		if run.ID == id {
			return run, nil
		}
	}
	return domain.TaskAgentRun{}, domain.ErrTaskAgentRunNotFound
}

func (f *fakeRunStore) CancelIfLive(_ context.Context, id uuid.UUID, reason string) (domain.TaskAgentRun, bool, error) {
	for i, run := range f.runs {
		if run.ID != id {
			continue
		}
		if domain.TaskAgentRunIsTerminal(run.Status) {
			return run, false, nil
		}
		f.runs[i].Status = domain.TaskAgentRunStatusCancelled
		f.runs[i].Summary = reason
		return f.runs[i], true, nil
	}
	return domain.TaskAgentRun{}, false, domain.ErrTaskAgentRunNotFound
}

type fakeRunner struct {
	jobs []board.RunJob
}

func (f *fakeRunner) Enqueue(job board.RunJob) {
	f.jobs = append(f.jobs, job)
}

type DispatcherSuite struct {
	suite.Suite
	board  *fakeBoardConfigStore
	events *fakeEventStore
	runs   *fakeRunStore
	runner *fakeRunner
	disp   *board.Dispatcher
}

func TestDispatcherSuite(t *testing.T) {
	suite.Run(t, new(DispatcherSuite))
}

func (s *DispatcherSuite) SetupTest() {
	agentA := uuid.New()
	s.board = &fakeBoardConfigStore{
		agentsByColumn: map[string][]uuid.UUID{
			"todo": {agentA},
		},
	}
	s.events = &fakeEventStore{}
	s.runs = &fakeRunStore{}
	s.runner = &fakeRunner{}
	s.disp = board.NewDispatcher(s.board, s.events, s.runs, s.runner, true)
	s.disp.SetWorkflows(workflowtest.Default().Reader())
}

func (s *DispatcherSuite) TestDispatchTodoColumn() {
	taskID := uuid.New()
	repositoryID := uuid.New()
	err := s.disp.Dispatch(context.Background(), board.DispatchInput{
		RepositoryID: repositoryID,
		Task: domain.BoardTask{
			ID:           taskID,
			RepositoryID: repositoryID,
			Title:        "t",
			Column:       domain.TaskColumnTodo,
		},
		EventType: domain.BoardEventTaskCreated,
	})
	s.Require().NoError(err)
	s.Len(s.events.events, 1)
	s.Len(s.runs.runs, 1)
	s.Len(s.runner.jobs, 1)
}

func (s *DispatcherSuite) TestActorAgentNotRedispatchedOnOwnMove() {
	assignee := uuid.New()
	repositoryID := uuid.New()

	err := s.disp.Dispatch(context.Background(), board.DispatchInput{
		RepositoryID: repositoryID,
		Task: domain.BoardTask{
			ID:              uuid.New(),
			RepositoryID:    repositoryID,
			Title:           "t",
			Column:          domain.TaskColumnInProgress,
			AssigneeAgentID: &assignee,
		},
		EventType: domain.BoardEventTaskMoved,
		Payload:   map[string]interface{}{"actor_agent_id": assignee.String()},
	})

	s.Require().NoError(err)
	s.Len(s.events.events, 1, "event history stays complete")
	s.Empty(s.runs.runs, "the acting agent keeps working in its existing run")
	s.Empty(s.runner.jobs)
}

func (s *DispatcherSuite) TestDispatchStampsActorUserIDFromContext() {
	repositoryID := uuid.New()
	ctx := registry.ContextWithActorUserID(context.Background(), "firebase-uid-77")

	err := s.disp.Dispatch(ctx, board.DispatchInput{
		RepositoryID: repositoryID,
		Task: domain.BoardTask{
			ID:           uuid.New(),
			RepositoryID: repositoryID,
			Title:        "t",
			Column:       domain.TaskColumnTodo,
		},
		EventType: domain.BoardEventTaskMoved,
	})

	s.Require().NoError(err)
	s.Require().Len(s.events.events, 1)
	s.Require().NotNil(s.events.events[0].ActorUserID)
	s.Equal("firebase-uid-77", *s.events.events[0].ActorUserID)
}

func (s *DispatcherSuite) TestDispatchLeavesActorUserIDNilWithoutContext() {
	repositoryID := uuid.New()

	err := s.disp.Dispatch(context.Background(), board.DispatchInput{
		RepositoryID: repositoryID,
		Task: domain.BoardTask{
			ID:           uuid.New(),
			RepositoryID: repositoryID,
			Title:        "t",
			Column:       domain.TaskColumnTodo,
		},
		EventType: domain.BoardEventTaskMoved,
	})

	s.Require().NoError(err)
	s.Require().Len(s.events.events, 1)
	s.Nil(s.events.events[0].ActorUserID)
}

func (s *DispatcherSuite) TestOtherActorStillDispatchesAssignee() {
	assignee := uuid.New()
	reviewer := uuid.New()
	repositoryID := uuid.New()

	err := s.disp.Dispatch(context.Background(), board.DispatchInput{
		RepositoryID: repositoryID,
		Task: domain.BoardTask{
			ID:              uuid.New(),
			RepositoryID:    repositoryID,
			Title:           "t",
			Column:          domain.TaskColumnNeedRevision,
			AssigneeAgentID: &assignee,
		},
		EventType: domain.BoardEventTaskMoved,
		Payload:   map[string]interface{}{"actor_agent_id": reviewer.String()},
	})

	s.Require().NoError(err)
	s.Require().Len(s.runner.jobs, 1)
	s.Equal(assignee, s.runner.jobs[0].Run.AgentID)
}

func (s *DispatcherSuite) TestPendingRunOnTaskDedupes() {
	assignee := uuid.New()
	taskID := uuid.New()
	repositoryID := uuid.New()
	s.runs.runs = []domain.TaskAgentRun{{
		ID:      uuid.New(),
		TaskID:  taskID,
		AgentID: assignee,
		Status:  domain.TaskAgentRunStatusPending,
	}}

	err := s.disp.Dispatch(context.Background(), board.DispatchInput{
		RepositoryID: repositoryID,
		Task: domain.BoardTask{
			ID:              taskID,
			RepositoryID:    repositoryID,
			Title:           "t",
			Column:          domain.TaskColumnInProgress,
			AssigneeAgentID: &assignee,
		},
		EventType: domain.BoardEventTaskMoved,
	})

	s.Require().NoError(err)
	s.Len(s.runs.runs, 1, "no second pending run for the same task+agent")
	s.Empty(s.runner.jobs)
}

func (s *DispatcherSuite) TestRunningRunDoesNotBlockFollowUp() {
	assignee := uuid.New()
	taskID := uuid.New()
	repositoryID := uuid.New()
	s.runs.runs = []domain.TaskAgentRun{{
		ID:      uuid.New(),
		TaskID:  taskID,
		AgentID: assignee,
		Status:  domain.TaskAgentRunStatusRunning,
	}}

	err := s.disp.Dispatch(context.Background(), board.DispatchInput{
		RepositoryID: repositoryID,
		Task: domain.BoardTask{
			ID:              taskID,
			RepositoryID:    repositoryID,
			Title:           "t",
			Column:          domain.TaskColumnInProgress,
			AssigneeAgentID: &assignee,
		},
		EventType: domain.BoardEventTaskMoved,
	})

	s.Require().NoError(err)
	s.Len(s.runs.runs, 2, "follow-up run queues behind the running one")
	s.Len(s.runner.jobs, 1)
}

func (s *DispatcherSuite) TestConcurrentDispatchQueuesOnlyOneRun() {
	assignee := uuid.New()
	taskID := uuid.New()
	repositoryID := uuid.New()

	winner := domain.TaskAgentRun{
		ID:           uuid.New(),
		TaskID:       taskID,
		AgentID:      assignee,
		BoardEventID: uuid.New(),
		Status:       domain.TaskAgentRunStatusPending,
	}
	s.runs.beforeCreate = func() {
		s.runs.beforeCreate = nil
		s.runs.runs = append(s.runs.runs, winner)
	}

	err := s.disp.Dispatch(context.Background(), board.DispatchInput{
		RepositoryID: repositoryID,
		Task: domain.BoardTask{
			ID:              taskID,
			RepositoryID:    repositoryID,
			Title:           "t",
			Column:          domain.TaskColumnInProgress,
			AssigneeAgentID: &assignee,
		},
		EventType: domain.BoardEventTaskMoved,
	})

	s.Require().NoError(err, "losing the race is a no-op, not an error the user sees")
	s.Len(s.events.events, 1, "event history stays complete")
	s.Require().Len(s.runs.runs, 1, "the racer's insert must not add a second queued run")
	s.Equal(winner.ID, s.runs.runs[0].ID)
	s.Empty(s.runner.jobs, "the winner's run is already queued by the dispatch that created it")
}

func (s *DispatcherSuite) TestConcurrentAssignDispatchDoesNotDuplicateRun() {
	assignee := uuid.New()
	taskID := uuid.New()
	repositoryID := uuid.New()

	winner := domain.TaskAgentRun{
		ID:           uuid.New(),
		TaskID:       taskID,
		AgentID:      assignee,
		BoardEventID: uuid.New(),
		Status:       domain.TaskAgentRunStatusPending,
	}
	s.runs.beforeCreate = func() {
		s.runs.beforeCreate = nil
		s.runs.runs = append(s.runs.runs, winner)
	}

	err := s.disp.Dispatch(context.Background(), board.DispatchInput{
		RepositoryID: repositoryID,
		Task: domain.BoardTask{
			ID:              taskID,
			RepositoryID:    repositoryID,
			Title:           "t",
			Column:          domain.TaskColumnInProgress,
			AssigneeAgentID: &assignee,
		},
		EventType: domain.BoardEventTaskAssigned,
	})

	s.Require().NoError(err)
	s.Len(s.runs.runs, 1, "no second queued run for the same task+agent")
	s.Empty(s.runner.jobs)
}

func (s *DispatcherSuite) TestDispatchWinnerIsStillEnqueued() {
	assignee := uuid.New()
	taskID := uuid.New()
	repositoryID := uuid.New()

	err := s.disp.Dispatch(context.Background(), board.DispatchInput{
		RepositoryID: repositoryID,
		Task: domain.BoardTask{
			ID:              taskID,
			RepositoryID:    repositoryID,
			Title:           "t",
			Column:          domain.TaskColumnInProgress,
			AssigneeAgentID: &assignee,
		},
		EventType: domain.BoardEventTaskMoved,
	})

	s.Require().NoError(err)
	s.Require().Len(s.runs.runs, 1)
	s.Require().Len(s.runner.jobs, 1)
	s.Equal(s.runs.runs[0].ID, s.runner.jobs[0].Run.ID)
	s.Equal(s.events.events[0].ID, s.runner.jobs[0].Run.BoardEventID)
}

func (s *DispatcherSuite) TestAgentOwnCommentDoesNotRedispatchIt() {
	assignee := uuid.New()
	repositoryID := uuid.New()

	err := s.disp.Dispatch(context.Background(), board.DispatchInput{
		RepositoryID: repositoryID,
		Task: domain.BoardTask{
			ID:              uuid.New(),
			RepositoryID:    repositoryID,
			Title:           "t",
			Column:          domain.TaskColumnInProgress,
			AssigneeAgentID: &assignee,
		},
		EventType: domain.BoardEventTaskCommented,
		Payload: map[string]interface{}{
			"content":     "progress note",
			"author_type": "agent",
			"author_id":   assignee.String(),
		},
	})

	s.Require().NoError(err)
	s.Len(s.events.events, 1, "event history stays complete")
	s.Empty(s.runs.runs, "the commenting agent keeps working in its existing run")
	s.Empty(s.runner.jobs)
}

func (s *DispatcherSuite) TestCommentDoesNotQueueSecondRunWhileAgentIsRunning() {
	assignee := uuid.New()
	taskID := uuid.New()
	repositoryID := uuid.New()
	s.runs.runs = []domain.TaskAgentRun{{
		ID:      uuid.New(),
		TaskID:  taskID,
		AgentID: assignee,
		Status:  domain.TaskAgentRunStatusRunning,
	}}

	err := s.disp.Dispatch(context.Background(), board.DispatchInput{
		RepositoryID: repositoryID,
		Task: domain.BoardTask{
			ID:              taskID,
			RepositoryID:    repositoryID,
			Title:           "t",
			Column:          domain.TaskColumnInProgress,
			AssigneeAgentID: &assignee,
		},
		EventType: domain.BoardEventTaskCommented,
		Payload:   map[string]interface{}{"content": "one more thing", "author_type": "user"},
	})

	s.Require().NoError(err)
	s.Len(s.events.events, 1, "event history stays complete")
	s.Len(s.runs.runs, 1, "no second run on a task its agent is already running")
	s.Empty(s.runner.jobs)
}

func (s *DispatcherSuite) TestCommentDispatchesWhenNoRunIsLive() {
	assignee := uuid.New()
	taskID := uuid.New()
	repositoryID := uuid.New()
	s.runs.runs = []domain.TaskAgentRun{{
		ID:      uuid.New(),
		TaskID:  taskID,
		AgentID: assignee,
		Status:  domain.TaskAgentRunStatusCompleted,
	}}

	err := s.disp.Dispatch(context.Background(), board.DispatchInput{
		RepositoryID: repositoryID,
		Task: domain.BoardTask{
			ID:              taskID,
			RepositoryID:    repositoryID,
			Title:           "t",
			Column:          domain.TaskColumnNeedRevision,
			AssigneeAgentID: &assignee,
		},
		EventType: domain.BoardEventTaskCommented,
		Payload:   map[string]interface{}{"content": "please fix", "author_type": "user"},
	})

	s.Require().NoError(err)
	s.Require().Len(s.runner.jobs, 1)
	s.Equal(assignee, s.runner.jobs[0].Run.AgentID)
}

func (s *DispatcherSuite) TestBlockedColumnNeverDispatches() {
	s.board.agentsByColumn[string(domain.TaskColumnBlocked)] = []uuid.UUID{uuid.New()}
	repositoryID := uuid.New()

	err := s.disp.Dispatch(context.Background(), board.DispatchInput{
		RepositoryID: repositoryID,
		Task: domain.BoardTask{
			ID:           uuid.New(),
			RepositoryID: repositoryID,
			Title:        "waiting on an answer",
			Column:       domain.TaskColumnBlocked,
		},
		EventType: domain.BoardEventTaskMoved,
	})

	s.Require().NoError(err)
	s.Len(s.events.events, 1, "event history stays complete")
	s.Empty(s.runs.runs)
	s.Empty(s.runner.jobs)
}

func (s *DispatcherSuite) TestPipelineGateBlocksDispatchOnReadyForQAMove() {
	s.disp.SetPipelineGate(true)
	taskID := uuid.New()
	repositoryID := uuid.New()
	err := s.disp.Dispatch(context.Background(), board.DispatchInput{
		RepositoryID: repositoryID,
		Task: domain.BoardTask{
			ID:           taskID,
			RepositoryID: repositoryID,
			Title:        "t",
			Column:       domain.TaskColumnReadyForQA,
		},
		EventType: domain.BoardEventTaskMoved,
	})
	s.Require().NoError(err)
	s.Len(s.events.events, 1, "event history must stay complete even when the pipeline gate blocks dispatch")
	s.Empty(s.runs.runs)
	s.Empty(s.runner.jobs)
}

func (s *DispatcherSuite) TestPipelineGateDisabledStillDispatchesReadyForQAMove() {
	agentQA := uuid.New()
	s.board.agentsByColumn[string(domain.TaskColumnReadyForQA)] = []uuid.UUID{agentQA}
	taskID := uuid.New()
	repositoryID := uuid.New()
	err := s.disp.Dispatch(context.Background(), board.DispatchInput{
		RepositoryID: repositoryID,
		Task: domain.BoardTask{
			ID:           taskID,
			RepositoryID: repositoryID,
			Title:        "t",
			Column:       domain.TaskColumnReadyForQA,
		},
		EventType: domain.BoardEventTaskMoved,
	})
	s.Require().NoError(err)
	s.Len(s.events.events, 1)
	s.Require().Len(s.runs.runs, 1)
	s.Equal(agentQA, s.runs.runs[0].AgentID)
}

func (s *DispatcherSuite) TestDispatchQACreatesRunsWithPipelinePayload() {
	agentQA := uuid.New()
	s.board.agentsByColumn[string(domain.TaskColumnReadyForQA)] = []uuid.UUID{agentQA}
	s.disp.SetPipelineGate(true)

	taskID := uuid.New()
	repositoryID := uuid.New()
	pipelineID := uuid.New()
	task := domain.BoardTask{
		ID:           taskID,
		RepositoryID: repositoryID,
		Title:        "t",
		Column:       domain.TaskColumnReadyForQA,
	}

	err := s.disp.DispatchQA(context.Background(), repositoryID, task, pipelineID, "")
	s.Require().NoError(err)
	s.Require().Len(s.events.events, 1)
	s.Equal(domain.BoardEventTaskMoved, s.events.events[0].EventType)
	s.Require().Len(s.runs.runs, 1)
	s.Equal(agentQA, s.runs.runs[0].AgentID)
	s.Require().Len(s.runner.jobs, 1)

	var payload map[string]interface{}
	s.Require().NoError(json.Unmarshal(s.events.events[0].Payload, &payload))
	s.Equal("success", payload["pipeline"])
	s.Equal(pipelineID.String(), payload["pipeline_id"])
}

func (s *DispatcherSuite) TestNeedRevisionAssigneeOnly() {
	assignee := uuid.New()
	other := uuid.New()
	s.board.agentsByColumn[string(domain.TaskColumnNeedRevision)] = []uuid.UUID{other}
	taskID := uuid.New()
	projectID := uuid.New()
	err := s.disp.Dispatch(context.Background(), board.DispatchInput{
		RepositoryID: projectID,
		Task: domain.BoardTask{
			ID:              taskID,
			RepositoryID:    projectID,
			Column:          domain.TaskColumnNeedRevision,
			AssigneeAgentID: &assignee,
		},
		EventType: domain.BoardEventTaskCommented,
		Payload:   map[string]interface{}{"content": "fix"},
	})
	s.Require().NoError(err)
	s.Len(s.runs.runs, 1)
	s.Equal(assignee, s.runs.runs[0].AgentID)
}

type fakeSpanStore struct {
	moves    []string
	attached []string
}

func (f *fakeSpanStore) RecordMove(_ context.Context, _, taskID uuid.UUID, toColumn string, _ time.Time) error {
	f.moves = append(f.moves, taskID.String()+":"+toColumn)
	return nil
}

func (f *fakeSpanStore) AttachAgent(_ context.Context, taskID, agentID uuid.UUID) error {
	f.attached = append(f.attached, taskID.String()+":"+agentID.String())
	return nil
}

func (f *fakeSpanStore) SetReviewVerdict(context.Context, uuid.UUID, string, string) error {
	return nil
}

func (f *fakeSpanStore) OpenSpan(context.Context, uuid.UUID) (domain.TaskColumnSpan, bool, error) {
	return domain.TaskColumnSpan{}, false, nil
}

func (f *fakeSpanStore) OwnersForTask(context.Context, uuid.UUID) (map[string]uuid.UUID, error) {
	return nil, nil
}

func (f *fakeSpanStore) HasVisited(context.Context, uuid.UUID, string) (bool, error) {
	return false, nil
}

func (f *fakeSpanStore) LatestVerdicts(context.Context, uuid.UUID) (map[string]string, error) {
	return nil, nil
}

func (f *fakeSpanStore) CleanTaskHours(context.Context, uuid.UUID, []string, time.Time, time.Time) ([]float64, error) {
	return nil, nil
}

func (s *DispatcherSuite) TestDispatchRecordsSpanForMove() {
	spans := &fakeSpanStore{}
	s.disp.SetSpans(spans)
	taskID := uuid.New()

	err := s.disp.Dispatch(context.Background(), board.DispatchInput{
		RepositoryID: uuid.New(),
		Task:         domain.BoardTask{ID: taskID, Column: domain.TaskColumnInQA},
		EventType:    domain.BoardEventTaskMoved,
	})

	s.Require().NoError(err)
	s.Equal([]string{taskID.String() + ":in_qa"}, spans.moves)
}

func (s *DispatcherSuite) TestDispatchDoesNotRecordSpanForComment() {
	spans := &fakeSpanStore{}
	s.disp.SetSpans(spans)

	err := s.disp.Dispatch(context.Background(), board.DispatchInput{
		RepositoryID: uuid.New(),
		Task:         domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnInQA},
		EventType:    domain.BoardEventTaskCommented,
	})

	s.Require().NoError(err)
	s.Empty(spans.moves)
}

func (s *DispatcherSuite) TestDispatchAttachesRunningAgentToSpan() {
	spans := &fakeSpanStore{}
	s.disp.SetSpans(spans)
	agent := uuid.New()
	s.board.agentsByColumn[string(domain.TaskColumnInProgress)] = []uuid.UUID{agent}
	taskID := uuid.New()

	err := s.disp.Dispatch(context.Background(), board.DispatchInput{
		RepositoryID: uuid.New(),
		Task:         domain.BoardTask{ID: taskID, Column: domain.TaskColumnInProgress},
		EventType:    domain.BoardEventTaskMoved,
	})

	s.Require().NoError(err)
	s.Equal([]string{taskID.String() + ":" + agent.String()}, spans.attached)
}

func (s *DispatcherSuite) TestBacklogColumnNeverDispatches() {
	assignee := uuid.New()
	s.board.agentsByColumn[string(domain.TaskColumnBacklog)] = []uuid.UUID{uuid.New()}
	repositoryID := uuid.New()

	err := s.disp.Dispatch(context.Background(), board.DispatchInput{
		RepositoryID: repositoryID,
		Task: domain.BoardTask{
			ID:              uuid.New(),
			RepositoryID:    repositoryID,
			Title:           "not on the board yet",
			Column:          domain.TaskColumnBacklog,
			AssigneeAgentID: &assignee,
		},
		EventType: domain.BoardEventTaskCreated,
	})

	s.Require().NoError(err)
	s.Len(s.events.events, 1, "event history stays complete")
	s.Empty(s.runs.runs)
	s.Empty(s.runner.jobs)
}

func (s *DispatcherSuite) TestMoveOutOfBacklogDispatches() {
	assignee := uuid.New()
	repositoryID := uuid.New()

	err := s.disp.Dispatch(context.Background(), board.DispatchInput{
		RepositoryID: repositoryID,
		Task: domain.BoardTask{
			ID:              uuid.New(),
			RepositoryID:    repositoryID,
			Title:           "taken onto the board",
			Column:          domain.TaskColumnTodo,
			AssigneeAgentID: &assignee,
		},
		EventType: domain.BoardEventTaskMoved,
	})

	s.Require().NoError(err)
	s.Require().Len(s.runner.jobs, 1)
	s.Equal(assignee, s.runner.jobs[0].Run.AgentID)
}

func (s *DispatcherSuite) TestInQARoutesToQAAgentNotAssignee() {
	assignee := uuid.New()
	agentQA := uuid.New()
	s.board.agentsByColumn[string(domain.TaskColumnInQA)] = []uuid.UUID{agentQA}
	repositoryID := uuid.New()

	err := s.disp.Dispatch(context.Background(), board.DispatchInput{
		RepositoryID: repositoryID,
		Task: domain.BoardTask{
			ID:              uuid.New(),
			RepositoryID:    repositoryID,
			Title:           "under test",
			Column:          domain.TaskColumnInQA,
			AssigneeAgentID: &assignee,
		},
		EventType: domain.BoardEventTaskMoved,
	})

	s.Require().NoError(err)
	s.Require().Len(s.runs.runs, 1)
	s.Equal(agentQA, s.runs.runs[0].AgentID, "in_qa belongs to QA, never to the implementer")
}

func (s *DispatcherSuite) TestHumanUATDoesNotDispatchAssignee() {
	assignee := uuid.New()
	repositoryID := uuid.New()

	err := s.disp.Dispatch(context.Background(), board.DispatchInput{
		RepositoryID: repositoryID,
		Task: domain.BoardTask{
			ID:              uuid.New(),
			RepositoryID:    repositoryID,
			Title:           "awaiting human approval",
			Column:          domain.TaskColumnHumanUAT,
			AssigneeAgentID: &assignee,
		},
		EventType: domain.BoardEventTaskMoved,
	})

	s.Require().NoError(err)
	s.Len(s.events.events, 1, "event history stays complete")
	s.Empty(s.runs.runs)
	s.Empty(s.runner.jobs)
}

func (s *DispatcherSuite) TestDoneDoesNotDispatchImplementationAssignee() {
	assignee := uuid.New()
	repositoryID := uuid.New()

	err := s.disp.Dispatch(context.Background(), board.DispatchInput{
		RepositoryID: repositoryID,
		Task: domain.BoardTask{
			ID:              uuid.New(),
			RepositoryID:    repositoryID,
			Title:           "shipped",
			Column:          domain.TaskColumnDone,
			TaskType:        "task",
			AssigneeAgentID: &assignee,
		},
		EventType: domain.BoardEventTaskMoved,
	})

	s.Require().NoError(err)
	s.Len(s.events.events, 1, "event history stays complete")
	s.Empty(s.runs.runs)
	s.Empty(s.runner.jobs)
}

func doneTaskWithOpenPR(repositoryID uuid.UUID, assignee *uuid.UUID) domain.BoardTask {
	return domain.BoardTask{
		ID:              uuid.New(),
		RepositoryID:    repositoryID,
		Title:           "signed off, not merged",
		Column:          domain.TaskColumnDone,
		TaskType:        "task",
		AssigneeAgentID: assignee,
		PRURL:           "https://github.com/acme/widget/pull/42",
		PRNumber:        42,
	}
}

func (s *DispatcherSuite) TestDoneWakesTheQAAgentToMergeAnUnmergedPR() {
	qa := uuid.New()
	assignee := uuid.New()
	repositoryID := uuid.New()
	s.board.agentsByColumn["done"] = []uuid.UUID{qa}

	err := s.disp.Dispatch(context.Background(), board.DispatchInput{
		RepositoryID: repositoryID,
		Task:         doneTaskWithOpenPR(repositoryID, &assignee),
		EventType:    domain.BoardEventTaskMoved,
	})

	s.Require().NoError(err)
	s.Require().Len(s.runs.runs, 1)
	s.Equal(qa, s.runs.runs[0].AgentID, "done dispatches its subscriber, not the implementer")
	s.Len(s.runner.jobs, 1)
}

func (s *DispatcherSuite) TestDoneDoesNotWakeQAForAnAlreadyMergedPR() {
	qa := uuid.New()
	repositoryID := uuid.New()
	s.board.agentsByColumn["done"] = []uuid.UUID{qa}
	task := doneTaskWithOpenPR(repositoryID, nil)
	task.MergeCommitSHA = "abc1234567890000000000000000000000000000"

	err := s.disp.Dispatch(context.Background(), board.DispatchInput{
		RepositoryID: repositoryID,
		Task:         task,
		EventType:    domain.BoardEventTaskMoved,
	})

	s.Require().NoError(err)
	s.Len(s.events.events, 1, "event history stays complete")
	s.Empty(s.runs.runs)
	s.Empty(s.runner.jobs)
}

func (s *DispatcherSuite) TestDoneDoesNotWakeQAOnAComment() {
	qa := uuid.New()
	repositoryID := uuid.New()
	s.board.agentsByColumn["done"] = []uuid.UUID{qa}

	err := s.disp.Dispatch(context.Background(), board.DispatchInput{
		RepositoryID: repositoryID,
		Task:         doneTaskWithOpenPR(repositoryID, nil),
		EventType:    domain.BoardEventTaskCommented,
	})

	s.Require().NoError(err)
	s.Empty(s.runs.runs)
	s.Empty(s.runner.jobs)
}

func (s *DispatcherSuite) TestDoneDoesNotWakeQATwiceWhileAMergeRunIsLive() {
	qa := uuid.New()
	repositoryID := uuid.New()
	s.board.agentsByColumn["done"] = []uuid.UUID{qa}
	task := doneTaskWithOpenPR(repositoryID, nil)

	s.Require().NoError(s.disp.Dispatch(context.Background(), board.DispatchInput{
		RepositoryID: repositoryID,
		Task:         task,
		EventType:    domain.BoardEventTaskMoved,
	}))
	s.Require().Len(s.runs.runs, 1)
	s.runs.runs[0].Status = domain.TaskAgentRunStatusRunning

	s.Require().NoError(s.disp.Dispatch(context.Background(), board.DispatchInput{
		RepositoryID: repositoryID,
		Task:         task,
		EventType:    domain.BoardEventTaskMoved,
	}))

	s.Len(s.runs.runs, 1, "a running merge is not duplicated")
	s.Len(s.runner.jobs, 1)
}

func (s *DispatcherSuite) TestDoneDoesNotWakeTheAgentThatMovedItThere() {
	qa := uuid.New()
	repositoryID := uuid.New()
	s.board.agentsByColumn["done"] = []uuid.UUID{qa}

	err := s.disp.Dispatch(context.Background(), board.DispatchInput{
		RepositoryID: repositoryID,
		Task:         doneTaskWithOpenPR(repositoryID, nil),
		EventType:    domain.BoardEventTaskMoved,
		Payload:      map[string]interface{}{"actor_agent_id": qa.String()},
	})

	s.Require().NoError(err)
	s.Empty(s.runs.runs)
}

func (s *DispatcherSuite) TestDoneMergeWakeIgnoresAnalizTasks() {
	qa := uuid.New()
	assignee := uuid.New()
	repositoryID := uuid.New()
	s.board.agentsByColumn["done"] = []uuid.UUID{qa}
	task := doneTaskWithOpenPR(repositoryID, &assignee)
	task.TaskType = "analiz"

	err := s.disp.Dispatch(context.Background(), board.DispatchInput{
		RepositoryID: repositoryID,
		Task:         task,
		EventType:    domain.BoardEventTaskMoved,
	})

	s.Require().NoError(err)
	s.Require().Len(s.runs.runs, 1)
	s.Equal(assignee, s.runs.runs[0].AgentID, "analiz in done still goes to the architect")
}

func (s *DispatcherSuite) TestDoneDispatchesAnalizAssignee() {
	assignee := uuid.New()
	repositoryID := uuid.New()

	err := s.disp.Dispatch(context.Background(), board.DispatchInput{
		RepositoryID: repositoryID,
		Task: domain.BoardTask{
			ID:              uuid.New(),
			RepositoryID:    repositoryID,
			Title:           "analiz approved",
			Column:          domain.TaskColumnDone,
			TaskType:        "analiz",
			AssigneeAgentID: &assignee,
		},
		EventType: domain.BoardEventTaskMoved,
	})

	s.Require().NoError(err)
	s.Require().Len(s.runs.runs, 1)
	s.Equal(assignee, s.runs.runs[0].AgentID)
}

func (s *DispatcherSuite) TestReleasedDoesNotDispatch() {
	assignee := uuid.New()
	repositoryID := uuid.New()

	err := s.disp.Dispatch(context.Background(), board.DispatchInput{
		RepositoryID: repositoryID,
		Task: domain.BoardTask{
			ID:              uuid.New(),
			RepositoryID:    repositoryID,
			Title:           "released",
			Column:          domain.TaskColumnReleased,
			TaskType:        "analiz",
			AssigneeAgentID: &assignee,
		},
		EventType: domain.BoardEventTaskMoved,
	})

	s.Require().NoError(err)
	s.Len(s.events.events, 1, "event history stays complete")
	s.Empty(s.runs.runs)
	s.Empty(s.runner.jobs)
}

func (s *DispatcherSuite) TestCommentInQAReachesQANotAssignee() {
	assignee := uuid.New()
	agentQA := uuid.New()
	s.board.agentsByColumn[string(domain.TaskColumnInQA)] = []uuid.UUID{agentQA}
	repositoryID := uuid.New()

	err := s.disp.Dispatch(context.Background(), board.DispatchInput{
		RepositoryID: repositoryID,
		Task: domain.BoardTask{
			ID:              uuid.New(),
			RepositoryID:    repositoryID,
			Column:          domain.TaskColumnInQA,
			AssigneeAgentID: &assignee,
		},
		EventType: domain.BoardEventTaskCommented,
		Payload:   map[string]interface{}{"content": "also check the empty state"},
	})

	s.Require().NoError(err)
	s.Require().Len(s.runs.runs, 1)
	s.Equal(agentQA, s.runs.runs[0].AgentID)
}

func (s *DispatcherSuite) TestQAOwnMoveIntoInQADoesNotRedispatchQA() {
	agentQA := uuid.New()
	s.board.agentsByColumn[string(domain.TaskColumnReadyForQA)] = []uuid.UUID{agentQA}
	s.board.agentsByColumn[string(domain.TaskColumnInQA)] = []uuid.UUID{agentQA}
	repositoryID := uuid.New()

	err := s.disp.Dispatch(context.Background(), board.DispatchInput{
		RepositoryID: repositoryID,
		Task: domain.BoardTask{
			ID:           uuid.New(),
			RepositoryID: repositoryID,
			Column:       domain.TaskColumnInQA,
		},
		EventType: domain.BoardEventTaskMoved,
		Payload:   map[string]interface{}{"actor_agent_id": agentQA.String()},
	})

	s.Require().NoError(err)
	s.Len(s.events.events, 1, "the move is still recorded")
	s.Empty(s.runs.runs, "QA moving its own task into in_qa must not start a second QA run")
}

func (s *DispatcherSuite) TestAssignedDefersToRunningRun() {
	assignee := uuid.New()
	repositoryID := uuid.New()
	taskID := uuid.New()
	s.runs.runs = append(s.runs.runs, domain.TaskAgentRun{
		ID:      uuid.New(),
		TaskID:  taskID,
		AgentID: assignee,
		Status:  domain.TaskAgentRunStatusRunning,
	})

	err := s.disp.Dispatch(context.Background(), board.DispatchInput{
		RepositoryID: repositoryID,
		Task: domain.BoardTask{
			ID:              taskID,
			RepositoryID:    repositoryID,
			Column:          domain.TaskColumnTodo,
			AssigneeAgentID: &assignee,
		},
		EventType: domain.BoardEventTaskAssigned,
		Payload:   map[string]interface{}{"to_agent_id": assignee.String()},
	})

	s.Require().NoError(err)
	s.Len(s.events.events, 1, "the assignment is still recorded")
	s.Len(s.runs.runs, 1, "no second run while the agent is already running the task")
}

// TestCodeReviewIsGatedUnconditionally proves the pipeline gate is no longer
// a per-repository opt-out (require_pipeline_for_review is gone): with the
// gate armed, wait_for_ci alone is enough to defer the reviewer.
func (s *DispatcherSuite) TestCodeReviewIsGatedUnconditionally() {
	architect := uuid.New()
	s.board.agentsByColumn[string(domain.TaskColumnCodeReview)] = []uuid.UUID{architect}
	s.disp.SetPipelineGate(true)

	repositoryID := uuid.New()
	err := s.disp.Dispatch(context.Background(), board.DispatchInput{
		RepositoryID: repositoryID,
		Task: domain.BoardTask{
			ID: uuid.New(), RepositoryID: repositoryID, Title: "t",
			Column: domain.TaskColumnCodeReview,
		},
		EventType: domain.BoardEventTaskMoved,
	})
	s.Require().NoError(err)
	s.Len(s.events.events, 1)
	s.Empty(s.runs.runs, "the reviewer must wait for the pipeline")
}

func (s *DispatcherSuite) TestDispatchQACarriesTheGateReasonWhenTheGateWasForcedOpen() {
	architect := uuid.New()
	s.board.agentsByColumn[string(domain.TaskColumnCodeReview)] = []uuid.UUID{architect}
	s.disp.SetPipelineGate(true)

	repositoryID := uuid.New()
	pipelineID := uuid.New()
	task := domain.BoardTask{
		ID: uuid.New(), RepositoryID: repositoryID, Title: "t",
		Column: domain.TaskColumnCodeReview,
	}

	err := s.disp.DispatchQA(context.Background(), repositoryID, task, pipelineID,
		domain.PipelineGateReasonCIUnavailable)
	s.Require().NoError(err)
	s.Require().Len(s.runs.runs, 1, "the gate was opened; the reviewer must be dispatched")

	var payload map[string]interface{}
	s.Require().NoError(json.Unmarshal(s.events.events[0].Payload, &payload))
	s.Equal(domain.PipelineGateReasonCIUnavailable, payload[domain.EventPayloadPipelineGate])
	s.Equal("gate_opened", payload["pipeline"], "a forced-open gate must not report itself as a success")
	s.Equal(domain.MoveReasonPipelineGateOpened, payload[domain.EventPayloadReason])
}
