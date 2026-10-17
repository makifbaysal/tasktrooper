package prodops_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prodops"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// fakeReleaseAttributor returns a fixed domain.ReleaseAttribution, letting a
// test control its AutoRollback bit independently of any legacy deploy
// target.
type fakeReleaseAttributor struct {
	attribution domain.ReleaseAttribution
	ok          bool
	window      time.Duration
}

func (f *fakeReleaseAttributor) AttributeRelease(context.Context, uuid.UUID, string, time.Time) (domain.ReleaseAttribution, bool) {
	return f.attribution, f.ok
}

func (f *fakeReleaseAttributor) HealthWindow() time.Duration { return f.window }

type fakeReleaseRollbackDispatcher struct {
	calls []bool
}

func (f *fakeReleaseRollbackDispatcher) DispatchReleaseRollback(_ context.Context, _ domain.ReleaseAttribution, _ domain.Incident, autoRollback bool) error {
	f.calls = append(f.calls, autoRollback)
	return nil
}

// fakeAutoRollbackTargets is a port.DeployTargetStore whose AutoRollback is
// the OPPOSITE of the release's own profile in the tests below — if
// attributeAndMaybeRollBack still reads from here, the dispatched bit would
// come out flipped from what the attribution says.
type fakeAutoRollbackTargets struct {
	autoRollback bool
}

func (f *fakeAutoRollbackTargets) ListByRepository(context.Context, uuid.UUID) ([]domain.DeployTarget, error) {
	return nil, nil
}
func (f *fakeAutoRollbackTargets) ListAll(context.Context) ([]domain.DeployTarget, error) {
	return nil, nil
}
func (f *fakeAutoRollbackTargets) Get(context.Context, uuid.UUID, string, string) (domain.DeployTarget, error) {
	return domain.DeployTarget{AutoRollback: f.autoRollback}, nil
}
func (f *fakeAutoRollbackTargets) Save(_ context.Context, t domain.DeployTarget) (domain.DeployTarget, error) {
	return t, nil
}
func (f *fakeAutoRollbackTargets) Delete(context.Context, uuid.UUID, string, string) error {
	return nil
}

// TestIngestReadsAutoRollbackFromTheAttributedReleaseNotTheLegacyTarget guards
// a repository's legacy deploy target and the release that is actually
// live can disagree (the target is a leftover from before delivery profiles
// existed, or was edited after this release shipped). The dispatched
// auto_rollback bit must follow the release's own frozen profile
// (ReleaseAttribution.AutoRollback), not port.DeployTargetStore.
func TestIngestReadsAutoRollbackFromTheAttributedReleaseNotTheLegacyTarget(t *testing.T) {
	for _, releaseAutoRollback := range []bool{true, false} {
		repoID := uuid.New()
		taskID := uuid.New()

		incidents := newFakeIncidents()
		// The legacy target is deliberately the OPPOSITE of the release's
		// profile, so a test failure here means the fix regressed to reading
		// the target again.
		targets := &fakeAutoRollbackTargets{autoRollback: !releaseAutoRollback}
		attributor := &fakeReleaseAttributor{
			ok:     true,
			window: 15 * time.Minute,
			attribution: domain.ReleaseAttribution{
				TaskID:       taskID,
				TaskKey:      "T-1",
				Title:        "release under test",
				MergeSHA:     "deadbeef",
				Env:          domain.DeployEnvProd,
				DeployedAt:   time.Now().Add(-time.Minute),
				AutoRollback: releaseAutoRollback,
			},
		}
		rollbacks := &fakeReleaseRollbackDispatcher{}

		svc := prodops.NewService(prodops.Deps{Incidents: incidents, Targets: targets})
		svc.SetReleaseAttributor(attributor)
		svc.SetReleaseRollbackDispatcher(rollbacks)

		_, err := svc.Ingest(context.Background(), domain.IncidentInput{
			RepositoryID: repoID,
			Env:          domain.DeployEnvProd,
			Source:       domain.IncidentSourceWebhook,
			Fingerprint:  "fp-attribution-autorollback",
			Title:        "prod incident",
			Severity:     domain.IncidentSeverityLow,
		})
		require.NoError(t, err)

		require.Len(t, rollbacks.calls, 1)
		require.Equal(t, releaseAutoRollback, rollbacks.calls[0],
			"the dispatched auto_rollback bit must come from the release's own profile")
	}
}

// incidentAttributor also reads the incident itself, the way the release
// service does.
type incidentAttributor struct {
	fakeReleaseAttributor
	seen []domain.Incident
}

func (f *incidentAttributor) AttributeIncident(_ context.Context, incident domain.Incident, _ time.Time) (domain.ReleaseAttribution, bool) {
	f.seen = append(f.seen, incident)
	return f.attribution, f.ok
}

func TestIngestHandsTheIncidentToAnAttributorThatReadsIt(t *testing.T) {
	componentID := uuid.New()
	attributor := &incidentAttributor{fakeReleaseAttributor: fakeReleaseAttributor{window: 15 * time.Minute}}
	svc := prodops.NewService(prodops.Deps{Incidents: newFakeIncidents()})
	svc.SetReleaseAttributor(attributor)

	_, err := svc.Ingest(context.Background(), domain.IncidentInput{
		RepositoryID: uuid.New(), Env: domain.DeployEnvProd, Source: domain.IncidentSourceAlertRule,
		Fingerprint: "alert:x", Title: "errors", Severity: domain.IncidentSeverityLow,
		Payload: map[string]any{domain.IncidentPayloadComponentID: componentID.String()},
	})
	require.NoError(t, err)

	require.Len(t, attributor.seen, 1)
	got, ok := attributor.seen[0].ComponentID()
	require.True(t, ok)
	require.Equal(t, componentID, got)
}
