//go:build windows

package proctree

import "time"

// KillProcessesUnder does nothing on Windows: the job object a tree runs in
// has KILL_ON_JOB_CLOSE, so it ends with the server that created it.
func KillProcessesUnder(string, time.Duration) (int, error) { return 0, nil }
