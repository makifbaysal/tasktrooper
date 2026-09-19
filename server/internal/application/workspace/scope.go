package workspace

import (
	"fmt"
	"os"
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
	if runtime.GOOS == "windows" {
		absPath = strings.ToLower(absPath)
		absRoot = strings.ToLower(absRoot)
	}
	if absPath == absRoot {
		return true, nil
	}
	sep := string(os.PathSeparator)
	return strings.HasPrefix(absPath, absRoot+sep), nil
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
	// Join treats an absolute rel as relative to root ("/etc/passwd" becomes
	// root/etc/passwd), which is the behaviour we want: the caller never gets
	// to name a path outside the workspace, only one inside it.
	joined := filepath.Join(absRoot, filepath.FromSlash(strings.TrimSpace(rel)))

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

// resolveSymlinks resolves every symlink in the parts of path that exist,
// keeping the not-yet-existing tail as written. EvalSymlinks fails outright on
// a missing path, but a path that does not exist yet still has to be judged:
// what matters is whether an existing component redirects out of the root.
func resolveSymlinks(path string) string {
	remainder := ""
	for cur := path; ; {
		if resolved, err := filepath.EvalSymlinks(cur); err == nil {
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

func ResolveScopedWorkDir(requested, scopeRoot string) (string, error) {
	scope := strings.TrimSpace(scopeRoot)
	if scope == "" {
		if strings.TrimSpace(requested) == "" {
			return "", fmt.Errorf("workspace scope is empty")
		}
		abs, err := filepath.Abs(requested)
		if err != nil {
			return "", fmt.Errorf("resolve working directory: %w", err)
		}
		return abs, nil
	}
	scopeAbs, err := filepath.Abs(scope)
	if err != nil {
		return "", fmt.Errorf("resolve workspace scope: %w", err)
	}
	if strings.TrimSpace(requested) == "" {
		return scopeAbs, nil
	}
	reqAbs, err := filepath.Abs(requested)
	if err != nil {
		return "", fmt.Errorf("resolve working directory: %w", err)
	}
	ok, err := IsWithinRoot(reqAbs, scopeAbs)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("working directory %q is outside repository scope %q", reqAbs, scopeAbs)
	}
	return reqAbs, nil
}
