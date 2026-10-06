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

type EnvRequirementSuite struct {
	suite.Suite
	ctx        context.Context
	cancel     context.CancelFunc
	pg         *database.Embedded
	pool       *pgxpool.Pool
	store      *postgres.EnvRequirementStore
	repoID     uuid.UUID
	components *postgres.ProjectModelStore
}

func TestEnvRequirementSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(EnvRequirementSuite))
}

func (s *EnvRequirementSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 3*time.Minute)
	pg, err := newTestDatabase(s.ctx)
	s.Require().NoError(err)
	s.pg = pg
	pool, err := pgxpool.New(s.ctx, pg.DSN())
	s.Require().NoError(err)
	s.pool = pool
	db := postgres.NewDB(pool)
	s.store = postgres.NewEnvRequirementStore(db)
	s.components = postgres.NewProjectModelStore(db)
}

func (s *EnvRequirementSuite) SetupTest() {
	repos := postgres.NewRepositoryStore(postgres.NewDB(s.pool))
	repo, err := repos.Create(s.ctx, "env-req-"+uuid.NewString()[:8], "", "/tmp/env-req-"+uuid.New().String(), "", "")
	s.Require().NoError(err)
	s.repoID = repo.ID
}

func (s *EnvRequirementSuite) TearDownSuite() {
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

func (s *EnvRequirementSuite) upsert(r domain.EnvRequirement) domain.EnvRequirement {
	r.RepositoryID = s.repoID
	stored, err := s.store.UpsertEnvRequirement(s.ctx, r)
	s.Require().NoError(err)
	return stored
}

func (s *EnvRequirementSuite) TestUpsertCreatesThenUpdatesTheSameName() {
	s.upsert(domain.EnvRequirement{Name: "SESSION_SECRET", Kind: domain.EnvKindGenerated, Description: "cookie key"})
	updated := s.upsert(domain.EnvRequirement{Name: "SESSION_SECRET", Kind: domain.EnvKindHumanSecret})

	s.Equal(domain.EnvKindHumanSecret, updated.Kind)
	s.Equal("cookie key", updated.Description, "an empty description keeps the old one")
	all, err := s.store.ListEnvRequirements(s.ctx, s.repoID)
	s.Require().NoError(err)
	s.Len(all, 1)
}

func (s *EnvRequirementSuite) TestAnAgentNeverOverwritesAHumanDecision() {
	s.upsert(domain.EnvRequirement{Name: "CONTENT_BACKEND", Kind: domain.EnvKindOptional, Source: domain.EnvSourceHuman})

	stood := s.upsert(domain.EnvRequirement{Name: "CONTENT_BACKEND", Kind: domain.EnvKindHumanSecret, Source: domain.EnvSourceAgent})

	s.Equal(domain.EnvKindOptional, stood.Kind)
	s.Equal(domain.EnvSourceHuman, stood.Source)

	human := s.upsert(domain.EnvRequirement{Name: "CONTENT_BACKEND", Kind: domain.EnvKindValue, Value: "github", Source: domain.EnvSourceHuman})
	s.Equal(domain.EnvKindValue, human.Kind)
	s.Equal("github", human.Value)
}

func (s *EnvRequirementSuite) TestTheSameNameIsSeparatePerComponent() {
	comp, err := s.components.SaveComponent(s.ctx, domain.Component{RepositoryID: s.repoID, Path: "web", Status: domain.ComponentStatusActive})
	s.Require().NoError(err)

	s.upsert(domain.EnvRequirement{Name: "API_URL", Kind: domain.EnvKindValue, Value: "https://a"})
	s.upsert(domain.EnvRequirement{Name: "API_URL", Kind: domain.EnvKindValue, Value: "https://b", ComponentID: &comp.ID})

	all, err := s.store.ListEnvRequirements(s.ctx, s.repoID)
	s.Require().NoError(err)
	s.Require().Len(all, 2)
	s.Nil(all[0].ComponentID, "the repository-wide row sorts first")
	s.Equal("https://b", all[1].Value)
}
