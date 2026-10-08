package registry_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type FilterSuite struct {
	suite.Suite
}

func (s *FilterSuite) TestNoPolicyReturnsAll() {
	all := []string{"run_terminal", "web_search", "mcp_browser_navigate"}
	result := registry.FilterToolNames(all, domain.ToolPolicy{}, nil)
	s.ElementsMatch(all, result)
}

func (s *FilterSuite) TestAllowToolsOnly() {
	all := []string{"run_terminal", "web_search", "fetch_url", "mcp_browser_navigate"}
	policy := domain.ToolPolicy{AllowTools: []string{"web_search", "fetch_url"}}
	result := registry.FilterToolNames(all, policy, nil)
	s.ElementsMatch([]string{"web_search", "fetch_url", "mcp_browser_navigate"}, result)
}

func (s *FilterSuite) TestAllowMCPServersOnly() {
	all := []string{"run_terminal", "web_search", "mcp_browser_navigate", "mcp_git_log"}
	policy := domain.ToolPolicy{AllowMCPServers: []string{"browser"}}
	result := registry.FilterToolNames(all, policy, nil)
	s.ElementsMatch([]string{"run_terminal", "web_search", "mcp_browser_navigate"}, result)
}

func (s *FilterSuite) TestAllowToolsAndMCPServers() {
	all := []string{"run_terminal", "web_search", "mcp_browser_navigate", "mcp_git_log"}
	policy := domain.ToolPolicy{
		AllowMCPServers: []string{"browser"},
		AllowTools:      []string{"web_search"},
	}
	result := registry.FilterToolNames(all, policy, nil)
	s.ElementsMatch([]string{"web_search", "mcp_browser_navigate"}, result)
}

func (s *FilterSuite) TestWildcardAllow() {
	all := []string{"grep_code", "get_repo_tree", "get_symbol_skeleton", "web_search"}
	policy := domain.ToolPolicy{AllowTools: []string{"get_*"}}
	result := registry.FilterToolNames(all, policy, nil)
	s.Contains(result, "get_repo_tree")
	s.Contains(result, "get_symbol_skeleton")
	s.NotContains(result, "grep_code")
	s.NotContains(result, "web_search")
}

func (s *FilterSuite) TestAccessModes() {
	sources := registry.MCPSources{
		"mcp_browser_navigate": {ServerID: "browser", Access: domain.MCPAccessAll},
		"mcp_figma_get_frame":  {ServerID: "figma", Access: domain.MCPAccessListed},
		"mcp_legacy_ping":      {ServerID: "legacy"},
	}
	all := []string{"run_terminal", "mcp_browser_navigate", "mcp_figma_get_frame", "mcp_legacy_ping"}

	tests := []struct {
		name   string
		policy domain.ToolPolicy
		want   []string
	}{
		{
			name:   "zero policy sees all-mode servers but not listed ones",
			policy: domain.ToolPolicy{},
			want:   []string{"run_terminal", "mcp_browser_navigate", "mcp_legacy_ping"},
		},
		{
			name:   "a tool-only policy leaves the server rule unchanged",
			policy: domain.ToolPolicy{AllowTools: []string{"run_terminal"}},
			want:   []string{"run_terminal", "mcp_browser_navigate", "mcp_legacy_ping"},
		},
		{
			name:   "naming a listed server is what admits it",
			policy: domain.ToolPolicy{AllowMCPServers: []string{"figma"}},
			want:   []string{"run_terminal", "mcp_figma_get_frame"},
		},
		{
			name:   "an explicit list still excludes all-mode servers it does not name",
			policy: domain.ToolPolicy{AllowMCPServers: []string{"browser", "figma"}},
			want:   []string{"run_terminal", "mcp_browser_navigate", "mcp_figma_get_frame"},
		},
		{
			name:   "server patterns match like tool patterns",
			policy: domain.ToolPolicy{AllowMCPServers: []string{"fig*"}},
			want:   []string{"run_terminal", "mcp_figma_get_frame"},
		},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.ElementsMatch(tt.want, registry.FilterToolNames(all, tt.policy, sources))
		})
	}
}

func (s *FilterSuite) TestServerIDsContainingUnderscores() {
	sources := registry.MCPSources{
		"mcp_my_server_list_files": {ServerID: "my_server", Access: domain.MCPAccessListed},
		"mcp_my_ping":              {ServerID: "my", Access: domain.MCPAccessAll},
	}
	all := []string{"mcp_my_server_list_files", "mcp_my_ping", "mcp_my_server_search"}

	s.Run("the registered source wins over the name", func() {
		got := registry.FilterToolNames(all, domain.ToolPolicy{AllowMCPServers: []string{"my_server"}}, sources)
		s.ElementsMatch([]string{"mcp_my_server_list_files", "mcp_my_server_search"}, got)
	})
	s.Run("an unregistered name takes the longest known server id", func() {
		s.False(registry.IsToolAllowed("mcp_my_server_search", domain.ToolPolicy{AllowMCPServers: []string{"my"}}, sources))
		s.True(registry.IsToolAllowed("mcp_my_server_search", domain.ToolPolicy{AllowMCPServers: []string{"my_server"}}, sources))
		s.False(registry.IsToolAllowed("mcp_my_server_search", domain.ToolPolicy{}, sources),
			"it inherits the listed mode of the server it belongs to")
	})
	s.Run("the shorter id still owns its own tools", func() {
		got := registry.FilterToolNames(all, domain.ToolPolicy{AllowMCPServers: []string{"my"}}, sources)
		s.ElementsMatch([]string{"mcp_my_ping"}, got)
	})
}

func TestFilterSuite(t *testing.T) {
	suite.Run(t, new(FilterSuite))
}

type mcpStubTool struct {
	stubTool
	source domain.MCPToolSource
}

func (m *mcpStubTool) MCPSource() domain.MCPToolSource { return m.source }

type RegistryAccessSuite struct {
	suite.Suite
}

func (s *RegistryAccessSuite) TestListedServerIsHiddenFromAnAgentThatDoesNotNameIt() {
	reg := registry.New()
	reg.Register(&stubTool{name: "read_file"})
	reg.Register(&mcpStubTool{stubTool: stubTool{name: "mcp_figma_team_get_frame"}, source: domain.MCPToolSource{ServerID: "figma_team", Access: domain.MCPAccessListed}})
	reg.Register(&mcpStubTool{stubTool: stubTool{name: "mcp_browser_open"}, source: domain.MCPToolSource{ServerID: "browser", Access: domain.MCPAccessAll}})

	names := func(policy domain.ToolPolicy) []string {
		var out []string
		for _, d := range reg.DefinitionsForPolicy(policy) {
			out = append(out, d.Function.Name)
		}
		return out
	}

	s.Equal([]string{"mcp_browser_open", "read_file"}, names(domain.ToolPolicy{}))
	s.Equal([]string{"mcp_figma_team_get_frame", "read_file"}, names(domain.ToolPolicy{AllowMCPServers: []string{"figma_team"}}))
	s.Len(reg.Definitions(), 3, "the admin listing shows every tool")

	call := domain.ToolCall{ID: "c1", Function: domain.FunctionCall{Name: "mcp_figma_team_get_frame", Arguments: "{}"}}
	refused := reg.ExecuteWithPolicy(context.Background(), call, domain.ToolPolicy{})
	s.True(refused.IsError)
	s.Contains(refused.Content, "not allowed")

	allowed := reg.ExecuteWithPolicy(context.Background(), call, domain.ToolPolicy{AllowMCPServers: []string{"figma_team"}})
	s.False(allowed.IsError)
}

func (s *RegistryAccessSuite) TestReRegisteringPicksUpANewAccessModeAndUnregisterDropsTheTool() {
	reg := registry.New()
	tool := &mcpStubTool{stubTool: stubTool{name: "mcp_figma_get_frame"}, source: domain.MCPToolSource{ServerID: "figma", Access: domain.MCPAccessListed}}
	reg.Register(tool)
	s.Empty(reg.DefinitionsForPolicy(domain.ToolPolicy{}))

	reg.Register(&mcpStubTool{stubTool: tool.stubTool, source: domain.MCPToolSource{ServerID: "figma", Access: domain.MCPAccessAll}})
	s.Len(reg.DefinitionsForPolicy(domain.ToolPolicy{}), 1)

	unregisterer, ok := reg.(port.ToolUnregisterer)
	s.Require().True(ok)
	unregisterer.Unregister("mcp_figma_get_frame")
	s.Empty(reg.AllToolNames())
}

func TestRegistryAccessSuite(t *testing.T) {
	suite.Run(t, new(RegistryAccessSuite))
}
