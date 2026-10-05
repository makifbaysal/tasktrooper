package opencode

import "strings"

// nativeToolNames maps opencode's tool names onto the ledger's canonical ones.
var nativeToolNames = map[string]string{
	"read":      "read_file",
	"write":     "write_file",
	"edit":      "edit_file",
	"grep":      "grep_code",
	"glob":      "get_repo_tree",
	"bash":      "run_terminal",
	"webfetch":  "fetch_url",
	"websearch": "web_search",
	"task":      "subagent",
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