package runtime

import (
	"path/filepath"

	httpadapter "github.com/makifbaysal/tasktrooper/server/internal/adapter/http"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/local/ghrunner"
	githubapi "github.com/makifbaysal/tasktrooper/server/internal/adapter/vcs/github"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// newGitHubRunner is this computer as a self-hosted Actions runner, for the
// install the desktop app supervises (it has a data directory).
func newGitHubRunner(dataDir string, tokens port.GitHubTokenStore, store ghrunner.Store) *ghrunner.Manager {
	var source githubapi.TokenSource
	if tokens != nil {
		source = tokens.GitHubToken
	}
	return ghrunner.New(ghrunner.Config{
		Dir:   filepath.Join(dataDir, "github-runner"),
		API:   githubapi.NewRunnersAPI(source),
		Store: store,
	})
}

func githubRunnerOrNil(m *ghrunner.Manager) httpadapter.GitHubRunnerService {
	if m == nil {
		return nil
	}
	return m
}
