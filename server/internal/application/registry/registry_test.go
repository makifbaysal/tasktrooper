package registry_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type stubTool struct {
	name string
}

func (s *stubTool) Name() string { return s.name }
func (s *stubTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type:     "function",
		Function: domain.FunctionDefinition{Name: s.name, Description: "stub"},
	}
}
func (s *stubTool) Execute(_ context.Context, args string) domain.ToolResult {
	return domain.ToolResult{Name: s.name, Content: "result:" + args}
}

var _ port.ToolExecutor = (*stubTool)(nil)

type RegistrySuite struct {
	suite.Suite
}

func (s *RegistrySuite) TestRegisterAndDefinitions() {
	reg := registry.New()
	reg.Register(&stubTool{name: "tool_a"})
	reg.Register(&stubTool{name: "tool_b"})

	defs := reg.Definitions()
	s.Len(defs, 2)
}

func (s *RegistrySuite) TestDefinitionsAndNamesComeBackSortedByName() {
	reg := registry.New()
	for _, name := range []string{"write_file", "grep_code", "read_file", "codebase_search", "run_terminal", "edit_lines"} {
		reg.Register(&stubTool{name: name})
	}
	want := []string{"codebase_search", "edit_lines", "grep_code", "read_file", "run_terminal", "write_file"}

	for range 20 {
		names := make([]string, 0, len(want))
		for _, d := range reg.DefinitionsForPolicy(domain.ToolPolicy{}) {
			names = append(names, d.Function.Name)
		}
		s.Equal(want, names)
		s.Equal(want, reg.AllToolNames())
	}
}

func (s *RegistrySuite) TestExecuteKnownTool() {
	reg := registry.New()
	reg.Register(&stubTool{name: "echo"})

	result := reg.Execute(context.Background(), domain.ToolCall{
		ID: "c1", Type: "function",
		Function: domain.FunctionCall{Name: "echo", Arguments: "hello"},
	})
	s.False(result.IsError)
	s.Equal("result:hello", result.Content)
}

func (s *RegistrySuite) TestExecuteUnknownTool() {
	reg := registry.New()
	result := reg.Execute(context.Background(), domain.ToolCall{
		ID: "c2", Type: "function",
		Function: domain.FunctionCall{Name: "nonexistent"},
	})
	s.True(result.IsError)
}

func (s *RegistrySuite) TestDefinitionsForPolicyAllowOnly() {
	reg := registry.New()
	reg.Register(&stubTool{name: "run_terminal"})
	reg.Register(&stubTool{name: "web_search"})
	reg.Register(&stubTool{name: "mcp_browser_navigate"})

	policy := domain.ToolPolicy{AllowTools: []string{"web_search"}}
	defs := reg.DefinitionsForPolicy(policy)
	s.Len(defs, 2)
	names := map[string]bool{}
	for _, d := range defs {
		names[d.Function.Name] = true
	}
	s.False(names["run_terminal"])
	s.True(names["web_search"])
	s.True(names["mcp_browser_navigate"])
}

func (s *RegistrySuite) TestExecuteWithPolicyBlocked() {
	reg := registry.New()
	reg.Register(&stubTool{name: "run_terminal"})

	policy := domain.ToolPolicy{AllowTools: []string{"web_search"}}
	result := reg.ExecuteWithPolicy(context.Background(), domain.ToolCall{
		ID: "c1", Type: "function",
		Function: domain.FunctionCall{Name: "run_terminal", Arguments: `{}`},
	}, policy)
	s.True(result.IsError)
	s.Contains(result.Content, "not allowed")
}

func TestRegistrySuite(t *testing.T) {
	suite.Run(t, new(RegistrySuite))
}
