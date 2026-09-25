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

// Fixture rows go in through raw SQL rather than the old RepoDependencyStore /
// RepositoryProfileStore adapters: those tables (migrations 086, 134) stay for
// LegacyModelSource to read, but the stores that used to own them are gone
// along with the profile/dependency feature they backed.
type LegacyModelSourceSuite struct {
	suite.Suite
	ctx    context.Context
	cancel context.CancelFunc
	pg     *database.Embedded
	pool   *pgxpool.Pool
	source *postgres.LegacyModelSource
	repos  *postgres.RepositoryStore
	repoA  uuid.UUID
	repoB  uuid.UUID
}

func TestLegacyModelSourceSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(LegacyModelSourceSuite))
}

func (s *LegacyModelSourceSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 3*time.Minute)
	pg, err := newTestDatabase(s.ctx)
	s.Require().NoError(err)
	s.pg = pg
	pool, err := pgxpool.New(s.ctx, pg.DSN())
	s.Require().NoError(err)
	s.pool = pool
	db := postgres.NewDB(pool)
	s.source = postgres.NewLegacyModelSource(db)
	s.repos = postgres.NewRepositoryStore(db)
}

func (s *LegacyModelSourceSuite) SetupTest() {
	suffix := uuid.NewString()
	repoA, err := s.repos.Create(s.ctx, "legacy-a-"+suffix, "", "/tmp/legacy-a-"+suffix, "", "")
	s.Require().NoError(err)
	repoB, err := s.repos.Create(s.ctx, "legacy-b-"+suffix, "", "/tmp/legacy-b-"+suffix, "", "")
	s.Require().NoError(err)
	s.repoA = repoA.ID
	s.repoB = repoB.ID
}

func (s *LegacyModelSourceSuite) TearDownSuite() {
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

func (s *LegacyModelSourceSuite) insertDependency(repositoryID uuid.UUID, targetKind string, targetRepositoryID *uuid.UUID, databaseLabel, databaseEngine, databaseHost string, databasePort int, databaseName, note string) {
	_, err := s.pool.Exec(s.ctx, `
		INSERT INTO repository_dependencies (
			repository_id, target_kind, target_repository_id,
			database_label, database_engine, database_host, database_port, database_name, note
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, repositoryID, targetKind, targetRepositoryID, databaseLabel, databaseEngine, databaseHost, databasePort, databaseName, note)
	s.Require().NoError(err)
}

func (s *LegacyModelSourceSuite) TestListLegacyDependenciesReadsEveryTargetKind() {
	s.insertDependency(s.repoA, "repo", &s.repoB, "", "", "", 0, "", "calls the public API")
	s.insertDependency(s.repoA, "database", nil, "Primary DB", "postgres", "db.internal", 5432, "app", "")

	got, err := s.source.ListLegacyDependencies(s.ctx, s.repoA)
	s.Require().NoError(err)
	s.Require().Len(got, 2)

	byKind := map[string]domain.LegacyDependency{}
	for _, d := range got {
		byKind[d.TargetKind] = d
	}

	repoDep := byKind["repo"]
	s.Equal(s.repoA, repoDep.RepositoryID)
	s.Require().NotNil(repoDep.TargetRepositoryID)
	s.Equal(s.repoB, *repoDep.TargetRepositoryID)
	s.Equal("calls the public API", repoDep.Note)

	dbDep := byKind["database"]
	s.Equal("Primary DB", dbDep.DatabaseLabel)
	s.Equal("postgres", dbDep.DatabaseEngine)
	s.Equal("db.internal", dbDep.DatabaseHost)
	s.Equal(5432, dbDep.DatabasePort)
	s.Equal("app", dbDep.DatabaseName)
}

func (s *LegacyModelSourceSuite) TestListLegacyDependenciesIsScopedToItsRepository() {
	s.insertDependency(s.repoB, "database", nil, "Other repo's DB", "", "", 0, "", "")

	got, err := s.source.ListLegacyDependencies(s.ctx, s.repoA)
	s.Require().NoError(err)
	s.Empty(got)
}
