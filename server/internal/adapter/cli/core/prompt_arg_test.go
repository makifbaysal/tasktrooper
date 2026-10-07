package core

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"
)

type PromptArgSuite struct {
	suite.Suite
}

func TestPromptArgSuite(t *testing.T) {
	suite.Run(t, new(PromptArgSuite))
}

func (s *PromptArgSuite) TestShortPromptStaysOnTheCommandLine() {
	got, cleanup, err := argvSafePrompt("windows", "fix the login bug")
	defer cleanup()
	s.Require().NoError(err)
	s.Equal("fix the login bug", got)
}

func (s *PromptArgSuite) TestLongPromptMovesToAFileOnWindows() {
	long := strings.Repeat("ş", 31_000)

	got, cleanup, err := argvSafePrompt("windows", long)
	s.Require().NoError(err)

	s.Less(len(got), 1000)
	path := got[strings.Index(got, "file ")+5 : strings.Index(got, " (")]
	body, readErr := os.ReadFile(path)
	s.Require().NoError(readErr)
	s.Equal(long, string(body))
	s.Contains(got, "31000 characters")

	cleanup()
	_, statErr := os.Stat(path)
	s.True(os.IsNotExist(statErr), "cleanup removes the prompt file")
}

func (s *PromptArgSuite) TestTheSamePromptFitsElsewhere() {
	long := strings.Repeat("x", 31_000)
	got, cleanup, err := argvSafePrompt("linux", long)
	defer cleanup()
	s.Require().NoError(err)
	s.Equal(long, got)
}
