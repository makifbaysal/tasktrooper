//go:build unix

package localpreview

import (
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func shellCommand(command string) *exec.Cmd {
	cmd := exec.Command("sh", "-c", command)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return cmd
}

func terminateProcessGroup(pid int) {
	_ = syscall.Kill(-pid, syscall.SIGTERM)
}

// processCommand is a live process's command line, "" when there is none.
// -ww: a command line cut at the terminal width can no longer contain the
// command it is compared against.
func processCommand(pid int) string {
	if syscall.Kill(pid, 0) != nil {
		return ""
	}
	out, err := exec.Command("ps", "-ww", "-o", "command=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func leadsOwnGroup(pid int) bool {
	pgid, err := syscall.Getpgid(pid)
	return err == nil && pgid == pid
}

// stopStale stops a process this Service did not start, with the group it
// runs in (npm, its shell and the server all go together) — unless that group
// is this server's own, where only the process itself is signalled.
func stopStale(pid int, grace time.Duration) {
	target := pid
	if pgid, err := syscall.Getpgid(pid); err == nil && pgid != syscall.Getpgrp() {
		target = -pgid
	}
	_ = syscall.Kill(target, syscall.SIGTERM)
	for deadline := time.Now().Add(grace); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		if syscall.Kill(pid, 0) != nil {
			return
		}
	}
	_ = syscall.Kill(target, syscall.SIGKILL)
}
