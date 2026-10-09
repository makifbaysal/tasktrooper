package main

import (
	"errors"
	"os"
	"os/exec"
	"sync"
	"syscall"
)

// processGroup is a child and every process it starts, held as one thing that
// can be killed together: a POSIX process group on macOS and Linux, a job
// object on Windows.
//
// Every child that is reaped with its call is started by startProcessGroup and
// stopped by killProcessGroup, so the rule "cancellation kills the GROUP" has
// one implementation per platform (proc_unix.go, proc_windows.go) rather than
// one per call site. The emulator is the one child that must outlive its call
// and this process, and it has its own door: startDetached.
type processGroup struct {
	cmd *exec.Cmd

	// mu orders a kill against release. On Windows the job is a HANDLE, and a
	// handle value that was closed can be handed out again for something else
	// — the same hazard as a recycled pid, which is why a kill that lost the
	// race with release must do nothing at all.
	mu       sync.Mutex
	released bool
	plat     platformGroup
}

// startProcessGroup starts cmd as the root of a group of its own. It replaces
// cmd.Start(); the caller still owns Wait.
func startProcessGroup(cmd *exec.Cmd) (*processGroup, error) {
	prepareProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &processGroup{cmd: cmd, plat: attachProcessGroup(cmd.Process.Pid)}, nil
}

// reaped reports whether the root has been waited for, so its pid may belong
// to somebody else by now. Not `cmd.ProcessState`: Wait writes that with no
// lock while the killer reads it from another goroutine. exited is closed by
// every caller once Wait has returned, and os.Process knows from inside Wait,
// earlier, under its own lock.
func reaped(cmd *exec.Cmd, exited <-chan struct{}) bool {
	if cmd.Process == nil {
		return true
	}
	select {
	case <-exited:
		return true
	default:
	}
	return errors.Is(cmd.Process.Signal(syscall.Signal(0)), os.ErrProcessDone)
}

// release gives back what the group holds once its root has been reaped. It
// kills nothing: what a child that exited on its own left running is what it
// meant to leave (adb's server outlives `adb devices` by design), and on unix
// that survives the group leader too.
func (g *processGroup) release() {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.released {
		return
	}
	g.released = true
	g.plat.release()
}
