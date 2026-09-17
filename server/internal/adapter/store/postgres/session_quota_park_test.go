package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/store/postgres"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/database"
)

// SessionQuotaParkSuite covers ParkPendingTurn/TakePendingSessionTurn's one
// piece of SQL that only a real Postgres can answer for: TakePendingSessionTurn
// reads the request/policy JSON from a CTE snapshot taken before the same
// statement's own UPDATE clears them. RETURNING would hand back the
// post-UPDATE NULLs, and a Go-level test with a fake store would pass either
// way — the assertion here is only meaningful against the real engine.
type SessionQuotaParkSuite struct {
	suite.Suite
	ctx    context.Context
	cancel context.CancelFunc
	pg     *database.Embedded
	pool   *pgxpool.Pool
	db     *postgres.DB
	store  *postgres.SessionStore
}

func TestSessionQuotaParkSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(SessionQuotaParkSuite))
}

func (s *SessionQuotaParkSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithCancel(context.Background())
	startupCtx, cancelStartup := context.WithTimeout(s.ctx, 3*time.Minute)
	defer cancelStartup()

	pg, err := newTestDatabase(startupCtx)
	s.Require().NoError(err)
	s.pg = pg
	pool, err := pgxpool.New(s.ctx, pg.DSN())
	s.Require().NoError(err)
	s.pool = pool
	s.db = postgres.NewDB(pool)
	s.store = postgres.NewSessionStore(s.db)
}

func (s *SessionQuotaParkSuite) TearDownSuite() {
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

func (s *SessionQuotaParkSuite) TestParkThenTakeRoundTripsTheRequestAndPolicy() {
	created, err := s.store.Create(s.ctx, "quota chat", "sonnet", "/tmp/quota-chat", nil, nil, nil)
	s.Require().NoError(err)

	req := domain.SessionMessageRequest{
		Role:    domain.RoleUser,
		Content: "keep going on the auth refactor",
		FileIDs: []string{"file-1"},
	}
	policy := domain.ToolPolicy{AllowTools: []string{"read_file", "run_terminal"}}
	resumeAt := time.Now().Add(-time.Minute).UTC() // already due

	s.Require().NoError(s.store.ParkPendingTurn(s.ctx, created.ID, req, policy, resumeAt))

	pending, ok, err := s.store.TakePendingSessionTurn(s.ctx, time.Now())
	s.Require().NoError(err)
	s.Require().True(ok)
	s.Equal(created.ID, pending.SessionID)
	s.Equal(req.Content, pending.Request.Content)
	s.Equal(req.FileIDs, pending.Request.FileIDs)
	s.Equal(policy.AllowTools, pending.Policy.AllowTools)
	s.WithinDuration(resumeAt, pending.ResumeAt, time.Second)

	// The claim also clears the park: a second take must find nothing due.
	_, ok, err = s.store.TakePendingSessionTurn(s.ctx, time.Now())
	s.Require().NoError(err)
	s.False(ok)
}

func (s *SessionQuotaParkSuite) TestTakeIgnoresAParkThatIsNotDueYet() {
	created, err := s.store.Create(s.ctx, "future quota chat", "sonnet", "/tmp/quota-chat-future", nil, nil, nil)
	s.Require().NoError(err)

	req := domain.SessionMessageRequest{Role: domain.RoleUser, Content: "not due yet"}
	resumeAt := time.Now().Add(time.Hour).UTC()
	s.Require().NoError(s.store.ParkPendingTurn(s.ctx, created.ID, req, domain.ToolPolicy{}, resumeAt))

	_, ok, err := s.store.TakePendingSessionTurn(s.ctx, time.Now())
	s.Require().NoError(err)
	s.False(ok, "a park due an hour from now must not be claimed yet")
}
