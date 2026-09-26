package projectmodel

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestB2AlignDeliveryToProductionOverrideNoneBecomesOnMergeVercel(t *testing.T) {
	svc, store, repos, _, _, _ := newB2Service(t)
	ctx := context.Background()
	repo := b2SeedRepo(t, repos, "demo")
	comp := b2SeedComponent(t, store, domain.Component{
		RepositoryID: repo.ID, Path: ".", Status: domain.ComponentStatusActive,
		Delivery: domain.Fact[domain.ComponentDelivery]{Override: &domain.ComponentDelivery{
			Mode:         domain.DeliveryNone,
			Verify:       domain.DeliveryVerify{SoakMinutes: 20, MaxNewErrors: 3},
			AutoRollback: true,
		}},
	})

	require.NoError(t, svc.AlignDeliveryToProduction(ctx, comp.ID, domain.CloudVercel))

	got, err := store.GetComponent(ctx, comp.ID)
	require.NoError(t, err)
	require.NotNil(t, got.Delivery.Override)
	assert.Equal(t, domain.DeliveryOnMerge, got.Delivery.Override.Mode)
	assert.Equal(t, domain.ExecutorVercel, got.Delivery.Override.Executor)
	assert.Equal(t, 20, got.Delivery.Override.Verify.SoakMinutes, "verify must be kept")
	assert.Equal(t, 3, got.Delivery.Override.Verify.MaxNewErrors, "verify must be kept")
	assert.True(t, got.Delivery.Override.AutoRollback, "auto_rollback must be kept")
	assert.Empty(t, got.Delivery.Override.Workflow)
	assert.Empty(t, got.Delivery.Override.TagPattern)
	assert.Empty(t, got.Delivery.Override.LocalCommand)
}

func TestB2AlignDeliveryToProductionOverrideBatchLocalBecomesOnMergeVercel(t *testing.T) {
	svc, store, repos, _, _, _ := newB2Service(t)
	ctx := context.Background()
	repo := b2SeedRepo(t, repos, "demo")
	comp := b2SeedComponent(t, store, domain.Component{
		RepositoryID: repo.ID, Path: ".", Status: domain.ComponentStatusActive,
		Delivery: domain.Fact[domain.ComponentDelivery]{Override: &domain.ComponentDelivery{
			Mode:         domain.DeliveryBatch,
			Executor:     domain.ExecutorLocal,
			LocalCommand: "npm run release {version}",
			Verify:       domain.DeliveryVerify{SoakMinutes: 15},
		}},
	})

	require.NoError(t, svc.AlignDeliveryToProduction(ctx, comp.ID, domain.CloudVercel))

	got, err := store.GetComponent(ctx, comp.ID)
	require.NoError(t, err)
	require.NotNil(t, got.Delivery.Override)
	assert.Equal(t, domain.DeliveryOnMerge, got.Delivery.Override.Mode)
	assert.Equal(t, domain.ExecutorVercel, got.Delivery.Override.Executor)
	assert.Empty(t, got.Delivery.Override.LocalCommand)
}

func TestB2AlignDeliveryToProductionOverrideGitHubActionsUntouched(t *testing.T) {
	svc, store, repos, _, _, _ := newB2Service(t)
	ctx := context.Background()
	repo := b2SeedRepo(t, repos, "demo")
	original := domain.ComponentDelivery{
		Mode: domain.DeliveryOnMerge, Executor: domain.ExecutorGitHubActions,
		Workflow: "deploy.yml", Verify: domain.DeliveryVerify{SoakMinutes: 10},
	}
	comp := b2SeedComponent(t, store, domain.Component{
		RepositoryID: repo.ID, Path: ".", Status: domain.ComponentStatusActive,
		Delivery: domain.Fact[domain.ComponentDelivery]{Override: &original},
	})

	require.NoError(t, svc.AlignDeliveryToProduction(ctx, comp.ID, domain.CloudVercel))

	got, err := store.GetComponent(ctx, comp.ID)
	require.NoError(t, err)
	assert.Equal(t, original, *got.Delivery.Override, "github_actions deploying to Vercel is a valid setup that must not be touched")
}

func TestB2AlignDeliveryToProductionOverrideVercelUntouched(t *testing.T) {
	svc, store, repos, _, _, _ := newB2Service(t)
	ctx := context.Background()
	repo := b2SeedRepo(t, repos, "demo")
	original := domain.ComponentDelivery{
		Mode: domain.DeliveryOnMerge, Executor: domain.ExecutorVercel,
		Verify: domain.DeliveryVerify{SoakMinutes: 10},
	}
	comp := b2SeedComponent(t, store, domain.Component{
		RepositoryID: repo.ID, Path: ".", Status: domain.ComponentStatusActive,
		Delivery: domain.Fact[domain.ComponentDelivery]{Override: &original},
	})

	require.NoError(t, svc.AlignDeliveryToProduction(ctx, comp.ID, domain.CloudVercel))

	got, err := store.GetComponent(ctx, comp.ID)
	require.NoError(t, err)
	assert.Equal(t, original, *got.Delivery.Override)
}

func TestB2AlignDeliveryToProductionNoOverrideUntouched(t *testing.T) {
	svc, store, repos, _, _, _ := newB2Service(t)
	ctx := context.Background()
	repo := b2SeedRepo(t, repos, "demo")
	detected := domain.ComponentDelivery{Mode: domain.DeliveryNone}
	comp := b2SeedComponent(t, store, domain.Component{
		RepositoryID: repo.ID, Path: ".", Status: domain.ComponentStatusActive,
		Delivery: domain.Fact[domain.ComponentDelivery]{Detected: &detected},
	})

	require.NoError(t, svc.AlignDeliveryToProduction(ctx, comp.ID, domain.CloudVercel))

	got, err := store.GetComponent(ctx, comp.ID)
	require.NoError(t, err)
	assert.Nil(t, got.Delivery.Override, "detection alone follows the environment on the next RefreshDelivery")
}

func TestB2AlignDeliveryToProductionNonVercelProviderUntouched(t *testing.T) {
	svc, store, repos, _, _, _ := newB2Service(t)
	ctx := context.Background()
	repo := b2SeedRepo(t, repos, "demo")
	original := domain.ComponentDelivery{Mode: domain.DeliveryNone}
	comp := b2SeedComponent(t, store, domain.Component{
		RepositoryID: repo.ID, Path: ".", Status: domain.ComponentStatusActive,
		Delivery: domain.Fact[domain.ComponentDelivery]{Override: &original},
	})

	require.NoError(t, svc.AlignDeliveryToProduction(ctx, comp.ID, domain.CloudGCP))

	got, err := store.GetComponent(ctx, comp.ID)
	require.NoError(t, err)
	assert.Equal(t, original, *got.Delivery.Override)
}

func TestB2AlignDeliveryToProductionMobileUntouched(t *testing.T) {
	svc, store, repos, _, _, _ := newB2Service(t)
	ctx := context.Background()
	repo := b2SeedRepo(t, repos, "demo")
	mobileRole := domain.ComponentRoleMobile
	original := domain.ComponentDelivery{Mode: domain.DeliveryBatch, Executor: domain.ExecutorStore, AutoRollback: true}
	comp := b2SeedComponent(t, store, domain.Component{
		RepositoryID: repo.ID, Path: ".", Status: domain.ComponentStatusActive,
		Role:     domain.Fact[domain.ComponentRole]{Override: &mobileRole},
		Delivery: domain.Fact[domain.ComponentDelivery]{Override: &original},
	})

	require.NoError(t, svc.AlignDeliveryToProduction(ctx, comp.ID, domain.CloudVercel))

	got, err := store.GetComponent(ctx, comp.ID)
	require.NoError(t, err)
	assert.Equal(t, original, *got.Delivery.Override)
}

func TestB2AlignDeliveryToProductionInactiveComponentUntouched(t *testing.T) {
	svc, store, repos, _, _, _ := newB2Service(t)
	ctx := context.Background()
	repo := b2SeedRepo(t, repos, "demo")
	original := domain.ComponentDelivery{Mode: domain.DeliveryNone}
	comp := b2SeedComponent(t, store, domain.Component{
		RepositoryID: repo.ID, Path: ".", Status: domain.ComponentStatusDismissed,
		Delivery: domain.Fact[domain.ComponentDelivery]{Override: &original},
	})

	require.NoError(t, svc.AlignDeliveryToProduction(ctx, comp.ID, domain.CloudVercel))

	got, err := store.GetComponent(ctx, comp.ID)
	require.NoError(t, err)
	assert.Equal(t, original, *got.Delivery.Override)
}
