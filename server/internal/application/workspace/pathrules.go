package workspace

import (
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

// The helpers in this file judge a path by the rules of the OS that will open
// it, and take that OS as goos instead of reading runtime.GOOS. Windows and
// macOS resolve one directory entry from many spellings, and every one of the
// workspace's guards has to agree with the filesystem about which spellings
// those are; passing goos in is what lets a test on any host prove it does.

// segmentMatch says how far two spellings of a path segment may differ and
// still be treated as the same directory entry.
type segmentMatch int

const (
	// matchExact is containment: case-insensitive only on Windows, where it
	// always is. Erring towards "different" here can only refuse a path.
	matchExact segmentMatch = iota
	// matchFold also folds case on macOS, whose default APFS volume is
	// case-insensitive. For translating a path onto a root, never for a guard.
	matchFold
	// matchAlias is every spelling the filesystem would open as the same
	// entry. Erring towards "same" here is what makes a deny-list hold.
	matchAlias
)

func isSeparator(c byte, goos string) bool {
	return c == '/' || (goos == "windows" && c == '\\')
}

func isDriveLetter(c byte) bool {
	return ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z')
}

func volumeOf(p, goos string) string {
	if goos != "windows" {
		return ""
	}
	if len(p) >= 2 && p[1] == ':' && isDriveLetter(p[0]) {
		return p[:2]
	}
	if len(p) < 2 || !isSeparator(p[0], goos) || !isSeparator(p[1], goos) {
		return ""
	}
	i := 2
	for i < len(p) && !isSeparator(p[i], goos) {
		i++
	}
	if i >= len(p) {
		return p
	}
	i++
	for i < len(p) && !isSeparator(p[i], goos) {
		i++
	}
	return p[:i]
}

func splitSegments(p, goos string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(p); i++ {
		if i < len(p) && !isSeparator(p[i], goos) {
			continue
		}
		if seg := p[start:i]; seg != "" && seg != "." {
			out = append(out, seg)
		}
		start = i + 1
	}
	return out
}

// hfsIgnorable are the code points HFS+ drops from a name before comparing
// it, so ".g\u200cit" opens .git there. The list is git's own (utf8.c,
// next_hfs_char), written for the same guard.
func hfsIgnorable(r rune) bool {
	switch {
	case r == 0x200c, r == 0x200d, r == 0x200e, r == 0x200f:
		return true
	case r >= 0x202a && r <= 0x202e:
		return true
	case r >= 0x206a && r <= 0x206f:
		return true
	case r == 0xfeff:
		return true
	}
	return false
}

func segmentKey(seg, goos string, m segmentMatch) string {
	if seg == "." || seg == ".." {
		return seg
	}
	switch goos {
	case "windows":
		if m == matchAlias {
			// Win32 path normalisation drops trailing dots and spaces, so
			// ".git." and ".git " both open .git.
			seg = strings.TrimRight(seg, ". ")
		}
		return strings.ToLower(seg)
	case "darwin":
		if m == matchAlias {
			seg = strings.Map(func(r rune) rune {
				if hfsIgnorable(r) {
					return -1
				}
				return r
			}, seg)
		}
		if m == matchFold || m == matchAlias {
			return strings.ToLower(seg)
		}
	}
	return seg
}

// residualUnder returns the segments of target below base, in target's own
// spelling, when target lies at or under base under match m. Both are expected
// to be absolute and cleaned; a ".." left in either is refused rather than
// interpreted.
func residualUnder(base, target, goos string, m segmentMatch) ([]string, bool) {
	bv, tv := volumeOf(base, goos), volumeOf(target, goos)
	if !strings.EqualFold(bv, tv) {
		return nil, false
	}
	keyed := func(p string) ([]string, []string, bool) {
		var segs, keys []string
		for _, seg := range splitSegments(p, goos) {
			key := segmentKey(seg, goos, m)
			if key == ".." {
				return nil, nil, false
			}
			// A segment the OS normalises away entirely ("..." or " " on
			// Windows) names the directory it sits in.
			if key == "" {
				continue
			}
			segs = append(segs, seg)
			keys = append(keys, key)
		}
		return segs, keys, true
	}
	_, baseKeys, ok := keyed(base[len(bv):])
	if !ok {
		return nil, false
	}
	targetSegs, targetKeys, ok := keyed(target[len(tv):])
	if !ok || len(targetKeys) < len(baseKeys) {
		return nil, false
	}
	for i, key := range baseKeys {
		if targetKeys[i] != key {
			return nil, false
		}
	}
	return targetSegs[len(baseKeys):], true
}

// relUnderRoot is residualUnder for this host, comparing both the paths as
// written and, failing that, their symlink-resolved forms: /var and
// /private/var on macOS, an 8.3 short name and its long form on Windows.
func relUnderRoot(base, target string, m segmentMatch) ([]string, bool) {
	if segs, ok := residualUnder(base, target, runtime.GOOS, m); ok {
		return segs, true
	}
	return residualUnder(resolveSymlinks(base), resolveSymlinks(target), runtime.GOOS, m)
}

// CanonicalPath is the one spelling a host path is stored and looked up under.
// Windows hands out both c:\ and C:\ for the same drive depending on who typed
// it, and an exact-match lookup then registers the same checkout twice.
func CanonicalPath(p string) string {
	trimmed := strings.TrimSpace(p)
	if trimmed == "" {
		return ""
	}
	return canonicalDrive(filepath.Clean(trimmed), runtime.GOOS)
}

func canonicalDrive(p, goos string) string {
	if goos == "windows" && len(p) >= 2 && p[1] == ':' && isDriveLetter(p[0]) {
		return strings.ToUpper(p[:1]) + p[1:]
	}
	return p
}

// BaseName is the final segment of a path that may have been written on a
// different OS. Stored paths cross hosts, and filepath.Base on Unix returns
// `C:\Users\me\acme` whole because a backslash is not a separator there.
func BaseName(p string) string {
	slashed := strings.ReplaceAll(strings.TrimSpace(p), `\`, "/")
	if slashed == "" {
		return ""
	}
	base := path.Base(path.Clean(slashed))
	if base == "." || base == ".." || base == "/" {
		return ""
	}
	if len(base) == 2 && base[1] == ':' && isDriveLetter(base[0]) {
		return ""
	}
	return base
}

// IndexPath turns a model-supplied file path into the form index rows and
// tree listings use: relative to root and slash-separated. A model on Windows
// sends `src\foo.ts` or the absolute `C:\ws\src\foo.ts`, neither of which
// equals the stored "src/foo.ts".
func IndexPath(root, p string) string {
	trimmed := strings.TrimSpace(p)
	if trimmed == "" {
		return ""
	}
	native := filepath.FromSlash(trimmed)
	if r := strings.TrimSpace(root); r != "" && filepath.IsAbs(native) {
		if absRoot, err := filepath.Abs(r); err == nil {
			if segs, ok := relUnderRoot(absRoot, filepath.Clean(native), matchFold); ok {
				return strings.Join(segs, "/")
			}
		}
	}
	return indexPathFor(native, runtime.GOOS)
}

func indexPathFor(p, goos string) string {
	if goos == "windows" {
		p = strings.ReplaceAll(p, `\`, "/")
	}
	cleaned := path.Clean(p)
	if cleaned == "." {
		return ""
	}
	return cleaned
}
