package postgres_test

import (
	"path/filepath"
	"runtime"
	"strings"
)

// A row a Windows host wrote is C:\...\acme-web; the by-name fallback split
// root_path only on "/", so this host's own path never matched it and opening
// the repository here inserted a second row for it.
func (s *RepositoryRootPathSuite) TestGetByRootPathMatchesAWindowsWrittenRowByName() {
	stored := `C:\Users\someone\code\win-repo`
	if runtime.GOOS == "windows" {
		stored = `Z:\nonexistent-host\code\win-repo`
	}
	created, err := s.store.Create(s.ctx, "win-repo", "", stored, "", "")
	s.Require().NoError(err)
	s.Equal(filepath.Join(s.hostRoot, "repos", "win-repo"), created.RootPath,
		"the re-anchor must take the last segment, not the whole Windows path")

	local, err := s.store.GetByRootPath(s.ctx, filepath.Join(s.hostRoot, "repos", "win-repo"))
	s.Require().NoError(err)
	s.Equal(created.ID, local.ID)
}

func (s *RepositoryRootPathSuite) TestGetByRootPathIgnoresDriveLetterCaseOnWindows() {
	if runtime.GOOS != "windows" {
		s.T().Skip("drive letters only exist on Windows")
	}
	path := filepath.Join(s.T().TempDir(), "drive-case")
	lower := strings.ToLower(path[:1]) + path[1:]
	created, err := s.store.Create(s.ctx, "drive-case", "", lower, "", "")
	s.Require().NoError(err)

	got, err := s.store.GetByRootPath(s.ctx, strings.ToUpper(path[:1])+path[1:])
	s.Require().NoError(err)
	s.Equal(created.ID, got.ID)
}
