package board_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/board"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type changeCommenter struct {
	got []domain.CreateTaskCommentRequest
}

func (f *changeCommenter) AddComment(_ context.Context, _, _ uuid.UUID, req domain.CreateTaskCommentRequest) (domain.TaskComment, error) {
	f.got = append(f.got, req)
	return domain.TaskComment{ID: uuid.New()}, nil
}

type changeRunner struct{ active map[uuid.UUID]uuid.UUID }

func (f changeRunner) ActiveRunForTask(taskID uuid.UUID) (uuid.UUID, bool) {
	id, ok := f.active[taskID]
	return id, ok
}

type ChangeRequestSuite struct {
	suite.Suite
	repoID, taskID, runID, pmID uuid.UUID
	runs                        *controlRunStore
	runner                      *controlRunner
	tasks                       *controlTaskStore
	comments                    *changeCommenter
	ctl                         *board.Controller
}

func TestChangeRequestSuite(t *testing.T) { suite.Run(t, new(ChangeRequestSuite)) }

func (s *ChangeRequestSuite) SetupTest() {
	s.repoID, s.taskID, s.runID, s.pmID = uuid.New(), uuid.New(), uuid.New(), uuid.New()
	s.runs = newControlRunStore(domain.TaskAgentRun{ID: s.runID, TaskID: s.taskID, AgentID: uuid.New(), Status: domain.TaskAgentRunStatusRunning})
	s.runner = &controlRunner{}
	s.tasks = &controlTaskStore{task: domain.BoardTask{ID: s.taskID, RepositoryID: s.repoID, Column: domain.TaskColumnInProgress}}
	s.comments = &changeCommenter{}
	s.ctl = board.NewController(s.runs, &controlEventStore{}, s.tasks, s.runner)
	s.ctl.SetCommenter(s.comments)
}

func (s *ChangeRequestSuite) service(active bool) *board.ChangeRequests {
	live := changeRunner{active: map[uuid.UUID]uuid.UUID{}}
	if active {
		live.active[s.taskID] = s.runID
	}
	return board.NewChangeRequests(board.ChangeRequestDeps{Tasks: s.tasks, Runner: live, Interrupter: s.ctl, Commenter: s.comments})
}

func (s *ChangeRequestSuite) TestWhileARunIsActiveTheChangeIsRecordedAndTheRunLeftAlone() {
	out, err := s.service(true).Deliver(context.Background(), s.repoID, s.taskID, s.pmID, " add an archive filter ", false)

	s.Require().NoError(err)
	s.Equal(board.ChangeDeliveryNotified, out.Outcome)
	s.Equal(&s.runID, out.RunID)
	s.Require().Len(s.comments.got, 1)
	s.Equal("Change request from PM: add an archive filter", s.comments.got[0].Content)
	s.Equal("agent", s.comments.got[0].AuthorType)
	s.Equal(s.pmID.String(), s.comments.got[0].AuthorID)
	s.True(s.comments.got[0].Informational, "the live run is told here; the comment must not wake a second run")
	s.Empty(s.runner.cancelled)
	s.Empty(s.runner.jobs)
}

func (s *ChangeRequestSuite) TestInterruptStopsTheRunAndRestartsTheSameAgentWithTheChange() {
	out, err := s.service(true).Deliver(context.Background(), s.repoID, s.taskID, s.pmID, "drop the CSV option", true)

	s.Require().NoError(err)
	s.Equal(board.ChangeDeliveryInterrupted, out.Outcome)
	s.Equal([]uuid.UUID{s.runID}, s.runner.cancelled)
	s.Require().Len(s.runner.jobs, 1)
	s.Equal(*out.RunID, s.runner.jobs[0].Run.ID)
	s.Empty(s.tasks.blocked)
	s.Require().Len(s.comments.got, 1, "one durable copy, not one from the interrupt and one more")
	s.True(strings.HasPrefix(s.comments.got[0].Content, domain.ChangeRequestCommentPrefix))
	s.Equal("agent", s.comments.got[0].AuthorType)
}

func (s *ChangeRequestSuite) TestWithNoActiveRunTheChangeIsStoredForTheNextOne() {
	out, err := s.service(false).Deliver(context.Background(), s.repoID, s.taskID, s.pmID, "rename the tab", true)

	s.Require().NoError(err)
	s.Equal(board.ChangeDeliveryStored, out.Outcome)
	s.Nil(out.RunID)
	s.Empty(s.runner.cancelled, "nothing to interrupt")
	s.Require().Len(s.comments.got, 1)
	s.NotNil(out.CommentID)
}

func (s *ChangeRequestSuite) TestARunThatEndedBetweenTheCheckAndTheInterruptFallsBackToStoring() {
	s.runs.byID[s.runID] = domain.TaskAgentRun{ID: s.runID, TaskID: s.taskID, Status: domain.TaskAgentRunStatusCompleted}

	out, err := s.service(true).Deliver(context.Background(), s.repoID, s.taskID, s.pmID, "rename the tab", true)

	s.Require().NoError(err)
	s.Equal(board.ChangeDeliveryStored, out.Outcome)
	s.Require().Len(s.comments.got, 1)
}

func (s *ChangeRequestSuite) TestEmptyAndOversizedRequestsAreRefused() {
	_, err := s.service(true).Deliver(context.Background(), s.repoID, s.taskID, s.pmID, "   ", false)
	s.ErrorIs(err, domain.ErrChangeRequestEmpty)

	_, err = s.service(true).Deliver(context.Background(), s.repoID, s.taskID, s.pmID, strings.Repeat("x", domain.MaxChangeRequestChars+1), false)
	s.ErrorIs(err, domain.ErrChangeRequestTooLong)
	s.Empty(s.comments.got)
}
