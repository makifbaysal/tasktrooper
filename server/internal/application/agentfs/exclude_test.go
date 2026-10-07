package agentfs

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitIn(t *testing.T, dir string, args ...string) string {
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

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
}

func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitIn(t, dir, "init", "--initial-branch=main")
	gitIn(t, dir, "commit", "--allow-empty", "-m", "base")
	return dir
}

// gitExcludePath is where git itself reads info/exclude for the checkout.
func gitExcludePath(t *testing.T, dir string) string {
	t.Helper()
	p := filepath.FromSlash(gitIn(t, dir, "rev-parse", "--path-format=absolute", "--git-path", "info/exclude"))
	resolved, err := filepath.EvalSymlinks(filepath.Dir(p))
	if err != nil {
		t.Fatalf("resolve %s: %v", p, err)
	}
	return filepath.Join(resolved, filepath.Base(p))
}

func requireExcluded(t *testing.T, checkout string) {
	t.Helper()
	body, err := os.ReadFile(gitExcludePath(t, checkout))
	if err != nil {
		t.Fatalf("read the exclude file git uses: %v", err)
	}
	for _, p := range excludePatterns {
		if !strings.Contains(string(body), p) {
			t.Errorf("pattern %q missing from the exclude file git reads:\n%s", p, body)
		}
	}
	probe := filepath.Join(checkout, filepath.FromSlash(claudeAgentsDir), ttPrefix+"probe.md")
	if err := os.MkdirAll(filepath.Dir(probe), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(probe, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if status := gitIn(t, checkout, "status", "--porcelain", "--untracked-files=all"); status != "" {
		t.Errorf("materialised file still shows in git status:\n%s", status)
	}
}

func TestExcludeInALinkedWorktreeWritesTheSharedExcludeFile(t *testing.T) {
	requireGit(t)
	main := newRepo(t)
	worktree := filepath.Join(t.TempDir(), "wt")
	gitIn(t, main, "worktree", "add", "--detach", worktree)

	if err := Exclude(worktree); err != nil {
		t.Fatalf("Exclude in a worktree whose .git is a file: %v", err)
	}
	requireExcluded(t, worktree)
}

func TestExcludeInASubmoduleWritesItsOwnExcludeFile(t *testing.T) {
	requireGit(t)
	sub := newRepo(t)
	super := newRepo(t)
	gitIn(t, super, "-c", "protocol.file.allow=always", "submodule", "add", sub, "lib")
	checkout := filepath.Join(super, "lib")

	if err := Exclude(checkout); err != nil {
		t.Fatalf("Exclude in a submodule whose .git is a file: %v", err)
	}
	requireExcluded(t, checkout)
}

func TestExcludeSkipsAGitFileWhoseRepositoryIsGone(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".git"), []byte("gitdir: "+filepath.Join(root, "missing")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Exclude(root); err != nil {
		t.Fatalf("Exclude with a dangling gitdir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "missing")); !os.IsNotExist(err) {
		t.Fatalf("Exclude created the dangling gitdir: %v", err)
	}
}

func TestExcludeRefusesAGitFileWithoutAGitdirLine(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".git"), []byte("not a pointer\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Exclude(root); err == nil {
		t.Fatal("a malformed .git file was accepted")
	}
}
