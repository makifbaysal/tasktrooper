package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ScopeWorkingTree keeps the uncommitted changes under keepDir and undoes
// every other one: tracked files go back to HEAD, files HEAD never had are
// removed. It returns the changed paths it kept and the ones it undid.
func (c *Client) ScopeWorkingTree(ctx context.Context, workspacePath, keepDir string) (kept, discarded []string, err error) {
	if !c.HasGit(workspacePath) {
		return nil, nil, nil
	}
	out, err := c.run(ctx, workspacePath, "git", "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return nil, nil, fmt.Errorf("git status: %w", err)
	}
	prefix := strings.Trim(filepath.ToSlash(keepDir), "/") + "/"
	for _, path := range statusPaths(out) {
		if strings.HasPrefix(path, prefix) {
			kept = append(kept, path)
			continue
		}
		if err := c.undoPath(ctx, workspacePath, path); err != nil {
			return kept, discarded, err
		}
		discarded = append(discarded, path)
	}
	return kept, discarded, nil
}

// statusPaths reads `git status --porcelain=v1 -z`: "XY path", and for a
// rename or copy the original path follows as its own NUL-terminated field.
func statusPaths(out string) []string {
	fields := strings.Split(out, "\x00")
	var paths []string
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if len(f) < 4 {
			continue
		}
		paths = append(paths, f[3:])
		if f[0] == 'R' || f[0] == 'C' {
			if i+1 < len(fields) && fields[i+1] != "" {
				paths = append(paths, fields[i+1])
			}
			i++
		}
	}
	return paths
}

func (c *Client) undoPath(ctx context.Context, workspacePath, path string) error {
	if _, err := c.run(ctx, workspacePath, "git", "cat-file", "-e", "HEAD:"+path); err == nil {
		if _, err := c.run(ctx, workspacePath, "git", "checkout", "HEAD", "--", path); err != nil {
			return fmt.Errorf("git checkout %s: %w", path, err)
		}
		return nil
	}
	_, _ = c.run(ctx, workspacePath, "git", "rm", "-r", "-q", "--cached", "--ignore-unmatch", "--", path)
	abs := filepath.Join(workspacePath, filepath.FromSlash(path))
	if !strings.HasPrefix(abs, filepath.Clean(workspacePath)+string(os.PathSeparator)) {
		return fmt.Errorf("refusing to remove %s: outside the workspace", path)
	}
	if err := os.RemoveAll(abs); err != nil {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	return nil
}
