package core

import (
	"testing"

	"github.com/stretchr/testify/suite"
)

type MinimalEnvSuite struct {
	suite.Suite
}

func TestMinimalEnvSuite(t *testing.T) {
	suite.Run(t, new(MinimalEnvSuite))
}

var parentEnv = []string{
	"Path=C:\\Windows\\System32;C:\\Users\\me\\AppData\\Roaming\\npm",
	"SystemRoot=C:\\Windows",
	"USERPROFILE=C:\\Users\\me",
	"APPDATA=C:\\Users\\me\\AppData\\Roaming",
	"TEMP=C:\\Users\\me\\AppData\\Local\\Temp",
	"OPENAI_API_KEY=sk-must-not-leak",
	"SERVER_API_KEY=bridge-key",
}

func (s *MinimalEnvSuite) TestUnixKeepsOnlyPathAndHome() {
	got := minimalEnv("linux", []string{"PATH=/usr/bin", "HOME=/home/me", "OPENAI_API_KEY=sk"})
	s.Equal([]string{"PATH=/usr/bin", "HOME=/home/me"}, got)
}

func (s *MinimalEnvSuite) TestWindowsKeepsProcessBasicsAndDerivesHome() {
	got := minimalEnv("windows", parentEnv)

	s.Contains(got, "Path=C:\\Windows\\System32;C:\\Users\\me\\AppData\\Roaming\\npm")
	s.Contains(got, "SystemRoot=C:\\Windows")
	s.Contains(got, "APPDATA=C:\\Users\\me\\AppData\\Roaming")
	s.Contains(got, "TEMP=C:\\Users\\me\\AppData\\Local\\Temp")
	s.Contains(got, "HOME=C:\\Users\\me")
	for _, entry := range got {
		s.NotContains(entry, "must-not-leak")
		s.NotContains(entry, "bridge-key")
	}
}
