package board_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/board"
	"github.com/makifbaysal/tasktrooper/server/internal/application/workflow/workflowtest"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type fakeGatePipelines struct{ pipelines []domain.TaskPipeline }

func (f *fakeGatePipelines) ListByTask(context.Context, uuid.UUID) ([]domain.TaskPipeline, error) {
	return f.pipelines, nil
}

type fakeTaskMover struct {
	moves []domain.UpdateBoardTaskRequest
}

func (f *fakeTaskMover) UpdateTask(_ context.Context, _, _ uuid.UUID, req domain.UpdateBoardTaskRequest) (domain.BoardTask, error) {
	f.moves = append(f.moves, req)
	return domain.BoardTask{}, nil
}

// A review round in code_review with two required reviewers, the pipeline
// gate on and CI already green: the state a card is in when one reviewer has
// decided and the other one's run died.
type StalledReviewSuite struct {
	suite.Suite
	board     *fakeBoardConfigStore
	events    *fakeEventStore
	runs      *fakeRunStore
	runner    *fakeRunner
	tasks     *fakeBoardTaskStore
	reviews   *memReviewStore
	pipelines *fakeGatePipelines
	mover     *fakeTaskMover
	quorum    *board.ReviewQuorum
	disp      *board.Dispatcher
	rec       *board.Reconciler
	task      domain.BoardTask
	architect uuid.UUID
	security  uuid.UUID
	developer uuid.UUID
}

func TestStalledReviewSuite(t *testing.T) { suite.Run(t, new(StalledReviewSuite)) }

func (s *StalledReviewSuite) SetupTest() {
	s.architect, s.security, s.developer = uuid.New(), uuid.New(), uuid.New()
	s.board = &fakeBoardConfigStore{agentsByColumn: map[string][]uuid.UUID{
		string(domain.TaskColumnCodeReview): {s.architect, s.security},
	}}
	s.events = &fakeEventStore{}
	s.runs = &fakeRunStore{}
	s.runner = &fakeRunner{}
	s.tasks = &fakeBoardTaskStore{runs: s.runs}
	s.reviews = &memReviewStore{
		clock: time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC),
		required: []domain.ReviewerRef{
			{ID: s.security, Name: "security-agent"},
			{ID: s.architect, Name: "system-architect"},
		},
	}
	s.pipelines = &fakeGatePipelines{pipelines: []domain.TaskPipeline{{
		ID: uuid.New(), Status: domain.PipelineStatusSuccess, CreatedAt: s.reviews.clock,
	}}}
	s.mover = &fakeTaskMover{}
	s.quorum = board.NewReviewQuorum(s.reviews, workflowtest.Default().Reader())

	s.disp = board.NewDispatcher(s.board, s.events, s.runs, s.runner, true)
	s.disp.SetWorkflows(workflowtest.Default().Reader())
	s.disp.SetPipelineGate(true)
	s.disp.SetGatePipelines(s.pipelines)
	s.disp.SetReviewQuorum(s.quorum)
	s.rec = board.NewReconciler(s.runs, s.tasks, s.disp, 0)
	s.rec.SetReviewQuorum(s.quorum, s.mover)

	s.task = domain.BoardTask{
		ID: uuid.New(), RepositoryID: uuid.New(), TaskType: "task",
		Column: domain.TaskColumnCodeReview, AssigneeAgentID: &s.developer,
	}
	s.tasks.tasks = []domain.BoardTask{s.task}
	s.reviews.visit(domain.TaskColumnInProgress, false)
	s.reviews.visit(domain.TaskColumnCodeReview, true)
}

func (s *StalledReviewSuite) decide(agent uuid.UUID, to domain.TaskColumn) {
	_, err := s.quorum.Decide(context.Background(), s.task, domain.TaskColumnCodeReview, to, domain.TaskActorAgent, &agent)
	s.Require().NoError(err)
}

// The fake run store lists the last appended run first, as the real one
// lists the newest: runs are given oldest first.
func (s *StalledReviewSuite) run(agent uuid.UUID, status string, age time.Duration) domain.TaskAgentRun {
	return domain.TaskAgentRun{
		ID: uuid.New(), TaskID: s.task.ID, AgentID: agent, Status: status,
		CreatedAt: time.Now().Add(-age), UpdatedAt: time.Now().Add(-age),
	}
}

func (s *StalledReviewSuite) redispatch() {
	s.Require().NoError(s.disp.Dispatch(context.Background(), board.DispatchInput{
		RepositoryID: s.task.RepositoryID,
		Task:         s.task,
		EventType:    domain.BoardEventTaskMoved,
		Payload:      map[string]interface{}{"reconciled": true},
	}))
}

func (s *StalledReviewSuite) TestARedispatchAfterGreenCIStartsOnlyTheReviewerStillToDecide() {
	s.decide(s.architect, domain.TaskColumnNeedRevision)

	s.redispatch()

	s.Require().Len(s.runner.jobs, 1, "CI reported before the run died, so no report is left to open the gate")
	s.Equal(s.security, s.runner.jobs[0].Run.AgentID, "the reviewer that already decided is not asked again")
}

func (s *StalledReviewSuite) TestARedispatchStillWaitsWhileCIIsRunning() {
	s.pipelines.pipelines = append(s.pipelines.pipelines, domain.TaskPipeline{
		ID: uuid.New(), Status: domain.PipelineStatusRunning, CreatedAt: s.reviews.clock.Add(time.Hour),
	})

	s.redispatch()

	s.Empty(s.runner.jobs, "the newest pipeline has not reported; its result dispatches the reviewers")
}

func (s *StalledReviewSuite) TestAnOrdinaryMoveIntoReviewStillWaitsForCI() {
	s.Require().NoError(s.disp.Dispatch(context.Background(), board.DispatchInput{
		RepositoryID: s.task.RepositoryID,
		Task:         s.task,
		EventType:    domain.BoardEventTaskMoved,
	}))

	s.Empty(s.runner.jobs, "an earlier push's green pipeline must not open the gate for a new one")
}

func (s *StalledReviewSuite) TestAReviewerWhoseRunDiedIsRestartedEvenWhenAnotherFinishedLater() {
	s.decide(s.architect, domain.TaskColumnNeedRevision)
	s.runs.runs = []domain.TaskAgentRun{
		s.run(s.security, domain.TaskAgentRunStatusFailed, 2*time.Minute),
		s.run(s.architect, domain.TaskAgentRunStatusCompleted, time.Minute),
	}

	s.rec.Run(context.Background())

	s.Require().Len(s.runner.jobs, 1)
	s.Equal(s.security, s.runner.jobs[0].Run.AgentID)
	s.Empty(s.mover.moves)
}

func (s *StalledReviewSuite) TestARoundIsLeftAloneWhileAReviewerIsStillRunning() {
	s.decide(s.architect, domain.TaskColumnNeedRevision)
	s.runs.runs = []domain.TaskAgentRun{
		s.run(s.security, domain.TaskAgentRunStatusFailed, 2*time.Minute),
		s.run(s.security, domain.TaskAgentRunStatusRunning, time.Second),
	}

	s.rec.Run(context.Background())

	s.Empty(s.runner.jobs)
	s.Empty(s.mover.moves)
}

func (s *StalledReviewSuite) TestARejectedRoundIsSentBackOnceTheOtherReviewerKeepsFailing() {
	s.decide(s.architect, domain.TaskColumnNeedRevision)
	s.runs.runs = []domain.TaskAgentRun{
		s.run(s.security, domain.TaskAgentRunStatusFailed, time.Minute),
		s.run(s.security, domain.TaskAgentRunStatusFailed, 2*time.Minute),
		s.run(s.security, domain.TaskAgentRunStatusFailed, 3*time.Minute),
	}

	s.rec.Run(context.Background())

	s.Empty(s.runner.jobs)
	s.Require().Len(s.mover.moves, 1)
	s.Equal(domain.TaskColumnNeedRevision, *s.mover.moves[0].Column)
	s.Equal(domain.MoveReasonReviewRejectedStalled, s.mover.moves[0].SystemReason)
}

func (s *StalledReviewSuite) TestAnApprovedRoundIsNeverClosedForAReviewerThatKeepsFailing() {
	s.decide(s.architect, domain.TaskColumnReadyForQA)
	s.runs.runs = []domain.TaskAgentRun{
		s.run(s.security, domain.TaskAgentRunStatusFailed, time.Minute),
		s.run(s.security, domain.TaskAgentRunStatusFailed, 2*time.Minute),
		s.run(s.security, domain.TaskAgentRunStatusFailed, 3*time.Minute),
	}

	s.rec.Run(context.Background())

	s.Empty(s.mover.moves, "an approval is not the round's verdict while a required reviewer has not decided")
}
