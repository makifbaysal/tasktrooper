package discovery

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// gitRun fails the test on error: every command here is fixture setup, so a
// failure means the fixture is broken rather than the behavior under test.
func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func writeCommit(t *testing.T, dir, path, body, message string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", message)
}

// newGitFixture builds a bare origin with one commit on main, plus a "seed"
// clone new branches and commits are pushed from, and the "root" clone the
// functions under test read — the same two-clone shape client_test.go uses,
// needed because branch/merge/commit facts are read off refs/remotes/origin.
func newGitFixture(t *testing.T) (root, seed string) {
	t.Helper()
	base := t.TempDir()
	origin := filepath.Join(base, "origin.git")
	gitRun(t, base, "init", "--bare", "--initial-branch=main", origin)

	seed = filepath.Join(base, "seed")
	gitRun(t, base, "clone", origin, seed)
	writeCommit(t, seed, "README.md", "hello\n", "feat: init")
	gitRun(t, seed, "push", "origin", "main")

	root = filepath.Join(base, "root")
	gitRun(t, base, "clone", origin, root)
	return root, seed
}

func TestBranchConventionDominantPrefix(t *testing.T) {
	root, seed := newGitFixture(t)
	ctx := context.Background()

	for _, name := range []string{"feature/a", "feature/b", "feature/c", "fix/d"} {
		gitRun(t, seed, "checkout", "-b", name)
		writeCommit(t, seed, name+".txt", "x\n", "feat: "+name)
		gitRun(t, seed, "push", "origin", name)
		gitRun(t, seed, "checkout", "main")
	}
	gitRun(t, root, "fetch", "origin")

	samples, pattern := branchConvention(ctx, root, "main")
	require.Contains(t, pattern, "feature/* prefix", "the 3-of-4 prefix must win")
	require.Contains(t, pattern, "3 of 4 recent branches")
	require.Len(t, samples, 4, "every non-default branch must be sampled")
}

func TestBranchConventionFlatNames(t *testing.T) {
	root, seed := newGitFixture(t)
	ctx := context.Background()

	for _, name := range []string{"alpha", "beta"} {
		gitRun(t, seed, "checkout", "-b", name)
		writeCommit(t, seed, name+".txt", "x\n", "feat: "+name)
		gitRun(t, seed, "push", "origin", name)
		gitRun(t, seed, "checkout", "main")
	}
	gitRun(t, root, "fetch", "origin")

	_, pattern := branchConvention(ctx, root, "main")
	require.Contains(t, pattern, "flat branch names")
}

func TestBranchConventionNoOtherBranches(t *testing.T) {
	root, _ := newGitFixture(t)
	ctx := context.Background()

	samples, pattern := branchConvention(ctx, root, "main")
	require.Nil(t, samples)
	require.Empty(t, pattern)
}

func TestMergeStyleLinearWhenNoMerges(t *testing.T) {
	root, seed := newGitFixture(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		writeCommit(t, seed, fmt.Sprintf("file%d.txt", i), "x\n", fmt.Sprintf("feat: file %d", i))
	}
	gitRun(t, seed, "push", "origin", "main")
	gitRun(t, root, "pull", "origin", "main")

	style, direct := mergeStyle(ctx, root)
	require.True(t, direct, "a merge-free history lands directly on the default branch")
	require.Contains(t, style, "linear history")
}

func TestMergeStyleReportsMergeCommitRatio(t *testing.T) {
	root, seed := newGitFixture(t)
	ctx := context.Background()

	gitRun(t, seed, "checkout", "-b", "feature/x")
	writeCommit(t, seed, "feature.txt", "1", "feat: add feature")
	gitRun(t, seed, "checkout", "main")
	gitRun(t, seed, "merge", "--no-ff", "-m", "Merge feature/x", "feature/x")
	gitRun(t, seed, "push", "origin", "main")
	gitRun(t, root, "pull", "origin", "main")

	style, direct := mergeStyle(ctx, root)
	require.False(t, direct)
	require.Contains(t, style, "merge commits", "1 merge of 3 commits clears the merges*4>=total bar")
}

func TestMergeStyleReportsMostlyLinear(t *testing.T) {
	root, seed := newGitFixture(t)
	ctx := context.Background()

	for i := 0; i < 8; i++ {
		writeCommit(t, seed, "churn.txt", fmt.Sprintf("v%d", i), fmt.Sprintf("chore: churn %d", i))
	}
	gitRun(t, seed, "checkout", "-b", "feature/x")
	writeCommit(t, seed, "feature.txt", "1", "feat: add feature")
	gitRun(t, seed, "checkout", "main")
	gitRun(t, seed, "merge", "--no-ff", "-m", "Merge feature/x", "feature/x")
	gitRun(t, seed, "push", "origin", "main")
	gitRun(t, root, "pull", "origin", "main")

	style, direct := mergeStyle(ctx, root)
	require.False(t, direct)
	require.Contains(t, style, "mostly linear", "1 merge of 11 commits misses the merges*4>=total bar")
}

func TestCommitStyleConventionalMajority(t *testing.T) {
	root, seed := newGitFixture(t)
	ctx := context.Background()

	writeCommit(t, seed, "a.txt", "1", "fix: bug")
	writeCommit(t, seed, "b.txt", "2", "docs: update readme")
	writeCommit(t, seed, "c.txt", "3", "random subject line")
	gitRun(t, seed, "push", "origin", "main")
	gitRun(t, root, "pull", "origin", "main")

	style := commitStyle(ctx, root)
	require.Contains(t, style, "Conventional Commits")
	require.Contains(t, style, "3 of last 4")
}

func TestCommitStyleFreeFormMajority(t *testing.T) {
	root, seed := newGitFixture(t)
	ctx := context.Background()

	writeCommit(t, seed, "a.txt", "1", "wip")
	writeCommit(t, seed, "b.txt", "2", "more stuff")
	writeCommit(t, seed, "c.txt", "3", "fixes")
	gitRun(t, seed, "push", "origin", "main")
	gitRun(t, root, "pull", "origin", "main")

	style := commitStyle(ctx, root)
	require.Contains(t, style, "free-form subjects")
	require.Contains(t, style, "1 of last 4")
}

func TestHotspotsRanksByChurnAndExcludesGithubDir(t *testing.T) {
	root, seed := newGitFixture(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		writeCommit(t, seed, "server/hot.go", fmt.Sprintf("v%d", i), fmt.Sprintf("chore: touch hot %d", i))
	}
	for i := 0; i < 2; i++ {
		writeCommit(t, seed, "server/warm.go", fmt.Sprintf("v%d", i), fmt.Sprintf("chore: touch warm %d", i))
	}
	writeCommit(t, seed, ".github/workflows/ci.yml", "name: CI\n", "ci: add workflow")
	gitRun(t, seed, "push", "origin", "main")
	gitRun(t, root, "pull", "origin", "main")

	hot := hotspots(ctx, root)
	require.Len(t, hot, 1, "warm.go stays under the 3-commit threshold and .github/ is always excluded")
	require.Equal(t, domain.GitHotspot{Path: "server/hot.go", Commits: 3}, hot[0])
}

func TestGitFactsPopulatesConventions(t *testing.T) {
	root, seed := newGitFixture(t)
	ctx := context.Background()

	writeCommit(t, seed, "a.txt", "1", "fix: bug")
	gitRun(t, seed, "checkout", "-b", "feature/x")
	writeCommit(t, seed, "feature.txt", "1", "feat: add feature")
	gitRun(t, seed, "push", "origin", "feature/x")
	gitRun(t, seed, "checkout", "main")
	gitRun(t, seed, "push", "origin", "main")
	gitRun(t, root, "fetch", "origin")
	gitRun(t, root, "pull", "origin", "main")

	g, warnings := gitFacts(ctx, root)
	require.Empty(t, warnings)
	require.Equal(t, "main", g.DefaultBranch)
	require.NotEmpty(t, g.MergeStyle)
	require.NotEmpty(t, g.CommitStyle)
	require.NotEmpty(t, g.BranchPattern)
	require.NotEmpty(t, g.BranchSamples)
}

func TestGitFactsWarnsWhenNotAGitRepository(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	g, warnings := gitFacts(ctx, root)
	require.Equal(t, domain.ScanGit{}, g)
	require.NotEmpty(t, warnings)
}
