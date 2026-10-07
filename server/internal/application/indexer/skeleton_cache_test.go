package indexer

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/mapper"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

func TestSkeletonCacheEvictsTheLeastRecentlyUsed(t *testing.T) {
	c := newSkeletonCache(2)
	c.put("a", "A")
	c.put("b", "B")
	_, _ = c.get("a")
	c.put("c", "C")

	_, hasB := c.get("b")
	a, hasA := c.get("a")
	assert.False(t, hasB, "b was the least recently used")
	assert.True(t, hasA)
	assert.Equal(t, "A", a)
	assert.Equal(t, 2, c.order.Len())
}

// edgeCountingStore counts skeleton builds: each one reads the index's edges
// for its fan-in ranking, and a cache hit reads nothing.
type edgeCountingStore struct {
	port.IndexStore
	builds int
}

func (s *edgeCountingStore) ListEdges(context.Context, uuid.UUID) ([]domain.WorkspaceEdge, error) {
	s.builds++
	return nil, nil
}

type SkeletonCacheSuite struct {
	suite.Suite
	root     string
	store    *edgeCountingStore
	injector *Injector
	idx      domain.WorkspaceIndex
}

func TestSkeletonCacheSuite(t *testing.T) {
	suite.Run(t, new(SkeletonCacheSuite))
}

func (s *SkeletonCacheSuite) SetupTest() {
	if _, err := exec.LookPath("git"); err != nil {
		s.T().Skip("git is not on PATH")
	}
	s.T().Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	s.T().Setenv("GIT_CONFIG_NOSYSTEM", "1")
	s.root = s.T().TempDir()
	s.git("init", "-q")
	s.write("svc.go", "package svc\n\nfunc FirstName() {}\n")
	s.commitAll("first")
	s.store = &edgeCountingStore{}
	s.injector = NewInjector(s.store, nil, mapper.NewService(domain.MappingConfig{Enabled: true}), "", domain.GraphConfig{})
	s.idx = domain.WorkspaceIndex{ID: uuid.New(), CommitSHA: "c1", RootPath: s.root}
}

func (s *SkeletonCacheSuite) git(args ...string) {
	argv := append([]string{"-C", s.root, "-c", "user.name=Indexer Test", "-c", "user.email=indexer@example.com", "-c", "commit.gpgsign=false"}, args...)
	cmd := exec.Command("git", argv...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	s.Require().NoError(cmd.Run(), stderr.String())
}

func (s *SkeletonCacheSuite) write(rel, content string) {
	s.Require().NoError(os.WriteFile(filepath.Join(s.root, rel), []byte(content), 0o644))
}

func (s *SkeletonCacheSuite) commitAll(message string) {
	s.git("add", "-A")
	s.git("commit", "-q", "-m", message)
}

func (s *SkeletonCacheSuite) skeleton() string {
	return s.injector.skeleton(context.Background(), s.idx, s.root)
}

func (s *SkeletonCacheSuite) TestAnUnchangedWorkspaceReusesTheSkeleton() {
	first := s.skeleton()

	s.Equal(first, s.skeleton())
	s.Contains(first, "FirstName")
	s.Equal(1, s.store.builds)
}

func (s *SkeletonCacheSuite) TestAnUncommittedEditInTheWorkspaceIsSeen() {
	s.skeleton()
	s.write("svc.go", "package svc\n\nfunc SecondName() {}\n")

	s.Contains(s.skeleton(), "SecondName", "a reviewer must see the developer's change, not the indexed commit")
	s.write("svc.go", "package svc\n\nfunc ThirdNameIsLonger() {}\n")
	s.Contains(s.skeleton(), "ThirdNameIsLonger", "a second edit to an already-dirty file")
	s.Equal(3, s.store.builds)
}

func (s *SkeletonCacheSuite) TestANewCommitOrIndexCommitRebuildsIt() {
	s.skeleton()
	s.write("svc.go", "package svc\n\nfunc Committed() {}\n")
	s.commitAll("second")

	s.Contains(s.skeleton(), "Committed")
	s.idx.CommitSHA = "c2"
	s.skeleton()
	s.Equal(3, s.store.builds)
}

func (s *SkeletonCacheSuite) TestARootGitCannotVouchForIsNeverCached() {
	s.root = s.T().TempDir()
	s.write("svc.go", "package svc\n\nfunc Plain() {}\n")

	s.Contains(s.skeleton(), "Plain")
	s.write("svc.go", "package svc\n\nfunc Edited() {}\n")
	s.Contains(s.skeleton(), "Edited")
	s.Equal(2, s.store.builds)
}

func (s *SkeletonCacheSuite) TestTheFingerprintIgnoresNothingGitReports() {
	before, ok := worktreeFingerprint(context.Background(), s.root)
	s.Require().True(ok)
	again, _ := worktreeFingerprint(context.Background(), s.root)
	s.Equal(before, again)

	s.write("new.go", "package svc\n")
	untracked, _ := worktreeFingerprint(context.Background(), s.root)
	s.NotEqual(before, untracked)

	s.Require().NoError(os.Remove(filepath.Join(s.root, "new.go")))
	s.Require().NoError(os.Remove(filepath.Join(s.root, "svc.go")))
	deleted, _ := worktreeFingerprint(context.Background(), s.root)
	s.NotEqual(before, deleted)
}
