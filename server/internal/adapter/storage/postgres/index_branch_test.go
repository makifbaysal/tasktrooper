package postgres_test

import (
	"context"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/storage/postgres"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/database"
)

// IndexBranchStoreSuite covers the two store methods the workspace reaper
// stands on: listing every branch index and deleting one.
type IndexBranchStoreSuite struct {
	suite.Suite
	ctx    context.Context
	cancel context.CancelFunc
	pg     *database.Embedded
	pool   *pgxpool.Pool
	db     *postgres.DB
	repos  *postgres.RepositoryStore
}

func TestIndexBranchStoreSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(IndexBranchStoreSuite))
}

func (s *IndexBranchStoreSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 3*time.Minute)
	pg, err := newTestDatabase(s.ctx)
	s.Require().NoError(err)
	s.pg = pg
	pool, err := pgxpool.New(s.ctx, pg.DSN())
	s.Require().NoError(err)
	s.pool = pool
	s.db = postgres.NewDB(pool)
	s.repos = postgres.NewRepositoryStore(s.db)
}

func (s *IndexBranchStoreSuite) TearDownSuite() {
	if s.pool != nil {
		s.pool.Close()
	}
	if s.pg != nil {
		_ = s.pg.Stop()
	}
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *IndexBranchStoreSuite) newRepository(root string) domain.Repository {
	repo, err := s.repos.Create(s.ctx, "index-branch-"+uuid.NewString(), "", root, "", "")
	s.Require().NoError(err)
	return repo
}

// listedFor returns the listed branch indexes of the given repositories, keyed
// by repository: the suite's database is shared by every test in it.
func (s *IndexBranchStoreSuite) listedFor(store *postgres.IndexStore, repoIDs ...uuid.UUID) map[uuid.UUID]domain.WorkspaceIndex {
	listed, err := store.ListBranchIndexes(s.ctx)
	s.Require().NoError(err)
	out := map[uuid.UUID]domain.WorkspaceIndex{}
	for _, idx := range listed {
		s.Require().NotNil(idx.ProjectID)
		if slices.Contains(repoIDs, *idx.ProjectID) {
			out[*idx.ProjectID] = idx
		}
	}
	return out
}

func (s *IndexBranchStoreSuite) TestListBranchIndexesReturnsBranchRowsReanchored() {
	wsRoot := filepath.Join(s.T().TempDir(), "workspaces")
	store := postgres.NewIndexStore(s.db).SetHostRoots(wsRoot, nil)

	defaultRoot := filepath.Join(wsRoot, "repos", "branch-list-"+uuid.NewString())
	defaultRepo := s.newRepository(defaultRoot)
	_, err := store.CreateProjectIndex(s.ctx, defaultRepo.ID, defaultRoot, "tree")
	s.Require().NoError(err)

	foreignRepo := s.newRepository(filepath.Join(wsRoot, "repos", "branch-list-"+uuid.NewString()))
	taskID := uuid.New()
	foreign, err := store.CreateProjectBranchIndex(s.ctx, foreignRepo.ID, "feature/foreign", "/data/workspaces/task-"+taskID.String(), "tree")
	s.Require().NoError(err)

	localRepo := s.newRepository(filepath.Join(wsRoot, "repos", "branch-list-"+uuid.NewString()))
	localRoot := filepath.Join(wsRoot, "task-"+uuid.NewString())
	local, err := store.CreateProjectBranchIndex(s.ctx, localRepo.ID, "feature/local", localRoot, "tree")
	s.Require().NoError(err)

	listed := s.listedFor(store, defaultRepo.ID, foreignRepo.ID, localRepo.ID)

	s.Len(listed, 2)
	s.NotContains(listed, defaultRepo.ID, "the default-branch index is not a branch index")
	s.Equal(foreign.ID, listed[foreignRepo.ID].ID)
	s.Equal("feature/foreign", listed[foreignRepo.ID].Branch)
	s.Equal(filepath.Join(wsRoot, "task-"+taskID.String()), listed[foreignRepo.ID].RootPath)
	s.Equal(local.ID, listed[localRepo.ID].ID)
	s.Equal("feature/local", listed[localRepo.ID].Branch)
	s.Equal(localRoot, listed[localRepo.ID].RootPath)
	for _, idx := range listed {
		s.Empty(idx.TreeText)
	}
}

func (s *IndexBranchStoreSuite) TestDeleteIndexRemovesTheRowAndItsData() {
	store := postgres.NewIndexStore(s.db)
	root := "/tmp/index-branch-" + uuid.NewString()
	repo := s.newRepository(root)
	branch, err := store.CreateProjectBranchIndex(s.ctx, repo.ID, "feature/gone", root, "")
	s.Require().NoError(err)
	otherRoot := "/tmp/index-branch-" + uuid.NewString()
	otherRepo := s.newRepository(otherRoot)
	def, err := store.CreateProjectIndex(s.ctx, otherRepo.ID, otherRoot, "")
	s.Require().NoError(err)

	embedding := []float32{1, 0, 0, 0}
	chunk := domain.WorkspaceChunk{FilePath: "a.go", SymbolName: "A", Kind: "function", Language: "go", Content: "func A() {}", Embedding: embedding}
	s.Require().NoError(store.SaveChunks(s.ctx, branch.ID, []domain.WorkspaceChunk{chunk}))
	s.Require().NoError(store.SaveChunks(s.ctx, def.ID, []domain.WorkspaceChunk{chunk}))
	s.Require().NoError(store.SaveSymbols(s.ctx, branch.ID, []domain.WorkspaceSymbol{{FilePath: "a.go", Kind: "function", Name: "A"}}))
	found, err := store.SearchChunksByIndex(s.ctx, branch.ID, embedding, 5)
	s.Require().NoError(err)
	s.Require().Len(found, 1)

	s.Require().NoError(store.DeleteIndex(s.ctx, branch.ID))

	s.Empty(s.listedFor(store, repo.ID))
	_, err = store.GetIndexByProjectBranch(s.ctx, repo.ID, "feature/gone")
	s.ErrorIs(err, pgx.ErrNoRows)
	chunks, symbols, err := store.CountIndexData(s.ctx, branch.ID)
	s.Require().NoError(err)
	s.Zero(chunks)
	s.Zero(symbols)
	after, err := store.SearchChunksByIndex(s.ctx, branch.ID, embedding, 5)
	s.Require().NoError(err)
	s.Empty(after)

	kept, err := store.GetIndexByProject(s.ctx, otherRepo.ID)
	s.Require().NoError(err)
	s.Equal(def.ID, kept.ID)
	defChunks, _, err := store.CountIndexData(s.ctx, def.ID)
	s.Require().NoError(err)
	s.Equal(1, defChunks)

	s.NoError(store.DeleteIndex(s.ctx, branch.ID), "deleting an index that is already gone is not an error")
}

func (s *IndexBranchStoreSuite) TestARepositoryHoldsItsDefaultAndBranchIndexesSideBySide() {
	store := postgres.NewIndexStore(s.db)
	root := "/tmp/index-branch-" + uuid.NewString()
	repo := s.newRepository(root)
	def, err := store.CreateProjectIndex(s.ctx, repo.ID, root, "")
	s.Require().NoError(err)

	first, err := store.CreateProjectBranchIndex(s.ctx, repo.ID, "feature/a", root+"-a", "")
	s.Require().NoError(err)
	second, err := store.CreateProjectBranchIndex(s.ctx, repo.ID, "feature/b", root+"-b", "")
	s.Require().NoError(err)

	kept, err := store.GetIndexByProject(s.ctx, repo.ID)
	s.Require().NoError(err)
	s.Equal(def.ID, kept.ID)
	gotA, err := store.GetIndexByProjectBranch(s.ctx, repo.ID, "feature/a")
	s.Require().NoError(err)
	s.Equal(first.ID, gotA.ID)
	gotB, err := store.GetIndexByProjectBranch(s.ctx, repo.ID, "feature/b")
	s.Require().NoError(err)
	s.Equal(second.ID, gotB.ID)

	again, err := store.CreateProjectBranchIndex(s.ctx, repo.ID, "feature/a", root+"-a", "")
	s.Require().NoError(err)
	s.NotEqual(first.ID, again.ID, "a branch's index is replaced, not duplicated")
	_, err = store.GetIndexByProjectBranch(s.ctx, repo.ID, "feature/b")
	s.NoError(err, "replacing one branch's index leaves the other's")
}
