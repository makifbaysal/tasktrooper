//go:build windows

package embeddedpg

import "golang.org/x/sys/windows"

// stillActive is the exit code GetExitCodeProcess reports for a process that
// has not exited (STILL_ACTIVE).
const stillActive = 259

// os.Process.Signal supports only Kill on Windows, so the unix Signal(0) probe
// reads every live postmaster as dead: adoption is skipped and its
// postmaster.pid deleted from under it. A process this user cannot open is not
// this user's postmaster, which is also how the unix probe treats EPERM.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	return code == stillActive
}
