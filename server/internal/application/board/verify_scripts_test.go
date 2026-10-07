package board

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// Verify commands come from CI files written for a POSIX runner; they must
// run as scripts on every host, Windows included, where a .sh only runs once
// it is handed to Git Bash.
type VerifyScriptsSuite struct {
	suite.Suite
}

func TestVerifyScriptsSuite(t *testing.T) {
	suite.Run(t, new(VerifyScriptsSuite))
}

func (s *VerifyScriptsSuite) workspaceWithScript(body string) string {
	dir := s.T().TempDir()
	s.Require().NoError(os.MkdirAll(filepath.Join(dir, "scripts"), 0o755))
	s.Require().NoError(os.WriteFile(filepath.Join(dir, "scripts", "ci.sh"), []byte("#!/bin/sh\n"+body+"\n"), 0o755))
	return dir
}

func (s *VerifyScriptsSuite) TestRepoRelativeScriptsRun() {
	for _, command := range []string{"./scripts/ci.sh", "sh scripts/ci.sh", "bash scripts/ci.sh"} {
		s.Run(command, func() {
			if _, err := exec.LookPath("bash"); command == "bash scripts/ci.sh" && runtime.GOOS != "windows" && err != nil {
				s.T().Skip("bash is not installed")
			}
			dir := s.workspaceWithScript("exit 0")

			ok, report := runVerification(context.Background(), dir, domain.Repository{VerifyCommand: command}, nil)

			s.True(ok, report)
			s.Empty(report)
		})
	}
}

func (s *VerifyScriptsSuite) TestAFailingScriptIsAFailure() {
	dir := s.workspaceWithScript("echo broken build >&2\nexit 2")

	ok, report := runVerification(context.Background(), dir, domain.Repository{VerifyCommand: "./scripts/ci.sh"}, nil)

	s.False(ok)
	s.Contains(report, "broken build")
}

func (s *VerifyScriptsSuite) TestAMissingToolIsUnverifiedNotFailed() {
	dir := s.T().TempDir()

	ok, report := runVerification(context.Background(), dir, domain.Repository{VerifyCommand: "tasktrooper-no-such-tool --check"}, nil)

	s.True(ok)
	s.Contains(report, "[unverified]")
	s.Contains(report, "tasktrooper-no-such-tool is not installed in this environment")
}
