package localindex

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

var errNoRows = errors.New("no rows in result set")

type rowStore struct {
	port.LocalIndexStore
	base *domain.WorkspaceIndex
}

func (s *rowStore) GetIndexByProject(context.Context, uuid.UUID) (domain.WorkspaceIndex, error) {
	if s.base == nil {
		return domain.WorkspaceIndex{}, errNoRows
	}
	return *s.base, nil
}

func (s *rowStore) GetIndexByProjectBranch(context.Context, uuid.UUID, string) (domain.WorkspaceIndex, error) {
	return domain.WorkspaceIndex{}, errNoRows
}

type rowOpener struct{ store port.LocalIndexStore }

func (o rowOpener) Open(context.Context) (port.LocalIndexStore, error) { return o.store, nil }

type StatusSuite struct {
	suite.Suite
	store *rowStore
	svc   *Service
	ref   port.LocalIndexRef
}

func TestStatusSuite(t *testing.T) {
	suite.Run(t, new(StatusSuite))
}

func (s *StatusSuite) SetupTest() {
	s.store = &rowStore{}
	s.svc = NewService(Deps{Opener: rowOpener{store: s.store}})
	s.ref = port.LocalIndexRef{RepoKey: "repo-1"}
}

func (s *StatusSuite) TestARowLeftRunningByNoPassReadsAsAnInterruptedPass() {
	indexedAt := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	s.store.base = &domain.WorkspaceIndex{
		Status: domain.IndexStatusRunning, FileCount: 40, ChunkCount: 90, FilesProcessed: 40, FilesTotal: 212, IndexedAt: &indexedAt,
	}

	st, err := s.svc.Status(context.Background(), s.ref)

	s.Require().NoError(err)
	s.True(st.Found)
	s.False(st.Running)
	s.Equal(domain.IndexStatusFailed, st.Status)
	s.Equal(interruptedPass, st.Error)
	s.Equal(40, st.FilesProcessed)
	s.Equal(212, st.FilesTotal)
	s.Equal(&indexedAt, st.IndexedAt)
}

func (s *StatusSuite) TestAPassInProgressReadsAsRunningWithItsOwnProgress() {
	s.store.base = &domain.WorkspaceIndex{Status: domain.IndexStatusCompleted, FileCount: 200, Error: "an old failure"}
	pass := s.svc.passes.begin(passKey(s.ref))
	pass.report(port.LocalIndexProgress{Phase: port.LocalIndexPhaseIndexing, FilesProcessed: 12, FilesTotal: 30})

	st, err := s.svc.Status(context.Background(), s.ref)

	s.Require().NoError(err)
	s.True(st.Running)
	s.Equal(domain.IndexStatusRunning, st.Status)
	s.Equal(port.LocalIndexPhaseIndexing, st.Phase)
	s.Equal(12, st.FilesProcessed)
	s.Equal(30, st.FilesTotal)
	s.Empty(st.Error)

	pass.end(nil)
	after, err := s.svc.Status(context.Background(), s.ref)
	s.Require().NoError(err)
	s.False(after.Running)
	s.Equal(domain.IndexStatusCompleted, after.Status)
}

func (s *StatusSuite) TestAPassThatFailedBeforeReachingTheIndexSaysWhy() {
	pass := s.svc.passes.begin(passKey(s.ref))
	pass.end(errors.New("the embedding engine failed: connection refused"))

	st, err := s.svc.Status(context.Background(), s.ref)

	s.Require().NoError(err)
	s.False(st.Found)
	s.False(st.Running)
	s.Equal("the embedding engine failed: connection refused", st.Error)

	again := s.svc.passes.begin(passKey(s.ref))
	again.end(nil)
	cleared, err := s.svc.Status(context.Background(), s.ref)
	s.Require().NoError(err)
	s.Empty(cleared.Error, "a pass that succeeds clears the last failure")
}

func (s *StatusSuite) TestTwoPassesOverOneIndexEndOnlyWhenBothHaveEnded() {
	first := s.svc.passes.begin(passKey(s.ref))
	second := s.svc.passes.begin(passKey(s.ref))
	first.end(nil)

	st, err := s.svc.Status(context.Background(), s.ref)
	s.Require().NoError(err)
	s.True(st.Running)

	second.end(nil)
	st, err = s.svc.Status(context.Background(), s.ref)
	s.Require().NoError(err)
	s.False(st.Running)
}

func (s *StatusSuite) TestAComputerWithNoPlaceForTheStoreCannotSay() {
	_, err := NewService(Deps{}).Status(context.Background(), s.ref)

	s.ErrorIs(err, port.ErrLocalIndexUnavailable)
}
