package domain

import (
	"errors"
	"os/exec"
	"runtime"
	"syscall"
)

// ExitSignal reports the signal that killed a child process, if it was killed
// by one. The agent CLIs are only ever stopped by this codebase through their
// own context; a signaled exit reaching here came from outside — the OS, the
// supervisor's process-tree kill, App Nap — and would otherwise surface as a
// bare "exit status 143".
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
	return signalFromExitCode(runtime.GOOS, status.ExitStatus())
}

// signalFromExitCode reads the shell convention 128+n that a child trapping a
// signal to shut down cleanly exits with. The upper bound is the standard
// signal range (1-31), not syscall.SIGUSR2: that constant is platform-dependent
// (12 on Linux, 31 on Darwin), so using it excluded 128+15=143 (SIGTERM) on
// Linux while passing on macOS. Windows has no signals and no such
// convention; an exit code there is only ever an exit code.
func signalFromExitCode(goos string, code int) (syscall.Signal, bool) {
	if goos == "windows" {
		return 0, false
	}
	if code > 128 && code <= 128+31 {
		return syscall.Signal(code - 128), true
	}
	return 0, false
}
