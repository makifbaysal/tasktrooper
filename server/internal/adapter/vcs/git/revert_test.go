package git

// Rollback mechanism (ii): revert the merge commit on the default branch and
// push.
//
// This is the rollback for a repository that has no deploy workflow to
// dispatch — a push-to-deploy host builds whatever the default branch points
// at, so the only thing that redeploys it is a new commit. It runs unattended,
// on the branch production is built from, which is why every one of these tests
// is about a way it could do damage rather than about the happy path.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newRevertFixture builds a bare origin on main with a base commit and a
// "release" commit on top, plus the mirror clone the rollback runs in.
func newRevertFixture(t *testing.T) (root, origin, releaseSHA string) {
	t.Helper()
	base := t.TempDir()
	origin = filepath.Join(base, "origin.git")
	gitRun(t, base, "init", "--bare", "--initial-branch=main", origin)

	author := filepath.Join(base, "author")
	gitRun(t, base, "clone", origin, author)
	if err := os.WriteFile(filepath.Join(author, "app.txt"), []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, author, "add", ".")
	gitRun(t, author, "commit", "-m", "base")
	gitRun(t, author, "push", "origin", "main")

	if err := os.WriteFile(filepath.Join(author, "app.txt"), []byte("v2 broken\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, author, "add", ".")
	gitRun(t, author, "commit", "-m", "T-7: ship v2")
	gitRun(t, author, "push", "origin", "main")
	releaseSHA = gitRun(t, author, "rev-parse", "HEAD")

	root = filepath.Join(base, "root")
	gitRun(t, base, "clone", origin, root)
	return root, origin, releaseSHA
}

func TestRevertCommitOnDefaultBranchUndoesTheReleaseAndPushes(t *testing.T) {
	root, origin, releaseSHA := newRevertFixture(t)

	revertSHA, err := NewClient().RevertCommitOnDefaultBranch(context.Background(), root, releaseSHA,
		"revert: roll back T-7 (ship v2)")
	if err != nil {
		t.Fatalf("RevertCommitOnDefaultBranch: %v", err)
	}
	if revertSHA == "" || revertSHA == releaseSHA {
		t.Fatalf("revert sha = %q, want the new commit that was actually created", revertSHA)
	}

	// The revert must be on ORIGIN — a rollback that only exists locally is a
	// production still running the bad release while the card says "rolled
	// back".
	pushed := gitRun(t, origin, "rev-parse", "main")
	if pushed != revertSHA {
		t.Fatalf("origin/main = %q, want the revert commit %q", pushed, revertSHA)
	}
	// And the content is actually back.
	content := gitRun(t, origin, "show", "main:app.txt")
	if strings.TrimSpace(content) != "v1" {
		t.Fatalf("app.txt on origin = %q, want the pre-release content", content)
	}
	// The commit message is read back from origin, never from root — root's
	// HEAD is untouched by a safe revert (see
	// TestRevertOnDefaultBranchNeverTouchesTheRootCheckout).
	subject := gitRun(t, origin, "log", "-1", "--pretty=%s")
	if !strings.Contains(subject, "roll back T-7") {
		t.Fatalf("commit subject = %q, want the rollback message", subject)
	}
}

// The defining safety property of this rollback: it must never run
// `checkout`/`reset`/`revert` against rootPath's own working tree or index,
// because that is the repository's root clone, which the desktop app and a
// human may have uncommitted work sitting in. The old implementation did
// exactly that (`git checkout` + `git reset --hard` in rootPath) and this
// test is what replaced it.
func TestRevertOnDefaultBranchNeverTouchesTheRootCheckout(t *testing.T) {
	root, origin, releaseSHA := newRevertFixture(t)

	// Uncommitted work in root: a modified tracked file and an untracked one.
	if err := os.WriteFile(filepath.Join(root, "app.txt"), []byte("local edit, never committed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "scratch.txt"), []byte("untracked\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	headBefore := gitRun(t, root, "rev-parse", "HEAD")
	branchBefore := gitRun(t, root, "rev-parse", "--abbrev-ref", "HEAD")
	statusBefore := gitRun(t, root, "status", "--porcelain")

	revertSHA, err := NewClient().RevertOnDefaultBranch(context.Background(), root, []string{releaseSHA}, "revert: roll back T-7")
	if err != nil {
		t.Fatalf("RevertOnDefaultBranch: %v", err)
	}

	// The rollback still happened — on origin.
	if pushed := gitRun(t, origin, "rev-parse", "main"); pushed != revertSHA {
		t.Fatalf("origin/main = %q, want the revert commit %q", pushed, revertSHA)
	}

	// root is exactly as it was: same HEAD, same branch, same uncommitted
	// changes, still present and unmodified.
	if head := gitRun(t, root, "rev-parse", "HEAD"); head != headBefore {
		t.Fatalf("root HEAD moved from %q to %q — the root checkout was touched", headBefore, head)
	}
	if branch := gitRun(t, root, "rev-parse", "--abbrev-ref", "HEAD"); branch != branchBefore {
		t.Fatalf("root branch moved from %q to %q", branchBefore, branch)
	}
	if status := gitRun(t, root, "status", "--porcelain"); status != statusBefore {
		t.Fatalf("root's uncommitted state changed:\nbefore:\n%s\nafter:\n%s", statusBefore, status)
	}
	edited, err := os.ReadFile(filepath.Join(root, "app.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(edited)) != "local edit, never committed" {
		t.Fatalf("root's uncommitted edit to app.txt was overwritten: %q", edited)
	}
	if _, err := os.Stat(filepath.Join(root, "scratch.txt")); err != nil {
		t.Fatalf("root's untracked file was removed: %v", err)
	}
}

// The detached worktree used to perform the revert must not linger — a stray
// worktree registration would eventually make `git worktree add` collide or
// leak temp directories across every rollback.
func TestRevertOnDefaultBranchLeavesNoWorktreeRegistered(t *testing.T) {
	root, _, releaseSHA := newRevertFixture(t)

	if _, err := NewClient().RevertOnDefaultBranch(context.Background(), root, []string{releaseSHA}, "revert"); err != nil {
		t.Fatalf("RevertOnDefaultBranch: %v", err)
	}

	list := gitRun(t, root, "worktree", "list", "--porcelain")
	if strings.Count(list, "worktree ") != 1 {
		t.Fatalf("worktree list after a successful revert:\n%s\nwant only the root checkout", list)
	}
}

// Multiple commits are reverted in exactly the order given — the caller
// (newest-first for a release rollback) controls that, not this method.
func TestRevertOnDefaultBranchRevertsMultipleCommitsInGivenOrder(t *testing.T) {
	base := t.TempDir()
	origin := filepath.Join(base, "origin.git")
	gitRun(t, base, "init", "--bare", "--initial-branch=main", origin)

	author := filepath.Join(base, "author")
	gitRun(t, base, "clone", origin, author)
	write := func(content string) {
		if err := os.WriteFile(filepath.Join(author, "app.txt"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("v1\n")
	gitRun(t, author, "add", ".")
	gitRun(t, author, "commit", "-m", "base")
	gitRun(t, author, "push", "origin", "main")

	write("v2\n")
	gitRun(t, author, "add", ".")
	gitRun(t, author, "commit", "-m", "T-1")
	gitRun(t, author, "push", "origin", "main")
	sha1 := gitRun(t, author, "rev-parse", "HEAD")

	write("v3\n")
	gitRun(t, author, "add", ".")
	gitRun(t, author, "commit", "-m", "T-2")
	gitRun(t, author, "push", "origin", "main")
	sha2 := gitRun(t, author, "rev-parse", "HEAD")

	root := filepath.Join(base, "root")
	gitRun(t, base, "clone", origin, root)

	revertSHA, err := NewClient().RevertOnDefaultBranch(context.Background(), root, []string{sha2, sha1}, "revert: T-2, T-1")
	if err != nil {
		t.Fatalf("RevertOnDefaultBranch: %v", err)
	}
	if revertSHA == "" {
		t.Fatal("no revert commit sha returned")
	}
	content := gitRun(t, origin, "show", "main:app.txt")
	if strings.TrimSpace(content) != "v1" {
		t.Fatalf("app.txt on origin = %q, want both commits undone back to v1", content)
	}
}

// Nothing is force-pushed and nothing is rewritten: the release commit is still
// in origin's history, with the revert on top of it. A rollback that rewrote
// the branch would destroy every commit that landed after the one being undone.
func TestRevertCommitOnDefaultBranchDoesNotRewriteHistory(t *testing.T) {
	root, origin, releaseSHA := newRevertFixture(t)

	if _, err := NewClient().RevertCommitOnDefaultBranch(context.Background(), root, releaseSHA, "revert"); err != nil {
		t.Fatalf("RevertCommitOnDefaultBranch: %v", err)
	}

	history := gitRun(t, origin, "log", "--pretty=%H", "main")
	if !strings.Contains(history, releaseSHA) {
		t.Fatalf("the reverted commit is gone from origin's history — this was a rewrite, not a revert:\n%s", history)
	}
	if count := len(strings.Fields(history)); count != 3 {
		t.Fatalf("origin/main has %d commits, want 3 (base, release, revert)", count)
	}
}

// A revert that conflicts means later commits touched the same lines. Guessing
// which side wins, unattended, on the branch production builds from, is not
// something to do — the tree is left clean and the caller is told.
func TestRevertCommitOnDefaultBranchAbortsOnConflict(t *testing.T) {
	root, origin, releaseSHA := newRevertFixture(t)

	// Somebody else edits the same line after the release.
	other := filepath.Join(t.TempDir(), "other")
	gitRun(t, filepath.Dir(other), "clone", origin, other)
	if err := os.WriteFile(filepath.Join(other, "app.txt"), []byte("v3 by somebody else\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, other, "add", ".")
	gitRun(t, other, "commit", "-m", "v3")
	gitRun(t, other, "push", "origin", "main")

	_, err := NewClient().RevertCommitOnDefaultBranch(context.Background(), root, releaseSHA, "revert")
	if err == nil {
		t.Fatal("a conflicting revert must fail loudly, not resolve itself")
	}
	if !strings.Contains(err.Error(), "git revert") {
		t.Fatalf("err = %v, want it to name the failing operation", err)
	}
	if !strings.Contains(err.Error(), "app.txt") {
		t.Fatalf("err = %v, want it to name the conflicting path (app.txt)", err)
	}
	// Nothing half-applied in root, which the conflicting revert never even
	// touched — it ran in a detached worktree.
	if status := gitRun(t, root, "status", "--porcelain"); status != "" {
		t.Fatalf("the working tree was left dirty after a failed revert:\n%s", status)
	}
	// And origin is untouched.
	if head := gitRun(t, origin, "rev-parse", "main"); head != gitRun(t, other, "rev-parse", "HEAD") {
		t.Fatalf("origin/main moved despite the revert failing")
	}
	// The failed attempt's detached worktree must not linger either.
	list := gitRun(t, root, "worktree", "list", "--porcelain")
	if strings.Count(list, "worktree ") != 1 {
		t.Fatalf("worktree list after a failed revert:\n%s\nwant only the root checkout", list)
	}
}

// A ref that starts with '-' is an argument, not a commit. git parses its own
// argv, so there is no shell needed for this to be injection.
func TestRevertCommitOnDefaultBranchRefusesAnOptionLikeRef(t *testing.T) {
	root, _, _ := newRevertFixture(t)

	if _, err := NewClient().RevertCommitOnDefaultBranch(context.Background(), root, "--help", "revert"); err == nil {
		t.Fatal("a ref beginning with '-' must be refused before it reaches git")
	}
	if _, err := NewClient().RevertCommitOnDefaultBranch(context.Background(), root, "  ", "revert"); err == nil {
		t.Fatal("an empty ref must be refused")
	}
}

// A SHA that is not in this repository is refused before anything is committed.
func TestRevertCommitOnDefaultBranchRefusesAnUnknownCommit(t *testing.T) {
	root, _, _ := newRevertFixture(t)

	_, err := NewClient().RevertCommitOnDefaultBranch(context.Background(), root,
		"0123456789012345678901234567890123456789", "revert")
	if err == nil {
		t.Fatal("an unknown commit must be refused")
	}
	if !strings.Contains(err.Error(), "not a commit") {
		t.Fatalf("err = %v, want it to say the commit does not exist", err)
	}
}

// Every sha in the batch is validated before anything runs — a bad second sha
// must not leave a worktree behind from reverting the first.
func TestRevertOnDefaultBranchRefusesTheWholeBatchOnOneBadSHA(t *testing.T) {
	root, _, releaseSHA := newRevertFixture(t)

	_, err := NewClient().RevertOnDefaultBranch(context.Background(), root, []string{releaseSHA, "not-hex-!!"}, "revert")
	if err == nil {
		t.Fatal("a batch with an invalid sha must be refused entirely")
	}

	list := gitRun(t, root, "worktree", "list", "--porcelain")
	if strings.Count(list, "worktree ") != 1 {
		t.Fatalf("worktree list after a refused batch:\n%s\nwant only the root checkout — nothing should have started", list)
	}
}

func TestRevertOnDefaultBranchRefusesNoCommits(t *testing.T) {
	root, _, _ := newRevertFixture(t)

	if _, err := NewClient().RevertOnDefaultBranch(context.Background(), root, nil, "revert"); err == nil {
		t.Fatal("an empty commit list must be refused")
	}
}

// §H5: the caller's order is not trusted — RevertOnDefaultBranch reorders by
// the actual git history itself. Reverting T-1 (v2) while T-2 (v3) still sits
// on top of it is a conflict (the file is at v3, not the v2 the T-1 revert
// expects); passing them oldest-first — the WRONG order — must still
// succeed, because the method reorders them newest-first regardless of what
// was given.
func TestRevertOnDefaultBranchOrdersCommitsByTopologyRegardlessOfInputOrder(t *testing.T) {
	base := t.TempDir()
	origin := filepath.Join(base, "origin.git")
	gitRun(t, base, "init", "--bare", "--initial-branch=main", origin)

	author := filepath.Join(base, "author")
	gitRun(t, base, "clone", origin, author)
	write := func(content string) {
		if err := os.WriteFile(filepath.Join(author, "app.txt"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("v1\n")
	gitRun(t, author, "add", ".")
	gitRun(t, author, "commit", "-m", "base")
	gitRun(t, author, "push", "origin", "main")

	write("v2\n")
	gitRun(t, author, "add", ".")
	gitRun(t, author, "commit", "-m", "T-1")
	gitRun(t, author, "push", "origin", "main")
	sha1 := gitRun(t, author, "rev-parse", "HEAD")

	write("v3\n")
	gitRun(t, author, "add", ".")
	gitRun(t, author, "commit", "-m", "T-2")
	gitRun(t, author, "push", "origin", "main")
	sha2 := gitRun(t, author, "rev-parse", "HEAD")

	root := filepath.Join(base, "root")
	gitRun(t, base, "clone", origin, root)

	// Oldest-first: the wrong order for a naive sequential revert.
	revertSHA, err := NewClient().RevertOnDefaultBranch(context.Background(), root, []string{sha1, sha2}, "revert: T-1, T-2 (given out of order)")
	if err != nil {
		t.Fatalf("RevertOnDefaultBranch with commits given oldest-first: %v", err)
	}
	if revertSHA == "" {
		t.Fatal("no revert commit sha returned")
	}
	content := gitRun(t, origin, "show", "main:app.txt")
	if strings.TrimSpace(content) != "v1" {
		t.Fatalf("app.txt on origin = %q, want both commits undone back to v1 even though they were given oldest-first", content)
	}
}
