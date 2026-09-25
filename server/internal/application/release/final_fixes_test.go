package release

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestAPendingReleaseWhoseDeployCouldNotStartIsWokenAgain(t *testing.T) {
	f := newDeployFixture()
	clock := newSteppableClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	f.svc.now = clock.Now
	f.actions.dispatchErr = errors.New("dial tcp: network is unreachable")
	repositoryID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), RepositoryID: repositoryID, Column: domain.TaskColumnDone}
	f.tasks.tasks[task.ID] = task
	created, err := f.store.Create(context.Background(), pendingDispatchRelease(repositoryID), []uuid.UUID{task.ID})
	require.NoError(t, err)

	_, err = f.svc.Deploy(context.Background(), created.ID, domain.ReleaseActorAgent)
	require.Error(t, err)

	f.svc.SweepOnce(context.Background())
	assert.Empty(t, f.waker.calls, "too soon: the agent may still be retrying itself")

	clock.Advance(11 * time.Minute)
	f.svc.SweepOnce(context.Background())
	require.Len(t, f.waker.calls, 1, "nothing else ever wakes a pending release whose deploy never started")
	assert.Equal(t, task.ID, f.waker.calls[0].task.ID)
}

func TestAnOnMergeDeployIsHeldWhileAProviderRollbackPinsProduction(t *testing.T) {
	f := newRollbackFixture()
	f.svc.deployStatus = newFakeDeployStatus()
	repositoryID := uuid.New()
	componentID := uuid.New()

	pinned := providerRollingBackRelease(repositoryID, componentID, domain.DeliveryOnMerge, f.clock.Now())
	pinned.Status = domain.ReleaseFailed
	pinned.Version = "aaaaaaaaaaaa"
	_, err := f.store.Create(context.Background(), pinned, nil)
	require.NoError(t, err)

	started := f.clock.Now()
	next := domain.Release{
		RepositoryID: repositoryID, ComponentID: &componentID, Mode: domain.DeliveryOnMerge,
		Executor: domain.ExecutorGitHubActions, Status: domain.ReleaseDeploying, CommitSHA: "cccccccccccccccccccccccccccccccccccccccc",
		Profile: deliveryProfile(domain.DeliveryOnMerge, domain.ExecutorGitHubActions), DeployStartedAt: &started,
	}
	created, err := f.store.Create(context.Background(), next, nil)
	require.NoError(t, err)
	f.svc.deployStatus.(*fakeDeployStatus).set(next.CommitSHA, "deploy.yml", domain.DeployWatchStatus{State: domain.DeployWatchSuccess})

	f.svc.sweepDeploying(context.Background(), created)
	got, err := f.store.Get(context.Background(), created.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.ReleaseDeploying, got.Status, "a READY build is not live while production is pinned")

	f.clock.Advance(61 * time.Minute)
	f.svc.sweepDeploying(context.Background(), got)
	got, err = f.store.Get(context.Background(), created.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.ReleaseFailed, got.Status)
	assert.Contains(t, got.FailureReason, "pinned")
}

func TestThePromoteAfterAProviderRollbackPicksTheNewestBuildOnTheRevert(t *testing.T) {
	f := newRollbackFixture()
	revert := "revert0000000000000000000000000000000000"
	f.svc.git = &fakeGit{ancestors: map[string]bool{revert: true}}
	repositoryID := uuid.New()
	componentID := uuid.New()
	envID := f.bindProdEnv(repositoryID, componentID)
	base := f.clock.Now()
	f.envs.deployments[envID] = []domain.CloudDeployment{
		{ID: "dpl_revert", CommitSHA: revert, Status: domain.CloudDeployReady, Environment: domain.EnvironmentProduction, CreatedAt: base},
		{ID: "dpl_after", CommitSHA: "dddddddddddddddddddddddddddddddddddddddd", Status: domain.CloudDeployReady, Environment: domain.EnvironmentProduction, CreatedAt: base.Add(time.Minute)},
		{ID: "dpl_preview", CommitSHA: "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", Status: domain.CloudDeployReady, Environment: domain.EnvironmentPreview, CreatedAt: base.Add(2 * time.Minute)},
	}
	task := f.withTask(repositoryID, "T-1", mergeSHA)
	created, err := f.store.Create(context.Background(), providerRollingBackRelease(repositoryID, componentID, domain.DeliveryOnMerge, base), []uuid.UUID{task.ID})
	require.NoError(t, err)

	f.svc.sweepRollingBack(context.Background(), created)

	require.Len(t, f.envs.promoteCalls, 1)
	assert.Equal(t, "dpl_after", f.envs.promoteCalls[0].deploymentID, "a merge that landed during the rollback must not be left un-served")
}

func TestARetriedProviderRollbackKeepsThePinInItsClaim(t *testing.T) {
	f := newRollbackFixture()
	repositoryID := uuid.New()
	componentID := uuid.New()
	f.bindProdEnv(repositoryID, componentID)
	task := f.withTask(repositoryID, "T-1", mergeSHA)
	r := providerRollingBackRelease(repositoryID, componentID, domain.DeliveryOnMerge, f.clock.Now())
	r.Status = domain.ReleaseFailed
	r.Rollback.RestoredRef = ""
	created, err := f.store.Create(context.Background(), r, []uuid.UUID{task.ID})
	require.NoError(t, err)

	var claimed *domain.ReleaseRollback
	f.store.beforeUpdate = func(next domain.Release) {
		if claimed == nil && next.Status == domain.ReleaseRollingBack && next.Rollback != nil {
			copy := *next.Rollback
			claimed = &copy
		}
	}
	_, _ = f.svc.Rollback(context.Background(), created.ID, domain.ReleaseActorHuman, domain.RollbackManual, "retry")

	require.NotNil(t, claimed)
	assert.Equal(t, domain.RollbackMechanismProvider, claimed.Mechanism, "a crash after the retry's claim must not forget production is pinned")
	assert.Equal(t, "dpl_prev", claimed.ProviderDeploymentID)
}

func TestAFailedOnMergeReleaseWatchesTheRevertDeployInsteadOfFinishingAtOnce(t *testing.T) {
	f := newRollbackFixture()
	repositoryID := uuid.New()
	componentID := uuid.New()
	task := f.withTask(repositoryID, "T-1", mergeSHA)
	failed := domain.Release{
		RepositoryID: repositoryID, ComponentID: &componentID, Mode: domain.DeliveryOnMerge,
		Executor: domain.ExecutorGitHubActions, Status: domain.ReleaseFailed, CommitSHA: mergeSHA,
		Profile: deliveryProfile(domain.DeliveryOnMerge, domain.ExecutorGitHubActions),
	}
	failed.Profile.AutoRollback = true
	created, err := f.store.Create(context.Background(), failed, []uuid.UUID{task.ID})
	require.NoError(t, err)

	got, err := f.svc.Rollback(context.Background(), created.ID, domain.ReleaseActorAgent, domain.RollbackDeployFailed, "deploy job red")
	require.NoError(t, err)
	assert.Equal(t, domain.ReleaseRollingBack, got.Status, "the revert push redeploys an on_merge component; that deploy must be watched")
}
