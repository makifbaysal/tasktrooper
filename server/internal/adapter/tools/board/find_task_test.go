package board

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type pointLookupTaskManager struct {
	*fakeTaskManager
	task   domain.BoardTask
	listed int
}

func (p *pointLookupTaskManager) ListTasks(context.Context, uuid.UUID) ([]domain.BoardTask, error) {
	p.listed++
	return []domain.BoardTask{p.task}, nil
}

func (p *pointLookupTaskManager) ListAllTasks(context.Context) ([]domain.BoardTask, error) {
	p.listed++
	return []domain.BoardTask{p.task}, nil
}

func (p *pointLookupTaskManager) GetTask(_ context.Context, repositoryID, taskID uuid.UUID) (domain.BoardTask, error) {
	if repositoryID != p.task.RepositoryID || taskID != p.task.ID {
		return domain.BoardTask{}, domain.ErrBoardTaskNotFound
	}
	return p.task, nil
}

type FindTaskSuite struct {
	suite.Suite
	mgr *pointLookupTaskManager
	kit *ToolKit
}

func TestFindTaskSuite(t *testing.T) {
	suite.Run(t, new(FindTaskSuite))
}

func (s *FindTaskSuite) SetupTest() {
	repoID := uuid.New()
	s.mgr = &pointLookupTaskManager{
		fakeTaskManager: &fakeTaskManager{taskRepoID: repoID},
		task:            domain.BoardTask{ID: uuid.New(), RepositoryID: repoID, Column: domain.TaskColumnTodo, TaskType: "task"},
	}
	s.kit = &ToolKit{Tasks: s.mgr}
}

func (s *FindTaskSuite) TestReadsOneTaskWithoutListingTheBoard() {
	for _, ctx := range []context.Context{
		context.Background(),
		registry.ContextWithRepositoryID(context.Background(), s.mgr.task.RepositoryID),
	} {
		task, ok := s.kit.findTask(ctx, s.mgr.task.ID)

		s.True(ok)
		s.Equal(s.mgr.task, task)
	}
	s.Zero(s.mgr.listed)
}

func (s *FindTaskSuite) TestUnknownTaskIsNotFound() {
	_, ok := s.kit.findTask(registry.ContextWithRepositoryID(context.Background(), s.mgr.task.RepositoryID), uuid.New())

	s.False(ok)
}
