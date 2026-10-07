package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/storage/postgres"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/database"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type ProjectModelBatchReadSuite struct {
	suite.Suite
	ctx      context.Context
	cancel   context.CancelFunc
	pg       *database.Embedded
	pool     *pgxpool.Pool
	store    *postgres.ProjectModelStore
	repos    *postgres.RepositoryStore
	repoA    uuid.UUID
	repoB    uuid.UUID
	repoC    uuid.UUID
	outsider uuid.UUID
	newestB  domain.ProjectScan
}

func TestProjectModelBatchReadSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(ProjectModelBatchReadSuite))
}

// SetupSuite seeds every repository once: the tests only read. Checks are
// inserted out of path/workflow/job order so the batch read has to sort them
// rather than inherit insertion order; repoC has checks but no scan.
func (s *ProjectModelBatchReadSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 3*time.Minute)
	pg, err := newTestDatabase(s.ctx)
	s.Require().NoError(err)
	s.pg = pg
	pool, err := pgxpool.New(s.ctx, pg.DSN())
	s.Require().NoError(err)
	s.pool = pool
	db := postgres.NewDB(pool)
	s.store = postgres.NewProjectModelStore(db)
	s.repos = postgres.NewRepositoryStore(db)

	s.repoA = s.newRepository("batch-a")
	s.repoB = s.newRepository("batch-b")
	s.repoC = s.newRepository("batch-c")
	s.outsider = s.newRepository("batch-outsider")

	servicesB := s.newComponent(s.repoA, "services/b")
	appsA := s.newComponent(s.repoA, "apps/a")
	s.newCheck(s.repoA, servicesB, "ci.yml", "test")
	s.newCheck(s.repoA, appsA, "lint.yml", "lint")
	s.newCheck(s.repoA, appsA, "ci.yml", "test")
	s.newCheck(s.repoA, appsA, "ci.yml", "build")

	rootB := s.newComponent(s.repoB, ".")
	s.newCheck(s.repoB, rootB, "ci.yml", "z-last")
	s.newCheck(s.repoB, rootB, "ci.yml", "a-first")

	s.newCheck(s.repoC, s.newComponent(s.repoC, "."), "ci.yml", "test")
	s.newCheck(s.outsider, s.newComponent(s.outsider, "."), "ci.yml", "test")

	base := time.Now().Add(-time.Hour).UTC()
	s.newScan(s.repoA, base, nil)
	s.newScan(s.repoB, base, nil)
	s.newestB = s.newScan(s.repoB, base.Add(10*time.Minute), &domain.ScanResult{Shape: domain.RepoShapeSingle, FileCount: 3})
	s.newScan(s.repoB, base.Add(5*time.Minute), nil)
	s.newScan(s.outsider, base.Add(20*time.Minute), nil)
}

func (s *ProjectModelBatchReadSuite) TearDownSuite() {
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

func (s *ProjectModelBatchReadSuite) newRepository(name string) uuid.UUID {
	suffix := uuid.NewString()
	repo, err := s.repos.Create(s.ctx, name+"-"+suffix, "", "/tmp/"+name+"-"+suffix, "", "")
	s.Require().NoError(err)
	return repo.ID
}

func (s *ProjectModelBatchReadSuite) newComponent(repositoryID uuid.UUID, path string) uuid.UUID {
	c, err := s.store.SaveComponent(s.ctx, domain.Component{RepositoryID: repositoryID, Path: path})
	s.Require().NoError(err)
	return c.ID
}

func (s *ProjectModelBatchReadSuite) newCheck(repositoryID, componentID uuid.UUID, workflow, jobKey string) {
	_, err := s.store.SaveCheck(s.ctx, domain.ComponentCheck{
		RepositoryID: repositoryID,
		ComponentID:  componentID,
		Workflow:     workflow,
		JobKey:       jobKey,
		Gate:         domain.Detected(domain.CheckGateRequired, domain.ConfidenceHigh),
	})
	s.Require().NoError(err)
}

func (s *ProjectModelBatchReadSuite) newScan(repositoryID uuid.UUID, startedAt time.Time, result *domain.ScanResult) domain.ProjectScan {
	sc, err := s.store.CreateScan(s.ctx, domain.ProjectScan{
		RepositoryID: repositoryID,
		Trigger:      domain.ScanTriggerManual,
		Status:       domain.ScanSucceeded,
		StartedAt:    startedAt,
		Result:       result,
	})
	s.Require().NoError(err)
	return sc
}

func (s *ProjectModelBatchReadSuite) TestListChecksForRepositoriesMatchesPerRepositoryListChecks() {
	requested := []uuid.UUID{s.repoA, s.repoB, s.repoC}

	batch, err := s.store.ListChecksForRepositories(s.ctx, requested)
	s.Require().NoError(err)

	grouped := map[uuid.UUID][]domain.ComponentCheck{}
	for _, c := range batch {
		grouped[c.RepositoryID] = append(grouped[c.RepositoryID], c)
	}
	s.Len(grouped, len(requested))
	s.NotContains(grouped, s.outsider)

	for _, repoID := range requested {
		perRepo, err := s.store.ListChecks(s.ctx, repoID)
		s.Require().NoError(err)
		s.Require().NotEmpty(perRepo)
		s.Equal(perRepo, grouped[repoID], "repository %s", repoID)
	}
}

func (s *ProjectModelBatchReadSuite) TestLatestScansMatchesPerRepositoryLatestScan() {
	batch, err := s.store.LatestScans(s.ctx, []uuid.UUID{s.repoA, s.repoB, s.repoC})
	s.Require().NoError(err)
	s.Len(batch, 2)

	for _, repoID := range []uuid.UUID{s.repoA, s.repoB} {
		perRepo, err := s.store.LatestScan(s.ctx, repoID)
		s.Require().NoError(err)
		s.Require().Contains(batch, repoID)
		s.Equal(perRepo, batch[repoID], "repository %s", repoID)
		s.Nil(batch[repoID].Result, "the batch read must not load scan results")
	}
	s.Equal(s.newestB.ID, batch[s.repoB].ID)

	s.NotContains(batch, s.repoC)
	_, err = s.store.LatestScan(s.ctx, s.repoC)
	s.ErrorIs(err, port.ErrNotFound)
	s.NotContains(batch, s.outsider)
}

func (s *ProjectModelBatchReadSuite) TestBatchReadsWithNoRepositoriesReturnNothing() {
	checks, err := s.store.ListChecksForRepositories(s.ctx, nil)
	s.Require().NoError(err)
	s.Empty(checks)

	scans, err := s.store.LatestScans(s.ctx, nil)
	s.Require().NoError(err)
	s.Empty(scans)
}
