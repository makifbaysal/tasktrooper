package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/storage/postgres"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/database"
)

type MCPTemplateSeedsSuite struct {
	suite.Suite
	ctx    context.Context
	cancel context.CancelFunc
	pg     *database.Embedded
	pool   *pgxpool.Pool
	store  *postgres.MCPStore
}

func TestMCPTemplateSeedsSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(MCPTemplateSeedsSuite))
}

func (s *MCPTemplateSeedsSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 3*time.Minute)
	pg, err := newTestDatabase(s.ctx)
	s.Require().NoError(err)
	s.pg = pg
	s.pool, err = pgxpool.New(s.ctx, pg.DSN())
	s.Require().NoError(err)
	s.store = postgres.NewMCPStore(postgres.NewDB(s.pool))
}

func (s *MCPTemplateSeedsSuite) TearDownSuite() {
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

func (s *MCPTemplateSeedsSuite) TestMarkingIsIdempotentAndOutlivesTheServer() {
	s.Require().NoError(s.store.MarkTemplatesSeeded(s.ctx, []string{"unity", "godot"}))
	s.Require().NoError(s.store.MarkTemplatesSeeded(s.ctx, []string{"godot", "blender"}))
	s.Require().NoError(s.store.MarkTemplatesSeeded(s.ctx, nil))

	ids, err := s.store.SeededTemplateIDs(s.ctx)
	s.Require().NoError(err)
	s.Equal([]string{"blender", "godot", "unity"}, ids)
}
