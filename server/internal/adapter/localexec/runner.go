// Package localexec runs a batch release's local build-and-publish command:
// never through a shell (argv is exec'd directly), always in a detached git
// worktree of the release commit (the repository's own root checkout is never
// touched), and killed by process group on timeout so a command that spawns
// children cannot outlive it.
package localexec

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/release"
)

const defaultTimeout = 60 * time.Minute

const tailLines = 120

// Runner tracks every run it has started so Close can kill their process
// groups and remove their worktrees on server shutdown instead of leaving
// them to the 60-minute default timeout (or forever, for a run with no
// timeout set) after the process that owned them is gone.
type Runner struct {
	mu     sync.Mutex
	active map[*run]struct{}
	closed bool
}

type run struct {
	cancel context.CancelFunc
}

func NewRunner() *Runner { return &Runner{active: make(map[*run]struct{})} }

var _ release.LocalRunner = (*Runner)(nil)

// Close cancels every run still in flight — which kills its process group via
// cmd.Cancel exactly as a timeout would — and waits for each one's goroutine
// to finish closing its log file and removing its worktree before returning,
// so a shutdown that calls Close does not race the process it is trying to
// stop against the data directory being torn down around it. Idempotent and
// safe to call with no runs active.
func (r *Runner) Close() error {
	r.mu.Lock()
	r.closed = true
	cancels := make([]context.CancelFunc, 0, len(r.active))
	for a := range r.active {
		cancels = append(cancels, a.cancel)
	}
	r.mu.Unlock()

	for _, cancel := range cancels {
		cancel()
	}

	for {
		r.mu.Lock()
		remaining := len(r.active)
		r.mu.Unlock()
		if remaining == 0 {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Start prepares a detached worktree at spec.CommitSHA, starts spec.Argv in
// it with output tee'd to spec.LogPath, and returns once the process has
// started (or failed to). done runs later, from a goroutine, once the
// process exits, is killed on timeout, is killed by Close, or could not be
// waited on.
func (r *Runner) Start(ctx context.Context, spec release.LocalRunSpec, done func(exitCode int, tail string, err error)) error {
	if len(spec.Argv) == 0 {
		return fmt.Errorf("localexec: no command given")
	}
	r.mu.Lock()
	closed := r.closed
	r.mu.Unlock()
	if closed {
		return fmt.Errorf("localexec: the runner is shutting down")
	}
	if err := os.MkdirAll(filepath.Dir(spec.LogPath), 0o755); err != nil {
		return fmt.Errorf("localexec: creating the log directory: %w", err)
	}

	worktree, cleanup, err := addDetachedWorktree(context.WithoutCancel(ctx), spec.RootPath, spec.CommitSHA)
	if err != nil {
		return fmt.Errorf("localexec: preparing the detached worktree: %w", err)
	}

	logFile, err := os.Create(spec.LogPath)
	if err != nil {
		cleanup()
		return fmt.Errorf("localexec: creating the log file: %w", err)
	}

	timeout := spec.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)

	cmd := exec.CommandContext(runCtx, spec.Argv[0], spec.Argv[1:]...)
	cmd.Dir = worktree
	cmd.Env = spec.Env
	setProcessGroup(cmd)
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			killProcessGroup(cmd.Process.Pid)
		}
		return nil
	}
	cmd.WaitDelay = 5 * time.Second

	tail := newTailBuffer(tailLines)
	cmd.Stdout = io.MultiWriter(logFile, tail)
	cmd.Stderr = io.MultiWriter(logFile, tail)

	if err := cmd.Start(); err != nil {
		cancel()
		_ = logFile.Close()
		cleanup()
		return fmt.Errorf("localexec: starting the command: %w", err)
	}

	self := &run{cancel: cancel}
	r.mu.Lock()
	r.active[self] = struct{}{}
	r.mu.Unlock()

	go func() {
		defer cancel()
		defer func() {
			r.mu.Lock()
			delete(r.active, self)
			r.mu.Unlock()
		}()

		waitErr := cmd.Wait()
		timedOut := runCtx.Err() != nil

		exitCode := 0
		var runErr error
		switch {
		case timedOut:
			exitCode = -1
			runErr = fmt.Errorf("the command timed out after %s and was killed", timeout)
		case waitErr == nil:
		default:
			if exitErr, ok := waitErr.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			} else {
				exitCode = -1
				runErr = waitErr
			}
		}

		// Close the log and remove the worktree BEFORE calling done: the
		// caller may act on the release the moment it is notified (a test
		// tearing down its temp dir, the release service reading the log
		// path), and nothing here needs the process alive to finish.
		if cerr := logFile.Close(); cerr != nil {
			log.Warn().Err(cerr).Str("path", spec.LogPath).Msg("localexec: closing the log file failed")
		}
		cleanup()

		done(exitCode, tail.String(), runErr)
	}()
	return nil
}

func addDetachedWorktree(ctx context.Context, rootPath, sha string) (worktree string, cleanup func(), err error) {
	parent, err := os.MkdirTemp("", "tasktrooper-local-release-")
	if err != nil {
		return "", nil, fmt.Errorf("creating the worktree directory: %w", err)
	}
	worktree = filepath.Join(parent, "wt")

	cmd := exec.CommandContext(ctx, "git", "worktree", "add", "--detach", worktree, sha)
	cmd.Dir = rootPath
	if out, err := cmd.CombinedOutput(); err != nil {
		_ = os.RemoveAll(parent)
		return "", nil, fmt.Errorf("git worktree add: %w (%s)", err, strings.TrimSpace(string(out)))
	}

	cleanup = func() {
		rmCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		rmCmd := exec.CommandContext(rmCtx, "git", "worktree", "remove", "--force", worktree)
		rmCmd.Dir = rootPath
		if out, err := rmCmd.CombinedOutput(); err != nil {
			log.Warn().Err(err).Str("output", strings.TrimSpace(string(out))).Msg("localexec: removing the detached worktree failed")
		}
		pruneCmd := exec.CommandContext(rmCtx, "git", "worktree", "prune")
		pruneCmd.Dir = rootPath
		if out, err := pruneCmd.CombinedOutput(); err != nil {
			log.Warn().Err(err).Str("output", strings.TrimSpace(string(out))).Msg("localexec: pruning worktrees failed")
		}
		_ = os.RemoveAll(parent)
	}
	return worktree, cleanup, nil
}

// tailBuffer keeps only the last max lines written to it — an io.Writer so it
// can sit alongside the log file in an io.MultiWriter without buffering the
// whole (potentially long) run in memory.
type tailBuffer struct {
	mu    sync.Mutex
	lines []string
	cur   strings.Builder
	max   int
}

func newTailBuffer(max int) *tailBuffer { return &tailBuffer{max: max} }

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, b := range p {
		if b == '\n' {
			t.push(t.cur.String())
			t.cur.Reset()
		} else {
			t.cur.WriteByte(b)
		}
	}
	return len(p), nil
}

func (t *tailBuffer) push(line string) {
	t.lines = append(t.lines, line)
	if len(t.lines) > t.max {
		t.lines = t.lines[len(t.lines)-t.max:]
	}
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	lines := t.lines
	if t.cur.Len() > 0 {
		lines = append(append([]string{}, lines...), t.cur.String())
		if len(lines) > t.max {
			lines = lines[len(lines)-t.max:]
		}
	}
	return strings.Join(lines, "\n")
}
