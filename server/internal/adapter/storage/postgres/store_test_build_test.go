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
	"github.com/makifbaysal/tasktrooper/server/internal/platform/database"
)

type StoreTestBuildStoreSuite struct {
	suite.Suite
	ctx    context.Context
	cancel context.CancelFunc
	pg     *database.Embedded
	pool   *pgxpool.Pool
	db     *postgres.DB
	store  *postgres.StoreTestBuildStore
	repoID uuid.UUID
}

func TestStoreTestBuildStoreSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(StoreTestBuildStoreSuite))
}

func (s *StoreTestBuildStoreSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 3*time.Minute)
	pg, err := newTestDatabase(s.ctx)
	s.Require().NoError(err)
	s.pg = pg
	pool, err := pgxpool.New(s.ctx, pg.DSN())
	s.Require().NoError(err)
	s.pool = pool
	s.db = postgres.NewDB(pool)
	s.store = postgres.NewStoreTestBuildStore(s.db)
}

func (s *StoreTestBuildStoreSuite) SetupTest() {
	name := "store-test-build-" + uuid.NewString()[:8]
	repo, err := postgres.NewRepositoryStore(s.db).Create(s.ctx, name, "", "/tmp/"+name, "", "")
	s.Require().NoError(err)
	s.repoID = repo.ID
}

func (s *StoreTestBuildStoreSuite) TearDownSuite() {
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

func (s *StoreTestBuildStoreSuite) build(platform, number string, seq int64, attempt int) domain.StoreTestBuild {
	return domain.StoreTestBuild{
		RepositoryID: s.repoID,
		Platform:     platform,
		TaskKey:      "T-54",
		TaskNumber:   54,
		Attempt:      attempt,
		Sequence:     seq,
		BuildNumber:  number,
		Status:       domain.TestBuildQueued,
		Trigger:      domain.TestBuildTriggerManual,
	}
}

func (s *StoreTestBuildStoreSuite) TestRoundTripAndCounters() {
	created, err := s.store.Create(s.ctx, s.build(domain.MobileStorePlatformIOS, "412.54.1", 412, 1))
	s.Require().NoError(err)
	s.NotEqual(uuid.Nil, created.ID)
	s.Equal([]string{}, created.Groups)

	now := time.Now()
	created.Status = domain.TestBuildReady
	created.Groups = []string{"group-a"}
	created.ArtifactPath = "/tmp/app.aab"
	created.FinishedAt = &now
	updated, err := s.store.Update(s.ctx, created)
	s.Require().NoError(err)
	s.Equal(domain.TestBuildReady, updated.Status)
	s.Equal([]string{"group-a"}, updated.Groups)
	s.True(updated.HasArtifact)

	_, err = s.store.Create(s.ctx, s.build(domain.MobileStorePlatformIOS, "413.54.2", 413, 2))
	s.Require().NoError(err)
	_, err = s.store.Create(s.ctx, s.build(domain.MobileStorePlatformAndroid, "900", 900, 1))
	s.Require().NoError(err)

	maxSeq, err := s.store.MaxSequence(s.ctx, s.repoID, domain.MobileStorePlatformIOS)
	s.Require().NoError(err)
	s.Equal(int64(413), maxSeq)

	ios, err := s.store.List(s.ctx, s.repoID, domain.MobileStorePlatformIOS, nil, 10)
	s.Require().NoError(err)
	s.Require().Len(ios, 2)
	s.Equal("413.54.2", ios[0].BuildNumber, "newest first")

	unfinished, err := s.store.ListUnfinished(s.ctx)
	s.Require().NoError(err)
	for _, b := range unfinished {
		s.NotEqual(domain.TestBuildReady, b.Status)
	}
}

func (s *StoreTestBuildStoreSuite) TestABuildNumberIsUsedOnce() {
	_, err := s.store.Create(s.ctx, s.build(domain.MobileStorePlatformIOS, "500.1.1", 500, 1))
	s.Require().NoError(err)
	_, err = s.store.Create(s.ctx, s.build(domain.MobileStorePlatformIOS, "500.1.1", 500, 1))
	s.Error(err, "the store would refuse a duplicate build number anyway; refusing it here keeps the counter honest")
}

func (s *StoreTestBuildStoreSuite) TestAutoGroupsDefaultUntilSet() {
	_, set, err := s.store.AutoGroups(s.ctx, s.repoID, domain.MobileStorePlatformAndroid)
	s.Require().NoError(err)
	s.False(set)

	s.Require().NoError(s.store.SetAutoGroups(s.ctx, s.repoID, domain.MobileStorePlatformAndroid, []string{"internal"}))
	groups, set, err := s.store.AutoGroups(s.ctx, s.repoID, domain.MobileStorePlatformAndroid)
	s.Require().NoError(err)
	s.True(set)
	s.Equal([]string{"internal"}, groups)

	s.Require().NoError(s.store.SetAutoGroups(s.ctx, s.repoID, domain.MobileStorePlatformAndroid, nil))
	groups, set, err = s.store.AutoGroups(s.ctx, s.repoID, domain.MobileStorePlatformAndroid)
	s.Require().NoError(err)
	s.True(set, "an explicit empty choice is not the default")
	s.Empty(groups)
}

func (s *StoreTestBuildStoreSuite) TestHumanUATStagesCarryTheBehaviour() {
	var n int
	err := s.pool.QueryRow(s.ctx, `SELECT count(*) FROM workflow_stages
		WHERE column_slug = 'human_uat' AND behaviours @> '[{"key":"store_test_build_on_enter"}]'`).Scan(&n)
	s.Require().NoError(err)
	s.Equal(3, n, "task, bug and technical")
}
