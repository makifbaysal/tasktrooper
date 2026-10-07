package board_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/board"
	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type fakeBlockedTaker struct {
	task  domain.BoardTask
	on    uuid.UUID
	taken bool
	err   error
	calls int
}

func (f *fakeBlockedTaker) TakeBlockedBySession(_ context.Context, sessionID uuid.UUID) (domain.BoardTask, bool, error) {
	f.calls++
	if f.err != nil {
		return domain.BoardTask{}, false, f.err
	}
	if f.taken || sessionID != f.on {
		return domain.BoardTask{}, false, nil
	}
	f.taken = true
	return f.task, true, nil
}

type fakeTaskCommenter struct {
	comments []domain.CreateTaskCommentRequest
	err      error
}

func (f *fakeTaskCommenter) AddComment(_ context.Context, _, _ uuid.UUID, req domain.CreateTaskCommentRequest) (domain.TaskComment, error) {
	if f.err != nil {
		return domain.TaskComment{}, f.err
	}
	f.comments = append(f.comments, req)
	return domain.TaskComment{Content: req.Content}, nil
}

type ResumeSuite struct {
	suite.Suite
	board     *fakeBoardConfigStore
	events    *fakeEventStore
	runs      *fakeRunStore
	runner    *fakeRunner
	disp      *board.Dispatcher
	taker     *fakeBlockedTaker
	commenter *fakeTaskCommenter
	resumer   *board.AnswerResumer
	session   uuid.UUID
	taskID    uuid.UUID
	repoID    uuid.UUID
	question  string
}

func TestResumeSuite(t *testing.T) {
	suite.Run(t, new(ResumeSuite))
}

func (s *ResumeSuite) SetupTest() {
	agentA := uuid.New()
	s.board = &fakeBoardConfigStore{
		agentsByColumn: map[string][]uuid.UUID{"todo": {agentA}},
	}
	s.events = &fakeEventStore{}
	s.runs = &fakeRunStore{}
	s.runner = &fakeRunner{}
	s.disp = board.NewDispatcher(s.board, s.events, s.runs, s.runner, true)

	s.session = uuid.New()
	s.taskID = uuid.New()
	s.repoID = uuid.New()
	s.question = "Which database should the new service use?"
	s.taker = &fakeBlockedTaker{
		on: s.session,
		task: domain.BoardTask{
			ID:                  s.taskID,
			RepositoryID:        s.repoID,
			Title:               "Add billing service",
			Column:              domain.TaskColumnTodo,
			BlockedOriginColumn: domain.TaskColumnTodo,
			BlockedQuestion:     s.question,
		},
	}
	s.commenter = &fakeTaskCommenter{}
	s.resumer = board.NewAnswerResumer(s.taker, s.disp, s.commenter)
}

func (s *ResumeSuite) TestAnswerRedispatchesWithQuestionAndAnswer() {
	s.True(s.resumer.ResumeOnAnswer(context.Background(), s.session, "Postgres"))

	s.Require().Len(s.runner.jobs, 1)
	s.Equal(s.taskID, s.runner.jobs[0].Task.ID)

	s.Require().Len(s.events.events, 1)
	var payload map[string]any
	s.Require().NoError(json.Unmarshal(s.events.events[0].Payload, &payload))
	s.Equal("question_answered", payload["resumed"])
	s.Equal(s.question, payload["question"])
	s.Equal("Postgres", payload["answer"])
}

func (s *ResumeSuite) TestAnswerIsRecordedOnTheTask() {
	s.resumer.ResumeOnAnswer(context.Background(), s.session, "Postgres")

	s.Require().Len(s.commenter.comments, 1)
	comment := s.commenter.comments[0]
	s.Equal("system", comment.AuthorType)
	s.True(prompt.IsClarificationComment(comment.Content))
	s.Contains(comment.Content, s.question)
	s.Contains(comment.Content, "Postgres")

	replay := prompt.AnsweredClarificationsMessage([]domain.TaskComment{{Content: comment.Content}})
	s.Contains(replay, s.question)
	s.Contains(replay, "Postgres")
}

func (s *ResumeSuite) TestTaskChatAnswerIsAnInformationalClarificationComment() {
	repositoryID, taskID := uuid.New(), uuid.New()

	s.resumer.RecordTaskAnswer(context.Background(), repositoryID, taskID, s.question, "Postgres")

	s.Require().Len(s.commenter.comments, 1)
	comment := s.commenter.comments[0]
	s.Equal("system", comment.AuthorType)
	s.True(comment.Informational, "the chat carries the conversation on; the comment must not wake a board run")
	s.True(prompt.IsClarificationComment(comment.Content))
	s.Contains(comment.Content, s.question)
	s.Contains(comment.Content, "Postgres")
	s.Empty(s.runner.jobs)
}

func (s *ResumeSuite) TestCommentFailureStillResumes() {
	s.commenter.err = errors.New("db down")

	s.True(s.resumer.ResumeOnAnswer(context.Background(), s.session, "Postgres"))
	s.Len(s.runner.jobs, 1)
}

func (s *ResumeSuite) TestUnrelatedSessionIsANoOp() {
	s.False(s.resumer.ResumeOnAnswer(context.Background(), uuid.New(), "hello"))

	s.Equal(1, s.taker.calls)
	s.Empty(s.runner.jobs)
	s.Empty(s.events.events)
	s.Empty(s.commenter.comments)
}

func (s *ResumeSuite) TestSecondAnswerDoesNotDispatchTwice() {
	s.resumer.ResumeOnAnswer(context.Background(), s.session, "Postgres")
	s.resumer.ResumeOnAnswer(context.Background(), s.session, "...and use pgx")

	s.Len(s.runner.jobs, 1, "the take clears the block, so only the first answer resumes the task")
}

func (s *ResumeSuite) TestStoreErrorIsSwallowed() {
	s.taker.err = errors.New("db down")

	s.NotPanics(func() {
		s.resumer.ResumeOnAnswer(context.Background(), s.session, "Postgres")
	})
	s.Empty(s.runner.jobs)
}

func (s *ResumeSuite) TestNilResumerIsInert() {
	var nilResumer *board.AnswerResumer
	s.NotPanics(func() {
		nilResumer.ResumeOnAnswer(context.Background(), s.session, "Postgres")
	})

	partial := board.NewAnswerResumer(nil, s.disp, nil)
	s.NotPanics(func() {
		partial.ResumeOnAnswer(context.Background(), s.session, "Postgres")
	})
	s.Empty(s.runner.jobs)
}
