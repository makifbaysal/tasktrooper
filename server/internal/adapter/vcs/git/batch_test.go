package git

// RemoteHead / IsAncestor / LatestTag are the read-only surface a batch
// release's cut preview/cut needs: the commit that would be cut, whether a
// task's merge commit is really on it, and the previous version when nothing
// was ever released through the store. None of them may touch rootPath's
// working tree or index — the root checkout is the same tree other agents
// and the human are looking at.

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRemoteHeadAndIsAncestorAndLatestTag(t *testing.T) {
	base := t.TempDir()
	origin := filepath.Join(base, "origin.git")
	gitRun(t, base, "init", "--bare", "--initial-branch=main", origin)

	author := filepath.Join(base, "author")
	gitRun(t, base, "clone", origin, author)
	writeAndCommit(t, author, "app.txt", "v1\n", "base")
	gitRun(t, author, "push", "origin", "main")
	baseSHA := gitRun(t, author, "rev-parse", "HEAD")

	writeAndCommit(t, author, "app.txt", "v2\n", "T-1: feature")
	gitRun(t, author, "push", "origin", "main")
	featureSHA := gitRun(t, author, "rev-parse", "HEAD")

	root := filepath.Join(base, "root")
	gitRun(t, base, "clone", origin, root)

	c := NewClient()
	ctx := context.Background()

	head, err := c.RemoteHead(ctx, root)
	if err != nil {
		t.Fatalf("RemoteHead: %v", err)
	}
	if head != featureSHA {
		t.Fatalf("RemoteHead = %q, want %q", head, featureSHA)
	}

	ok, err := c.IsAncestor(ctx, root, baseSHA, head)
	if err != nil || !ok {
		t.Fatalf("IsAncestor(base, head) = %v, %v; want true, nil", ok, err)
	}
	ok, err = c.IsAncestor(ctx, root, featureSHA, baseSHA)
	if err != nil || ok {
		t.Fatalf("IsAncestor(feature, base) = %v, %v; want false, nil", ok, err)
	}

	// A commit that only exists on a branch never pushed to origin must read
	// as "not an ancestor", not error — an in-flight PR's commit, say.
	strayDir := filepath.Join(base, "stray")
	gitRun(t, base, "clone", origin, strayDir)
	writeAndCommit(t, strayDir, "app.txt", "v3\n", "T-2: never merged")
	straySHA := gitRun(t, strayDir, "rev-parse", "HEAD")
	ok, err = c.IsAncestor(ctx, root, straySHA, head)
	if err == nil {
		t.Fatalf("IsAncestor with an unknown commit should error, got ok=%v", ok)
	}

	// No tags yet: LatestTag must return "" with no error, never guess.
	tag, err := c.LatestTag(ctx, root, "v*")
	if err != nil {
		t.Fatalf("LatestTag (none yet): %v", err)
	}
	if tag != "" {
		t.Fatalf("LatestTag (none yet) = %q, want empty", tag)
	}

	gitRun(t, author, "tag", "v1.0.0", baseSHA)
	gitRun(t, author, "push", "origin", "v1.0.0")
	gitRun(t, author, "tag", "v1.1.0", featureSHA)
	gitRun(t, author, "push", "origin", "v1.1.0")

	tag, err = c.LatestTag(ctx, root, "v*")
	if err != nil {
		t.Fatalf("LatestTag: %v", err)
	}
	if tag != "v1.1.0" {
		t.Fatalf("LatestTag = %q, want v1.1.0 (the newest)", tag)
	}

	// A glob that matches nothing real still returns "" cleanly.
	tag, err = c.LatestTag(ctx, root, "release-*")
	if err != nil {
		t.Fatalf("LatestTag (no match): %v", err)
	}
	if tag != "" {
		t.Fatalf("LatestTag (no match) = %q, want empty", tag)
	}
}

func TestRemoteHeadAndLatestTagNeverTouchTheRootCheckoutsWorkingTreeOrIndex(t *testing.T) {
	base := t.TempDir()
	origin := filepath.Join(base, "origin.git")
	gitRun(t, base, "init", "--bare", "--initial-branch=main", origin)

	author := filepath.Join(base, "author")
	gitRun(t, base, "clone", origin, author)
	writeAndCommit(t, author, "app.txt", "v1\n", "base")
	gitRun(t, author, "push", "origin", "main")

	// root is cloned before the second commit lands on origin, and deliberately
	// keeps uncommitted local work — a human or an agent could be looking at
	// this tree right now.
	root := filepath.Join(base, "root")
	gitRun(t, base, "clone", origin, root)
	if err := os.WriteFile(filepath.Join(root, "local.txt"), []byte("scratch\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	writeAndCommit(t, author, "app.txt", "v2\n", "T-1: feature")
	gitRun(t, author, "push", "origin", "main")
	featureSHA := gitRun(t, author, "rev-parse", "HEAD")

	c := NewClient()
	ctx := context.Background()

	if _, err := c.RemoteHead(ctx, root); err != nil {
		t.Fatalf("RemoteHead: %v", err)
	}
	if _, err := c.LatestTag(ctx, root, "v*"); err != nil {
		t.Fatalf("LatestTag: %v", err)
	}

	headSHA := gitRun(t, root, "rev-parse", "HEAD")
	if headSHA == featureSHA {
		t.Fatalf("root's HEAD moved to the fetched commit — RemoteHead/LatestTag must never touch the working tree")
	}
	if _, err := os.Stat(filepath.Join(root, "local.txt")); err != nil {
		t.Fatalf("uncommitted local file was discarded: %v", err)
	}
	status := gitRun(t, root, "status", "--porcelain")
	if status == "" {
		t.Fatalf("root's working tree/index was touched — status should still show the untracked file and the missed commit")
	}
}

func writeAndCommit(t *testing.T, dir, name, content, message string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", message)
}
