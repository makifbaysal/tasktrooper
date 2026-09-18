package childenv_test

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/platform/childenv"
)

// ChildEnvSuite pins the properties all three call sites depend on. The
// end-to-end proofs live next to each caller (they run real subprocesses and
// read real output); these are the invariants that would otherwise have to be
// re-asserted in each of the three.
type ChildEnvSuite struct {
	suite.Suite
}

func TestChildEnvSuite(t *testing.T) {
	suite.Run(t, new(ChildEnvSuite))
}

// A parent process's environment plus the shapes a future credential is
// likely to arrive in.
var parentWithSecrets = []string{
	"PATH=/usr/bin:/bin",
	"HOME=/home/bridge",
	"DATABASE_URL=postgres://tenant:hunter2@10.0.0.5:5432/tenant_x",
	"INTERNAL_AUTH_KEY=gateway-hmac-key-9f21",
	"MCP_SECRETS_KEY=bWNwLXNlY3JldHMta2V5",
	"OPENAI_API_KEY=sk-proj-must-not-leak",
	"ANTHROPIC_API_KEY=sk-ant-must-not-leak",
	"SERVER_API_KEY=bridge-server-key",
	"GOOGLE_APPLICATION_CREDENTIALS=/var/run/secrets/gcp.json",
	"NODE_AUTH_TOKEN=npm-registry-token",
	"GIT_ASKPASS=/usr/local/bin/askpass",
	"AWS_SECRET_ACCESS_KEY=aws-secret",
	"SOME_FUTURE_CREDENTIAL=not-yet-invented",
}

func (s *ChildEnvSuite) TestNothingUnlistedSurvives() {
	env := childenv.For(parentWithSecrets, nil)

	for _, entry := range env {
		s.NotContains(entry, "hunter2")
		s.NotContains(entry, "gateway-hmac-key-9f21")
		s.NotContains(entry, "bWNwLXNlY3JldHMta2V5")
		s.NotContains(entry, "must-not-leak")
		s.NotContains(entry, "bridge-server-key")
		s.NotContains(entry, "npm-registry-token")
		s.NotContains(entry, "aws-secret")
		s.NotContains(entry, "not-yet-invented")
	}
}

// The allowlist earns its keep on the variable nobody has thought of yet: a
// prefix rule (GO*, NODE_*, GIT_*) would forward these by default.
func (s *ChildEnvSuite) TestPrefixLookalikesAreNotForwarded() {
	for _, name := range []string{
		"GOOGLE_APPLICATION_CREDENTIALS",
		"NODE_AUTH_TOKEN",
		"GIT_ASKPASS",
		"NPM_CONFIG_TOKEN",
		"AWS_SECRET_ACCESS_KEY",
		"SOME_FUTURE_CREDENTIAL",
	} {
		s.False(childenv.IsForwarded(name), "%s must not be forwarded", name)
	}
}

func (s *ChildEnvSuite) TestToolchainSurfaceSurvives() {
	for _, name := range []string{
		"PATH", "HOME", "LANG", "TMPDIR",
		"GOFLAGS", "GOTOOLCHAIN", "GOCACHE",
		"NODE_OPTIONS", "PNPM_HOME", "JAVA_HOME", "CARGO_HOME",
		"SSL_CERT_FILE", "NODE_EXTRA_CA_CERTS",
		"GIT_AUTHOR_NAME", "GIT_COMMITTER_EMAIL",
		"LC_ALL", "LC_CTYPE",
	} {
		s.True(childenv.IsForwarded(name), "%s must reach the child", name)
	}
}

// The tools image sets these so Playwright/Puppeteer drive the preinstalled
// system Chromium instead of downloading a browser on every npm ci; forwarding
// them must not drag the pod's own secrets through the same gap.
func (s *ChildEnvSuite) TestBrowserVariablesForwardedPodSecretsAreNot() {
	for _, name := range []string{
		"CHROME_BIN", "PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD",
		"PUPPETEER_SKIP_CHROMIUM_DOWNLOAD", "PUPPETEER_EXECUTABLE_PATH",
	} {
		s.True(childenv.IsForwarded(name), "%s must reach the child", name)
	}
	for _, name := range []string{"DATABASE_URL", "INTERNAL_AUTH_KEY", "MCP_SECRETS_KEY"} {
		s.False(childenv.IsForwarded(name), "%s must not be forwarded", name)
	}
}

// The result is handed straight to exec.Cmd.Env. An empty slice there means
// "inherit the parent's environment", so returning one would reinstate the
// exact leak this package removes — the failure mode is silent and total.
func (s *ChildEnvSuite) TestNeverReturnsAnEmptyEnvironment() {
	for _, parent := range [][]string{nil, {}, {"DATABASE_URL=postgres://x"}, {"malformed-no-equals"}} {
		env := childenv.For(parent, nil)
		s.NotEmpty(env)
		s.Contains(env, "GIT_TERMINAL_PROMPT=0")
	}
}

// A child with no PATH cannot run anything, and a scrub that breaks every
// command gets switched off rather than fixed.
func (s *ChildEnvSuite) TestPathFallsBackWhenTheParentHasNone() {
	env := childenv.For([]string{"HOME=/home/bridge"}, nil)

	var path string
	for _, entry := range env {
		if len(entry) > 5 && entry[:5] == "PATH=" {
			path = entry
		}
	}
	s.NotEmpty(path)
	if runtime.GOOS == "windows" {
		s.Contains(path, "System32")
	} else {
		s.Contains(path, "/usr/bin")
	}
}

// The overlay is the call site's own statement of intent — a repo's pinned
// toolchain, an MCP server's configured credentials — and must beat anything
// inherited. os/exec keeps the last value for a repeated name, so "last" is the
// whole mechanism.
func (s *ChildEnvSuite) TestOverlayIsAppendedLast() {
	env := childenv.For([]string{"PATH=/usr/bin", "GOTOOLCHAIN=host-default"},
		[]string{"PATH=/opt/go1.23/bin:/usr/bin", "GOTOOLCHAIN=go1.23.4+auto"})

	s.Equal("PATH=/opt/go1.23/bin:/usr/bin", env[len(env)-2])
	s.Equal("GOTOOLCHAIN=go1.23.4+auto", env[len(env)-1])
}

// git authentication travels in a per-command http.extraHeader in this
// codebase, so the child has no credential to answer a prompt with. Without
// this the command blocks on stdin until its timeout, which reads as a flaky
// failure rather than "this remote needs auth".
func (s *ChildEnvSuite) TestGitPromptsAreDisabled() {
	s.Contains(childenv.For(parentWithSecrets, nil), "GIT_TERMINAL_PROMPT=0")
}

func (s *ChildEnvSuite) TestWindowsEnvironmentVariablesForwarded() {
	for _, name := range []string{
		"USERPROFILE", "HOMEDRIVE", "HOMEPATH", "APPDATA", "LOCALAPPDATA",
		"SYSTEMROOT", "WINDIR", "COMSPEC", "PATHEXT", "SYSTEMDRIVE",
		"PROGRAMDATA", "ProgramData", "ProgramFiles", "ProgramFiles(x86)",
		"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY",
	} {
		s.True(childenv.IsForwarded(name), "%s must reach the child", name)
	}
}

func (s *ChildEnvSuite) TestHomePopulatedFromUserProfile() {
	env := childenv.For([]string{"USERPROFILE=C:\\Users\\tester", "PATH=C:\\bin"}, nil)
	s.Contains(env, "USERPROFILE=C:\\Users\\tester")
	s.Contains(env, "HOME=C:\\Users\\tester")
}
