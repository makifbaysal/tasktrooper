//go:build unix

package mobilebuild

import (
	"os"
	"syscall"
)

// ownedByCurrentUser keeps Sweep to this user's own run directories: a shared
// temp directory can hold another user's, and those are not this process's to
// judge stale.
func ownedByCurrentUser(info os.FileInfo) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == os.Getuid()
}
