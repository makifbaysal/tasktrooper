package discovery

import (
	"context"
	"os/exec"
	"strings"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// gitCmdTimeout bounds one git invocation: a pack-corrupt or network-backed
// repo must not hang a scan.
const gitCmdTimeout = 15 * time.Second

// gitFacts fills domain.ScanGit; any failure (no git, not a repo, no
// origin) degrades to a warning, never an error — a scan of a fresh working
// copy with no remote yet is still a successful scan.
func gitFacts(ctx context.Context, root string) (domain.ScanGit, []string) {
	if _, err := exec.LookPath("git"); err != nil {
		return domain.ScanGit{}, []string{"git is not on PATH; git facts are missing"}
	}
	if out, err := runGit(ctx, root, "rev-parse", "--is-inside-work-tree"); err != nil || strings.TrimSpace(out) != "true" {
		return domain.ScanGit{}, []string{"working copy is not a git repository"}
	}
	g := domain.ScanGit{
		DefaultBranch: defaultBranch(ctx, root),
		HeadSHA:       gitLine(ctx, root, "rev-parse", "--short", "HEAD"),
	}
	_, g.RemoteSlug = parseRemote(gitLine(ctx, root, "remote", "get-url", "origin"))
	return g, nil
}

func runGit(ctx context.Context, root string, args ...string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, gitCmdTimeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, "git", args...) //nolint:gosec // fixed binary, args are literals
	cmd.Dir = root
	out, err := cmd.Output()
	return string(out), err
}

func gitLine(ctx context.Context, root string, args ...string) string {
	out, err := runGit(ctx, root, args...)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strings.SplitN(strings.TrimSpace(out), "\n", 2)[0])
}

func defaultBranch(ctx context.Context, root string) string {
	if ref := gitLine(ctx, root, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); ref != "" {
		return strings.TrimPrefix(ref, "origin/")
	}
	// No origin/HEAD (a freshly cloned working copy commonly lacks it): fall
	// back to whichever of the usual two actually exists, then to HEAD.
	for _, candidate := range []string{"main", "master"} {
		if _, err := runGit(ctx, root, "rev-parse", "--verify", "refs/remotes/origin/"+candidate); err == nil {
			return candidate
		}
	}
	return gitLine(ctx, root, "rev-parse", "--abbrev-ref", "HEAD")
}

func parseRemote(url string) (host, slug string) {
	url = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(url), ".git"))
	if url == "" {
		return "", ""
	}
	switch {
	case strings.HasPrefix(url, "git@"):
		rest := strings.TrimPrefix(url, "git@")
		parts := strings.SplitN(rest, ":", 2)
		if len(parts) == 2 {
			return parts[0], parts[1]
		}
	case strings.Contains(url, "://"):
		parts := strings.SplitN(url, "://", 2)
		hostAndPath := strings.SplitN(parts[1], "/", 2)
		if len(hostAndPath) == 2 {
			h := hostAndPath[0]
			if i := strings.IndexByte(h, '@'); i >= 0 {
				h = h[i+1:]
			}
			return h, hostAndPath[1]
		}
	}
	return "", ""
}
