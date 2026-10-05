package domain

import "sort"

// The claudecode adapter's package comment states the gap this file closes: a
// claude_code agent's ToolPolicy governed only the MCP half of its tool
// surface, because the CLI's own Bash/Read/Write/Edit/Grep/Glob belong to the
// CLI and have no notion of TaskTrooper's allow patterns. The CLI does expose
// the mechanism — `--tools` selects from the built-in set — so the policy can
// now reach both halves instead of looking enforced on one.
//
// Two things follow from turning it on, and the second is why this exists at
// all beyond correctness. A narrower built-in set is fewer tool schemas in
// every request of the session, and — the larger effect — it is a session that
// cannot spend twenty turns in a capability the task never needed. A QA agent
// with WebSearch will eventually search the web.
//
// The mapping is deliberately coarse. TaskTrooper's tool names describe
// capabilities (read_file, run_terminal); the CLI's describe implementations
// (Read, Bash). One capability can imply two CLI tools and two capabilities can
// imply one, so this is a translation between vocabularies, not a lookup table
// that could be generated.

// NativeSubagentTool is the built-in that hands work to a subagent session.
// The CLI resolves its newer name, Agent, to this one.
const NativeSubagentTool = "Task"

// nativeAlways are the built-ins every session keeps regardless of policy.
//
// Read/Glob/Grep are read-only and are how a session orients itself in a
// workspace it has never seen; a run that cannot list a directory burns its
// turns guessing at paths. TodoWrite writes nothing outside the transcript.
// Withholding these buys no safety and costs the run its footing.
//
// Task is here because delegating is the agent's call on every CLI, not a
// grant: cursor-agent, opencode and agy expose their subagent tools with no
// switch, and a subagent cannot reach past the session it was spawned from —
// it inherits this same --tools surface and the run's MCP policy.
var nativeAlways = []string{"Read", "Glob", "Grep", "TodoWrite", NativeSubagentTool}

// nativeByCapability maps a TaskTrooper tool name to the built-ins that grant
// the same reach. A capability absent from an agent's policy takes its CLI
// equivalents with it.
var nativeByCapability = map[string][]string{
	"run_terminal": {"Bash", "BashOutput", "KillShell"},
	"write_file":   {"Write", "Edit", "NotebookEdit"},
	"edit_file":    {"Write", "Edit", "NotebookEdit"},
	"delete_file":  {"Write", "Edit"},
	"web_search":   {"WebSearch"},
	"fetch_url":    {"WebFetch"},
}

// NativeToolsForPolicy returns the built-in tools a claude_code session should
// be given under p, or nil when p places no restriction.
//
// nil means "omit --tools", which leaves the CLI on its own default of every
// built-in. That is the honest answer for an unrestricted policy: an empty
// allowlist has always meant unrestricted everywhere else in this package
// (ToolAllowedByPolicy returns true for it), and a caller that turned the
// policy off should not silently get a narrowed session instead.
func NativeToolsForPolicy(p ToolPolicy) []string {
	if len(p.AllowTools) == 0 {
		return nil
	}

	set := make(map[string]struct{}, len(nativeAlways)+8)
	for _, t := range nativeAlways {
		set[t] = struct{}{}
	}
	// Matched through ToolAllowedByPolicy rather than by comparing strings, so
	// a policy written with patterns ("browser_*", "*_file") grants the same
	// built-ins a policy that spells every name out would.
	for capability, natives := range nativeByCapability {
		if !ToolAllowedByPolicy(capability, p) {
			continue
		}
		for _, n := range natives {
			set[n] = struct{}{}
		}
	}

	out := make([]string, 0, len(set))
	for t := range set {
		out = append(out, t)
	}
	// Sorted so two runs of the same agent produce byte-identical command
	// lines — the same reason buildArgs fixes its own order. A tool list that
	// reshuffled per run would also move the prompt-cache prefix under the
	// session for no reason.
	sort.Strings(out)
	return out
}

// EffortLevels are the values the CLI's --effort flag accepts, in ascending
// order of how much a session will think before it answers.
//
// Exported so the API can reject a typo at the edge instead of letting it reach
// the command line: an unknown level fails the spawn, and a run that dies on
// argument parsing looks to its caller exactly like a run that died for a real
// reason. Empty is always valid and means "no --effort flag" — the CLI's own
// default stands.
var EffortLevels = []string{"low", "medium", "high", "xhigh", "max"}

// ValidEffort reports whether e is an acceptable Agent.Effort. The empty string
// is valid: it is how an agent says it has no opinion.
func ValidEffort(e string) bool {
	if e == "" {
		return true
	}
	for _, lvl := range EffortLevels {
		if lvl == e {
			return true
		}
	}
	return false
}
