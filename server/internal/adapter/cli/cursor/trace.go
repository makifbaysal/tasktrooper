package cursor

import "strings"

// nativeToolNames maps the CLI's tool names onto the ledger's canonical ones.
// Names cursor-agent happens to share with TaskTrooper's own tools map to
// themselves.
var nativeToolNames = map[string]string{
	"read":  "read_file",
	"write": "write_file",
	"edit":  "edit_file",
	// cursor-agent reports its terminal as shellToolCall; unmapped, every
	// command it ran was invisible to the hand-off's "ran a command" check.
	"shell": "run_terminal",
	"grep":  "grep_code",
	"ls":    "get_repo_tree",
	"glob":  "get_repo_tree",
	"task":  "subagent",
}

const ownToolMarker = "tasktrooper"

// recordedElsewhere filters TaskTrooper's own MCP tools back out of the
// ledger: their calls are recorded by the MCP server, and counting the CLI's
// reflect of them again would double-count.
func recordedElsewhere(nativeName string) bool {
	return strings.Contains(strings.ToLower(nativeName), ownToolMarker)
}

func ledgerToolName(native string) string {
	if mapped, ok := nativeToolNames[native]; ok {
		return mapped
	}
	return native
}