package mobilebuild

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/proctree"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// wrapperBody is exec'd directly through its own shebang — no shell is named
// in argv. Its arguments are the release script and the secrets file; it
// carries no secret itself.
//
//   - It removes ITSELF first, so no ending can leave a stray executable.
//   - It SOURCES the secrets rather than exporting them, then unlinks the
//     file: plain shell variables, in no environment block.
//   - It SOURCES the release script rather than running it, because a new
//     process would inherit only exported variables. `$0` stays this
//     wrapper's path, which is why the script is handed TT_PROJECT_ROOT.
const wrapperBody = `#!/bin/bash
set -eu
tt_script="$1"
tt_secrets="$2"
rm -f "$0"
. "$tt_secrets"
rm -f "$tt_secrets"
set -- stage
. "$tt_script"
`

// nameGrammar is a name that becomes a shell variable in a sourced file, or an
// environment variable of a shell about to source one. Anything outside it is
// a bug upstream, and an unchecked one is an assignment injected into code.
var nameGrammar = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)

const (
	maxSecrets     = 32
	maxSecretBytes = 1 << 20
	maxKnobBytes   = 256
)

// knobNames are the only names Env may carry: the non-secret knobs the
// generated script reads. An allowlist, because a list of names to refuse is
// never complete — BASH_ENV, SHELLOPTS, PATH, ENV and every runtime's own
// "load this file" variable each turn a shell about to source signing material
// into somebody else's program.
var knobNames = map[string]bool{
	"BUILD_NUMBER":          true,
	"MOBILE_RELEASE_UPLOAD": true,
	"PLAY_ROLLOUT_FRACTION": true,
}

// reservedSecretPrefixes and reservedSecretNames are names a secret may not
// take. Assigning a variable that is already exported changes the exported
// value, so a secret named like anything in the child's environment would be
// handed to every process the script starts; the rest change what the shell
// or the script does — GITHUB_ACTIONS alone makes it print decoded keys for
// Actions to mask.
var (
	reservedSecretPrefixes = []string{"BASH", "TT_", "GITHUB_", "RUNNER_", "ACTIONS_", "ANDROID_SDK", "JAVA_", "LC_", "DYLD_", "LD_"}
	reservedSecretNames    = map[string]bool{
		"PATH": true, "HOME": true, "USER": true, "LOGNAME": true, "LANG": true, "TMPDIR": true,
		"ANDROID_HOME": true, "IFS": true, "ENV": true, "CDPATH": true, "GLOBIGNORE": true,
		"SHELLOPTS": true, "PS1": true, "PS2": true, "PS3": true, "PS4": true, "PWD": true,
		"OLDPWD": true, "SHELL": true, "OPTIND": true, "OPTARG": true, "OPTERR": true,
		"UID": true, "EUID": true, "PPID": true, "HOSTNAME": true, "TERM": true,
		"FUNCNEST": true, "PROMPT_COMMAND": true, "POSIXLY_CORRECT": true, "TMOUT": true,
		"MAIL": true, "MAILPATH": true, "HISTFILE": true, "GRADLE_USER_HOME": true,
		"API_PRIVATE_KEYS_DIR": true,
	}
)

func checkSecrets(secrets map[string]string) (map[string]string, error) {
	if len(secrets) > maxSecrets {
		return nil, fmt.Errorf("mobilebuild: %d secrets is more than the %d a run materialises", len(secrets), maxSecrets)
	}
	out := make(map[string]string, len(secrets))
	total := 0
	for name, value := range secrets {
		if !nameGrammar.MatchString(name) {
			return nil, fmt.Errorf("mobilebuild: secret name %q is not one the release script reads", name)
		}
		if reservedSecretNames[name] || knobNames[name] || hasAnyPrefix(name, reservedSecretPrefixes) {
			return nil, fmt.Errorf("mobilebuild: secret name %s is reserved: it would change the build's environment or its shell", name)
		}
		// A NUL cannot be written into a sourced file and read back as the
		// same bytes; the script would decode a truncated key.
		if strings.ContainsRune(value, 0) {
			return nil, fmt.Errorf("mobilebuild: secret %s contains a NUL byte", name)
		}
		// Left out rather than refused: the script's own require names a
		// missing secret it needs, and one it does not need is not an error.
		if value == "" {
			continue
		}
		total += len(name) + len(value)
		if total > maxSecretBytes {
			return nil, fmt.Errorf("mobilebuild: the signing material is larger than the %d bytes a run materialises", maxSecretBytes)
		}
		out[name] = value
	}
	return out, nil
}

func checkEnv(env map[string]string) ([]string, error) {
	out := make([]string, 0, len(env))
	for name, value := range env {
		if !knobNames[name] {
			return nil, fmt.Errorf("mobilebuild: %s is not a variable a build may be given (allowed: BUILD_NUMBER, MOBILE_RELEASE_UPLOAD, PLAY_ROLLOUT_FRACTION)", name)
		}
		if len(value) > maxKnobBytes || strings.ContainsAny(value, "\x00\r\n") {
			return nil, fmt.Errorf("mobilebuild: %s is not a single-line value", name)
		}
		out = append(out, name+"="+value)
	}
	sort.Strings(out)
	return out, nil
}

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

func checkProjectDir(dir string) (string, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" || !filepath.IsAbs(dir) {
		return "", fmt.Errorf("mobilebuild: the project directory %q is not an absolute path", dir)
	}
	if strings.ContainsRune(dir, 0) || !isDir(dir) {
		return "", fmt.Errorf("mobilebuild: the project directory %s does not exist", dir)
	}
	return filepath.Clean(dir), nil
}

// Run builds req's stage channel on this machine. On a failure the result
// still names whatever signed build the run left: a refused upload comes after
// the binary was written.
func (b *Builder) Run(ctx context.Context, req port.MobileBuildRequest) (port.MobileBuildResult, error) {
	platform := normPlatform(req.Platform)
	switch {
	case runtime.GOOS == "windows":
		return port.MobileBuildResult{}, errors.New("mobilebuild: the release script is a bash program and this machine runs Windows")
	case platform == domain.MobileStorePlatformIOS && runtime.GOOS != "darwin":
		return port.MobileBuildResult{}, fmt.Errorf("mobilebuild: an iOS build needs macOS, and this machine runs %s", runtime.GOOS)
	case platform != domain.MobileStorePlatformIOS && platform != domain.MobileStorePlatformAndroid:
		return port.MobileBuildResult{}, fmt.Errorf("mobilebuild: %q is not a platform this machine builds", req.Platform)
	}
	project, err := checkProjectDir(req.ProjectDir)
	if err != nil {
		return port.MobileBuildResult{}, err
	}
	if strings.TrimSpace(req.Script) == "" {
		return port.MobileBuildResult{}, errors.New("mobilebuild: the release script is empty")
	}
	secrets, err := checkSecrets(req.Secrets)
	if err != nil {
		return port.MobileBuildResult{}, err
	}
	knobs, err := checkEnv(req.Env)
	if err != nil {
		return port.MobileBuildResult{}, err
	}

	select {
	case b.slot <- struct{}{}:
	case <-ctx.Done():
		return port.MobileBuildResult{}, fmt.Errorf("mobilebuild: cancelled while another build held this machine: %w", ctx.Err())
	}
	defer func() { <-b.slot }()

	runCtx, cancel := context.WithTimeout(ctx, b.timeout)
	defer cancel()

	// Deferred before the error is looked at: a half-written run directory is
	// still a directory with a signing key in it.
	files, remove, err := b.writeRun(req.Script, secrets)
	defer remove()
	if err != nil {
		return port.MobileBuildResult{}, err
	}

	out := newTranscript(newRedactor(secrets), req.Log)
	stdout, stderr := out.writer(), out.writer()
	cmd := exec.Command(files.wrapper, files.script, files.secrets)
	cmd.Dir = project
	// The child's WHOLE environment: the secrets are not in it, and neither is
	// anything this process was started with that nobody chose.
	cmd.Env = append(b.baseEnv(), "TT_PROJECT_ROOT="+project, "TT_RELEASE_WORKDIR="+files.workdir)
	cmd.Env = append(cmd.Env, knobs...)
	// Nil, so a step that stops for input reads EOF instead of waiting forever.
	cmd.Stdin = nil
	cmd.Stdout, cmd.Stderr = stdout, stderr

	started := time.Now()
	tree, err := proctree.Start(cmd)
	if err != nil {
		return port.MobileBuildResult{}, fmt.Errorf("mobilebuild: start the release script: %w", err)
	}
	names := make([]string, 0, len(secrets))
	for name := range secrets {
		names = append(names, name)
	}
	sort.Strings(names)
	log.Info().Str("build", req.BuildID.String()).Str("platform", platform).Int("pid", tree.Pid()).
		Strs("secrets", names).Msg("mobile build started")

	done := make(chan struct{})
	var watcher sync.WaitGroup
	watcher.Add(1)
	go func() {
		defer watcher.Done()
		select {
		// The whole group, politely first: a SIGTERM lets the script's trap
		// delete its keychain, and everything it started — gradle daemons,
		// xcodebuild workers, a fastlane ruby — is in that group.
		case <-runCtx.Done():
			tree.Terminate(b.grace)
		case <-done:
		}
	}()
	waitErr := cmd.Wait()
	close(done)
	tree.Close()
	watcher.Wait()
	stdout.flush()
	stderr.flush()

	result := port.MobileBuildResult{Artifact: findArtifact(project, platform, started)}
	log.Info().Str("build", req.BuildID.String()).Err(waitErr).Dur("took", time.Since(started)).
		Str("artifact", result.Artifact).Msg("mobile build finished")

	// A daemon still holding the output pipe after a clean exit is not a
	// failed build; WaitDelay already stopped waiting for it.
	if waitErr == nil || errors.Is(waitErr, exec.ErrWaitDelay) {
		return result, nil
	}
	if runErr := runCtx.Err(); runErr != nil {
		if errors.Is(runErr, context.DeadlineExceeded) && ctx.Err() == nil {
			return result, fmt.Errorf("mobilebuild: the build passed its %s limit and was stopped%s", b.timeout, out.tailSuffix())
		}
		return result, fmt.Errorf("mobilebuild: the build was cancelled: %w%s", context.Cause(ctx), out.tailSuffix())
	}
	var exitErr *exec.ExitError
	if errors.As(waitErr, &exitErr) {
		if code := exitErr.ExitCode(); code >= 0 {
			return result, fmt.Errorf("mobilebuild: the release script exited with code %d%s", code, out.tailSuffix())
		}
		return result, fmt.Errorf("mobilebuild: the release script ended: %s%s", exitErr, out.tailSuffix())
	}
	return result, fmt.Errorf("mobilebuild: waiting for the release script: %w%s", waitErr, out.tailSuffix())
}

type runFiles struct {
	dir     string
	workdir string
	secrets string
	script  string
	wrapper string
}

// writeRun materialises one run. The removal it returns is never nil, on
// every error path included, so the caller defers it before looking at the
// error. crypto/rand rather than os.MkdirTemp names the directory, and
// os.Mkdir rather than MkdirAll creates it, so the call can never adopt a
// directory — or follow a link — that something else put there.
func (b *Builder) writeRun(script string, secrets map[string]string) (runFiles, func(), error) {
	var f runFiles
	noop := func() {}
	if err := os.MkdirAll(b.root, 0o700); err != nil {
		return f, noop, fmt.Errorf("mobilebuild: create %s: %w", b.root, err)
	}
	var seed [16]byte
	if _, err := rand.Read(seed[:]); err != nil {
		return f, noop, fmt.Errorf("mobilebuild: name the run directory: %w", err)
	}
	f.dir = filepath.Join(b.root, runDirPrefix+hex.EncodeToString(seed[:]))
	if err := os.Mkdir(f.dir, 0o700); err != nil {
		return f, noop, fmt.Errorf("mobilebuild: create the run directory: %w", err)
	}
	b.track(f.dir)
	f.workdir = filepath.Join(f.dir, workDirName)
	remove := func() {
		b.deleteRunKeychain(f.workdir)
		if err := os.RemoveAll(f.dir); err != nil {
			// Loud: this is a signing key still on disk, with only the next
			// startup's Sweep left to remove it.
			log.Error().Err(err).Str("dir", f.dir).Msg("mobile build: could not remove the run directory; signing material is still on disk")
		}
		b.untrack(f.dir)
	}

	// The script's own WORKDIR, inside the run directory rather than its
	// `mktemp -d`, so what it DECODES — dist.p12, upload.keystore,
	// play_sa.json, the keychain — goes with the run directory and is swept
	// with it.
	if err := os.Mkdir(f.workdir, 0o700); err != nil {
		return f, remove, fmt.Errorf("mobilebuild: create the run's work directory: %w", err)
	}
	f.secrets = filepath.Join(f.dir, secretsFile)
	if err := writeExclusive(f.secrets, secretsBody(secrets), 0o600); err != nil {
		return f, remove, fmt.Errorf("mobilebuild: write the signing material: %w", err)
	}
	f.script = filepath.Join(f.dir, scriptFile)
	if err := writeExclusive(f.script, []byte(script), 0o700); err != nil {
		return f, remove, fmt.Errorf("mobilebuild: write the release script: %w", err)
	}
	f.wrapper = filepath.Join(f.dir, wrapperFile)
	if err := writeExclusive(f.wrapper, []byte(wrapperBody), 0o700); err != nil {
		return f, remove, fmt.Errorf("mobilebuild: write the wrapper: %w", err)
	}
	return f, remove, nil
}

// secretsBody is one NAME='value' line per secret, sorted so two identical
// runs write an identical file. Single-quoted with the one escape a
// single-quoted shell word has; a newline inside is fine and expected, since a
// base64 secret often arrives wrapped.
func secretsBody(secrets map[string]string) []byte {
	names := make([]string, 0, len(secrets))
	for name := range secrets {
		names = append(names, name)
	}
	sort.Strings(names)
	var body bytes.Buffer
	for _, name := range names {
		body.WriteString(name)
		body.WriteString("='")
		body.WriteString(strings.ReplaceAll(secrets[name], "'", `'\''`))
		body.WriteString("'\n")
	}
	return body.Bytes()
}

// writeExclusive creates path or fails: O_EXCL, so a file something else put
// there first is never written into — or, for the two programs, executed.
func writeExclusive(path string, data []byte, mode os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// findArtifact reads the signed build off the directory the script writes it
// to rather than out of its log, and only one written during this run: a
// build that failed before producing anything must not report the previous
// run's binary as its own. The start is cut to the second because file times
// come from a coarser clock than time.Now, so a file written just after the
// start can carry a time just before it.
func findArtifact(project, platform string, started time.Time) string {
	dir, ext := filepath.Join(project, "build", "mobile-release"), ".aab"
	if platform == domain.MobileStorePlatformIOS {
		dir, ext = filepath.Join(dir, "ipa"), ".ipa"
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	since := started.Truncate(time.Second)
	newest, newestAt := "", time.Time{}
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !strings.EqualFold(filepath.Ext(entry.Name()), ext) {
			continue
		}
		info, err := entry.Info()
		if err != nil || info.ModTime().Before(since) {
			continue
		}
		if newest == "" || info.ModTime().After(newestAt) {
			newest, newestAt = filepath.Join(dir, entry.Name()), info.ModTime()
		}
	}
	return newest
}
