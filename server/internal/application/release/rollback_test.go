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
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type rollbackFixture struct {
	svc       *Service
	store     *fakeReleaseStore
	tasks     *fakeTasks
	parked    *fakeParked
	merge     *fakeMergeState
	reverter  *fakeReverter
	actions   *fakeActions
	repos     *fakeRepos
	incidents *fakeIncidents
	envs      *fakeEnvironments
	clock     *steppableClock
}

func newRollbackFixture() *rollbackFixture {
	store := newFakeReleaseStore()
	tasks := newFakeTasks()
	parked := newFakeParked()
	merge := &fakeMergeState{}
	reverter := &fakeReverter{sha: "revert0000000000000000000000000000000000"}
	actions := newFakeActions()
	repos := &fakeRepos{repo: domain.Repository{ID: uuid.New(), Name: "widget", RootPath: "/repos/widget"}}
	incidents := &fakeIncidents{}
	envs := newFakeEnvironments()
	clock := newSteppableClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	svc := New(Deps{
		Store:        store,
		Tasks:        tasks,
		ParkedTasks:  parked,
		MergeState:   merge,
		Reverter:     reverter,
		Actions:      actions,
		Repos:        repos,
		Incidents:    incidents,
		Environments: envs,
		RepoCoordinates: func(context.Context, domain.Repository) (string, string, error) {
			return "acme", "widget", nil
		},
		Clock: clock.Now,
	})
	return &rollbackFixture{svc: svc, store: store, tasks: tasks, parked: parked, merge: merge, reverter: reverter, actions: actions, repos: repos, incidents: incidents, envs: envs, clock: clock}
}

// bindProdEnv makes the component's production environment resolvable
// through fakeEnvironments — the precondition attemptProviderRollback checks
// before it ever calls CanRollback.
func (f *rollbackFixture) bindProdEnv(repositoryID, componentID uuid.UUID) uuid.UUID {
	envID := uuid.New()
	f.envs.envs[repositoryID] = append(f.envs.envs[repositoryID], domain.ComponentEnvironment{
		ID: envID, RepositoryID: repositoryID, ComponentID: componentID,
		Environment: domain.EnvironmentProduction, Status: domain.LinkConfirmed,
	})
	return envID
}

func (f *rollbackFixture) withTask(repositoryID uuid.UUID, key, sha string) domain.BoardTask {
	task := domain.BoardTask{ID: uuid.New(), RepositoryID: repositoryID, Column: domain.TaskColumnDone, Key: key, MergeCommitSHA: sha}
	f.tasks.tasks[task.ID] = task
	return task
}

func awaitingVerdictRelease(repositoryID uuid.UUID, componentID uuid.UUID, mode domain.DeliveryMode, autoRollback bool) domain.Release {
	profile := deliveryProfile(mode, domain.ExecutorGitHubActions)
	profile.AutoRollback = autoRollback
	return domain.Release{
		RepositoryID: repositoryID,
		ComponentID:  &componentID,
		Version:      "abcdef0",
		Mode:         mode,
		Executor:     domain.ExecutorGitHubActions,
		Status:       domain.ReleaseAwaitingVerdict,
		CommitSHA:    mergeSHA,
		Profile:      profile,
	}
}

func TestRollbackWithAutoOffWritesAProposalAndRefuses(t *testing.T) {
	f := newRollbackFixture()
	repositoryID := uuid.New()
	componentID := uuid.New()
	task := f.withTask(repositoryID, "T-1", mergeSHA)
	created, err := f.store.Create(context.Background(), awaitingVerdictRelease(repositoryID, componentID, domain.DeliveryDispatch, false), []uuid.UUID{task.ID})
	require.NoError(t, err)

	_, err = f.svc.Rollback(context.Background(), created.ID, domain.ReleaseActorAgent, domain.RollbackVerifyFailed, "smoke check failed")
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrRollbackNeedsHuman)

	require.Len(t, f.tasks.comments, 1)
	assert.Contains(t, f.tasks.comments[0].Content, "PROPOSED")
	assert.Empty(t, f.reverter.calls, "nothing must be reverted when the proposal is only written up")

	r, err := f.store.Get(context.Background(), created.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.ReleaseAwaitingVerdict, r.Status, "no state change")
}

func TestRollbackWithAutoOffAllowsAHuman(t *testing.T) {
	f := newRollbackFixture()
	repositoryID := uuid.New()
	componentID := uuid.New()
	task := f.withTask(repositoryID, "T-1", mergeSHA)
	created, err := f.store.Create(context.Background(), awaitingVerdictRelease(repositoryID, componentID, domain.DeliveryOnMerge, false), []uuid.UUID{task.ID})
	require.NoError(t, err)

	updated, err := f.svc.Rollback(context.Background(), created.ID, domain.ReleaseActorHuman, domain.RollbackManual, "rolling back by hand")
	require.NoError(t, err)
	assert.Equal(t, domain.ReleaseRollingBack, updated.Status)
}

func TestRollbackDispatchRedeploysThePreviousGoodRelease(t *testing.T) {
	f := newRollbackFixture()
	repositoryID := uuid.New()
	componentID := uuid.New()
	prevFinished := f.clock.Now().Add(-2 * time.Hour)
	_, err := f.store.Create(context.Background(), domain.Release{
		RepositoryID: repositoryID, ComponentID: &componentID, Status: domain.ReleaseReleased,
		CommitSHA: "prevgoodsha0000000000000000000000000000", FinishedAt: &prevFinished, CreatedAt: f.clock.Now().Add(-3 * time.Hour),
	}, nil)
	require.NoError(t, err)

	task := f.withTask(repositoryID, "T-2", mergeSHA)
	bad := awaitingVerdictRelease(repositoryID, componentID, domain.DeliveryDispatch, true)
	bad.CreatedAt = f.clock.Now().Add(-1 * time.Hour)
	created, err := f.store.Create(context.Background(), bad, []uuid.UUID{task.ID})
	require.NoError(t, err)

	updated, err := f.svc.Rollback(context.Background(), created.ID, domain.ReleaseActorAgent, domain.RollbackVerifyFailed, "new error groups since deploy")
	require.NoError(t, err)

	assert.Equal(t, domain.ReleaseRollingBack, updated.Status)
	require.NotNil(t, updated.Rollback)
	assert.Equal(t, domain.RollbackMechanismWorkflow, updated.Rollback.Mechanism)
	assert.Equal(t, "prevgoodsha0000000000000000000000000000", updated.Rollback.RestoredRef)
	require.Len(t, f.actions.dispatchCalls, 1)
	assert.Contains(t, f.actions.dispatchCalls[0], domain.ReleaseTagForCommit("prevgoodsha0000000000000000000000000000"))
	require.Len(t, f.reverter.calls, 1)
	assert.Equal(t, []string{mergeSHA}, f.reverter.calls[0].shas)
}

func TestRollbackDispatchWithNoPreviousReleaseRedeploysAtTheRevert(t *testing.T) {
	f := newRollbackFixture()
	repositoryID := uuid.New()
	componentID := uuid.New()
	task := f.withTask(repositoryID, "T-1", mergeSHA)
	created, err := f.store.Create(context.Background(), awaitingVerdictRelease(repositoryID, componentID, domain.DeliveryDispatch, true), []uuid.UUID{task.ID})
	require.NoError(t, err)

	updated, err := f.svc.Rollback(context.Background(), created.ID, domain.ReleaseActorAgent, domain.RollbackDeployFailed, "deploy job red")
	require.NoError(t, err)

	require.NotNil(t, updated.Rollback)
	assert.Equal(t, f.reverter.sha, updated.Rollback.RestoredRef)
}

func TestRollbackOnMergeUsesRevertPush(t *testing.T) {
	f := newRollbackFixture()
	repositoryID := uuid.New()
	componentID := uuid.New()
	task := f.withTask(repositoryID, "T-1", mergeSHA)
	created, err := f.store.Create(context.Background(), awaitingVerdictRelease(repositoryID, componentID, domain.DeliveryOnMerge, true), []uuid.UUID{task.ID})
	require.NoError(t, err)

	updated, err := f.svc.Rollback(context.Background(), created.ID, domain.ReleaseActorAgent, domain.RollbackHealthIncident, "health failing")
	require.NoError(t, err)

	require.NotNil(t, updated.Rollback)
	assert.Equal(t, domain.RollbackMechanismRevert, updated.Rollback.Mechanism)
	assert.Equal(t, f.reverter.sha, updated.Rollback.RestoredRef)
	assert.Empty(t, f.actions.dispatchCalls, "on_merge never dispatches — the revert push itself redeploys")
}

func TestRollbackRevertFailureFailsTheReleaseAndChangesNothingElse(t *testing.T) {
	f := newRollbackFixture()
	f.reverter.err = errors.New("conflict in server/internal/foo.go")
	repositoryID := uuid.New()
	componentID := uuid.New()
	task := f.withTask(repositoryID, "T-1", mergeSHA)
	created, err := f.store.Create(context.Background(), awaitingVerdictRelease(repositoryID, componentID, domain.DeliveryOnMerge, true), []uuid.UUID{task.ID})
	require.NoError(t, err)

	_, err = f.svc.Rollback(context.Background(), created.ID, domain.ReleaseActorAgent, domain.RollbackVerifyFailed, "note")
	require.Error(t, err)

	r, err := f.store.Get(context.Background(), created.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.ReleaseFailed, r.Status)
	assert.Contains(t, r.FailureReason, "nothing was reverted")
	assert.Empty(t, f.actions.dispatchCalls)
}

func TestRollbackAllowedWithinTheReleasedWindowForTheNewestRelease(t *testing.T) {
	f := newRollbackFixture()
	repositoryID := uuid.New()
	componentID := uuid.New()
	task := f.withTask(repositoryID, "T-1", mergeSHA)
	finishedAt := f.clock.Now().Add(-2 * time.Hour)
	released := domain.Release{
		RepositoryID: repositoryID, ComponentID: &componentID, Status: domain.ReleaseReleased,
		Mode: domain.DeliveryOnMerge, Executor: domain.ExecutorGitHubActions,
		CommitSHA: mergeSHA, FinishedAt: &finishedAt,
		Profile: deliveryProfile(domain.DeliveryOnMerge, domain.ExecutorGitHubActions),
	}
	released.Profile.AutoRollback = true
	created, err := f.store.Create(context.Background(), released, []uuid.UUID{task.ID})
	require.NoError(t, err)

	updated, err := f.svc.Rollback(context.Background(), created.ID, domain.ReleaseActorAgent, domain.RollbackHealthIncident, "prod incident")
	require.NoError(t, err)
	assert.Equal(t, domain.ReleaseRollingBack, updated.Status)
}

func TestRollbackRefusesAReleasedReleaseThatIsNoLongerNewest(t *testing.T) {
	f := newRollbackFixture()
	repositoryID := uuid.New()
	componentID := uuid.New()
	finishedAt := f.clock.Now().Add(-2 * time.Hour)
	old, err := f.store.Create(context.Background(), domain.Release{
		RepositoryID: repositoryID, ComponentID: &componentID, Status: domain.ReleaseReleased,
		CommitSHA: mergeSHA, FinishedAt: &finishedAt,
		Profile: domain.ComponentDelivery{AutoRollback: true},
	}, nil)
	require.NoError(t, err)
	newerFinished := f.clock.Now().Add(-1 * time.Hour)
	_, err = f.store.Create(context.Background(), domain.Release{
		RepositoryID: repositoryID, ComponentID: &componentID, Status: domain.ReleaseReleased,
		CommitSHA: "newer0000000000000000000000000000000000", FinishedAt: &newerFinished,
	}, nil)
	require.NoError(t, err)

	_, err = f.svc.Rollback(context.Background(), old.ID, domain.ReleaseActorAgent, domain.RollbackHealthIncident, "note")
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrReleaseWrongStatus)
}

func TestRollbackRefusesAReleasedReleaseOlderThanADay(t *testing.T) {
	f := newRollbackFixture()
	repositoryID := uuid.New()
	componentID := uuid.New()
	finishedAt := f.clock.Now().Add(-25 * time.Hour)
	created, err := f.store.Create(context.Background(), domain.Release{
		RepositoryID: repositoryID, ComponentID: &componentID, Status: domain.ReleaseReleased,
		CommitSHA: mergeSHA, FinishedAt: &finishedAt,
		Profile: domain.ComponentDelivery{AutoRollback: true},
	}, nil)
	require.NoError(t, err)

	_, err = f.svc.Rollback(context.Background(), created.ID, domain.ReleaseActorAgent, domain.RollbackHealthIncident, "note")
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrReleaseWrongStatus)
}

func TestSweepRollingBackToRolledBackReopensTasks(t *testing.T) {
	f := newRollbackFixture()
	ds := newFakeDeployStatus()
	f.svc.deployStatus = ds
	repositoryID := uuid.New()
	componentID := uuid.New()
	task := f.withTask(repositoryID, "T-1", mergeSHA)
	f.parked.parked[task.ID] = task

	startedAt := f.clock.Now()
	r := domain.Release{
		RepositoryID: repositoryID, ComponentID: &componentID, Status: domain.ReleaseRollingBack,
		Version: "abcdef0", CommitSHA: mergeSHA,
		Profile: deliveryProfile(domain.DeliveryOnMerge, domain.ExecutorGitHubActions),
		Rollback: &domain.ReleaseRollback{
			Reason: domain.RollbackHealthIncident, RevertSHA: "revert00000000000000000000000000000000",
			RestoredRef: "revert00000000000000000000000000000000",
			Mechanism:   domain.RollbackMechanismRevert,
			StartedAt:   startedAt,
		},
	}
	created, err := f.store.Create(context.Background(), r, []uuid.UUID{task.ID})
	require.NoError(t, err)
	// Keyed by since = Rollback.StartedAt (§H1) — a plain by-sha watch would
	// also read the release's OWN original deploy of this tag, if any, as
	// though it were the rollback's.
	ds.setSince("revert00000000000000000000000000000000", "deploy.yml", startedAt, domain.DeployWatchStatus{State: domain.DeployWatchSuccess})

	f.svc.sweepRollingBack(context.Background(), created)

	got, err := f.store.Get(context.Background(), created.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.ReleaseRolledBack, got.Status)
	require.NotNil(t, got.FinishedAt)

	assert.Contains(t, f.merge.reset, task.ID)
	require.Len(t, f.tasks.updates, 1)
	assert.Equal(t, domain.TaskColumnNeedRevision, *f.tasks.updates[0].Column)
	assert.Equal(t, domain.MoveReasonReleaseRolledBack, f.tasks.updates[0].SystemReason)
	require.Len(t, f.tasks.comments, 1)
	assert.Contains(t, f.tasks.comments[0].Content, "git revert")
	assert.Contains(t, f.parked.taken, task.ID, "the parked card must be claimed before the move")
}

func TestSweepRollingBackFailureIngestsAnIncident(t *testing.T) {
	f := newRollbackFixture()
	ds := newFakeDeployStatus()
	f.svc.deployStatus = ds
	repositoryID := uuid.New()
	componentID := uuid.New()
	task := f.withTask(repositoryID, "T-1", mergeSHA)

	startedAt := f.clock.Now()
	r := domain.Release{
		RepositoryID: repositoryID, ComponentID: &componentID, Status: domain.ReleaseRollingBack,
		CommitSHA: mergeSHA,
		Profile:   deliveryProfile(domain.DeliveryDispatch, domain.ExecutorGitHubActions),
		Rollback: &domain.ReleaseRollback{
			Reason: domain.RollbackDeployFailed, RestoredRef: "restore0000000000000000000000000000000",
			Mechanism: domain.RollbackMechanismWorkflow, StartedAt: startedAt,
		},
	}
	created, err := f.store.Create(context.Background(), r, []uuid.UUID{task.ID})
	require.NoError(t, err)
	ds.setSince("restore0000000000000000000000000000000", "deploy.yml", startedAt, domain.DeployWatchStatus{State: domain.DeployWatchFailure, Detail: "workflow errored"})

	f.svc.sweepRollingBack(context.Background(), created)

	got, err := f.store.Get(context.Background(), created.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.ReleaseFailed, got.Status)
	assert.Contains(t, got.FailureReason, "workflow errored")
	require.Len(t, f.incidents.ingested, 1)
}

// --- WP-N: provider rollback ---

func TestRollbackProviderSuccessOnMergeMatchesPreviousReleaseCommit(t *testing.T) {
	f := newRollbackFixture()
	repositoryID := uuid.New()
	componentID := uuid.New()
	envID := f.bindProdEnv(repositoryID, componentID)
	f.envs.rollbackable[envID] = true

	prevFinished := f.clock.Now().Add(-2 * time.Hour)
	_, err := f.store.Create(context.Background(), domain.Release{
		RepositoryID: repositoryID, ComponentID: &componentID, Status: domain.ReleaseReleased,
		CommitSHA: "prevgoodsha0000000000000000000000000000", FinishedAt: &prevFinished, CreatedAt: f.clock.Now().Add(-3 * time.Hour),
	}, nil)
	require.NoError(t, err)
	f.envs.deployments[envID] = []domain.CloudDeployment{
		{ID: "dpl_prev", CommitSHA: "prevgoodsha0000000000000000000000000000", Status: domain.CloudDeployReady, CreatedAt: prevFinished, Environment: domain.EnvironmentProduction},
	}

	task := f.withTask(repositoryID, "T-1", mergeSHA)
	bad := awaitingVerdictRelease(repositoryID, componentID, domain.DeliveryOnMerge, true)
	bad.CreatedAt = f.clock.Now().Add(-1 * time.Hour)
	created, err := f.store.Create(context.Background(), bad, []uuid.UUID{task.ID})
	require.NoError(t, err)

	updated, err := f.svc.Rollback(context.Background(), created.ID, domain.ReleaseActorAgent, domain.RollbackHealthIncident, "health failing")
	require.NoError(t, err)

	require.NotNil(t, updated.Rollback)
	assert.Equal(t, domain.RollbackMechanismProvider, updated.Rollback.Mechanism)
	assert.Equal(t, "dpl_prev", updated.Rollback.ProviderDeploymentID)
	assert.Equal(t, f.reverter.sha, updated.Rollback.RestoredRef, "the revert still lands so the default branch cannot ship the bad change again")
	require.Len(t, f.envs.rollbackCalls, 1)
	assert.Equal(t, "dpl_prev", f.envs.rollbackCalls[0].deploymentID)
	require.Len(t, f.reverter.calls, 1, "the revert always runs, provider rollback or not")
}

// §H2: dispatch mode redeploys the previous release itself, so a provider
// rollback must never even be attempted for it — pinning production via the
// provider would immediately be undone by that redeploy, and then leave
// automatic production assignment off for every later deploy.
func TestRollbackDispatchNeverAttemptsAProviderRollback(t *testing.T) {
	f := newRollbackFixture()
	repositoryID := uuid.New()
	componentID := uuid.New()
	envID := f.bindProdEnv(repositoryID, componentID)
	f.envs.rollbackable[envID] = true
	f.envs.deployments[envID] = []domain.CloudDeployment{
		{ID: "dpl_ready", Status: domain.CloudDeployReady, CreatedAt: f.clock.Now().Add(-30 * time.Minute), Environment: domain.EnvironmentProduction},
	}

	task := f.withTask(repositoryID, "T-1", mergeSHA)
	bad := awaitingVerdictRelease(repositoryID, componentID, domain.DeliveryDispatch, true)
	bad.DeployStartedAt = timePtr(f.clock.Now().Add(-10 * time.Minute))
	created, err := f.store.Create(context.Background(), bad, []uuid.UUID{task.ID})
	require.NoError(t, err)

	updated, err := f.svc.Rollback(context.Background(), created.ID, domain.ReleaseActorAgent, domain.RollbackDeployFailed, "deploy job red")
	require.NoError(t, err)

	require.NotNil(t, updated.Rollback)
	assert.Equal(t, domain.RollbackMechanismWorkflow, updated.Rollback.Mechanism)
	assert.Empty(t, updated.Rollback.ProviderDeploymentID)
	assert.Empty(t, f.envs.rollbackCalls, "dispatch must never call the provider's rollback route")
	require.Len(t, f.actions.dispatchCalls, 1, "dispatch redeploys the revert commit since there is no previous released release")
}

func TestRollbackProviderDeniedFallsBackToTheF1Mechanism(t *testing.T) {
	f := newRollbackFixture()
	repositoryID := uuid.New()
	componentID := uuid.New()
	envID := f.bindProdEnv(repositoryID, componentID)
	f.envs.rollbackable[envID] = true
	f.envs.deployments[envID] = []domain.CloudDeployment{
		{ID: "dpl_ready", Status: domain.CloudDeployReady, CreatedAt: f.clock.Now().Add(-30 * time.Minute), Environment: domain.EnvironmentProduction},
	}
	f.envs.rollbackErr[envID] = port.ErrCloudWriteDenied

	task := f.withTask(repositoryID, "T-1", mergeSHA)
	created, err := f.store.Create(context.Background(), awaitingVerdictRelease(repositoryID, componentID, domain.DeliveryOnMerge, true), []uuid.UUID{task.ID})
	require.NoError(t, err)

	updated, err := f.svc.Rollback(context.Background(), created.ID, domain.ReleaseActorAgent, domain.RollbackHealthIncident, "health failing")
	require.NoError(t, err)

	require.NotNil(t, updated.Rollback)
	assert.Equal(t, domain.RollbackMechanismRevert, updated.Rollback.Mechanism, "a denied provider rollback never fails the rollback itself")
	assert.Contains(t, updated.Rollback.Detail, "denied")
	assert.Empty(t, updated.Rollback.ProviderDeploymentID)
}

// on_merge, not dispatch (§H2) — an unsupported provider still falls back to
// the revert-push mechanism cleanly.
func TestRollbackProviderUnsupportedFallsBackToTheF1Mechanism(t *testing.T) {
	f := newRollbackFixture()
	repositoryID := uuid.New()
	componentID := uuid.New()
	envID := f.bindProdEnv(repositoryID, componentID)
	f.envs.rollbackable[envID] = true
	f.envs.deployments[envID] = []domain.CloudDeployment{
		{ID: "dpl_ready", Status: domain.CloudDeployReady, CreatedAt: f.clock.Now().Add(-30 * time.Minute), Environment: domain.EnvironmentProduction},
	}
	f.envs.rollbackErr[envID] = port.ErrUnsupported

	task := f.withTask(repositoryID, "T-1", mergeSHA)
	created, err := f.store.Create(context.Background(), awaitingVerdictRelease(repositoryID, componentID, domain.DeliveryOnMerge, true), []uuid.UUID{task.ID})
	require.NoError(t, err)

	updated, err := f.svc.Rollback(context.Background(), created.ID, domain.ReleaseActorAgent, domain.RollbackHealthIncident, "health failing")
	require.NoError(t, err)

	require.NotNil(t, updated.Rollback)
	assert.Equal(t, domain.RollbackMechanismRevert, updated.Rollback.Mechanism)
	assert.Contains(t, updated.Rollback.Detail, "not supported")
	assert.Empty(t, f.actions.dispatchCalls, "on_merge never dispatches — the revert push itself redeploys")
}

func TestRollbackProviderCapabilityAbsentSkipsTheAttemptSilently(t *testing.T) {
	f := newRollbackFixture()
	repositoryID := uuid.New()
	componentID := uuid.New()
	envID := f.bindProdEnv(repositoryID, componentID)
	f.envs.rollbackable[envID] = false // provider lacks CloudRollbacker (or the credential can't write)

	task := f.withTask(repositoryID, "T-1", mergeSHA)
	created, err := f.store.Create(context.Background(), awaitingVerdictRelease(repositoryID, componentID, domain.DeliveryOnMerge, true), []uuid.UUID{task.ID})
	require.NoError(t, err)

	updated, err := f.svc.Rollback(context.Background(), created.ID, domain.ReleaseActorAgent, domain.RollbackHealthIncident, "health failing")
	require.NoError(t, err)

	require.NotNil(t, updated.Rollback)
	assert.Equal(t, domain.RollbackMechanismRevert, updated.Rollback.Mechanism)
	assert.Empty(t, updated.Rollback.Detail)
	assert.Empty(t, f.envs.rollbackCalls, "CanRollback false must never call RollbackEnvironment")
}

func TestRollbackProviderNoTargetFoundFallsBackAndNotesIt(t *testing.T) {
	f := newRollbackFixture()
	repositoryID := uuid.New()
	componentID := uuid.New()
	envID := f.bindProdEnv(repositoryID, componentID)
	f.envs.rollbackable[envID] = true
	// No deployments at all: neither the previous release's commit nor any
	// ready deployment before DeployStartedAt exists to roll back to.

	task := f.withTask(repositoryID, "T-1", mergeSHA)
	created, err := f.store.Create(context.Background(), awaitingVerdictRelease(repositoryID, componentID, domain.DeliveryOnMerge, true), []uuid.UUID{task.ID})
	require.NoError(t, err)

	updated, err := f.svc.Rollback(context.Background(), created.ID, domain.ReleaseActorAgent, domain.RollbackHealthIncident, "health failing")
	require.NoError(t, err)

	require.NotNil(t, updated.Rollback)
	assert.Equal(t, domain.RollbackMechanismRevert, updated.Rollback.Mechanism)
	assert.Contains(t, updated.Rollback.Detail, "no earlier production deployment")
	assert.Empty(t, f.envs.rollbackCalls)
}

func TestRollbackProviderBatchNeverAttemptsIt(t *testing.T) {
	f := newRollbackFixture()
	f.svc.git = &fakeGit{head: mergeSHA}
	repositoryID := uuid.New()
	componentID := uuid.New()
	envID := f.bindProdEnv(repositoryID, componentID)
	f.envs.rollbackable[envID] = true
	f.envs.deployments[envID] = []domain.CloudDeployment{{ID: "dpl_ready", Status: domain.CloudDeployReady, CreatedAt: f.clock.Now().Add(-time.Hour)}}

	task := f.withTask(repositoryID, "T-1", mergeSHA)
	batch := awaitingVerdictRelease(repositoryID, componentID, domain.DeliveryBatch, true)
	batch.Executor = domain.ExecutorLocal
	created, err := f.store.Create(context.Background(), batch, []uuid.UUID{task.ID})
	require.NoError(t, err)

	_, err = f.svc.Rollback(context.Background(), created.ID, domain.ReleaseActorAgent, domain.RollbackDeployFailed, "build failed")
	require.NoError(t, err)

	assert.Empty(t, f.envs.rollbackCalls, "batch releases never attempt a provider rollback")
}

func timePtr(t time.Time) *time.Time { return &t }

// --- WP-N: sweeping a provider-mechanism rollback ---

func providerRollingBackRelease(repositoryID, componentID uuid.UUID, mode domain.DeliveryMode, startedAt time.Time) domain.Release {
	return domain.Release{
		RepositoryID: repositoryID, ComponentID: &componentID, Status: domain.ReleaseRollingBack,
		Mode: mode, CommitSHA: mergeSHA,
		Profile: deliveryProfile(mode, domain.ExecutorVercel),
		Rollback: &domain.ReleaseRollback{
			Reason: domain.RollbackHealthIncident, RevertSHA: "revert0000000000000000000000000000000000",
			RestoredRef:          "revert0000000000000000000000000000000000",
			Mechanism:            domain.RollbackMechanismProvider,
			ProviderDeploymentID: "dpl_prev",
			StartedAt:            startedAt,
		},
	}
}

func TestSweepRollingBackProviderDispatchFinishesAsSoonAsProviderServesTheTarget(t *testing.T) {
	f := newRollbackFixture()
	repositoryID := uuid.New()
	componentID := uuid.New()
	envID := f.bindProdEnv(repositoryID, componentID)
	f.envs.current[envID] = domain.CloudDeployment{ID: "dpl_prev"}

	task := f.withTask(repositoryID, "T-1", mergeSHA)
	f.parked.parked[task.ID] = task
	r := providerRollingBackRelease(repositoryID, componentID, domain.DeliveryDispatch, f.clock.Now())
	created, err := f.store.Create(context.Background(), r, []uuid.UUID{task.ID})
	require.NoError(t, err)

	f.svc.sweepRollingBack(context.Background(), created)

	got, err := f.store.Get(context.Background(), created.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.ReleaseRolledBack, got.Status)
	assert.Empty(t, f.envs.promoteCalls, "dispatch never promotes — it has no automatic-production-assignment concept to restore")
}

func TestSweepRollingBackProviderOnMergeWaitsThenPromotesTheRevertDeployment(t *testing.T) {
	f := newRollbackFixture()
	repositoryID := uuid.New()
	componentID := uuid.New()
	envID := f.bindProdEnv(repositoryID, componentID)
	f.envs.current[envID] = domain.CloudDeployment{ID: "dpl_prev"}

	task := f.withTask(repositoryID, "T-1", mergeSHA)
	f.parked.parked[task.ID] = task
	r := providerRollingBackRelease(repositoryID, componentID, domain.DeliveryOnMerge, f.clock.Now())
	created, err := f.store.Create(context.Background(), r, []uuid.UUID{task.ID})
	require.NoError(t, err)

	// First sweep: the provider is already serving the earlier deployment,
	// but the revert commit has no deployment yet — must keep watching.
	f.svc.sweepRollingBack(context.Background(), created)
	got, err := f.store.Get(context.Background(), created.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.ReleaseRollingBack, got.Status, "still waiting for the revert commit's own deployment")
	assert.Empty(t, f.envs.promoteCalls)

	// The revert commit's deployment appears and settles ready.
	f.envs.deployments[envID] = []domain.CloudDeployment{
		{ID: "dpl_revert", CommitSHA: "revert0000000000000000000000000000000000", Status: domain.CloudDeployReady},
	}
	f.svc.sweepRollingBack(context.Background(), got)

	final, err := f.store.Get(context.Background(), created.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.ReleaseRolledBack, final.Status)
	require.NotNil(t, final.Rollback)
	assert.Equal(t, "dpl_revert", final.Rollback.PromotedDeploymentID)
	require.Len(t, f.envs.promoteCalls, 1)
	assert.Equal(t, "dpl_revert", f.envs.promoteCalls[0].deploymentID)
	assert.Contains(t, f.merge.reset, task.ID, "a promoted rollback still reopens its tasks like any other")
}

func TestSweepRollingBackProviderPromoteFailureReportsProductionSafeButFails(t *testing.T) {
	f := newRollbackFixture()
	repositoryID := uuid.New()
	componentID := uuid.New()
	envID := f.bindProdEnv(repositoryID, componentID)
	f.envs.current[envID] = domain.CloudDeployment{ID: "dpl_prev"}
	f.envs.deployments[envID] = []domain.CloudDeployment{
		{ID: "dpl_revert", CommitSHA: "revert0000000000000000000000000000000000", Status: domain.CloudDeployReady},
	}
	f.envs.promoteErr[envID] = errors.New("token lacks scope")

	task := f.withTask(repositoryID, "T-1", mergeSHA)
	r := providerRollingBackRelease(repositoryID, componentID, domain.DeliveryOnMerge, f.clock.Now())
	created, err := f.store.Create(context.Background(), r, []uuid.UUID{task.ID})
	require.NoError(t, err)

	f.svc.sweepRollingBack(context.Background(), created)

	got, err := f.store.Get(context.Background(), created.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.ReleaseFailed, got.Status)
	assert.Contains(t, got.FailureReason, "production is SAFE")
}

func TestSweepRollingBackProviderTimesOutAfterThirtyMinutes(t *testing.T) {
	f := newRollbackFixture()
	repositoryID := uuid.New()
	componentID := uuid.New()
	envID := f.bindProdEnv(repositoryID, componentID)
	// current never matches ProviderDeploymentID — the provider rollback
	// never actually took effect.
	f.envs.current[envID] = domain.CloudDeployment{ID: "dpl_something_else"}

	task := f.withTask(repositoryID, "T-1", mergeSHA)
	r := providerRollingBackRelease(repositoryID, componentID, domain.DeliveryDispatch, f.clock.Now().Add(-31*time.Minute))
	created, err := f.store.Create(context.Background(), r, []uuid.UUID{task.ID})
	require.NoError(t, err)

	f.svc.sweepRollingBack(context.Background(), created)

	got, err := f.store.Get(context.Background(), created.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.ReleaseFailed, got.Status)
	assert.Contains(t, got.FailureReason, "did not take effect")
}

// --- §H1: a dispatch redeploy that fails must fail the release, and that
// status must survive, not be overwritten by rolling_back. ---

func TestRollbackDispatchRedeployFailureForAnyReasonFailsTheRelease(t *testing.T) {
	f := newRollbackFixture()
	f.actions.dispatchErr = errors.New("connection reset by peer") // NOT a CI-unavailable text
	repositoryID := uuid.New()
	componentID := uuid.New()
	task := f.withTask(repositoryID, "T-1", mergeSHA)
	f.parked.parked[task.ID] = task
	created, err := f.store.Create(context.Background(), awaitingVerdictRelease(repositoryID, componentID, domain.DeliveryDispatch, true), []uuid.UUID{task.ID})
	require.NoError(t, err)

	updated, err := f.svc.Rollback(context.Background(), created.ID, domain.ReleaseActorAgent, domain.RollbackDeployFailed, "deploy job red")
	require.NoError(t, err, "the revert itself succeeded — Rollback only reports an error when the revert fails")

	assert.Equal(t, domain.ReleaseFailed, updated.Status, "a redeploy failure for ANY reason must fail the release, not leave it rolling_back")
	assert.Contains(t, updated.FailureReason, "connection reset by peer")
	require.NotNil(t, updated.Rollback)
	assert.Equal(t, domain.RollbackMechanismWorkflow, updated.Rollback.Mechanism)
	assert.NotEmpty(t, updated.Rollback.RevertSHA, "the revert landed even though the redeploy failed")
	assert.Contains(t, f.parked.taken, task.ID, "a failed redeploy must hand back so the agent is woken")

	got, err := f.store.Get(context.Background(), created.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.ReleaseFailed, got.Status, "the persisted status must also be failed, never rolling_back")
}

// The old by-sha watch would resolve the rollback's redeploy from the stale
// (already green) run its own original release deploy left behind at the
// same sha/workflow — StatusForCommitSince (keyed on Rollback.StartedAt) must
// ignore it and keep the release rolling_back until a NEW run appears.
func TestSweepRollingBackIgnoresARunThatStartedBeforeTheRollback(t *testing.T) {
	f := newRollbackFixture()
	ds := newFakeDeployStatus()
	f.svc.deployStatus = ds
	repositoryID := uuid.New()
	componentID := uuid.New()
	task := f.withTask(repositoryID, "T-1", mergeSHA)

	startedAt := f.clock.Now()
	r := domain.Release{
		RepositoryID: repositoryID, ComponentID: &componentID, Status: domain.ReleaseRollingBack,
		CommitSHA: mergeSHA,
		Profile:   deliveryProfile(domain.DeliveryOnMerge, domain.ExecutorGitHubActions),
		Rollback: &domain.ReleaseRollback{
			Reason: domain.RollbackHealthIncident, RevertSHA: "revert00000000000000000000000000000000",
			RestoredRef: "revert00000000000000000000000000000000",
			Mechanism:   domain.RollbackMechanismRevert,
			StartedAt:   startedAt,
		},
	}
	created, err := f.store.Create(context.Background(), r, []uuid.UUID{task.ID})
	require.NoError(t, err)
	// A stale run for the very same sha/workflow, from before the rollback
	// even started (e.g. the release's own original deploy of this tag).
	ds.set("revert00000000000000000000000000000000", "deploy.yml", domain.DeployWatchStatus{State: domain.DeployWatchSuccess})

	f.svc.sweepRollingBack(context.Background(), created)

	got, err := f.store.Get(context.Background(), created.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.ReleaseRollingBack, got.Status, "the stale run must not be read as the rollback's own")
}

// --- §H3: a revert failure after a successful provider rollback must say so
// and keep the provider outcome on the record. ---

func TestRollbackRevertFailureAfterProviderSuccessKeepsTheProviderOutcome(t *testing.T) {
	f := newRollbackFixture()
	repositoryID := uuid.New()
	componentID := uuid.New()
	envID := f.bindProdEnv(repositoryID, componentID)
	f.envs.rollbackable[envID] = true
	f.envs.deployments[envID] = []domain.CloudDeployment{
		{ID: "dpl_prev", Status: domain.CloudDeployReady, CreatedAt: f.clock.Now().Add(-time.Hour), Environment: domain.EnvironmentProduction},
	}
	f.reverter.err = errors.New("conflict in server/internal/foo.go")

	task := f.withTask(repositoryID, "T-1", mergeSHA)
	f.parked.parked[task.ID] = task
	created, err := f.store.Create(context.Background(), awaitingVerdictRelease(repositoryID, componentID, domain.DeliveryOnMerge, true), []uuid.UUID{task.ID})
	require.NoError(t, err)

	_, err = f.svc.Rollback(context.Background(), created.ID, domain.ReleaseActorAgent, domain.RollbackHealthIncident, "health failing")
	require.Error(t, err)

	got, err := f.store.Get(context.Background(), created.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.ReleaseFailed, got.Status)
	assert.Contains(t, got.FailureReason, "production WAS rolled back")
	assert.Contains(t, got.FailureReason, "was NOT reverted")
	require.NotNil(t, got.Rollback, "the provider outcome must survive the revert failure")
	assert.Equal(t, domain.RollbackMechanismProvider, got.Rollback.Mechanism)
	assert.Equal(t, "dpl_prev", got.Rollback.ProviderDeploymentID)
	require.Len(t, f.envs.rollbackCalls, 1)
	assert.Contains(t, f.parked.taken, task.ID, "a revert failure must still hand back so a human is woken")
}

// --- §M3: Rollback claims rolling_back BEFORE any side effect. ---

func TestRollbackClaimsRollingBackBeforeTheRevertRuns(t *testing.T) {
	f := newRollbackFixture()
	repositoryID := uuid.New()
	componentID := uuid.New()
	task := f.withTask(repositoryID, "T-1", mergeSHA)
	created, err := f.store.Create(context.Background(), awaitingVerdictRelease(repositoryID, componentID, domain.DeliveryOnMerge, true), []uuid.UUID{task.ID})
	require.NoError(t, err)

	observed := observingReverter{store: f.store, releaseID: created.ID, sha: "revertobserved00000000000000000000000000"}
	f.svc.reverter = &observed

	updated, err := f.svc.Rollback(context.Background(), created.ID, domain.ReleaseActorAgent, domain.RollbackHealthIncident, "health failing")
	require.NoError(t, err)
	assert.Equal(t, domain.ReleaseRollingBack, updated.Status)

	require.True(t, observed.sawClaim, "the reverter must run after the claiming Update, not before")
	assert.Equal(t, domain.ReleaseRollingBack, observed.statusAtRevertTime)
	assert.Empty(t, observed.restoredRefAtRevertTime, "RestoredRef is not filled until the outcome Update, after the revert")
}

// observingReverter is release.Reverter: it reads the release straight out of
// the store the instant it is invoked, so a test can see exactly what
// Rollback persisted before running any side effect (§M3).
type observingReverter struct {
	store     *fakeReleaseStore
	releaseID uuid.UUID
	sha       string

	sawClaim                bool
	statusAtRevertTime      domain.ReleaseStatus
	restoredRefAtRevertTime string
}

func (o *observingReverter) RevertOnDefaultBranch(ctx context.Context, _ string, _ []string, _ string) (string, error) {
	r, err := o.store.Get(ctx, o.releaseID)
	if err == nil {
		o.sawClaim = true
		o.statusAtRevertTime = r.Status
		if r.Rollback != nil {
			o.restoredRefAtRevertTime = r.Rollback.RestoredRef
		}
	}
	return o.sha, nil
}

// A rolling_back release whose Rollback.RestoredRef never got filled (the
// process died between the claim and the outcome Update) must eventually be
// failed by the sweeper, not left stuck forever — but only after a grace
// window, since a brand-new claim looks the same for the moment its own
// side effects take.
func TestSweepRollingBackFailsAnAbandonedClaimAfterTheGraceWindow(t *testing.T) {
	f := newRollbackFixture()
	repositoryID := uuid.New()
	componentID := uuid.New()
	task := f.withTask(repositoryID, "T-1", mergeSHA)
	startedAt := f.clock.Now()
	r := domain.Release{
		RepositoryID: repositoryID, ComponentID: &componentID, Status: domain.ReleaseRollingBack,
		CommitSHA: mergeSHA,
		Profile:   deliveryProfile(domain.DeliveryOnMerge, domain.ExecutorGitHubActions),
		Rollback:  &domain.ReleaseRollback{Reason: domain.RollbackHealthIncident, StartedAt: startedAt},
	}
	created, err := f.store.Create(context.Background(), r, []uuid.UUID{task.ID})
	require.NoError(t, err)

	f.svc.sweepRollingBack(context.Background(), created)
	stillWaiting, err := f.store.Get(context.Background(), created.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.ReleaseRollingBack, stillWaiting.Status, "within the grace window a claim with no RestoredRef yet is normal")

	f.clock.Advance(6 * time.Minute)
	f.svc.sweepRollingBack(context.Background(), stillWaiting)

	got, err := f.store.Get(context.Background(), created.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.ReleaseFailed, got.Status)
	assert.Contains(t, got.FailureReason, "server may have restarted")
}

// --- §M4: retrying a failed rollback must not re-revert. ---

func TestRollbackRetryAfterAFailedRedeploySkipsTheRevert(t *testing.T) {
	f := newRollbackFixture()
	repositoryID := uuid.New()
	componentID := uuid.New()
	task := f.withTask(repositoryID, "T-1", mergeSHA)
	failed := awaitingVerdictRelease(repositoryID, componentID, domain.DeliveryDispatch, true)
	failed.Status = domain.ReleaseFailed
	deployedAt := f.clock.Now().Add(-2 * time.Hour)
	failed.DeployedAt = &deployedAt
	failed.FailureReason = "the rollback deploy failed — production may still run the bad release: boom"
	failed.Rollback = &domain.ReleaseRollback{
		Reason: domain.RollbackDeployFailed, RevertSHA: "revertalreadydone000000000000000000000000",
		Mechanism: domain.RollbackMechanismWorkflow, Actor: string(domain.ReleaseActorAgent), StartedAt: f.clock.Now().Add(-time.Minute),
	}
	created, err := f.store.Create(context.Background(), failed, []uuid.UUID{task.ID})
	require.NoError(t, err)

	updated, err := f.svc.Rollback(context.Background(), created.ID, domain.ReleaseActorAgent, domain.RollbackDeployFailed, "retrying")
	require.NoError(t, err)

	assert.Equal(t, domain.ReleaseRollingBack, updated.Status)
	require.NotNil(t, updated.Rollback)
	assert.Equal(t, "revertalreadydone000000000000000000000000", updated.Rollback.RevertSHA, "the earlier revert is reused, not redone")
	assert.Empty(t, f.reverter.calls, "a retry must never revert a second time")
	require.Len(t, f.actions.dispatchCalls, 1, "the redeploy itself is retried")
}

// --- §M5: refuse to roll back a released release while the component has a
// newer open release. ---

func TestRollbackRefusesAReleasedReleaseWithANewerOpenRelease(t *testing.T) {
	f := newRollbackFixture()
	repositoryID := uuid.New()
	componentID := uuid.New()
	finishedAt := f.clock.Now().Add(-time.Hour)
	old, err := f.store.Create(context.Background(), domain.Release{
		RepositoryID: repositoryID, ComponentID: &componentID, Status: domain.ReleaseReleased,
		CommitSHA: mergeSHA, FinishedAt: &finishedAt, CreatedAt: f.clock.Now().Add(-2 * time.Hour),
		Profile: domain.ComponentDelivery{AutoRollback: true},
	}, nil)
	require.NoError(t, err)
	_, err = f.store.Create(context.Background(), domain.Release{
		RepositoryID: repositoryID, ComponentID: &componentID, Status: domain.ReleaseDeploying,
		CommitSHA: "newer0000000000000000000000000000000000", CreatedAt: f.clock.Now(),
		Version: "newer00",
	}, nil)
	require.NoError(t, err)

	_, err = f.svc.Rollback(context.Background(), old.ID, domain.ReleaseActorAgent, domain.RollbackHealthIncident, "note")
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrReleaseWrongStatus)
	assert.Contains(t, err.Error(), "newer00")
	assert.Empty(t, f.reverter.calls, "a refused rollback must never touch the default branch")
}

// A failed/awaiting_verdict release is neither Open() nor Terminal(), so a
// later merge's OpenForMerge never supersedes it — the newer-open-release
// check has to apply to it too, not just to released.
func TestRollbackRefusesAFailedReleaseWithANewerOpenRelease(t *testing.T) {
	f := newRollbackFixture()
	repositoryID := uuid.New()
	componentID := uuid.New()
	task := f.withTask(repositoryID, "T-1", mergeSHA)
	stuck := awaitingVerdictRelease(repositoryID, componentID, domain.DeliveryDispatch, true)
	stuck.Status = domain.ReleaseFailed
	stuck.CreatedAt = f.clock.Now().Add(-time.Hour)
	old, err := f.store.Create(context.Background(), stuck, []uuid.UUID{task.ID})
	require.NoError(t, err)
	_, err = f.store.Create(context.Background(), domain.Release{
		RepositoryID: repositoryID, ComponentID: &componentID, Status: domain.ReleasePending,
		CommitSHA: "newer0000000000000000000000000000000000", CreatedAt: f.clock.Now(), Version: "newer00",
	}, nil)
	require.NoError(t, err)

	_, err = f.svc.Rollback(context.Background(), old.ID, domain.ReleaseActorAgent, domain.RollbackDeployFailed, "note")
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrReleaseWrongStatus)
	assert.Contains(t, err.Error(), "newer00")
}
