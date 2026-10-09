package git

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	githubapi "github.com/makifbaysal/tasktrooper/server/internal/adapter/vcs/github"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

// recordingCheckout records every git child of every call and pre-seeds the
// token's identity, so a commit is attributed without a /user round-trip.
func recordingCheckout() (*Checkout, *commandRecorder) {
	recorder := &commandRecorder{}
	k := &Checkout{clientFor: func(token string) *Client {
		c := tokenClient(token)
		if token != "" {
			c.identityToken = token
			c.identity = githubapi.Identity{Login: "tester", ID: 42}
			c.identityAt = time.Now()
		}
		c.observe = func(cmd *exec.Cmd) {
			recorder.cmds = append(recorder.cmds, recordedCommand{
				argv: append([]string(nil), cmd.Args...),
				env:  append([]string(nil), cmd.Env...),
			})
		}
		return c
	}}
	return k, recorder
}

func TestCheckoutStatusListsBranchAndEveryKindOfChange(t *testing.T) {
	root, _ := newSyncFixture(t)
	writeFile(t, root, "first.go", "package main\n\nfunc main() {}\n")
	writeFile(t, root, "new dir/untracked.txt", "x")
	gitRun(t, root, "mv", "first.go", "renamed.go")
	writeFile(t, root, "renamed.go", "package main\n\nfunc main() {}\n")

	status, err := NewCheckout().Status(context.Background(), root)
	require.NoError(t, err)

	assert.Equal(t, "main", status.Branch)
	assert.Len(t, status.Head, 40)
	assert.Equal(t, "origin/main", status.Upstream)
	assert.False(t, status.Clean)
	byPath := map[string]port.CheckoutFileStatus{}
	for _, f := range status.Files {
		byPath[f.Path] = f
	}
	require.Contains(t, byPath, "renamed.go")
	assert.Equal(t, "first.go", byPath["renamed.go"].OrigPath)
	assert.Equal(t, "R", byPath["renamed.go"].Index)
	require.Contains(t, byPath, "new dir/untracked.txt")
	assert.Equal(t, "?", byPath["new dir/untracked.txt"].Index)
}

func TestCheckoutStatusOfACleanCheckout(t *testing.T) {
	root, _ := newSyncFixture(t)

	status, err := NewCheckout().Status(context.Background(), root)
	require.NoError(t, err)
	assert.True(t, status.Clean)
	assert.Empty(t, status.Files)
	assert.Zero(t, status.Ahead)
}

func TestCheckoutRefusesADirectoryWithoutARepository(t *testing.T) {
	_, err := NewCheckout().Status(context.Background(), t.TempDir())
	assert.ErrorIs(t, err, port.ErrCheckoutNotRepository)
}

func TestCheckoutDiffIsTheTasksWholeChangeAgainstItsBase(t *testing.T) {
	root, _ := newSyncFixture(t)
	gitRun(t, root, "checkout", "-b", "feature/tt-1")
	writeFile(t, root, "committed.go", "package main\n")
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "-m", "committed")
	writeFile(t, root, "first.go", "package main\n// edited\n")

	k := NewCheckout()
	diff, err := k.Diff(context.Background(), root, port.CheckoutDiffQuery{})
	require.NoError(t, err)
	assert.Len(t, diff.Base, 40)
	assert.ElementsMatch(t, []string{"committed.go", "first.go"}, diff.Files)
	assert.Contains(t, diff.Stat, "2 files changed")
	assert.Contains(t, diff.Patch, "+// edited")
	assert.False(t, diff.Truncated)

	uncommitted, err := k.Diff(context.Background(), root, port.CheckoutDiffQuery{Base: "HEAD", NameOnly: true})
	require.NoError(t, err)
	assert.Equal(t, []string{"first.go"}, uncommitted.Files)
	assert.Empty(t, uncommitted.Patch)

	capped, err := k.Diff(context.Background(), root, port.CheckoutDiffQuery{MaxBytes: 10})
	require.NoError(t, err)
	assert.True(t, capped.Truncated)
	assert.Len(t, capped.Patch, 10)
}

func TestCheckoutDiffRefusesABaseThatCouldBeAFlag(t *testing.T) {
	root, _ := newSyncFixture(t)
	_, err := NewCheckout().Diff(context.Background(), root, port.CheckoutDiffQuery{Base: "--output=/tmp/x"})
	assert.ErrorIs(t, err, port.ErrCheckoutInvalid)
}

func TestCheckoutLogReadsTheTasksOwnCommits(t *testing.T) {
	root, _ := newSyncFixture(t)
	gitRun(t, root, "checkout", "-b", "feature/tt-1")
	writeFile(t, root, "a.go", "package main\n")
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "-m", "add a")

	k := NewCheckout()
	all, err := k.Log(context.Background(), root, port.CheckoutLogQuery{})
	require.NoError(t, err)
	require.Len(t, all, 2)
	assert.Equal(t, "add a", all[0].Subject)
	assert.Equal(t, "test", all[0].Author)

	own, err := k.Log(context.Background(), root, port.CheckoutLogQuery{Base: "origin/main"})
	require.NoError(t, err)
	require.Len(t, own, 1)
	assert.Equal(t, "add a", own[0].Subject)
}

func TestCommitPushCutsTheTaskBranchAndPushesWithTheTokenOffArgv(t *testing.T) {
	root, _ := newSyncFixture(t)
	writeFile(t, root, "feature.go", "package main\n")
	k, recorder := recordingCheckout()

	result, err := k.CommitPush(context.Background(), root, port.CheckoutCommitPush{
		Message: "feat: the feature", Branch: "feature/tt-7", Token: testToken,
	})
	require.NoError(t, err)

	assert.Equal(t, "feature/tt-7", result.Branch)
	assert.True(t, result.BranchCreated)
	assert.True(t, result.Committed)
	assert.NotEqual(t, result.HeadBefore, result.Head)
	assert.Equal(t, "feat: the feature", result.Subject)
	assert.Equal(t, []string{"feature.go"}, result.Files)
	assert.Equal(t, result.Head, gitRun(t, root, "rev-parse", "origin/feature/tt-7"))

	for _, cmd := range recorder.cmds {
		for _, arg := range cmd.argv {
			assert.NotContains(t, arg, testToken)
			assert.NotContains(t, arg, basicToken(testToken))
		}
	}
	push := recorder.withVerb("push")
	require.NotNil(t, push)
	value, ok := push.envValue("GIT_CONFIG_VALUE_0")
	require.True(t, ok)
	assert.Equal(t, "AUTHORIZATION: basic "+basicToken(testToken), value)
}

func TestCommitPushOnTheTaskBranchCommitsWhatIsLeft(t *testing.T) {
	root, _ := newSyncFixture(t)
	gitRun(t, root, "checkout", "-b", "feature/tt-8")
	writeFile(t, root, "agent_commit.go", "package main\n")
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "-m", "the agent's own commit")
	writeFile(t, root, "left_over.go", "package main\n")

	result, err := NewCheckout().CommitPush(context.Background(), root, port.CheckoutCommitPush{
		Message: "chore: leftovers", Branch: "feature/tt-8",
	})
	require.NoError(t, err)
	assert.False(t, result.BranchCreated)
	assert.True(t, result.Committed)
	assert.ElementsMatch(t, []string{"agent_commit.go", "left_over.go"}, result.Files)
	assert.Equal(t, result.Head, gitRun(t, root, "rev-parse", "origin/feature/tt-8"))
}

func TestCommitPushWithNothingNewStillPublishesTheBranch(t *testing.T) {
	root, _ := newSyncFixture(t)
	gitRun(t, root, "checkout", "-b", "feature/tt-9")
	writeFile(t, root, "done.go", "package main\n")
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "-m", "already committed")

	result, err := NewCheckout().CommitPush(context.Background(), root, port.CheckoutCommitPush{
		Message: "unused", Branch: "feature/tt-9",
	})
	require.NoError(t, err)
	assert.False(t, result.Committed)
	assert.Equal(t, "already committed", result.Subject)
	assert.Equal(t, result.Head, gitRun(t, root, "rev-parse", "origin/feature/tt-9"))
}

func TestCommitPushRefusals(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, root string)
		req   port.CheckoutCommitPush
		want  error
	}{
		{name: "the default branch", req: port.CheckoutCommitPush{Message: "m", Branch: "main"}, want: port.ErrCheckoutConflict},
		{name: "a branch that could be a flag", req: port.CheckoutCommitPush{Message: "m", Branch: "--force"}, want: port.ErrCheckoutInvalid},
		{name: "a range", req: port.CheckoutCommitPush{Message: "m", Branch: "a..b"}, want: port.ErrCheckoutInvalid},
		{name: "no message", req: port.CheckoutCommitPush{Message: "  ", Branch: "feature/x"}, want: port.ErrCheckoutInvalid},
		{
			name: "work on another branch while the task branch exists",
			setup: func(t *testing.T, root string) {
				gitRun(t, root, "branch", "feature/x")
				gitRun(t, root, "checkout", "-b", "elsewhere")
			},
			req:  port.CheckoutCommitPush{Message: "m", Branch: "feature/x"},
			want: port.ErrCheckoutConflict,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, _ := newSyncFixture(t)
			if tt.setup != nil {
				tt.setup(t, root)
			}
			writeFile(t, root, "work.go", "package main\n")
			_, err := NewCheckout().CommitPush(context.Background(), root, tt.req)
			require.Error(t, err)
			assert.True(t, errors.Is(err, tt.want), "got %v", err)
			assert.Contains(t, gitRun(t, root, "status", "--porcelain"), "work.go", "a refusal leaves the work where it was")
		})
	}
}

func TestCommitPushReportsARejectedPush(t *testing.T) {
	root, _ := newSyncFixture(t)
	gitRun(t, root, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "gone.git"))
	writeFile(t, root, "work.go", "package main\n")

	_, err := NewCheckout().CommitPush(context.Background(), root, port.CheckoutCommitPush{Message: "m", Branch: "feature/y"})
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "git push"), err.Error())
}
