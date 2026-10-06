package release

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type fakeEnvGate struct {
	result  domain.EnvEnsureResult
	err     error
	calls   int
	gotTask *uuid.UUID
}

func (f *fakeEnvGate) Ensure(_ context.Context, _, _ uuid.UUID, taskID *uuid.UUID) (domain.EnvEnsureResult, error) {
	f.calls++
	f.gotTask = taskID
	return f.result, f.err
}

func envFixture(gate *fakeEnvGate) (*orderFixture, domain.BoardTask) {
	dependency := domain.BoardTask{ID: uuid.New(), Key: "T-1", Column: domain.TaskColumnReleased}
	dependent := domain.BoardTask{ID: uuid.New(), Key: "T-2", Column: domain.TaskColumnDone}
	f := newOrderFixture(domain.DeliveryOnMerge, dependent, dependency).withHolds()
	f.svc.envs = gate
	return f, f.tasks.tasks[dependent.ID]
}

func TestMergeGateHoldsWhileAHumanMustEnterEnvVars(t *testing.T) {
	gate := &fakeEnvGate{result: domain.EnvEnsureResult{Missing: []string{"GITHUB_TOKEN", "ADMIN_PASSWORD_HASH"}}}
	f, task := envFixture(gate)

	err := f.svc.MergeGate(context.Background(), f.repo, task)

	require.ErrorIs(t, err, domain.ErrDeployEnvMissing)
	assert.Contains(t, err.Error(), "GITHUB_TOKEN, ADMIN_PASSWORD_HASH")
	assert.Contains(t, err.Error(), "Do not retry")
	held := f.tasks.tasks[task.ID]
	assert.Equal(t, domain.ResourceDeployEnv, held.BlockedResource)
	assert.Equal(t, "GITHUB_TOKEN, ADMIN_PASSWORD_HASH", held.BlockedQuestion)
	require.Len(t, f.tasks.comments, 1)
	assert.Contains(t, f.tasks.comments[0].Content, "GITHUB_TOKEN")
	require.NotNil(t, gate.gotTask)
	assert.Equal(t, task.ID, *gate.gotTask, "the task's own checkout is what ships")
}

func TestMergeGateSaysWhatItCreatedAndLetsTheMergeGo(t *testing.T) {
	gate := &fakeEnvGate{result: domain.EnvEnsureResult{Created: []string{"SESSION_SECRET"}}}
	f, task := envFixture(gate)

	require.NoError(t, f.svc.MergeGate(context.Background(), f.repo, task))
	require.Len(t, f.tasks.comments, 1)
	assert.Contains(t, f.tasks.comments[0].Content, "SESSION_SECRET")
	assert.Empty(t, f.tasks.tasks[task.ID].BlockedResource)
}

func TestMergeGateRefusesWhenTheEnvVarsCannotBeRead(t *testing.T) {
	gate := &fakeEnvGate{err: fmt.Errorf("%w: vercel down", domain.ErrDeployEnvUnchecked)}
	f, task := envFixture(gate)

	err := f.svc.MergeGate(context.Background(), f.repo, task)

	require.ErrorIs(t, err, domain.ErrDeployEnvUnchecked)
	assert.Equal(t, domain.ResourceDeployEnv, f.tasks.tasks[task.ID].BlockedResource)
}

func TestMergeGateChecksEnvVarsOnlyAfterTheDeployOrderIsSatisfied(t *testing.T) {
	gate := &fakeEnvGate{}
	dependency := domain.BoardTask{ID: uuid.New(), Key: "T-1", Column: domain.TaskColumnInProgress}
	dependent := domain.BoardTask{ID: uuid.New(), Key: "T-2", Column: domain.TaskColumnDone}
	f := newOrderFixture(domain.DeliveryOnMerge, dependent, dependency).withHolds()
	f.svc.envs = gate

	err := f.svc.MergeGate(context.Background(), f.repo, f.tasks.tasks[dependent.ID])

	require.ErrorIs(t, err, domain.ErrDeployDependencyPending)
	assert.Zero(t, gate.calls)
}

func TestMergeGateWithoutAnEnvGateIsUnchanged(t *testing.T) {
	f, task := envFixture(nil)
	f.svc.envs = nil

	assert.NoError(t, f.svc.MergeGate(context.Background(), f.repo, task))
}

func TestResumeEnvHoldsWakesOnlyTheTasksWaitingOnEnvVars(t *testing.T) {
	f, waiting := envFixture(&fakeEnvGate{})
	waiting.BlockedResource = domain.ResourceDeployEnv
	f.tasks.tasks[waiting.ID] = waiting
	other := domain.BoardTask{ID: uuid.New(), Key: "T-3", Column: domain.TaskColumnDone, BlockedResource: domain.ResourceBeforeDeploy}
	f.tasks.tasks[other.ID] = other

	f.svc.ResumeEnvHolds(context.Background(), f.repo)

	require.Len(t, f.waker.calls, 1)
	assert.Equal(t, waiting.ID, f.waker.calls[0].task.ID)
	assert.Empty(t, f.tasks.tasks[waiting.ID].BlockedResource)
	assert.Equal(t, domain.ResourceBeforeDeploy, f.tasks.tasks[other.ID].BlockedResource)
}

func TestTheCreatedCommentWarnsWhenADeployRewritesTheConfiguration(t *testing.T) {
	gate := &fakeEnvGate{result: domain.EnvEnsureResult{Created: []string{"SESSION_SECRET"}, OverwrittenOnDeploy: true}}
	f, task := envFixture(gate)

	require.NoError(t, f.svc.MergeGate(context.Background(), f.repo, task))
	require.Len(t, f.tasks.comments, 1)
	assert.Contains(t, f.tasks.comments[0].Content, "task definition")
}
