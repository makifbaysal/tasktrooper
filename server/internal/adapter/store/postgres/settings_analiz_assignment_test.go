package postgres_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/store/postgres"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/database"
)

// SettingsAnalizAssignmentSuite covers the backend/frontend/mobile
// analiz-assignment rows: a fresh install with none of the three keys must
// default to system-architect, and Update must persist a change so a later
// Get reflects it.
type SettingsAnalizAssignmentSuite struct {
	suite.Suite
	ctx    context.Context
	cancel context.CancelFunc
	pg     *database.Embedded
	pool   *pgxpool.Pool
	store  *postgres.SettingsStore
}

func TestSettingsAnalizAssignmentSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(SettingsAnalizAssignmentSuite))
}

func (s *SettingsAnalizAssignmentSuite) SetupSuite() {
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
	s.store = postgres.NewSettingsStore(postgres.NewDB(pool), "", "", false)
}

func (s *SettingsAnalizAssignmentSuite) TearDownSuite() {
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

func (s *SettingsAnalizAssignmentSuite) TestGetDefaultsAllThreeAreasToSystemArchitectOnAFreshInstall() {
	got, err := s.store.Get(s.ctx)
	s.Require().NoError(err)

	s.Equal(domain.AgentSystemArchitect, got.AnalizAssigneeBackend)
	s.Equal(domain.AgentSystemArchitect, got.AnalizAssigneeFrontend)
	s.Equal(domain.AgentSystemArchitect, got.AnalizAssigneeMobile)
}

func (s *SettingsAnalizAssignmentSuite) TestUpdateAnalizAssignmentPersistsAndIsReadBackByGet() {
	saved, err := s.store.UpdateAnalizAssignment(s.ctx, domain.AgentBackendDeveloper, "", "")
	s.Require().NoError(err)
	s.Equal(domain.AgentBackendDeveloper, saved.AnalizAssigneeBackend)

	got, err := s.store.Get(s.ctx)
	s.Require().NoError(err)
	s.Equal(domain.AgentBackendDeveloper, got.AnalizAssigneeBackend)
	s.Equal(domain.AgentSystemArchitect, got.AnalizAssigneeFrontend, "an empty area is left untouched")

	reset, err := s.store.UpdateAnalizAssignment(s.ctx, "-", "", "")
	s.Require().NoError(err)
	s.Equal(domain.AgentSystemArchitect, reset.AnalizAssigneeBackend, "\"-\" resets the area back to the default")
}
