package discovery

import (
	"context"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// gitCmdTimeout bounds one git invocation: a pack-corrupt or network-backed
// repo must not hang a scan.
const gitCmdTimeout = 15 * time.Second

// churnCommits bounds how far back the hotspot ranking looks: far enough to
// show what the team actually keeps touching, short enough that a two-year-old
// rewrite does not dominate today's map.
const churnCommits = 300

// conventionalCommitRe matches the `type(scope): subject` shape; the ratio of
// matching subjects decides whether commitStyle can call it a convention — a
// convention is only a convention when history obeys it.
var conventionalCommitRe = regexp.MustCompile(`^(feat|fix|docs|style|refactor|perf|test|build|ci|chore|revert)(\([^)]+\))?!?: .+`)

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
	g.BranchSamples, g.BranchPattern = branchConvention(ctx, root, g.DefaultBranch)
	g.MergeStyle, g.DirectToMain = mergeStyle(ctx, root)
	g.CommitStyle = commitStyle(ctx, root)
	g.Hotspots = hotspots(ctx, root)
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

// branchConvention samples recent remote branches and reports the dominant
// naming shape — with the samples too, because a pattern with no examples is
// an assertion, and agents ignore assertions they cannot check.
func branchConvention(ctx context.Context, root, defaultBranch string) (samples []string, pattern string) {
	out, err := runGit(ctx, root, "for-each-ref", "--sort=-committerdate", "--count=40", "--format=%(refname:short)", "refs/remotes/origin")
	if err != nil {
		return nil, ""
	}
	prefixCount := map[string]int{}
	total := 0
	for _, line := range strings.Split(out, "\n") {
		name := strings.TrimPrefix(strings.TrimSpace(line), "origin/")
		// "origin" is what refs/remotes/origin/HEAD shortens to — the symbolic pointer, not a branch anyone named.
		if name == "" || name == "HEAD" || name == "origin" || name == defaultBranch {
			continue
		}
		total++
		if len(samples) < 6 {
			samples = append(samples, name)
		}
		if i := strings.IndexByte(name, '/'); i > 0 {
			prefixCount[name[:i]]++
		} else {
			prefixCount["(flat)"]++
		}
	}
	if total == 0 {
		return nil, ""
	}
	type kv struct {
		k string
		n int
	}
	ranked := make([]kv, 0, len(prefixCount))
	for k, n := range prefixCount {
		ranked = append(ranked, kv{k, n})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].n != ranked[j].n {
			return ranked[i].n > ranked[j].n
		}
		return ranked[i].k < ranked[j].k
	})
	top := ranked[0]
	if top.k == "(flat)" {
		pattern = "flat branch names (no type/ prefix)"
	} else {
		pattern = top.k + "/* prefix"
	}
	pattern += " — " + strconv.Itoa(top.n) + " of " + strconv.Itoa(total) + " recent branches"
	return samples, pattern
}

// mergeStyle distinguishes a merge-commit history from a linear one, and
// reports whether work lands on the default branch without a merge at all —
// the "we push straight to main" shape.
func mergeStyle(ctx context.Context, root string) (style string, directToMain bool) {
	total := countLines(gitOut(ctx, root, "log", "-100", "--format=%H"))
	if total == 0 {
		return "", false
	}
	merges := countLines(gitOut(ctx, root, "log", "-100", "--merges", "--format=%H"))
	switch {
	case merges == 0:
		return "linear history (squash or rebase merges, or direct pushes)", true
	case merges*4 >= total:
		return "merge commits (" + strconv.Itoa(merges) + " of last " + strconv.Itoa(total) + ")", false
	default:
		return "mostly linear (" + strconv.Itoa(merges) + " merge commits in last " + strconv.Itoa(total) + ")", false
	}
}

func commitStyle(ctx context.Context, root string) string {
	out := gitOut(ctx, root, "log", "-60", "--format=%s")
	lines := nonEmptyLines(out)
	if len(lines) == 0 {
		return ""
	}
	conventional := 0
	for _, s := range lines {
		if conventionalCommitRe.MatchString(s) {
			conventional++
		}
	}
	if conventional*2 >= len(lines) {
		return "Conventional Commits (" + strconv.Itoa(conventional) + " of last " + strconv.Itoa(len(lines)) + " subjects)"
	}
	return "free-form subjects (" + strconv.Itoa(conventional) + " of last " + strconv.Itoa(len(lines)) + " are conventional)"
}

func hotspots(ctx context.Context, root string) []domain.GitHotspot {
	out := gitOut(ctx, root, "log", "-"+strconv.Itoa(churnCommits), "--name-only", "--format=")
	counts := map[string]int{}
	for _, line := range nonEmptyLines(out) {
		path := strings.TrimSpace(line)
		if path == "" || strings.HasPrefix(path, ".github/") {
			continue
		}
		counts[path]++
	}
	ranked := make([]domain.GitHotspot, 0, len(counts))
	for p, n := range counts {
		if n < 3 {
			continue
		}
		ranked = append(ranked, domain.GitHotspot{Path: p, Commits: n})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].Commits != ranked[j].Commits {
			return ranked[i].Commits > ranked[j].Commits
		}
		return ranked[i].Path < ranked[j].Path
	})
	if len(ranked) > 10 {
		ranked = ranked[:10]
	}
	return ranked
}

func gitOut(ctx context.Context, root string, args ...string) string {
	out, err := runGit(ctx, root, args...)
	if err != nil {
		return ""
	}
	return out
}

func nonEmptyLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

func countLines(s string) int { return len(nonEmptyLines(s)) }
