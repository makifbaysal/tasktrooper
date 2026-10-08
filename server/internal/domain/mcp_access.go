package domain

// MCPAccess decides which agents a user-configured MCP server's tools reach
// when an agent's tool policy does not name its MCP servers.
type MCPAccess string

const (
	MCPAccessAll    MCPAccess = "all"
	MCPAccessListed MCPAccess = "listed"
)

// DefaultNewMCPAccess is what a server added through the API gets when the
// request does not say: a new server's tools (a design tool, a ticket
// tracker) reach no agent until someone names it on that agent.
const DefaultNewMCPAccess = MCPAccessListed

func (a MCPAccess) Valid() bool {
	return a == MCPAccessAll || a == MCPAccessListed
}

// Effective reads an unset mode as "all": only rows written before migration
// 179 and hand-built configs lack one, and both always reached every agent.
func (a MCPAccess) Effective() MCPAccess {
	if a == MCPAccessListed {
		return MCPAccessListed
	}
	return MCPAccessAll
}

// MCPToolSource names the MCP server a registered tool proxies and that
// server's access mode, so the tool filter never has to recover a server id
// from the namespaced tool name — ids may themselves contain "_".
type MCPToolSource struct {
	ServerID string
	Access   MCPAccess
}

// AllowsMCPServer reports whether a policy admits one MCP server's tools. A
// non-empty allow_mcp_servers is exact: the agent gets those servers and no
// others, whatever their mode. An empty one admits only "all" servers.
func (p ToolPolicy) AllowsMCPServer(serverID string, access MCPAccess) bool {
	if len(p.AllowMCPServers) > 0 {
		return matchesAnyToolPattern(serverID, p.AllowMCPServers)
	}
	return access.Effective() == MCPAccessAll
}
