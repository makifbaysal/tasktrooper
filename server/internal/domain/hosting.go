package domain

import (
	"strings"
	"time"
)

// HostingAreaRoot is the area of a single-kind repository: the whole tree. A
// monorepo uses its sub-repo kinds (RepoKindFrontend, RepoKindBackend…) as
// areas instead. Kept for the cloud package's legacy backfill
// (resolveLegacyComponentByArea), which still reads the pre-cloud-accounts
// repository_hosting_links table this way.
const HostingAreaRoot = ""

// Vercel objects, as the rest of the system needs them — domain types, not
// adapter ones, so the application layer can reason about a project without
// importing the HTTP client.

// VercelUser is the account a token belongs to.
type VercelUser struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email,omitempty"`
	Name     string `json:"name,omitempty"`
}

// VercelTeam is a scope a token can act in.
type VercelTeam struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
	Name string `json:"name"`
}

// VercelGitLink is the repository a Vercel project deploys from.
type VercelGitLink struct {
	Type             string `json:"type"`
	Org              string `json:"org"`
	Repo             string `json:"repo"`
	ProductionBranch string `json:"production_branch,omitempty"`
}

// Slug renders org/repo, lower-cased, for comparison with a git remote.
func (l VercelGitLink) Slug() string {
	if l.Org == "" || l.Repo == "" {
		return ""
	}
	return strings.ToLower(l.Org + "/" + l.Repo)
}

// VercelProject is one project as the link and detection views need it.
type VercelProject struct {
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	Framework     string         `json:"framework,omitempty"`
	RootDirectory string         `json:"root_directory,omitempty"`
	Link          *VercelGitLink `json:"link,omitempty"`
	ProductionURL string         `json:"production_url,omitempty"`
	TeamID        string         `json:"team_id,omitempty"`
	TeamSlug      string         `json:"team_slug,omitempty"`
	UpdatedAt     time.Time      `json:"updated_at,omitempty"`
}
