package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/storage/postgres"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/domain/secrets"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/database"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type CloudAccountStoreSuite struct {
	suite.Suite
	ctx    context.Context
	cancel context.CancelFunc
	pg     *database.Embedded
	pool   *pgxpool.Pool
	store  *postgres.CloudAccountStore
}

func TestCloudAccountStoreSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(CloudAccountStoreSuite))
}

func (s *CloudAccountStoreSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 3*time.Minute)
	pg, err := newTestDatabase(s.ctx)
	s.Require().NoError(err)
	s.pg = pg
	pool, err := pgxpool.New(s.ctx, pg.DSN())
	s.Require().NoError(err)
	s.pool = pool
	db := postgres.NewDB(pool)
	s.store = postgres.NewCloudAccountStore(db)
	cipher, err := secrets.NewCipher(make([]byte, 32))
	s.Require().NoError(err)
	s.store.SetCipher(cipher, nil)
}

func (s *CloudAccountStoreSuite) TearDownSuite() {
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

func (s *CloudAccountStoreSuite) TestCreateGetListRoundTripWithoutExposingSecret() {
	acct := domain.CloudAccount{
		Provider: domain.CloudVercel,
		Label:    "prod team",
		Meta:     map[string]string{"team": "acme"},
	}
	fields := map[string]string{"token": "top-secret-token"}

	created, err := s.store.CreateCloudAccount(s.ctx, acct, fields)
	s.Require().NoError(err)
	s.NotEqual(uuid.Nil, created.ID)
	s.Equal(domain.CloudVercel, created.Provider)
	s.Equal("prod team", created.Label)
	s.Equal(map[string]string{"team": "acme"}, created.Meta)
	s.Equal(domain.CloudAccountUnverified, created.Status)
	s.False(created.CreatedAt.IsZero())
	s.False(created.UpdatedAt.IsZero())

	got, err := s.store.GetCloudAccount(s.ctx, created.ID)
	s.Require().NoError(err)
	s.Equal(created, got)

	list, err := s.store.ListCloudAccounts(s.ctx)
	s.Require().NoError(err)
	var found bool
	for _, a := range list {
		if a.ID == created.ID {
			found = true
		}
	}
	s.True(found)

	var raw []byte
	s.Require().NoError(s.pool.QueryRow(s.ctx, `SELECT secret_enc FROM cloud_accounts WHERE id = $1`, created.ID).Scan(&raw))
	s.NotContains(string(raw), "top-secret-token")
}

func (s *CloudAccountStoreSuite) TestMetaNilDefaultsToEmptyObject() {
	created, err := s.store.CreateCloudAccount(s.ctx, domain.CloudAccount{Provider: domain.CloudGCP}, map[string]string{"service_account_json": "{}"})
	s.Require().NoError(err)
	s.NotNil(created.Meta)
	s.Empty(created.Meta)
}

func (s *CloudAccountStoreSuite) TestCreateCloudAccountRequiresFields() {
	_, err := s.store.CreateCloudAccount(s.ctx, domain.CloudAccount{Provider: domain.CloudGCP}, nil)
	s.Error(err)

	_, err = s.store.CreateCloudAccount(s.ctx, domain.CloudAccount{Provider: domain.CloudGCP}, map[string]string{})
	s.Error(err)
}

func (s *CloudAccountStoreSuite) TestCloudCredentialDecryptsSavedFields() {
	fields := map[string]string{"token": "tkn-abc", "team_id": "team-1"}
	created, err := s.store.CreateCloudAccount(s.ctx, domain.CloudAccount{
		Provider: domain.CloudVercel,
		Meta:     map[string]string{"account": "acme"},
	}, fields)
	s.Require().NoError(err)

	cred, err := s.store.CloudCredential(s.ctx, created.ID)
	s.Require().NoError(err)
	s.Equal(created.ID, cred.AccountID)
	s.Equal(domain.CloudVercel, cred.Provider)
	s.Equal(map[string]string{"account": "acme"}, cred.Meta)
	s.Equal(fields, cred.Fields)
}

func (s *CloudAccountStoreSuite) TestUpdateWithoutFieldsKeepsTheSecret() {
	created, err := s.store.CreateCloudAccount(s.ctx, domain.CloudAccount{
		Provider: domain.CloudAWS,
		Label:    "original",
	}, map[string]string{"access_key_id": "AKIA...", "secret_access_key": "shh"})
	s.Require().NoError(err)

	updated := created
	updated.Label = "renamed"
	updated.Status = domain.CloudAccountOK
	updated.StatusDetail = "verified"
	now := time.Now().UTC().Truncate(time.Microsecond)
	updated.VerifiedAt = &now

	out, err := s.store.UpdateCloudAccount(s.ctx, updated, nil)
	s.Require().NoError(err)
	s.Equal("renamed", out.Label)
	s.Equal(domain.CloudAccountOK, out.Status)
	s.Equal("verified", out.StatusDetail)
	s.Require().NotNil(out.VerifiedAt)
	s.True(now.Equal(*out.VerifiedAt))

	cred, err := s.store.CloudCredential(s.ctx, created.ID)
	s.Require().NoError(err)
	s.Equal(map[string]string{"access_key_id": "AKIA...", "secret_access_key": "shh"}, cred.Fields)
}

func (s *CloudAccountStoreSuite) TestUpdateWithFieldsReplacesTheSecret() {
	created, err := s.store.CreateCloudAccount(s.ctx, domain.CloudAccount{Provider: domain.CloudAWS}, map[string]string{"access_key_id": "old"})
	s.Require().NoError(err)

	_, err = s.store.UpdateCloudAccount(s.ctx, created, map[string]string{"access_key_id": "new"})
	s.Require().NoError(err)

	cred, err := s.store.CloudCredential(s.ctx, created.ID)
	s.Require().NoError(err)
	s.Equal(map[string]string{"access_key_id": "new"}, cred.Fields)
}

func (s *CloudAccountStoreSuite) TestUpdateCloudAccountNotFound() {
	_, err := s.store.UpdateCloudAccount(s.ctx, domain.CloudAccount{ID: uuid.New(), Provider: domain.CloudGCP}, nil)
	s.ErrorIs(err, port.ErrNotFound)
}

func (s *CloudAccountStoreSuite) TestDeleteCloudAccountRemovesRow() {
	created, err := s.store.CreateCloudAccount(s.ctx, domain.CloudAccount{Provider: domain.CloudVercel}, map[string]string{"token": "x"})
	s.Require().NoError(err)

	s.Require().NoError(s.store.DeleteCloudAccount(s.ctx, created.ID))

	_, err = s.store.GetCloudAccount(s.ctx, created.ID)
	s.ErrorIs(err, port.ErrNotFound)

	err = s.store.DeleteCloudAccount(s.ctx, created.ID)
	s.ErrorIs(err, port.ErrNotFound)
}

func (s *CloudAccountStoreSuite) TestGetCloudAccountNotFound() {
	_, err := s.store.GetCloudAccount(s.ctx, uuid.New())
	s.ErrorIs(err, port.ErrNotFound)
}

func (s *CloudAccountStoreSuite) TestCloudCredentialNotFound() {
	_, err := s.store.CloudCredential(s.ctx, uuid.New())
	s.ErrorIs(err, port.ErrNotFound)
}
