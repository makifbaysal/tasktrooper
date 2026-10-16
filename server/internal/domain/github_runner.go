package domain

import (
	"fmt"
	"time"
)

// GitHubRunnerConfig is this computer as a self-hosted GitHub Actions runner
// for the listed repositories ("owner/name"), switched on from Settings →
// Integrations → GitHub.
type GitHubRunnerConfig struct {
	Enabled bool     `json:"enabled"`
	Repos   []string `json:"repos"`
}

// GitHubRunnerMaxRepos bounds the list: every repository gets its own runner
// install on disk, a few hundred MB each.
const GitHubRunnerMaxRepos = 5

type GitHubRunnerState string

const (
	GitHubRunnerStopped    GitHubRunnerState = "stopped"
	GitHubRunnerInstalling GitHubRunnerState = "installing"
	GitHubRunnerStarting   GitHubRunnerState = "starting"
	GitHubRunnerIdle       GitHubRunnerState = "idle"
	GitHubRunnerBusy       GitHubRunnerState = "busy"
	GitHubRunnerFailed     GitHubRunnerState = "error"
)

// Error codes the UI words itself; ErrorDetail carries GitHub's own sentence.
const (
	GitHubRunnerErrNotConnected  = "github_not_connected"
	GitHubRunnerErrForbidden     = "forbidden"
	GitHubRunnerErrNotFound      = "not_found"
	GitHubRunnerErrPublicRepo    = "public_repo"
	GitHubRunnerErrNeedsAdmin    = "needs_admin"
	GitHubRunnerErrInvalidRepo   = "invalid_repo"
	GitHubRunnerErrTooManyRepos  = "too_many_repos"
	GitHubRunnerErrUnsupported   = "unsupported_platform"
	GitHubRunnerErrDownload      = "download_failed"
	GitHubRunnerErrStart         = "start_failed"
	GitHubRunnerErrExited        = "exited"
	GitHubRunnerErrGitHubRefused = "github_error"
)

type GitHubRunnerRepoStatus struct {
	Repo           string            `json:"repo"`
	State          GitHubRunnerState `json:"state"`
	Job            string            `json:"job,omitempty"`
	LastJob        string            `json:"last_job,omitempty"`
	LastResult     string            `json:"last_result,omitempty"`
	LastFinishedAt *time.Time        `json:"last_finished_at,omitempty"`
	ErrorCode      string            `json:"error_code,omitempty"`
	ErrorDetail    string            `json:"error_detail,omitempty"`
}

type GitHubRunnerStatus struct {
	// Supported is false where this server cannot run a runner for the person
	// using it: no data directory, or a platform GitHub ships no runner for.
	Supported bool                     `json:"supported"`
	Enabled   bool                     `json:"enabled"`
	Labels    []string                 `json:"labels"`
	Version   string                   `json:"version"`
	Repos     []GitHubRunnerRepoStatus `json:"repos"`
	// Candidates are the GitHub repositories of this install's projects, the
	// list the settings card offers.
	Candidates []string `json:"candidates"`
}

// GitHubRunnerRepoError is a repository Configure refused, with the reason
// as one of the GitHubRunnerErr* codes.
type GitHubRunnerRepoError struct {
	Repo   string
	Code   string
	Detail string
}

func (e *GitHubRunnerRepoError) Error() string {
	if e.Detail == "" {
		return fmt.Sprintf("%s: %s", e.Repo, e.Code)
	}
	return fmt.Sprintf("%s: %s: %s", e.Repo, e.Code, e.Detail)
}
