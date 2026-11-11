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

type completionRecorder struct {
	taskID uuid.UUID
	clean  bool
	calls  int
}

func (c *completionRecorder) MarkCompleted(_ context.Context, taskID uuid.UUID, clean bool, _ time.Time) error {
	c.taskID = taskID
	c.clean = clean
	c.calls++
	return nil
}

type visitedSpans struct {
	visited map[string]bool
	err     error
}

func (v *visitedSpans) HasVisited(_ context.Context, _ uuid.UUID, column string) (bool, error) {
	if v.err != nil {
		return false, v.err
	}
	return v.visited[column], nil
}

type CompletionSuite struct{ suite.Suite }

func TestCompletionSuite(t *testing.T) { suite.Run(t, new(CompletionSuite)) }

func (s *CompletionSuite) TestCleanWhenNeverRevised() {
	rec := &completionRecorder{}
	stamper := board.NewCompletionStamper(rec, &visitedSpans{visited: map[string]bool{}})
	task := domain.BoardTask{ID: uuid.New()}

	stamper.OnColumnTransition(context.Background(), task, domain.TaskColumnHumanUAT, domain.TaskColumnDone)

	s.Equal(1, rec.calls)
	s.Equal(task.ID, rec.taskID)
	s.True(rec.clean)
}

func (s *CompletionSuite) TestNotCleanAfterRevision() {
	rec := &completionRecorder{}
	stamper := board.NewCompletionStamper(rec, &visitedSpans{
		visited: map[string]bool{string(domain.TaskColumnNeedRevision): true},
	})

	stamper.OnColumnTransition(context.Background(), domain.BoardTask{ID: uuid.New()},
		domain.TaskColumnHumanUAT, domain.TaskColumnDone)

	s.Equal(1, rec.calls)
	s.False(rec.clean)
}

func (s *CompletionSuite) TestUnknownHistoryIsNotScoredAsClean() {
	rec := &completionRecorder{}
	stamper := board.NewCompletionStamper(rec, &visitedSpans{err: errors.New("pool down")})

	stamper.OnColumnTransition(context.Background(), domain.BoardTask{ID: uuid.New()},
		domain.TaskColumnHumanUAT, domain.TaskColumnReleased)

	s.Equal(1, rec.calls)
	s.False(rec.clean)
}

func (s *CompletionSuite) TestIgnoresNonTerminalMoves() {
	rec := &completionRecorder{}
	stamper := board.NewCompletionStamper(rec, &visitedSpans{visited: map[string]bool{}})

	stamper.OnColumnTransition(context.Background(), domain.BoardTask{ID: uuid.New()},
		domain.TaskColumnInProgress, domain.TaskColumnCodeReview)

	s.Zero(rec.calls)
}
