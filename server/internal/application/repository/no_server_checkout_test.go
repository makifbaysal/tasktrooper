package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func newNoCheckoutService(t *testing.T, repo domain.Repository) (*Service, *fakeReleaseRepoStore, *fakeRestoreGit) {
	t.Helper()
	git := &fakeRestoreGit{}
	store := &fakeReleaseRepoStore{repo: repo, getByRootPathErr: errors.New("not found")}
	svc := &Service{
		repos:         store,
		git:           git,
		workspaceRoot: t.TempDir(),
		gitWarnings:   map[uuid.UUID]string{},
		restoreRun:    func(fn func()) { fn() },
	}
	svc.SetNoServerCheckout()
	return svc, store, git
}

func TestImportWithoutServerCheckoutRecordsTheRemoteAndClonesNothing(t *testing.T) {
	svc, store, git := newNoCheckoutService(t, domain.Repository{})

	got, err := svc.ImportFromGitHub(context.Background(), domain.ImportGitHubRepositoryRequest{Owner: "acme", Name: "shop"})
	if err != nil {
		t.Fatalf("ImportFromGitHub: %v", err)
	}
	if len(git.clones) != 0 {
		t.Fatalf("clones = %v, want none: the code lives on members' computers", git.clones)
	}
	if store.repo.RemoteURL != "https://github.com/acme/shop.git" {
		t.Fatalf("remote_url = %q, want the GitHub clone URL", store.repo.RemoteURL)
	}
	if got.GitWarning != "" {
		t.Fatalf("git_warning = %q, want none for a server that keeps no checkout", got.GitWarning)
	}
}

func TestNoServerCheckoutRefusesRestoreAndSkipsTheIndexMirror(t *testing.T) {
	repo := missingRepo()
	svc, _, git := newNoCheckoutService(t, repo)

	if _, err := svc.RestoreWorkingCopy(context.Background(), repo.ID); err == nil {
		t.Fatal("restore was allowed on a server that keeps no checkout")
	}
	if err := svc.EnsureIndexMirror(context.Background(), repo.ID, repo.RootPath); err != nil {
		t.Fatalf("EnsureIndexMirror: %v, want a no-op", err)
	}
	if len(git.clones) != 0 {
		t.Fatalf("clones = %v, want none", git.clones)
	}
}
