package domain_test

import (
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/stretchr/testify/assert"
)

// An unrestricted policy has always meant unrestricted everywhere else in the
// package, and the CLI's own default is every built-in. Returning a narrowed
// list here would silently take tools away from an agent whose operator turned
// the policy OFF, which is the opposite of what turning it off asks for.
func TestNativeToolsForPolicyLeavesAnEmptyPolicyAlone(t *testing.T) {
	assert.Nil(t, domain.NativeToolsForPolicy(domain.ToolPolicy{}))
	assert.Nil(t, domain.NativeToolsForPolicy(domain.ToolPolicy{
		AllowMCPServers: []string{"tasktrooper"},
	}), "an MCP-server allowlist says nothing about the built-in surface")
}

// The read-only built-ins are how a session finds its way around a workspace it
// has never seen. Withholding them buys no safety — the agent can read through
// its own MCP tools regardless — and costs the run turns spent guessing paths.
func TestNativeToolsForPolicyAlwaysKeepsTheReadOnlyTools(t *testing.T) {
	got := domain.NativeToolsForPolicy(domain.ToolPolicy{AllowTools: []string{"read_file"}})

	assert.Subset(t, got, []string{"Read", "Glob", "Grep", "TodoWrite"})
	assert.NotContains(t, got, "Bash", "run_terminal is not in this policy")
	assert.NotContains(t, got, "Write", "no write capability is in this policy")
	assert.NotContains(t, got, "WebSearch", "no web capability is in this policy")
}

// The gap this closes: a claude_code agent had the CLI's whole shell-and-file
// surface whatever its policy said. A policy that grants the terminal should
// grant Bash; one that does not, should not.
func TestNativeToolsForPolicyMapsCapabilitiesToBuiltins(t *testing.T) {
	shell := domain.NativeToolsForPolicy(domain.ToolPolicy{
		AllowTools: []string{"read_file", "run_terminal"},
	})
	assert.Subset(t, shell, []string{"Bash", "BashOutput", "KillShell"})
	assert.NotContains(t, shell, "Write")

	writer := domain.NativeToolsForPolicy(domain.ToolPolicy{
		AllowTools: []string{"write_file"},
	})
	assert.Subset(t, writer, []string{"Write", "Edit"})
	assert.NotContains(t, writer, "Bash")

	web := domain.NativeToolsForPolicy(domain.ToolPolicy{
		AllowTools: []string{"web_search", "fetch_url"},
	})
	assert.Subset(t, web, []string{"WebSearch", "WebFetch"})
}

// A policy written with patterns has to grant the same built-ins as one that
// spells every name out — the rest of the package resolves patterns, and a
// mapping that only compared literals would quietly strip the shell from every
// agent whose policy says "*_terminal".
func TestNativeToolsForPolicyHonoursPatterns(t *testing.T) {
	got := domain.NativeToolsForPolicy(domain.ToolPolicy{AllowTools: []string{"*"}})

	assert.Subset(t, got, []string{"Bash", "Write", "Edit", "WebSearch", "WebFetch"})
}

// Delegating is the agent's call on every CLI: cursor-agent, opencode and agy
// expose their subagent tools with no switch, so a restricted claude_code
// policy keeps Task too. The subagent inherits this same surface, so it buys
// no reach the policy withheld.
func TestNativeToolsForPolicyKeepsTheSubagentTool(t *testing.T) {
	assert.Contains(t, domain.NativeToolsForPolicy(domain.ToolPolicy{AllowTools: []string{"read_file"}}), domain.NativeSubagentTool)
	assert.Contains(t, domain.NativeToolsForPolicy(domain.ToolPolicy{AllowTools: []string{"*"}}), domain.NativeSubagentTool)
}

// Two runs of the same agent must produce byte-identical command lines, for the
// same reason buildArgs fixes its own flag order: a reproducible failure, and a
// prompt-cache prefix that does not move under the session.
func TestNativeToolsForPolicyIsDeterministic(t *testing.T) {
	p := domain.ToolPolicy{AllowTools: []string{"read_file", "run_terminal", "write_file"}}

	first := domain.NativeToolsForPolicy(p)
	for range 5 {
		assert.Equal(t, first, domain.NativeToolsForPolicy(p))
	}
	assert.IsIncreasing(t, first)
}

func TestValidEffort(t *testing.T) {
	assert.True(t, domain.ValidEffort(""), "empty means the executor's default")
	for _, lvl := range domain.EffortLevels {
		assert.True(t, domain.ValidEffort(lvl), lvl)
	}
	assert.False(t, domain.ValidEffort("HIGH"), "the CLI's levels are lower-case")
	assert.False(t, domain.ValidEffort("extreme"))
}
