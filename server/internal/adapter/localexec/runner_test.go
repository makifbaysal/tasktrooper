package localexec

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/application/release"
)

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

// newRepoFixture is a plain (non-bare) repository with one commit — a local
// batch release's RootPath is the repository the desktop/mobile checkout
// already lives in, not a bare origin.
func newRepoFixture(t *testing.T) (root, headSHA string) {
	t.Helper()
	root = t.TempDir()
	gitRun(t, root, "init", "--initial-branch=main")
	if err := os.WriteFile(filepath.Join(root, "app.txt"), []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "-m", "base")
	return root, gitRun(t, root, "rev-parse", "HEAD")
}

type runResult struct {
	exitCode int
	tail     string
	err      error
}

func waitForRun(t *testing.T) (chan runResult, func(exitCode int, tail string, err error)) {
	t.Helper()
	ch := make(chan runResult, 1)
	var once sync.Once
	return ch, func(exitCode int, tail string, err error) {
		once.Do(func() { ch <- runResult{exitCode, tail, err} })
	}
}

func TestRunnerRunsTheCommandInADetachedWorktreeAndCallsDoneOnSuccess(t *testing.T) {
	root, headSHA := newRepoFixture(t)
	logPath := filepath.Join(t.TempDir(), "release.log")

	scriptDir := t.TempDir()
	script := filepath.Join(scriptDir, "release.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho building in \"$PWD\"\ncat app.txt\necho \"version=$RELEASE_VERSION\"\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	r := NewRunner()
	ch, done := waitForRun(t)
	err := r.Start(context.Background(), release.LocalRunSpec{
		RootPath:  root,
		CommitSHA: headSHA,
		Argv:      []string{script},
		Env:       append(os.Environ(), "RELEASE_VERSION=1.0.0"),
		LogPath:   logPath,
		Timeout:   30 * time.Second,
	}, done)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	select {
	case res := <-ch:
		if res.err != nil {
			t.Fatalf("done callback err = %v, want nil", res.err)
		}
		if res.exitCode != 0 {
			t.Fatalf("exit code = %d, want 0", res.exitCode)
		}
		if !strings.Contains(res.tail, "v1") {
			t.Fatalf("tail = %q, want it to contain the worktree's file content", res.tail)
		}
		if !strings.Contains(res.tail, "version=1.0.0") {
			t.Fatalf("tail = %q, want the RELEASE_VERSION env var visible", res.tail)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("done was never called")
	}

	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("reading the log file: %v", err)
	}
	if !strings.Contains(string(logBytes), "v1") {
		t.Fatalf("log file = %q, want the command's output", logBytes)
	}

	// The root checkout's own working tree/branch must be untouched by the run.
	branch := gitRun(t, root, "rev-parse", "--abbrev-ref", "HEAD")
	if branch != "main" {
		t.Fatalf("root checkout branch = %q, want main (untouched)", branch)
	}
}

func TestRunnerReportsANonZeroExit(t *testing.T) {
	root, headSHA := newRepoFixture(t)
	logPath := filepath.Join(t.TempDir(), "release.log")
	script := filepath.Join(t.TempDir(), "fail.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho failing >&2\nexit 3\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	r := NewRunner()
	ch, done := waitForRun(t)
	if err := r.Start(context.Background(), release.LocalRunSpec{
		RootPath: root, CommitSHA: headSHA, Argv: []string{script}, LogPath: logPath, Timeout: 30 * time.Second,
	}, done); err != nil {
		t.Fatalf("Start: %v", err)
	}

	select {
	case res := <-ch:
		if res.exitCode != 3 {
			t.Fatalf("exit code = %d, want 3", res.exitCode)
		}
		if !strings.Contains(res.tail, "failing") {
			t.Fatalf("tail = %q, want the script's stderr", res.tail)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("done was never called")
	}
}

func TestRunnerKillsTheProcessGroupOnTimeout(t *testing.T) {
	root, headSHA := newRepoFixture(t)
	logPath := filepath.Join(t.TempDir(), "release.log")
	script := filepath.Join(t.TempDir(), "hang.sh")
	// A child that outlives its parent's own sleep — only killing the whole
	// process GROUP reaps it; killing just the shell would leave it running.
	if err := os.WriteFile(script, []byte("#!/bin/sh\nsleep 30 &\nwait\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	r := NewRunner()
	ch, done := waitForRun(t)
	start := time.Now()
	if err := r.Start(context.Background(), release.LocalRunSpec{
		RootPath: root, CommitSHA: headSHA, Argv: []string{script}, LogPath: logPath, Timeout: 500 * time.Millisecond,
	}, done); err != nil {
		t.Fatalf("Start: %v", err)
	}

	select {
	case res := <-ch:
		if res.err == nil {
			t.Fatalf("expected a timeout error, got nil (exit code %d)", res.exitCode)
		}
		if elapsed := time.Since(start); elapsed > 5*time.Second {
			t.Fatalf("done fired after %s, want it to fire close to the 500ms timeout", elapsed)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("done was never called — the process group was not killed")
	}
}

func TestRunnerStartFailsOnAnEmptyArgv(t *testing.T) {
	root, headSHA := newRepoFixture(t)
	r := NewRunner()
	err := r.Start(context.Background(), release.LocalRunSpec{
		RootPath: root, CommitSHA: headSHA, LogPath: filepath.Join(t.TempDir(), "x.log"),
	}, func(int, string, error) {})
	if err == nil {
		t.Fatal("expected an error for an empty argv")
	}
}

// L6: before Close existed, a still-running local build had nothing to stop
// it on server shutdown — it (and its detached worktree) outlived the
// process, or the data directory got torn down from under it. Close must
// kill the process group promptly (not wait out the run's own long timeout)
// and remove the worktree before returning.
func TestRunnerCloseKillsRunningProcessesAndRemovesTheirWorktrees(t *testing.T) {
	root, headSHA := newRepoFixture(t)
	logPath := filepath.Join(t.TempDir(), "release.log")
	script := filepath.Join(t.TempDir(), "hang.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nsleep 300 &\nwait\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	r := NewRunner()
	ch, done := waitForRun(t)
	if err := r.Start(context.Background(), release.LocalRunSpec{
		// A long timeout — Close must not depend on it firing.
		RootPath: root, CommitSHA: headSHA, Argv: []string{script}, LogPath: logPath, Timeout: 10 * time.Minute,
	}, done); err != nil {
		t.Fatalf("Start: %v", err)
	}

	before := countWorktrees(t, root)

	closeDone := make(chan struct{})
	start := time.Now()
	go func() {
		if err := r.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
		close(closeDone)
	}()

	select {
	case <-closeDone:
		if elapsed := time.Since(start); elapsed > 5*time.Second {
			t.Fatalf("Close took %s, want it to kill the run promptly rather than wait out its 10m timeout", elapsed)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Close never returned — the running process was not killed")
	}

	select {
	case res := <-ch:
		if res.err == nil {
			t.Fatalf("expected the killed run to report an error, exit code %d", res.exitCode)
		}
	case <-time.After(time.Second):
		t.Fatal("done was never called for the run Close killed")
	}

	if after := countWorktrees(t, root); after != before-1 {
		t.Fatalf("worktrees after Close = %d, want %d (the run's worktree removed)", after, before-1)
	}
}

// TestRunnerStartAfterCloseIsRefused guards the shutdown ordering: once Close
// has been called (server is going down), a late Start must not race it by
// launching a new detached worktree/process that Close will never know about.
func TestRunnerStartAfterCloseIsRefused(t *testing.T) {
	root, headSHA := newRepoFixture(t)
	r := NewRunner()
	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	err := r.Start(context.Background(), release.LocalRunSpec{
		RootPath: root, CommitSHA: headSHA, Argv: []string{"true"}, LogPath: filepath.Join(t.TempDir(), "x.log"),
	}, func(int, string, error) {})
	if err == nil {
		t.Fatal("expected Start after Close to be refused")
	}
}

func countWorktrees(t *testing.T, root string) int {
	t.Helper()
	out := gitRun(t, root, "worktree", "list", "--porcelain")
	n := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "worktree ") {
			n++
		}
	}
	return n
}
