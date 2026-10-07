package board

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	gitadapter "github.com/makifbaysal/tasktrooper/server/internal/adapter/vcs/git"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type AdvisoryChecksSuite struct {
	suite.Suite
	workspace string
	commit    string
	comments  *commentRecorder
	runner    *Runner
	job       RunJob
}

func TestAdvisoryChecksSuite(t *testing.T) {
	suite.Run(t, new(AdvisoryChecksSuite))
}

func (s *AdvisoryChecksSuite) SetupTest() {
	for _, bin := range []string{"go", "git"} {
		if _, err := exec.LookPath(bin); err != nil {
			s.T().Skip(bin + " not installed")
		}
	}
	s.workspace = s.T().TempDir()
	files := map[string]string{
		"go.mod":       "module example.com/calc\n\ngo 1.22\n",
		"calc.go":      "package calc\n\nfunc Add(a, b int) int { return a + b }\n\nfunc Sub(a, b int) int { return a - b }\n",
		"calc_test.go": "package calc\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(1, 2) != 3 {\n\t\tt.Fatal(\"add\")\n\t}\n}\n",
	}
	for name, body := range files {
		s.Require().NoError(os.WriteFile(filepath.Join(s.workspace, name), []byte(body), 0o644))
	}
	s.git("init", "-q")
	s.commit = s.commitAll("hand-off")
	s.comments = &commentRecorder{}
	s.runner = NewRunner(RunnerDeps{Git: gitadapter.NewClient()})
	s.runner.SetTaskUpdater(s.comments)
	s.job = RunJob{Task: domain.BoardTask{ID: uuid.New()}, RepositoryID: uuid.New()}
}

func (s *AdvisoryChecksSuite) TearDownTest() {
	if s.runner != nil {
		s.runner.stopBackground()
	}
}

func (s *AdvisoryChecksSuite) git(args ...string) string {
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com"}, args...)...)
	cmd.Dir = s.workspace
	out, err := cmd.CombinedOutput()
	s.Require().NoError(err, string(out))
	return strings.TrimSpace(string(out))
}

func (s *AdvisoryChecksSuite) commitAll(message string) string {
	s.git("add", "-A")
	s.git("commit", "-q", "-m", message)
	return s.git("rev-parse", "HEAD")
}

func (s *AdvisoryChecksSuite) checks() advisoryChecks {
	return advisoryChecks{workspace: s.workspace, coverage: true, commit: s.commit}
}

func (s *AdvisoryChecksSuite) TestUnenforcedChecksAreDeferredNotRun() {
	notes, deferred := enforcedQualityChecks(context.Background(), s.workspace, domain.Repository{})

	s.Empty(notes, "nothing runs inline when the repository enforces nothing")
	s.True(deferred.coverage)
	s.True(deferred.mutation)
	s.NoFileExists(filepath.Join(s.workspace, "coverage.out"))
}

func (s *AdvisoryChecksSuite) TestAnEnforcedCoverageGateStillRunsBeforeTheHandOff() {
	repo := domain.Repository{RequireOverallCoverage: true, CoverageThreshold: 10}

	notes, deferred := enforcedQualityChecks(context.Background(), s.workspace, repo)

	s.Require().Len(notes, 1)
	s.Contains(notes[0], "[coverage] overall 50.0%")
	s.False(deferred.coverage)
	s.True(deferred.mutation, "the mutation gate is not enforced, so it still waits")
}

func (s *AdvisoryChecksSuite) TestTheHandOffCommitIsWhatTheChecksWillMeasure() {
	s.Equal(s.commit, s.runner.handoffCommit(context.Background(), s.checks()))
	s.Empty(s.runner.handoffCommit(context.Background(), advisoryChecks{workspace: s.workspace}), "nothing pending, nothing to read")
}

func (s *AdvisoryChecksSuite) TestTheResultIsPostedAsAnInformationalCommentAfterTheHandOff() {
	s.runner.startAdvisoryChecks(s.job, s.checks())
	s.runner.bgWG.Wait()

	s.Require().Equal(1, s.comments.count())
	comment := s.comments.comments[0]
	s.True(isAdvisoryChecksComment(comment.Content))
	s.Contains(comment.Content, "[coverage] overall 50.0%")
	s.Equal("system", comment.AuthorType)
	s.True(comment.Informational, "the note must not wake the column's agent")
}

func (s *AdvisoryChecksSuite) TestTheChecksRunOnTheHandedOffCommitOutsideTheWorkspace() {
	broken := "package calc\n\nfunc Add(a, b int) int { return a +\n"
	s.Require().NoError(os.WriteFile(filepath.Join(s.workspace, "calc.go"), []byte(broken), 0o644))

	s.runner.startAdvisoryChecks(s.job, s.checks())
	s.runner.bgWG.Wait()

	s.Require().Equal(1, s.comments.count())
	s.Contains(s.comments.comments[0].Content, "[coverage] overall 50.0%", "the next run's uncommitted edits are not what was handed off")
	s.NoFileExists(filepath.Join(s.workspace, "coverage.out"), "nothing the checks write may land where the next run commits")
	s.Equal("M calc.go", s.git("status", "--porcelain"), "only the next run's own edit is in the workspace")
	s.NotContains(s.git("worktree", "list"), "tasktrooper-worktree-", "the temporary checkout is removed")
}

func (s *AdvisoryChecksSuite) TestAWorkspaceThatMovedPastTheHandOffIsSkipped() {
	s.Require().NoError(os.WriteFile(filepath.Join(s.workspace, "README"), []byte("revision\n"), 0o644))
	s.commitAll("revision")

	s.runner.startAdvisoryChecks(s.job, s.checks())
	s.runner.bgWG.Wait()

	s.Zero(s.comments.count())
}

func (s *AdvisoryChecksSuite) TestAReapedWorkspaceIsSkippedQuietly() {
	checks := s.checks()
	checks.workspace = filepath.Join(s.workspace, "gone")
	s.runner.startAdvisoryChecks(s.job, checks)
	s.runner.bgWG.Wait()

	s.Zero(s.comments.count())
}

func (s *AdvisoryChecksSuite) TestWithoutAHandOffCommitNothingStarts() {
	checks := s.checks()
	checks.commit = ""
	s.runner.startAdvisoryChecks(s.job, checks)
	s.runner.bgWG.Wait()

	s.Zero(s.comments.count())
}

func (s *AdvisoryChecksSuite) TestOneAdvisoryJobRunsAtATime() {
	advisorySlot <- struct{}{}
	s.runner.startAdvisoryChecks(s.job, s.checks())

	time.Sleep(300 * time.Millisecond)
	s.Zero(s.comments.count(), "a second job waits while another holds the slot")

	<-advisorySlot
	s.runner.bgWG.Wait()
	s.Equal(1, s.comments.count())
}

func (s *AdvisoryChecksSuite) TestShutdownCancelsTheChecksAndPostsNothing() {
	s.runner.startAdvisoryChecks(s.job, s.checks())
	s.runner.stopBackground()

	s.Zero(s.comments.count())

	s.runner.startAdvisoryChecks(s.job, s.checks())
	s.runner.bgWG.Wait()
	s.Zero(s.comments.count(), "nothing new starts once the runner is stopping")
}

func (s *AdvisoryChecksSuite) TestNothingPendingStartsNothing() {
	s.runner.startAdvisoryChecks(s.job, advisoryChecks{workspace: s.workspace, commit: s.commit})
	s.runner.bgWG.Wait()

	s.Zero(s.comments.count())
}

func (s *AdvisoryChecksSuite) TestTheNoteIsNotRevisionFeedback() {
	comments := []domain.TaskComment{
		{AuthorType: "system", Content: advisoryChecksCommentKey.Render(advisoryChecksCommentInput{Marker: advisoryChecksMarker, Report: "[coverage] overall 50.0%"})},
		{AuthorType: "agent", Content: "Rename parseThing before this can merge."},
	}

	msg := revisionCommentsMessage(comments)

	s.Contains(msg, "parseThing")
	s.NotContains(msg, "coverage")
}
