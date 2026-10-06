package database_test

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/database"
)

const lastMigrationBeforeDefaultTransitions = "171_env_requirements"

// DefaultBoardTransitionsSuite covers migration 172 against the Go source of
// truth: a stock board ends on exactly domain.DefaultBoardTransitions whatever
// it had, a rule-less custom board keeps none, and a restricted custom board
// gains the required moves only where its source was already restricted.
type DefaultBoardTransitionsSuite struct {
	suite.Suite
	ctx    context.Context
	cancel context.CancelFunc
	pg     *database.Embedded
	boot   *pgxpool.Pool
	seq    atomic.Uint64
}

func TestDefaultBoardTransitionsSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(DefaultBoardTransitionsSuite))
}

func (s *DefaultBoardTransitionsSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 5*time.Minute)
	pg, err := database.StartEmbedded(s.ctx, database.EmbeddedConfig{DataDir: filepath.Join(s.T().TempDir(), "postgres")})
	s.Require().NoError(err)
	s.pg = pg
	s.boot, err = pgxpool.New(s.ctx, pg.DSN())
	s.Require().NoError(err)
}

func (s *DefaultBoardTransitionsSuite) TearDownSuite() {
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

func (s *DefaultBoardTransitionsSuite) freshDatabase() *pgxpool.Pool {
	name := fmt.Sprintf("default_transitions_%d", s.seq.Add(1))
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

func (s *DefaultBoardTransitionsSuite) edges(pool *pgxpool.Pool) map[domain.BoardTransition]bool {
	rows, err := pool.Query(s.ctx, `SELECT from_slug, to_slug FROM board_column_transitions`)
	s.Require().NoError(err)
	defer rows.Close()
	out := map[domain.BoardTransition]bool{}
	for rows.Next() {
		var t domain.BoardTransition
		s.Require().NoError(rows.Scan(&t.From, &t.To))
		out[t] = true
	}
	s.Require().NoError(rows.Err())
	return out
}

// An install seeded before 172 (columns from bootseed, no rules) is the case
// the migration exists for; a brand-new install gets the same rules from
// bootseed's seed.sql instead (boot_seed_test.go).
func (s *DefaultBoardTransitionsSuite) TestStockInstallGetsTheDefaultGraph() {
	pool := s.freshDatabase()
	s.Require().NoError(database.RunMigrationsUpTo(s.ctx, pool, lastMigrationBeforeDefaultTransitions))
	s.seedStockColumns(pool)

	s.Require().NoError(database.RunMigrations(s.ctx, pool))

	s.Equal(s.defaults(), s.edges(pool))
}

func (s *DefaultBoardTransitionsSuite) TestCustomColumnBoardStaysPermissive() {
	pool := s.freshDatabase()
	s.Require().NoError(database.RunMigrationsUpTo(s.ctx, pool, lastMigrationBeforeDefaultTransitions))
	s.seedStockColumns(pool)
	_, err := pool.Exec(s.ctx, `INSERT INTO board_columns (slug, label, position) VALUES ('staging', 'Staging', 99)`)
	s.Require().NoError(err)

	s.Require().NoError(database.RunMigrations(s.ctx, pool))

	s.Empty(s.edges(pool))
}

func (s *DefaultBoardTransitionsSuite) TestStockBoardsHandDrawnGraphIsReplaced() {
	pool := s.freshDatabase()
	s.Require().NoError(database.RunMigrationsUpTo(s.ctx, pool, lastMigrationBeforeDefaultTransitions))
	s.seedStockColumns(pool)
	_, err := pool.Exec(s.ctx, `INSERT INTO board_column_transitions (from_slug, to_slug) VALUES
		('code_review', 'human_uat'), ('ready_for_qa', 'in_progress'), ('in_progress', 'ready_for_qa')`)
	s.Require().NoError(err)

	s.Require().NoError(database.RunMigrations(s.ctx, pool))

	s.Equal(s.defaults(), s.edges(pool))
}

func (s *DefaultBoardTransitionsSuite) TestCustomBoardGainsOnlyTheMissingRequiredMoves() {
	pool := s.freshDatabase()
	s.Require().NoError(database.RunMigrationsUpTo(s.ctx, pool, lastMigrationBeforeDefaultTransitions))
	s.seedStockColumns(pool)
	_, err := pool.Exec(s.ctx, `INSERT INTO board_columns (slug, label, position) VALUES ('staging', 'Staging', 99)`)
	s.Require().NoError(err)
	_, err = pool.Exec(s.ctx, `INSERT INTO board_column_transitions (from_slug, to_slug) VALUES
		('code_review', 'staging'), ('ready_for_qa', 'in_progress'), ('todo', 'in_progress')`)
	s.Require().NoError(err)

	s.Require().NoError(database.RunMigrations(s.ctx, pool))

	got := s.edges(pool)
	for _, kept := range []domain.BoardTransition{
		{From: "code_review", To: "staging"},
		{From: "ready_for_qa", To: "in_progress"},
		{From: "todo", To: "in_progress"},
	} {
		s.True(got[kept], "the operator's own edge %v must survive", kept)
	}
	for _, added := range []domain.BoardTransition{
		{From: "code_review", To: "ready_for_qa"},
		{From: "code_review", To: "need_revision"},
		{From: "ready_for_qa", To: "in_qa"},
		{From: "ready_for_qa", To: "need_revision"},
	} {
		s.True(got[added], "a restricted source must gain the required %v", added)
	}
	for t := range got {
		s.Contains([]string{"code_review", "ready_for_qa", "todo"}, t.From,
			"an unrestricted source must stay unrestricted, got %v", t)
	}
	s.False(got[domain.BoardTransition{From: "todo", To: "blocked"}], "only required moves are added to a custom graph")
}

func (s *DefaultBoardTransitionsSuite) defaults() map[domain.BoardTransition]bool {
	want := map[domain.BoardTransition]bool{}
	for _, t := range domain.DefaultBoardTransitions() {
		want[t] = true
	}
	return want
}

func (s *DefaultBoardTransitionsSuite) seedStockColumns(pool *pgxpool.Pool) {
	s.T().Helper()
	slugs := []string{
		"backlog", "todo", "in_progress", "analiz_review", "code_review", "ready_for_qa",
		"in_qa", "need_revision", "pm_uat", "human_uat", "blocked", "done", "released",
	}
	for i, slug := range slugs {
		_, err := pool.Exec(s.ctx, `INSERT INTO board_columns (slug, label, position, is_backlog) VALUES ($1, $1, $2, $3)`,
			slug, i, slug == "backlog")
		s.Require().NoError(err)
	}
}
