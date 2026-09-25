package release

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type attributionFixture struct {
	svc   *Service
	store *fakeReleaseStore
	clock *steppableClock
}

func newAttributionFixture(window time.Duration) *attributionFixture {
	store := newFakeReleaseStore()
	clock := newSteppableClock(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	svc := New(Deps{
		Store:        store,
		Clock:        clock.Now,
		HealthWindow: window,
	})
	return &attributionFixture{svc: svc, store: store, clock: clock}
}

func releasedRelease(repositoryID uuid.UUID, sha string, finishedAt time.Time, tasks []domain.ReleaseTaskRef) domain.Release {
	return domain.Release{
		RepositoryID: repositoryID,
		Status:       domain.ReleaseReleased,
		CommitSHA:    sha,
		FinishedAt:   &finishedAt,
		Tasks:        tasks,
	}
}

func TestHealthWindowDefaultsTo15Minutes(t *testing.T) {
	f := newAttributionFixture(0)
	assert.Equal(t, DefaultHealthWindow, f.svc.HealthWindow())
	assert.Equal(t, 15*time.Minute, f.svc.HealthWindow())
}

func TestHealthWindowHonoursAnExplicitValue(t *testing.T) {
	f := newAttributionFixture(30 * time.Minute)
	assert.Equal(t, 30*time.Minute, f.svc.HealthWindow())
}

func TestAttributeReleaseFindsTheNewestTaskOfTheReleaseWithinTheWindow(t *testing.T) {
	f := newAttributionFixture(15 * time.Minute)
	repositoryID := uuid.New()
	onset := f.clock.Now()
	finishedAt := onset.Add(-5 * time.Minute)
	oldest := domain.ReleaseTaskRef{ID: uuid.New(), Key: "T-1", Title: "First"}
	newest := domain.ReleaseTaskRef{ID: uuid.New(), Key: "T-2", Title: "Second"}
	created, err := f.store.Create(context.Background(), releasedRelease(repositoryID, mergeSHA, finishedAt, nil), []uuid.UUID{oldest.ID, newest.ID})
	require.NoError(t, err)
	// fakeReleaseStore.Create rebuilds Tasks as bare {ID} refs from taskIDs;
	// this test needs the added_at-ascending shape port.ReleaseStore
	// actually fills (key/title), so it is written straight into the fake's
	// map rather than through the interface.
	created.Tasks = []domain.ReleaseTaskRef{oldest, newest}
	f.store.releases[created.ID] = created

	attribution, ok := f.svc.AttributeRelease(context.Background(), repositoryID, domain.DeployEnvProd, onset)
	require.True(t, ok)
	assert.Equal(t, newest.ID, attribution.TaskID)
	assert.Equal(t, "T-2", attribution.TaskKey)
	assert.Equal(t, mergeSHA, attribution.MergeSHA)
	assert.Equal(t, domain.DeployEnvProd, attribution.Env)
	assert.True(t, attribution.DeployedAt.Equal(finishedAt))
}

func TestAttributeReleaseDeclinesOutsideTheHealthWindow(t *testing.T) {
	f := newAttributionFixture(15 * time.Minute)
	repositoryID := uuid.New()
	onset := f.clock.Now()
	finishedAt := onset.Add(-20 * time.Minute)
	task := domain.ReleaseTaskRef{ID: uuid.New(), Key: "T-1"}
	_, err := f.store.Create(context.Background(), releasedRelease(repositoryID, mergeSHA, finishedAt, []domain.ReleaseTaskRef{task}), []uuid.UUID{task.ID})
	require.NoError(t, err)

	_, ok := f.svc.AttributeRelease(context.Background(), repositoryID, domain.DeployEnvProd, onset)
	assert.False(t, ok)
}

func TestAttributeReleaseDeclinesWhenTheReleaseFinishesAfterOnset(t *testing.T) {
	f := newAttributionFixture(15 * time.Minute)
	repositoryID := uuid.New()
	onset := f.clock.Now()
	finishedAt := onset.Add(1 * time.Minute)
	task := domain.ReleaseTaskRef{ID: uuid.New(), Key: "T-1"}
	_, err := f.store.Create(context.Background(), releasedRelease(repositoryID, mergeSHA, finishedAt, []domain.ReleaseTaskRef{task}), []uuid.UUID{task.ID})
	require.NoError(t, err)

	_, ok := f.svc.AttributeRelease(context.Background(), repositoryID, domain.DeployEnvProd, onset)
	assert.False(t, ok)
}

func TestAttributeReleaseIgnoresNonProdEnvironments(t *testing.T) {
	f := newAttributionFixture(15 * time.Minute)
	repositoryID := uuid.New()
	onset := f.clock.Now()
	finishedAt := onset.Add(-1 * time.Minute)
	task := domain.ReleaseTaskRef{ID: uuid.New(), Key: "T-1"}
	_, err := f.store.Create(context.Background(), releasedRelease(repositoryID, mergeSHA, finishedAt, []domain.ReleaseTaskRef{task}), []uuid.UUID{task.ID})
	require.NoError(t, err)

	_, ok := f.svc.AttributeRelease(context.Background(), repositoryID, "staging", onset)
	assert.False(t, ok, "a release has no notion of any environment but production")
}

func TestAttributeReleaseIgnoresReleasesNotInReleasedStatus(t *testing.T) {
	f := newAttributionFixture(15 * time.Minute)
	repositoryID := uuid.New()
	onset := f.clock.Now()
	finishedAt := onset.Add(-1 * time.Minute)
	task := domain.ReleaseTaskRef{ID: uuid.New(), Key: "T-1"}
	r := releasedRelease(repositoryID, mergeSHA, finishedAt, []domain.ReleaseTaskRef{task})
	r.Status = domain.ReleaseRolledBack
	_, err := f.store.Create(context.Background(), r, []uuid.UUID{task.ID})
	require.NoError(t, err)

	_, ok := f.svc.AttributeRelease(context.Background(), repositoryID, domain.DeployEnvProd, onset)
	assert.False(t, ok)
}

func TestAttributeReleasePicksTheMostRecentlyFinishedAmongCandidates(t *testing.T) {
	f := newAttributionFixture(15 * time.Minute)
	repositoryID := uuid.New()
	onset := f.clock.Now()

	older := domain.ReleaseTaskRef{ID: uuid.New(), Key: "T-1"}
	_, err := f.store.Create(context.Background(), releasedRelease(repositoryID, "older00000000000000000000000000000000000", onset.Add(-10*time.Minute), []domain.ReleaseTaskRef{older}), []uuid.UUID{older.ID})
	require.NoError(t, err)

	newer := domain.ReleaseTaskRef{ID: uuid.New(), Key: "T-2"}
	_, err = f.store.Create(context.Background(), releasedRelease(repositoryID, "newer00000000000000000000000000000000000", onset.Add(-2*time.Minute), []domain.ReleaseTaskRef{newer}), []uuid.UUID{newer.ID})
	require.NoError(t, err)

	attribution, ok := f.svc.AttributeRelease(context.Background(), repositoryID, domain.DeployEnvProd, onset)
	require.True(t, ok)
	assert.Equal(t, newer.ID, attribution.TaskID)
	assert.Equal(t, "newer00000000000000000000000000000000000", attribution.MergeSHA)
}
