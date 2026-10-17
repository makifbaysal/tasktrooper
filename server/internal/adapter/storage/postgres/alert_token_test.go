package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/storage/postgres"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/database"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type AlertTokenStoreSuite struct {
	suite.Suite
	ctx    context.Context
	cancel context.CancelFunc
	pg     *database.Embedded
	pool   *pgxpool.Pool
	db     *postgres.DB
	store  *postgres.AlertIngestTokenStore
	repos  *postgres.RepositoryStore
	repoID uuid.UUID
}

func TestAlertTokenStoreSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(AlertTokenStoreSuite))
}

func (s *AlertTokenStoreSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 3*time.Minute)
	pg, err := newTestDatabase(s.ctx)
	s.Require().NoError(err)
	s.pg = pg
	pool, err := pgxpool.New(s.ctx, pg.DSN())
	s.Require().NoError(err)
	s.pool = pool
	s.db = postgres.NewDB(pool)
	s.store = postgres.NewAlertIngestTokenStore(s.db)
	s.repos = postgres.NewRepositoryStore(s.db)
}

func (s *AlertTokenStoreSuite) SetupTest() {
	name := "alert-tokens-" + uuid.NewString()
	repo, err := s.repos.Create(s.ctx, name, "", "/tmp/"+name, "", "")
	s.Require().NoError(err)
	s.repoID = repo.ID
}

func (s *AlertTokenStoreSuite) TearDownSuite() {
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

func (s *AlertTokenStoreSuite) TestTheHashFindsTheTokenAndMarkUsedStampsIt() {
	created, err := s.store.Create(s.ctx, s.repoID, "Alertmanager", "hash-"+uuid.NewString(), "stage")
	s.Require().NoError(err)
	s.Equal("Alertmanager", created.Label)
	s.Equal("stage", created.DefaultEnv)
	s.Nil(created.LastUsedAt)

	var hash string
	s.Require().NoError(s.pool.QueryRow(s.ctx, `SELECT token_hash FROM alert_ingest_tokens WHERE id = $1`, created.ID).Scan(&hash))
	found, err := s.store.FindByHash(s.ctx, hash)
	s.Require().NoError(err)
	s.Equal(created.ID, found.ID)
	s.Equal(s.repoID, found.RepositoryID)

	s.Require().NoError(s.store.MarkUsed(s.ctx, created.ID))
	list, err := s.store.List(s.ctx, s.repoID)
	s.Require().NoError(err)
	s.Require().Len(list, 1)
	s.NotNil(list[0].LastUsedAt)
}

func (s *AlertTokenStoreSuite) TestAnUnknownHashIsNotFound() {
	_, err := s.store.FindByHash(s.ctx, "no-such-hash")
	s.ErrorIs(err, port.ErrNotFound)
}

func (s *AlertTokenStoreSuite) TestAHashIsUnique() {
	hash := "dup-" + uuid.NewString()
	_, err := s.store.Create(s.ctx, s.repoID, "first", hash, "prod")
	s.Require().NoError(err)
	_, err = s.store.Create(s.ctx, s.repoID, "second", hash, "prod")
	s.Error(err)
}

func (s *AlertTokenStoreSuite) TestListIsPerRepositoryNewestFirst() {
	other, err := s.repos.Create(s.ctx, "alert-tokens-other-"+uuid.NewString(), "", "/tmp/other-"+uuid.NewString(), "", "")
	s.Require().NoError(err)
	older, err := s.store.Create(s.ctx, s.repoID, "older", "h-"+uuid.NewString(), "prod")
	s.Require().NoError(err)
	newer, err := s.store.Create(s.ctx, s.repoID, "newer", "h-"+uuid.NewString(), "prod")
	s.Require().NoError(err)
	_, err = s.store.Create(s.ctx, other.ID, "elsewhere", "h-"+uuid.NewString(), "prod")
	s.Require().NoError(err)
	_, err = s.pool.Exec(s.ctx, `UPDATE alert_ingest_tokens SET created_at = created_at - interval '1 minute' WHERE id = $1`, older.ID)
	s.Require().NoError(err)

	list, err := s.store.List(s.ctx, s.repoID)
	s.Require().NoError(err)
	s.Require().Len(list, 2)
	s.Equal(newer.ID, list[0].ID)
	s.Equal(older.ID, list[1].ID)
}

func (s *AlertTokenStoreSuite) TestDeleteRevokesOnlyWithinItsRepository() {
	created, err := s.store.Create(s.ctx, s.repoID, "Sentry", "h-"+uuid.NewString(), "prod")
	s.Require().NoError(err)

	s.ErrorIs(s.store.Delete(s.ctx, uuid.New(), created.ID), port.ErrNotFound, "another repository cannot revoke it")
	s.Require().NoError(s.store.Delete(s.ctx, s.repoID, created.ID))
	s.ErrorIs(s.store.Delete(s.ctx, s.repoID, created.ID), port.ErrNotFound)
	list, err := s.store.List(s.ctx, s.repoID)
	s.Require().NoError(err)
	s.Empty(list)
}

func (s *AlertTokenStoreSuite) TestDeletingTheRepositoryRevokesItsTokens() {
	_, err := s.store.Create(s.ctx, s.repoID, "Grafana", "h-"+uuid.NewString(), "prod")
	s.Require().NoError(err)

	_, err = s.pool.Exec(s.ctx, `DELETE FROM repositories WHERE id = $1`, s.repoID)
	s.Require().NoError(err)

	var n int
	s.Require().NoError(s.pool.QueryRow(s.ctx, `SELECT count(*) FROM alert_ingest_tokens WHERE repository_id = $1`, s.repoID).Scan(&n))
	s.Zero(n)
}
