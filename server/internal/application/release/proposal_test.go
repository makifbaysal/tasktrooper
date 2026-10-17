package release

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/notify"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port/mocks"
)

func (f *rollbackFixture) awaiting(t *testing.T, autoRollback bool) domain.Release {
	t.Helper()
	repositoryID := uuid.New()
	task := f.withTask(repositoryID, "T-1", mergeSHA)
	created, err := f.store.Create(context.Background(), awaitingVerdictRelease(repositoryID, uuid.New(), domain.DeliveryOnMerge, autoRollback), []uuid.UUID{task.ID})
	require.NoError(t, err)
	return created
}

func (f *rollbackFixture) propose(t *testing.T, r domain.Release) {
	t.Helper()
	_, err := f.svc.Rollback(context.Background(), r.ID, domain.ReleaseActorAgent, domain.RollbackHealthIncident, "error rate up 2 points")
	require.ErrorIs(t, err, domain.ErrRollbackNeedsHuman)
}

func TestAnAgentsProposalIsStoredBeforeItIsTold(t *testing.T) {
	f := newRollbackFixture()
	r := f.awaiting(t, false)
	var storedWhenTold *domain.RollbackProposal
	channel := mocks.NewNotificationChannel(t)
	channel.On("Kind").Return(domain.NotificationChannelDesktop).Maybe()
	channel.On("Send", mock.Anything, mock.Anything).Run(func(mock.Arguments) {
		got, err := f.store.Get(context.Background(), r.ID)
		require.NoError(t, err)
		storedWhenTold = got.RollbackProposal
	}).Return(nil).Once()
	notifier := notify.NewService()
	notifier.Register(channel)
	f.svc.notifier = notifier

	f.propose(t, r)

	require.NotNil(t, storedWhenTold)
	got, err := f.svc.Get(context.Background(), r.ID)
	require.NoError(t, err)
	require.NotNil(t, got.RollbackProposal)
	assert.Equal(t, domain.ReleaseActorAgent, got.RollbackProposal.By)
	assert.Equal(t, domain.RollbackHealthIncident, got.RollbackProposal.Reason)
	assert.Equal(t, "error rate up 2 points", got.RollbackProposal.Note)
	assert.Equal(t, f.clock.Now().UTC(), got.RollbackProposal.ProposedAt)
	assert.Nil(t, got.RollbackProposal.IncidentID)
	assert.True(t, got.RollbackAllowed)
	assert.Empty(t, got.RollbackRefusal)
}

func TestAProposalThatCannotBeStoredIsNotTold(t *testing.T) {
	f := newRollbackFixture()
	sent := withNotifier(t, f.svc)
	r := f.awaiting(t, false)
	f.svc.store = failingProposalStore{fakeReleaseStore: f.store}

	_, err := f.svc.Rollback(context.Background(), r.ID, domain.ReleaseActorAgent, domain.RollbackHealthIncident, "errors")

	require.Error(t, err)
	assert.NotErrorIs(t, err, domain.ErrRollbackNeedsHuman)
	assert.Empty(t, sent.all())
}

type failingProposalStore struct{ *fakeReleaseStore }

func (failingProposalStore) SetRollbackProposal(context.Context, uuid.UUID, *domain.RollbackProposal) error {
	return errors.New("database is down")
}

func TestARollbackClearsTheProposal(t *testing.T) {
	f := newRollbackFixture()
	r := f.awaiting(t, false)
	f.propose(t, r)

	updated, err := f.svc.Rollback(context.Background(), r.ID, domain.ReleaseActorHuman, domain.RollbackManual, "approved")
	require.NoError(t, err)

	assert.Equal(t, domain.ReleaseRollingBack, updated.Status)
	assert.Nil(t, updated.RollbackProposal)
	stored, err := f.store.Get(context.Background(), r.ID)
	require.NoError(t, err)
	assert.Nil(t, stored.RollbackProposal)
}

func TestDismissingAProposalClearsIt(t *testing.T) {
	f := newRollbackFixture()
	r := f.awaiting(t, false)
	f.propose(t, r)

	got, err := f.svc.DismissRollbackProposal(context.Background(), r.ID)
	require.NoError(t, err)
	assert.Nil(t, got.RollbackProposal)
	assert.True(t, got.RollbackAllowed, "a rollback stays possible by hand")

	again, err := f.svc.DismissRollbackProposal(context.Background(), r.ID)
	require.NoError(t, err, "dismissing nothing is answered as it is")
	assert.Nil(t, again.RollbackProposal)

	_, err = f.svc.DismissRollbackProposal(context.Background(), uuid.New())
	assert.ErrorIs(t, err, domain.ErrReleaseNotFound)
}

func TestShippingANewerReleaseClearsTheOlderProposals(t *testing.T) {
	f := newRollbackFixture()
	repositoryID, componentID := uuid.New(), uuid.New()
	finished := f.clock.Now().Add(-2 * time.Hour)
	older, err := f.store.Create(context.Background(), domain.Release{
		RepositoryID: repositoryID, ComponentID: &componentID, Status: domain.ReleaseReleased,
		FinishedAt: &finished, CreatedAt: f.clock.Now().Add(-3 * time.Hour),
	}, nil)
	require.NoError(t, err)
	otherComponent := uuid.New()
	other, err := f.store.Create(context.Background(), domain.Release{
		RepositoryID: repositoryID, ComponentID: &otherComponent, Status: domain.ReleaseReleased,
		FinishedAt: &finished, CreatedAt: f.clock.Now().Add(-3 * time.Hour),
	}, nil)
	require.NoError(t, err)
	proposal := &domain.RollbackProposal{ProposedAt: finished, By: domain.ReleaseActorAgent, Reason: domain.RollbackHealthIncident}
	require.NoError(t, f.store.SetRollbackProposal(context.Background(), older.ID, proposal))
	require.NoError(t, f.store.SetRollbackProposal(context.Background(), other.ID, proposal))
	newer := awaitingVerdictRelease(repositoryID, componentID, domain.DeliveryOnMerge, false)
	newer.CreatedAt = f.clock.Now().Add(-time.Hour)
	created, err := f.store.Create(context.Background(), newer, nil)
	require.NoError(t, err)

	_, err = f.svc.Finish(context.Background(), created.ID, domain.ReleaseActorAgent, "good")
	require.NoError(t, err)
	f.clock.Advance(time.Second)

	got, err := f.svc.Get(context.Background(), older.ID)
	require.NoError(t, err)
	assert.Nil(t, got.RollbackProposal)
	assert.False(t, got.RollbackAllowed)
	assert.Equal(t, domain.RollbackRefusalNewerShipped, got.RollbackRefusal)
	kept, err := f.store.Get(context.Background(), other.ID)
	require.NoError(t, err)
	assert.NotNil(t, kept.RollbackProposal, "another component's release is not this one's to clear")
}

func TestEveryReadAnswersWhetherARollbackWouldRun(t *testing.T) {
	f := newRollbackFixture()
	repositoryID, componentID := uuid.New(), uuid.New()
	now := f.clock.Now()
	at := func(d time.Duration) *time.Time { v := now.Add(d); return &v }
	create := func(status domain.ReleaseStatus, created time.Duration, finished *time.Time) domain.Release {
		r, err := f.store.Create(context.Background(), domain.Release{
			RepositoryID: repositoryID, ComponentID: &componentID, Status: status,
			CreatedAt: now.Add(created), FinishedAt: finished,
		}, nil)
		require.NoError(t, err)
		return r
	}
	tooOld := create(domain.ReleaseReleased, -30*time.Hour, at(-26*time.Hour))
	shippedOver := create(domain.ReleaseReleased, -5*time.Hour, at(-4*time.Hour))
	failedEarlier := create(domain.ReleaseFailed, -4*time.Hour, nil)
	newest := create(domain.ReleaseReleased, -3*time.Hour, at(-2*time.Hour))
	awaitingWithOpen := create(domain.ReleaseAwaitingVerdict, -90*time.Minute, nil)
	pending := create(domain.ReleasePending, -time.Hour, nil)
	rolledBack := create(domain.ReleaseRolledBack, -10*time.Hour, at(-9*time.Hour))

	want := map[uuid.UUID]string{
		tooOld.ID:           domain.RollbackRefusalTooOld,
		shippedOver.ID:      domain.RollbackRefusalNewerShipped,
		failedEarlier.ID:    domain.RollbackRefusalNewerShipped,
		newest.ID:           domain.RollbackRefusalNewerOpen,
		awaitingWithOpen.ID: domain.RollbackRefusalNewerOpen,
		pending.ID:          domain.RollbackRefusalNotDeployed,
		rolledBack.ID:       domain.RollbackRefusalWrongStatus,
	}
	listed, err := f.svc.List(context.Background(), domain.ReleaseListFilter{RepositoryID: &repositoryID, Limit: 50})
	require.NoError(t, err)
	require.Len(t, listed, len(want))
	for _, r := range listed {
		assert.Equal(t, want[r.ID], r.RollbackRefusal, "release %s (%s)", r.ID, r.Status)
		assert.Equal(t, want[r.ID] == "", r.RollbackAllowed)
		one, err := f.svc.Get(context.Background(), r.ID)
		require.NoError(t, err)
		assert.Equal(t, r.RollbackRefusal, one.RollbackRefusal, "Get and List agree")
	}

	_, err = f.svc.Rollback(context.Background(), pending.ID, domain.ReleaseActorHuman, domain.RollbackManual, "")
	assert.ErrorIs(t, err, domain.ErrReleaseWrongStatus, "the refusal is the rollback's own")
}

func TestAnAwaitingReleaseWithNothingNewerMayRollBack(t *testing.T) {
	f := newRollbackFixture()
	r := f.awaiting(t, false)

	got, err := f.svc.Get(context.Background(), r.ID)
	require.NoError(t, err)
	assert.True(t, got.RollbackAllowed)
	assert.Empty(t, got.RollbackRefusal)
}

func TestAProposalAnsweringAnAttributedIncidentNamesIt(t *testing.T) {
	f := newRollbackFixture()
	repositoryID, componentID := uuid.New(), uuid.New()
	task := f.withTask(repositoryID, "T-1", mergeSHA)
	finished := f.clock.Now().Add(-5 * time.Minute)
	released := awaitingVerdictRelease(repositoryID, componentID, domain.DeliveryOnMerge, false)
	released.Status, released.FinishedAt = domain.ReleaseReleased, &finished
	created, err := f.store.Create(context.Background(), released, []uuid.UUID{task.ID})
	require.NoError(t, err)
	incident := domain.Incident{
		ID: uuid.New(), RepositoryID: repositoryID, Env: domain.DeployEnvProd,
		Payload: map[string]any{domain.IncidentPayloadComponentID: componentID.String()},
	}

	_, ok := f.svc.AttributeIncident(context.Background(), incident, f.clock.Now())
	require.True(t, ok)
	f.propose(t, created)

	got, err := f.store.Get(context.Background(), created.ID)
	require.NoError(t, err)
	require.NotNil(t, got.RollbackProposal)
	require.NotNil(t, got.RollbackProposal.IncidentID)
	assert.Equal(t, incident.ID, *got.RollbackProposal.IncidentID)
}

func TestAnIncidentNamingAComponentIsAttributedOnlyToItsReleases(t *testing.T) {
	f := newRollbackFixture()
	repositoryID, api, web := uuid.New(), uuid.New(), uuid.New()
	release := func(componentID uuid.UUID, key string, finishedAgo time.Duration) {
		task := f.withTask(repositoryID, key, mergeSHA)
		finished := f.clock.Now().Add(-finishedAgo)
		r := awaitingVerdictRelease(repositoryID, componentID, domain.DeliveryOnMerge, false)
		r.Status, r.FinishedAt = domain.ReleaseReleased, &finished
		_, err := f.store.Create(context.Background(), r, []uuid.UUID{task.ID})
		require.NoError(t, err)
	}
	release(api, "API-1", 10*time.Minute)
	release(web, "WEB-1", 2*time.Minute)
	incident := domain.Incident{
		ID: uuid.New(), RepositoryID: repositoryID, Env: domain.DeployEnvProd,
		Payload: map[string]any{domain.IncidentPayloadComponentID: api.String()},
	}

	byComponent, ok := f.svc.AttributeIncident(context.Background(), incident, f.clock.Now())
	require.True(t, ok)
	repoWide, ok := f.svc.AttributeRelease(context.Background(), repositoryID, domain.DeployEnvProd, f.clock.Now())
	require.True(t, ok)

	apiRelease, err := f.store.List(context.Background(), domain.ReleaseListFilter{ComponentID: &api})
	require.NoError(t, err)
	webRelease, err := f.store.List(context.Background(), domain.ReleaseListFilter{ComponentID: &web})
	require.NoError(t, err)
	assert.Equal(t, apiRelease[0].Tasks[0].ID, byComponent.TaskID, "the web deploy two minutes ago says nothing about the api")
	assert.Equal(t, webRelease[0].Tasks[0].ID, repoWide.TaskID, "without a component the newest release of the repository")

	incident.Payload = nil
	unnamed, ok := f.svc.AttributeIncident(context.Background(), incident, f.clock.Now())
	require.True(t, ok)
	assert.Equal(t, repoWide.TaskID, unnamed.TaskID)
}

func TestARollbackThatFailsAfterTheRevertLandedIsStillTold(t *testing.T) {
	f := newRollbackFixture()
	sent := withNotifier(t, f.svc)
	f.actions.dispatchErr = errors.New("connection reset by peer")
	repositoryID := uuid.New()
	task := f.withTask(repositoryID, "T-1", mergeSHA)
	created, err := f.store.Create(context.Background(), awaitingVerdictRelease(repositoryID, uuid.New(), domain.DeliveryDispatch, true), []uuid.UUID{task.ID})
	require.NoError(t, err)

	updated, err := f.svc.Rollback(context.Background(), created.ID, domain.ReleaseActorAgent, domain.RollbackDeployFailed, "deploy job red")
	require.NoError(t, err)
	require.Equal(t, domain.ReleaseFailed, updated.Status)

	got := sent.all()
	require.Len(t, got, 1)
	assert.Equal(t, domain.NotificationReleaseRolledBack, got[0].Kind)
	assert.Contains(t, got[0].Body, "deploy job red")
	assert.Contains(t, got[0].Body, "not finished")
	assert.Contains(t, got[0].Body, "connection reset by peer")
}

func TestARollbackThatCouldNotRecordItsRevertIsStillTold(t *testing.T) {
	f := newRollbackFixture()
	sent := withNotifier(t, f.svc)
	repositoryID := uuid.New()
	task := f.withTask(repositoryID, "T-1", mergeSHA)
	created, err := f.store.Create(context.Background(), awaitingVerdictRelease(repositoryID, uuid.New(), domain.DeliveryOnMerge, true), []uuid.UUID{task.ID})
	require.NoError(t, err)
	f.store.beforeUpdate = func(r domain.Release) {
		if r.Rollback != nil && r.Rollback.RevertSHA != "" {
			f.store.updateErr = errors.New("database is down")
		}
	}

	_, err = f.svc.Rollback(context.Background(), created.ID, domain.ReleaseActorAgent, domain.RollbackHealthIncident, "errors")
	require.Error(t, err)

	got := sent.all()
	require.Len(t, got, 1)
	assert.Equal(t, domain.NotificationReleaseRolledBack, got[0].Kind)
	assert.Contains(t, got[0].Body, "database is down")
}

func TestAStoredProposalStopsReWakingTheReleaseEngineer(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	handBack := now.Add(-time.Hour)
	r := domain.Release{Status: domain.ReleaseAwaitingVerdict, LastHandBackAt: &handBack, UpdatedAt: now.Add(-time.Hour)}
	require.True(t, handBackDue(r, now))

	r.RollbackProposal = &domain.RollbackProposal{ProposedAt: handBack.Add(time.Minute)}
	assert.False(t, handBackDue(r, now), "it waits on a human now")

	r.RollbackProposal.ProposedAt = handBack.Add(-time.Minute)
	assert.True(t, handBackDue(r, now), "a proposal from before the hand-back did not answer it")
}
