package claudecode

import (
	"strings"
)

var nativeToolNames = map[string]string{
	"Read":      "read_file",
	"Grep":      "grep_code",
	"Glob":      "get_repo_tree",
	"LS":        "get_repo_tree",
	"Bash":      "run_terminal",
	"Edit":      "edit_file",
	"MultiEdit": "edit_file",
	"Write":     "write_file",
	"WebFetch":  "fetch_url",
	"WebSearch": "web_search",
	"Task":      "subagent",
	"Agent":     "subagent",
}

func ledgerToolName(native string) string {
	if mapped, ok := nativeToolNames[native]; ok {
		return mapped
	}
	return native
}

const ownToolPrefix = "mcp__" + mcpServerName + "__"

func recordedElsewhere(nativeName string) bool {
	return strings.HasPrefix(nativeName, ownToolPrefix)
}