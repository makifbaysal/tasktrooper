package postgres_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/storage/postgres"
	"github.com/makifbaysal/tasktrooper/server/internal/application/bootseed"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/database"
)

// BootSeedSuite runs the real board seed against an embedded database.
type BootSeedSuite struct {
	suite.Suite
	ctx    context.Context
	cancel context.CancelFunc
	pg     *database.Embedded
	pool   *pgxpool.Pool
	db     *postgres.DB
}

func TestBootSeedSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(BootSeedSuite))
}

func (s *BootSeedSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 3*time.Minute)
	pg, err := newTestDatabase(s.ctx)
	s.Require().NoError(err)
	s.pg = pg
	s.pool, err = pgxpool.New(s.ctx, pg.DSN())
	s.Require().NoError(err)
	s.db = postgres.NewDB(s.pool)
}

func (s *BootSeedSuite) TearDownSuite() {
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

// TestBoardIsSeededOnce covers the three ways the seed is reached: an install
// seeded before migration 133 (install_state carried over from
// tenants.bootstrapped_at), a fresh install, and every later start.
func (s *BootSeedSuite) TestBoardIsSeededOnce() {
	seeds := postgres.NewBoardSeedStore(s.db)

	s.setSeeded(true)
	ran, err := seeds.SeedBoardOnce(s.ctx, `INSERT INTO board_columns (slug, label, position) VALUES ('probe', 'Probe', 99)`)
	s.Require().NoError(err)
	s.False(ran, "an install whose board was already seeded must never be seeded again")
	s.Zero(s.count("board_columns"))

	s.setSeeded(false)
	s.Require().NoError(bootseed.NewService(seeds).Ensure(s.ctx))
	s.Equal(13, s.count("board_columns"), "the 13-column default board")
	s.Equal(3, s.count("board_task_counters"), "one counter per task type")
	s.Equal(1, s.count("board_settings"))
	s.Equal(1, s.count("billing_plan"))
	s.Positive(s.count("app_settings"))
	s.Positive(s.count("llm_provider_configs"))
	s.Positive(s.count("model_prices"))
	// board_tasks.board_column is validated against these slugs and agent
	// subscriptions name them.
	s.Equal([]string{
		"backlog", "todo", "in_progress", "analiz_review", "code_review",
		"ready_for_qa", "in_qa", "need_revision", "pm_uat", "human_uat",
		"blocked", "done", "released",
	}, s.slugs())
	s.Equal(s.defaultTransitions(), s.transitions(), "the recommended transition rules")

	// A restarted process has an empty cache, so only install_state stops it.
	s.Require().NoError(bootseed.NewService(seeds).Ensure(s.ctx))
	s.Equal(13, s.count("board_columns"), "a second start duplicated the board")
	s.Equal(1, s.count("board_settings"))
	var seeded bool
	s.Require().NoError(s.db.QueryRow(s.ctx, `SELECT board_seeded_at IS NOT NULL FROM install_state`).Scan(&seeded))
	s.True(seeded)
}

// TestConcurrentStartsSeedOnce: the gate's upsert is also its lock, so starts
// racing on an unseeded install seed it exactly once.
func (s *BootSeedSuite) TestConcurrentStartsSeedOnce() {
	seeds := postgres.NewBoardSeedStore(s.db)
	s.setSeeded(false)

	var ran atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := seeds.SeedBoardOnce(s.ctx, `SELECT pg_sleep(0.2)`)
			s.NoError(err)
			if ok {
				ran.Add(1)
			}
		}()
	}
	wg.Wait()
	s.EqualValues(1, ran.Load())
}

func (s *BootSeedSuite) setSeeded(seeded bool) {
	s.T().Helper()
	tag, err := s.db.Exec(s.ctx,
		`UPDATE install_state SET board_seeded_at = CASE WHEN $1::boolean THEN now() END`, seeded)
	s.Require().NoError(err)
	s.Require().EqualValues(1, tag.RowsAffected(), "migration 133 must leave exactly one install_state row")
}

func (s *BootSeedSuite) count(table string) int {
	s.T().Helper()
	var n int
	s.Require().NoError(s.db.QueryRow(s.ctx, `SELECT count(*) FROM `+table).Scan(&n))
	return n
}

func (s *BootSeedSuite) slugs() []string {
	s.T().Helper()
	rows, err := s.db.Query(s.ctx, `SELECT slug FROM board_columns ORDER BY position`)
	s.Require().NoError(err)
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var v string
		s.Require().NoError(rows.Scan(&v))
		out = append(out, v)
	}
	s.Require().NoError(rows.Err())
	return out
}

func (s *BootSeedSuite) transitions() map[domain.BoardTransition]bool {
	s.T().Helper()
	rows, err := s.db.Query(s.ctx, `SELECT from_slug, to_slug FROM board_column_transitions`)
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

func (s *BootSeedSuite) defaultTransitions() map[domain.BoardTransition]bool {
	out := map[domain.BoardTransition]bool{}
	for _, t := range domain.DefaultBoardTransitions() {
		out[t] = true
	}
	return out
}
