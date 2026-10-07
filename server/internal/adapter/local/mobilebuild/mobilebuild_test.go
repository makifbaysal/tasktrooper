package mobilebuild

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/storeops/pipeline"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

func requireBash(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the release script is a bash program; Run refuses Windows")
	}
	if !isExecutableFile(wrapperShell) {
		t.Skip("no " + wrapperShell + " on this machine")
	}
}

type fixture struct {
	b       *Builder
	root    string
	project string
	out     string

	mu    sync.Mutex
	lines []string
	onLog func(string)
}

func newFixture(t *testing.T, cfg Config) *fixture {
	t.Helper()
	f := &fixture{root: t.TempDir(), project: t.TempDir(), out: t.TempDir()}
	cfg.Root = f.root
	f.b = New(cfg)
	// A test must never touch the real keychain search list: a developer's
	// own TaskTrooper may be mid-build on this machine.
	f.b.security = ""
	return f
}

func (f *fixture) log(line string) {
	f.mu.Lock()
	f.lines = append(f.lines, line)
	hook := f.onLog
	f.mu.Unlock()
	if hook != nil {
		hook(line)
	}
}

func (f *fixture) logText() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.lines, "\n")
}

func (f *fixture) script(body string) string {
	return strings.ReplaceAll(body, "@OUT@", f.out)
}

func (f *fixture) request(script string, secrets, env map[string]string) port.MobileBuildRequest {
	return port.MobileBuildRequest{
		BuildID:    uuid.New(),
		Platform:   domain.MobileStorePlatformAndroid,
		Identifier: "com.example.app",
		ProjectDir: f.project,
		Script:     f.script(script),
		Secrets:    secrets,
		Env:        env,
		Log:        f.log,
	}
}

func (f *fixture) assertNoRunDirs(t *testing.T) {
	t.Helper()
	entries, err := os.ReadDir(f.root)
	require.NoError(t, err)
	for _, entry := range entries {
		assert.False(t, strings.HasPrefix(entry.Name(), runDirPrefix), "run directory %s outlived the run", entry.Name())
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(b)
}

func TestSecretsReachTheScriptButNeitherTheLogNorTheEnvironment(t *testing.T) {
	requireBash(t)
	const password = "hunter2-correct-horse"
	const alias = "key0"
	keystore := "QUJDREVGR0hJSktMTU5PUFFSU1RVVldY\nYWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4\n"

	bashEnv := filepath.Join(t.TempDir(), "bash_env")
	require.NoError(t, os.WriteFile(bashEnv, []byte("echo BASH_ENV_WAS_SOURCED\n"), 0o600))
	t.Setenv("BASH_ENV", bashEnv)
	t.Setenv("SHELLOPTS", "xtrace")
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("TT_PARENT_ONLY", "parent-only-value")
	t.Setenv("LANG", "")

	f := newFixture(t, Config{})
	script := `n=ANDROID_KEYSTORE_PASSWORD
printf '%s' "${!n}" > '@OUT@/seen'
echo "password=${!n}"
echo "alias=$ANDROID_KEY_ALIAS"
printf '%s' "$ANDROID_UPLOAD_KEYSTORE_B64"
if [ -e "$tt_secrets" ]; then echo SECRETS_FILE_PRESENT; else echo SECRETS_FILE_GONE; fi
if [ -e "$0" ]; then echo WRAPPER_PRESENT; else echo WRAPPER_GONE; fi
echo "channel=$1"
env > '@OUT@/env'
`
	_, err := f.b.Run(context.Background(), f.request(script, map[string]string{
		"ANDROID_KEYSTORE_PASSWORD":   password,
		"ANDROID_KEY_ALIAS":           alias,
		"ANDROID_UPLOAD_KEYSTORE_B64": keystore,
	}, map[string]string{"BUILD_NUMBER": "42"}))
	require.NoError(t, err, f.logText())

	assert.Equal(t, password, readFile(t, filepath.Join(f.out, "seen")), "the script reads the secret by name")

	logged := f.logText()
	assert.Contains(t, logged, "password=[redacted ANDROID_KEYSTORE_PASSWORD]")
	assert.Contains(t, logged, "alias=[redacted ANDROID_KEY_ALIAS]", "an alias is scrubbed at any length")
	assert.Equal(t, 2, strings.Count(logged, "[redacted ANDROID_UPLOAD_KEYSTORE_B64]"),
		"a wrapped key is scrubbed line by line")
	for _, leaked := range []string{password, alias, "QUJDREVGR0hJSktMTU5PUFFSU1RVVldY", "YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4"} {
		assert.NotContains(t, logged, leaked)
	}
	assert.Contains(t, logged, "SECRETS_FILE_GONE", "the wrapper unlinks the secrets once sourced")
	assert.Contains(t, logged, "WRAPPER_GONE", "the wrapper unlinks itself first")
	assert.Contains(t, logged, "channel=stage")
	assert.NotContains(t, logged, "BASH_ENV_WAS_SOURCED")
	for _, line := range f.lines {
		assert.False(t, strings.HasPrefix(line, "+ "), "SHELLOPTS=xtrace reached the shell: %q", line)
	}

	envText := readFile(t, filepath.Join(f.out, "env"))
	env := map[string]string{}
	for _, line := range strings.Split(envText, "\n") {
		if name, value, ok := strings.Cut(line, "="); ok {
			env[name] = value
		}
	}
	for _, name := range []string{
		"ANDROID_KEYSTORE_PASSWORD", "ANDROID_KEY_ALIAS", "ANDROID_UPLOAD_KEYSTORE_B64",
		"BASH_ENV", "SHELLOPTS", "GITHUB_ACTIONS", "TT_PARENT_ONLY",
	} {
		assert.NotContains(t, env, name, "%s must not be in the child's environment", name)
	}
	assert.NotContains(t, envText, password)
	assert.NotContains(t, envText, "QUJDREVGR0hJSktMTU5PUFFSU1RVVldY")
	assert.Equal(t, f.project, env["TT_PROJECT_ROOT"])
	assert.True(t, strings.HasPrefix(env["TT_RELEASE_WORKDIR"], filepath.Join(f.root, runDirPrefix)), env["TT_RELEASE_WORKDIR"])
	assert.Equal(t, workDirName, filepath.Base(env["TT_RELEASE_WORKDIR"]))
	assert.Equal(t, "42", env["BUILD_NUMBER"])
	assert.True(t, strings.HasPrefix(env["PATH"], strings.Join(fixedPath, string(os.PathListSeparator))), env["PATH"])
	assert.Equal(t, defaultLang, env["LANG"])

	f.assertNoRunDirs(t)
}

func TestFailureCarriesTheExitCodeAndTheRedactedTail(t *testing.T) {
	requireBash(t)
	const password = "hunter2-correct-horse"
	f := newFixture(t, Config{})
	script := `echo "pw=$ANDROID_KEYSTORE_PASSWORD"
echo "boom: gradle said no" >&2
exit 3
`
	_, err := f.b.Run(context.Background(), f.request(script, map[string]string{"ANDROID_KEYSTORE_PASSWORD": password}, nil))
	require.Error(t, err)
	msg := err.Error()
	assert.Contains(t, msg, "exited with code 3")
	assert.Contains(t, msg, "boom: gradle said no", "stderr is in the transcript")
	assert.Contains(t, msg, "pw=[redacted ANDROID_KEYSTORE_PASSWORD]")
	assert.NotContains(t, msg, password)
	f.assertNoRunDirs(t)

	f = newFixture(t, Config{})
	script = `i=1
while [ "$i" -le 300 ]; do echo "line $i"; i=$((i + 1)); done
exit 1
`
	_, err = f.b.Run(context.Background(), f.request(script, nil, nil))
	require.Error(t, err)
	msg = err.Error() + "\n"
	assert.Contains(t, msg, "\nline 300\n")
	assert.Contains(t, msg, "\nline 101\n")
	assert.NotContains(t, msg, "\nline 100\n", "only the last %d lines are kept", tailLines)
	f.assertNoRunDirs(t)
}

func alive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}

func waitGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for alive(pid) && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	assert.False(t, alive(pid), "pid %d outlived the cancelled build", pid)
}

func TestCancelStopsTheWholeGroupAndRemovesTheRunDir(t *testing.T) {
	requireBash(t)
	cases := map[string]struct {
		script string
		grace  time.Duration
	}{
		"polite": {script: "sleep 60 &\necho \"$!\" > '@OUT@/child'\necho started\nwait\n"},
		// The TERM is ignored by the shell and, inherited, by its child too;
		// only the escalation ends them.
		"stubborn": {script: "trap '' TERM\nsleep 60 &\necho \"$!\" > '@OUT@/child'\necho started\nwait\n", grace: 300 * time.Millisecond},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, Config{})
			if tc.grace > 0 {
				f.b.grace = tc.grace
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			f.onLog = func(line string) {
				if line == "started" {
					cancel()
				}
			}
			began := time.Now()
			_, err := f.b.Run(ctx, f.request(tc.script, nil, nil))
			require.Error(t, err)
			assert.True(t, errors.Is(err, context.Canceled), "%v", err)
			assert.Less(t, time.Since(began), 15*time.Second)

			pid, convErr := strconv.Atoi(strings.TrimSpace(readFile(t, filepath.Join(f.out, "child"))))
			require.NoError(t, convErr)
			waitGone(t, pid)
			f.assertNoRunDirs(t)
		})
	}
}

func TestTimeoutStopsTheBuild(t *testing.T) {
	requireBash(t)
	// Three seconds, not one: under a loaded `go test ./...` bash alone can
	// take a second to print its first line.
	f := newFixture(t, Config{Timeout: 3 * time.Second})
	began := time.Now()
	_, err := f.b.Run(context.Background(), f.request("echo started\nsleep 60\n", nil, nil))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "limit")
	assert.Contains(t, err.Error(), "started", "the tail says how far it got")
	assert.Less(t, time.Since(began), 15*time.Second)
	f.assertNoRunDirs(t)
}

func TestNewClampsTheTimeout(t *testing.T) {
	assert.Equal(t, MaxTimeout, New(Config{}).timeout)
	assert.Equal(t, MaxTimeout, New(Config{Timeout: 3 * time.Hour}).timeout)
	assert.Equal(t, time.Minute, New(Config{Timeout: time.Minute}).timeout)
}

func writeAged(t *testing.T, path string, age time.Duration) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("signed"), 0o644))
	at := time.Now().Add(-age)
	require.NoError(t, os.Chtimes(path, at, at))
}

func TestArtifactIsOnlyWhatThisRunWrote(t *testing.T) {
	requireBash(t)
	t.Run("a fresh bundle is reported", func(t *testing.T) {
		f := newFixture(t, Config{})
		writeAged(t, filepath.Join(f.project, "build", "mobile-release", "old.aab"), time.Hour)
		res, err := f.b.Run(context.Background(), f.request(
			"mkdir -p build/mobile-release && printf x > build/mobile-release/app-release.aab\n", nil, nil))
		require.NoError(t, err, f.logText())
		assert.Equal(t, filepath.Join(f.project, "build", "mobile-release", "app-release.aab"), res.Artifact)
	})
	t.Run("an old bundle is not", func(t *testing.T) {
		f := newFixture(t, Config{})
		writeAged(t, filepath.Join(f.project, "build", "mobile-release", "old.aab"), time.Hour)
		res, err := f.b.Run(context.Background(), f.request("echo built nothing\n", nil, nil))
		require.NoError(t, err, f.logText())
		assert.Empty(t, res.Artifact)
	})
	t.Run("a failed run still reports what it wrote", func(t *testing.T) {
		f := newFixture(t, Config{})
		res, err := f.b.Run(context.Background(), f.request(
			"mkdir -p build/mobile-release && printf x > build/mobile-release/app-release.aab\nexit 1\n", nil, nil))
		require.Error(t, err)
		assert.Equal(t, filepath.Join(f.project, "build", "mobile-release", "app-release.aab"), res.Artifact)
	})
}

func TestFindArtifactPerPlatform(t *testing.T) {
	project := t.TempDir()
	started := time.Now().Add(-time.Minute)
	ipa := filepath.Join(project, "build", "mobile-release", "ipa")
	writeAged(t, filepath.Join(ipa, "Old.ipa"), time.Hour)
	writeAged(t, filepath.Join(ipa, "Earlier.ipa"), 30*time.Second)
	writeAged(t, filepath.Join(ipa, "App.ipa"), 0)
	writeAged(t, filepath.Join(ipa, "stray.aab"), 0)
	writeAged(t, filepath.Join(project, "build", "mobile-release", "app.aab"), 0)

	assert.Equal(t, filepath.Join(ipa, "App.ipa"), findArtifact(project, domain.MobileStorePlatformIOS, started))
	assert.Equal(t, filepath.Join(project, "build", "mobile-release", "app.aab"),
		findArtifact(project, domain.MobileStorePlatformAndroid, started))
	assert.Empty(t, findArtifact(t.TempDir(), domain.MobileStorePlatformAndroid, started))
}

func TestEnvIsAnAllowlist(t *testing.T) {
	for _, name := range []string{"PATH", "BASH_ENV", "SHELLOPTS", "ENV", "LD_PRELOAD", "DYLD_INSERT_LIBRARIES", "HOME", "TT_PROJECT_ROOT", "ANDROID_KEYSTORE_PASSWORD"} {
		_, err := checkEnv(map[string]string{name: "x"})
		assert.Error(t, err, "%s must be refused", name)
	}
	_, err := checkEnv(map[string]string{"BUILD_NUMBER": "1\nPATH=/evil"})
	assert.Error(t, err, "a value is one line")

	knobs, err := checkEnv(map[string]string{"MOBILE_RELEASE_UPLOAD": "false", "BUILD_NUMBER": "412.54.2"})
	require.NoError(t, err)
	assert.Equal(t, []string{"BUILD_NUMBER=412.54.2", "MOBILE_RELEASE_UPLOAD=false"}, knobs)
}

func TestSecretNamesAreChecked(t *testing.T) {
	for _, name := range []string{"lower", "1ABC", "A-B", "PATH", "HOME", "BASH_ENV", "IFS", "GITHUB_ACTIONS", "TT_RELEASE_WORKDIR", "BUILD_NUMBER", "JAVA_HOME", "ANDROID_SDK_ROOT"} {
		_, err := checkSecrets(map[string]string{name: "value-long-enough"})
		assert.Error(t, err, "%s must be refused", name)
	}
	_, err := checkSecrets(map[string]string{"ASC_KEY_P8": "a\x00b"})
	assert.Error(t, err, "a NUL cannot survive a sourced file")

	got, err := checkSecrets(map[string]string{"ASC_KEY_ID": "ABC123", "PLAY_SA_JSON": ""})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"ASC_KEY_ID": "ABC123"}, got, "an empty secret is left for the script's require to name")
}

func TestRefusedRequestRunsNothing(t *testing.T) {
	requireBash(t)
	script := "touch '@OUT@/ran'\n"
	for name, mutate := range map[string]func(*port.MobileBuildRequest){
		"env PATH":           func(r *port.MobileBuildRequest) { r.Env = map[string]string{"PATH": "/evil"} },
		"env BASH_ENV":       func(r *port.MobileBuildRequest) { r.Env = map[string]string{"BASH_ENV": "/evil"} },
		"reserved secret":    func(r *port.MobileBuildRequest) { r.Secrets = map[string]string{"PATH": "/evil"} },
		"relative project":   func(r *port.MobileBuildRequest) { r.ProjectDir = "relative/dir" },
		"missing project":    func(r *port.MobileBuildRequest) { r.ProjectDir = filepath.Join(r.ProjectDir, "missing") },
		"unknown platform":   func(r *port.MobileBuildRequest) { r.Platform = "web" },
		"empty script":       func(r *port.MobileBuildRequest) { r.Script = " \n" },
		"ios off macOS only": func(r *port.MobileBuildRequest) { r.Platform = domain.MobileStorePlatformIOS },
	} {
		t.Run(name, func(t *testing.T) {
			if name == "ios off macOS only" && runtime.GOOS == "darwin" {
				t.Skip("iOS builds are allowed on macOS")
			}
			f := newFixture(t, Config{})
			req := f.request(script, nil, nil)
			mutate(&req)
			_, err := f.b.Run(context.Background(), req)
			require.Error(t, err)
			assert.NoFileExists(t, filepath.Join(f.out, "ran"))
			f.assertNoRunDirs(t)
		})
	}
}

func TestWriteRunLaysOutPrivateFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits")
	}
	f := newFixture(t, Config{})
	files, remove, err := f.b.writeRun("echo hi\n", map[string]string{
		"IOS_CERT_PASSWORD": "it's",
		"ASC_KEY_P8":        "line one\nline two",
	})
	require.NoError(t, err)

	assert.Regexp(t, regexp.MustCompile(`^`+runDirPrefix+`[0-9a-f]{32}$`), filepath.Base(files.dir))
	modes := map[string]os.FileMode{
		files.dir: 0o700, files.workdir: 0o700, files.secrets: 0o600, files.script: 0o700, files.wrapper: 0o700,
	}
	for path, want := range modes {
		info, err := os.Stat(path)
		require.NoError(t, err)
		assert.Equal(t, want, info.Mode().Perm(), path)
	}
	assert.Equal(t, "ASC_KEY_P8='line one\nline two'\nIOS_CERT_PASSWORD='it'\\''s'\n", readFile(t, files.secrets))
	assert.Equal(t, "echo hi\n", readFile(t, files.script))
	assert.True(t, strings.HasPrefix(readFile(t, files.wrapper), "#!/bin/bash\nset -eu\n"))

	_, _, err = f.b.writeRun("echo hi\n", nil)
	require.NoError(t, err, "a second run gets its own directory")

	remove()
	assert.NoDirExists(t, files.dir)
}

func TestSweepRemovesStaleRunDirsOnly(t *testing.T) {
	f := newFixture(t, Config{})
	stale := filepath.Join(f.root, runDirPrefix+"0123456789abcdef0123456789abcdef")
	require.NoError(t, os.MkdirAll(filepath.Join(stale, workDirName), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(stale, secretsFile), []byte("A='b'\n"), 0o600))
	other := filepath.Join(f.root, "something-else")
	require.NoError(t, os.Mkdir(other, 0o700))
	live, remove, err := f.b.writeRun("echo hi\n", nil)
	require.NoError(t, err)
	defer remove()

	f.b.Sweep()

	assert.NoDirExists(t, stale)
	assert.DirExists(t, other)
	assert.DirExists(t, live.dir, "a run in flight is not stale")
}

func TestNewBuildsAFixedPath(t *testing.T) {
	b := New(Config{ExtraBinDirs: []string{"/opt/tools/bin", "relative/bin", "/usr/bin", "/opt/tools/bin/", "/a:/b"}})
	assert.Equal(t, append(append([]string(nil), fixedPath...), "/opt/tools/bin"), b.path,
		"configured dirs follow the fixed part, absolute and once each")
}

func TestRedactor(t *testing.T) {
	redact := newRedactor(map[string]string{
		"ANDROID_KEY_PASSWORD": "abc",
		"ASC_KEY_ID":           "AB12",
		"PLAY_SA_JSON":         "longer-secret-value",
		"ASC_KEY_P8":           "secret-value",
	})
	assert.Equal(t, "pw=[redacted ANDROID_KEY_PASSWORD]", redact("pw=abc"), "a password at any length")
	assert.Equal(t, "id=AB12", redact("id=AB12"), "a short value that is not a password is left alone")
	assert.Equal(t, "[redacted PLAY_SA_JSON] [redacted ASC_KEY_P8]", redact("longer-secret-value secret-value"),
		"the longer value is replaced whole")
}

func TestAvailable(t *testing.T) {
	b := New(Config{})
	ok, reason := b.Available(context.Background(), "web")
	assert.False(t, ok)
	assert.NotEmpty(t, reason)

	if runtime.GOOS == "windows" {
		ok, reason = b.Available(context.Background(), domain.MobileStorePlatformAndroid)
		assert.False(t, ok)
		assert.NotEmpty(t, reason)
		return
	}
	requireBash(t)

	jdk := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(jdk, "bin"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(jdk, "bin", "java"), []byte("#!/bin/sh\nexit 0\n"), 0o755))
	t.Setenv("JAVA_HOME", jdk)
	ok, reason = b.Available(context.Background(), domain.MobileStorePlatformAndroid)
	assert.True(t, ok, reason)
	assert.Contains(t, b.baseEnv(), "JAVA_HOME="+jdk)

	t.Setenv("JAVA_HOME", filepath.Join(jdk, "missing"))
	assert.NotContains(t, strings.Join(b.baseEnv(), "\n"), "JAVA_HOME=", "a JAVA_HOME with no bin/java is not passed")

	if runtime.GOOS != "darwin" {
		ok, reason = b.Available(context.Background(), domain.MobileStorePlatformIOS)
		assert.False(t, ok)
		assert.Contains(t, reason, "macOS")
	}
}

// TestRunsTheGeneratedScript drives the real generated Android procedure
// end to end, with gradlew faked: the wrapper, TT_PROJECT_ROOT and
// TT_RELEASE_WORKDIR, the injected version code and the artifact all have to
// agree for it to pass.
func TestRunsTheGeneratedScript(t *testing.T) {
	requireBash(t)
	arts, err := pipeline.Render(pipeline.Spec{
		Platform:   domain.MobileStorePlatformAndroid,
		Identifier: "com.example.app",
		Module:     "app",
	})
	require.NoError(t, err)

	f := newFixture(t, Config{})
	t.Setenv("HOME", t.TempDir())
	jdk := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(jdk, "bin"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(jdk, "bin", "java"), []byte("#!/bin/sh\nexit 0\n"), 0o755))
	t.Setenv("JAVA_HOME", jdk)

	gradlew := f.script(`#!/bin/bash
set -eu
printf '%s\n' "$@" > '@OUT@/args'
home=""
while [ $# -gt 0 ]; do
  if [ "$1" = "-g" ]; then home="$2"; fi
  shift
done
cp "$home/gradle.properties" '@OUT@/gradle.properties'
mkdir -p app/build/outputs/bundle/release
printf 'signed' > app/build/outputs/bundle/release/app-release.aab
`)
	require.NoError(t, os.WriteFile(filepath.Join(f.project, "gradlew"), []byte(gradlew), 0o755))

	const storePassword = "store-password-123"
	res, err := f.b.Run(context.Background(), port.MobileBuildRequest{
		BuildID:    uuid.New(),
		Platform:   domain.MobileStorePlatformAndroid,
		Identifier: "com.example.app",
		ProjectDir: f.project,
		Script:     arts[0].Body,
		Secrets: map[string]string{
			"ANDROID_UPLOAD_KEYSTORE_B64": "a2V5c3RvcmUtYnl0ZXM=",
			"ANDROID_KEYSTORE_PASSWORD":   storePassword,
			"ANDROID_KEY_ALIAS":           "upload",
			"ANDROID_KEY_PASSWORD":        "key-password-456",
		},
		Env: map[string]string{"BUILD_NUMBER": "42", "MOBILE_RELEASE_UPLOAD": "false"},
		Log: f.log,
	})
	require.NoError(t, err, f.logText())

	assert.Equal(t, filepath.Join(f.project, "build", "mobile-release", "app-release.aab"), res.Artifact)
	args := readFile(t, filepath.Join(f.out, "args"))
	assert.Contains(t, args, "-PversionCode=42\n")
	assert.Contains(t, args, "-Pandroid.injected.version.code=42\n")
	assert.NotContains(t, args, storePassword, "signing values never reach argv")
	assert.Contains(t, readFile(t, filepath.Join(f.out, "gradle.properties")),
		"android.injected.signing.store.password="+storePassword, "the sourced secret reached gradle through the file")
	assert.NotContains(t, f.logText(), storePassword)
	f.assertNoRunDirs(t)
}
