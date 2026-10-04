//go:build windows

package proctree

import (
	"os/exec"
	"strconv"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// platformTree holds the job object the root was assigned to. Children are
// created inside the job unless they ask to break away, and the job is created
// with KILL_ON_JOB_CLOSE, so even a hard kill of agent-server — which closes
// this handle with the process — takes the whole tree down.
type platformTree struct {
	job windows.Handle
}

func prepare(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= syscall.CREATE_NEW_PROCESS_GROUP
}

func attach(pid int) platformTree {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return platformTree{}
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			// BREAKAWAY_OK: a child that explicitly asks to leave the job still
			// starts, rather than failing CreateProcess outright.
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE | windows.JOB_OBJECT_LIMIT_BREAKAWAY_OK,
		},
	}
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		_ = windows.CloseHandle(job)
		return platformTree{}
	}
	proc, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		_ = windows.CloseHandle(job)
		return platformTree{}
	}
	defer windows.CloseHandle(proc)
	if err := windows.AssignProcessToJobObject(job, proc); err != nil {
		_ = windows.CloseHandle(job)
		return platformTree{}
	}
	return platformTree{job: job}
}

func (p platformTree) kill(pid int) {
	if p.job != 0 {
		_ = windows.TerminateJobObject(p.job, 1)
		return
	}
	killFallback(pid)
}

func (p platformTree) release() {
	if p.job != 0 {
		_ = windows.CloseHandle(p.job)
	}
}

// Without a job (assignment failed), taskkill /T walks the tree from the root
// — it misses anything already reparented, which is why the job is preferred.
func killFallback(pid int) {
	if pid > 0 {
		_ = exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(pid)).Run()
	}
}

func terminate(int, time.Duration) {}
