package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type undoFixture struct {
	origin, ws, base, branch string
}

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644))
}

// newUndoFixture is a bare origin with main, and a task workspace cloned from it
// on its own branch, where a run has since committed one change, edited a
// tracked file without committing, and created two untracked files.
func newUndoFixture(t *testing.T) undoFixture {
	t.Helper()
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	gitRun(t, root, "init", "--bare", "--initial-branch=main", origin)
	seed := filepath.Join(root, "seed")
	gitRun(t, root, "clone", origin, seed)
	write(t, seed, "app.txt", "v1\n")
	write(t, seed, "other.txt", "o1\n")
	gitRun(t, seed, "add", ".")
	gitRun(t, seed, "commit", "-m", "base")
	gitRun(t, seed, "push", "origin", "main")

	ws := filepath.Join(root, "ws")
	gitRun(t, root, "clone", origin, ws)
	branch := "tt-7-export"
	gitRun(t, ws, "checkout", "-b", branch)
	base := gitRun(t, ws, "rev-parse", "HEAD")

	write(t, ws, "app.txt", "v2 by the run\n")
	gitRun(t, ws, "commit", "-am", "TT-7: change app")
	write(t, ws, "other.txt", "o2 uncommitted\n")
	write(t, ws, "created-by-run.txt", "new\n")
	write(t, ws, "made-by-shell.log", "log\n")
	return undoFixture{origin: origin, ws: ws, base: base, branch: branch}
}

func TestUndoStateReadsWhatTheRunDid(t *testing.T) {
	f := newUndoFixture(t)
	st, err := NewClient().UndoState(context.Background(), f.ws, f.base, f.branch)
	require.NoError(t, err)
	require.Equal(t, f.branch, st.Branch)
	require.True(t, st.BaseIsAncestor)
	require.Len(t, st.Commits, 1)
	require.Equal(t, "TT-7: change app", st.Commits[0].Subject)
	paths := map[string]string{}
	for _, c := range st.Changes {
		paths[c.Path] = c.Status
	}
	require.Equal(t, map[string]string{"app.txt": "M", "other.txt": "M", "created-by-run.txt": "?", "made-by-shell.log": "?"}, paths)
	var uncommitted []string
	for _, c := range st.Uncommitted {
		uncommitted = append(uncommitted, c.Path)
	}
	require.ElementsMatch(t, []string{"other.txt", "created-by-run.txt", "made-by-shell.log"}, uncommitted)
	require.False(t, st.Pushed)
	require.Empty(t, st.RemoteBranchSHA)
}

func TestUndoResetSavesEverythingThenResetsOnlyTheTaskBranch(t *testing.T) {
	f := newUndoFixture(t)
	c := NewClient()
	mainBefore := gitRun(t, f.ws, "rev-parse", "origin/main")
	ref := "tasktrooper/undo-backup/0a1b2c3d-0000-0000-0000-000000000000"

	out, err := c.UndoReset(context.Background(), f.ws, port.UndoPlan{
		Branch: f.branch, Base: f.base, BackupRef: ref, CleanPaths: []string{"created-by-run.txt"},
	})
	require.NoError(t, err)

	require.Equal(t, f.base, gitRun(t, f.ws, "rev-parse", "HEAD"))
	require.Equal(t, "v1", strings.TrimSpace(readFile(t, filepath.Join(f.ws, "app.txt"))))
	require.Equal(t, "o1", strings.TrimSpace(readFile(t, filepath.Join(f.ws, "other.txt"))))
	require.NoFileExists(t, filepath.Join(f.ws, "created-by-run.txt"))
	require.FileExists(t, filepath.Join(f.ws, "made-by-shell.log"), "an untracked file the trace does not list is left alone")
	require.Equal(t, []string{"created-by-run.txt"}, out.CleanedPaths)
	require.Equal(t, []string{"made-by-shell.log"}, out.LeftUntracked)

	require.Equal(t, ref, out.BackupRef)
	require.Equal(t, "v2 by the run", strings.TrimSpace(gitRun(t, f.ws, "show", ref+":app.txt")))
	require.Equal(t, "o2 uncommitted", strings.TrimSpace(gitRun(t, f.ws, "show", ref+":other.txt")), "uncommitted edits are in the backup")
	require.Equal(t, "new", strings.TrimSpace(gitRun(t, f.ws, "show", ref+":created-by-run.txt")), "so are untracked files")
	require.Equal(t, mainBefore, gitRun(t, f.ws, "rev-parse", "origin/main"), "main is untouched")
	require.Equal(t, "main", gitRun(t, f.ws, "rev-parse", "--abbrev-ref", "origin/HEAD")[len("origin/"):])

	again, err := c.UndoReset(context.Background(), f.ws, port.UndoPlan{Branch: f.branch, Base: f.base, BackupRef: ref})
	require.NoError(t, err)
	require.Equal(t, ref+"-2", again.BackupRef, "a second undo never overwrites an earlier backup")
}

func TestUndoResetRefusesWhatItMustNeverTouch(t *testing.T) {
	f := newUndoFixture(t)
	c := NewClient()
	ref := "tasktrooper/undo-backup/0a1b2c3d-0000-0000-0000-000000000000"
	plan := func(mod func(*port.UndoPlan)) port.UndoPlan {
		p := port.UndoPlan{Branch: f.branch, Base: f.base, BackupRef: ref}
		mod(&p)
		return p
	}
	headBefore := gitRun(t, f.ws, "rev-parse", "HEAD")

	for name, p := range map[string]port.UndoPlan{
		"another branch than the checked out one": plan(func(p *port.UndoPlan) { p.Branch = "something-else" }),
		"an option as the base":                   plan(func(p *port.UndoPlan) { p.Base = "--hard" }),
		"a backup name outside the namespace":     plan(func(p *port.UndoPlan) { p.BackupRef = "main" }),
		"a base that is not behind HEAD":          plan(func(p *port.UndoPlan) { p.Base = strings.Repeat("a", 40) }),
	} {
		_, err := c.UndoReset(context.Background(), f.ws, p)
		require.Error(t, err, name)
	}
	require.Equal(t, headBefore, gitRun(t, f.ws, "rev-parse", "HEAD"), "no refusal changed anything")
	require.FileExists(t, filepath.Join(f.ws, "created-by-run.txt"))

	gitRun(t, f.ws, "checkout", "main")
	_, err := c.UndoReset(context.Background(), f.ws, port.UndoPlan{Branch: "main", Base: f.base, BackupRef: ref})
	require.ErrorContains(t, err, "never reset", "the default branch is never reset")
}

func TestForcePushUsesALeaseAndNeverPushesMain(t *testing.T) {
	f := newUndoFixture(t)
	c := NewClient()
	gitRun(t, f.ws, "add", "-A")
	gitRun(t, f.ws, "commit", "-m", "TT-7: more")
	gitRun(t, f.ws, "push", "-u", "origin", f.branch)
	pushed := gitRun(t, f.ws, "rev-parse", "HEAD")

	st, err := c.UndoState(context.Background(), f.ws, f.base, f.branch)
	require.NoError(t, err)
	require.True(t, st.Pushed)
	require.Equal(t, pushed, st.RemoteBranchSHA)
	require.False(t, st.RemoteAhead)

	_, err = c.UndoReset(context.Background(), f.ws, port.UndoPlan{Branch: f.branch, Base: f.base, BackupRef: "tasktrooper/undo-backup/0a1b2c3d-0000-0000-0000-000000000000"})
	require.NoError(t, err)

	require.Error(t, c.ForcePushBranch(context.Background(), f.ws, f.branch, strings.Repeat("b", 40)), "a lease on a tip the remote does not have is refused")
	require.Equal(t, pushed, gitRun(t, f.origin, "rev-parse", f.branch))
	require.NoError(t, c.ForcePushBranch(context.Background(), f.ws, f.branch, pushed))
	require.Equal(t, f.base, gitRun(t, f.origin, "rev-parse", f.branch))
	require.ErrorContains(t, c.ForcePushBranch(context.Background(), f.ws, "main", pushed), "never force-pushed")
}

func TestRevertMergeBranchesFromMainAndNeverRewritesHistory(t *testing.T) {
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	gitRun(t, root, "init", "--bare", "--initial-branch=main", origin)
	repo := filepath.Join(root, "repo")
	gitRun(t, root, "clone", origin, repo)
	write(t, repo, "app.txt", "v1\n")
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-m", "base")
	gitRun(t, repo, "push", "origin", "main")
	gitRun(t, repo, "checkout", "-b", "tt-7-feature")
	write(t, repo, "app.txt", "v2 broken\n")
	gitRun(t, repo, "commit", "-am", "TT-7: feature")
	gitRun(t, repo, "checkout", "main")
	gitRun(t, repo, "merge", "--no-ff", "-m", "Merge TT-7", "tt-7-feature")
	gitRun(t, repo, "push", "origin", "main")
	mergeSHA := gitRun(t, repo, "rev-parse", "HEAD")

	mirror := filepath.Join(root, "mirror")
	gitRun(t, root, "clone", origin, mirror)
	out, err := NewClient().RevertMerge(context.Background(), mirror, port.RevertRequest{MergeSHA: mergeSHA, Branch: "tasktrooper/revert-tt-7"})
	require.NoError(t, err)
	require.Equal(t, "tasktrooper/revert-tt-7", out.Branch)
	require.NotEmpty(t, out.PRError, "a local remote has no pull request to open; the branch is still pushed")

	require.Equal(t, mergeSHA, gitRun(t, origin, "rev-parse", "main"), "main is exactly where it was")
	require.Equal(t, out.RevertSHA, gitRun(t, origin, "rev-parse", "tasktrooper/revert-tt-7"))
	require.Equal(t, "v1", strings.TrimSpace(gitRun(t, origin, "show", "tasktrooper/revert-tt-7:app.txt")))
	require.Equal(t, mergeSHA, gitRun(t, origin, "rev-parse", "tasktrooper/revert-tt-7~1"), "the revert sits on top of the merge")
}

func TestRevertMergeRefusesBadInput(t *testing.T) {
	c := NewClient()
	_, err := c.RevertMerge(context.Background(), t.TempDir(), port.RevertRequest{MergeSHA: "--all", Branch: "x"})
	require.Error(t, err)
	_, err = c.RevertMerge(context.Background(), t.TempDir(), port.RevertRequest{MergeSHA: strings.Repeat("a", 40), Branch: "main"})
	require.Error(t, err)
}
