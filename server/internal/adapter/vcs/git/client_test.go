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

	githubapi "github.com/makifbaysal/tasktrooper/server/internal/adapter/vcs/github"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// gitRun fails the test on error: every command here is setup, so a failure
// means the fixture is broken rather than the behaviour under test.
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

// newSyncFixture builds a bare origin with one commit on main, plus two clones:
// the "project root" the indexer walks and an "author" clone used to push new
// commits to origin.
func newSyncFixture(t *testing.T) (root, author string) {
	t.Helper()
	base := t.TempDir()
	origin := filepath.Join(base, "origin.git")
	gitRun(t, base, "init", "--bare", "--initial-branch=main", origin)

	author = filepath.Join(base, "author")
	gitRun(t, base, "clone", origin, author)
	if err := os.WriteFile(filepath.Join(author, "first.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, author, "add", ".")
	gitRun(t, author, "commit", "-m", "first")
	gitRun(t, author, "push", "origin", "main")

	root = filepath.Join(base, "root")
	gitRun(t, base, "clone", origin, root)
	return root, author
}

func TestSyncDefaultBranchPullsNewCommits(t *testing.T) {
	root, author := newSyncFixture(t)

	// A push that lands after the project root was cloned — exactly the case
	// that used to leave the index pinned to the clone-time commit.
	if err := os.WriteFile(filepath.Join(author, "second.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, author, "add", ".")
	gitRun(t, author, "commit", "-m", "second")
	gitRun(t, author, "push", "origin", "main")

	if _, err := os.Stat(filepath.Join(root, "second.go")); !os.IsNotExist(err) {
		t.Fatalf("fixture invalid: second.go already present in project root")
	}

	if err := NewClient().SyncDefaultBranch(context.Background(), root); err != nil {
		t.Fatalf("SyncDefaultBranch: %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, "second.go")); err != nil {
		t.Fatalf("working tree was not advanced, second.go missing: %v", err)
	}
	if got, want := gitRun(t, root, "rev-parse", "HEAD"), gitRun(t, author, "rev-parse", "HEAD"); got != want {
		t.Fatalf("HEAD = %s, want %s", got, want)
	}
}

// TestSyncDefaultBranchDiscardsBlockingEdit is the reported failure: an edit to
// a tracked file in the mirror clone made `merge --ff-only` abort with "Your
// local changes would be overwritten by merge", which froze the index at an old
// commit while the UI reported a completed run. Nothing writes to this tree on
// purpose, so the edit is discarded and the sync goes through.
func TestSyncDefaultBranchDiscardsBlockingEdit(t *testing.T) {
	root, author := newSyncFixture(t)

	if err := os.WriteFile(filepath.Join(author, "first.go"), []byte("package main // remote\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, author, "add", ".")
	gitRun(t, author, "commit", "-m", "remote edit")
	gitRun(t, author, "push", "origin", "main")

	// The same file, edited locally: exactly what blocks a fast-forward.
	if err := os.WriteFile(filepath.Join(root, "first.go"), []byte("package main // local junk\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := NewClient().SyncDefaultBranch(context.Background(), root); err != nil {
		t.Fatalf("SyncDefaultBranch must recover from a dirty mirror: %v", err)
	}
	if got, want := gitRun(t, root, "rev-parse", "HEAD"), gitRun(t, author, "rev-parse", "HEAD"); got != want {
		t.Fatalf("HEAD = %s, want origin's %s", got, want)
	}
	body, err := os.ReadFile(filepath.Join(root, "first.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "remote") {
		t.Fatalf("working tree still carries the local edit: %q", string(body))
	}
}

// TestSyncDefaultBranchKeepsUntrackedFiles: the recovery overwrites tracked
// files only. Anything a deployment keeps beside the checkout (env files,
// caches, agent worktrees) is not what blocks a fast-forward and must survive.
func TestSyncDefaultBranchKeepsUntrackedFiles(t *testing.T) {
	root, _ := newSyncFixture(t)
	if err := os.WriteFile(filepath.Join(root, "first.go"), []byte("package main // local\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".env.local"), []byte("SECRET=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := NewClient().SyncDefaultBranch(context.Background(), root); err != nil {
		t.Fatalf("SyncDefaultBranch: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".env.local")); err != nil {
		t.Fatalf("untracked file was deleted: %v", err)
	}
}

// TestSyncDefaultBranchResetsDivergedMirror: a commit made in the mirror is
// contamination, not work — task branches live in per-task workspaces — so the
// sync resets onto origin instead of refusing forever.
func TestSyncDefaultBranchResetsDivergedMirror(t *testing.T) {
	root, author := newSyncFixture(t)

	if err := os.WriteFile(filepath.Join(author, "remote.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, author, "add", ".")
	gitRun(t, author, "commit", "-m", "remote work")
	gitRun(t, author, "push", "origin", "main")

	if err := os.WriteFile(filepath.Join(root, "local.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "-m", "local work")

	if err := NewClient().SyncDefaultBranch(context.Background(), root); err != nil {
		t.Fatalf("SyncDefaultBranch must recover from a diverged mirror: %v", err)
	}
	if got, want := gitRun(t, root, "rev-parse", "HEAD"), gitRun(t, author, "rev-parse", "HEAD"); got != want {
		t.Fatalf("HEAD = %s, want origin's %s", got, want)
	}
	if _, err := os.Stat(filepath.Join(root, "remote.go")); err != nil {
		t.Fatalf("origin's commit was not checked out: %v", err)
	}
}

// TestSyncDefaultBranchReturnsToDefaultBranch: a mirror left on some other
// branch is put back rather than reported — a stuck branch is how the clone
// stopped tracking origin in the first place.
func TestSyncDefaultBranchReturnsToDefaultBranch(t *testing.T) {
	root, author := newSyncFixture(t)
	gitRun(t, root, "checkout", "-b", "feature/work")

	if err := os.WriteFile(filepath.Join(author, "second.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, author, "add", ".")
	gitRun(t, author, "commit", "-m", "second")
	gitRun(t, author, "push", "origin", "main")

	if err := NewClient().SyncDefaultBranch(context.Background(), root); err != nil {
		t.Fatalf("SyncDefaultBranch: %v", err)
	}
	if got := gitRun(t, root, "rev-parse", "--abbrev-ref", "HEAD"); got != "main" {
		t.Fatalf("branch = %s, want main", got)
	}
	if _, err := os.Stat(filepath.Join(root, "second.go")); err != nil {
		t.Fatalf("working tree was not advanced: %v", err)
	}
}

func TestSyncDefaultBranchRejectsNonRepo(t *testing.T) {
	if err := NewClient().SyncDefaultBranch(context.Background(), t.TempDir()); err == nil {
		t.Fatal("expected an error for a path that is not a git repository")
	}
}

// --- task workspace reuse -------------------------------------------------

const testBranch = "feature/task-abcd1234-work"

// taskFixture is a whole board setup on disk: the bare origin every clone talks
// to, the shared project root a task workspace is cut from, an "author" clone
// standing in for whatever else pushes to origin (another task's merged PR, a
// developer, CI), and the workspace path itself.
type taskFixture struct {
	origin    string
	root      string
	author    string
	workspace string
}

func newTaskFixture(t *testing.T) taskFixture {
	t.Helper()
	root, author := newSyncFixture(t)
	base := filepath.Dir(root)
	return taskFixture{
		origin:    filepath.Join(base, "origin.git"),
		root:      root,
		author:    author,
		workspace: filepath.Join(base, "task-1"),
	}
}

// pushUpstream lands a commit on origin/main after the workspace was created —
// the drift that makes a reused workspace stale.
func (f taskFixture) pushUpstream(t *testing.T, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(f.author, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, f.author, "add", ".")
	gitRun(t, f.author, "commit", "-m", "upstream: "+name)
	gitRun(t, f.author, "push", "origin", "main")
}

// commitLocal makes a commit in the workspace that origin has never seen.
func (f taskFixture) commitLocal(t *testing.T, name, content string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(f.workspace, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, f.workspace, "add", ".")
	gitRun(t, f.workspace, "commit", "-m", "agent work: "+name)
	return gitRun(t, f.workspace, "rev-parse", "HEAD")
}

// ensure runs the call under test: the second and later runs on a task, which
// find the workspace already on disk.
func (f taskFixture) ensure(t *testing.T) error {
	t.Helper()
	return NewClient().EnsureTaskWorkspace(context.Background(), f.root, f.workspace, testBranch)
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func requireFile(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
}

// requireNoRebaseInProgress guards the invariant that matters most on failure:
// the agent must never be handed a tree stopped halfway through a rebase.
func requireNoRebaseInProgress(t *testing.T, workspace string) {
	t.Helper()
	for _, dir := range []string{"rebase-merge", "rebase-apply"} {
		if _, err := os.Stat(filepath.Join(workspace, ".git", dir)); err == nil {
			t.Fatalf("workspace left mid-rebase: .git/%s exists", dir)
		}
	}
}

func TestEnsureTaskWorkspaceCreatesBranchFromDefault(t *testing.T) {
	f := newTaskFixture(t)
	if err := f.ensure(t); err != nil {
		t.Fatalf("EnsureTaskWorkspace: %v", err)
	}
	if got := gitRun(t, f.workspace, "rev-parse", "--abbrev-ref", "HEAD"); got != testBranch {
		t.Fatalf("branch = %s, want %s", got, testBranch)
	}
	requireFile(t, filepath.Join(f.workspace, "first.go"))
}

func TestEnsureTaskWorkspaceReuseFastForwardsCleanTree(t *testing.T) {
	f := newTaskFixture(t)
	if err := f.ensure(t); err != nil {
		t.Fatalf("first EnsureTaskWorkspace: %v", err)
	}
	f.pushUpstream(t, "merged.go", "package main\n")

	if err := f.ensure(t); err != nil {
		t.Fatalf("reuse: %v", err)
	}

	// The whole point: the second run sees the commit that landed on origin in
	// between, instead of the tree the first run was cut from.
	requireFile(t, filepath.Join(f.workspace, "merged.go"))
	if got, want := gitRun(t, f.workspace, "rev-parse", "HEAD"), gitRun(t, f.author, "rev-parse", "HEAD"); got != want {
		t.Fatalf("HEAD = %s, want origin/main %s", got, want)
	}
	if got := gitRun(t, f.workspace, "rev-parse", "--abbrev-ref", "HEAD"); got != testBranch {
		t.Fatalf("branch = %s, want %s", got, testBranch)
	}
}

func TestEnsureTaskWorkspaceReuseIsIdempotent(t *testing.T) {
	f := newTaskFixture(t)
	if err := f.ensure(t); err != nil {
		t.Fatalf("first EnsureTaskWorkspace: %v", err)
	}
	f.pushUpstream(t, "merged.go", "package main\n")
	if err := f.ensure(t); err != nil {
		t.Fatalf("second EnsureTaskWorkspace: %v", err)
	}
	head := gitRun(t, f.workspace, "rev-parse", "HEAD")

	if err := f.ensure(t); err != nil {
		t.Fatalf("third EnsureTaskWorkspace: %v", err)
	}
	if got := gitRun(t, f.workspace, "rev-parse", "HEAD"); got != head {
		t.Fatalf("a second refresh moved HEAD: %s, want %s", got, head)
	}
}

func TestEnsureTaskWorkspaceReuseRebasesLocalCommits(t *testing.T) {
	f := newTaskFixture(t)
	if err := f.ensure(t); err != nil {
		t.Fatalf("first EnsureTaskWorkspace: %v", err)
	}
	f.commitLocal(t, "agent.go", "package main // agent\n")
	f.pushUpstream(t, "merged.go", "package main\n")

	if err := f.ensure(t); err != nil {
		t.Fatalf("reuse: %v", err)
	}

	// Both sides survive: the agent's commit is replayed on top of the commit
	// that landed upstream, so the branch has the new base AND the work.
	requireFile(t, filepath.Join(f.workspace, "agent.go"))
	requireFile(t, filepath.Join(f.workspace, "merged.go"))
	upstream := gitRun(t, f.author, "rev-parse", "HEAD")
	if got := gitRun(t, f.workspace, "rev-parse", "HEAD~1"); got != upstream {
		t.Fatalf("agent commit was not replayed onto origin/main: HEAD~1 = %s, want %s", got, upstream)
	}
	if out := gitRun(t, f.workspace, "status", "--porcelain"); out != "" {
		t.Fatalf("tree is not clean after rebase:\n%s", out)
	}
}

func TestTaskPatchIDEmptyWithNoDiff(t *testing.T) {
	f := newTaskFixture(t)
	if err := f.ensure(t); err != nil {
		t.Fatalf("EnsureTaskWorkspace: %v", err)
	}
	id, err := NewClient().TaskPatchID(context.Background(), f.workspace)
	if err != nil {
		t.Fatalf("TaskPatchID: %v", err)
	}
	if id != "" {
		t.Fatalf("id = %q, want empty for a branch with no diff against origin/main", id)
	}
}

func TestTaskPatchIDStableAcrossRebase(t *testing.T) {
	f := newTaskFixture(t)
	if err := f.ensure(t); err != nil {
		t.Fatalf("first EnsureTaskWorkspace: %v", err)
	}
	f.commitLocal(t, "agent.go", "package main // agent\n")
	before, err := NewClient().TaskPatchID(context.Background(), f.workspace)
	if err != nil {
		t.Fatalf("TaskPatchID before rebase: %v", err)
	}
	if before == "" {
		t.Fatal("expected a non-empty patch id for a real diff")
	}

	f.pushUpstream(t, "merged.go", "package main\n")
	if err := f.ensure(t); err != nil {
		t.Fatalf("reuse (rebase): %v", err)
	}

	after, err := NewClient().TaskPatchID(context.Background(), f.workspace)
	if err != nil {
		t.Fatalf("TaskPatchID after rebase: %v", err)
	}
	if after != before {
		t.Fatalf("id changed across a rebase that replayed the same patch: before=%s after=%s", before, after)
	}
}

func TestTaskPatchIDChangesWithContent(t *testing.T) {
	f := newTaskFixture(t)
	if err := f.ensure(t); err != nil {
		t.Fatalf("EnsureTaskWorkspace: %v", err)
	}
	f.commitLocal(t, "agent.go", "package main // v1\n")
	first, err := NewClient().TaskPatchID(context.Background(), f.workspace)
	if err != nil {
		t.Fatalf("TaskPatchID: %v", err)
	}

	f.commitLocal(t, "agent.go", "package main // v2, a real change\n")
	second, err := NewClient().TaskPatchID(context.Background(), f.workspace)
	if err != nil {
		t.Fatalf("TaskPatchID: %v", err)
	}

	if first == second {
		t.Fatalf("id did not change for a genuinely different diff: %s", first)
	}
}

func TestEnsureTaskWorkspaceReuseConflictAbortsAndFails(t *testing.T) {
	f := newTaskFixture(t)
	if err := f.ensure(t); err != nil {
		t.Fatalf("first EnsureTaskWorkspace: %v", err)
	}
	localHead := f.commitLocal(t, "shared.go", "package main // agent version\n")
	f.pushUpstream(t, "shared.go", "package main // upstream version\n")

	err := f.ensure(t)
	if err == nil {
		t.Fatal("expected a rebase conflict to be reported")
	}
	if !strings.Contains(err.Error(), testBranch) {
		t.Fatalf("error should name the branch, got: %v", err)
	}

	requireNoRebaseInProgress(t, f.workspace)
	if got := gitRun(t, f.workspace, "rev-parse", "HEAD"); got != localHead {
		t.Fatalf("HEAD moved despite the abort: %s, want %s", got, localHead)
	}
	if got := readFile(t, filepath.Join(f.workspace, "shared.go")); got != "package main // agent version\n" {
		t.Fatalf("agent's committed work was altered: %q", got)
	}
	if out := gitRun(t, f.workspace, "status", "--porcelain"); out != "" {
		t.Fatalf("conflict markers left in the tree:\n%s", out)
	}
}

func TestEnsureTaskWorkspaceReuseAfterSquashMergeMovesOntoDefault(t *testing.T) {
	f := newTaskFixture(t)
	if err := f.ensure(t); err != nil {
		t.Fatalf("first EnsureTaskWorkspace: %v", err)
	}
	f.commitLocal(t, "shared.go", "package main // v1\n")
	f.commitLocal(t, "shared.go", "package main // v2\n")
	gitRun(t, f.workspace, "push", "origin", testBranch)

	// The PR is squash-merged and its branch deleted: main holds the branch's
	// content as one commit, so replaying v1 onto it conflicts.
	gitRun(t, f.author, "fetch", "origin")
	gitRun(t, f.author, "merge", "--squash", "origin/"+testBranch)
	gitRun(t, f.author, "commit", "-m", "squash: task")
	gitRun(t, f.author, "push", "origin", "main")
	gitRun(t, f.author, "push", "origin", "--delete", testBranch)

	if err := f.ensure(t); err != nil {
		t.Fatalf("reuse after squash merge: %v", err)
	}
	requireNoRebaseInProgress(t, f.workspace)
	if got, want := gitRun(t, f.workspace, "rev-parse", "HEAD"), gitRun(t, f.author, "rev-parse", "HEAD"); got != want {
		t.Fatalf("HEAD = %s, want origin/main %s", got, want)
	}
	if got := gitRun(t, f.workspace, "rev-parse", "--abbrev-ref", "HEAD"); got != testBranch {
		t.Fatalf("branch = %s, want %s", got, testBranch)
	}
}

func TestEnsureTaskWorkspaceReuseKeepsUnmergedWorkWhenRemoteBranchIsGone(t *testing.T) {
	f := newTaskFixture(t)
	if err := f.ensure(t); err != nil {
		t.Fatalf("first EnsureTaskWorkspace: %v", err)
	}
	localHead := f.commitLocal(t, "shared.go", "package main // agent version\n")
	f.pushUpstream(t, "shared.go", "package main // upstream version\n")

	if err := f.ensure(t); err == nil {
		t.Fatal("expected a real conflict to still be reported")
	}
	if got := gitRun(t, f.workspace, "rev-parse", "HEAD"); got != localHead {
		t.Fatalf("unmerged work was discarded: HEAD = %s, want %s", got, localHead)
	}
}

func TestEnsureTaskWorkspaceReuseKeepsDirtyTree(t *testing.T) {
	f := newTaskFixture(t)
	if err := f.ensure(t); err != nil {
		t.Fatalf("first EnsureTaskWorkspace: %v", err)
	}
	// A run that stopped to ask the stakeholder a question commits nothing, so
	// its edits are still sitting in the tree when the next run reuses it.
	if err := os.WriteFile(filepath.Join(f.workspace, "first.go"), []byte("package main // half-done\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.workspace, "scratch.go"), []byte("package main // never staged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.pushUpstream(t, "merged.go", "package main\n")

	if err := f.ensure(t); err != nil {
		t.Fatalf("reuse: %v", err)
	}

	if got := readFile(t, filepath.Join(f.workspace, "first.go")); got != "package main // half-done\n" {
		t.Fatalf("uncommitted edit was discarded: %q", got)
	}
	if got := readFile(t, filepath.Join(f.workspace, "scratch.go")); got != "package main // never staged\n" {
		t.Fatalf("untracked file was discarded: %q", got)
	}
	requireFile(t, filepath.Join(f.workspace, "merged.go"))
	if out := gitRun(t, f.workspace, "stash", "list"); out != "" {
		t.Fatalf("work was left behind in a stash:\n%s", out)
	}
}

func TestEnsureTaskWorkspaceReuseRollsBackWhenDirtyTreeConflicts(t *testing.T) {
	f := newTaskFixture(t)
	if err := f.ensure(t); err != nil {
		t.Fatalf("first EnsureTaskWorkspace: %v", err)
	}
	head := gitRun(t, f.workspace, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(f.workspace, "first.go"), []byte("package main // agent edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.pushUpstream(t, "first.go", "package main // upstream edit\n")

	if err := f.ensure(t); err == nil {
		t.Fatal("expected the conflicting uncommitted work to be reported")
	}

	requireNoRebaseInProgress(t, f.workspace)
	if got := gitRun(t, f.workspace, "rev-parse", "HEAD"); got != head {
		t.Fatalf("HEAD moved despite the rollback: %s, want %s", got, head)
	}
	if got := readFile(t, filepath.Join(f.workspace, "first.go")); got != "package main // agent edit\n" {
		t.Fatalf("uncommitted work was destroyed: %q", got)
	}
	if out := gitRun(t, f.workspace, "stash", "list"); out != "" {
		t.Fatalf("work was left stranded in a stash:\n%s", out)
	}
}

func TestEnsureTaskWorkspaceReuseDoesNotRewritePublishedBranch(t *testing.T) {
	f := newTaskFixture(t)
	if err := f.ensure(t); err != nil {
		t.Fatalf("first EnsureTaskWorkspace: %v", err)
	}
	f.commitLocal(t, "agent.go", "package main // agent\n")
	gitRun(t, f.workspace, "push", "-u", "origin", "HEAD")
	published := gitRun(t, f.workspace, "rev-parse", "HEAD")
	f.pushUpstream(t, "merged.go", "package main\n")

	if err := f.ensure(t); err != nil {
		t.Fatalf("reuse: %v", err)
	}

	// Rebasing a branch origin already has onto the default branch would rewrite
	// pushed commits, and the non-force push this client uses would then be
	// rejected. The branch stays where origin has it.
	if got := gitRun(t, f.workspace, "rev-parse", "HEAD"); got != published {
		t.Fatalf("published branch was rewritten: HEAD = %s, want %s", got, published)
	}
	if _, err := os.Stat(filepath.Join(f.workspace, "merged.go")); !os.IsNotExist(err) {
		t.Fatal("published branch was rebased onto the default branch")
	}
}

func TestEnsureTaskWorkspaceReusePicksUpBranchPushedElsewhere(t *testing.T) {
	f := newTaskFixture(t)
	if err := f.ensure(t); err != nil {
		t.Fatalf("first EnsureTaskWorkspace: %v", err)
	}
	// Another host (a reviewer's pod, a re-imported task) pushed the task branch.
	gitRun(t, f.author, "checkout", "-b", testBranch)
	if err := os.WriteFile(filepath.Join(f.author, "elsewhere.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, f.author, "add", ".")
	gitRun(t, f.author, "commit", "-m", "work from another host")
	gitRun(t, f.author, "push", "origin", testBranch)

	if err := f.ensure(t); err != nil {
		t.Fatalf("reuse: %v", err)
	}
	requireFile(t, filepath.Join(f.workspace, "elsewhere.go"))
}

func TestEnsureTaskWorkspaceReuseReattachesDetachedHead(t *testing.T) {
	f := newTaskFixture(t)
	if err := f.ensure(t); err != nil {
		t.Fatalf("first EnsureTaskWorkspace: %v", err)
	}
	gitRun(t, f.workspace, "checkout", "--detach", "HEAD")
	f.pushUpstream(t, "merged.go", "package main\n")

	if err := f.ensure(t); err != nil {
		t.Fatalf("reuse: %v", err)
	}
	if got := gitRun(t, f.workspace, "rev-parse", "--abbrev-ref", "HEAD"); got != testBranch {
		t.Fatalf("HEAD is still detached or on the wrong branch: %s", got)
	}
	requireFile(t, filepath.Join(f.workspace, "merged.go"))
}

func TestEnsureTaskWorkspaceReuseRecreatesMissingBranch(t *testing.T) {
	f := newTaskFixture(t)
	if err := f.ensure(t); err != nil {
		t.Fatalf("first EnsureTaskWorkspace: %v", err)
	}
	gitRun(t, f.workspace, "checkout", "main")
	gitRun(t, f.workspace, "branch", "-D", testBranch)
	f.pushUpstream(t, "merged.go", "package main\n")

	if err := f.ensure(t); err != nil {
		t.Fatalf("reuse: %v", err)
	}
	if got := gitRun(t, f.workspace, "rev-parse", "--abbrev-ref", "HEAD"); got != testBranch {
		t.Fatalf("branch = %s, want %s", got, testBranch)
	}
	requireFile(t, filepath.Join(f.workspace, "merged.go"))
}

// A project root with no remote of its own leaves the workspace's "origin"
// pointing at a directory on this machine. Refresh has to fetch the project
// root's own updates through that path instead of assuming GitHub.
func TestEnsureTaskWorkspaceReuseWithLocalProjectRootOrigin(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, root, "init", "--initial-branch=main")
	if err := os.WriteFile(filepath.Join(root, "first.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "-m", "first")

	workspace := filepath.Join(base, "task-1")
	client := NewClient()
	if err := client.EnsureTaskWorkspace(context.Background(), root, workspace, testBranch); err != nil {
		t.Fatalf("EnsureTaskWorkspace: %v", err)
	}
	if got := client.OriginURL(context.Background(), workspace); got != root {
		t.Fatalf("workspace origin = %q, want the project root %q", got, root)
	}

	// The project root moves on, exactly as it would after another task merged.
	if err := os.WriteFile(filepath.Join(root, "later.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "-m", "later")

	if err := client.EnsureTaskWorkspace(context.Background(), root, workspace, testBranch); err != nil {
		t.Fatalf("reuse: %v", err)
	}
	requireFile(t, filepath.Join(workspace, "later.go"))
	if got := gitRun(t, workspace, "rev-parse", "--abbrev-ref", "HEAD"); got != testBranch {
		t.Fatalf("branch = %s, want %s", got, testBranch)
	}
}

// Concurrency: two tasks on the same project refresh at the same time. They own
// separate directories (the runner keys them by task id and serialises the runs
// that share one), so the only shared state is the project root they fetch from.
func TestEnsureTaskWorkspaceReuseConcurrentWorkspaces(t *testing.T) {
	root, author := newSyncFixture(t)
	base := filepath.Dir(root)
	client := NewClient()
	branches := []string{"feature/task-1-a", "feature/task-2-b", "feature/task-3-c"}
	paths := make([]string, len(branches))
	for i, branch := range branches {
		paths[i] = filepath.Join(base, "task-"+branch[len(branch)-1:])
		if err := client.EnsureTaskWorkspace(context.Background(), root, paths[i], branch); err != nil {
			t.Fatalf("create %s: %v", paths[i], err)
		}
	}

	if err := os.WriteFile(filepath.Join(author, "merged.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, author, "add", ".")
	gitRun(t, author, "commit", "-m", "merged")
	gitRun(t, author, "push", "origin", "main")

	errs := make(chan error, len(branches))
	for i, branch := range branches {
		go func() {
			errs <- client.EnsureTaskWorkspace(context.Background(), root, paths[i], branch)
		}()
	}
	for range branches {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent refresh: %v", err)
		}
	}
	for _, path := range paths {
		requireFile(t, filepath.Join(path, "merged.go"))
	}
}

// A commit GitHub cannot trace to an account is a commit nobody is credited
// for — and Vercel refuses to build the PR it heads. The connected account's
// noreply address is what makes the commit the user's.
func TestCommitAndPushAttributesTheConnectedAccount(t *testing.T) {
	root, _ := newSyncFixture(t)

	c := NewClient()
	c.SetTokenSource(func(context.Context) (string, error) { return "tok", nil })
	// Pre-seeded rather than fetched: this test is about what lands in the
	// commit, not about the /user round-trip (covered in the github package).
	c.identityToken = "tok"
	c.identity = githubapi.Identity{Login: "makifbaysal", ID: 12345}
	c.identityAt = time.Now()

	if err := os.WriteFile(filepath.Join(root, "third.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := c.CommitAndPush(context.Background(), root, "feat: add third"); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}

	if got := gitRun(t, root, "log", "-1", "--format=%an <%ae>"); got != "makifbaysal <12345+makifbaysal@users.noreply.github.com>" {
		t.Fatalf("author = %s", got)
	}
	if got := gitRun(t, root, "log", "-1", "--format=%cn <%ce>"); got != "makifbaysal <12345+makifbaysal@users.noreply.github.com>" {
		t.Fatalf("committer = %s", got)
	}
}

// With no GitHub connected the fallback identity still has to produce a
// commit: an unattributed commit beats a discarded run.
func TestCommitAndPushWithoutTokenKeepsTheFallbackIdentity(t *testing.T) {
	root, _ := newSyncFixture(t)

	c := NewClient()
	if err := os.WriteFile(filepath.Join(root, "fourth.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := c.CommitAndPush(context.Background(), root, "feat: add fourth"); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}
	if got := gitRun(t, root, "log", "-1", "--format=%ae"); got != "agents@tasktrooper.ai" {
		t.Fatalf("author email = %s", got)
	}
}

// Merging is the one operation with no gh-CLI fallback. Every other method here
// degrades to the CLI when no token is connected, because opening a PR twice or
// failing to fetch is recoverable. A merge is not: its safety rests entirely on
// the expected-head-SHA precondition, and expressing that through the CLI
// depends on a `gh pr merge --match-head-commit` flag whose presence varies with
// the gh version on the host. Dropping the precondition silently is exactly the
// failure it exists to prevent, so a token-less deployment is refused instead.
func TestMergePullRequestWithoutTokenRefusesRatherThanUsingTheCLI(t *testing.T) {
	c := NewClient()

	_, err := c.MergePullRequest(context.Background(), domain.PullRequestMergeRequest{
		Owner: "acme", Repo: "widget", Number: 42, ExpectedHeadSHA: "1111111",
	})
	if err == nil {
		t.Fatal("a merge with no GitHub token must be refused")
	}
	if !strings.Contains(err.Error(), "token") {
		t.Fatalf("the refusal must name what is missing: %v", err)
	}
}

// Without a token, EnsurePullRequest falls back to the gh CLI. `gh pr create
// --fill` alone lets gh derive the title from commit history, which on a
// multi-commit branch collapses to the branch name instead of anything
// task-shaped. The fallback must pass an explicit --title, taken from the
// branch's last commit subject same as the API path does, so `--fill` is only
// ever left to fill in the body.
func TestEnsurePullRequestWithoutTokenTitlesFromTheLastCommitSubject(t *testing.T) {
	root, author := newSyncFixture(t)
	_ = root
	gitRun(t, author, "commit", "--allow-empty", "-m", "feat: add the widget endpoint")

	fakeGh, err := filepath.Abs("testdata/fake-gh.sh")
	if err != nil {
		t.Fatal(err)
	}
	binDir := t.TempDir()
	if err := os.Symlink(fakeGh, filepath.Join(binDir, "gh")); err != nil {
		t.Fatal(err)
	}
	fakeDir := t.TempDir()
	t.Setenv("GH_FAKE_DIR", fakeDir)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	url, err := NewClient().EnsurePullRequest(context.Background(), author)
	if err != nil {
		t.Fatalf("EnsurePullRequest: %v", err)
	}
	if url != "https://github.com/acme/widgets/pull/1" {
		t.Fatalf("url = %s", url)
	}

	argv, err := os.ReadFile(filepath.Join(fakeDir, "gh-argv.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(argv), "pr create --fill --title feat: add the widget endpoint\n") {
		t.Fatalf("gh was not called with an explicit --title from the last commit subject:\n%s", argv)
	}
}

// Coordinates are checked before anything is sent: a request assembled from an
// unresolved owner/repo would address some other repository.
func TestMergePullRequestRejectsIncompleteCoordinates(t *testing.T) {
	c := NewClient()
	c.SetTokenSource(func(context.Context) (string, error) { return "tok", nil })

	if _, err := c.MergePullRequest(context.Background(), domain.PullRequestMergeRequest{
		Owner: "acme", Number: 42, ExpectedHeadSHA: "1111111",
	}); err == nil {
		t.Fatal("a merge without a repository must be refused")
	}
}

func TestEnsurePullRequestRefusesNonGitHubOriginWithoutCallingGh(t *testing.T) {
	root, _ := newSyncFixture(t)
	gitRun(t, root, "remote", "set-url", "origin", "https://gitlab.com/acme/app.git")

	_, err := NewClient().EnsurePullRequest(context.Background(), root)

	if !errors.Is(err, domain.ErrNotGitHubRepo) {
		t.Fatalf("err = %v, want ErrNotGitHubRepo", err)
	}
}
