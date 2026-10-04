//go:build unix

package proctree

import (
	"bufio"
	"context"
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func alive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// startBackgrounded runs `sh -c "sleep 60 & echo $!"` — the shape of an agent
// command that leaves a server running — and returns the tree and the
// backgrounded child's pid.
func startBackgrounded(t *testing.T, ctx context.Context) (*Tree, int, *exec.Cmd) {
	t.Helper()
	cmd := exec.CommandContext(ctx, "sh", "-c", "sleep 60 >/dev/null 2>&1 & echo $!")
	var out strings.Builder
	cmd.Stdout = &out
	tree, err := Start(cmd)
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("wait: %v", err)
	}
	child, err := strconv.Atoi(strings.TrimSpace(out.String()))
	if err != nil {
		t.Fatalf("child pid %q: %v", out.String(), err)
	}
	return tree, child, cmd
}

func TestKillTakesTheBackgroundedChildToo(t *testing.T) {
	tree, child, _ := startBackgrounded(t, context.Background())
	if !alive(child) {
		t.Fatal("backgrounded child should outlive its shell")
	}
	tree.Close()
	deadline := time.Now().Add(2 * time.Second)
	for alive(child) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if alive(child) {
		t.Fatal("backgrounded child survived Close")
	}
}

func TestRegistryKillScopeEndsOnlyThatScope(t *testing.T) {
	r := NewRegistry()
	a, childA, _ := startBackgrounded(t, context.Background())
	b, childB, _ := startBackgrounded(t, context.Background())
	r.Track("run:a", a)
	r.Track("run:b", b)

	r.KillScope("run:a", time.Second)
	if alive(childA) {
		t.Fatal("run:a's child survived KillScope")
	}
	if !alive(childB) {
		t.Fatal("run:b's child was killed with run:a")
	}
	r.KillAll(time.Second)
	if alive(childB) {
		t.Fatal("KillAll left run:b's child running")
	}
}

func TestContextCancelKillsTheTree(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, "sh", "-c", "sleep 60 >/dev/null 2>&1 & echo $!; wait")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	tree, err := Start(cmd)
	if err != nil {
		t.Fatal(err)
	}
	defer tree.Close()
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatalf("read child pid: %v", err)
	}
	child, _ := strconv.Atoi(strings.TrimSpace(line))
	cancel()
	_ = cmd.Wait()
	deadline := time.Now().Add(2 * time.Second)
	for child > 0 && alive(child) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if child == 0 || alive(child) {
		t.Fatalf("cancel left the backgrounded child (%d) running", child)
	}
}

func TestScopeFromDefaultsToUnscoped(t *testing.T) {
	if got := ScopeFrom(context.Background()); got != UnscopedScope {
		t.Fatalf("got %q", got)
	}
	if got := ScopeFrom(WithScope(context.Background(), "run:x")); got != "run:x" {
		t.Fatalf("got %q", got)
	}
}
