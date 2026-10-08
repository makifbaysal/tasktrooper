package database_test

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/platform/database"
)

const lastMigrationBeforeMCPTemplateSeeds = "180_code_review_quorum"

// MCPTemplateSeedsMigrationSuite covers migration 181: which shipped MCP
// templates an existing install counts as already given.
type MCPTemplateSeedsMigrationSuite struct {
	suite.Suite
	ctx    context.Context
	cancel context.CancelFunc
	pg     *database.Embedded
	boot   *pgxpool.Pool
	seq    atomic.Uint64
}

func TestMCPTemplateSeedsMigrationSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(MCPTemplateSeedsMigrationSuite))
}

func (s *MCPTemplateSeedsMigrationSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 5*time.Minute)
	tmp := s.T().TempDir()
	pg, err := database.StartEmbedded(s.ctx, database.EmbeddedConfig{DataDir: filepath.Join(tmp, "postgres")})
	s.Require().NoError(err)
	s.pg = pg
	boot, err := pgxpool.New(s.ctx, pg.DSN())
	s.Require().NoError(err)
	s.boot = boot
}

func (s *MCPTemplateSeedsMigrationSuite) TearDownSuite() {
	if s.boot != nil {
		s.boot.Close()
	}
	if s.pg != nil {
		_ = s.pg.Stop()
	}
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *MCPTemplateSeedsMigrationSuite) freshDatabase() *pgxpool.Pool {
	name := fmt.Sprintf("mcp_template_seeds_%d", s.seq.Add(1))
	_, err := s.boot.Exec(s.ctx, `CREATE DATABASE `+name)
	s.Require().NoError(err)
	s.T().Cleanup(func() {
		_, _ = s.boot.Exec(context.Background(), `DROP DATABASE IF EXISTS `+name+` WITH (FORCE)`)
	})
	u, err := url.Parse(s.pg.DSN())
	s.Require().NoError(err)
	u.Path = "/" + name
	pool, err := pgxpool.New(s.ctx, u.String())
	s.Require().NoError(err)
	s.T().Cleanup(pool.Close)
	return pool
}

func (s *MCPTemplateSeedsMigrationSuite) seeded(pool *pgxpool.Pool) []string {
	rows, err := pool.Query(s.ctx, `SELECT template_id FROM mcp_template_seeds ORDER BY template_id`)
	s.Require().NoError(err)
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	s.Require().NoError(err)
	return ids
}

// An install from before this migration: its first launch seeded the original
// catalog, then the user deleted slack and added a server of their own. Only
// templates it never had — gitlab, the engine servers — are left to seed.
func (s *MCPTemplateSeedsMigrationSuite) TestAnInstallWithServersCountsWhatItHadAndWhatItDeleted() {
	pool := s.freshDatabase()
	s.Require().NoError(database.RunMigrationsUpTo(s.ctx, pool, lastMigrationBeforeMCPTemplateSeeds))
	for _, id := range []string{"filesystem", "git", "github", "postgres", "huggingface", "browser", "my-tools"} {
		_, err := pool.Exec(s.ctx, `INSERT INTO mcp_servers (id, transport, command) VALUES ($1, 'stdio', 'npx')`, id)
		s.Require().NoError(err)
	}

	s.Require().NoError(database.RunMigrations(s.ctx, pool))
	s.Require().NoError(database.RunMigrations(s.ctx, pool), "re-running is a no-op")

	ids := s.seeded(pool)
	s.ElementsMatch([]string{"browser", "filesystem", "git", "github", "huggingface", "my-tools", "postgres", "slack"}, ids)
	s.NotContains(ids, "gitlab")
	s.NotContains(ids, "unity")
}

func (s *MCPTemplateSeedsMigrationSuite) TestAFreshInstallStartsWithNothingGiven() {
	pool := s.freshDatabase()
	s.Require().NoError(database.RunMigrations(s.ctx, pool))
	s.Empty(s.seeded(pool))
}
