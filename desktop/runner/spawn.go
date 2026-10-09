package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// One place that runs a short-lived helper binary and reaps it.
//
// `claude.run` has its own spawn (session.go) because it streams, and the two
// are genuinely different problems: that one forwards two pipes for minutes and
// must never stop reading them, this one waits for a command that answers in
// seconds and hands back what it said. What they share is the part that has
// cost bugs, and that part lives here so it cannot drift between callers:
//
//   - **Its own process group.** `git clone` starts a transport helper, `xcrun
//     simctl` talks to CoreSimulator, `adb` forks a server. Signalling the
//     parent alone leaves those behind.
//   - **Cancelled means the GROUP is killed**, SIGTERM then SIGKILL, through
//     `killProcessGroup` — which refuses to signal a process that has already
//     been reaped, because a recycled pid belongs to somebody else.
//   - **Both pipes are read to completion before Wait**, which is what os/exec
//     requires and what the group kill guarantees will happen.
//
// It was duplicated in workspace.go, and a security-critical spawn helper with
// two copies is a helper where one copy quietly loses its reaped guard.

// errStartFailed marks the case where the binary never ran at all — a path that
// is not there, a file that is not executable. It is worth telling apart from a
// non-zero exit: one is this Mac's configuration, the other is the command's
// own answer.
var errStartFailed = errors.New("the program could not be started")

// toolOutcome is what one invocation produced.
type toolOutcome struct {
	stdout string
	stderr string
	// timedOut is true when THIS invocation's own ceiling expired, and it is
	// deliberately not the same fact as the call being cancelled. A timeout is
	// this Mac being slow or a command being wedged; a cancellation is the
	// caller having gone away. They deserve different answers, and only the
	// function that ran the command can still tell them apart.
	timedOut bool
}

// runTool runs one command to completion.
//
// `extraEnv` is appended to this process's own environment, so os/exec's
// last-occurrence-wins makes the caller's value beat an inherited one
// deterministically. `dir` may be empty, which means this process's working
// directory — the right answer for a `git clone` that has not created its
// target yet.
func runTool(ctx context.Context, bin, dir string, extraEnv []string, timeout time.Duration, args ...string) (toolOutcome, error) {
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Deliberately not exec.CommandContext: its cancellation kills the process
	// and not the group, which is the exact bug the group kill below exists to
	// avoid.
	cmd, err := commandFor(bin, args...)
	if err != nil {
		return toolOutcome{}, fmt.Errorf("%w: %v", errStartFailed, err)
	}
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(), extraEnv...)

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	group, err := startProcessGroup(cmd)
	if err != nil {
		return toolOutcome{}, fmt.Errorf("%w: %s: %v", errStartFailed, bin, err)
	}
	defer group.release()

	exited := make(chan struct{})
	go func() {
		select {
		case <-runCtx.Done():
			killProcessGroup(group, claudeGrace, exited)
		case <-exited:
		}
	}()

	err = cmd.Wait()
	close(exited)

	out := toolOutcome{
		stdout: stdout.String(),
		stderr: stderr.String(),
		// The invocation's own ceiling, and not the caller's cancellation:
		// runCtx is done in both cases, so the parent has to be asked which it
		// was.
		timedOut: runCtx.Err() != nil && ctx.Err() == nil,
	}
	return out, err
}
