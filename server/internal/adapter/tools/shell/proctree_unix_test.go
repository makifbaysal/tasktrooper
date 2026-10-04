//go:build unix

package shell_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/tools/shell"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/proctree"
)

func alive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

func readPID(t *testing.T, path string) int {
	t.Helper()
	var pid int
	require.Eventually(t, func() bool {
		b, err := os.ReadFile(path)
		if err != nil {
			return false
		}
		n, err := strconv.Atoi(strings.TrimSpace(string(b)))
		if err != nil {
			return false
		}
		pid = n
		return true
	}, 5*time.Second, 20*time.Millisecond)
	return pid
}

func sleeperCommand(pidFile string) string {
	return fmt.Sprintf("sh -c 'echo $$ > %s; exec sleep 30' >/dev/null 2>&1", pidFile)
}

func requireDead(t *testing.T, pid int) {
	t.Helper()
	require.Eventually(t, func() bool { return !alive(pid) }, 5*time.Second, 20*time.Millisecond)
}

func TestBackgroundedCommandStaysTrackedUntilScopeEnds(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	scope := "test:" + t.Name()
	ctx := proctree.WithScope(context.Background(), scope)
	t.Cleanup(func() { proctree.Default.KillScope(scope, time.Second) })

	tool := shell.New(t.TempDir(), 10*time.Second, 0, offSandbox())
	res := tool.Execute(ctx, `{"command":`+quote(sleeperCommand(pidFile)+" &")+`}`)
	require.False(t, res.IsError, res.Content)

	pid := readPID(t, pidFile)
	require.True(t, alive(pid), "backgrounded server must survive the call")

	proctree.Default.KillScope(scope, time.Second)
	requireDead(t, pid)
}

func TestForegroundCommandLeavesNothingBehind(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")

	tool := shell.New(t.TempDir(), 10*time.Second, 0, offSandbox())
	res := tool.Execute(context.Background(), `{"command":`+quote(sleeperCommand(pidFile)+" & while [ ! -s "+pidFile+" ]; do sleep 0.05; done")+`}`)
	require.False(t, res.IsError, res.Content)

	requireDead(t, readPID(t, pidFile))
}

func TestTimeoutStillReportedWithBackgroundedDescendant(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")

	tool := shell.New(t.TempDir(), time.Second, 0, offSandbox())
	res := tool.Execute(context.Background(), `{"command":`+quote("sh -c 'echo $$ > "+pidFile+"; exec sleep 30'")+`,"timeout_seconds":1}`)
	require.True(t, res.IsError)
	require.Contains(t, res.Content, "timed out")
	requireDead(t, readPID(t, pidFile))
}
