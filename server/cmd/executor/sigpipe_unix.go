//go:build unix

package main

import (
	"os/signal"
	"syscall"
)

// ignoreBrokenPipe keeps the drain alive when the supervisor dies hard: its end
// of our stdout/stderr pipes closes with it, and Go's default is to kill the
// process on the next write to fd 1 or 2 — the drain's first log line — which
// skipped ending the runs and the process trees they started.
func ignoreBrokenPipe() {
	signal.Ignore(syscall.SIGPIPE)
}
