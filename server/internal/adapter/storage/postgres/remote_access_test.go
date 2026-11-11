package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/storage/postgres"
	"github.com/makifbaysal/tasktrooper/server/internal/application/remoteaccess"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/database"
)

type RemoteAccessStoreSuite struct {
	suite.Suite
	ctx    context.Context
	cancel context.CancelFunc
	pg     *database.Embedded
	pool   *pgxpool.Pool
	svc    *remoteaccess.Service
}

func TestRemoteAccessStoreSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(RemoteAccessStoreSuite))
}

func (s *RemoteAccessStoreSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 3*time.Minute)
	pg, err := newTestDatabase(s.ctx)
	s.Require().NoError(err)
	s.pg = pg
	s.pool, err = pgxpool.New(s.ctx, pg.DSN())
	s.Require().NoError(err)
	s.svc = remoteaccess.New(postgres.NewRemoteAccessStore(postgres.NewDB(s.pool)), "test")
}

func (s *RemoteAccessStoreSuite) TearDownSuite() {
	if s.pool != nil {
		s.pool.Close()
	}
	if s.pg != nil {
		_ = s.pg.Stop()
	}
	s.cancel()
}

func (s *RemoteAccessStoreSuite) TestPairExchangeAuthRevokeAgainstPostgres() {
	p, err := s.svc.CreatePairing(s.ctx, remoteaccess.RoleOperator, 30, "")
	s.Require().NoError(err)

	var storedCode string
	s.Require().NoError(s.pool.QueryRow(s.ctx, `SELECT code_hash FROM remote_pairings WHERE id = $1`, p.ID).Scan(&storedCode))
	s.NotContains(storedCode, p.Code)

	token, dev, err := s.svc.Exchange(s.ctx, p.Code, "Akif's iPhone", "ios")
	s.Require().NoError(err)
	s.NotNil(dev.ExpiresAt)

	var storedHash string
	s.Require().NoError(s.pool.QueryRow(s.ctx, `SELECT token_hash FROM remote_devices WHERE id = $1`, dev.ID).Scan(&storedHash))
	s.Equal(remoteaccess.HashSecret(token), storedHash)
	s.NotContains(storedHash, token)

	_, _, err = s.svc.Exchange(s.ctx, p.Code, "second", "ios")
	s.ErrorIs(err, remoteaccess.ErrInvalidPairing, "single use")

	got, err := s.svc.Authenticate(s.ctx, token)
	s.Require().NoError(err)
	s.Equal(dev.ID, got.ID)

	name := "Renamed"
	viewer := remoteaccess.RoleViewer
	upd, err := s.svc.UpdateDevice(s.ctx, dev.ID, &name, &viewer)
	s.Require().NoError(err)
	s.Equal("Renamed", upd.Name)
	s.Equal(remoteaccess.RoleViewer, upd.Role)

	devices, err := s.svc.Devices(s.ctx)
	s.Require().NoError(err)
	s.NotEmpty(devices)

	s.Require().NoError(s.svc.Revoke(s.ctx, dev.ID))
	_, err = s.svc.Authenticate(s.ctx, token)
	s.ErrorIs(err, remoteaccess.ErrTokenRevoked)

	entries, err := s.svc.AuditEntries(s.ctx, &dev.ID, 10)
	s.Require().NoError(err)
	kinds := map[remoteaccess.AuditKind]bool{}
	for _, e := range entries {
		kinds[e.Kind] = true
	}
	s.True(kinds[remoteaccess.AuditPair])
	s.True(kinds[remoteaccess.AuditRevoke])
}

func (s *RemoteAccessStoreSuite) TestExpiredPairingIsNotExchangeable() {
	p, err := s.svc.CreatePairing(s.ctx, remoteaccess.RoleOperator, 0, "")
	s.Require().NoError(err)
	_, err = s.pool.Exec(s.ctx, `UPDATE remote_pairings SET expires_at = now() - interval '1 minute' WHERE id = $1`, p.ID)
	s.Require().NoError(err)
	_, _, err = s.svc.Exchange(s.ctx, p.Code, "late", "ios")
	s.ErrorIs(err, remoteaccess.ErrInvalidPairing)
}

func (s *RemoteAccessStoreSuite) TestServerNameSetting() {
	s.Require().NoError(s.svc.SetServerName(s.ctx, "Akif's MacBook"))
	s.Equal("Akif's MacBook", s.svc.ServerName(s.ctx))
}
