package release

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestWatchParksAWatchedRelease(t *testing.T) {
	store := newFakeReleaseStore()
	svc := New(Deps{Store: store})
	repositoryID := uuid.New()
	created, err := store.Create(context.Background(), domain.Release{
		RepositoryID: repositoryID, Status: domain.ReleaseVerifying, Version: "abc123",
	}, nil)
	require.NoError(t, err)

	r, block, err := svc.Watch(context.Background(), created.ID)
	require.NoError(t, err)
	require.NotNil(t, block)
	assert.Equal(t, domain.ResourceReleaseWatch, block.Resource)
	assert.Equal(t, domain.ReleaseVerifying, r.Status)
}

func TestWatchReturnsTheReleaseWithoutABlockWhenSettled(t *testing.T) {
	store := newFakeReleaseStore()
	svc := New(Deps{Store: store})
	repositoryID := uuid.New()
	created, err := store.Create(context.Background(), domain.Release{
		RepositoryID: repositoryID, Status: domain.ReleaseAwaitingVerdict,
	}, nil)
	require.NoError(t, err)

	r, block, err := svc.Watch(context.Background(), created.ID)
	require.NoError(t, err)
	assert.Nil(t, block)
	assert.Equal(t, domain.ReleaseAwaitingVerdict, r.Status)
}

// settlesOnSecondGetStore reports a release as still Watched() on its first
// Get and settled on every Get after — modelling the sweeper settling a
// release in the narrow window between Watch's two reads.
type settlesOnSecondGetStore struct {
	*fakeReleaseStore
	releaseID uuid.UUID
	calls     int
}

func (s *settlesOnSecondGetStore) Get(ctx context.Context, id uuid.UUID) (domain.Release, error) {
	r, err := s.fakeReleaseStore.Get(ctx, id)
	if err != nil || id != s.releaseID {
		return r, err
	}
	s.calls++
	if s.calls == 1 {
		r.Status = domain.ReleaseVerifying
	} else {
		r.Status = domain.ReleaseAwaitingVerdict
	}
	return r, nil
}

// Watch must re-read the release right before reporting the park: a
// release the sweeper settles between the first read and the decision to
// park must come back as settled, not stuck behind a stale park.
func TestWatchReReadsBeforeParkingSoASweeperSettleWinsTheRace(t *testing.T) {
	fake := newFakeReleaseStore()
	repositoryID := uuid.New()
	created, err := fake.Create(context.Background(), domain.Release{
		RepositoryID: repositoryID, Status: domain.ReleaseVerifying,
	}, nil)
	require.NoError(t, err)
	store := &settlesOnSecondGetStore{fakeReleaseStore: fake, releaseID: created.ID}
	svc := New(Deps{Store: store})

	r, block, err := svc.Watch(context.Background(), created.ID)
	require.NoError(t, err)
	assert.Nil(t, block, "the second, fresher read must win over the first")
	assert.Equal(t, domain.ReleaseAwaitingVerdict, r.Status)
	assert.Equal(t, 2, store.calls, "Watch must read twice before deciding to park")
}
