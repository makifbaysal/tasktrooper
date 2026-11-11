package git

import (
	"context"
	"fmt"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

var _ port.RunGitInspector = (*Client)(nil)

// ChangesSince lists what differs from base in the working tree: committed,
// staged and unstaged tracked changes, then untracked files git does not
// ignore (status "?").
func (c *Client) ChangesSince(ctx context.Context, workspacePath, base string) ([]port.RunGitFile, error) {
	if strings.TrimSpace(base) == "" {
		return nil, fmt.Errorf("no base commit")
	}
	out, err := c.run(ctx, workspacePath, "git", "diff", "--name-status", "-z", "-M", base, "--")
	if err != nil {
		return nil, fmt.Errorf("git diff --name-status: %w (%s)", err, strings.TrimSpace(out))
	}
	files := parseNameStatusZ(out)
	untracked, err := c.run(ctx, workspacePath, "git", "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, fmt.Errorf("git ls-files --others: %w (%s)", err, strings.TrimSpace(untracked))
	}
	for _, p := range nulSeparated(untracked) {
		files = append(files, port.RunGitFile{Path: p, Status: "?"})
	}
	return files, nil
}

func parseNameStatusZ(out string) []port.RunGitFile {
	tokens := nulSeparated(out)
	var files []port.RunGitFile
	for i := 0; i < len(tokens); i++ {
		status := tokens[i]
		if status == "" {
			continue
		}
		switch status[0] {
		case 'R', 'C':
			if i+2 >= len(tokens) {
				return files
			}
			files = append(files, port.RunGitFile{Path: tokens[i+2], Status: status[:1], From: tokens[i+1]})
			i += 2
		default:
			if i+1 >= len(tokens) {
				return files
			}
			files = append(files, port.RunGitFile{Path: tokens[i+1], Status: status[:1]})
			i++
		}
	}
	return files
}

// TrackedFileCount is how many files rev tracks.
func (c *Client) TrackedFileCount(ctx context.Context, workspacePath, rev string) (int, error) {
	if strings.TrimSpace(rev) == "" {
		rev = "HEAD"
	}
	out, err := c.run(ctx, workspacePath, "git", "ls-tree", "-r", "-z", "--name-only", rev)
	if err != nil {
		return 0, fmt.Errorf("git ls-tree: %w (%s)", err, strings.TrimSpace(out))
	}
	return len(nulSeparated(out)), nil
}
