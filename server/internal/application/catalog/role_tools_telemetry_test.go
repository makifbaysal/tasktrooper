package catalog

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestOnlyTheReleaseEngineerChangesTelemetry(t *testing.T) {
	catalog := repoCatalogAgents(t)
	for slug, agent := range catalog {
		for _, tool := range roleTelemetryWriteTools {
			if slug == "release-engineer" {
				assert.Containsf(t, agent.ToolPolicy.AllowTools, tool, "%s", slug)
			} else {
				assert.NotContainsf(t, agent.ToolPolicy.AllowTools, tool, "%s may not change telemetry", slug)
			}
		}
	}
}

func TestTelemetryReadsAreScopedPerRole(t *testing.T) {
	catalog := repoCatalogAgents(t)
	for _, slug := range []string{"backend-developer", "frontend-developer", "mobile-developer", "data-scientist", "game-developer", "qa-agent"} {
		for _, tool := range roleTelemetryDiagnoseTools {
			assert.Containsf(t, catalog[slug].ToolPolicy.AllowTools, tool, "%s", slug)
		}
		assert.NotContainsf(t, catalog[slug].ToolPolicy.AllowTools, "telemetry_api_get", "%s gets the narrow tools, not the raw API", slug)
		assert.NotContainsf(t, catalog[slug].ToolPolicy.AllowTools, "telemetry_get_incident", "%s: incidents are the release engineer's", slug)
	}
	sec := catalog["security-agent"].ToolPolicy.AllowTools
	assert.Subset(t, sec, []string{"telemetry_search_logs", "telemetry_search_traces", "telemetry_list_errors"})
	assert.NotContains(t, sec, "telemetry_get_service")

	assert.Contains(t, catalog["system-architect"].ToolPolicy.AllowTools, "telemetry_get_service")
	assert.NotContains(t, catalog["system-architect"].ToolPolicy.AllowTools, "telemetry_search_logs")

	pm := catalog["product-manager"].ToolPolicy.AllowTools
	assert.Subset(t, pm, []string{"telemetry_get_service", "telemetry_list_slos"})
	assert.NotContains(t, pm, "telemetry_search_logs")

	assert.NotContains(t, catalog["ui-designer"].ToolPolicy.AllowTools, "telemetry_get_service")
}
