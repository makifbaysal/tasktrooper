package runtime

import (
	"context"
	"fmt"

	githubapi "github.com/makifbaysal/tasktrooper/server/internal/adapter/vcs/github"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// repoCoordinates resolves a repository to its GitHub owner/name. The
// persisted RemoteURL answers first, so a server with no checkout of its own
// (the shared cloud pod) never needs a clone just to read the origin; the
// checkout's git origin is the fallback for rows saved before RemoteURL was.
func repoCoordinates(ctx context.Context, git port.GitClient, repo domain.Repository) (owner, name string, err error) {
	if owner, name, ok := githubapi.ParseOwnerRepo(repo.RemoteURL); ok {
		return owner, name, nil
	}
	if git == nil {
		return "", "", fmt.Errorf("repository %s has no GitHub remote and no git client", repo.Name)
	}
	info, err := git.TaskGitInfo(ctx, repo.RootPath)
	if err != nil {
		return "", "", fmt.Errorf("git origin: %w", err)
	}
	return info.Owner, info.Repo, nil
}
