package domain_test

import (
	"strings"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateSmokeChecks(t *testing.T) {
	tests := []struct {
		name    string
		checks  []domain.SmokeCheck
		wantErr bool
	}{
		{
			name:   "valid GET with name and latency budget",
			checks: []domain.SmokeCheck{{Method: "GET", Path: "/health", Name: "health check", MaxLatencyMS: 500}},
		},
		{
			name:   "valid HEAD with no limits",
			checks: []domain.SmokeCheck{{Method: "HEAD", Path: "/"}},
		},
		{
			name:   "valid absolute URL",
			checks: []domain.SmokeCheck{{Method: "GET", Path: "https://example.com/health"}},
		},
		{
			name:    "name too long",
			checks:  []domain.SmokeCheck{{Method: "GET", Path: "/health", Name: strings.Repeat("a", 81)}},
			wantErr: true,
		},
		{
			name:   "name at exactly the limit",
			checks: []domain.SmokeCheck{{Method: "GET", Path: "/health", Name: strings.Repeat("a", 80)}},
		},
		{
			name:    "latency zero means no limit and is fine",
			checks:  []domain.SmokeCheck{{Method: "GET", Path: "/health", MaxLatencyMS: 0}},
			wantErr: false,
		},
		{
			name:    "latency negative is invalid",
			checks:  []domain.SmokeCheck{{Method: "GET", Path: "/health", MaxLatencyMS: -1}},
			wantErr: true,
		},
		{
			name:    "latency above the probe timeout is invalid",
			checks:  []domain.SmokeCheck{{Method: "GET", Path: "/health", MaxLatencyMS: 10001}},
			wantErr: true,
		},
		{
			name:   "latency at exactly the probe timeout",
			checks: []domain.SmokeCheck{{Method: "GET", Path: "/health", MaxLatencyMS: 10000}},
		},
		{
			name:    "method other than GET/HEAD",
			checks:  []domain.SmokeCheck{{Method: "POST", Path: "/health"}},
			wantErr: true,
		},
		{
			name:    "too many checks",
			checks:  make([]domain.SmokeCheck, domain.MaxSmokeChecks+1),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.wantErr {
				assert.ErrorIs(t, domain.ValidateSmokeChecks(tt.checks), domain.ErrInvalidDelivery)
			} else {
				require.NoError(t, domain.ValidateSmokeChecks(tt.checks))
			}
		})
	}
}

func TestSmokeCheckNormalizedTrimsName(t *testing.T) {
	c := domain.SmokeCheck{Method: "get", Path: " /health ", Name: "  health  "}.Normalized()
	assert.Equal(t, "GET", c.Method)
	assert.Equal(t, "/health", c.Path)
	assert.Equal(t, "health", c.Name)
}

func TestComponentDeliveryValidateSharesSmokeCheckRules(t *testing.T) {
	d := domain.ComponentDelivery{
		Mode:     domain.DeliveryOnMerge,
		Executor: domain.ExecutorVercel,
		Verify: domain.DeliveryVerify{
			Smoke: []domain.SmokeCheck{{Method: "GET", Path: "/health", Name: strings.Repeat("a", 81)}},
		},
	}
	assert.ErrorIs(t, d.Validate(), domain.ErrInvalidDelivery)
}
