//go:build unix

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"
)

// platformGroup is empty on unix: the group IS the root's pid, read back from
// the kernel at kill time.
type platformGroup struct{}

// prepareProcessGroup puts the child in its own process group. Two reasons,
// and both have cost a bug: a stray signal reaching this process must not race
// the ordered teardown by killing the child first, and killing a GROUP is the
// only way to take down the git, node and compiler processes a session spawns.
func prepareProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func attachProcessGroup(int) platformGroup { return platformGroup{} }

func (platformGroup) release() {}

// killProcessGroup sends SIGTERM to the whole group, then SIGKILL if anything
// is still there when the grace period runs out.
//
// The negative pid is the entire point of this function. `kill(pid)` signals
// one process; `kill(-pgid)` signals every process in the group, which is what
// a Claude Code session's children are because of `Setpgid` at spawn. Without
// it, cancelling a task leaves the compiler it started running.
//
// Getpgid rather than assuming pid == pgid: they are equal for a group leader,
// which this process is, but reading it back means a future change to how the
// child is spawned fails loudly here instead of silently signalling THIS
// process's group — which would kill the runner.
func killProcessGroup(g *processGroup, grace time.Duration, exited <-chan struct{}) {
	cmd := g.cmd
	// Never signal a process that has already been reaped. Between the caller
	// deciding to cancel and this line, `cmd.Wait` may have returned — and once
	// a pid is reaped the kernel is free to hand it to somebody else, so a
	// SIGKILL aimed at it lands on whatever got there first. Getpgid failing is
	// not a reliable guard on its own: it fails for a reaped pid and for a
	// recycled one alike, and the recycled case is exactly the one that must
	// not fall through to the single-process branch below. See reaped for why
	// this is not `cmd.ProcessState`.
	if reaped(cmd, exited) {
		return
	}
	pid := cmd.Process.Pid
	command := filepath.Base(cmd.Path)

	pgid, err := syscall.Getpgid(pid)
	if err != nil || pgid <= 1 {
		log.Warn().Int("pid", pid).Str("command", command).Err(err).Msg("no process group; signalling the process alone")
		_ = syscall.Kill(pid, syscall.SIGTERM)
		select {
		case <-exited:
			return
		case <-time.After(grace):
		}
		_ = syscall.Kill(pid, syscall.SIGKILL)
		return
	}

	log.Info().Int("pgid", pgid).Str("command", command).Msg("stopping the process group")
	_ = syscall.Kill(-pgid, syscall.SIGTERM)
	select {
	case <-exited:
		return
	case <-time.After(grace):
	}
	// Not a fallback but the rule: whatever ignored SIGTERM does not get to
	// outlive the task. A stopped task that leaves a live `claude` behind is a
	// bug this project has shipped before.
	log.Warn().Int("pgid", pgid).Str("command", command).Msg("the process group ignored SIGTERM; killing it")
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
}

// killProcessTree kills a long-lived helper's group outright, with no grace:
// the caller already asked politely through the helper's own channel.
func killProcessTree(g *processGroup) {
	if g == nil || g.cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-g.cmd.Process.Pid, syscall.SIGKILL)
}

// startDetached starts the one child that must outlive both its call and this
// process — see startEmulator. Its own group is not so this program can kill
// it, but so a signal aimed at the runner's group on quit does not take a
// booting emulator down with it.
func startDetached(build func() *exec.Cmd) (*exec.Cmd, error) {
	cmd := build()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return cmd, cmd.Start()
}

// ownedByThisUser reports whether a temp-dir entry belongs to this uid.
// os.TempDir() is only guaranteed private on macOS; on Linux it is /tmp, whose
// entries belong to whoever made them, and removing another user's files is
// not this program's business.
func ownedByThisUser(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(stat.Uid) == os.Getuid()
}
