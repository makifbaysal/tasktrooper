// Package localexec runs a batch release's local build-and-publish command:
// never through a shell (argv is exec'd directly), always in a detached git
// worktree of the release commit (the repository's own root checkout is never
// touched), and killed by process group on timeout so a command that spawns
// children cannot outlive it.
package localexec

import (
	"context"
	"errors"
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

// ErrInterrupted marks a run's done callback as having been cut short by
// Close (server shutdown) rather than by its own timeout or the command's own
// exit — CompleteLocalRun (release/local.go) maps it to a distinct "check
// what was published before deploying again" message instead of a plain
// timeout that would misreport why the run stopped.
var ErrInterrupted = errors.New("localexec: interrupted by shutdown")

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
// stop against the data directory being torn down around it. It stops
// waiting as soon as ctx is done (the runtime passes a bounded timeout so
// shutdown itself cannot hang forever on a run that will not die). Idempotent
// and safe to call with no runs active.
func (r *Runner) Close(ctx context.Context) error {
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
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// Start prepares a detached worktree at spec.CommitSHA, starts spec.Argv in
// it with output tee'd to spec.LogPath, and returns once the process has
// started (or failed to). done runs later, from a goroutine, once the
// process exits, is killed on timeout, is killed by Close, or could not be
// waited on. The run is registered (and the closed-check made) under the
// same lock, before the slow worktree add: a Close racing a Start that has
// not reached the worktree add yet must still see and cancel it, or Close
// could return believing nothing is in flight while this call keeps going.
func (r *Runner) Start(ctx context.Context, spec release.LocalRunSpec, done func(exitCode int, tail string, err error)) error {
	if len(spec.Argv) == 0 {
		return fmt.Errorf("localexec: no command given")
	}

	timeout := spec.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)

	self := &run{cancel: cancel}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		cancel()
		return fmt.Errorf("localexec: the runner is shutting down")
	}
	r.active[self] = struct{}{}
	r.mu.Unlock()

	unregister := func() {
		r.mu.Lock()
		delete(r.active, self)
		r.mu.Unlock()
	}

	if err := os.MkdirAll(filepath.Dir(spec.LogPath), 0o755); err != nil {
		cancel()
		unregister()
		return fmt.Errorf("localexec: creating the log directory: %w", err)
	}

	worktree, cleanup, err := addDetachedWorktree(context.WithoutCancel(ctx), spec.RootPath, spec.CommitSHA)
	if err != nil {
		cancel()
		unregister()
		return fmt.Errorf("localexec: preparing the detached worktree: %w", err)
	}

	logFile, err := os.Create(spec.LogPath)
	if err != nil {
		cancel()
		unregister()
		cleanup()
		return fmt.Errorf("localexec: creating the log file: %w", err)
	}

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
		unregister()
		_ = logFile.Close()
		cleanup()
		return fmt.Errorf("localexec: starting the command: %w", err)
	}

	go func() {
		defer cancel()
		defer unregister()

		waitErr := cmd.Wait()

		exitCode := 0
		var runErr error
		switch {
		case errors.Is(runCtx.Err(), context.DeadlineExceeded):
			exitCode = -1
			runErr = fmt.Errorf("the command timed out after %s and was killed", timeout)
		case errors.Is(runCtx.Err(), context.Canceled):
			exitCode = -1
			runErr = fmt.Errorf("%w: killed while running", ErrInterrupted)
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

// addDetachedWorktree is a var (not a plain func) so a test can wrap it with
// a delay to exercise the register-before-the-slow-add ordering in Start
// that lets Close never miss an in-flight run.
var addDetachedWorktree = func(ctx context.Context, rootPath, sha string) (worktree string, cleanup func(), err error) {
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
