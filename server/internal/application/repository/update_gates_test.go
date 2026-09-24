package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func newUpdateTestService(repo domain.Repository) (*Service, *fakeReleaseRepoStore) {
	repos := &fakeReleaseRepoStore{repo: repo}
	return &Service{repos: repos}, repos
}

func TestUpdateSetsTheMutationGate(t *testing.T) {
	svc, repos := newUpdateTestService(domain.Repository{ID: uuid.New(), Kind: domain.RepoKindBackend})
	on := true
	threshold := 65.0

	repo, err := svc.Update(context.Background(), repos.repo.ID, domain.UpdateRepositoryRequest{
		MutationEnabled: &on, MutationThreshold: &threshold,
	})
	require.NoError(t, err)
	require.True(t, repo.MutationEnabled)
	require.Equal(t, 65.0, repo.MutationThreshold)
}

func TestUpdateRejectsAMutationThresholdOutside0To100(t *testing.T) {
	svc, repos := newUpdateTestService(domain.Repository{ID: uuid.New(), Kind: domain.RepoKindBackend})
	bad := 140.0

	_, err := svc.Update(context.Background(), repos.repo.ID, domain.UpdateRepositoryRequest{MutationThreshold: &bad})
	require.ErrorContains(t, err, "must be between 0 and 100")
	require.Zero(t, repos.repo.MutationThreshold)
}

func TestUpdateValidatesSubProjectQualityGates(t *testing.T) {
	svc, repos := newUpdateTestService(domain.Repository{ID: uuid.New(), Kind: domain.RepoKindMonorepo})
	bad := -5.0
	subs := []domain.RepoSubProject{{Path: "apps/api", Kind: domain.RepoKindBackend, MutationThreshold: &bad}}

	_, err := svc.Update(context.Background(), repos.repo.ID, domain.UpdateRepositoryRequest{SubProjects: &subs})
	require.ErrorContains(t, err, "mutation_threshold must be between 0 and 100")
	require.Empty(t, repos.subProjectWrites)
}

func TestUpdateSetsTheReleaseEngineOnAMobileRepo(t *testing.T) {
	svc, repos := newUpdateTestService(domain.Repository{ID: uuid.New(), Kind: domain.RepoKindMobile})
	engine := domain.ReleaseEngineLocal

	repo, err := svc.Update(context.Background(), repos.repo.ID, domain.UpdateRepositoryRequest{ReleaseEngine: &engine})
	require.NoError(t, err)
	require.Equal(t, domain.ReleaseEngineLocal, repo.ReleaseEngine)
	require.Equal(t, []string{domain.ReleaseEngineLocal}, repos.releaseEngineWrites)
}

func TestUpdateRejectsAPinnedEngineOnANonMobileRepo(t *testing.T) {
	svc, repos := newUpdateTestService(domain.Repository{ID: uuid.New(), Kind: domain.RepoKindBackend})
	engine := domain.ReleaseEngineActions

	_, err := svc.Update(context.Background(), repos.repo.ID, domain.UpdateRepositoryRequest{ReleaseEngine: &engine})
	require.ErrorContains(t, err, "only meaningful on a mobile project")
	require.Empty(t, repos.releaseEngineWrites)
}

func TestUpdateRejectsAnUnknownReleaseEngine(t *testing.T) {
	svc, repos := newUpdateTestService(domain.Repository{ID: uuid.New(), Kind: domain.RepoKindMobile})
	engine := "jenkins"

	_, err := svc.Update(context.Background(), repos.repo.ID, domain.UpdateRepositoryRequest{ReleaseEngine: &engine})
	require.ErrorContains(t, err, "invalid release engine")
	require.Empty(t, repos.releaseEngineWrites)
}

func TestUpdateWritesAutoForAnEmptyReleaseEngine(t *testing.T) {
	svc, repos := newUpdateTestService(domain.Repository{
		ID: uuid.New(), Kind: domain.RepoKindMobile, ReleaseEngine: domain.ReleaseEngineLocal,
	})
	empty := ""

	repo, err := svc.Update(context.Background(), repos.repo.ID, domain.UpdateRepositoryRequest{ReleaseEngine: &empty})
	require.NoError(t, err)
	require.Equal(t, domain.ReleaseEngineAuto, repo.ReleaseEngine)
	require.Equal(t, []string{domain.ReleaseEngineAuto}, repos.releaseEngineWrites)
}
