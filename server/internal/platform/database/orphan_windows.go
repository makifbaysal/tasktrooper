//go:build windows

package database

import "golang.org/x/sys/windows"

// stopWhenOrphaned is a no-op on Windows: the unix side's detached /bin/sh
// watcher (kill -0/-INT, Setsid) has no equivalent here, so a hard-killed
// process leaves its embedded cluster running. The clean-shutdown Stop() path is
// unaffected.
func stopWhenOrphaned(dataDir string) {}

// stillActive is the exit code GetExitCodeProcess reports for a process that
// has not exited (STILL_ACTIVE).
const stillActive = 259

// os.Process.Signal supports only Kill on Windows, so the unix Signal(0) probe
// reads every live postmaster as dead, and the cluster a hard-killed run left
// behind loses its postmaster.pid instead of being reused.
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
