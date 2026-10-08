package board_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/board"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type dispatchBlockerReader struct {
	blockers []domain.BoardTask
	err      error
}

func (d *dispatchBlockerReader) ListBlockingSources(context.Context, uuid.UUID) ([]domain.BoardTask, error) {
	if d.err != nil {
		return nil, d.err
	}
	return d.blockers, nil
}

type dispatchParker struct {
	parked []uuid.UUID
}

func (d *dispatchParker) MarkWorkOrderWaiting(_ context.Context, _, taskID uuid.UUID, _ string) error {
	d.parked = append(d.parked, taskID)
	return nil
}

func (s *DispatcherSuite) TestBlockedTaskIsParkedInsteadOfStarted() {
	parker := &dispatchParker{}
	s.disp.SetWorkOrder(board.NewWorkOrder(&dispatchBlockerReader{
		blockers: []domain.BoardTask{{
			ID: uuid.New(), Key: "T-1", Title: "API migration", Column: domain.TaskColumnInProgress,
		}},
	}, parker))

	taskID := uuid.New()
	repositoryID := uuid.New()
	err := s.disp.Dispatch(context.Background(), board.DispatchInput{
		RepositoryID: repositoryID,
		Task: domain.BoardTask{
			ID: taskID, RepositoryID: repositoryID, Key: "T-2", Title: "web button",
			Column: domain.TaskColumnTodo,
		},
		EventType: domain.BoardEventTaskCreated,
	})

	s.Require().NoError(err)
	s.Len(s.events.events, 1, "the board event is still recorded — history stays complete")
	s.Empty(s.runs.runs, "no agent run may be created for a task whose blocker is open")
	s.Empty(s.runner.jobs)
	s.Equal([]uuid.UUID{taskID}, parker.parked)
}

func (s *DispatcherSuite) TestUnblockedTaskIsDispatchedNormally() {
	assignee := uuid.New()
	parker := &dispatchParker{}
	s.disp.SetWorkOrder(board.NewWorkOrder(&dispatchBlockerReader{}, parker))

	taskID := uuid.New()
	repositoryID := uuid.New()
	err := s.disp.Dispatch(context.Background(), board.DispatchInput{
		RepositoryID: repositoryID,
		Task: domain.BoardTask{
			ID: taskID, RepositoryID: repositoryID, Column: domain.TaskColumnTodo, AssigneeAgentID: &assignee,
		},
		EventType: domain.BoardEventTaskCreated,
	})

	s.Require().NoError(err)
	s.Len(s.runs.runs, 1)
	s.Len(s.runner.jobs, 1)
	s.Empty(parker.parked)
}

func (s *DispatcherSuite) TestUnreadableWorkOrderStopsTheDispatch() {
	parker := &dispatchParker{}
	s.disp.SetWorkOrder(board.NewWorkOrder(&dispatchBlockerReader{err: assertErr}, parker))

	repositoryID := uuid.New()
	err := s.disp.Dispatch(context.Background(), board.DispatchInput{
		RepositoryID: repositoryID,
		Task: domain.BoardTask{
			ID: uuid.New(), RepositoryID: repositoryID, Column: domain.TaskColumnTodo,
		},
		EventType: domain.BoardEventTaskCreated,
	})

	s.Require().Error(err)
	s.Empty(s.runs.runs)
	s.Empty(parker.parked, "an unreadable order is not a known block — parking it would state something nobody checked")
}

type reentrantCommenter struct {
	disp     *board.Dispatcher
	task     domain.BoardTask
	comments int
}

func (r *reentrantCommenter) AddComment(ctx context.Context, repositoryID, taskID uuid.UUID, _ domain.CreateTaskCommentRequest) (domain.TaskComment, error) {
	r.comments++
	if r.comments > 10 {
		return domain.TaskComment{}, errTest("runaway recursion")
	}
	r.task.BlockedResource = domain.ResourceWorkOrder
	if err := r.disp.Dispatch(ctx, board.DispatchInput{
		RepositoryID: repositoryID,
		Task:         r.task,
		EventType:    domain.BoardEventTaskCommented,
	}); err != nil {
		return domain.TaskComment{}, err
	}
	return domain.TaskComment{}, nil
}

func (s *DispatcherSuite) TestParkCommentReenteringDispatchDoesNotRecurse() {
	parker := &dispatchParker{}
	taskID := uuid.New()
	repositoryID := uuid.New()
	task := domain.BoardTask{
		ID: taskID, RepositoryID: repositoryID, Key: "T-2", Title: "web button",
		Column: domain.TaskColumnTodo,
	}
	commenter := &reentrantCommenter{disp: s.disp, task: task}
	workOrder := board.NewWorkOrder(&dispatchBlockerReader{
		blockers: []domain.BoardTask{{
			ID: uuid.New(), Key: "T-1", Title: "API migration", Column: domain.TaskColumnInProgress,
		}},
	}, parker)
	workOrder.SetCommenter(commenter)
	s.disp.SetWorkOrder(workOrder)

	err := s.disp.Dispatch(context.Background(), board.DispatchInput{
		RepositoryID: repositoryID,
		Task:         task,
		EventType:    domain.BoardEventTaskCreated,
	})

	s.Require().NoError(err)
	s.Equal(1, commenter.comments, "the re-entrant Dispatch call must not park (and comment) again")
	s.Equal([]uuid.UUID{taskID}, parker.parked, "parked exactly once")
}

func (s *DispatcherSuite) TestDispatchWithoutAWorkOrderGateIsUnchanged() {
	repositoryID := uuid.New()
	assignee := uuid.New()
	err := s.disp.Dispatch(context.Background(), board.DispatchInput{
		RepositoryID: repositoryID,
		Task: domain.BoardTask{
			ID: uuid.New(), RepositoryID: repositoryID, Column: domain.TaskColumnTodo, AssigneeAgentID: &assignee,
		},
		EventType: domain.BoardEventTaskCreated,
	})
	s.Require().NoError(err)
	s.Len(s.runs.runs, 1)
}

var assertErr = errTest("relation store unavailable")

type errTest string

func (e errTest) Error() string { return string(e) }

func TestBlocksRelationDirectionIsSourceFirst(t *testing.T) {
	blocker := uuid.New()
	blocked := uuid.New()
	rel := domain.TaskRelation{
		SourceTaskID: blocker,
		TargetTaskID: blocked,
		RelationType: domain.TaskRelationBlocks,
	}
	require.True(t, domain.ValidTaskRelationType(rel.RelationType))
	assert.Equal(t, blocker, rel.SourceTaskID, "the blocker is the source; ListBlockingSources selects it by the target")
}
