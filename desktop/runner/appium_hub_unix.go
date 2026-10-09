//go:build unix

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// psBin is the system's own ps, by absolute path like securityBin: it is not a
// tool the desktop detects.
const psBin = "/bin/ps"

const (
	hubPSTimeout  = 5 * time.Second
	hubGonePoll   = 50 * time.Millisecond
	hubLstartSize = 5
)

// hubRecordPath is in the user's own cache directory, private to them and
// outside the workspace, which may be a synced folder. A runner killed on
// macOS or Linux leaves its hub running in a process group of its own; this
// record is how the next one knows it.
func hubRecordPath() string {
	dir, err := os.UserCacheDir()
	if err != nil || dir == "" {
		return ""
	}
	return filepath.Join(dir, "TaskTrooper", "runner", "appium-hub.json")
}

// processIdentity is pid's start time and command line, as ps prints them.
// The locale and the time zone are pinned so two runners print one process's
// start time the same way.
func processIdentity(ctx context.Context, pid int) (start, command string, err error) {
	out, err := runTool(ctx, psBin, "", []string{"LC_ALL=C", "TZ=UTC"}, hubPSTimeout,
		"-ww", "-o", "lstart=", "-o", "command=", "-p", strconv.Itoa(pid))
	if err != nil {
		return "", "", err
	}
	fields := strings.Fields(out.stdout)
	if len(fields) <= hubLstartSize {
		return "", "", errors.New("ps printed no start time and command for that pid")
	}
	return strings.Join(fields[:hubLstartSize], " "), strings.Join(fields[hubLstartSize:], " "), nil
}

func pidAlive(pid int) bool {
	return pid > 1 && syscall.Kill(pid, 0) == nil
}

// stopLeftover stops a hub a previous runner started. It is not this
// process's child, so nothing here waits for it: it is asked whether it is
// still the same process — same start time — before each signal, which is what
// keeps a pid recycled in the meantime from being signalled.
func stopLeftover(p *hubProcess, grace time.Duration) {
	if !sameLeftover(p) {
		p.markDone()
		return
	}
	target := p.pid
	if pgid, err := syscall.Getpgid(p.pid); err == nil && pgid == p.pid {
		target = -p.pid
	}
	_ = syscall.Kill(target, syscall.SIGTERM)
	if waitGone(p.pid, grace) || !sameLeftover(p) {
		p.markDone()
		return
	}
	_ = syscall.Kill(target, syscall.SIGKILL)
	if waitGone(p.pid, claudeReapTimeout) {
		p.markDone()
	}
}

func sameLeftover(p *hubProcess) bool {
	if !pidAlive(p.pid) {
		return false
	}
	start, _, err := processIdentity(context.Background(), p.pid)
	return err == nil && start == p.start
}

func waitGone(pid int, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for pidAlive(pid) {
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(hubGonePoll)
	}
	return true
}
