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
