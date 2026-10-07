//go:build windows

package mobilebuild

import "os"

// Windows temp directories are per user, and Run refuses to build here, so
// anything with the prefix under Root is this user's leftover.
func ownedByCurrentUser(os.FileInfo) bool { return true }
