package workspace_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/workspace"
)

func TestValidateProjectRoot_InsideWorkspaceRoot(t *testing.T) {
	wsRoot := filepath.Join(t.TempDir(), "workspaces")
	repo := filepath.Join(wsRoot, "repos", "api")
	require.NoError(t, os.MkdirAll(repo, 0o755))

	root, err := workspace.ValidateProjectRoot(repo, wsRoot, nil)
	require.NoError(t, err)
	require.Equal(t, mustAbs(repo), root)
}

// An empty allowed_roots is not a wildcard. It used to mean "any absolute
// path", which made POST /v1/repositories/open a one-request read of any
// directory the server could see.
func TestValidateProjectRoot_EmptyAllowlistRefusesArbitraryPaths(t *testing.T) {
	wsRoot := filepath.Join(t.TempDir(), "workspaces")
	require.NoError(t, os.MkdirAll(wsRoot, 0o755))
	outside := t.TempDir()

	_, err := workspace.ValidateProjectRoot(outside, wsRoot, nil)
	require.Error(t, err)
	// The refusal must not confirm the directory exists.
	require.NotContains(t, err.Error(), outside)
}

// allowed_roots only ever WIDENS the set, for the install that keeps its
// checkouts outside the managed workspace.
func TestValidateProjectRoot_AllowedRootWidens(t *testing.T) {
	wsRoot := filepath.Join(t.TempDir(), "workspaces")
	outside := t.TempDir()

	root, err := workspace.ValidateProjectRoot(outside, wsRoot, []string{outside})
	require.NoError(t, err)
	require.Equal(t, mustAbs(outside), root)
}

func TestValidateProjectRoot_WildcardAllowed(t *testing.T) {
	wsRoot := filepath.Join(t.TempDir(), "workspaces")
	outside := t.TempDir()

	root, err := workspace.ValidateProjectRoot(outside, wsRoot, []string{"*"})
	require.NoError(t, err)
	require.Equal(t, mustAbs(outside), root)
}

func TestValidateProjectRoot_CaseInsensitive(t *testing.T) {
	wsRoot := filepath.Join(t.TempDir(), "workspaces")
	outside := t.TempDir()

	// Even with different casing in allowed root, it should match
	root, err := workspace.ValidateProjectRoot(outside, wsRoot, []string{filepath.Dir(outside)})
	require.NoError(t, err)
	require.Equal(t, mustAbs(outside), root)
}

func TestValidateProjectRoot_NotDirectory(t *testing.T) {
	f, err := os.CreateTemp("", "file-*")
	require.NoError(t, err)
	_ = f.Close()
	_, err = workspace.ValidateProjectRoot(f.Name(), t.TempDir(), nil)
	require.Error(t, err)
}

func mustAbs(path string) string {
	abs, _ := filepath.Abs(path)
	return abs
}
