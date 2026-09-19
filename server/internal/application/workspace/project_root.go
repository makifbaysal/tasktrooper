package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// ValidateProjectRoot resolves a caller-supplied project root and refuses
// anything outside the roots a repository may be registered from.
//
// The allowlist is [the managed workspace root] + the operator's configured
// allowed_roots, and the first term is why an EMPTY allowed_roots is not a
// wildcard. It used to be: `len(allowedRoots) == 0` returned the path
// unconditionally, so POST /v1/repositories/open would index any readable
// directory for whoever asked. An empty allowlist now means what an empty
// allowlist means everywhere else here: nothing extra.
//
// allowed_roots therefore only ever WIDENS the set, and exists for the install
// that keeps its checkouts outside the managed workspace.
func ValidateProjectRoot(path, workspaceRoot string, allowedRoots []string) (string, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "", fmt.Errorf("project root is empty")
	}
	abs, err := filepath.Abs(trimmed)
	if err != nil {
		return "", fmt.Errorf("resolve project root: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("project root not accessible: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("project root is not a directory")
	}

	root, err := ResolveRoot(workspaceRoot)
	if err != nil {
		return "", err
	}
	roots := append([]string{root}, allowedRoots...)
	for _, allowed := range roots {
		allowed = strings.TrimSpace(allowed)
		if allowed == "" {
			continue
		}
		if allowed == "*" {
			return abs, nil
		}
		allowedAbs, err := filepath.Abs(allowed)
		if err != nil {
			continue
		}
		if pathMatches(abs, allowedAbs) {
			return abs, nil
		}
	}
	// The path is not echoed back: "not allowed" versus "does not exist" would
	// tell a caller which directories exist, and they already know what they
	// asked for.
	return "", fmt.Errorf("project root is outside this workspace's allowed roots")
}

func pathMatches(abs, allowedAbs string) bool {
	if runtime.GOOS == "windows" {
		abs = strings.ToLower(abs)
		allowedAbs = strings.ToLower(allowedAbs)
	}
	return abs == allowedAbs || strings.HasPrefix(abs, allowedAbs+string(os.PathSeparator))
}

func EffectiveProjectRoot(sessionWorkspace, projectRoot string) string {
	if strings.TrimSpace(projectRoot) != "" {
		return projectRoot
	}
	return sessionWorkspace
}
