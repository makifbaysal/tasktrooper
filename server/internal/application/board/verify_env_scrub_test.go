package board

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type VerifyEnvScrubSuite struct {
	suite.Suite
}

func TestVerifyEnvScrubSuite(t *testing.T) {
	suite.Run(t, new(VerifyEnvScrubSuite))
}

var verifyPlantedSecrets = map[string]string{
	"DATABASE_URL":      "postgres://tenant:hunter2@10.0.0.5:5432/tenant_x",
	"INTERNAL_AUTH_KEY": "gateway-hmac-key-9f21",
	"MCP_SECRETS_KEY":   "bWNwLXNlY3JldHMta2V5",
	"OPENAI_API_KEY":    "sk-proj-must-not-leak",
	"ANTHROPIC_API_KEY": "sk-ant-must-not-leak",
	"SERVER_API_KEY":    "bridge-server-key",
	"PG_PASSWORD":       "postgres-superuser-password",
}

func (s *VerifyEnvScrubSuite) plantSecrets() {
	for name, value := range verifyPlantedSecrets {
		s.T().Setenv(name, value)
	}
}

func (s *VerifyEnvScrubSuite) assertNoSecrets(output string) {
	for name, value := range verifyPlantedSecrets {
		s.NotContains(output, value, "secret value for %s reached the verify stage", name)
		s.NotContains(output, name+"=", "variable %s reached the verify stage", name)
	}
}

func (s *VerifyEnvScrubSuite) hostileRepo(script string) (string, domain.Repository) {
	dir := s.T().TempDir()
	s.Require().NoError(os.WriteFile(filepath.Join(dir, "leak.sh"), []byte(script+"\nexit 1\n"), 0o700))
	return dir, domain.Repository{VerifyCommand: "sh leak.sh"}
}

func (s *VerifyEnvScrubSuite) TestRepoDeclaredStageGetsNoSecrets() {
	s.plantSecrets()
	dir, repo := s.hostileRepo("env")

	ok, report := runVerification(context.Background(), dir, repo, nil)

	s.False(ok, "the stage exits 1, so verification must report a failure")
	s.assertNoSecrets(report)
}

func (s *VerifyEnvScrubSuite) TestObfuscatedReadsInAStageFindNothing() {
	s.plantSecrets()
	for _, script := range []string{
		`env`,
		`printenv`,
		`e''nv`,
		`export -p`,
		`set`,
		`echo "$INTERNAL_AUTH_KEY$DATABASE_URL$MCP_SECRETS_KEY"`,
		`printf '%s' "$(env)"`,
		`cat /proc/self/environ`,
	} {
		dir, repo := s.hostileRepo(script)
		_, report := runVerification(context.Background(), dir, repo, nil)
		s.assertNoSecrets(report)
	}
}

func (s *VerifyEnvScrubSuite) TestStageKeepsItsToolchain() {
	s.T().Setenv("GOFLAGS", "-mod=mod")
	s.T().Setenv("JAVA_HOME", "/opt/java")
	dir, repo := s.hostileRepo(`echo path=[$PATH] home=[$HOME] goflags=[$GOFLAGS] java=[$JAVA_HOME]`)

	_, report := runVerification(context.Background(), dir, repo, nil)

	s.NotContains(report, "path=[]")
	s.NotContains(report, "home=[]")
	s.Contains(report, "goflags=[-mod=mod]")
	s.Contains(report, "java=[/opt/java]")
}

func (s *VerifyEnvScrubSuite) TestNoToolchainOverlayStillScrubs() {
	s.plantSecrets()
	dir, repo := s.hostileRepo("env")

	ok, report := runVerification(context.Background(), dir, repo, nil)

	s.False(ok)
	s.NotEmpty(report, "the stage's output must reach the report, or this proves nothing")
	s.assertNoSecrets(report)
}
