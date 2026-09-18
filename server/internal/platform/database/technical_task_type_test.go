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

// TechnicalTaskTypeSuite covers migration 140: it must widen the task_type
// CHECK constraint, and — only for an install that already runs a restricted
// board_column_transitions graph — add the ready_for_qa/in_qa -> human_uat
// edges a technical task needs to skip pm_uat.
//
// StartEmbedded migrates its default database on boot (see embedded.go), so a
// test standing a database at an older schema needs its OWN, freshly
// CREATE DATABASE'd database on that same cluster — connecting to the default
// DSN would already be at HEAD and no migration would have anything to apply.
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

// freshDatabase creates a new, unmigrated database on the shared cluster and
// returns a pool connected to it, plus a cleanup that drops it.
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

	s.Require().NoError(database.RunMigrations(s.ctx, pool))

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

func (s *TechnicalTaskTypeSuite) TestTaskTypeCheckConstraintAcceptsTechnical() {
	pool := s.freshDatabase()

	s.Require().NoError(database.RunMigrations(s.ctx, pool))

	var definition string
	s.Require().NoError(pool.QueryRow(s.ctx,
		`SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = 'board_tasks_task_type_check'`).
		Scan(&definition))
	s.Contains(definition, "'technical'")
	s.Contains(definition, "'task'")
	s.Contains(definition, "'analiz'")
	s.Contains(definition, "'bug'")
}
