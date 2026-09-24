package projectmodel

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestRelinkConfirmsExactHostMatchAcrossRepositories(t *testing.T) {
	svc, store, repos, _, _, _ := newB2Service(t)
	ctx := context.Background()
	envs := newFakeEnvironmentStore()
	svc.SetEnvironmentReader(envs)

	repoA := b2SeedRepo(t, repos, "storefront")
	repoB := b2SeedRepo(t, repos, "billing")
	from := b2SeedComponent(t, store, domain.Component{RepositoryID: repoA.ID, Path: "api", Status: domain.ComponentStatusActive})
	to := b2SeedComponent(t, store, domain.Component{RepositoryID: repoB.ID, Path: "svc", Status: domain.ComponentStatusActive})

	envs.seed(domain.ComponentEnvironment{
		RepositoryID: repoB.ID, ComponentID: to.ID, Environment: domain.EnvironmentProduction,
		Status: domain.LinkConfirmed, URL: "https://billing-svc.example.com",
	})

	link, err := store.SaveLink(ctx, domain.ComponentLink{
		RepositoryID: repoA.ID, FromComponentID: from.ID, Protocol: domain.LinkHTTP,
		Status: domain.LinkSuggested, Confidence: domain.ConfidenceMedium,
		TargetHost: "www.Billing-Svc.example.com", Hint: "billing-svc.example.com",
	})
	require.NoError(t, err)

	require.NoError(t, svc.Relink(ctx))

	got, err := store.GetLink(ctx, link.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.LinkConfirmed, got.Status)
	assert.True(t, got.AutoConfirmed)
	assert.Equal(t, domain.ConfidenceExact, got.Confidence)
	require.NotNil(t, got.ToComponentID)
	assert.Equal(t, to.ID, *got.ToComponentID)
	assert.Contains(t, got.Reason, "billing/svc")
	assert.Contains(t, got.Reason, "production")
	assert.Empty(t, got.Hint)
}

func TestRelinkLeavesAmbiguousHostMatchAlone(t *testing.T) {
	svc, store, repos, _, _, _ := newB2Service(t)
	ctx := context.Background()
	envs := newFakeEnvironmentStore()
	svc.SetEnvironmentReader(envs)

	repoA := b2SeedRepo(t, repos, "storefront")
	repoB := b2SeedRepo(t, repos, "billing-1")
	repoC := b2SeedRepo(t, repos, "billing-2")
	from := b2SeedComponent(t, store, domain.Component{RepositoryID: repoA.ID, Path: "api", Status: domain.ComponentStatusActive})
	toB := b2SeedComponent(t, store, domain.Component{RepositoryID: repoB.ID, Path: "svc", Status: domain.ComponentStatusActive})
	toC := b2SeedComponent(t, store, domain.Component{RepositoryID: repoC.ID, Path: "svc", Status: domain.ComponentStatusActive})

	envs.seed(domain.ComponentEnvironment{
		RepositoryID: repoB.ID, ComponentID: toB.ID, Environment: domain.EnvironmentProduction,
		Status: domain.LinkConfirmed, URL: "https://shared.example.com",
	})
	envs.seed(domain.ComponentEnvironment{
		RepositoryID: repoC.ID, ComponentID: toC.ID, Environment: domain.EnvironmentProduction,
		Status: domain.LinkConfirmed, URL: "https://shared.example.com",
	})

	link, err := store.SaveLink(ctx, domain.ComponentLink{
		RepositoryID: repoA.ID, FromComponentID: from.ID, Protocol: domain.LinkHTTP,
		Status: domain.LinkSuggested, Confidence: domain.ConfidenceMedium,
		TargetHost: "shared.example.com",
	})
	require.NoError(t, err)

	require.NoError(t, svc.Relink(ctx))

	got, err := store.GetLink(ctx, link.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.LinkSuggested, got.Status, "two candidates is the human's call, not the platform's")
	assert.Nil(t, got.ToComponentID)
}

func TestRelinkLocalDevPortSuggestsWithinSameProjectOnly(t *testing.T) {
	svc, store, repos, _, _, _ := newB2Service(t)
	ctx := context.Background()
	svc.SetEnvironmentReader(newFakeEnvironmentStore())

	projectID := uuid.New()
	repoA := repos.put(domain.Repository{ID: uuid.New(), Name: "web", RootPath: t.TempDir(), ProjectIDs: []uuid.UUID{projectID}})
	repoB := repos.put(domain.Repository{ID: uuid.New(), Name: "api", RootPath: t.TempDir(), ProjectIDs: []uuid.UUID{projectID}})
	repoOutside := repos.put(domain.Repository{ID: uuid.New(), Name: "unrelated", RootPath: t.TempDir()})

	from := b2SeedComponent(t, store, domain.Component{RepositoryID: repoA.ID, Path: ".", Status: domain.ComponentStatusActive})
	inProject := b2SeedComponent(t, store, domain.Component{
		RepositoryID: repoB.ID, Path: ".", Status: domain.ComponentStatusActive,
		Stack: domain.Detected(domain.ComponentStack{DevPort: 5173}, domain.ConfidenceHigh),
	})
	b2SeedComponent(t, store, domain.Component{
		RepositoryID: repoOutside.ID, Path: ".", Status: domain.ComponentStatusActive,
		Stack: domain.Detected(domain.ComponentStack{DevPort: 5173}, domain.ConfidenceHigh),
	})

	link, err := store.SaveLink(ctx, domain.ComponentLink{
		RepositoryID: repoA.ID, FromComponentID: from.ID, Protocol: domain.LinkHTTP,
		Status: domain.LinkSuggested, Confidence: domain.ConfidenceMedium,
		TargetHost: "localhost", TargetPort: 5173,
	})
	require.NoError(t, err)

	require.NoError(t, svc.Relink(ctx))

	got, err := store.GetLink(ctx, link.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.LinkSuggested, got.Status, "a dev-port match is never more than a suggestion")
	assert.Equal(t, domain.ConfidenceMedium, got.Confidence)
	assert.False(t, got.AutoConfirmed)
	require.NotNil(t, got.ToComponentID)
	assert.Equal(t, inProject.ID, *got.ToComponentID, "only the same-project component may match, never the unrelated repository")
}

func TestRelinkNeverTouchesDismissedOrConfirmedResolvedLinks(t *testing.T) {
	svc, store, repos, _, _, _ := newB2Service(t)
	ctx := context.Background()
	envs := newFakeEnvironmentStore()
	svc.SetEnvironmentReader(envs)

	repoA := b2SeedRepo(t, repos, "storefront")
	repoB := b2SeedRepo(t, repos, "billing")
	from := b2SeedComponent(t, store, domain.Component{RepositoryID: repoA.ID, Path: "api", Status: domain.ComponentStatusActive})
	to := b2SeedComponent(t, store, domain.Component{RepositoryID: repoB.ID, Path: "svc", Status: domain.ComponentStatusActive})
	otherTarget := b2SeedComponent(t, store, domain.Component{RepositoryID: repoB.ID, Path: "other", Status: domain.ComponentStatusActive})

	envs.seed(domain.ComponentEnvironment{
		RepositoryID: repoB.ID, ComponentID: to.ID, Environment: domain.EnvironmentProduction,
		Status: domain.LinkConfirmed, URL: "https://billing-svc.example.com",
	})

	dismissed, err := store.SaveLink(ctx, domain.ComponentLink{
		RepositoryID: repoA.ID, FromComponentID: from.ID, Protocol: domain.LinkHTTP,
		Status: domain.LinkDismissed, TargetHost: "billing-svc.example.com",
	})
	require.NoError(t, err)

	resolved, err := store.SaveLink(ctx, domain.ComponentLink{
		RepositoryID: repoA.ID, FromComponentID: from.ID, Protocol: domain.LinkHTTP,
		Status: domain.LinkConfirmed, ToComponentID: &otherTarget.ID, Confidence: domain.ConfidenceMedium,
		TargetHost: "billing-svc.example.com",
	})
	require.NoError(t, err)

	require.NoError(t, svc.Relink(ctx))

	gotDismissed, err := store.GetLink(ctx, dismissed.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.LinkDismissed, gotDismissed.Status)
	assert.Nil(t, gotDismissed.ToComponentID)

	gotResolved, err := store.GetLink(ctx, resolved.ID)
	require.NoError(t, err)
	assert.Equal(t, otherTarget.ID, *gotResolved.ToComponentID, "a confirmed-and-resolved link is the human's final word")
}
