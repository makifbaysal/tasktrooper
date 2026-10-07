package mapper

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"
)

type WalkerSuite struct {
	suite.Suite
	fixtureRoot string
}

func TestWalkerSuite(t *testing.T) {
	suite.Run(t, new(WalkerSuite))
}

func (s *WalkerSuite) SetupSuite() {
	s.fixtureRoot = filepath.Join("testdata", "sample")
}

func (s *WalkerSuite) TestWalkDefaultIgnoresGitignore() {
	paths, err := Walk(s.fixtureRoot, WalkOptions{UseGitignore: true})
	s.Require().NoError(err)
	s.Contains(paths, "pkg/main.go")
	s.Contains(paths, "web/app.ts")
	s.Contains(paths, "scripts/run.py")
	s.Contains(paths, "readme.md")
	s.NotContains(paths, "debug.log")
	s.NotContains(paths, "ignored/secret.txt")
}

func (s *WalkerSuite) TestWalkWithoutGitignoreIncludesLog() {
	paths, err := Walk(s.fixtureRoot, WalkOptions{UseGitignore: false})
	s.Require().NoError(err)
	s.Contains(paths, "debug.log")
}

func (s *WalkerSuite) TestWalkMissingRoot() {
	_, err := Walk(filepath.Join("testdata", "missing"), WalkOptions{})
	s.Error(err)
}

func (s *WalkerSuite) TestWalkExtraIgnoreFiles() {
	root := s.T().TempDir()
	s.Require().NoError(os.MkdirAll(filepath.Join(root, "skip"), 0o755))
	s.Require().NoError(os.WriteFile(filepath.Join(root, "skip", "a.go"), []byte("x"), 0o644))
	s.Require().NoError(os.WriteFile(filepath.Join(root, "keep.go"), []byte("x"), 0o644))
	s.Require().NoError(os.WriteFile(filepath.Join(root, ".extraignore"), []byte("skip/\n"), 0o644))

	paths, err := Walk(root, WalkOptions{ExtraIgnoreFiles: []string{".extraignore", ".absent"}})
	s.Require().NoError(err)
	s.Contains(paths, "keep.go")
	s.NotContains(paths, "skip/a.go")
}

func (s *WalkerSuite) TestWalkSubdirMatchesTheFullWalkFilteredToIt() {
	full, err := Walk(s.fixtureRoot, WalkOptions{UseGitignore: true})
	s.Require().NoError(err)
	var want []string
	for _, p := range full {
		if filepath.Dir(filepath.FromSlash(p)) == "pkg" {
			want = append(want, p)
		}
	}

	for _, sub := range []string{"pkg", "pkg/", "./pkg", "web/../pkg"} {
		got, err := Walk(s.fixtureRoot, WalkOptions{UseGitignore: true, Subdir: sub})
		s.Require().NoError(err, sub)
		s.Equal(want, got, sub)
	}
}

func (s *WalkerSuite) TestWalkSubdirNeverListsWhatTheFullWalkSkips() {
	tests := []struct {
		name string
		sub  string
	}{
		{"gitignored directory", "ignored"},
		{"default-ignored directory", "node_modules/pkg"},
		{"outside the root", "../mapper"},
		{"missing directory", "nope"},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			got, err := Walk(s.fixtureRoot, WalkOptions{UseGitignore: true, Subdir: tt.sub})
			s.Require().NoError(err)
			s.Empty(got)
		})
	}
}

func (s *WalkerSuite) TestWalkSubdirNamingAFileReturnsJustThatFile() {
	got, err := Walk(s.fixtureRoot, WalkOptions{UseGitignore: true, Subdir: "pkg/main.go"})
	s.Require().NoError(err)
	s.Equal([]string{"pkg/main.go"}, got)
}

func (s *WalkerSuite) TestWalkSubdirNeverFollowsASymlink() {
	outside := s.T().TempDir()
	s.Require().NoError(os.MkdirAll(filepath.Join(outside, "secret"), 0o755))
	s.Require().NoError(os.WriteFile(filepath.Join(outside, "secret", "id_rsa"), []byte("x"), 0o600))
	root := s.T().TempDir()
	s.Require().NoError(os.MkdirAll(filepath.Join(root, "pkg", "inner"), 0o755))
	s.Require().NoError(os.WriteFile(filepath.Join(root, "pkg", "inner", "a.go"), []byte("package inner"), 0o644))
	if err := os.Symlink(outside, filepath.Join(root, "out")); err != nil {
		s.T().Skipf("symlinks unavailable: %v", err)
	}
	s.Require().NoError(os.Symlink(filepath.Join(root, "pkg"), filepath.Join(root, "alias")))
	full, err := Walk(root, WalkOptions{UseGitignore: true})
	s.Require().NoError(err)
	s.ElementsMatch([]string{"pkg/inner/a.go", "out", "alias"}, full)

	tests := []struct {
		sub  string
		want []string
	}{
		{"out/secret", nil},
		{"out/secret/id_rsa", nil},
		{"alias/inner", nil},
		{"out", []string{"out"}},
		{"pkg/inner", []string{"pkg/inner/a.go"}},
	}
	for _, tt := range tests {
		s.Run(tt.sub, func() {
			got, err := Walk(root, WalkOptions{UseGitignore: true, Subdir: tt.sub})
			s.Require().NoError(err)
			s.Equal(tt.want, got)
		})
	}
}
