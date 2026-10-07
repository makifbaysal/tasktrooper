package workspace

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
)

func IsWithinRoot(path, root string) (bool, error) {
	absPath, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil {
		return false, fmt.Errorf("resolve path: %w", err)
	}
	trimmedRoot := strings.TrimSpace(root)
	if trimmedRoot == "*" {
		return true, nil
	}
	absRoot, err := filepath.Abs(trimmedRoot)
	if err != nil {
		return false, fmt.Errorf("resolve root: %w", err)
	}
	return isWithin(absPath, absRoot, runtime.GOOS), nil
}

// isWithin compares segment by segment rather than by string prefix: a prefix
// test against root+separator turns a filesystem root (`D:\`, `/`) into
// `D:\\` or `//`, which no path starts with.
func isWithin(absPath, absRoot, goos string) bool {
	_, ok := residualUnder(absRoot, absPath, goos, matchExact)
	return ok
}

// ResolveWithinRoot turns a caller-supplied relative path into an absolute one
// and returns it only when it genuinely lands inside root.
//
// It exists because `filepath.Join(root, userPath)` reads like a confinement
// and is not one. Join calls Clean, so "../../../../etc/passwd" is collapsed
// into a perfectly valid absolute path *outside* root and handed to whatever
// reads it. Every tool that turns model-supplied text into a filesystem path
// has to prove containment after the join; the tool policy depends on it. The
// restricted "cursor" API key is granted the read-only code tools and denied
// run_terminal on purpose, so an unchecked join in one of those tools hands
// that key every file the pod can read and the policy means nothing.
//
// Symlinks are resolved before the comparison. A repository checked out into
// the workspace can carry a symlink pointing at /etc or anywhere else outside
// the workspace root, and a textual prefix test on the unresolved path accepts it.
func ResolveWithinRoot(root, rel string) (string, error) {
	absRoot, err := filepath.Abs(strings.TrimSpace(root))
	if err != nil {
		return "", fmt.Errorf("resolve root: %w", err)
	}
	joined := filepath.Join(absRoot, relativeToRoot(absRoot, rel))

	ok, err := IsWithinRoot(joined, absRoot)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("path %q is outside the workspace root", rel)
	}

	ok, err = IsWithinRoot(resolveSymlinks(joined), resolveSymlinks(absRoot))
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("path %q resolves outside the workspace root through a symlink", rel)
	}

	return joined, nil
}

// relativeToRoot makes rel safe to join onto absRoot. An absolute rel that
// already names a place under the root is taken relative to it — a model on
// Windows sends `C:\ws\src\a.ts`, and joining that onto C:\ws gives
// `C:\ws\C:\ws\src\a.ts`. Any other absolute rel is re-rooted: the caller
// never gets to name a path outside the workspace, only one inside it, so
// "/etc/passwd" becomes root/etc/passwd and a foreign drive letter is dropped.
func relativeToRoot(absRoot, rel string) string {
	native := filepath.FromSlash(strings.TrimSpace(rel))
	if filepath.IsAbs(native) {
		if segs, ok := relUnderRoot(absRoot, filepath.Clean(native), matchFold); ok {
			return filepath.Join(segs...)
		}
	}
	return native[len(filepath.VolumeName(native)):]
}

// resolveSymlinks resolves every link in the parts of path that exist,
// keeping the not-yet-existing tail as written. Resolution fails outright on
// a missing path, but a path that does not exist yet still has to be judged:
// what matters is whether an existing component redirects out of the root.
func resolveSymlinks(path string) string {
	remainder := ""
	for cur := path; ; {
		if resolved, err := evalExisting(cur); err == nil {
			return filepath.Join(resolved, remainder)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return path // reached the filesystem root without resolving anything
		}
		remainder = filepath.Join(filepath.Base(cur), remainder)
		cur = parent
	}
}

// ResolveScopedWorkDir confines a requested working directory to scopeRoot.
// A relative request is taken relative to the scope — the model is told it is
// working in the repository, not in the server's own working directory — and
// the comparison runs on symlink-resolved forms, folded for case where the
// filesystem is, so /var vs /private/var on macOS and a %TEMP% 8.3 name on
// Windows do not turn a path inside the scope into a refusal. The result is
// always spelled under scopeRoot, so a later textual check agrees with it.
func ResolveScopedWorkDir(requested, scopeRoot string) (string, error) {
	scope := strings.TrimSpace(scopeRoot)
	req := strings.TrimSpace(requested)
	if scope == "" {
		if req == "" {
			return "", fmt.Errorf("workspace scope is empty")
		}
		abs, err := filepath.Abs(req)
		if err != nil {
			return "", fmt.Errorf("resolve working directory: %w", err)
		}
		return abs, nil
	}
	scopeAbs, err := filepath.Abs(scope)
	if err != nil {
		return "", fmt.Errorf("resolve workspace scope: %w", err)
	}
	if req == "" {
		return scopeAbs, nil
	}
	candidate := filepath.FromSlash(req)
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(scopeAbs, candidate)
	}
	candidate = filepath.Clean(candidate)
	segs, ok := residualUnder(resolveSymlinks(scopeAbs), resolveSymlinks(candidate), runtime.GOOS, matchFold)
	if !ok {
		return "", fmt.Errorf("working directory %q is outside repository scope %q", candidate, scopeAbs)
	}
	return filepath.Join(append([]string{scopeAbs}, segs...)...), nil
}
