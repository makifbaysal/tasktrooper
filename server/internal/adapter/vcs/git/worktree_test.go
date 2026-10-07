package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestADetachedWorktreeHoldsTheCommitAndLeavesNothingBehind(t *testing.T) {
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q")
	require.NoError(t, os.WriteFile(filepath.Join(repo, "a.go"), []byte("package a\n"), 0o644))
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-q", "-m", "first")
	c := NewClient()
	sha, err := c.HeadSHA(context.Background(), repo)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(repo, "a.go"), []byte("package a // edited\n"), 0o644))

	dir, remove, err := c.AddDetachedWorktree(context.Background(), repo, sha)
	require.NoError(t, err)
	body, err := os.ReadFile(filepath.Join(dir, "a.go"))
	require.NoError(t, err)
	require.Equal(t, "package a\n", string(body), "the checkout holds the commit, not the repository's working tree")
	got, err := c.HeadSHA(context.Background(), dir)
	require.NoError(t, err)
	require.Equal(t, sha, got)

	remove()

	require.NoDirExists(t, filepath.Dir(dir))
	require.NotContains(t, gitRun(t, repo, "worktree", "list"), filepath.Base(filepath.Dir(dir)))
	require.Equal(t, "M a.go", gitRun(t, repo, "status", "--porcelain"), "the repository's own edit is untouched")
}
