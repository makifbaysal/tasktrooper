package release

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type countingListStore struct {
	*fakeReleaseStore
	lists atomic.Int32
}

func (c *countingListStore) List(ctx context.Context, f domain.ReleaseListFilter) ([]domain.Release, error) {
	c.lists.Add(1)
	return c.fakeReleaseStore.List(ctx, f)
}

type SweepCadenceSuite struct {
	suite.Suite
	fx *sweepFixture
}

func TestSweepCadenceSuite(t *testing.T) {
	suite.Run(t, new(SweepCadenceSuite))
}

func (s *SweepCadenceSuite) SetupTest() {
	s.fx = newSweepFixture()
}

func (s *SweepCadenceSuite) kicked() bool {
	select {
	case <-s.fx.svc.watchKick:
		return true
	default:
		return false
	}
}

func (s *SweepCadenceSuite) TestNothingWatchedSweepsAtTheIdleInterval() {
	s.Equal(IdleSweepInterval, s.fx.svc.nextSweepIn(DefaultSweepInterval))

	s.fx.svc.SweepOnce(context.Background())

	s.Equal(5*time.Minute, s.fx.svc.nextSweepIn(DefaultSweepInterval))
}

func (s *SweepCadenceSuite) TestAWatchedReleaseKeepsTheActiveIntervalUntilItSettles() {
	repositoryID := uuid.New()
	created, err := s.fx.store.Create(context.Background(), deployingRelease(repositoryID, s.fx.clock.Now()), []uuid.UUID{s.fx.withTask(repositoryID).ID})
	s.Require().NoError(err)

	s.fx.svc.SweepOnce(context.Background())
	s.Equal(DefaultSweepInterval, s.fx.svc.nextSweepIn(DefaultSweepInterval))

	created.Status = domain.ReleaseAwaitingVerdict
	_, err = s.fx.store.Update(context.Background(), created, domain.ReleaseDeploying)
	s.Require().NoError(err)
	s.fx.svc.SweepOnce(context.Background())

	s.Equal(IdleSweepInterval, s.fx.svc.nextSweepIn(DefaultSweepInterval))
}

func (s *SweepCadenceSuite) TestAWatchStartingWhileIdleWakesTheSweeper() {
	ctx := context.Background()
	tests := []struct {
		name  string
		start func(repositoryID uuid.UUID)
	}{
		{"a release created deploying", func(repositoryID uuid.UUID) {
			_, err := s.fx.svc.store.Create(ctx, deployingRelease(repositoryID, s.fx.clock.Now()), nil)
			s.Require().NoError(err)
		}},
		{"a release moved into rolling back", func(repositoryID uuid.UUID) {
			r, err := s.fx.store.Create(ctx, domain.Release{RepositoryID: repositoryID, Status: domain.ReleaseAwaitingVerdict}, nil)
			s.Require().NoError(err)
			r.Status = domain.ReleaseRollingBack
			_, err = s.fx.svc.store.Update(ctx, r, domain.ReleaseAwaitingVerdict)
			s.Require().NoError(err)
		}},
		{"an agent watching a verifying release", func(repositoryID uuid.UUID) {
			r, err := s.fx.store.Create(ctx, domain.Release{RepositoryID: repositoryID, Status: domain.ReleaseVerifying}, nil)
			s.Require().NoError(err)
			_, block, err := s.fx.svc.Watch(ctx, r.ID)
			s.Require().NoError(err)
			s.Require().NotNil(block)
		}},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.SetupTest()
			s.fx.svc.SweepOnce(ctx)
			s.Require().False(s.fx.svc.watchActive())

			tt.start(uuid.New())

			s.True(s.kicked())
			s.Equal(DefaultSweepInterval, s.fx.svc.nextSweepIn(DefaultSweepInterval))
		})
	}
}

func (s *SweepCadenceSuite) TestASettledWriteDoesNotWakeTheSweeper() {
	r, err := s.fx.store.Create(context.Background(), domain.Release{RepositoryID: uuid.New(), Status: domain.ReleaseVerifying}, nil)
	s.Require().NoError(err)
	r.Status = domain.ReleaseAwaitingVerdict

	_, err = s.fx.svc.store.Update(context.Background(), r, domain.ReleaseVerifying)

	s.Require().NoError(err)
	s.False(s.kicked())
	s.False(s.fx.svc.watchActive())
}

func (s *SweepCadenceSuite) TestAWatchStartedDuringAnEmptySweepKeepsTheActiveInterval() {
	notes := s.fx.svc.watchNoteCount()
	s.fx.svc.noteWatching()

	s.fx.svc.settleCadence(notes, false)

	s.Equal(DefaultSweepInterval, s.fx.svc.nextSweepIn(DefaultSweepInterval))
}

func (s *SweepCadenceSuite) TestStartSleepsWhileIdleAndComesBackWhenAWatchStarts() {
	store := &countingListStore{fakeReleaseStore: newFakeReleaseStore()}
	svc := New(Deps{Store: store, DeployStatus: newFakeDeployStatus(), Clock: s.fx.clock.Now})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	const active = 20 * time.Millisecond

	svc.Start(ctx, active)
	s.Require().Eventually(func() bool { return store.lists.Load() >= 1 }, time.Second, 5*time.Millisecond)
	idle := store.lists.Load()
	time.Sleep(10 * active)
	s.Equal(idle, store.lists.Load(), "an idle sweeper must not tick at the active interval")

	_, err := svc.store.Create(ctx, deployingRelease(uuid.New(), s.fx.clock.Now()), nil)
	s.Require().NoError(err)

	s.Eventually(func() bool { return store.lists.Load() > idle }, time.Second, 5*time.Millisecond)
}
