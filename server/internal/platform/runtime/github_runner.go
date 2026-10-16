package runtime

import (
	"context"
	"path/filepath"

	httpadapter "github.com/makifbaysal/tasktrooper/server/internal/adapter/http"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/local/ghrunner"
	githubapi "github.com/makifbaysal/tasktrooper/server/internal/adapter/vcs/github"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// newGitHubRunner is this computer as a self-hosted Actions runner, for the
// install the desktop app supervises (it has a data directory).
func newGitHubRunner(dataDir string, tokens port.GitHubTokenStore, store ghrunner.Store, repos port.RepositoryStore) *ghrunner.Manager {
	var source githubapi.TokenSource
	if tokens != nil {
		source = tokens.GitHubToken
	}
	return ghrunner.New(ghrunner.Config{
		Dir:   filepath.Join(dataDir, "github-runner"),
		API:   githubapi.NewRunnersAPI(source),
		Store: store,
		Candidates: func(ctx context.Context) ([]string, error) {
			if repos == nil {
				return nil, nil
			}
			list, err := repos.List(ctx)
			if err != nil {
				return nil, err
			}
			out := make([]string, 0, len(list))
			for _, r := range list {
				if owner, name, ok := githubapi.ParseOwnerRepo(r.RemoteURL); ok {
					out = append(out, owner+"/"+name)
				}
			}
			return out, nil
		},
	})
}

func githubRunnerOrNil(m *ghrunner.Manager) httpadapter.GitHubRunnerService {
	if m == nil {
		return nil
	}
	return m
}
