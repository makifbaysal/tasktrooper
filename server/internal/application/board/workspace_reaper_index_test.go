package board

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type fakeBranchIndexes struct {
	indexes   []domain.WorkspaceIndex
	listErr   error
	failOn    map[uuid.UUID]bool
	deleted   []uuid.UUID
	attempted int
}

func (f *fakeBranchIndexes) ListBranchIndexes(context.Context) ([]domain.WorkspaceIndex, error) {
	return f.indexes, f.listErr
}

func (f *fakeBranchIndexes) DeleteIndex(_ context.Context, id uuid.UUID) error {
	f.attempted++
	if f.failOn[id] {
		return errors.New("database unavailable")
	}
	f.deleted = append(f.deleted, id)
	return nil
}

type ReaperBranchIndexSuite struct {
	suite.Suite
	root    string
	indexes *fakeBranchIndexes
	reaper  *WorkspaceReaper
}

func TestReaperBranchIndexSuite(t *testing.T) {
	suite.Run(t, new(ReaperBranchIndexSuite))
}

func (s *ReaperBranchIndexSuite) SetupTest() {
	s.root = s.T().TempDir()
	s.indexes = &fakeBranchIndexes{failOn: map[uuid.UUID]bool{}}
	s.reaper = NewWorkspaceReaper(fakeTaskLister{}, fakeActive{}, s.root, time.Hour)
	s.reaper.SetBranchIndexes(s.indexes)
}

func (s *ReaperBranchIndexSuite) branchIndex(rootPath string) domain.WorkspaceIndex {
	return domain.WorkspaceIndex{ID: uuid.New(), Branch: "t-1", RootPath: rootPath}
}

func (s *ReaperBranchIndexSuite) TestIndexOfAReapedWorkspaceIsDeleted() {
	finished := uuid.New()
	finishedDir := mkTaskDir(s.T(), s.root, finished, 10*time.Hour)
	s.reaper.tasks = fakeTaskLister{tasks: []domain.BoardTask{
		{ID: finished, Column: domain.TaskColumnReleased, UpdatedAt: time.Now().Add(-10 * time.Hour)},
	}}
	idx := s.branchIndex(finishedDir)
	s.indexes.indexes = []domain.WorkspaceIndex{idx}

	s.reaper.Sweep(context.Background())

	s.False(exists(finishedDir))
	s.Equal([]uuid.UUID{idx.ID}, s.indexes.deleted)
}

func (s *ReaperBranchIndexSuite) TestIndexOfALiveWorkspaceIsKept() {
	working := uuid.New()
	workingDir := mkTaskDir(s.T(), s.root, working, 10*time.Hour)
	s.reaper.tasks = fakeTaskLister{tasks: []domain.BoardTask{
		{ID: working, Column: domain.TaskColumnInProgress, UpdatedAt: time.Now().Add(-10 * time.Hour)},
	}}
	s.indexes.indexes = []domain.WorkspaceIndex{s.branchIndex(workingDir)}

	s.reaper.Sweep(context.Background())

	s.True(exists(workingDir))
	s.Empty(s.indexes.deleted)
}

func (s *ReaperBranchIndexSuite) TestLeakedIndexWithoutAWorkspaceIsDeleted() {
	leaked := s.branchIndex(filepath.Join(s.root, "task-"+uuid.NewString()))
	s.indexes.indexes = []domain.WorkspaceIndex{leaked}

	s.reaper.Sweep(context.Background())

	s.Equal([]uuid.UUID{leaked.ID}, s.indexes.deleted)
}

func (s *ReaperBranchIndexSuite) TestIndexNotAnchoredOnATaskWorkspaceIsLeftAlone() {
	s.indexes.indexes = []domain.WorkspaceIndex{
		s.branchIndex(filepath.Join(s.root, "repos", "acme-web")),
		s.branchIndex(filepath.Join(s.root, "task-not-a-uuid")),
	}

	s.reaper.Sweep(context.Background())

	s.Zero(s.indexes.attempted)
}

func (s *ReaperBranchIndexSuite) TestOneFailedDeleteDoesNotStopTheRest() {
	first := s.branchIndex(filepath.Join(s.root, "task-"+uuid.NewString()))
	second := s.branchIndex(filepath.Join(s.root, "task-"+uuid.NewString()))
	s.indexes.failOn[first.ID] = true
	s.indexes.indexes = []domain.WorkspaceIndex{first, second}

	s.reaper.Sweep(context.Background())

	s.Equal(2, s.indexes.attempted)
	s.Equal([]uuid.UUID{second.ID}, s.indexes.deleted)
}

func (s *ReaperBranchIndexSuite) TestListFailureDeletesNothing() {
	s.indexes.listErr = errors.New("database unavailable")

	s.reaper.Sweep(context.Background())

	s.Zero(s.indexes.attempted)
}
