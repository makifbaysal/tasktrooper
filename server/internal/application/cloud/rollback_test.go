package cloud_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/cloud"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// fakeRollbackProvider adds port.CloudRollbacker on top of the package's
// existing fakeProvider (fakes_test.go), the same way a real adapter adds it
// on top of port.CloudProvider.
type fakeRollbackProvider struct {
	*fakeProvider

	current    domain.CloudDeployment
	currentErr error

	rollbackErr   error
	rollbackCalls []string

	promoteErr   error
	promoteCalls []string
}

var _ port.CloudProvider = (*fakeRollbackProvider)(nil)
var _ port.CloudRollbacker = (*fakeRollbackProvider)(nil)

func (f *fakeRollbackProvider) Current(ctx context.Context, cred domain.CloudCredential, ref domain.CloudResourceRef) (domain.CloudDeployment, error) {
	return f.current, f.currentErr
}

func (f *fakeRollbackProvider) RollbackTo(ctx context.Context, cred domain.CloudCredential, ref domain.CloudResourceRef, deploymentID string) error {
	f.rollbackCalls = append(f.rollbackCalls, deploymentID)
	return f.rollbackErr
}

func (f *fakeRollbackProvider) Promote(ctx context.Context, cred domain.CloudCredential, ref domain.CloudResourceRef, deploymentID string) error {
	f.promoteCalls = append(f.promoteCalls, deploymentID)
	return f.promoteErr
}

// rollbackHarness wires a Service to one bound production environment over a
// given provider (a plain fakeProvider for the "no capability" cases, a
// fakeRollbackProvider for everything else).
type rollbackHarness struct {
	svc  *cloud.Service
	envs *fakeEnvironments
	accs *fakeAccounts
	env  domain.ComponentEnvironment
}

func newRollbackHarness(t *testing.T, provider port.CloudProvider) rollbackHarness {
	t.Helper()
	accs := newFakeAccounts()
	envs := newFakeEnvironments()

	acct, err := accs.CreateCloudAccount(context.Background(), domain.CloudAccount{Provider: domain.CloudVercel}, map[string]string{"token": "tok"})
	require.NoError(t, err)

	env := envs.seed(domain.ComponentEnvironment{
		RepositoryID: uuid.New(),
		ComponentID:  uuid.New(),
		Environment:  domain.EnvironmentProduction,
		Provider:     domain.CloudVercel,
		AccountID:    &acct.ID,
		Resource:     &domain.CloudResourceRef{Kind: domain.CloudResourceVercelProject, ID: "prj_1"},
	})

	svc := cloud.NewService(cloud.Deps{
		Accounts:     accs,
		Environments: envs,
		Providers:    []port.CloudProvider{provider},
	})
	return rollbackHarness{svc: svc, envs: envs, accs: accs, env: env}
}

func TestCanRollbackTrueWhenProviderImplementsCloudRollbacker(t *testing.T) {
	h := newRollbackHarness(t, &fakeRollbackProvider{fakeProvider: &fakeProvider{kind: domain.CloudVercel}})
	assert.True(t, h.svc.CanRollback(context.Background(), h.env.ID))
}

func TestCanRollbackFalseWhenProviderLacksTheCapability(t *testing.T) {
	h := newRollbackHarness(t, &fakeProvider{kind: domain.CloudVercel})
	assert.False(t, h.svc.CanRollback(context.Background(), h.env.ID))
}

func TestCanRollbackFalseWhenEnvironmentIsUnbound(t *testing.T) {
	accs := newFakeAccounts()
	envs := newFakeEnvironments()
	env := envs.seed(domain.ComponentEnvironment{RepositoryID: uuid.New(), ComponentID: uuid.New(), Environment: domain.EnvironmentProduction})
	svc := cloud.NewService(cloud.Deps{Accounts: accs, Environments: envs, Providers: []port.CloudProvider{&fakeRollbackProvider{fakeProvider: &fakeProvider{kind: domain.CloudVercel}}}})

	assert.False(t, svc.CanRollback(context.Background(), env.ID))
}

func TestCurrentDeploymentReturnsTheProviderAnswer(t *testing.T) {
	want := domain.CloudDeployment{ID: "dpl_1", Status: domain.CloudDeployReady, CommitSHA: "abc123"}
	h := newRollbackHarness(t, &fakeRollbackProvider{fakeProvider: &fakeProvider{kind: domain.CloudVercel}, current: want})

	got, err := h.svc.CurrentDeployment(context.Background(), h.env.ID)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestCurrentDeploymentUnboundIsNotConnected(t *testing.T) {
	accs := newFakeAccounts()
	envs := newFakeEnvironments()
	env := envs.seed(domain.ComponentEnvironment{RepositoryID: uuid.New(), ComponentID: uuid.New(), Environment: domain.EnvironmentProduction})
	svc := cloud.NewService(cloud.Deps{Accounts: accs, Environments: envs, Providers: []port.CloudProvider{&fakeRollbackProvider{fakeProvider: &fakeProvider{kind: domain.CloudVercel}}}})

	_, err := svc.CurrentDeployment(context.Background(), env.ID)
	require.Error(t, err)
	assert.ErrorIs(t, err, cloud.ErrNotConnected)
}

func TestCurrentDeploymentUnsupportedWhenProviderLacksCapability(t *testing.T) {
	h := newRollbackHarness(t, &fakeProvider{kind: domain.CloudVercel})

	_, err := h.svc.CurrentDeployment(context.Background(), h.env.ID)
	require.Error(t, err)
	assert.ErrorIs(t, err, port.ErrUnsupported)
}

func TestRollbackEnvironmentCallsRollbackToWithTheDeploymentID(t *testing.T) {
	fp := &fakeRollbackProvider{fakeProvider: &fakeProvider{kind: domain.CloudVercel}}
	h := newRollbackHarness(t, fp)

	err := h.svc.RollbackEnvironment(context.Background(), h.env.ID, "dpl_prev")
	require.NoError(t, err)
	assert.Equal(t, []string{"dpl_prev"}, fp.rollbackCalls)
}

func TestRollbackEnvironmentPropagatesWriteDenied(t *testing.T) {
	fp := &fakeRollbackProvider{fakeProvider: &fakeProvider{kind: domain.CloudVercel}, rollbackErr: port.ErrCloudWriteDenied}
	h := newRollbackHarness(t, fp)

	err := h.svc.RollbackEnvironment(context.Background(), h.env.ID, "dpl_prev")
	require.Error(t, err)
	assert.ErrorIs(t, err, port.ErrCloudWriteDenied)
}

func TestRollbackEnvironmentUnsupportedWhenProviderLacksCapability(t *testing.T) {
	h := newRollbackHarness(t, &fakeProvider{kind: domain.CloudVercel})

	err := h.svc.RollbackEnvironment(context.Background(), h.env.ID, "dpl_prev")
	require.Error(t, err)
	assert.ErrorIs(t, err, port.ErrUnsupported)
}

func TestRollbackEnvironmentMarksTheAccountOnCloudAuthFailure(t *testing.T) {
	fp := &fakeRollbackProvider{fakeProvider: &fakeProvider{kind: domain.CloudVercel}, rollbackErr: port.ErrCloudAuth}
	h := newRollbackHarness(t, fp)

	err := h.svc.RollbackEnvironment(context.Background(), h.env.ID, "dpl_prev")
	require.Error(t, err)
	assert.ErrorIs(t, err, port.ErrCloudAuth)

	acct, getErr := h.accs.GetCloudAccount(context.Background(), *h.env.AccountID)
	require.NoError(t, getErr)
	assert.Equal(t, domain.CloudAccountError, acct.Status)
}

func TestPromoteDeploymentCallsPromoteWithTheDeploymentID(t *testing.T) {
	fp := &fakeRollbackProvider{fakeProvider: &fakeProvider{kind: domain.CloudVercel}}
	h := newRollbackHarness(t, fp)

	err := h.svc.PromoteDeployment(context.Background(), h.env.ID, "dpl_revert")
	require.NoError(t, err)
	assert.Equal(t, []string{"dpl_revert"}, fp.promoteCalls)
}

func TestPromoteDeploymentPropagatesFailure(t *testing.T) {
	fp := &fakeRollbackProvider{fakeProvider: &fakeProvider{kind: domain.CloudVercel}, promoteErr: errors.New("promote failed")}
	h := newRollbackHarness(t, fp)

	err := h.svc.PromoteDeployment(context.Background(), h.env.ID, "dpl_revert")
	require.Error(t, err)
	assert.Equal(t, "promote failed", err.Error())
}

func TestPromoteDeploymentUnsupportedWhenProviderLacksCapability(t *testing.T) {
	h := newRollbackHarness(t, &fakeProvider{kind: domain.CloudVercel})

	err := h.svc.PromoteDeployment(context.Background(), h.env.ID, "dpl_revert")
	require.Error(t, err)
	assert.ErrorIs(t, err, port.ErrUnsupported)
}
