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

// freshFakeImpact is deploy impact that can also read past its cache.
type freshFakeImpact struct {
	*fakeImpact
	freshReads int
}

func (f *freshFakeImpact) ForReleaseFresh(ctx context.Context, r domain.Release) (domain.DeployImpact, error) {
	f.freshReads++
	return f.ForRelease(ctx, r)
}

func TestASoakWithImpactThresholdsLastsAtLeastTenMinutesAfterTheDeploy(t *testing.T) {
	for _, tt := range []struct {
		name       string
		thresholds bool
		want       time.Duration
	}{
		{"with a threshold", true, minImpactAfter},
		{"without one", false, 3 * time.Minute},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newSweepFixture()
			repositoryID := uuid.New()
			task := f.withTask(repositoryID)
			r := deployingRelease(repositoryID, f.clock.Now())
			r.Profile.Verify.SoakMinutes = 3
			if tt.thresholds {
				r.Profile.Verify.MaxErrorRateDelta = 1
			}
			created, err := f.store.Create(context.Background(), r, []uuid.UUID{task.ID})
			require.NoError(t, err)
			f.ds.set(mergeSHA, "deploy.yml", domain.DeployWatchStatus{State: domain.DeployWatchSuccess})

			f.svc.SweepOnce(context.Background())

			got, err := f.store.Get(context.Background(), created.ID)
			require.NoError(t, err)
			require.NotNil(t, got.VerifyUntil)
			assert.Equal(t, tt.want, got.VerifyUntil.Sub(*got.DeployedAt))
		})
	}
}

func (f *sweepFixture) endedSoakWithSettlingSignal(t *testing.T, impact *freshFakeImpact) uuid.UUID {
	t.Helper()
	f.svc.SetImpact(impact)
	repositoryID := uuid.New()
	task := f.withTask(repositoryID)
	r := verifyingRelease(repositoryID, f.clock.Now().Add(-12*time.Minute), f.clock.Now())
	r.Profile.Verify.MaxErrorRateDelta = 1
	created, err := f.store.Create(context.Background(), r, []uuid.UUID{task.ID})
	require.NoError(t, err)
	return created.ID
}

func sparseErrorRate() domain.SignalImpact {
	s := errorRateImpact(0.4, 0.5)
	s.AfterPoints = 3
	return s
}

func TestASoakWhoseThresholdNeverSettlesEndsWithItUnchecked(t *testing.T) {
	f := newSweepFixture()
	impact := &freshFakeImpact{fakeImpact: &fakeImpact{impact: domain.DeployImpact{Signals: []domain.SignalImpact{sparseErrorRate()}}}}
	id := f.endedSoakWithSettlingSignal(t, impact)

	f.svc.SweepOnce(context.Background())
	got, err := f.store.Get(context.Background(), id)
	require.NoError(t, err)
	assert.Equal(t, domain.ReleaseVerifying, got.Status, "the window is over but the threshold's signal is still settling")
	assert.Empty(t, got.Checks.Impact.Unchecked)
	assert.Equal(t, 1, impact.freshReads, "the would-be last read skips the cache")

	f.clock.Advance(5 * time.Minute)
	f.svc.SweepOnce(context.Background())
	got, err = f.store.Get(context.Background(), id)
	require.NoError(t, err)
	assert.Equal(t, domain.ReleaseVerifying, got.Status)
	assert.Equal(t, 1, impact.freshReads, "while it settles, reads are paced and cached")

	f.clock.Advance(impactSettleGrace)
	f.svc.SweepOnce(context.Background())
	got, err = f.store.Get(context.Background(), id)
	require.NoError(t, err)
	assert.Equal(t, domain.ReleaseAwaitingVerdict, got.Status, "the extension is bounded")
	assert.Equal(t, []string{domain.VerifyMaxErrorRateDelta}, got.Checks.Impact.Unchecked, "a threshold never judged is unchecked, not passed")
	assert.Equal(t, 2, impact.freshReads)
}

func TestASoakWhoseThresholdSettlesInTheGraceIsJudged(t *testing.T) {
	f := newSweepFixture()
	impact := &freshFakeImpact{fakeImpact: &fakeImpact{impact: domain.DeployImpact{Signals: []domain.SignalImpact{sparseErrorRate()}}}}
	id := f.endedSoakWithSettlingSignal(t, impact)
	f.svc.SweepOnce(context.Background())

	impact.impact.Signals = []domain.SignalImpact{errorRateImpact(0.4, 0.5)}
	f.clock.Advance(3 * time.Minute)
	f.svc.SweepOnce(context.Background())
	got, err := f.store.Get(context.Background(), id)
	require.NoError(t, err)
	assert.Equal(t, domain.ReleaseVerifying, got.Status)

	f.clock.Advance(30 * time.Second)
	f.svc.SweepOnce(context.Background())
	got, err = f.store.Get(context.Background(), id)
	require.NoError(t, err)
	assert.Equal(t, domain.ReleaseAwaitingVerdict, got.Status)
	assert.Empty(t, got.Checks.Impact.Unchecked)
	assert.False(t, got.Checks.Impact.Signals[0].Settling)
}

type scopeKey struct{}

func scoped(name string) context.Context {
	return context.WithValue(context.Background(), scopeKey{}, name)
}

func TestTheSoakPacingOfAnotherScopeIsNotForgotten(t *testing.T) {
	f := newSweepFixture()
	f.svc.SetScope(func(ctx context.Context) string { v, _ := ctx.Value(scopeKey{}).(string); return v })
	id, now := uuid.New(), f.clock.Now()
	f.svc.markErrorChecked(scoped("b"), id, now)
	f.svc.markImpactChecked(scoped("b"), id, now)

	f.svc.forgetUnverifiedChecks(scoped("a"), nil)
	assert.False(t, f.svc.errorCheckDue(scoped("b"), id, now), "a sweep of tenant a leaves b's pacing")
	assert.True(t, f.svc.errorCheckDue(scoped("a"), id, now))

	f.svc.forgetUnverifiedChecks(scoped("b"), nil)
	assert.True(t, f.svc.errorCheckDue(scoped("b"), id, now))
	assert.True(t, f.svc.impactCheckDue(scoped("b"), id, now))
}

func (f *rollbackFixture) releasedWithIncident(t *testing.T) (domain.Release, domain.Incident) {
	t.Helper()
	repositoryID, componentID := uuid.New(), uuid.New()
	task := f.withTask(repositoryID, "T-1", mergeSHA)
	finished := f.clock.Now().Add(-5 * time.Minute)
	r := awaitingVerdictRelease(repositoryID, componentID, domain.DeliveryOnMerge, false)
	r.Status, r.FinishedAt = domain.ReleaseReleased, &finished
	created, err := f.store.Create(context.Background(), r, []uuid.UUID{task.ID})
	require.NoError(t, err)
	incident := domain.Incident{
		ID: uuid.New(), RepositoryID: repositoryID, Env: domain.DeployEnvProd,
		Payload: map[string]any{domain.IncidentPayloadComponentID: componentID.String()},
	}
	_, ok := f.svc.AttributeIncident(context.Background(), incident, f.clock.Now())
	require.True(t, ok)
	return created, incident
}

func TestADismissedProposalIsNotProposedAgainUntilANewIncident(t *testing.T) {
	f := newRollbackFixture()
	sent := withNotifier(t, f.svc)
	r, incident := f.releasedWithIncident(t)
	f.propose(t, r)
	require.Len(t, sent.all(), 1)

	dismissed, err := f.svc.DismissRollbackProposal(context.Background(), r.ID)
	require.NoError(t, err)
	assert.Nil(t, dismissed.RollbackProposal)
	require.NotNil(t, dismissed.RollbackDismissal)
	assert.Equal(t, incident.ID, *dismissed.RollbackDismissal.IncidentID)

	_, err = f.svc.Rollback(context.Background(), r.ID, domain.ReleaseActorAgent, domain.RollbackHealthIncident, "still bad")
	require.ErrorIs(t, err, domain.ErrRollbackProposalDismissed)
	assert.Contains(t, err.Error(), incident.ID.String())
	stored, err := f.store.Get(context.Background(), r.ID)
	require.NoError(t, err)
	assert.Nil(t, stored.RollbackProposal, "the dismissed proposal does not come back")
	assert.Len(t, sent.all(), 1, "and nobody is told it again")

	again := incident
	again.ID = uuid.New()
	_, ok := f.svc.AttributeIncident(context.Background(), again, f.clock.Now())
	require.True(t, ok)
	f.propose(t, r)
	stored, err = f.store.Get(context.Background(), r.ID)
	require.NoError(t, err)
	require.NotNil(t, stored.RollbackProposal, "a new incident may be answered")
	assert.Equal(t, again.ID, *stored.RollbackProposal.IncidentID)

	_, err = f.svc.Rollback(context.Background(), r.ID, domain.ReleaseActorHuman, domain.RollbackManual, "by hand")
	require.NoError(t, err, "a person may still roll it back")
}

func TestADismissalSinceTheHandBackAnswersIt(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	handBack := now.Add(-time.Hour)
	r := domain.Release{Status: domain.ReleaseAwaitingVerdict, LastHandBackAt: &handBack, UpdatedAt: now.Add(-time.Hour)}
	require.True(t, handBackDue(r, now))

	r.RollbackDismissal = &domain.RollbackDismissal{DismissedAt: handBack.Add(time.Minute)}
	assert.False(t, handBackDue(r, now), "a person dismissing the proposal is an answer")

	r.RollbackDismissal.DismissedAt = handBack.Add(-time.Minute)
	assert.True(t, handBackDue(r, now))
}

func TestFinishingAReleaseClearsItsOwnProposal(t *testing.T) {
	f := newRollbackFixture()
	r := f.awaiting(t, false)
	f.propose(t, r)

	done, err := f.svc.Finish(context.Background(), r.ID, domain.ReleaseActorHuman, "ship it")
	require.NoError(t, err)
	assert.Nil(t, done.RollbackProposal)
	stored, err := f.store.Get(context.Background(), r.ID)
	require.NoError(t, err)
	assert.Nil(t, stored.RollbackProposal, "a released release no longer waits on its proposal")
}

func TestWithoutAReverterNoRollbackIsAllowed(t *testing.T) {
	f := newRollbackFixture()
	f.svc.reverter = nil
	r := f.awaiting(t, false)

	got, err := f.svc.Get(context.Background(), r.ID)
	require.NoError(t, err)
	assert.False(t, got.RollbackAllowed)
	assert.Equal(t, domain.RollbackRefusalNotConfigured, got.RollbackRefusal)

	_, err = f.svc.Rollback(context.Background(), r.ID, domain.ReleaseActorAgent, domain.RollbackHealthIncident, "errors")
	require.ErrorIs(t, err, domain.ErrReleaseWrongStatus)
	stored, err := f.store.Get(context.Background(), r.ID)
	require.NoError(t, err)
	assert.Nil(t, stored.RollbackProposal, "nothing is proposed that could not run")
}
