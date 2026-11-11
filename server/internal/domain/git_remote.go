package domain

import (
	"errors"
	"net/url"
	"strings"
)

// SameGitRemote reports whether two git remote URLs address the same
// repository. It is the identity test every path that ADOPTS an existing
// checkout applies before acting on one: "there is a git repository at this
// path" and "there is THIS repository at this path" are different questions,
// and only the second is safe to answer yes to. Both GitHub spellings — https
// and the scp-style ssh form — must compare equal, along with the optional .git
// suffix, credentials and case; a comparison that refused legitimate adoptions
// would be turned off.
func SameGitRemote(a, b string) bool {
	na, nb := NormalizeGitRemote(a), NormalizeGitRemote(b)
	return na != "" && na == nb
}

// NormalizeGitRemote reduces a remote URL to "host/path", lowercased, without
// credentials, port, trailing slash or .git suffix. "" for anything it cannot
// read as a remote, which callers treat as "no answer" rather than a match.
func NormalizeGitRemote(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	// scp-style (git@github.com:owner/repo.git) is not a URL, so it is
	// rewritten into one before parsing rather than handled by a second code
	// path that could disagree with the first.
	if !strings.Contains(trimmed, "://") {
		if at := strings.LastIndex(trimmed, "@"); at >= 0 {
			trimmed = trimmed[at+1:]
		}
		trimmed = strings.Replace(trimmed, ":", "/", 1)
		trimmed = "ssh://" + trimmed
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	path := strings.Trim(u.Path, "/")
	path = strings.TrimSuffix(path, ".git")
	if host == "" || path == "" {
		return ""
	}
	return host + "/" + strings.ToLower(path)
}

// ErrNotGitHubRepo is what every GitHub-only step (pull request, CI checks,
// runner registration) answers for a repository whose origin is hosted
// elsewhere. Callers that can carry on with plain git treat it as "skip this
// step"; none retry it.
var ErrNotGitHubRepo = errors.New("not a GitHub repo: this repository's origin is not hosted on github.com, so pull requests, CI checks and the GitHub runner are unavailable (plain git steps still work)")

// IsNonGitHubRemote reports whether a remote URL names a host other than
// github.com. A remote it cannot read (empty, a local path) is not "non-GitHub":
// only a host that is positively something else turns GitHub features off.
func IsNonGitHubRemote(raw string) bool {
	n := NormalizeGitRemote(raw)
	if n == "" {
		return false
	}
	host, _, _ := strings.Cut(n, "/")
	return host != "github.com" && host != "www.github.com"
}
