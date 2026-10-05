package antigravity

import "strings"

// nativeToolNames maps agy's tool names onto the ledger's canonical ones.
var nativeToolNames = map[string]string{
	"read_file":                  "read_file",
	"view_file":                  "read_file",
	"code_search":                "codebase_search",
	"grep_search":                "grep_code",
	"list_directory":             "get_repo_tree",
	"glob":                       "get_repo_tree",
	"write_file":                 "write_file",
	"write_to_file":              "write_file",
	"replace_file_content":       "edit_file",
	"multi_replace_file_content": "edit_file",
	"run_command":                "run_terminal",
	"run_shell_command":          "run_terminal",
	"read_url":                   "fetch_url",
	"google_web_search":          "web_search",
	"invoke_subagent":            "subagent",
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