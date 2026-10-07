//go:build windows

package workspace

import (
	"golang.org/x/sys/windows"
)

// GetFinalPathNameByHandle flags, which x/sys/windows does not name.
const (
	fileNameNormalized = 0x0
	volumeNameDOS      = 0x0
)

// evalExisting asks the filesystem where path really lands. Since Go 1.23
// (winsymlink=1) filepath.EvalSymlinks no longer follows NTFS junctions, so a
// junction inside the workspace — mklink /J, npm link — passed the
// containment check while every open went straight through it.
// GetFinalPathNameByHandle reports the path the opened handle ended up at,
// following symlinks and junctions alike and expanding 8.3 short names.
func evalExisting(path string) (string, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}
	h, err := windows.CreateFile(p, 0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(h)

	buf := make([]uint16, windows.MAX_PATH)
	for {
		n, err := windows.GetFinalPathNameByHandle(h, &buf[0], uint32(len(buf)), fileNameNormalized|volumeNameDOS)
		if err != nil {
			return "", err
		}
		if int(n) < len(buf) {
			return stripExtendedPrefix(windows.UTF16ToString(buf[:n])), nil
		}
		buf = make([]uint16, n)
	}
}
