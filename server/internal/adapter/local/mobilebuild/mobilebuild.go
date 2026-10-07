// Package mobilebuild runs a TaskTrooper test build on this machine: the
// generated release script's stage channel, with the signing material for
// that one run.
//
// The secrets never reach argv or an environment block. On macOS any process
// running as this user can read another's environment through KERN_PROCARGS2
// (`ps eww <pid>`), and argv is world-readable. So they are written to one
// 0600 file in a 0700 run directory with an unguessable name, and a generated
// wrapper sources that file and then the release script in the same shell.
// They become plain shell variables — all `${!name}` in the script needs — and
// are in no process's environment: not the wrapper's, and not the envp of the
// xcodebuild, gradlew and fastlane the script starts. The wrapper unlinks the
// file once it has been read, and itself before anything else.
//
// The run directory's lifetime is the run's: a deferred removal registered
// before anything can fail, and Sweep for the endings that cannot run one —
// SIGKILL, a crash, a machine losing power.
package mobilebuild

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// MaxTimeout bounds one run, whatever Config asks for.
const MaxTimeout = 90 * time.Minute

const (
	// runDirPrefix is shared by the writer and Sweep, and with the desktop
	// runner's sweep and the generated script's TT_RELEASE_WORKDIR comment: it
	// is how a directory a killed run left behind is recognised.
	runDirPrefix = "tasktrooper-release-"
	secretsFile  = "signing.env"
	scriptFile   = "script.sh"
	wrapperFile  = "wrapper.sh"
	workDirName  = "work"

	// keychainName is the generated script's KEYCHAIN basename
	// (application/storeops/pipeline/script.go). The script deletes it in an
	// EXIT trap, which a SIGKILLed process group never runs; this name is how
	// what that leaves in the user's keychain search list is found again.
	// Renaming either side alone stops that cleanup silently.
	keychainName = "tt-release.keychain-db"

	// wrapperShell is absolute rather than `#!/usr/bin/env bash`: env resolves
	// bash through PATH, and a PATH chosen by someone else is a shell chosen by
	// someone else, one line before a file of signing material is sourced.
	wrapperShell = "/bin/bash"

	// securityBin is absolute for the same reason: it deletes keychains.
	securityBin = "/usr/bin/security"

	stopGrace       = 30 * time.Second
	probeTimeout    = 30 * time.Second
	securityTimeout = 15 * time.Second
)

// fixedPath is the child's PATH, never the inherited one: the wrapper's
// shebang is absolute, but everything the script runs after it resolves
// through PATH. The Homebrew prefixes are constants, not a lookup.
var fixedPath = []string{"/usr/bin", "/bin", "/usr/sbin", "/sbin", "/opt/homebrew/bin", "/usr/local/bin"}

// inheritedEnv is the whole of what a build keeps from this process's
// environment. An allowlist, because what a wider one hands a shell about to
// source signing material is the problem: BASH_ENV is sourced at bash
// startup, SHELLOPTS=xtrace echoes every command — the `security import -P`
// line included — and GITHUB_ACTIONS makes the script print decoded keys for
// Actions to mask.
var inheritedEnv = []string{"HOME", "USER", "LOGNAME", "LANG", "TMPDIR"}

// defaultLang is set when this process has none, which is how an app started
// from the Dock runs: fastlane and CocoaPods refuse a non-UTF-8 locale.
const defaultLang = "en_US.UTF-8"

type Config struct {
	// Root is where run directories are made; os.TempDir() when empty.
	Root string
	// ExtraBinDirs are the directories of tool binaries this machine was
	// configured with (fastlane, a JDK, the Android SDK's tools). They follow
	// the fixed PATH, so none of them can shadow /usr/bin.
	ExtraBinDirs []string
	// AndroidSDK becomes ANDROID_HOME and ANDROID_SDK_ROOT. Empty means
	// Android Studio's default location, when it exists.
	AndroidSDK string
	// Timeout bounds one run; zero or anything above MaxTimeout is MaxTimeout.
	Timeout time.Duration
}

// Builder is port.LocalMobileBuilder. Sweep is NOT run by New: it is the
// caller's to run once at startup, before the first Run. Constructing a
// builder has no side effects, and only the caller knows when no other engine
// on this machine can be mid-build.
type Builder struct {
	root     string
	path     []string
	sdk      string
	timeout  time.Duration
	grace    time.Duration
	security string

	// slot runs one build at a time. Two would share the user's keychain
	// search list and the provisioning-profile directory under $HOME, and the
	// first to finish removes a profile the other is still signing with.
	slot chan struct{}

	mu     sync.Mutex
	active map[string]struct{}
}

var _ port.LocalMobileBuilder = (*Builder)(nil)

func New(cfg Config) *Builder {
	b := &Builder{
		root:     strings.TrimSpace(cfg.Root),
		timeout:  cfg.Timeout,
		grace:    stopGrace,
		security: securityBin,
		slot:     make(chan struct{}, 1),
		active:   make(map[string]struct{}),
	}
	if b.root == "" {
		b.root = os.TempDir()
	}
	if b.timeout <= 0 || b.timeout > MaxTimeout {
		b.timeout = MaxTimeout
	}
	seen := make(map[string]bool)
	for _, dir := range append(append([]string(nil), fixedPath...), cfg.ExtraBinDirs...) {
		dir = strings.TrimSpace(dir)
		if !usableDir(dir) || seen[filepath.Clean(dir)] {
			continue
		}
		seen[filepath.Clean(dir)] = true
		b.path = append(b.path, filepath.Clean(dir))
	}
	b.sdk = androidSDK(strings.TrimSpace(cfg.AndroidSDK))
	return b
}

// usableDir is the shape check for a PATH entry: absolute, and nothing that
// would split into two entries.
func usableDir(dir string) bool {
	return dir != "" && filepath.IsAbs(dir) && !strings.ContainsRune(dir, os.PathListSeparator) && !strings.ContainsRune(dir, 0)
}

func androidSDK(configured string) string {
	if configured != "" {
		if usableDir(configured) && isDir(configured) {
			return filepath.Clean(configured)
		}
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	for _, dir := range []string{
		filepath.Join(home, "Library", "Android", "sdk"),
		filepath.Join(home, "Android", "Sdk"),
	} {
		if usableDir(dir) && isDir(dir) {
			return dir
		}
	}
	return ""
}

func isDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

func isExecutableFile(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0
}

// javaHome is the one inherited value that names a program, so it passes only
// as a directory holding an executable bin/java. An Android build runs
// ./gradlew, which finds its JDK there or not at all; dropping it would not be
// safer, just Android builds failing on most machines.
func javaHome() string {
	dir := os.Getenv("JAVA_HOME")
	if !usableDir(dir) || !isExecutableFile(filepath.Join(dir, "bin", "java")) {
		return ""
	}
	return filepath.Clean(dir)
}

func lookPath(name string, dirs []string) string {
	for _, dir := range dirs {
		if p := filepath.Join(dir, name); isExecutableFile(p) {
			return p
		}
	}
	return ""
}

// baseEnv is the child's WHOLE environment apart from the per-run values, not
// an addition to this process's.
func (b *Builder) baseEnv() []string {
	env := make([]string, 0, len(inheritedEnv)+6)
	for _, name := range inheritedEnv {
		value := os.Getenv(name)
		if value == "" || strings.ContainsRune(value, 0) {
			continue
		}
		env = append(env, name+"="+value)
	}
	if os.Getenv("LANG") == "" {
		env = append(env, "LANG="+defaultLang)
	}
	path := append([]string(nil), b.path...)
	if jh := javaHome(); jh != "" {
		env = append(env, "JAVA_HOME="+jh)
		// After the fixed part, so /usr/bin still wins; there only so the
		// script's `command -v java` passes on a machine whose one JDK is this.
		path = append(path, filepath.Join(jh, "bin"))
	}
	env = append(env, "PATH="+strings.Join(path, string(os.PathListSeparator)))
	if b.sdk != "" {
		env = append(env, "ANDROID_HOME="+b.sdk, "ANDROID_SDK_ROOT="+b.sdk)
	}
	return env
}

func normPlatform(p string) string {
	return strings.ToLower(strings.TrimSpace(p))
}

// Available says whether this machine can build platform. A false answer
// always carries the sentence to show a person.
func (b *Builder) Available(ctx context.Context, platform string) (bool, string) {
	platform = normPlatform(platform)
	if platform != domain.MobileStorePlatformIOS && platform != domain.MobileStorePlatformAndroid {
		return false, fmt.Sprintf("%q is not a platform this machine builds", platform)
	}
	if runtime.GOOS == "windows" {
		return false, "a test build on this machine runs the release script, which is a bash program, and this machine runs Windows"
	}
	if !isExecutableFile(wrapperShell) {
		return false, wrapperShell + " is missing, and the release script needs bash"
	}
	if platform == domain.MobileStorePlatformIOS {
		return b.iosAvailable(ctx)
	}
	return b.androidAvailable(ctx)
}

// iosAvailable runs xcodebuild rather than only finding it: /usr/bin/xcodebuild
// is on every Mac, and without Xcode it is a shim that only says so.
func (b *Builder) iosAvailable(ctx context.Context) (bool, string) {
	if runtime.GOOS != "darwin" {
		return false, fmt.Sprintf("an iOS build needs macOS, and this machine runs %s", runtime.GOOS)
	}
	xcodebuild := lookPath("xcodebuild", b.path)
	if xcodebuild == "" {
		return false, "xcodebuild was not found: install Xcode"
	}
	if err := b.probe(ctx, xcodebuild, "-version"); err != nil {
		return false, fmt.Sprintf("xcodebuild does not run (%v): install Xcode and select it with xcode-select", err)
	}
	return true, ""
}

// androidAvailable runs java for the same reason: /usr/bin/java on a Mac is a
// shim that exists with no JDK installed.
func (b *Builder) androidAvailable(ctx context.Context) (bool, string) {
	if javaHome() != "" {
		return true, ""
	}
	java := lookPath("java", b.path)
	if java == "" {
		return false, "no JDK was found: an Android build runs ./gradlew, which needs JAVA_HOME or java on the PATH"
	}
	if err := b.probe(ctx, java, "-version"); err != nil {
		return false, fmt.Sprintf("java does not run (%v): install a JDK or set JAVA_HOME to one", err)
	}
	return true, ""
}

func (b *Builder) probe(ctx context.Context, bin string, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = b.baseEnv()
	cmd.WaitDelay = time.Second
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	for _, line := range strings.Split(string(out), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return errors.New(line)
		}
	}
	return err
}

func (b *Builder) track(dir string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.active[dir] = struct{}{}
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		b.active[real] = struct{}{}
	}
}

func (b *Builder) untrack(dir string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.active, dir)
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		delete(b.active, real)
	}
}

func (b *Builder) inActiveRun(path string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	for dir := range b.active {
		if path == dir || strings.HasPrefix(path, dir+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// Sweep removes what a run killed before its deferred removal left behind:
// on macOS, the temporary keychain in the user's search list, and every run
// directory under Root — decoded signing material included, because the
// script's WORKDIR is inside one. Keychains go first, while their files are
// still there for `security delete-keychain` to take out of the search list
// along with the file. Runs of this Builder still in flight are skipped.
func (b *Builder) Sweep() {
	b.sweepKeychains()
	b.sweepRunDirs()
}

func (b *Builder) sweepRunDirs() {
	entries, err := os.ReadDir(b.root)
	if err != nil {
		log.Debug().Err(err).Str("dir", b.root).Msg("mobile build: could not scan for stale run directories")
		return
	}
	swept := 0
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), runDirPrefix) {
			continue
		}
		dir := filepath.Join(b.root, entry.Name())
		if b.inActiveRun(dir) {
			continue
		}
		info, err := entry.Info()
		if err != nil || !ownedByCurrentUser(info) {
			continue
		}
		if err := os.RemoveAll(dir); err != nil {
			log.Warn().Err(err).Str("dir", dir).Msg("mobile build: could not remove a stale run directory; it may hold signing material")
			continue
		}
		swept++
	}
	if swept > 0 {
		log.Info().Int("count", swept).Msg("mobile build: removed run directories an unclean exit left behind")
	}
}

// sweepKeychains deletes EVERY search-list keychain with the script's
// basename that no run of this Builder owns. Only a dead run can have left
// one: the script's own trap deletes its keychain on every ending it
// controls, and this runs at startup.
func (b *Builder) sweepKeychains() {
	if runtime.GOOS != "darwin" || b.security == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), securityTimeout)
	defer cancel()
	listed, err := exec.CommandContext(ctx, b.security, "list-keychains", "-d", "user").Output()
	if err != nil {
		log.Debug().Err(err).Msg("mobile build: could not read the keychain search list")
		return
	}
	swept := 0
	for _, line := range strings.Split(string(listed), "\n") {
		path := strings.Trim(strings.TrimSpace(line), `"`)
		if path == "" || filepath.Base(path) != keychainName || b.inActiveRun(path) {
			continue
		}
		if err := exec.CommandContext(ctx, b.security, "delete-keychain", path).Run(); err != nil {
			log.Warn().Err(err).Str("keychain", path).Msg("mobile build: could not remove a stale release keychain")
			continue
		}
		swept++
	}
	if swept > 0 {
		log.Info().Int("count", swept).Msg("mobile build: removed release keychains an unclean exit left behind")
	}
}

// deleteRunKeychain takes a run's keychain out of the search list when the
// script's trap did not — the run was SIGKILLed after creating it. Removing
// the directory alone would delete the file and leave a search-list entry
// every later codesign walks.
func (b *Builder) deleteRunKeychain(workdir string) {
	if runtime.GOOS != "darwin" || b.security == "" {
		return
	}
	keychain := filepath.Join(workdir, keychainName)
	if _, err := os.Lstat(keychain); err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), securityTimeout)
	defer cancel()
	if err := exec.CommandContext(ctx, b.security, "delete-keychain", keychain).Run(); err != nil {
		log.Warn().Err(err).Str("keychain", keychain).Msg("mobile build: could not remove the run's keychain")
	}
}
