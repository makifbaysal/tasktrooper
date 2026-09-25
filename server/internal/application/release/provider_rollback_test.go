package release

// §M13: a provider rollback target must be a production-target deployment
// that is NOT one of the release's own (bad) commits — never a preview build,
// and never a deployment the review found reachable simply because it was
// the newest READY one before the release's own deploy started.

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func newProviderRollbackTargetFixture() (*Service, *fakeEnvironments, *steppableClock) {
	envs := newFakeEnvironments()
	clock := newSteppableClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	svc := New(Deps{
		Store:        newFakeReleaseStore(),
		Environments: envs,
		Clock:        clock.Now,
	})
	return svc, envs, clock
}

func TestProviderRollbackTargetSkipsAPreviewDeployment(t *testing.T) {
	svc, envs, clock := newProviderRollbackTargetFixture()
	repositoryID := uuid.New()
	componentID := uuid.New()
	envID := uuid.New()

	deployStarted := clock.Now()
	envs.deployments[envID] = []domain.CloudDeployment{
		{
			ID: "dpl_preview", CommitSHA: "preview000000000000000000000000000000000",
			Status: domain.CloudDeployReady, Environment: domain.EnvironmentPreview,
			CreatedAt: deployStarted.Add(-2 * time.Hour),
		},
	}

	r := domain.Release{
		ID: uuid.New(), RepositoryID: repositoryID, ComponentID: &componentID,
		CommitSHA:       "badcommit0000000000000000000000000000000",
		DeployStartedAt: timePtr(deployStarted),
	}

	target, ok := svc.providerRollbackTarget(context.Background(), r, envID)
	assert.False(t, ok, "a preview deployment must never be a provider rollback target")
	assert.Empty(t, target.ID)
}

func TestProviderRollbackTargetPrefersAProductionDeploymentOverAPreviewOne(t *testing.T) {
	svc, envs, clock := newProviderRollbackTargetFixture()
	repositoryID := uuid.New()
	componentID := uuid.New()
	envID := uuid.New()

	deployStarted := clock.Now()
	envs.deployments[envID] = []domain.CloudDeployment{
		{
			ID: "dpl_preview", CommitSHA: "preview000000000000000000000000000000000",
			Status: domain.CloudDeployReady, Environment: domain.EnvironmentPreview,
			CreatedAt: deployStarted.Add(-1 * time.Hour), // newer than the production one below
		},
		{
			ID: "dpl_prod", CommitSHA: "prevgoodsha0000000000000000000000000000",
			Status: domain.CloudDeployReady, Environment: domain.EnvironmentProduction,
			CreatedAt: deployStarted.Add(-2 * time.Hour),
		},
	}

	r := domain.Release{
		ID: uuid.New(), RepositoryID: repositoryID, ComponentID: &componentID,
		CommitSHA:       "badcommit0000000000000000000000000000000",
		DeployStartedAt: timePtr(deployStarted),
	}

	target, ok := svc.providerRollbackTarget(context.Background(), r, envID)
	require.True(t, ok)
	assert.Equal(t, "dpl_prod", target.ID, "the newer preview deployment must be skipped for the older production one")
}

func TestProviderRollbackTargetExcludesTheReleasesOwnCommit(t *testing.T) {
	svc, envs, clock := newProviderRollbackTargetFixture()
	repositoryID := uuid.New()
	componentID := uuid.New()
	envID := uuid.New()

	deployStarted := clock.Now()
	badTaskSHA := "badcommit0000000000000000000000000000000"
	envs.deployments[envID] = []domain.CloudDeployment{
		// A production-target deployment of the release's OWN bad commit —
		// e.g. a preview promotion or a retry build — must never be offered
		// back as "the rollback target".
		{
			ID: "dpl_bad", CommitSHA: badTaskSHA,
			Status: domain.CloudDeployReady, Environment: domain.EnvironmentProduction,
			CreatedAt: deployStarted.Add(-30 * time.Minute),
		},
		{
			ID: "dpl_good", CommitSHA: "prevgoodsha0000000000000000000000000000",
			Status: domain.CloudDeployReady, Environment: domain.EnvironmentProduction,
			CreatedAt: deployStarted.Add(-2 * time.Hour),
		},
	}

	r := domain.Release{
		ID: uuid.New(), RepositoryID: repositoryID, ComponentID: &componentID,
		CommitSHA:       "released0000000000000000000000000000000",
		DeployStartedAt: timePtr(deployStarted),
		Tasks:           []domain.ReleaseTaskRef{{ID: uuid.New(), MergeCommitSHA: badTaskSHA}},
	}

	target, ok := svc.providerRollbackTarget(context.Background(), r, envID)
	require.True(t, ok)
	assert.Equal(t, "dpl_good", target.ID, "a deployment of the release's own commit must be excluded even though it is READY and before DeployStartedAt")
}

func TestProviderRollbackTargetNoneWhenOnlyTheReleasesOwnCommitQualifies(t *testing.T) {
	svc, envs, clock := newProviderRollbackTargetFixture()
	repositoryID := uuid.New()
	componentID := uuid.New()
	envID := uuid.New()

	deployStarted := clock.Now()
	badSHA := "badcommit0000000000000000000000000000000"
	envs.deployments[envID] = []domain.CloudDeployment{
		{
			ID: "dpl_bad", CommitSHA: badSHA,
			Status: domain.CloudDeployReady, Environment: domain.EnvironmentProduction,
			CreatedAt: deployStarted.Add(-30 * time.Minute),
		},
	}

	r := domain.Release{
		ID: uuid.New(), RepositoryID: repositoryID, ComponentID: &componentID,
		CommitSHA:       badSHA,
		DeployStartedAt: timePtr(deployStarted),
	}

	_, ok := svc.providerRollbackTarget(context.Background(), r, envID)
	assert.False(t, ok, "no legitimate target exists once the only candidate is the release's own commit")
}
