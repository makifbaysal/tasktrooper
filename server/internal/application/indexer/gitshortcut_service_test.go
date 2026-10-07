package indexer_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/chunker"
	"github.com/makifbaysal/tasktrooper/server/internal/application/indexer"
	"github.com/makifbaysal/tasktrooper/server/internal/application/mapper"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/stretchr/testify/suite"
)

type GitShortcutServiceSuite struct {
	suite.Suite
	store     *fakeIndexStore
	svc       *indexer.Service
	root      string
	sessionID uuid.UUID

	mu       sync.Mutex
	embedded map[string]struct{}
}

func (s *GitShortcutServiceSuite) SetupTest() {
	if _, err := exec.LookPath("git"); err != nil {
		s.T().Skip("git is not on PATH")
	}
	s.T().Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	s.T().Setenv("GIT_CONFIG_NOSYSTEM", "1")
	s.root = s.T().TempDir()
	s.git("init", "-q")
	s.write("a.go", "package a\n\nfunc A() int { return 1 }\n")
	s.write("b.go", "package a\n\nfunc B() int { return 2 }\n")
	s.commitAll("initial")

	s.store = newFakeIndexStore()
	s.sessionID = uuid.New()
	s.embedded = map[string]struct{}{}
	llm := &fakeLLM{embedFn: func(_ context.Context, input string, _ string) ([]float32, error) {
		s.mu.Lock()
		s.embedded[strings.Fields(input)[0]] = struct{}{}
		s.mu.Unlock()
		return []float32{float32(len(input))}, nil
	}}
	s.svc = indexer.NewService(
		s.store,
		llm,
		mapper.NewService(domain.MappingConfig{Enabled: true, MaxFiles: 50, TreeMaxDepth: 4}),
		chunker.DefaultRegistry(),
		domain.IndexerConfig{Enabled: true, TopK: 5, ReindexOnChange: true},
		domain.GraphConfig{Enabled: true},
		"embed-model",
	)
}

func (s *GitShortcutServiceSuite) git(args ...string) string {
	argv := append([]string{"-C", s.root, "-c", "user.name=Indexer Test", "-c", "user.email=indexer@example.com", "-c", "commit.gpgsign=false"}, args...)
	cmd := exec.Command("git", argv...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	s.Require().NoError(err, stderr.String())
	return strings.TrimSpace(string(out))
}

func (s *GitShortcutServiceSuite) write(rel, content string) {
	s.Require().NoError(os.WriteFile(filepath.Join(s.root, rel), []byte(content), 0o644))
}

func (s *GitShortcutServiceSuite) commitAll(message string) string {
	s.git("add", "-A")
	s.git("commit", "-q", "-m", message)
	return s.git("rev-parse", "HEAD")
}

func (s *GitShortcutServiceSuite) indexPass() (domain.WorkspaceIndex, []string) {
	s.mu.Lock()
	s.embedded = map[string]struct{}{}
	s.mu.Unlock()

	idx, err := s.svc.IndexSession(context.Background(), s.sessionID, s.root)
	s.Require().NoError(err)
	s.Require().Equal(domain.IndexStatusCompleted, idx.Status)

	s.mu.Lock()
	defer s.mu.Unlock()
	files := make([]string, 0, len(s.embedded))
	for path := range s.embedded {
		files = append(files, path)
	}
	return idx, files
}

func (s *GitShortcutServiceSuite) TestOnlyFilesGitReportsChangedAreCompared() {
	first, embedded := s.indexPass()
	s.ElementsMatch([]string{"a.go", "b.go"}, embedded)
	s.Equal(s.git("rev-parse", "HEAD"), first.CommitSHA)

	// A wrong stored hash for a.go would make a full comparison re-embed it.
	s.Require().NoError(s.store.SaveFileHashes(context.Background(), first.ID, []domain.WorkspaceFileHash{{FilePath: "a.go", Hash: "not compared"}}))
	s.write("b.go", "package a\n\nfunc B() int { return 3 }\n")
	head := s.commitAll("edit b")

	second, embedded := s.indexPass()
	s.ElementsMatch([]string{"b.go"}, embedded)
	s.Equal(head, second.CommitSHA)
}

func (s *GitShortcutServiceSuite) TestPassOverUncommittedEditIsNotTrusted() {
	s.indexPass()

	s.write("a.go", "package a\n\nfunc A() int { return 100 }\n")
	_, embedded := s.indexPass()
	s.ElementsMatch([]string{"a.go"}, embedded)

	s.git("checkout", "--", "a.go")
	_, embedded = s.indexPass()
	s.ElementsMatch([]string{"a.go"}, embedded, "the reverted file is re-indexed, not trusted to git")
}

func (s *GitShortcutServiceSuite) TestPushThatTouchesNoIndexableFileStampsTheCommit() {
	s.indexPass()

	s.write("README.md", "# docs\n")
	head := s.commitAll("docs")

	second, embedded := s.indexPass()
	s.Empty(embedded)
	s.Equal(head, second.CommitSHA)
	stored, err := s.store.GetIndexBySession(context.Background(), s.sessionID)
	s.Require().NoError(err)
	s.Equal(head, stored.CommitSHA)
}

func TestGitShortcutServiceSuite(t *testing.T) {
	suite.Run(t, new(GitShortcutServiceSuite))
}
