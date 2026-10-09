//go:build windows

package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
	"unsafe"

	"github.com/rs/zerolog/log"
	"golang.org/x/sys/windows"
)

// platformGroup is the job object the child was assigned to. Windows has no
// process groups that can be killed as one; a job can, and everything the child
// starts lands in it unless it explicitly breaks away.
//
// The job has KILL_ON_JOB_CLOSE while the child runs, so this process dying
// any way at all — including TerminateProcess, which is all a force-kill is on
// Windows — closes the handle and takes the tree with it. That is the Windows
// answer to the orphaned `claude` a SIGKILLed runner leaves on unix.
type platformGroup struct {
	job windows.Handle
}

// prepareProcessGroup gives the child a process group of its own and a console
// of its own with no window.
//
// Its own console is the point of CREATE_NO_WINDOW, not only the missing
// window: a child sharing this process's console could send it a console
// control event (Ctrl+C to group 0 reaches everything on the console), and
// os.Interrupt is one of the signals that shuts this runner down.
func prepareProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_NEW_PROCESS_GROUP | windows.CREATE_NO_WINDOW
	cmd.SysProcAttr.HideWindow = true
}

// attachProcessGroup assigns the started child to a new job. The child runs
// for a moment before it is assigned, and anything it starts in that moment
// is outside the job; os/exec offers no way to create a process suspended, and
// a process that has not finished loading has not started anything yet.
func attachProcessGroup(pid int) platformGroup {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		log.Warn().Err(err).Int("pid", pid).Msg("could not create a job object; a cancel will fall back to taskkill")
		return platformGroup{}
	}
	if err := setJobLimits(job, windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE|windows.JOB_OBJECT_LIMIT_BREAKAWAY_OK); err != nil {
		_ = windows.CloseHandle(job)
		log.Warn().Err(err).Int("pid", pid).Msg("could not configure a job object; a cancel will fall back to taskkill")
		return platformGroup{}
	}
	proc, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		_ = windows.CloseHandle(job)
		log.Warn().Err(err).Int("pid", pid).Msg("could not open the child to assign it a job; a cancel will fall back to taskkill")
		return platformGroup{}
	}
	defer func() { _ = windows.CloseHandle(proc) }()
	if err := windows.AssignProcessToJobObject(job, proc); err != nil {
		_ = windows.CloseHandle(job)
		log.Warn().Err(err).Int("pid", pid).Msg("could not assign the child to a job; a cancel will fall back to taskkill")
		return platformGroup{}
	}
	return platformGroup{job: job}
}

// setJobLimits replaces the job's limit flags. BREAKAWAY_OK is always on so a
// grandchild that asks to leave the job still starts, rather than failing its
// CreateProcess outright.
func setJobLimits(job windows.Handle, flags uint32) error {
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{LimitFlags: flags},
	}
	_, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)))
	return err
}

// release drops KILL_ON_JOB_CLOSE before closing the handle, so letting go of
// a job whose root exited on its own does not kill what the root left behind
// — unix semantics, where a group outlives its leader.
func (p platformGroup) release() {
	if p.job == 0 {
		return
	}
	_ = setJobLimits(p.job, windows.JOB_OBJECT_LIMIT_BREAKAWAY_OK)
	_ = windows.CloseHandle(p.job)
}

// kill ends every process in the job at once, or walks the tree from the root
// with taskkill when there is no job. taskkill misses anything already
// reparented, which is why the job is preferred.
func (p platformGroup) kill(pid int) {
	if p.job != 0 {
		if err := windows.TerminateJobObject(p.job, 1); err == nil {
			return
		}
	}
	if pid > 0 {
		_ = exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(pid)).Run()
	}
}

// killProcessGroup kills the tree immediately; grace and exited are unix's.
//
// There is no SIGTERM to send. The polite signal Windows has for a console
// process group, CTRL_BREAK, reaches only processes sharing the sender's
// console, and prepareProcessGroup gives every child a console of its own on
// purpose. Waiting out a grace period after sending nothing would only delay
// the kill.
func killProcessGroup(g *processGroup, _ time.Duration, exited <-chan struct{}) {
	if reaped(g.cmd, exited) {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.released {
		return
	}
	log.Info().Int("pid", g.cmd.Process.Pid).Str("command", filepath.Base(g.cmd.Path)).Msg("stopping the process tree")
	g.plat.kill(g.cmd.Process.Pid)
}

// killProcessTree is killProcessGroup for a long-lived helper the caller has
// already asked to stop through its own channel.
func killProcessTree(g *processGroup) {
	if g == nil || g.cmd.Process == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.released {
		return
	}
	g.plat.kill(g.cmd.Process.Pid)
}

// detachedFlags is what lets the emulator outlive this process: a group of its
// own, and a console of its own with no window rather than DETACHED_PROCESS's
// none at all — the emulator starts console children of its own (qemu), and a
// console child of a process with no console opens a visible window.
const detachedFlags = windows.CREATE_NEW_PROCESS_GROUP | windows.CREATE_NO_WINDOW

// startDetached starts the one child that must outlive both its call and this
// process — see startEmulator. It is given no job of ours, and it breaks away
// from any job this process is in: libuv puts every child Electron spawns
// without `detached` in a KILL_ON_JOB_CLOSE job, and a device inside it would
// die with the app — including a device a parked task is waiting on.
func startDetached(build func() *exec.Cmd) (*exec.Cmd, error) {
	cmd := build()
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: detachedFlags | windows.CREATE_BREAKAWAY_FROM_JOB}
	err := cmd.Start()
	if !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		return cmd, err
	}
	// A job that does not allow breakaway fails the whole CreateProcess rather
	// than ignoring the flag. A device tied to this process's lifetime is worse
	// than one that outlives it, and much better than no device.
	log.Warn().Err(err).Msg("this runner's job does not allow breakaway; the emulator will end with it")
	cmd = build()
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: detachedFlags}
	return cmd, cmd.Start()
}

// ownedByThisUser is always true on Windows: %TEMP% is the per-user
// AppData\Local\Temp, whose ACL admits only this account, so every entry in it
// is this user's. There is no uid to compare against.
func ownedByThisUser(os.FileInfo) bool { return true }
