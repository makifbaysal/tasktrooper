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
	"github.com/makifbaysal/tasktrooper/server/migrations"
)

const lastMigrationBeforeRemovePMUAT = "198_remote_access"

// RemovePMUATMigrationSuite covers migration 199: the PM UAT step leaves the
// board, its workflow stages and everything keyed by its slug, and tasks that
// sat in it move on to human_uat. Running it again must change nothing.
type RemovePMUATMigrationSuite struct {
	suite.Suite
	ctx    context.Context
	cancel context.CancelFunc
	pg     *database.Embedded
	boot   *pgxpool.Pool
	seq    atomic.Uint64
}

func TestRemovePMUATMigrationSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(RemovePMUATMigrationSuite))
}

func (s *RemovePMUATMigrationSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 5*time.Minute)
	tmp := s.T().TempDir()
	pg, err := database.StartEmbedded(s.ctx, database.EmbeddedConfig{
		DataDir:     filepath.Join(tmp, "postgres"),
		RuntimePath: filepath.Join(tmp, "runtime"),
	})
	s.Require().NoError(err)
	s.pg = pg
	s.boot, err = pgxpool.New(s.ctx, pg.DSN())
	s.Require().NoError(err)
}

func (s *RemovePMUATMigrationSuite) TearDownSuite() {
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

func (s *RemovePMUATMigrationSuite) freshDatabase() *pgxpool.Pool {
	name := fmt.Sprintf("remove_pm_uat_%d", s.seq.Add(1))
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

func (s *RemovePMUATMigrationSuite) exec(pool *pgxpool.Pool, sql string, args ...any) {
	_, err := pool.Exec(s.ctx, sql, args...)
	s.Require().NoError(err, sql)
}

func (s *RemovePMUATMigrationSuite) text(pool *pgxpool.Pool, sql string, args ...any) string {
	var out string
	s.Require().NoError(pool.QueryRow(s.ctx, sql, args...).Scan(&out), sql)
	return out
}

func (s *RemovePMUATMigrationSuite) count(pool *pgxpool.Pool, sql string, args ...any) int {
	var n int
	s.Require().NoError(pool.QueryRow(s.ctx, sql, args...).Scan(&n), sql)
	return n
}

func (s *RemovePMUATMigrationSuite) TestTheStepLeavesAndItsTasksMoveOn() {
	pool := s.freshDatabase()
	s.Require().NoError(database.RunMigrationsUpTo(s.ctx, pool, lastMigrationBeforeRemovePMUAT))

	for i, slug := range []string{"in_qa", "pm_uat", "human_uat", "done"} {
		s.exec(pool, `INSERT INTO board_columns (slug, label, position) VALUES ($1, $1, $2)`, slug, i)
	}
	s.exec(pool, `INSERT INTO board_column_transitions (from_slug, to_slug) VALUES
		('in_qa', 'pm_uat'), ('in_qa', 'human_uat'), ('ready_for_qa', 'pm_uat'),
		('pm_uat', 'human_uat'), ('pm_uat', 'need_revision'), ('human_uat', 'done')`)

	repo := s.text(pool, `INSERT INTO repositories (name, root_path) VALUES ('acme', '/w/acme') RETURNING id::text`)
	agent := s.text(pool, `INSERT INTO agents (name) VALUES ('product-manager') RETURNING id::text`)
	inUAT := s.text(pool, `INSERT INTO board_tasks (repository_id, title, task_number, board_column) VALUES ($1, 'a', 1, 'pm_uat') RETURNING id::text`, repo)
	parked := s.text(pool, `INSERT INTO board_tasks (repository_id, title, task_number, board_column, blocked_origin_column)
		VALUES ($1, 'b', 2, 'blocked', 'pm_uat') RETURNING id::text`, repo)
	elsewhere := s.text(pool, `INSERT INTO board_tasks (repository_id, title, task_number, board_column) VALUES ($1, 'c', 3, 'in_qa') RETURNING id::text`, repo)
	event := s.text(pool, `INSERT INTO board_events (repository_id, task_id, event_type) VALUES ($1, $2, 'task.moved') RETURNING id::text`, repo, inUAT)
	s.exec(pool, `INSERT INTO task_column_spans (task_id, repository_id, board_column, agent_id) VALUES ($1, $2, 'pm_uat', $3)`, inUAT, repo, agent)
	s.exec(pool, `INSERT INTO task_column_spans (task_id, repository_id, board_column, left_at, duration_seconds) VALUES ($1, $2, 'pm_uat', now(), 60)`, inUAT, repo)
	run := s.text(pool, `INSERT INTO task_agent_runs (task_id, agent_id, board_event_id, status) VALUES ($1, $2, $3, 'pending') RETURNING id::text`, inUAT, agent, event)
	s.exec(pool, `INSERT INTO agent_column_subscriptions (agent_id, column_slug) VALUES ($1, 'pm_uat'), ($1, 'in_qa')`, agent)
	s.exec(pool, `INSERT INTO agent_column_instructions (agent_id, column_slug, instruction) VALUES ($1, 'pm_uat', 'x')`, agent)
	s.exec(pool, `INSERT INTO agent_kpis (agent_id, metric_key, target_full, target_half) VALUES ($1, 'clean_time_pm_uat', 1, 2), ($1, 'tasks_completed', 1, 2)`, agent)
	s.exec(pool, `INSERT INTO agent_templates (name, subscriptions, kpis) VALUES
		('t', '["pm_uat","in_qa"]', '[{"metric_key":"clean_time_pm_uat"},{"metric_key":"tasks_completed"}]')`)
	s.exec(pool, `INSERT INTO app_settings (key, value) VALUES ('pm_uat_enabled', 'false')`)
	s.exec(pool, `UPDATE workflow_stages SET behaviours = behaviours || '[{"key":"require_product_check"},{"key":"criterion_verdict","params":{"channel":"pm"}}]'::jsonb
		WHERE task_type = 'task' AND column_slug = 'code_review'`)

	s.Require().NoError(database.RunMigrations(s.ctx, pool))

	s.Equal("human_uat", s.text(pool, `SELECT board_column FROM board_tasks WHERE id = $1`, inUAT))
	s.Equal("human_uat", s.text(pool, `SELECT blocked_origin_column FROM board_tasks WHERE id = $1`, parked))
	s.Equal("in_qa", s.text(pool, `SELECT board_column FROM board_tasks WHERE id = $1`, elsewhere))
	s.Equal("cancelled", s.text(pool, `SELECT status FROM task_agent_runs WHERE id = $1`, run))
	s.Zero(s.count(pool, `SELECT COUNT(*) FROM board_tasks WHERE board_column = 'pm_uat' OR blocked_origin_column = 'pm_uat'`))
	s.Equal(1, s.count(pool, `SELECT COUNT(*) FROM task_column_spans WHERE board_column = 'human_uat' AND left_at IS NULL AND agent_id IS NULL`),
		"the open span is renamed and no longer belongs to an agent")
	s.Equal(1, s.count(pool, `SELECT COUNT(*) FROM task_column_spans WHERE board_column = 'pm_uat'`), "closed history stays")

	s.Zero(s.count(pool, `SELECT COUNT(*) FROM board_columns WHERE slug = 'pm_uat'`))
	s.Zero(s.count(pool, `SELECT COUNT(*) FROM board_column_transitions WHERE from_slug = 'pm_uat' OR to_slug = 'pm_uat'`))
	for _, edge := range [][2]string{{"in_qa", "human_uat"}, {"ready_for_qa", "human_uat"}, {"human_uat", "done"}} {
		s.Equal(1, s.count(pool, `SELECT COUNT(*) FROM board_column_transitions WHERE from_slug = $1 AND to_slug = $2`, edge[0], edge[1]), "%v", edge)
	}

	s.Zero(s.count(pool, `SELECT COUNT(*) FROM agent_column_subscriptions WHERE column_slug = 'pm_uat'`))
	s.Equal(1, s.count(pool, `SELECT COUNT(*) FROM agent_column_subscriptions WHERE column_slug = 'in_qa'`))
	s.Zero(s.count(pool, `SELECT COUNT(*) FROM agent_column_instructions WHERE column_slug = 'pm_uat'`))
	s.Zero(s.count(pool, `SELECT COUNT(*) FROM agent_kpis WHERE metric_key = 'clean_time_pm_uat'`))
	s.Equal(1, s.count(pool, `SELECT COUNT(*) FROM agent_kpis WHERE metric_key = 'tasks_completed'`))
	s.Equal(`["in_qa"]`, s.text(pool, `SELECT subscriptions::text FROM agent_templates WHERE name = 't'`))
	s.Equal(`[{"metric_key": "tasks_completed"}]`, s.text(pool, `SELECT kpis::text FROM agent_templates WHERE name = 't'`))
	s.Zero(s.count(pool, `SELECT COUNT(*) FROM app_settings WHERE key = 'pm_uat_enabled'`))

	s.Zero(s.count(pool, `SELECT COUNT(*) FROM workflow_stages WHERE column_slug = 'pm_uat'`))
	s.Zero(s.count(pool, `SELECT COUNT(*) FROM workflow_stages WHERE behaviours::text LIKE '%pm_uat%' OR behaviours::text LIKE '%require_product_check%'`))
	s.Zero(s.count(pool, `SELECT COUNT(*) FROM workflow_stages WHERE behaviours::text LIKE '%"channel": "pm"%'`))
	s.Equal(3, s.count(pool, `SELECT COUNT(*) FROM workflow_stages
		WHERE column_slug = 'human_uat' AND task_type IN ('task', 'bug', 'technical')
		  AND behaviours @> '[{"key":"ensure_pr_on_enter"}]'::jsonb`), "human_uat takes over the PR guarantee")
	s.Equal(2, s.count(pool, `SELECT COUNT(*) FROM workflow_stages
		WHERE column_slug IN ('ready_for_qa', 'in_qa') AND task_type = 'task'
		  AND behaviours @> '[{"key":"review_verdict_sweep","params":{"pass_to":"human_uat"}}]'::jsonb`), "QA passes straight to human_uat")

	body, err := migrations.Up.ReadFile("199_remove_pm_uat.up.sql")
	s.Require().NoError(err)
	s.exec(pool, string(body))
	s.Equal("human_uat", s.text(pool, `SELECT board_column FROM board_tasks WHERE id = $1`, inUAT))
	s.Equal(3, s.count(pool, `SELECT COUNT(*) FROM workflow_stages
		WHERE column_slug = 'human_uat' AND task_type IN ('task', 'bug', 'technical')
		  AND behaviours @> '[{"key":"ensure_pr_on_enter"}]'::jsonb`))
	s.Equal(1, s.count(pool, `SELECT COUNT(*) FROM board_column_transitions WHERE from_slug = 'ready_for_qa' AND to_slug = 'human_uat'`))
}

func (s *RemovePMUATMigrationSuite) TestABoardThatNeverHadTheStepIsUntouched() {
	pool := s.freshDatabase()
	s.Require().NoError(database.RunMigrationsUpTo(s.ctx, pool, lastMigrationBeforeRemovePMUAT))
	s.exec(pool, `DELETE FROM workflow_stages WHERE column_slug = 'pm_uat'`)
	s.exec(pool, `INSERT INTO board_columns (slug, label, position) VALUES ('human_uat', 'Human UAT', 1)`)
	before := s.count(pool, `SELECT COUNT(*) FROM workflow_stages`)

	s.Require().NoError(database.RunMigrations(s.ctx, pool))

	s.Equal(before, s.count(pool, `SELECT COUNT(*) FROM workflow_stages`))
	s.Equal(1, s.count(pool, `SELECT COUNT(*) FROM board_columns WHERE slug = 'human_uat'`))
}
