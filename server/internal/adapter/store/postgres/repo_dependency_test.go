package postgres_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/store/postgres"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/domain/secrets"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/database"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type RepoDependencyStoreSuite struct {
	suite.Suite
	ctx    context.Context
	cancel context.CancelFunc
	pg     *database.Embedded
	pool   *pgxpool.Pool
	store  *postgres.RepoDependencyStore
	repos  *postgres.RepositoryStore
	repoA  uuid.UUID
	repoB  uuid.UUID
	repoC  uuid.UUID
}

func TestRepoDependencyStoreSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(RepoDependencyStoreSuite))
}

func (s *RepoDependencyStoreSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 3*time.Minute)
	tmp := s.T().TempDir()
	pg, err := database.StartEmbedded(s.ctx, database.EmbeddedConfig{
		DataDir:     filepath.Join(tmp, "postgres"),
		RuntimePath: filepath.Join(tmp, "runtime"),
	})
	s.Require().NoError(err)
	s.pg = pg
	pool, err := pgxpool.New(s.ctx, pg.DSN())
	s.Require().NoError(err)
	s.pool = pool
	db := postgres.NewDB(pool)
	s.store = postgres.NewRepoDependencyStore(db)
	cipher, err := secrets.NewCipher(make([]byte, 32))
	s.Require().NoError(err)
	s.store.SetCipher(cipher, nil)
	s.repos = postgres.NewRepositoryStore(db)
}

func (s *RepoDependencyStoreSuite) SetupTest() {
	suffix := uuid.NewString()
	repoA, err := s.repos.Create(s.ctx, "repo-a-"+suffix, "", "/tmp/repo-a-"+suffix, "", "")
	s.Require().NoError(err)
	repoB, err := s.repos.Create(s.ctx, "repo-b-"+suffix, "", "/tmp/repo-b-"+suffix, "", "")
	s.Require().NoError(err)
	repoC, err := s.repos.Create(s.ctx, "repo-c-"+suffix, "", "/tmp/repo-c-"+suffix, "", "")
	s.Require().NoError(err)
	s.repoA = repoA.ID
	s.repoB = repoB.ID
	s.repoC = repoC.ID
}

func (s *RepoDependencyStoreSuite) TearDownSuite() {
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

func (s *RepoDependencyStoreSuite) TestCreateGetRoundtripsARepoTarget() {
	created, err := s.store.Create(s.ctx, s.repoA, domain.SaveRepoDependencyRequest{
		TargetKind:         domain.DependencyTargetRepo,
		TargetRepositoryID: &s.repoB,
		Note:               "calls the public API",
	})
	s.Require().NoError(err)
	s.Equal(domain.DependencyTargetRepo, created.TargetKind)
	s.Equal(s.repoB, *created.TargetRepositoryID)

	got, err := s.store.Get(s.ctx, created.ID)
	s.Require().NoError(err)
	s.Equal(created, got)
}

func (s *RepoDependencyStoreSuite) TestCreateGetRoundtripsASubRepoTarget() {
	created, err := s.store.Create(s.ctx, s.repoA, domain.SaveRepoDependencyRequest{
		TargetKind:           domain.DependencyTargetSubRepo,
		TargetRepositoryID:   &s.repoB,
		TargetSubProjectPath: "apps/api",
	})
	s.Require().NoError(err)

	got, err := s.store.Get(s.ctx, created.ID)
	s.Require().NoError(err)
	s.Equal("apps/api", got.TargetSubProjectPath)
}

func (s *RepoDependencyStoreSuite) TestCreateGetRoundtripsADatabaseTargetAndMasksTheSecret() {
	created, err := s.store.Create(s.ctx, s.repoA, domain.SaveRepoDependencyRequest{
		TargetKind:     domain.DependencyTargetDatabase,
		DatabaseLabel:  "Prod Postgres",
		DatabaseEngine: domain.DatabaseEnginePostgres,
		DatabaseEnv:    domain.DeployEnvProd,
		DatabaseHost:   "db.internal",
		DatabasePort:   5432,
		DatabaseSecret: "super-secret-password",
	})
	s.Require().NoError(err)
	s.Equal(secrets.MaskedValue(), created.DatabaseSecret, "the real secret must never round-trip")

	got, err := s.store.Get(s.ctx, created.ID)
	s.Require().NoError(err)
	s.Equal(secrets.MaskedValue(), got.DatabaseSecret)
	s.Equal("Prod Postgres", got.DatabaseLabel)
}

func (s *RepoDependencyStoreSuite) TestUpdateWithAnEmptySecretKeepsTheStoredSecret() {
	created, err := s.store.Create(s.ctx, s.repoA, domain.SaveRepoDependencyRequest{
		TargetKind:     domain.DependencyTargetDatabase,
		DatabaseLabel:  "Cache",
		DatabaseSecret: "the-real-password",
	})
	s.Require().NoError(err)

	updated, err := s.store.Update(s.ctx, created.ID, domain.SaveRepoDependencyRequest{
		TargetKind:    domain.DependencyTargetDatabase,
		DatabaseLabel: "Cache (renamed)",
	})
	s.Require().NoError(err)
	s.Equal("Cache (renamed)", updated.DatabaseLabel)
	s.Equal(secrets.MaskedValue(), updated.DatabaseSecret, "an empty secret on update must not erase the stored one")

	got, err := s.store.Get(s.ctx, created.ID)
	s.Require().NoError(err)
	s.Equal(secrets.MaskedValue(), got.DatabaseSecret)
	s.Equal("Cache (renamed)", got.DatabaseLabel)
}

func (s *RepoDependencyStoreSuite) TestDeleteRemovesTheRow() {
	created, err := s.store.Create(s.ctx, s.repoA, domain.SaveRepoDependencyRequest{
		TargetKind:         domain.DependencyTargetRepo,
		TargetRepositoryID: &s.repoB,
	})
	s.Require().NoError(err)

	s.Require().NoError(s.store.Delete(s.ctx, created.ID))

	_, err = s.store.Get(s.ctx, created.ID)
	s.Require().ErrorIs(err, port.ErrNotFound)

	list, err := s.store.ListByRepository(s.ctx, s.repoA)
	s.Require().NoError(err)
	s.Empty(list)
}

func (s *RepoDependencyStoreSuite) TestListByProjectSplitsOutgoingAndIncoming() {
	projects := postgres.NewInitiativeProjectStore(postgres.NewDB(s.pool))
	projA, err := projects.Create(s.ctx, "project-a-"+uuid.NewString(), "")
	s.Require().NoError(err)
	projB, err := projects.Create(s.ctx, "project-b-"+uuid.NewString(), "")
	s.Require().NoError(err)

	s.Require().NoError(s.repos.SetProjects(s.ctx, s.repoA, []uuid.UUID{projA.ID}))
	s.Require().NoError(s.repos.SetProjects(s.ctx, s.repoB, []uuid.UUID{projB.ID}))

	_, err = s.store.Create(s.ctx, s.repoA, domain.SaveRepoDependencyRequest{
		TargetKind:         domain.DependencyTargetRepo,
		TargetRepositoryID: &s.repoB,
	})
	s.Require().NoError(err)

	outgoing, incoming, err := s.store.ListByProject(s.ctx, projA.ID)
	s.Require().NoError(err)
	s.Require().Len(outgoing, 1)
	s.Equal(s.repoA, outgoing[0].RepositoryID)
	s.Empty(incoming)

	outgoingB, incomingB, err := s.store.ListByProject(s.ctx, projB.ID)
	s.Require().NoError(err)
	s.Empty(outgoingB)
	s.Require().Len(incomingB, 1)
	s.Equal(s.repoA, incomingB[0].RepositoryID)
}
