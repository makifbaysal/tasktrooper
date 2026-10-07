package prompt

import "time"

type shellBlockingCommandInput struct {
	Segment   string
	What      string
	ShellKind string
}

var shellBlockingCommandKey = Define("guard.shell_blocking_command", shellBlockingCommandInput{Segment: "npm run dev", What: "a dev server", ShellKind: "posix"})

// ShellBlockingCommandText is run_terminal's refusal for a command whose only
// purpose is to run until interrupted — see adapter/tools/shell/blocking.go.
// shellKind picks how the refusal says to detach it (see platform/hostshell).
func ShellBlockingCommandText(segment, what, shellKind string) string {
	return shellBlockingCommandKey.Render(shellBlockingCommandInput{Segment: segment, What: what, ShellKind: shellKind})
}

type shellTimeoutRetryBelowInput struct {
	Timeout    time.Duration
	MaxSeconds int
}

var shellTimeoutRetryBelowKey = Define("guard.shell_timeout_retry_below", shellTimeoutRetryBelowInput{Timeout: 60 * time.Second, MaxSeconds: 900})

// ShellTimeoutRetryBelowText is run_terminal's retry hint for a timeout that
// has not yet hit the configured ceiling.
func ShellTimeoutRetryBelowText(timeout time.Duration, maxSeconds int) string {
	return shellTimeoutRetryBelowKey.Render(shellTimeoutRetryBelowInput{Timeout: timeout, MaxSeconds: maxSeconds})
}

type shellTimeoutAtCeilingInput struct{ MaxTimeout time.Duration }

var shellTimeoutAtCeilingKey = Define("guard.shell_timeout_at_ceiling", shellTimeoutAtCeilingInput{MaxTimeout: 15 * time.Minute})

// ShellTimeoutAtCeilingText is run_terminal's retry hint once a timeout is
// already at the configured maximum.
func ShellTimeoutAtCeilingText(maxTimeout time.Duration) string {
	return shellTimeoutAtCeilingKey.Render(shellTimeoutAtCeilingInput{MaxTimeout: maxTimeout})
}

var shellSilentSuccessKey = Define[struct{}]("tool_results.shell_silent_success", struct{}{})

// ShellSilentSuccessText is run_terminal's note for a command that exited 0
// with no output.
func ShellSilentSuccessText() string { return Text(shellSilentSuccessKey) }

type shellSlidingWindowHintInput struct{ Lines int }

var shellSlidingWindowHintKey = Define("tool_results.shell_sliding_window_hint", shellSlidingWindowHintInput{Lines: 11})

// ShellSlidingWindowHintText redirects a `sed -n 'N,Mp'` read to read_file.
func ShellSlidingWindowHintText(lines int) string {
	return shellSlidingWindowHintKey.Render(shellSlidingWindowHintInput{Lines: lines})
}
