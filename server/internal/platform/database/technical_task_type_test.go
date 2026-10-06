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

	"github.com/makifbaysal/tasktrooper/server/internal/platform/database"
)

const lastMigrationBeforeTechnicalTaskType = "139_task_criterion_check_verified_sha"

// TechnicalTaskTypeSuite covers migration 140: widen the task_type CHECK and,
// for restricted column graphs, add ready_for_qa/in_qa -> human_uat edges.
//
// StartEmbedded migrates its default database at boot, so an older schema is
// stood up on an own, freshly created database on the same cluster.
type TechnicalTaskTypeSuite struct {
	suite.Suite
	ctx    context.Context
	cancel context.CancelFunc
	pg     *database.Embedded
	boot   *pgxpool.Pool
	seq    atomic.Uint64
}

func TestTechnicalTaskTypeSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(TechnicalTaskTypeSuite))
}

func (s *TechnicalTaskTypeSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 5*time.Minute)
	tmp := s.T().TempDir()
	pg, err := database.StartEmbedded(s.ctx, database.EmbeddedConfig{DataDir: filepath.Join(tmp, "postgres")})
	s.Require().NoError(err)
	s.pg = pg
	boot, err := pgxpool.New(s.ctx, pg.DSN())
	s.Require().NoError(err)
	s.boot = boot
}

func (s *TechnicalTaskTypeSuite) TearDownSuite() {
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

func (s *TechnicalTaskTypeSuite) freshDatabase() *pgxpool.Pool {
	name := fmt.Sprintf("technical_task_type_%d", s.seq.Add(1))
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

func (s *TechnicalTaskTypeSuite) TestPermissiveInstallGainsNoTransitionRows() {
	pool := s.freshDatabase()

	s.Require().NoError(database.RunMigrationsUpTo(s.ctx, pool, lastMigrationBeforeTechnicalTaskType))

	var before int
	s.Require().NoError(pool.QueryRow(s.ctx, `SELECT COUNT(*) FROM board_column_transitions`).Scan(&before))
	s.Zero(before, "a fresh install has no restricted transition graph")

	// Up to 140 only: 172 seeds the default graph on an install with no rules.
	s.Require().NoError(database.RunMigrationsUpTo(s.ctx, pool, "140_technical_task_type"))

	var after int
	s.Require().NoError(pool.QueryRow(s.ctx, `SELECT COUNT(*) FROM board_column_transitions`).Scan(&after))
	s.Zero(after, "a permissive install must stay permissive: no rows added")
}

func (s *TechnicalTaskTypeSuite) TestRestrictedInstallGainsHumanUATEdges() {
	pool := s.freshDatabase()

	s.Require().NoError(database.RunMigrationsUpTo(s.ctx, pool, lastMigrationBeforeTechnicalTaskType))

	_, err := pool.Exec(s.ctx, `INSERT INTO board_column_transitions (from_slug, to_slug) VALUES
		('ready_for_qa', 'pm_uat'), ('in_qa', 'pm_uat')`)
	s.Require().NoError(err)

	s.Require().NoError(database.RunMigrations(s.ctx, pool))

	for _, edge := range [][2]string{
		{"ready_for_qa", "human_uat"},
		{"in_qa", "human_uat"},
	} {
		var exists bool
		s.Require().NoError(pool.QueryRow(s.ctx,
			`SELECT EXISTS (SELECT 1 FROM board_column_transitions WHERE from_slug = $1 AND to_slug = $2)`,
			edge[0], edge[1]).Scan(&exists))
		s.True(exists, "%s -> %s must be a legal edge on a restricted install", edge[0], edge[1])
	}

	var stillLegal bool
	s.Require().NoError(pool.QueryRow(s.ctx,
		`SELECT EXISTS (SELECT 1 FROM board_column_transitions WHERE from_slug = 'ready_for_qa' AND to_slug = 'pm_uat')`).
		Scan(&stillLegal))
	s.True(stillLegal, "the migration must be additive, not replace the operator's existing graph")
}

// The CHECK this test originally named became a FK to task_types in migration
// 143; it now asserts that FK exists and that "technical" is a real task_types
// row instead.
func (s *TechnicalTaskTypeSuite) TestTaskTypeCheckConstraintAcceptsTechnical() {
	pool := s.freshDatabase()

	s.Require().NoError(database.RunMigrations(s.ctx, pool))

	var fkExists bool
	s.Require().NoError(pool.QueryRow(s.ctx,
		`SELECT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'board_tasks_task_type_fk')`).
		Scan(&fkExists))
	s.True(fkExists, "board_tasks.task_type must be FK-governed by task_types")

	for _, key := range []string{"task", "analiz", "bug", "technical"} {
		var exists bool
		s.Require().NoError(pool.QueryRow(s.ctx,
			`SELECT EXISTS (SELECT 1 FROM task_types WHERE key = $1)`, key).Scan(&exists))
		s.True(exists, "task_types must carry a row for %q", key)
	}

	var technicalPrefix string
	s.Require().NoError(pool.QueryRow(s.ctx,
		`SELECT key_prefix FROM task_types WHERE key = 'technical'`).Scan(&technicalPrefix))
	s.Equal("TC", technicalPrefix)
}
