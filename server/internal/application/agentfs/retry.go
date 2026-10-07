package agentfs

import (
	"errors"
	"os"
	"runtime"
	"syscall"
	"time"
)

// sharingRetryDelays bound how long a rename or remove waits out another
// process. On Windows a file held open without FILE_SHARE_DELETE — an agent
// CLI still reading its skill, an antivirus or indexer scan — can be neither
// replaced nor deleted until it is closed, which is usually a matter of
// milliseconds. Unix never refuses an unlink or rename for that reason.
var sharingRetryDelays = []time.Duration{
	10 * time.Millisecond, 25 * time.Millisecond, 50 * time.Millisecond,
	100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond,
}

func retrySharing(goos string, sleep func(time.Duration), op func() error) error {
	err := op()
	if goos != "windows" {
		return err
	}
	for _, delay := range sharingRetryDelays {
		if err == nil || !isSharingViolation(err) {
			return err
		}
		sleep(delay)
		err = op()
	}
	return err
}

// isSharingViolation is only meaningful for a Windows error: 5, 32 and 33 are
// ERROR_ACCESS_DENIED (what replacing an open file reports), and
// ERROR_SHARING_VIOLATION and ERROR_LOCK_VIOLATION.
func isSharingViolation(err error) bool {
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return false
	}
	switch errno {
	case 5, 32, 33:
		return true
	}
	return false
}

// RemoveAll is os.RemoveAll that rides out a Windows sharing violation.
func RemoveAll(path string) error {
	return retrySharing(runtime.GOOS, time.Sleep, func() error { return os.RemoveAll(path) })
}

func rename(from, to string) error {
	return retrySharing(runtime.GOOS, time.Sleep, func() error { return os.Rename(from, to) })
}
