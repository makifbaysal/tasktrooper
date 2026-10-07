package indexer

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type GitChangesSuite struct {
	suite.Suite
	ctx  context.Context
	root string
}

func (s *GitChangesSuite) SetupTest() {
	if _, err := exec.LookPath("git"); err != nil {
		s.T().Skip("git is not on PATH")
	}
	s.T().Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	s.T().Setenv("GIT_CONFIG_NOSYSTEM", "1")
	s.ctx = context.Background()
	s.root = s.T().TempDir()
	s.git("init", "-q")
}

func (s *GitChangesSuite) git(args ...string) string {
	argv := append([]string{"-C", s.root, "-c", "user.name=Indexer Test", "-c", "user.email=indexer@example.com", "-c", "commit.gpgsign=false"}, args...)
	cmd := exec.Command("git", argv...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	s.Require().NoError(err, stderr.String())
	return strings.TrimSpace(string(out))
}

func (s *GitChangesSuite) write(rel, content string) {
	full := filepath.Join(s.root, filepath.FromSlash(rel))
	s.Require().NoError(os.MkdirAll(filepath.Dir(full), 0o755))
	s.Require().NoError(os.WriteFile(full, []byte(content), 0o644))
}

func (s *GitChangesSuite) commitAll(message string) string {
	s.git("add", "-A")
	s.git("commit", "-q", "-m", message)
	return s.git("rev-parse", "HEAD")
}

func (s *GitChangesSuite) suspectsSince(since string, paths []string) []string {
	state, ok := readGitTreeState(s.ctx, s.root)
	s.Require().True(ok)
	suspects, ok := gitSuspects(s.ctx, s.root, since, state, paths)
	s.Require().True(ok)
	return setKeys(suspects)
}

func (s *GitChangesSuite) TestSuspectsCoverEveryWayAFileCanDiffer() {
	s.write("keep.go", "package keep\n")
	s.write("later.go", "package later\n")
	s.write("unstaged.go", "package unstaged\n")
	s.write("staged.go", "package staged\n")
	s.write("dir/spa ced ü.go", "package spaced\n")
	indexed := s.commitAll("indexed")

	s.write("later.go", "package later // edited\n")
	s.write("dir/spa ced ü.go", "package spaced // edited\n")
	head := s.commitAll("later")

	s.write("unstaged.go", "package unstaged // edited\n")
	s.write("staged.go", "package staged // edited\n")
	s.git("add", "staged.go")
	s.write("new fïle.go", "package fresh\n")
	s.write("excluded.go", "package excluded\n")
	s.Require().NoError(os.WriteFile(filepath.Join(s.root, ".git", "info", "exclude"), []byte("excluded.go\n"), 0o644))

	state, ok := readGitTreeState(s.ctx, s.root)
	s.Require().True(ok)
	s.Equal(head, state.head)
	s.NotContains(state.dirty, "excluded.go", "git status is silent about the excluded file")

	paths := []string{"keep.go", "later.go", "unstaged.go", "staged.go", "dir/spa ced ü.go", "new fïle.go", "excluded.go"}
	s.ElementsMatch(
		[]string{"later.go", "dir/spa ced ü.go", "unstaged.go", "staged.go", "new fïle.go", "excluded.go"},
		s.suspectsSince(indexed, paths),
	)
}

func (s *GitChangesSuite) TestSymlinksAndAssumeUnchangedFilesAreAlwaysSuspects() {
	s.write("plain.go", "package plain\n")
	s.write("target.go", "package target\n")
	s.write("hidden.go", "package hidden\n")
	if err := os.Symlink("target.go", filepath.Join(s.root, "link.go")); err != nil {
		s.T().Skipf("symlinks unavailable: %v", err)
	}
	indexed := s.commitAll("indexed")
	s.git("update-index", "--assume-unchanged", "hidden.go")
	s.write("hidden.go", "package hidden // edited\n")

	s.ElementsMatch(
		[]string{"link.go", "hidden.go"},
		s.suspectsSince(indexed, []string{"plain.go", "target.go", "link.go", "hidden.go"}),
	)
}

func (s *GitChangesSuite) TestShortcutUnavailable() {
	s.write("dir/a.go", "package a\n")
	indexed := s.commitAll("indexed")
	paths := []string{"dir/a.go"}

	tests := []struct {
		name  string
		root  string
		since string
	}{
		{name: "garbage commit", root: s.root, since: "not-a-commit"},
		{name: "unknown commit", root: s.root, since: strings.Repeat("0123456789", 4)},
		{name: "empty commit", root: s.root, since: ""},
		{name: "subdirectory root", root: filepath.Join(s.root, "dir"), since: indexed},
		{name: "not a repository", root: s.T().TempDir(), since: indexed},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			state, ok := readGitTreeState(s.ctx, tt.root)
			if ok {
				_, ok = gitSuspects(s.ctx, tt.root, tt.since, state, paths)
			}
			s.False(ok)
		})
	}
}

func TestGitChangesSuite(t *testing.T) {
	suite.Run(t, new(GitChangesSuite))
}

func TestParsePorcelainZ(t *testing.T) {
	tests := []struct {
		name string
		out  string
		want []string
		ok   bool
	}{
		{name: "clean", out: "", want: nil, ok: true},
		{name: "modified, staged and untracked", out: " M a.go\x00M  b.go\x00?? c d.go\x00", want: []string{"a.go", "b.go", "c d.go"}, ok: true},
		{name: "rename carries its source", out: "R  new.go\x00old.go\x00 M x.go\x00", want: []string{"new.go", "old.go", "x.go"}, ok: true},
		{name: "copy carries its source", out: " C copy.go\x00orig.go\x00", want: []string{"copy.go", "orig.go"}, ok: true},
		{name: "rename without its source", out: "R  new.go\x00", ok: false},
		{name: "malformed entry", out: "M\x00", ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parsePorcelainZ([]byte(tt.out))
			require.Equal(t, tt.ok, ok)
			if tt.ok {
				assert.ElementsMatch(t, tt.want, got)
			}
		})
	}
}

func TestParseLsFilesZ(t *testing.T) {
	const object = "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391"
	tests := []struct {
		name string
		out  string
		want []string
		ok   bool
	}{
		{
			name: "only cached regular files are vouchable",
			out: strings.Join([]string{
				"H 100644 " + object + " 0\tplain.go",
				"H 100755 " + object + " 0\tscript.py",
				"H 120000 " + object + " 0\tlink.go",
				"H 160000 " + object + " 0\tsubmodule",
				"h 100644 " + object + " 0\tassumed.go",
				"S 100644 " + object + " 0\tsparse.go",
				"M 100644 " + object + " 2\tconflict.go",
				"H 100644 " + object + " 0\ttab\there.go",
			}, "\x00") + "\x00",
			want: []string{"plain.go", "script.py", "tab\there.go"},
			ok:   true,
		},
		{name: "entry without a path", out: "H 100644 " + object + " 0\x00", ok: false},
		{name: "entry with missing fields", out: "H 100644\tplain.go\x00", ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseLsFilesZ([]byte(tt.out))
			require.Equal(t, tt.ok, ok)
			if tt.ok {
				assert.ElementsMatch(t, tt.want, setKeys(got))
			}
		})
	}
}

func TestDetectChangedFilesReadsOnlySuspects(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		require.NoError(t, os.WriteFile(filepath.Join(root, rel), []byte(content), 0o644))
	}
	write("stale.go", "package stale\n")
	write("touched.go", "package touched\n")
	write("edited.go", "package edited // new\n")
	touched, err := HashFile(root, "touched.go")
	require.NoError(t, err)

	paths := []string{"vouched.go", "stale.go", "touched.go", "edited.go", "unreadable.go", "fresh.go"}
	stored := map[string]string{
		"vouched.go":    "bytes long gone from disk",
		"stale.go":      "not the file's hash",
		"touched.go":    touched,
		"edited.go":     "hash of the old bytes",
		"unreadable.go": "hash of the old bytes",
		"gone.go":       "hash of a removed file",
	}
	suspects := map[string]struct{}{"touched.go": {}, "edited.go": {}, "unreadable.go": {}, "fresh.go": {}}

	changes := detectChangedFiles(root, paths, stored, func(rel string) bool {
		_, suspect := suspects[rel]
		return !suspect
	})
	assert.ElementsMatch(t, []string{"vouched.go", "stale.go", "touched.go"}, changes.Unchanged)
	assert.ElementsMatch(t, []string{"edited.go", "unreadable.go"}, changes.Changed)
	assert.ElementsMatch(t, []string{"fresh.go"}, changes.Added)
	assert.ElementsMatch(t, []string{"gone.go"}, changes.Removed)

	full := DetectChangedFiles(root, paths, stored)
	assert.ElementsMatch(t, []string{"touched.go"}, full.Unchanged)
	assert.ElementsMatch(t, []string{"vouched.go", "stale.go", "edited.go", "unreadable.go", "fresh.go"}, full.Changed)
	assert.Empty(t, full.Added)
	assert.ElementsMatch(t, []string{"gone.go"}, full.Removed)
}

func setKeys(set map[string]struct{}) []string {
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	return keys
}
