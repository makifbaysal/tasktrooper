package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type originOnlyGit struct {
	port.GitClient
	info  domain.TaskGitInfo
	err   error
	calls int
}

func (g *originOnlyGit) TaskGitInfo(context.Context, string) (domain.TaskGitInfo, error) {
	g.calls++
	return g.info, g.err
}

func TestRepoCoordinatesPrefersThePersistedRemote(t *testing.T) {
	git := &originOnlyGit{err: errors.New("no checkout")}
	owner, name, err := repoCoordinates(context.Background(), git, domain.Repository{
		RemoteURL: "https://github.com/acme/shop.git",
		RootPath:  "/nowhere",
	})
	if err != nil || owner != "acme" || name != "shop" {
		t.Fatalf("got %q/%q, %v; want acme/shop", owner, name, err)
	}
	if git.calls != 0 {
		t.Fatalf("checkout was read %d times; the persisted remote should answer alone", git.calls)
	}
}

func TestRepoCoordinatesFallsBackToTheCheckoutOrigin(t *testing.T) {
	git := &originOnlyGit{info: domain.TaskGitInfo{Owner: "acme", Repo: "api"}}
	owner, name, err := repoCoordinates(context.Background(), git, domain.Repository{RootPath: "/repo"})
	if err != nil || owner != "acme" || name != "api" {
		t.Fatalf("got %q/%q, %v; want acme/api", owner, name, err)
	}
}

func TestRepoCoordinatesReportsAMissingCheckout(t *testing.T) {
	git := &originOnlyGit{err: errors.New("not a git repository")}
	if _, _, err := repoCoordinates(context.Background(), git, domain.Repository{RootPath: "/gone"}); err == nil {
		t.Fatal("want an error when neither the remote nor the checkout answers")
	}
}
