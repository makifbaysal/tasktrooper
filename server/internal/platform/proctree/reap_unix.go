//go:build unix

package proctree

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// KillProcessesUnder ends every process whose working directory is dir or
// inside it, except this process and its ancestors: what a previous server
// that died hard left running in task workspaces. SIGTERM first, SIGKILL after
// grace. It is for a moment when nothing legitimate runs there yet.
func KillProcessesUnder(dir string, grace time.Duration) (int, error) {
	root := resolveDir(dir)
	pids, err := pidsWithCwdUnder(root)
	if err != nil {
		return 0, err
	}
	skip := ancestorsAndSelf()
	matched := make(map[int]bool)
	for _, pid := range pids {
		if !skip[pid] {
			matched[pid] = true
		}
	}
	if len(matched) == 0 {
		return 0, nil
	}
	killed := len(matched)

	signalAll := func(sig syscall.Signal) {
		for pid := range matched {
			target := pid
			// A group is only ours to signal when its leader is itself one of
			// the matched processes; anything else could be an unrelated tree.
			if pgid, err := syscall.Getpgid(pid); err == nil && pgid == pid && !skip[pgid] {
				target = -pgid
			}
			_ = syscall.Kill(target, sig)
		}
	}
	signalAll(syscall.SIGTERM)

	for deadline := time.Now().Add(grace); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		for pid := range matched {
			if !pidAlive(pid) {
				delete(matched, pid)
			}
		}
		if len(matched) == 0 {
			return killed, nil
		}
	}
	signalAll(syscall.SIGKILL)
	return killed, nil
}

func pidAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

func ancestorsAndSelf() map[int]bool {
	skip := map[int]bool{os.Getpid(): true}
	pid := os.Getppid()
	for i := 0; pid > 1 && !skip[pid] && i < 64; i++ {
		skip[pid] = true
		pid = parentOf(pid)
	}
	return skip
}

func parentOf(pid int) int {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ps", "-o", "ppid=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0
	}
	ppid, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return ppid
}
