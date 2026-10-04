//go:build unix

package proctree

import (
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func startSleepIn(t *testing.T, dir string) (*exec.Cmd, chan struct{}) {
	t.Helper()
	cmd := exec.Command("sleep", "60")
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	return cmd, done
}

func TestKillProcessesUnderKillsOnlyThoseInTheDir(t *testing.T) {
	inside, outside := t.TempDir(), t.TempDir()
	in, inDone := startSleepIn(t, inside)
	out, outDone := startSleepIn(t, outside)
	time.Sleep(200 * time.Millisecond)

	killed, err := KillProcessesUnder(inside, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if killed < 1 {
		t.Fatalf("killed = %d, want at least 1", killed)
	}
	select {
	case <-inDone:
	case <-time.After(5 * time.Second):
		t.Fatalf("process %d in the dir survived", in.Process.Pid)
	}
	select {
	case <-outDone:
		t.Fatalf("process %d outside the dir was killed", out.Process.Pid)
	default:
	}
}
