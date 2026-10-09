package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestProcessGroupHelper is not a test of its own. It is the process tree the
// test below starts: this test binary run again, as a parent that starts a
// grandchild and reports its pid, or as that grandchild.
func TestProcessGroupHelper(t *testing.T) {
	switch os.Getenv("TT_PROC_HELPER") {
	case "parent":
		// On Windows the job is assigned just after Start returns; a grandchild
		// started before that would be outside it by design, and this test is
		// about the tree, not that window.
		time.Sleep(300 * time.Millisecond)
		child := exec.Command(os.Args[0], "-test.run=^TestProcessGroupHelper$")
		child.Env = append(os.Environ(), "TT_PROC_HELPER=grandchild")
		if err := child.Start(); err != nil {
			os.Exit(2)
		}
		fmt.Println(child.Process.Pid)
		time.Sleep(time.Minute)
		os.Exit(0)
	case "grandchild":
		time.Sleep(time.Minute)
		os.Exit(0)
	}
}

// Killing the group takes what the child started, not only the child — on
// unix through the process group, on Windows through the job object. The
// grandchild is a separate program the root started, which is the shape of
// `claude` starting git or a test runner.
func TestKillProcessGroupTakesTheGrandchildToo(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestProcessGroupHelper$")
	cmd.Env = append(os.Environ(), "TT_PROC_HELPER=parent")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	group, err := startProcessGroup(cmd)
	if err != nil {
		t.Fatalf("startProcessGroup: %v", err)
	}
	defer group.release()

	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatalf("reading the grandchild's pid: %v", err)
	}
	grandchild, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || !processAlive(grandchild) {
		t.Fatalf("grandchild %q is not running before the kill", line)
	}

	exited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(exited)
	}()
	killProcessGroup(group, 2*time.Second, exited)
	select {
	case <-exited:
	case <-time.After(15 * time.Second):
		t.Fatal("the root survived killProcessGroup")
	}

	deadline := time.Now().Add(5 * time.Second)
	for processAlive(grandchild) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if processAlive(grandchild) {
		t.Fatalf("grandchild %d outlived the group kill; a cancelled run would leave it working on a checkout nobody is watching", grandchild)
	}
}
