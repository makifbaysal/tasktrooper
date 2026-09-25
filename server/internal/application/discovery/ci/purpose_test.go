package ci

import (
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestNormalizeEnvironment(t *testing.T) {
	cases := []struct {
		raw  string
		want domain.DeployEnvironment
	}{
		{"production", domain.EnvironmentProduction},
		{"prod", domain.EnvironmentProduction},
		{"live", domain.EnvironmentProduction},
		{"staging", domain.EnvironmentStaging},
		{"stage", domain.EnvironmentStaging},
		{"preprod", domain.EnvironmentStaging},
		{"pre-prod", domain.EnvironmentStaging},
		{"pre_prod", domain.EnvironmentStaging},
		{"uat", domain.EnvironmentStaging},
		{"UAT", domain.EnvironmentStaging},
		{"backend-preprod", domain.EnvironmentStaging},
		{"preview", domain.EnvironmentPreview},
		{"pr", domain.EnvironmentPreview},
		{"dev", domain.EnvironmentDevelopment},
		{"development", domain.EnvironmentDevelopment},
		{"production-eu", domain.EnvironmentProduction},
		{"", domain.DeployEnvironment("")},
		{"${{ github.event.inputs.env }}", domain.DeployEnvironment("")},
	}
	for _, c := range cases {
		if got := normalizeEnvironment(c.raw); got != c.want {
			t.Errorf("normalizeEnvironment(%q) = %q, want %q", c.raw, got, c.want)
		}
	}
}

func TestEnvFromKeywords(t *testing.T) {
	cases := []struct {
		keyName string
		want    domain.DeployEnvironment
	}{
		{"deploy production", domain.EnvironmentProduction},
		{"deploy-prod", domain.EnvironmentProduction},
		{"deploy preprod", domain.EnvironmentStaging},
		{"deploy pre-prod", domain.EnvironmentStaging},
		{"deploy uat", domain.EnvironmentStaging},
		{"deploy staging", domain.EnvironmentStaging},
		{"deploy preview", domain.EnvironmentPreview},
		{"deploy dev", domain.EnvironmentDevelopment},
		{"deploy", domain.DeployEnvironment("")},
	}
	for _, c := range cases {
		if got := envFromKeywords(c.keyName); got != c.want {
			t.Errorf("envFromKeywords(%q) = %q, want %q", c.keyName, got, c.want)
		}
	}
}

// TestDeployEnvironment_PreprodIsNotProduction guards the bug directly: a job
// whose own `environment:` field is "preprod" must classify as staging even
// though the string contains "prod", and a push-to-main workflow with no
// environment info still falls back to production.
func TestDeployEnvironment_PreprodIsNotProduction(t *testing.T) {
	j := job{Key: "deploy", Environment: "preprod"}
	wf := workflow{Triggers: []string{"push:main"}}
	if got := deployEnvironment(wf, j); got != domain.EnvironmentStaging {
		t.Errorf("deployEnvironment with environment=preprod = %q, want staging", got)
	}

	j2 := job{Key: "deploy-preprod"}
	if got := deployEnvironment(wf, j2); got != domain.EnvironmentStaging {
		t.Errorf("deployEnvironment with key=deploy-preprod = %q, want staging", got)
	}

	j3 := job{Key: "deploy"}
	if got := deployEnvironment(wf, j3); got != domain.EnvironmentProduction {
		t.Errorf("deployEnvironment push-to-main fallback = %q, want production", got)
	}

	j4 := job{Key: "deploy", Environment: "production"}
	if got := deployEnvironment(wf, j4); got != domain.EnvironmentProduction {
		t.Errorf("deployEnvironment with environment=production = %q, want production", got)
	}
}
