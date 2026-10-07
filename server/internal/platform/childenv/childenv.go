// Package childenv builds the environment for child processes the bridge spawns
// for agent commands, repository verify stages and stdio MCP servers. All three
// run attacker-influenced programs, and a bare `env` would exfiltrate every
// credential in the bridge's environment.
//
// The defense is that the secrets are absent from the child's environment —
// never inspecting command text, whose spellings are unbounded. This package is
// that allowlist's one implementation, deliberately stdlib-only so any layer can
// use it without a cycle.
package childenv

import (
	"runtime"
	"strings"
)

// forwardedEnvVars is everything a child inherits; anything else never reaches
// it. An allowlist rather than a denylist, so credentials that do not exist yet
// are refused by default. Names are chosen individually because a prefix (GO*,
// NODE_*, GIT_*, NPM_CONFIG_*) would sweep in GOOGLE_APPLICATION_CREDENTIALS,
// NODE_AUTH_TOKEN, GIT_ASKPASS and the rest. One list serves all three call
// sites; what differs per site is the overlay For passes in.
var forwardedEnvVars = map[string]struct{}{
	"PATH": {}, "HOME": {}, "USER": {}, "LOGNAME": {}, "SHELL": {}, "TERM": {},
	"TMPDIR": {}, "TMP": {}, "TEMP": {}, "LANG": {}, "LANGUAGE": {}, "TZ": {}, "CI": {},

	"USERPROFILE": {}, "HOMEDRIVE": {}, "HOMEPATH": {}, "APPDATA": {}, "LOCALAPPDATA": {},
	"ALLUSERSPROFILE": {}, "PROGRAMDATA": {}, "ProgramData": {},
	"SYSTEMROOT": {}, "SystemRoot": {}, "WINDIR": {}, "windir": {},
	"COMSPEC": {}, "ComSpec": {}, "PATHEXT": {}, "SYSTEMDRIVE": {}, "SystemDrive": {},
	"PROGRAMFILES": {}, "ProgramFiles": {}, "PROGRAMFILES(X86)": {}, "ProgramFiles(x86)": {},
	"COMMONPROGRAMFILES": {}, "CommonProgramFiles": {}, "COMMONPROGRAMFILES(X86)": {}, "CommonProgramFiles(x86)": {},
	"NUMBER_OF_PROCESSORS": {}, "PROCESSOR_ARCHITECTURE": {}, "PROCESSOR_IDENTIFIER": {}, "PROCESSOR_LEVEL": {}, "PROCESSOR_REVISION": {},
	"OS": {}, "PUBLIC": {},

	"HTTP_PROXY": {}, "HTTPS_PROXY": {}, "NO_PROXY": {},
	"http_proxy": {}, "https_proxy": {}, "no_proxy": {},

	// TLS trust for images whose CA bundle lives outside the default location.
	"SSL_CERT_FILE": {}, "SSL_CERT_DIR": {}, "CURL_CA_BUNDLE": {},
	"REQUESTS_CA_BUNDLE": {}, "NODE_EXTRA_CA_CERTS": {}, "GIT_SSL_CAINFO": {},

	"GOPATH": {}, "GOROOT": {}, "GOBIN": {}, "GOCACHE": {}, "GOMODCACHE": {},
	"GOTOOLCHAIN": {}, "GOFLAGS": {}, "GOPROXY": {}, "GOSUMDB": {}, "GONOSUMDB": {},
	"GOPRIVATE": {}, "GONOSUMCHECK": {}, "GO111MODULE": {}, "GOOS": {}, "GOARCH": {},
	"GOEXPERIMENT": {}, "CGO_ENABLED": {}, "CGO_CFLAGS": {}, "CGO_LDFLAGS": {},

	"NODE_OPTIONS": {}, "NODE_PATH": {}, "NODE_ENV": {}, "NVM_DIR": {}, "NVM_BIN": {},
	"COREPACK_HOME": {}, "PNPM_HOME": {}, "YARN_CACHE_FOLDER": {},
	"NPM_CONFIG_CACHE": {}, "npm_config_cache": {},
	"NPM_CONFIG_PREFIX": {}, "npm_config_prefix": {},
	"NPM_CONFIG_REGISTRY": {}, "npm_config_registry": {},

	// The tools image sets these so Playwright/Puppeteer use the preinstalled
	// Chromium; all four are a binary path or a skip flag, never a credential.
	"CHROME_BIN": {}, "PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD": {},
	"PUPPETEER_SKIP_CHROMIUM_DOWNLOAD": {}, "PUPPETEER_EXECUTABLE_PATH": {},

	"PYTHONPATH": {}, "PYTHONUNBUFFERED": {}, "PYTHONDONTWRITEBYTECODE": {},
	"VIRTUAL_ENV": {}, "PIP_CACHE_DIR": {}, "PYENV_ROOT": {}, "POETRY_HOME": {},
	"UV_CACHE_DIR": {},

	"JAVA_HOME": {}, "GRADLE_USER_HOME": {}, "MAVEN_OPTS": {},
	"ANDROID_HOME": {}, "ANDROID_SDK_ROOT": {},
	"CARGO_HOME": {}, "RUSTUP_HOME": {}, "RBENV_ROOT": {},

	// Directory locations the toolchain resolver reads, never credentials.
	"MISE_DATA_DIR": {}, "MISE_CONFIG_DIR": {}, "MISE_CACHE_DIR": {},
	"ASDF_DIR": {}, "ASDF_DATA_DIR": {},
	"XDG_CACHE_HOME": {}, "XDG_DATA_HOME": {}, "XDG_CONFIG_HOME": {}, "XDG_STATE_HOME": {},

	// Git identity, not credentials — auth travels in a per-command
	// http.extraHeader — but without it git commit aborts in a bare container.
	"GIT_AUTHOR_NAME": {}, "GIT_AUTHOR_EMAIL": {},
	"GIT_COMMITTER_NAME": {}, "GIT_COMMITTER_EMAIL": {},

	// DOCKER_HOST is an address, not a secret; dropping it would break the image
	// builds the docker sidecar exists to serve.
	"DOCKER_HOST": {},
}

var forwardedEnvVarsUpper = func() map[string]struct{} {
	m := make(map[string]struct{}, len(forwardedEnvVars))
	for k := range forwardedEnvVars {
		m[strings.ToUpper(k)] = struct{}{}
	}
	return m
}()

// forwardedEnvPrefixes covers families whose individual names are not enumerable
// but the whole family is non-secret; LC_* is locale only.
var forwardedEnvPrefixes = []string{"LC_"}

// defaultChildPath is the fallback when the bridge process itself has no PATH (a
// GUI launch context); a scrub that breaks the toolchain gets switched off.
const defaultChildPath = "/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin:/opt/homebrew/bin"
const defaultChildPathWindows = `C:\Windows\System32;C:\Windows;C:\Windows\System32\Wbem`

// IsForwarded is exported so tests can state which variables must reach a child.
func IsForwarded(name string) bool {
	if _, ok := forwardedEnvVars[name]; ok {
		return true
	}
	if runtime.GOOS == "windows" {
		if _, ok := forwardedEnvVarsUpper[strings.ToUpper(name)]; ok {
			return true
		}
	}
	for _, prefix := range forwardedEnvPrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
		if runtime.GOOS == "windows" && strings.HasPrefix(strings.ToUpper(name), strings.ToUpper(prefix)) {
			return true
		}
	}
	return false
}

// For builds one child environment: the forwarded slice of parent, then overlay
// appended last so it wins (os/exec keeps the last value for a repeated name).
// overlay is the only per-call-site difference — a toolchain the repo pins, or
// the credentials an MCP server is configured with. Never returns empty: a nil
// or empty Env means "inherit everything", which is the leak this replaces.
func For(parent, overlay []string) []string {
	env := make([]string, 0, len(parent)+len(overlay)+4)
	hasPath := false
	hasHome := false
	var userProfileVal string

	for _, entry := range parent {
		name, val, ok := strings.Cut(entry, "=")
		if !ok || !IsForwarded(name) {
			continue
		}
		if strings.EqualFold(name, "PATH") {
			hasPath = true
		}
		if strings.EqualFold(name, "HOME") && val != "" {
			hasHome = true
		}
		if strings.EqualFold(name, "USERPROFILE") && val != "" {
			userProfileVal = val
		}
		env = append(env, entry)
	}
	if !hasPath {
		if runtime.GOOS == "windows" {
			env = append(env, "PATH="+defaultChildPathWindows)
		} else {
			env = append(env, "PATH="+defaultChildPath)
		}
	}
	if !hasHome && userProfileVal != "" {
		env = append(env, "HOME="+userProfileVal)
	}
	if runtime.GOOS == "windows" && userProfileVal == "" {
		for _, entry := range env {
			name, val, ok := strings.Cut(entry, "=")
			if ok && strings.EqualFold(name, "HOME") && val != "" {
				env = append(env, "USERPROFILE="+val)
				break
			}
		}
	}
	// Without this, git prompts on the terminal and the command hangs until
	// timeout, reading as a flaky failure instead of a missing credential.
	env = append(env, "GIT_TERMINAL_PROMPT=0")
	// Git Credential Manager on Windows ignores GIT_TERMINAL_PROMPT and opens
	// a GUI sign-in instead, which no one is there to answer.
	env = append(env, "GCM_INTERACTIVE=never")
	return append(env, overlay...)
}
