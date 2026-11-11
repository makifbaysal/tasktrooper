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
)

type ReadinessStoreSuite struct {
	suite.Suite
	ctx    context.Context
	cancel context.CancelFunc
	pg     *database.Embedded
	pool   *pgxpool.Pool
	store  *postgres.ReadinessStore
	repos  *postgres.RepositoryStore
	repoID uuid.UUID
}

func TestReadinessStoreSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(ReadinessStoreSuite))
}

func (s *ReadinessStoreSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 3*time.Minute)
	pg, err := newTestDatabase(s.ctx)
	s.Require().NoError(err)
	s.pg = pg
	pool, err := pgxpool.New(s.ctx, pg.DSN())
	s.Require().NoError(err)
	s.pool = pool
	db := postgres.NewDB(pool)
	s.store = postgres.NewReadinessStore(db)
	s.repos = postgres.NewRepositoryStore(db)
}

func (s *ReadinessStoreSuite) SetupTest() {
	name := "readiness-" + uuid.NewString()
	repo, err := s.repos.Create(s.ctx, name, "", "/tmp/"+name, "", "")
	s.Require().NoError(err)
	s.repoID = repo.ID
}

func (s *ReadinessStoreSuite) TearDownSuite() {
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

func (s *ReadinessStoreSuite) TestASkipIsOneRowPerStepAndCanBeUndone() {
	s.Require().NoError(s.store.SetSkipped(s.ctx, s.repoID, "guide", true))
	s.Require().NoError(s.store.SetSkipped(s.ctx, s.repoID, domain.ReadinessCardKey, true))
	list, err := s.store.List(s.ctx, s.repoID)
	s.Require().NoError(err)
	s.Len(list, 2)

	s.Require().NoError(s.store.SetSkipped(s.ctx, s.repoID, "guide", false))
	list, err = s.store.List(s.ctx, s.repoID)
	s.Require().NoError(err)
	s.Len(list, 2)
	for _, st := range list {
		s.Equal(st.StepID == domain.ReadinessCardKey, st.Skipped, st.StepID)
	}
}

func (s *ReadinessStoreSuite) TestARepositoryWithNoChoicesListsNothing() {
	list, err := s.store.List(s.ctx, s.repoID)
	s.Require().NoError(err)
	s.Empty(list)
}

func (s *ReadinessStoreSuite) TestATaskThatIsNotOnTheBoardIsRefused() {
	s.Error(s.store.SetTask(s.ctx, s.repoID, "tests", uuid.New()))
}
