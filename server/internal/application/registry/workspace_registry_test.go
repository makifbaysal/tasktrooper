package registry_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func (s *RegistrySuite) runTerminalWorkingDir(root, requested string) string {
	inner := registry.New()
	inner.Register(&stubTool{name: "run_terminal"})
	reg := registry.NewWorkspaceRegistry(inner, nil)
	args, err := json.Marshal(map[string]string{"command": "ls", "working_dir": requested})
	s.Require().NoError(err)

	result := reg.Execute(registry.ContextWithWorkspaceDir(context.Background(), root), domain.ToolCall{
		ID: "c1", Type: "function",
		Function: domain.FunctionCall{Name: "run_terminal", Arguments: string(args)},
	})

	var seen map[string]string
	s.Require().NoError(json.Unmarshal([]byte(result.Content[len("result:"):]), &seen))
	return seen["working_dir"]
}

// A relative working_dir is the repository's, not the server process's: it
// used to resolve against the server's cwd, fail the scope check, and run the
// command in the repository root instead.
func (s *RegistrySuite) TestWorkingDirIsResolvedAgainstTheWorkspace() {
	root := s.T().TempDir()
	s.Require().NoError(os.MkdirAll(filepath.Join(root, "web"), 0o755))

	s.Equal(filepath.Join(root, "web"), s.runTerminalWorkingDir(root, "web"))
	s.Equal(root, s.runTerminalWorkingDir(root, ""))
}

// Out of scope is left as asked so run_terminal refuses it by name, rather
// than running the command in the repository root without saying so.
func (s *RegistrySuite) TestOutOfScopeWorkingDirIsNotSwappedForTheRoot() {
	root := s.T().TempDir()
	outside := s.T().TempDir()

	s.Equal(outside, s.runTerminalWorkingDir(root, outside))
}
