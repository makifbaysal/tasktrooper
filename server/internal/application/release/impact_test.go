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

// fakeImpact answers ForRelease with what the test scripted and counts the
// reads, so the soak's pacing is visible.
type fakeImpact struct {
	impact     domain.DeployImpact
	err        error
	reads      int
	thresholds domain.DeployImpactConfig
	deadline   time.Duration
}

func (f *fakeImpact) Thresholds() domain.DeployImpactConfig { return f.thresholds }

func (f *fakeImpact) ForRelease(ctx context.Context, r domain.Release) (domain.DeployImpact, error) {
	f.reads++
	if d, ok := ctx.Deadline(); ok {
		f.deadline = time.Until(d)
	}
	out := f.impact
	out.ReleaseID = r.ID
	return out, f.err
}

func f64(v float64) *float64 { return &v }

func errorRateImpact(before, after float64) domain.SignalImpact {
	delta := after - before
	return domain.SignalImpact{Signal: domain.SignalHTTPErrorRatio, Unit: "%", Before: &before, After: &after, AfterPoints: 30, Delta: &delta, Verdict: domain.ImpactWorse}
}

func latencyImpact(before, after float64) domain.SignalImpact {
	delta := after - before
	pct := delta / before * 100
	return domain.SignalImpact{Signal: domain.SignalHTTPLatency, Unit: "ms", Before: &before, After: &after, AfterPoints: 30, Delta: &delta, DeltaPct: &pct, Verdict: domain.ImpactWorse}
}

func (f *sweepFixture) verifyingWithThresholds(t *testing.T, maxErrorRateDelta, maxP95DeltaPct float64) uuid.UUID {
	t.Helper()
	repositoryID := uuid.New()
	task := f.withTask(repositoryID)
	r := verifyingRelease(repositoryID, f.clock.Now().Add(-15*time.Minute), f.clock.Now().Add(7*time.Minute))
	r.Profile.Verify.MaxErrorRateDelta = maxErrorRateDelta
	r.Profile.Verify.MaxP95DeltaPct = maxP95DeltaPct
	created, err := f.store.Create(context.Background(), r, []uuid.UUID{task.ID})
	require.NoError(t, err)
	return created.ID
}

func TestSoakImpactBreachesStopTheSoakEarly(t *testing.T) {
	tests := []struct {
		name      string
		maxErrors float64
		maxP95    float64
		signal    domain.SignalImpact
		want      string
	}{
		{"error rate over max_error_rate_delta", 1, 0, errorRateImpact(0.4, 2.5), "max_error_rate_delta 1"},
		{"p95 over max_p95_delta_pct", 0, 20, latencyImpact(200, 300), "max_p95_delta_pct 20"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newSweepFixture()
			impact := &fakeImpact{impact: domain.DeployImpact{Pending: true, Verdict: domain.ImpactWorse, Signals: []domain.SignalImpact{tt.signal}}}
			f.svc.SetImpact(impact)
			id := f.verifyingWithThresholds(t, tt.maxErrors, tt.maxP95)

			f.svc.SweepOnce(context.Background())

			got, err := f.store.Get(context.Background(), id)
			require.NoError(t, err)
			assert.Equal(t, domain.ReleaseAwaitingVerdict, got.Status)
			assert.Contains(t, got.Checks.EarlyStop, tt.want)
			assert.True(t, got.Checks.Failing(got.Profile.Verify.MaxNewErrors))
			require.NotNil(t, got.Checks.Impact)
			assert.True(t, got.Checks.Impact.Pending)
			assert.Equal(t, domain.ImpactWorse, got.Checks.Impact.Verdict)
			require.Len(t, got.Checks.Impact.Signals, 1)
			assert.True(t, got.Checks.Impact.Signals[0].Breached)
			assert.Empty(t, got.Checks.Impact.Unchecked)
			assert.Len(t, f.waker.calls, 1, "a breach hands the release back for a verdict like any failed check")
		})
	}
}

func TestSoakImpactInsideTheThresholdsKeepsVerifyingAndRecordsTheFacts(t *testing.T) {
	f := newSweepFixture()
	f.svc.SetImpact(&fakeImpact{impact: domain.DeployImpact{
		Verdict: domain.ImpactNeutral,
		Signals: []domain.SignalImpact{errorRateImpact(0.4, 0.9), latencyImpact(200, 210)},
	}})
	id := f.verifyingWithThresholds(t, 1, 20)

	f.svc.SweepOnce(context.Background())

	got, err := f.store.Get(context.Background(), id)
	require.NoError(t, err)
	assert.Equal(t, domain.ReleaseVerifying, got.Status)
	assert.Empty(t, got.Checks.EarlyStop)
	require.NotNil(t, got.Checks.Impact)
	require.Len(t, got.Checks.Impact.Signals, 2)
	errs, lat := got.Checks.Impact.Signals[0], got.Checks.Impact.Signals[1]
	assert.InDelta(t, 0.4, *errs.Before, 1e-9)
	assert.InDelta(t, 0.9, *errs.After, 1e-9)
	assert.Equal(t, f64(1), errs.MaxDelta)
	assert.False(t, errs.Breached)
	assert.Equal(t, f64(20), lat.MaxDeltaPct)
	assert.False(t, lat.Breached)
	assert.Empty(t, f.waker.calls)
}

func TestSoakImpactWithAThresholdWhoseSignalIsUnboundListsItUnchecked(t *testing.T) {
	f := newSweepFixture()
	f.svc.SetImpact(&fakeImpact{impact: domain.DeployImpact{Verdict: domain.ImpactUnknown, Signals: []domain.SignalImpact{}}})
	id := f.verifyingWithThresholds(t, 1, 20)

	f.svc.SweepOnce(context.Background())

	got, err := f.store.Get(context.Background(), id)
	require.NoError(t, err)
	assert.Equal(t, domain.ReleaseVerifying, got.Status)
	require.NotNil(t, got.Checks.Impact)
	assert.Equal(t, []string{domain.VerifyMaxErrorRateDelta, domain.VerifyMaxP95DeltaPct}, got.Checks.Impact.Unchecked)
}

func TestSoakWithoutImpactThresholdsNeverReadsImpact(t *testing.T) {
	f := newSweepFixture()
	impact := &fakeImpact{impact: domain.DeployImpact{Verdict: domain.ImpactWorse, Signals: []domain.SignalImpact{errorRateImpact(0.4, 9)}}}
	f.svc.SetImpact(impact)
	id := f.verifyingWithThresholds(t, 0, 0)

	f.svc.SweepOnce(context.Background())

	got, err := f.store.Get(context.Background(), id)
	require.NoError(t, err)
	assert.Zero(t, impact.reads)
	assert.Nil(t, got.Checks.Impact)
	assert.Equal(t, domain.ReleaseVerifying, got.Status)
}

func TestSoakImpactIsReadAtMostEveryTwoMinutesAndAgainAtTheWindowEnd(t *testing.T) {
	f := newSweepFixture()
	impact := &fakeImpact{impact: domain.DeployImpact{Verdict: domain.ImpactNeutral, Signals: []domain.SignalImpact{errorRateImpact(0.4, 0.5)}}}
	f.svc.SetImpact(impact)
	f.verifyingWithThresholds(t, 1, 0)

	f.svc.SweepOnce(context.Background())
	f.clock.Advance(30 * time.Second)
	f.svc.SweepOnce(context.Background())
	assert.Equal(t, 1, impact.reads)

	f.clock.Advance(2 * time.Minute)
	f.svc.SweepOnce(context.Background())
	assert.Equal(t, 2, impact.reads)

	f.clock.Advance(10 * time.Second)
	f.svc.SweepOnce(context.Background())
	assert.Equal(t, 2, impact.reads)

	f.clock.Advance(5 * time.Minute)
	f.svc.SweepOnce(context.Background())
	assert.Equal(t, 3, impact.reads, "the window-end sweep reads impact one last time")
}

func TestSoakImpactThatCannotBeReadLeavesTheSoakToItsOtherChecks(t *testing.T) {
	f := newSweepFixture()
	f.svc.SetImpact(&fakeImpact{err: errors.New("source down")})
	id := f.verifyingWithThresholds(t, 1, 0)

	f.svc.SweepOnce(context.Background())

	got, err := f.store.Get(context.Background(), id)
	require.NoError(t, err)
	assert.Equal(t, domain.ReleaseVerifying, got.Status)
	assert.Nil(t, got.Checks.Impact)
	assert.Empty(t, got.Checks.EarlyStop)
}

func TestSoakImpactDoesNotReplaceAnEarlierEarlyStop(t *testing.T) {
	f := newSweepFixture()
	f.svc.SetImpact(&fakeImpact{impact: domain.DeployImpact{Verdict: domain.ImpactWorse, Signals: []domain.SignalImpact{errorRateImpact(0.4, 9)}}})
	repositoryID := uuid.New()
	task := f.withTask(repositoryID)
	envID := uuid.New()
	deployedAt := f.clock.Now().Add(-15 * time.Minute)
	r := verifyingRelease(repositoryID, deployedAt, f.clock.Now().Add(10*time.Minute))
	r.Checks.EnvironmentID = &envID
	r.Profile.Verify.MaxErrorRateDelta = 1
	created, err := f.store.Create(context.Background(), r, []uuid.UUID{task.ID})
	require.NoError(t, err)
	f.envs.errorGroups[envID] = []domain.RuntimeErrorGroup{{Fingerprint: "a", FirstSeen: deployedAt.Add(time.Minute)}}

	f.svc.SweepOnce(context.Background())

	got, err := f.store.Get(context.Background(), created.ID)
	require.NoError(t, err)
	assert.Contains(t, got.Checks.EarlyStop, "new runtime error groups")
	require.NotNil(t, got.Checks.Impact)
	assert.True(t, got.Checks.Impact.Signals[0].Breached, "the breach is still on the record")
}

func TestImpactWithoutDeployImpactWiredIsUnknown(t *testing.T) {
	f := newSweepFixture()
	deployedAt := f.clock.Now().Add(-2 * time.Hour)
	created, err := f.store.Create(context.Background(), domain.Release{RepositoryID: uuid.New(), Status: domain.ReleaseReleased, DeployedAt: &deployedAt}, nil)
	require.NoError(t, err)

	impact, err := f.svc.Impact(context.Background(), created.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.ImpactUnknown, impact.Verdict)
	assert.Empty(t, impact.Signals)
	assert.False(t, impact.Pending)

	_, err = f.svc.Impact(context.Background(), uuid.New())
	assert.ErrorIs(t, err, domain.ErrReleaseNotFound)
}

func TestImpactReadsTheReleaseItWasAskedFor(t *testing.T) {
	f := newSweepFixture()
	impact := &fakeImpact{impact: domain.DeployImpact{Verdict: domain.ImpactImproved}}
	f.svc.SetImpact(impact)
	created, err := f.store.Create(context.Background(), domain.Release{RepositoryID: uuid.New(), Status: domain.ReleaseReleased}, nil)
	require.NoError(t, err)

	got, err := f.svc.Impact(context.Background(), created.ID)
	require.NoError(t, err)
	assert.Equal(t, created.ID, got.ReleaseID)
	assert.Equal(t, domain.ImpactImproved, got.Verdict)
}

func (f *sweepFixture) soakImpact(t *testing.T, impact *fakeImpact, deployedAgo time.Duration) domain.Release {
	t.Helper()
	f.svc.SetImpact(impact)
	repositoryID := uuid.New()
	task := f.withTask(repositoryID)
	r := verifyingRelease(repositoryID, f.clock.Now().Add(-deployedAgo), f.clock.Now().Add(30*time.Minute))
	r.Profile.Verify.MaxErrorRateDelta = 1
	r.Profile.Verify.MaxP95DeltaPct = 20
	created, err := f.store.Create(context.Background(), r, []uuid.UUID{task.ID})
	require.NoError(t, err)
	f.svc.SweepOnce(context.Background())
	got, err := f.store.Get(context.Background(), created.ID)
	require.NoError(t, err)
	return got
}

func TestSoakImpactDoesNotBreachOnAShortOrSparseAfterWindow(t *testing.T) {
	sparse := errorRateImpact(0.4, 2.5)
	sparse.AfterPoints = 4
	tests := []struct {
		name        string
		signal      domain.SignalImpact
		deployedAgo time.Duration
	}{
		{"three minutes after the deploy", errorRateImpact(0.4, 2.5), 3 * time.Minute},
		{"four points after it", sparse, 15 * time.Minute},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newSweepFixture()
			got := f.soakImpact(t, &fakeImpact{impact: domain.DeployImpact{Signals: []domain.SignalImpact{tt.signal}}}, tt.deployedAgo)

			assert.Equal(t, domain.ReleaseVerifying, got.Status)
			assert.Empty(t, got.Checks.EarlyStop)
			require.Len(t, got.Checks.Impact.Signals, 1)
			assert.True(t, got.Checks.Impact.Signals[0].Settling)
			assert.False(t, got.Checks.Impact.Signals[0].Breached)
		})
	}
}

func TestSoakImpactYesterdaysSameHourAlsoShowsDoesNotBreach(t *testing.T) {
	errs := errorRateImpact(0.4, 2.5)
	errs.Baseline = f64(2.2)
	f := newSweepFixture()
	got := f.soakImpact(t, &fakeImpact{impact: domain.DeployImpact{Signals: []domain.SignalImpact{errs}}}, 15*time.Minute)

	assert.Equal(t, domain.ReleaseVerifying, got.Status)
	assert.False(t, got.Checks.Impact.Signals[0].Breached)
	assert.False(t, got.Checks.Impact.Signals[0].Settling)

	errs.Baseline = f64(0.5)
	f = newSweepFixture()
	got = f.soakImpact(t, &fakeImpact{impact: domain.DeployImpact{Signals: []domain.SignalImpact{errs}}}, 15*time.Minute)
	assert.Equal(t, domain.ReleaseAwaitingVerdict, got.Status, "past the limit from yesterday's hour too")
}

func TestSoakLatencyBreachNeedsTheLatencyFloor(t *testing.T) {
	f := newSweepFixture()
	got := f.soakImpact(t, &fakeImpact{impact: domain.DeployImpact{Signals: []domain.SignalImpact{latencyImpact(100, 130)}}}, 15*time.Minute)
	assert.Equal(t, domain.ReleaseVerifying, got.Status, "30% but only 30 ms")
	assert.False(t, got.Checks.Impact.Signals[0].Breached)

	f = newSweepFixture()
	got = f.soakImpact(t, &fakeImpact{
		impact:     domain.DeployImpact{Signals: []domain.SignalImpact{latencyImpact(100, 130)}},
		thresholds: domain.DeployImpactConfig{LatencyFloorMS: 20},
	}, 15*time.Minute)
	assert.Equal(t, domain.ReleaseAwaitingVerdict, got.Status, "the floor is deploy impact's own")
}

func TestSoakImpactIsReadUnderAShortTimeout(t *testing.T) {
	f := newSweepFixture()
	impact := &fakeImpact{impact: domain.DeployImpact{Signals: []domain.SignalImpact{errorRateImpact(0.4, 0.5)}}}
	f.soakImpact(t, impact, 15*time.Minute)

	require.Equal(t, 1, impact.reads)
	assert.Greater(t, impact.deadline, time.Duration(0))
	assert.LessOrEqual(t, impact.deadline, impactReadTimeout)
}

func TestSoakPacingIsForgottenWhenAReleaseLeavesVerifyingByAnyPath(t *testing.T) {
	f := newSweepFixture()
	got := f.soakImpact(t, &fakeImpact{impact: domain.DeployImpact{Signals: []domain.SignalImpact{errorRateImpact(0.4, 0.5)}}}, 15*time.Minute)
	f.svc.mu.Lock()
	_, paced := f.svc.lastImpactCheck[pacingKey{release: got.ID}]
	f.svc.mu.Unlock()
	require.True(t, paced)

	got.Status = domain.ReleaseSuperseded
	_, err := f.store.Update(context.Background(), got, domain.ReleaseVerifying)
	require.NoError(t, err)
	f.svc.SweepOnce(context.Background())

	f.svc.mu.Lock()
	defer f.svc.mu.Unlock()
	assert.NotContains(t, f.svc.lastImpactCheck, pacingKey{release: got.ID})
	assert.NotContains(t, f.svc.lastErrorCheck, pacingKey{release: got.ID})
}
