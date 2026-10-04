//go:build unix

package proctree

import (
	"errors"
	"os/exec"
	"syscall"
	"time"
)

type platformTree struct{}

func prepare(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	cmd.SysProcAttr.Pgid = 0
}

func attach(int) platformTree { return platformTree{} }

// The root was started with Setpgid, so its pid is the group id, and every
// descendant that did not create a group of its own is in it.
func (platformTree) kill(pid int) {
	if pid > 0 {
		_ = syscall.Kill(-pid, syscall.SIGKILL)
	}
}

func (platformTree) release() {}

func killFallback(pid int) {
	if pid > 0 {
		_ = syscall.Kill(-pid, syscall.SIGKILL)
	}
}

func terminate(pid int, grace time.Duration) {
	if pid <= 0 {
		return
	}
	if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil {
		return
	}
	deadline := time.Now().Add(grace)
	for time.Now().Before(deadline) {
		// Signal 0 probes the group: ESRCH once no member is left.
		if err := syscall.Kill(-pid, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}
