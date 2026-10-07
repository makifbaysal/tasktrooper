//go:build windows

package localpreview

import (
	"context"
	"os/exec"
	"strconv"
	"syscall"
	"time"

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

func terminateProcessGroup(pid int) {
	_ = exec.Command("taskkill", "/T", "/PID", strconv.Itoa(pid)).Run()
}

func killProcessGroup(pid int) {
	_ = exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(pid)).Run()
}

// processCommand is not read on Windows, so no stale server is ever stopped
// there.
func processCommand(int) string { return "" }

func stopStale(int, time.Duration) {}
