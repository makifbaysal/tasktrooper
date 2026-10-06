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

const lastMigrationBeforeUsageCorrections = "172_default_board_transitions"

// LLMUsageCorrectionsSuite covers migration 173: old opencode rows get their
// cache folded into prompt_tokens, and 168's '(unrecorded)' Claude Code rows
// take the model their run's session steps reported — only when both the run
// and the model are unambiguous. Re-running it must change nothing.
type LLMUsageCorrectionsSuite struct {
	suite.Suite
	ctx    context.Context
	cancel context.CancelFunc
	pg     *database.Embedded
	boot   *pgxpool.Pool
	seq    atomic.Uint64
}

func TestLLMUsageCorrectionsSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(LLMUsageCorrectionsSuite))
}

func (s *LLMUsageCorrectionsSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 5*time.Minute)
	pg, err := database.StartEmbedded(s.ctx, database.EmbeddedConfig{DataDir: filepath.Join(s.T().TempDir(), "postgres")})
	s.Require().NoError(err)
	s.pg = pg
	s.boot, err = pgxpool.New(s.ctx, pg.DSN())
	s.Require().NoError(err)
}

func (s *LLMUsageCorrectionsSuite) TearDownSuite() {
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

func (s *LLMUsageCorrectionsSuite) freshDatabase() *pgxpool.Pool {
	name := fmt.Sprintf("usage_corrections_%d", s.seq.Add(1))
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

type usageFixture struct {
	pool  *pgxpool.Pool
	s     *LLMUsageCorrectionsSuite
	repo  string
	agent string
	task  string
	event string
	at    time.Time
}

func (f *usageFixture) exec(sql string, args ...any) {
	_, err := f.pool.Exec(f.s.ctx, sql, args...)
	f.s.Require().NoError(err, sql)
}

func (f *usageFixture) scan(sql string, args ...any) string {
	var out string
	f.s.Require().NoError(f.pool.QueryRow(f.s.ctx, sql, args...).Scan(&out), sql)
	return out
}

// run writes a finished Claude Code run whose session steps report models,
// and the '(unrecorded)' llm_usage row 168 made from it. It returns the
// usage row id.
func (f *usageFixture) run(prompt int, models ...string) string {
	f.at = f.at.Add(time.Minute)
	session := f.scan(`INSERT INTO sessions (title) VALUES ('run') RETURNING id::text`)
	sessionRun := f.scan(`INSERT INTO session_runs (session_id, request_id, model, status)
		VALUES ($1, gen_random_uuid()::text, '', 'completed') RETURNING id::text`, session)
	for _, m := range models {
		f.exec(`INSERT INTO session_steps (run_id, step_type, payload)
			VALUES ($1, 'claude_code_session', jsonb_build_object('model', $2::text))`, sessionRun, m)
	}
	f.exec(`INSERT INTO task_agent_runs (task_id, agent_id, board_event_id, session_run_id, status,
			prompt_tokens, completion_tokens, cache_read_tokens, cache_write_tokens, created_at, updated_at)
		VALUES ($1, $2, $3, $4, 'completed', $5, 10, 50, 5, $6, $6)`,
		f.task, f.agent, f.event, sessionRun, prompt, f.at)
	return f.scan(`INSERT INTO llm_usage (kind, provider, model, prompt_tokens, completion_tokens,
			cache_read_tokens, cache_write_tokens, created_at)
		VALUES ('cli', 'claude_code', '(unrecorded)', $1, 10, 50, 5, $2) RETURNING id::text`, prompt, f.at)
}

func (s *LLMUsageCorrectionsSuite) seed(pool *pgxpool.Pool) *usageFixture {
	f := &usageFixture{pool: pool, s: s, at: time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)}
	f.repo = f.scan(`INSERT INTO repositories (name, root_path) VALUES ('acme', '/w/acme') RETURNING id::text`)
	f.agent = f.scan(`INSERT INTO agents (name, provider_type) VALUES ('Developer', 'claude_code') RETURNING id::text`)
	f.task = f.scan(`INSERT INTO board_tasks (repository_id, title, task_number) VALUES ($1, 't', 1) RETURNING id::text`, f.repo)
	f.event = f.scan(`INSERT INTO board_events (repository_id, task_id, event_type)
		VALUES ($1, $2, 'task.moved') RETURNING id::text`, f.repo, f.task)
	return f
}

func (s *LLMUsageCorrectionsSuite) model(pool *pgxpool.Pool, id string) string {
	var m string
	s.Require().NoError(pool.QueryRow(s.ctx, `SELECT model FROM llm_usage WHERE id = $1`, id).Scan(&m))
	return m
}

func (s *LLMUsageCorrectionsSuite) prompt(pool *pgxpool.Pool, id string) int64 {
	var p int64
	s.Require().NoError(pool.QueryRow(s.ctx, `SELECT prompt_tokens FROM llm_usage WHERE id = $1`, id).Scan(&p))
	return p
}

func (s *LLMUsageCorrectionsSuite) TestCorrections() {
	pool := s.freshDatabase()
	s.Require().NoError(database.RunMigrationsUpTo(s.ctx, pool, lastMigrationBeforeUsageCorrections))
	f := s.seed(pool)

	single := f.run(1000, "claude-sonnet-5", "claude-sonnet-5")
	mixed := f.run(2000, "claude-sonnet-5", "claude-opus-4-8")
	noSteps := f.run(3000)
	twinA := f.run(4000, "claude-sonnet-5")
	// A second run with the same counters at the same instant: the row can't
	// say which run it came from.
	f.exec(`INSERT INTO task_agent_runs (task_id, agent_id, board_event_id, status,
			prompt_tokens, completion_tokens, cache_read_tokens, cache_write_tokens, created_at, updated_at)
		VALUES ($1, $2, $3, 'completed', 4000, 10, 50, 5, $4, $4)`, f.task, f.agent, f.event, f.at)

	oldOpencode := f.scan(`INSERT INTO llm_usage (kind, provider, model, prompt_tokens, completion_tokens,
			cache_read_tokens, cache_write_tokens) VALUES ('cli', 'opencode', 'opencode/big-pickle', 100, 20, 800, 40)
		RETURNING id::text`)
	newOpencode := f.scan(`INSERT INTO llm_usage (kind, provider, model, prompt_tokens, completion_tokens,
			cache_read_tokens, cache_write_tokens) VALUES ('cli', 'opencode', 'opencode/big-pickle', 940, 20, 800, 40)
		RETURNING id::text`)

	s.Require().NoError(database.RunMigrations(s.ctx, pool))

	s.Equal("claude-sonnet-5", s.model(pool, single))
	s.Equal("(unrecorded)", s.model(pool, mixed), "a run that switched models cannot be split")
	s.Equal("(unrecorded)", s.model(pool, noSteps), "no step, no model")
	s.Equal("(unrecorded)", s.model(pool, twinA), "an ambiguous run match must not be named")
	s.EqualValues(940, s.prompt(pool, oldOpencode), "old opencode rows fold both cache buckets into the prompt")
	s.EqualValues(940, s.prompt(pool, newOpencode), "a row the fixed adapter wrote is already right")

	body, err := migrations.Up.ReadFile("173_llm_usage_corrections.up.sql")
	s.Require().NoError(err)
	_, err = pool.Exec(s.ctx, string(body))
	s.Require().NoError(err)
	s.EqualValues(940, s.prompt(pool, oldOpencode), "running it again must not add the cache twice")
	s.Equal("claude-sonnet-5", s.model(pool, single))
}
