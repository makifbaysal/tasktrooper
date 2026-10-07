//go:build unix

package database

import (
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

// stopWhenOrphaned leaves a detached watcher that kills the cluster once this
// process is gone. postgres daemonises, so a SIGKILLed test binary never reaches
// Stop and its cluster keeps holding the port and memory; Setsid keeps the
// watcher out of the process group such a kill takes down.
func stopWhenOrphaned(dataDir string) {
	const script = `while kill -0 "$1" 2>/dev/null; do sleep 1; done
pid=$(head -n 1 "$2/postmaster.pid" 2>/dev/null) && kill -INT "$pid" 2>/dev/null`
	cmd := exec.Command("/bin/sh", "-c", script, "sh", strconv.Itoa(os.Getpid()), dataDir)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return
	}
	_ = cmd.Process.Release()
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}
