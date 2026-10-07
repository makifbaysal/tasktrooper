//go:build windows

package localpreview

import (
	"context"
	"os/exec"
	"strconv"
	"syscall"
	"time"

	"golang.org/x/sys/windows"

	"github.com/makifbaysal/tasktrooper/server/internal/platform/hostshell"
)

// The detected run commands (`./gradlew bootRun`, `./mvnw`, repo scripts) are
// POSIX, so they go through the same shell run_terminal uses rather than cmd.
func shellCommand(command string) *exec.Cmd {
	cmd := hostshell.Default().Command(context.Background(), command)
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= syscall.CREATE_NEW_PROCESS_GROUP
	return cmd
}

// taskkill without /F only posts WM_CLOSE, which console processes never
// receive, so the polite stop has to be a console event. CTRL_BREAK is the one
// a CREATE_NEW_PROCESS_GROUP group still receives (CTRL_C is disabled for it),
// and the root's pid is that group's id. Node, Go and Python exit on it; a JVM
// prints a thread dump instead and waits out the grace. Without a console
// shared with the preview the event cannot be sent, and the tree is killed at
// once rather than after a grace that would change nothing.
func terminateProcessGroup(pid int) {
	// Group 0 would be every process on this console, this server included.
	if pid <= 0 {
		return
	}
	if err := windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(pid)); err != nil {
		killProcessGroup(pid)
	}
}

func killProcessGroup(pid int) {
	_ = exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(pid)).Run()
}

// processCommand is not read on Windows, so neither a stale next dev nor a
// pid persisted by an earlier server is ever signalled there.
func processCommand(int) string { return "" }

func stopStale(int, time.Duration) {}

func leadsOwnGroup(int) bool { return false }
