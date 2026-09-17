package domain

import (
	"errors"
	"os/exec"
	"syscall"
)

// ExitSignal reports the signal that killed a child process, if it was killed
// by one. The agent CLIs are only ever stopped by this codebase through their
// own context (the run timeout or the caller's cancellation), both of which
// are already attributed elsewhere; a signaled exit reaching here came from
// outside — the OS, the desktop supervisor's own process-tree kill, App Nap,
// or a stray `kill` — and would otherwise surface as a bare, unexplained
// "exit status 143".
func ExitSignal(err error) (syscall.Signal, bool) {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return 0, false
	}
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok {
		return 0, false
	}
	if status.Signaled() {
		return status.Signal(), true
	}
	// A child that traps the signal to shut down cleanly — the Node CLIs all
	// do — is never "signaled" to the kernel: it exits on its own with the
	// shell convention 128+n, which is how SIGTERM arrives here as 143.
	//
	// The upper bound is the standard signal range (1-31), not
	// syscall.SIGUSR2: that constant's value is platform-dependent (12 on
	// Linux, 31 on Darwin), so using it here silently excluded 128+15=143
	// (SIGTERM) on Linux while passing on macOS.
	if code := status.ExitStatus(); code > 128 && code <= 128+31 {
		return syscall.Signal(code - 128), true
	}
	return 0, false
}
