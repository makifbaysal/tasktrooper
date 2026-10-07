package indexer

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

const gitQueryTimeout = time.Minute

type gitTreeState struct {
	head  string
	dirty []string
}

// readGitTreeState is unavailable unless root is the top level of a work tree:
// porcelain and diff paths are relative to the top level and would not match
// the walker's root-relative paths otherwise.
func readGitTreeState(ctx context.Context, root string) (gitTreeState, bool) {
	prefix, err := runGit(ctx, root, "rev-parse", "--show-prefix")
	if err != nil || strings.TrimSpace(string(prefix)) != "" {
		return gitTreeState{}, false
	}
	head, err := runGit(ctx, root, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return gitTreeState{}, false
	}
	status, err := runGit(ctx, root, "status", "--porcelain", "-z", "--untracked-files=all", "--no-renames")
	if err != nil {
		return gitTreeState{}, false
	}
	dirty, ok := parsePorcelainZ(status)
	if !ok {
		return gitTreeState{}, false
	}
	return gitTreeState{head: strings.TrimSpace(string(head)), dirty: dirty}, true
}

// gitSuspects lists the paths whose bytes may differ from what they were at
// commit since. Every other path in paths is a regular file git tracks, that
// is clean in state and has the same blob at since and at state.head.
func gitSuspects(ctx context.Context, root, since string, state gitTreeState, paths []string) (map[string]struct{}, bool) {
	if !isFullSHA(since) || state.head == "" {
		return nil, false
	}
	diff, err := runGit(ctx, root, "diff", "--name-only", "-z", "--no-renames", since, state.head, "--")
	if err != nil {
		return nil, false
	}
	listed, err := runGit(ctx, root, "ls-files", "-z", "-s", "-v")
	if err != nil {
		return nil, false
	}
	vouchable, ok := parseLsFilesZ(listed)
	if !ok {
		return nil, false
	}

	suspects := make(map[string]struct{})
	for _, rel := range splitNulFields(diff) {
		suspects[rel] = struct{}{}
	}
	for _, rel := range state.dirty {
		suspects[rel] = struct{}{}
	}
	// A path git does not track may be ignored by rules the walker does not
	// share (.git/info/exclude, nested .gitignore, a global excludes file), so
	// git staying silent about it says nothing about its bytes.
	for _, rel := range paths {
		if _, ok := vouchable[rel]; !ok {
			suspects[rel] = struct{}{}
		}
	}
	return suspects, true
}

func runGit(ctx context.Context, root string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, gitQueryTimeout)
	defer cancel()
	// Without --no-optional-locks, status refreshes the index under index.lock
	// and can make the pull that runs beside an index pass fail.
	argv := append([]string{"--no-optional-locks", "-C", root}, args...)
	return exec.CommandContext(ctx, "git", argv...).Output()
}

// parsePorcelainZ reads `git status --porcelain -z`. --no-renames should keep
// rename and copy entries out, but if one appears its source path follows as
// its own NUL field and is dirty too.
func parsePorcelainZ(out []byte) ([]string, bool) {
	fields := splitNulFields(out)
	paths := make([]string, 0, len(fields))
	for i := 0; i < len(fields); i++ {
		entry := fields[i]
		if len(entry) < 4 || entry[2] != ' ' {
			return nil, false
		}
		paths = append(paths, entry[3:])
		if isRenameOrCopy(entry[0]) || isRenameOrCopy(entry[1]) {
			i++
			if i >= len(fields) {
				return nil, false
			}
			paths = append(paths, fields[i])
		}
	}
	return paths, true
}

func isRenameOrCopy(code byte) bool {
	return code == 'R' || code == 'C'
}

// parseLsFilesZ reads `git ls-files -z -s -v` and keeps the paths git can vouch
// for. A symlink's bytes are its target's, which git attributes to the target's
// path, and status hides edits to assume-unchanged (lowercase tag) and
// skip-worktree (S) entries, so only plain cached regular files qualify.
func parseLsFilesZ(out []byte) (map[string]struct{}, bool) {
	fields := splitNulFields(out)
	vouchable := make(map[string]struct{}, len(fields))
	for _, entry := range fields {
		meta, path, found := strings.Cut(entry, "\t")
		if !found {
			return nil, false
		}
		parts := strings.Fields(meta)
		if len(parts) != 4 {
			return nil, false
		}
		tag, mode, stage := parts[0], parts[1], parts[3]
		if tag != "H" || stage != "0" || (mode != "100644" && mode != "100755") {
			continue
		}
		vouchable[path] = struct{}{}
	}
	return vouchable, true
}

func splitNulFields(out []byte) []string {
	s := strings.TrimSuffix(string(out), "\x00")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\x00")
}

func isFullSHA(sha string) bool {
	if len(sha) != 40 && len(sha) != 64 {
		return false
	}
	for _, c := range sha {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
