package registry

import (
	"path"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const mcpToolPrefix = "mcp_"

// MCPSources maps a registered tool name to the MCP server it proxies.
type MCPSources map[string]domain.MCPToolSource

func matchesPattern(name, pattern string) bool {
	if pattern == name {
		return true
	}
	if strings.Contains(pattern, "*") {
		ok, err := path.Match(pattern, name)
		return err == nil && ok
	}
	return false
}

func matchesAny(name string, patterns []string) bool {
	for _, p := range patterns {
		if matchesPattern(name, p) {
			return true
		}
	}
	return false
}

func isMCPTool(name string) bool {
	return strings.HasPrefix(name, mcpToolPrefix)
}

// resolve finds the MCP server behind a tool name. A name registered without
// its source (tests, or anything outside adapter/mcp) is attributed to the
// longest known server id it is namespaced under, so "mcp_my_server_list" is
// never read as server "my"; only an id nothing else claims falls back to
// the first underscore.
func (s MCPSources) resolve(name string) (domain.MCPToolSource, bool) {
	if !isMCPTool(name) {
		return domain.MCPToolSource{}, false
	}
	if src, ok := s[name]; ok {
		return src, true
	}
	var best domain.MCPToolSource
	for _, src := range s {
		if src.ServerID == "" || len(src.ServerID) <= len(best.ServerID) {
			continue
		}
		if strings.HasPrefix(name, mcpToolPrefix+src.ServerID+"_") {
			best = src
		}
	}
	if best.ServerID != "" {
		return best, true
	}
	rest := strings.TrimPrefix(name, mcpToolPrefix)
	if idx := strings.Index(rest, "_"); idx > 0 {
		return domain.MCPToolSource{ServerID: rest[:idx], Access: domain.MCPAccessAll}, true
	}
	return domain.MCPToolSource{Access: domain.MCPAccessAll}, true
}

// IsToolAllowed applies a policy to one tool. MCP tools are decided by
// server: an explicit allow_mcp_servers list admits exactly those servers,
// and without one only servers whose access mode is "all" are visible — so a
// zero policy still hides a "listed" server.
func IsToolAllowed(name string, policy domain.ToolPolicy, sources MCPSources) bool {
	if src, ok := sources.resolve(name); ok {
		return policy.AllowsMCPServer(src.ServerID, src.Access)
	}
	if len(policy.AllowTools) == 0 {
		return true
	}
	return matchesAny(name, policy.AllowTools)
}

func FilterToolNames(all []string, policy domain.ToolPolicy, sources MCPSources) []string {
	result := make([]string, 0, len(all))
	for _, name := range all {
		if IsToolAllowed(name, policy, sources) {
			result = append(result, name)
		}
	}
	return result
}
