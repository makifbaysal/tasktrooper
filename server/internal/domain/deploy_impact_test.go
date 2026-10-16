package domain_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestWorstImpact(t *testing.T) {
	tests := []struct {
		name     string
		verdicts []domain.ImpactVerdict
		want     domain.ImpactVerdict
	}{
		{"nothing judged", nil, domain.ImpactUnknown},
		{"only unknown", []domain.ImpactVerdict{domain.ImpactUnknown, domain.ImpactUnknown}, domain.ImpactUnknown},
		{"all improved", []domain.ImpactVerdict{domain.ImpactImproved, domain.ImpactImproved}, domain.ImpactImproved},
		{"improved and neutral", []domain.ImpactVerdict{domain.ImpactImproved, domain.ImpactNeutral}, domain.ImpactNeutral},
		{"one worse wins", []domain.ImpactVerdict{domain.ImpactImproved, domain.ImpactWorse, domain.ImpactNeutral}, domain.ImpactWorse},
		{"unknown does not mask the rest", []domain.ImpactVerdict{domain.ImpactUnknown, domain.ImpactImproved}, domain.ImpactImproved},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, domain.WorstImpact(tt.verdicts))
		})
	}
}

func TestNewDeployImpactAnchorsOnTheDeploy(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	componentID := uuid.New()
	deployed := now.Add(-20 * time.Minute)
	finished := now.Add(-2 * time.Hour)
	tests := []struct {
		name        string
		release     domain.Release
		wantAt      *time.Time
		wantPending bool
	}{
		{"deployed twenty minutes ago is pending", domain.Release{DeployedAt: &deployed, FinishedAt: &finished}, &deployed, true},
		{"finished with no deploy is anchored at the finish", domain.Release{FinishedAt: &finished}, &finished, false},
		{"neither", domain.Release{}, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.release.ID, tt.release.ComponentID = uuid.New(), &componentID
			got := domain.NewDeployImpact(tt.release, now)
			assert.Equal(t, tt.release.ID, got.ReleaseID)
			assert.Equal(t, &componentID, got.ComponentID)
			assert.Equal(t, domain.EnvironmentProduction, got.Environment)
			assert.Equal(t, 60, got.WindowMinutes)
			assert.Equal(t, domain.ImpactUnknown, got.Verdict)
			assert.Equal(t, now, got.ComputedAt)
			assert.NotNil(t, got.Signals)
			assert.Equal(t, tt.wantPending, got.Pending)
			if tt.wantAt == nil {
				assert.Nil(t, got.DeployedAt)
				return
			}
			require.NotNil(t, got.DeployedAt)
			assert.Equal(t, *tt.wantAt, *got.DeployedAt)
		})
	}
}

func TestDeliveryImpactThresholdsAreValidated(t *testing.T) {
	tests := []struct {
		name      string
		maxErrors float64
		maxP95    float64
		wantErr   string
	}{
		{"unset", 0, 0, ""},
		{"in range", 1.5, 25, ""},
		{"the whole percentage", 100, 1000, ""},
		{"negative error rate delta", -1, 0, "max_error_rate_delta"},
		{"error rate delta past 100 points", 101, 0, "max_error_rate_delta"},
		{"negative p95 delta", 0, -5, "max_p95_delta_pct"},
		{"p95 delta past the limit", 0, 1001, "max_p95_delta_pct"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := domain.ComponentDelivery{
				Mode: domain.DeliveryOnMerge, Executor: domain.ExecutorVercel,
				Verify: domain.DeliveryVerify{MaxErrorRateDelta: tt.maxErrors, MaxP95DeltaPct: tt.maxP95},
			}.Normalized()
			err := d.Validate()
			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			assert.ErrorIs(t, err, domain.ErrInvalidDelivery)
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestDeliveryImpactThresholdsSwitchTheImpactCheckOn(t *testing.T) {
	assert.False(t, domain.DeliveryVerify{}.ImpactThresholds())
	assert.True(t, domain.DeliveryVerify{MaxErrorRateDelta: 1}.ImpactThresholds())
	assert.True(t, domain.DeliveryVerify{MaxP95DeltaPct: 10}.ImpactThresholds())
}

func TestNotificationLinksAreConsoleRoutes(t *testing.T) {
	id := uuid.New()
	assert.Equal(t, "/operations/issues?id="+id.String(), domain.IncidentLink(id))
	assert.Equal(t, "/operations/changes?release="+id.String(), domain.ReleaseLink(id))
}
