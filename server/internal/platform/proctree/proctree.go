// Package proctree starts commands so that everything they leave behind can be
// killed with them: a POSIX process group, or a Windows job object.
//
// Killing only the direct child is how agent runs leaked: `sh -c "npx serve &"`
// exits at once, a cancelled `claude` takes its Bash tool's servers and its MCP
// servers with it to nowhere, and on Windows nothing below the direct child is
// ever touched. Everything those commands start stays in the tree, so killing
// the tree is the cleanup.
package proctree

import (
	"os/exec"
	"sync"
	"time"
)

// DefaultWaitDelay bounds how long Wait blocks on stdout/stderr pipes that a
// descendant still holds after the direct child exited; without it a server
// backgrounded by the command wedges Wait forever.
const DefaultWaitDelay = 5 * time.Second

// Tree is a started command and every process it started. Methods are safe to
// call more than once and after the tree has exited.
type Tree struct {
	mu     sync.Mutex
	pid    int
	closed bool
	plat   platformTree
}

// Start starts cmd as the root of a killable tree. When cmd was built with
// exec.CommandContext, cancelling the context kills the whole tree instead of
// the direct child only, and WaitDelay is set if the caller left it zero.
func Start(cmd *exec.Cmd) (*Tree, error) {
	prepare(cmd)
	t := &Tree{}
	// exec.CommandContext installs a default Cancel; a nil Cancel means a plain
	// exec.Command, where setting one would make Start fail.
	if cmd.Cancel != nil {
		cmd.Cancel = func() error {
			// Cancel can fire before attach below has run; the pid is known by
			// then, and killing by it is the platform's best effort.
			t.killPID(cmd.Process.Pid)
			return nil
		}
	}
	if cmd.WaitDelay == 0 {
		cmd.WaitDelay = DefaultWaitDelay
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	t.mu.Lock()
	t.pid = cmd.Process.Pid
	t.plat = attach(cmd.Process.Pid)
	t.mu.Unlock()
	return t, nil
}

// Pid is the root process's pid (on POSIX also the process group id).
func (t *Tree) Pid() int {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.pid
}

// Kill kills every process still in the tree, at once.
func (t *Tree) Kill() {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return
	}
	t.plat.kill(t.pid)
}

// Terminate asks the tree to exit (SIGTERM on POSIX), waits up to grace for it
// to empty, then kills what is left. Windows has no polite signal for a tree,
// so there it is Kill.
func (t *Tree) Terminate(grace time.Duration) {
	if t == nil {
		return
	}
	t.mu.Lock()
	pid, closed := t.pid, t.closed
	t.mu.Unlock()
	if closed {
		return
	}
	terminate(pid, grace)
	t.Kill()
}

// Close kills whatever is left and releases the tree's OS resources. Call it
// once the tree is no longer needed; after Close the other methods do nothing.
func (t *Tree) Close() {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return
	}
	t.plat.kill(t.pid)
	t.plat.release()
	t.closed = true
}

func (t *Tree) killPID(pid int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return
	}
	if t.pid != 0 {
		t.plat.kill(t.pid)
		return
	}
	killFallback(pid)
}
