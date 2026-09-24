package projectmodel

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

const gitDiffTimeout = 15 * time.Second

// changedPathsSince returns the repo-relative paths that differ between two
// commits, via plain `git diff --name-only`, never a shell.
func changedPathsSince(ctx context.Context, root, oldSHA, newSHA string) []string {
	cctx, cancel := context.WithTimeout(ctx, gitDiffTimeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, "git", "-C", root, "diff", "--name-only", oldSHA+".."+newSHA)
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var paths []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			paths = append(paths, line)
		}
	}
	return paths
}
