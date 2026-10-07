package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// protectedNames are workspace entries no agent edit may create, change,
// remove or move, whatever it was asked to clean up. .git IS the task
// workspace: deleting it destroys the branch, the history and the run's only
// way to hand work back, and a hook planted under it runs on the server's next
// commit.
var protectedNames = []string{".git"}

var ErrWorkspaceRoot = errors.New("path is the workspace root itself")

// ProtectedPathError names the segment of a path that reaches a protected
// entry: the entry itself, any spelling the host filesystem resolves to it, or
// a segment this OS would not open as written (an NTFS stream).
type ProtectedPathError struct {
	Segment string
}

func (e *ProtectedPathError) Error() string {
	return fmt.Sprintf("%q is protected", e.Segment)
}

// ResolveEditableWithinRoot is ResolveWithinRoot for a path a tool is about to
// write, edit, delete or move.
//
// Containment is not enough for those. Windows and macOS open one entry under
// many spellings — case variants, trailing dots and spaces, 8.3 short names,
// NTFS stream suffixes, HFS-ignorable code points — so a textual comparison
// against "the root" or ".git" is bypassed by `..\WS`, `.GIT/hooks` or
// `GIT~1`. Names are therefore compared the way the filesystem would, and an
// entry that already exists is compared by identity as well.
func ResolveEditableWithinRoot(root, rel string) (string, error) {
	abs, err := ResolveWithinRoot(root, rel)
	if err != nil {
		return "", err
	}
	absRoot, err := filepath.Abs(strings.TrimSpace(root))
	if err != nil {
		return "", fmt.Errorf("resolve root: %w", err)
	}
	segs, err := editableSegments(absRoot, abs, runtime.GOOS)
	if err != nil {
		return "", err
	}
	if err := checkEditableOnDisk(absRoot, abs, segs); err != nil {
		return "", err
	}
	return abs, nil
}

func editableSegments(absRoot, abs, goos string) ([]string, error) {
	segs, ok := residualUnder(absRoot, abs, goos, matchAlias)
	if !ok {
		return nil, fmt.Errorf("path %q is outside the workspace root", abs)
	}
	if len(segs) == 0 {
		return nil, ErrWorkspaceRoot
	}
	for _, seg := range segs {
		if isProtectedSegment(seg, goos) {
			return nil, &ProtectedPathError{Segment: seg}
		}
	}
	return segs, nil
}

func isProtectedSegment(seg, goos string) bool {
	// "name:stream" opens a stream of name, and ".git::$INDEX_ALLOCATION" is
	// the .git directory itself.
	if goos == "windows" && strings.ContainsRune(seg, ':') {
		return true
	}
	key := segmentKey(seg, goos, matchAlias)
	for _, name := range protectedNames {
		if key == segmentKey(name, goos, matchAlias) {
			return true
		}
		if goos == "windows" && isShortNameAlias(key, name) {
			return true
		}
	}
	return false
}

// isShortNameAlias reports whether key (already lower-cased) could be the 8.3
// short name NTFS generated for name: up to six characters of the name with
// dots and spaces removed, "~" and a number; or, once ~1..~4 are taken, two
// characters and four hex digits of a hash before the "~". GIT~1 is .git.
func isShortNameAlias(key, name string) bool {
	tilde := strings.LastIndexByte(key, '~')
	if tilde <= 0 {
		return false
	}
	base, num := key[:tilde], key[tilde+1:]
	if dot := strings.IndexByte(num, '.'); dot >= 0 {
		num = num[:dot]
	}
	if num == "" || strings.Trim(num, "0123456789") != "" {
		return false
	}
	stem := strings.ToLower(strings.NewReplacer(".", "", " ", "").Replace(name))
	if stem == "" {
		return false
	}
	if base == stem[:min(len(stem), 6)] {
		return true
	}
	return len(base) == 6 &&
		strings.HasPrefix(base, stem[:min(len(stem), 2)]) &&
		strings.Trim(base[2:], "0123456789abcdef") == ""
}

// checkEditableOnDisk catches the spellings no name rule can enumerate: a
// symlink or junction to .git, an alias this code does not know about. It
// compares by file identity, so it only speaks about entries that exist.
func checkEditableOnDisk(absRoot, abs string, segs []string) error {
	rootInfo, err := os.Stat(absRoot)
	if err != nil {
		return nil
	}
	if info, err := os.Stat(abs); err == nil && os.SameFile(info, rootInfo) {
		return ErrWorkspaceRoot
	}
	var protected []os.FileInfo
	for _, name := range protectedNames {
		if info, err := os.Stat(filepath.Join(absRoot, name)); err == nil {
			protected = append(protected, info)
		}
	}
	if len(protected) == 0 {
		return nil
	}
	cur := absRoot
	for _, seg := range segs {
		cur = filepath.Join(cur, seg)
		info, err := os.Stat(cur)
		if err != nil {
			return nil
		}
		for _, p := range protected {
			if os.SameFile(info, p) {
				return &ProtectedPathError{Segment: seg}
			}
		}
	}
	return nil
}

func isRootItself(absRoot, abs string) bool {
	if segs, ok := residualUnder(absRoot, abs, runtime.GOOS, matchAlias); ok && len(segs) == 0 {
		return true
	}
	rootInfo, err := os.Stat(absRoot)
	if err != nil {
		return false
	}
	info, err := os.Stat(abs)
	return err == nil && os.SameFile(info, rootInfo)
}
