package catalog

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestCatalogRoleToolPolicies(t *testing.T) {
	catalog := repoCatalogAgents(t)

	byName := map[string]string{
		"backend-developer":  "developerToolPolicy",
		"frontend-developer": "developerToolPolicy",
		"mobile-developer":   "mobileDeveloperToolPolicy",
		"product-manager":    "productManagerToolPolicy",
		"qa-agent":           "qaToolPolicy",
		"system-architect":   "architectToolPolicy",
		"release-engineer":   "releaseEngineerToolPolicy",
	}
	expected := map[string]domain.ToolPolicy{
		"backend-developer":  developerToolPolicy(),
		"frontend-developer": developerToolPolicy(),
		"mobile-developer":   mobileDeveloperToolPolicy(),
		"product-manager":    productManagerToolPolicy(),
		"qa-agent":           qaToolPolicy(),
		"system-architect":   architectToolPolicy(),
		"release-engineer":   releaseEngineerToolPolicy(),
	}
	for slug, want := range expected {
		agent, ok := catalog[slug]
		require.Truef(t, ok, "catalog is missing agent %q", slug)
		assert.Truef(t, toolPolicyEqual(agent.ToolPolicy, want),
			"%s tool policy drifted from %s", slug, byName[slug])
	}

	dev := expected["backend-developer"]
	mob := expected["mobile-developer"]
	assert.NotContains(t, dev.AllowTools, "mobile_tap")
	assert.Contains(t, mob.AllowTools, "mobile_tap")

	qa := expected["qa-agent"]
	assert.Contains(t, qa.AllowTools, "mobile_launch_app")
	assert.Contains(t, qa.AllowTools, "mobile_screenshot")
	assert.Contains(t, expected["product-manager"].AllowTools, "mobile_screenshot")
}

func TestToolPolicyEqual(t *testing.T) {
	a := productManagerToolPolicy()
	b := productManagerToolPolicy()
	assert.True(t, toolPolicyEqual(a, b))
	b.AllowTools = append(b.AllowTools, "run_terminal")
	assert.False(t, toolPolicyEqual(a, b))
}
