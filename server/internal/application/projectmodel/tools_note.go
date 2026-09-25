package projectmodel

import (
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// toolNoteEntries are the project-model tools ToolsNote tells an agent about,
// in the order they render; wording lives here rather than in the tool
// registry because application must not import adapter/tools.
var toolNoteEntries = []struct {
	name string
	desc string
}{
	{"get_project_brief", "repository overview: stack, components and their build/test/lint commands, git conventions (default branch, branch naming, merge style), reference docs, where each component runs"},
	{"list_component_checks", "what CI runs, with the local command for each check; before you hand off, run the local commands of every required check for the components you changed"},
	{"list_links", "what a component talks to and what calls it (other repositories, databases, queues, external APIs)"},
	{"get_environment", "a component's environments: URLs, health"},
	{"query_runtime_logs", "live logs of a deployed environment"},
	{"list_runtime_errors", "recent errors of a deployed environment"},
}

// ToolsNote replaces injecting Brief's markdown into an agent's context: it
// only names the project-model tools the policy allows, so the agent fetches
// facts on demand instead of holding a copy that goes stale the moment the
// repository changes. It returns "" when the policy allows none of them.
func ToolsNote(policy domain.ToolPolicy) string {
	var b strings.Builder
	for _, entry := range toolNoteEntries {
		if !domain.ToolAllowedByPolicy(entry.name, policy) {
			continue
		}
		if b.Len() == 0 {
			b.WriteString("## Project model (TaskTrooper)\n")
			b.WriteString("TaskTrooper keeps a scanned model of this repository. It is not in your context: fetch what you need with these tools instead of guessing or reading it off the code:\n")
		}
		b.WriteString("- `" + entry.name + "`: " + entry.desc + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
