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

type DataSourceStoreSuite struct {
	suite.Suite
	ctx    context.Context
	cancel context.CancelFunc
	pg     *database.Embedded
	pool   *pgxpool.Pool
	store  *postgres.DataSourceStore
}

func TestDataSourceStoreSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(DataSourceStoreSuite))
}

func (s *DataSourceStoreSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 3*time.Minute)
	pg, err := newTestDatabase(s.ctx)
	s.Require().NoError(err)
	s.pg = pg
	pool, err := pgxpool.New(s.ctx, pg.DSN())
	s.Require().NoError(err)
	s.pool = pool
	s.store = postgres.NewDataSourceStore(postgres.NewDB(pool))
	cipher, err := secrets.NewCipher(make([]byte, 32))
	s.Require().NoError(err)
	s.store.SetCipher(cipher, nil)
}

func (s *DataSourceStoreSuite) TearDownSuite() {
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

func (s *DataSourceStoreSuite) SetupTest() {
	_, err := s.pool.Exec(s.ctx, `TRUNCATE signal_bindings, alert_events, alert_rules, cloud_account_data_sources, data_sources, observability_recipes`)
	s.Require().NoError(err)
}

func (s *DataSourceStoreSuite) TestCreateGetListRoundTripKeepsSecretsOutOfReads() {
	created, err := s.store.CreateDataSource(s.ctx, domain.DataSource{
		Kind:   domain.DataSourceKindPrometheus,
		Name:   "local",
		Config: map[string]string{"url": "http://localhost:9090", "auth": "bearer"},
	}, map[string]string{"token": "top-secret", "headers": "X-Scope-OrgID: t1"})
	s.Require().NoError(err)

	s.NotEqual(uuid.Nil, created.ID)
	s.Equal(domain.DataSourceUnverified, created.Status)
	s.Equal([]string{"headers", "token"}, created.SecretsSet)
	s.Equal(map[string]string{"url": "http://localhost:9090", "auth": "bearer"}, created.Config)
	s.Nil(created.VerifiedAt)

	got, err := s.store.GetDataSource(s.ctx, created.ID)
	s.Require().NoError(err)
	s.Equal(created, got)

	list, err := s.store.ListDataSources(s.ctx)
	s.Require().NoError(err)
	s.Require().Len(list, 1)
	s.Equal(created.ID, list[0].ID)

	var stored []byte
	s.Require().NoError(s.pool.QueryRow(s.ctx, `SELECT secret_enc FROM data_sources WHERE id = $1`, created.ID).Scan(&stored))
	s.NotContains(string(stored), "top-secret")

	secretsBack, err := s.store.DataSourceSecrets(s.ctx, created.ID)
	s.Require().NoError(err)
	s.Equal(map[string]string{"token": "top-secret", "headers": "X-Scope-OrgID: t1"}, secretsBack)
}

func (s *DataSourceStoreSuite) TestASourceWithoutSecretsStoresNoBlob() {
	created, err := s.store.CreateDataSource(s.ctx, domain.DataSource{
		Kind: domain.DataSourceKindPrometheus, Name: "open", Config: map[string]string{"url": "http://localhost:9090"},
	}, nil)
	s.Require().NoError(err)
	s.Equal([]string{}, created.SecretsSet)

	var stored []byte
	s.Require().NoError(s.pool.QueryRow(s.ctx, `SELECT secret_enc FROM data_sources WHERE id = $1`, created.ID).Scan(&stored))
	s.Nil(stored)

	got, err := s.store.DataSourceSecrets(s.ctx, created.ID)
	s.Require().NoError(err)
	s.Empty(got)
}

func (s *DataSourceStoreSuite) TestUpdateKeepsSecretsUnlessGiven() {
	created, err := s.store.CreateDataSource(s.ctx, domain.DataSource{
		Kind: domain.DataSourceKindPrometheus, Name: "a", Config: map[string]string{"url": "http://a:9090"},
	}, map[string]string{"token": "one"})
	s.Require().NoError(err)

	verified := time.Now().UTC().Truncate(time.Microsecond)
	created.Name = "renamed"
	created.Status = domain.DataSourceOK
	created.Info = domain.DataSourceInfo{Version: "2.53.0", Detail: "Prometheus API"}
	created.VerifiedAt = &verified
	updated, err := s.store.UpdateDataSource(s.ctx, created, nil)
	s.Require().NoError(err)
	s.Equal("renamed", updated.Name)
	s.Equal(domain.DataSourceOK, updated.Status)
	s.Equal(created.Info, updated.Info)
	s.Require().NotNil(updated.VerifiedAt)
	s.True(verified.Equal(*updated.VerifiedAt))
	s.Equal([]string{"token"}, updated.SecretsSet)
	kept, err := s.store.DataSourceSecrets(s.ctx, created.ID)
	s.Require().NoError(err)
	s.Equal(map[string]string{"token": "one"}, kept)

	updated, err = s.store.UpdateDataSource(s.ctx, updated, map[string]string{})
	s.Require().NoError(err)
	s.Equal([]string{}, updated.SecretsSet)
	cleared, err := s.store.DataSourceSecrets(s.ctx, created.ID)
	s.Require().NoError(err)
	s.Empty(cleared)
}

func (s *DataSourceStoreSuite) TestUnknownIDsAreNotFound() {
	id := uuid.New()
	_, err := s.store.GetDataSource(s.ctx, id)
	s.ErrorIs(err, port.ErrNotFound)
	_, err = s.store.UpdateDataSource(s.ctx, domain.DataSource{ID: id, Kind: "prometheus", Name: "x", Status: domain.DataSourceOK}, nil)
	s.ErrorIs(err, port.ErrNotFound)
	s.ErrorIs(s.store.DeleteDataSource(s.ctx, id), port.ErrNotFound)
	_, err = s.store.DataSourceSecrets(s.ctx, id)
	s.ErrorIs(err, port.ErrNotFound)
}

func (s *DataSourceStoreSuite) TestDeleteRemovesTheRow() {
	created, err := s.store.CreateDataSource(s.ctx, domain.DataSource{
		Kind: domain.DataSourceKindPrometheus, Name: "gone", Config: map[string]string{"url": "http://a:9090"},
	}, nil)
	s.Require().NoError(err)
	s.Require().NoError(s.store.DeleteDataSource(s.ctx, created.ID))
	list, err := s.store.ListDataSources(s.ctx)
	s.Require().NoError(err)
	s.Empty(list)
}

func (s *DataSourceStoreSuite) TestUserRecipesComeBackWithTheirRowID() {
	_, err := s.pool.Exec(s.ctx, `INSERT INTO observability_recipes (id, body) VALUES
		('my-exporter', '{"source_kind":"prometheus","detect":{"metric":"my_queue_depth"},"params":["queue"],"signals":{"queue.backlog":"sum(my_queue_depth{queue=\"$queue\"})"}}')`)
	s.Require().NoError(err)

	recipes, err := s.store.ListUserRecipes(s.ctx)
	s.Require().NoError(err)
	s.Require().Len(recipes, 1)
	s.Equal(domain.Recipe{
		ID:         "my-exporter",
		SourceKind: domain.DataSourceKindPrometheus,
		Detect:     domain.RecipeDetect{Metric: "my_queue_depth"},
		Params:     []string{"queue"},
		Signals:    map[string]string{"queue.backlog": `sum(my_queue_depth{queue="$queue"})`},
		Origin:     domain.RecipeOriginUser,
	}, recipes[0])
}

func (s *DataSourceStoreSuite) TestAVerifyOfAReplacedConfigDoesNotRevertTheNewerWrite() {
	created, err := s.store.CreateDataSource(s.ctx, domain.DataSource{
		Kind: domain.DataSourceKindPrometheus, Name: "prod", Config: map[string]string{"url": "http://old:9090"},
	}, nil)
	s.Require().NoError(err)
	snapshot := created

	created.Config = map[string]string{"url": "http://new:9090"}
	patched, err := s.store.UpdateDataSource(s.ctx, created, nil)
	s.Require().NoError(err)
	s.True(patched.UpdatedAt.After(snapshot.UpdatedAt))

	verified := time.Now().UTC().Truncate(time.Microsecond)
	_, err = s.store.RecordVerification(s.ctx, snapshot.ID, domain.DataSourceVerification{
		Status: domain.DataSourceOK, Info: domain.DataSourceInfo{Version: "old"}, VerifiedAt: &verified,
	}, snapshot.UpdatedAt)
	s.ErrorIs(err, port.ErrStaleDataSource)

	got, err := s.store.GetDataSource(s.ctx, snapshot.ID)
	s.Require().NoError(err)
	s.Equal(map[string]string{"url": "http://new:9090"}, got.Config)
	s.Equal(domain.DataSourceUnverified, got.Status)
	s.Nil(got.VerifiedAt)

	recorded, err := s.store.RecordVerification(s.ctx, snapshot.ID, domain.DataSourceVerification{
		Status: domain.DataSourceOK, Info: domain.DataSourceInfo{Version: "2.53.0"}, VerifiedAt: &verified,
	}, patched.UpdatedAt)
	s.Require().NoError(err)
	s.Equal(domain.DataSourceOK, recorded.Status)
	s.Equal(map[string]string{"url": "http://new:9090"}, recorded.Config)
	s.True(patched.UpdatedAt.Equal(recorded.UpdatedAt), "a verify leaves the config version alone")

	failed, err := s.store.RecordVerification(s.ctx, snapshot.ID, domain.DataSourceVerification{
		Status: domain.DataSourceError, StatusDetail: "timed out",
	}, patched.UpdatedAt)
	s.Require().NoError(err)
	s.Equal(domain.DataSourceError, failed.Status)
	s.Require().NotNil(failed.VerifiedAt)
	s.True(verified.Equal(*failed.VerifiedAt), "a failure keeps the last success")
}

func (s *DataSourceStoreSuite) TestTwoWritesInARowAlwaysMoveTheVersion() {
	created, err := s.store.CreateDataSource(s.ctx, domain.DataSource{
		Kind: domain.DataSourceKindPrometheus, Name: "a", Config: map[string]string{"url": "http://a:9090"},
	}, nil)
	s.Require().NoError(err)
	first, err := s.store.UpdateDataSource(s.ctx, created, nil)
	s.Require().NoError(err)
	second, err := s.store.UpdateDataSource(s.ctx, first, nil)
	s.Require().NoError(err)
	s.True(second.UpdatedAt.After(first.UpdatedAt))
}

func (s *DataSourceStoreSuite) TestRecordVerificationOfAnUnknownSourceIsNotFound() {
	_, err := s.store.RecordVerification(s.ctx, uuid.New(), domain.DataSourceVerification{Status: domain.DataSourceOK}, time.Now())
	s.ErrorIs(err, port.ErrNotFound)
}

func (s *DataSourceStoreSuite) TestCloudSourceLinksAreOncePerAccountAndKindAndSurviveADeletionAsADismissal() {
	var accountID uuid.UUID
	s.Require().NoError(s.pool.QueryRow(s.ctx,
		`INSERT INTO cloud_accounts (provider, label, status, secret_enc) VALUES ('vercel', 'acme', 'ok', '\x00') RETURNING id`).Scan(&accountID))
	s.T().Cleanup(func() { _, _ = s.pool.Exec(s.ctx, `DELETE FROM cloud_accounts WHERE id = $1`, accountID) })

	ds, err := s.store.CreateDataSource(s.ctx, domain.DataSource{
		Kind: domain.DataSourceKindVercel, Name: "v", Config: map[string]string{"cloud_account_id": accountID.String()},
	}, nil)
	s.Require().NoError(err)

	linked, err := s.store.LinkCloudSource(s.ctx, accountID, domain.DataSourceKindVercel, ds.ID)
	s.Require().NoError(err)
	s.True(linked)
	again, err := s.store.LinkCloudSource(s.ctx, accountID, domain.DataSourceKindVercel, ds.ID)
	s.Require().NoError(err)
	s.False(again)

	links, err := s.store.ListCloudSourceLinks(s.ctx)
	s.Require().NoError(err)
	s.Require().Len(links, 1)
	s.Equal(ds.ID, *links[0].DataSourceID)

	s.Require().NoError(s.store.DeleteDataSource(s.ctx, ds.ID))
	links, err = s.store.ListCloudSourceLinks(s.ctx)
	s.Require().NoError(err)
	s.Require().Len(links, 1)
	s.Nil(links[0].DataSourceID, "a deleted source leaves its link behind, as the mark that it was dismissed")

	_, err = s.pool.Exec(s.ctx, `DELETE FROM cloud_accounts WHERE id = $1`, accountID)
	s.Require().NoError(err)
	links, err = s.store.ListCloudSourceLinks(s.ctx)
	s.Require().NoError(err)
	s.Empty(links, "disconnecting the account takes its links with it")
}
