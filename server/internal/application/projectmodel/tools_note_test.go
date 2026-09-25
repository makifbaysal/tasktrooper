package projectmodel

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestToolsNoteListsOnlyAllowedToolsInFixedOrder(t *testing.T) {
	policy := domain.ToolPolicy{AllowTools: []string{"list_runtime_errors", "get_environment", "run_terminal"}}

	note := ToolsNote(policy)

	assert.Contains(t, note, "## Project model (TaskTrooper)")
	assert.Contains(t, note, "- `get_environment`: a component's environments: URLs, health")
	assert.Contains(t, note, "- `list_runtime_errors`: recent errors of a deployed environment")
	assert.NotContains(t, note, "get_project_brief")
	assert.NotContains(t, note, "list_component_checks")
	assert.NotContains(t, note, "list_links")
	assert.NotContains(t, note, "query_runtime_logs")

	envIdx := strings.Index(note, "get_environment")
	errIdx := strings.Index(note, "list_runtime_errors")
	assert.Less(t, envIdx, errIdx, "entries must render in the catalogue's own order, not the policy's")
}

func TestToolsNoteIsEmptyWhenThePolicyAllowsNoneOfTheProjectModelTools(t *testing.T) {
	policy := domain.ToolPolicy{AllowTools: []string{"run_terminal", "write_file"}}
	assert.Empty(t, ToolsNote(policy))
}

func TestToolsNoteListsEverythingForAnEmptyPolicy(t *testing.T) {
	note := ToolsNote(domain.ToolPolicy{})
	for _, name := range []string{
		"get_project_brief", "list_component_checks", "list_links",
		"get_environment", "query_runtime_logs", "list_runtime_errors",
	} {
		assert.Contains(t, note, name)
	}
}
