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
