package repository

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func monorepoLayout(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"apps/api/go.mod":       "module example.com/api\n",
		"apps/web/package.json": `{"dependencies":{"react":"^18.0.0"}}`,
	}
	for rel, content := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(content), 0o644))
	}
	return root
}

func newOpenTestService(getByRootPathErr error) (*Service, *fakeReleaseRepoStore) {
	repos := &fakeReleaseRepoStore{getByRootPathErr: getByRootPathErr}
	svc := &Service{
		repos: repos,
		git:   &fakeReleaseGit{hasGit: true},
	}
	return svc, repos
}

// Open used to run DetectRepoKind/DetectRepoSubProjects/DetectMobilePlatform/
// DetectAppIdentity/DetectBuildTargets and persist whatever they found. A scan
// (kicked off through the ModelRefresher, when one is wired) now projects all
// of that from the component model instead, so Open itself must never write
// it — the row is created with kind "" and left for the scan to fill in.
func TestServiceOpen_NeverDetectsKindOrSubProjects(t *testing.T) {
	svc, repos := newOpenTestService(errors.New("not found"))
	root := monorepoLayout(t)

	svc.allowedRoots = []string{root}

	repo, err := svc.Open(context.Background(), domain.OpenRepositoryRequest{RootPath: root})
	require.NoError(t, err)

	require.Empty(t, repo.Kind)
	require.Empty(t, repo.SubProjects)
	require.Empty(t, repos.subProjectWrites)
}

func TestServiceOpen_KeepsAnExplicitKindWithoutDetecting(t *testing.T) {
	svc, repos := newOpenTestService(errors.New("not found"))
	root := monorepoLayout(t)

	svc.allowedRoots = []string{root}

	repo, err := svc.Open(context.Background(), domain.OpenRepositoryRequest{RootPath: root, Kind: domain.RepoKindBackend})
	require.NoError(t, err)

	require.Equal(t, domain.RepoKindBackend, repo.Kind)
	require.Empty(t, repo.SubProjects)
	require.Empty(t, repos.subProjectWrites)
}

func TestServiceOpen_NeverDetectsMobilePlatformOrIdentity(t *testing.T) {
	svc, repos := newOpenTestService(errors.New("not found"))
	root := t.TempDir()
	for rel, body := range map[string]string{
		"pubspec.yaml":             "name: app\ndependencies:\n  flutter:\n    sdk: flutter\n",
		"android/app/build.gradle": "android {\n    defaultConfig {\n        applicationId \"com.acme.app\"\n    }\n}\n",
	} {
		full := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(body), 0o644))
	}

	svc.allowedRoots = []string{root}

	repo, err := svc.Open(context.Background(), domain.OpenRepositoryRequest{RootPath: root, Kind: domain.RepoKindMobile})
	require.NoError(t, err)

	require.Empty(t, repo.MobilePlatform)
	require.Empty(t, repos.mobilePlatformWrites)
	require.Equal(t, domain.AppIdentity{}, repo.DetectedAppIdentity)
	require.Empty(t, repos.appIdentityWrites)
	require.Equal(t, domain.BuildTargets{}, repo.DetectedBuildTargets)
	require.Empty(t, repos.buildTargetWrites)
}
