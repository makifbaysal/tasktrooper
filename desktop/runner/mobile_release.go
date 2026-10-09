package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
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
)

// mobile.release — run this app's own release script, here, with the signing
// material the control plane sent for exactly this run.
//
// The script is NOT written here and its steps are not re-derived here. It is
// the file agent-server generated into the checkout
// (application/storeops/pipeline), the same one GitHub Actions runs, invoked
// with the same single argument. That is the whole point of the arrangement:
// a release from this Mac and a release from CI are the same steps by
// construction, and a step that exists on only one of them is a step that
// drifts.
//
// # Why the secrets are FILES and never an environment
//
// loadConfig above says it for the runner's own token and it is the same fact
// here: on macOS any process running as this user can read another's full
// environment block through KERN_PROCARGS2 — `ps eww <pid>` does exactly that
// — and argv is world-readable outright. A distribution certificate is worse
// to lose than a bearer token, so neither is available.
//
// What happens instead: the values are written to one 0600 file in a per-run
// 0700 directory with an unguessable name, and a small generated WRAPPER
// sources that file and then sources the release script in the same shell.
// They become ordinary shell variables — which is all `${!name}` in the script
// needs — rather than exported ones, so they are in no process's environment
// block at all: not this program's, not the shell's exec-time snapshot, and not
// in the envp of the xcodebuild, gradlew and fastlane the script spawns. The
// wrapper unlinks the file the instant it has been read, so the window on disk
// is the length of one `source`.
//
// # Why a generated wrapper rather than `sh -c`
//
// Because this package never runs a shell, and rules_test.go enforces it: a
// shell interpreter appearing as a literal argv element is what turns a
// backtick in somebody's data into a command. The wrapper is a FILE with its
// own shebang, exec'd directly like any other child. It carries no secret —
// only the paths of two files — and it deletes ITSELF on its first line, so
// even a SIGKILL a second later leaves nothing behind.
//
// It is written into the release script's own directory, and that is not
// cosmetic: a sourced script keeps the SOURCING shell's `$0`, and the release
// script resolves its project with `dirname "$0"/..`. A wrapper anywhere else
// would silently point the build at the wrong directory.
//
// The lifetime is the run's, and it is guaranteed four times over — the
// wrapper's `rm` of itself, its `rm` of the secrets file, a deferred removal
// registered before anything can fail, and a sweep at startup for the one
// ending that cannot run a deferred function: SIGKILL, a panic, a Mac losing
// power.

// releaseDirPrefix is shared by the writer and the sweeper: it is how a
// directory left behind by a killed runner is recognised as ours.
const releaseDirPrefix = "tasktrooper-release-"

// releaseSecretsFile is the file inside that directory. Its PATH appears in
// argv, which is fine and is the same trade mcp.go makes: the path is
// unguessable and the directory is 0700, so knowing the name buys nothing.
const releaseSecretsFile = "signing.env"

// releaseWorkDirName is the run directory's subdirectory that the release
// script uses as its own WORKDIR. See writeReleaseRun.
const releaseWorkDirName = "work"

// releaseKeychainName is the basename the generated script gives the temporary
// keychain it imports the distribution certificate into. It is duplicated here
// on purpose and it is the one value that IS duplicated: the script deletes its
// own keychain in an EXIT trap, and an EXIT trap does not run when the process
// group is SIGKILLed. Without this name the sweeper below could not find what
// that leaves behind — a keychain in the user's search list holding a signing
// identity. If the generator ever renames it, this stops sweeping and starts
// leaking, silently — so this is a coordinated cross-repo constant, not a
// local one: its other half is KEYCHAIN in agent-server's
// internal/application/storeops/pipeline/script.go, which carries the matching
// warning. Changing either is a two-repo change plus a desktop release.
const releaseKeychainName = "tt-release.keychain-db"

// releaseTimeout bounds one release on this side. It sits just under the
// ninety minutes agent-server allows, so a build that will not end dies HERE,
// where the message can name what it was doing, rather than being abandoned at
// the far end of a tunnel.
const releaseTimeout = 88 * time.Minute

// releaseSecretName is the grammar for a name that becomes a shell variable in
// the release script. These are names agent-server itself mints, so anything
// outside the shape is a bug on that side rather than a case to accommodate —
// and an unchecked one would be an assignment injected into a sourced file.
var releaseSecretName = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)

const (
	maxReleaseSecrets     = 32
	maxReleaseSecretBytes = 1 << 20
)

// mobileReleaseParams is the call. It mirrors adapter/runner's
// mobileReleaseParams field for field; the two are the wire contract between
// two independently deployed programs, so a change to either is a change to
// both and a desktop release.
type mobileReleaseParams struct {
	Workspace string `json:"workspace"`
	Platform  string `json:"platform"`
	Channel   string `json:"channel"`
	Script    string `json:"script"`
	// ScriptSHA256 is the digest of the exact file this call means, lowercase
	// hex, and it is REQUIRED.
	//
	// The design has always been "the script is the file we generated into the
	// checkout", but nothing checked it, and a checkout is a directory a
	// session with a Bash tool has been editing for the last hour. The caller
	// can answer this because the generator (agent-server's
	// application/storeops/pipeline) runs in the same process that makes this
	// call, so the digest is a value it already has rather than one it has to
	// go and compute about a machine it cannot see.
	//
	// There is no field to omit: an absent digest is refused. A verification
	// the caller can skip by leaving a key out of a JSON object is not a
	// verification, and this is the same argument --strict-mcp-config settles
	// for claude.run.
	ScriptSHA256 string            `json:"script_sha256"`
	Secrets      map[string]string `json:"secrets,omitempty"`
	BuildNumber  int               `json:"build_number,omitempty"`
	Rollout      string            `json:"rollout,omitempty"`
	SkipUpload   bool              `json:"skip_upload,omitempty"`
	TimeoutMS    int64             `json:"timeout_ms,omitempty"`
}

type mobileReleaseRequest struct {
	mobileReleaseParams
	ID string `json:"id,omitempty"`
}

type mobileReleaseResult struct {
	ExitCode int    `json:"exit_code"`
	Artifact string `json:"artifact"`
	// Log is deliberately absent from what this side fills in: the transcript
	// went out as `output` events while it was happening, and repeating it in
	// the terminal frame would double a build log that is already megabytes.
	DurationMS int64 `json:"duration_ms"`
}

// preparedRelease is everything the run needs, worked out and validated BEFORE
// a byte of the response has been written — the same split prepareRun makes,
// and for the same reason: once the 200 is on the wire the only way left to
// refuse is a frame the caller has to read a stream to reach.
type preparedRelease struct {
	// script is absolute, inside the workspace with its symlinks resolved,
	// confirmed to exist, and confirmed to hash to what the caller named.
	script string
	// project is the directory the script belongs to — its parent's parent,
	// because the generator puts it in <project>/scripts. It is where the
	// build's artifacts land.
	project string
	channel string
	secrets map[string]string
	// env is the child's WHOLE environment, not an addition to this process's.
	// See releaseEnviron.
	env      []string
	timeout  time.Duration
	platform string
}

func prepareRelease(cfg config, p mobileReleaseParams) (*preparedRelease, *rpcError) {
	switch strings.ToLower(strings.TrimSpace(p.Platform)) {
	case "ios":
		// Refused here rather than ninety minutes in. Everything Apple needs
		// exists on macOS only, and a Linux or Windows host answering "no
		// xcodebuild" after a full checkout is a slow way to say what is
		// knowable now.
		if runtime.GOOS != "darwin" {
			return nil, failure(codeNotReady, "an iOS release needs macOS and this runner is on %s", runtime.GOOS)
		}
	case "android":
		// Refused on Windows until a release has a runner of its own there: it
		// runs through a generated bash wrapper exec'd by its shebang, a release
		// script written for bash, and a unix PATH — none of which a Windows
		// host has, and all of which would fail after a full checkout.
		if runtime.GOOS == "windows" {
			return nil, failure(codeNotReady, "an Android release from this runner needs macOS or Linux for now (the release script is a bash program), and this runner is on %s", runtime.GOOS)
		}
	default:
		return nil, failure(codeBadRequest, "platform %q is not one this runner releases", p.Platform)
	}

	channel := strings.ToLower(strings.TrimSpace(p.Channel))
	if channel != "stage" && channel != "prod" {
		return nil, failure(codeBadRequest, "channel %q is not stage or prod", p.Channel)
	}

	workspace := strings.TrimSpace(p.Workspace)
	if workspace == "" {
		return nil, failure(codeBadRequest, "workspace is required: it names the checkout this release is built from")
	}
	// The script is named RELATIVE TO THE CHECKOUT and resolved through the
	// same containment the rest of this program uses, so a call cannot name a
	// program outside the workspace to run with the signing material attached.
	rel := filepath.Join(workspace, filepath.Clean(strings.TrimSpace(p.Script)))
	named, err := resolveInWorkspace(cfg.workspaceDir, rel)
	if err != nil {
		return nil, failure(codeBadRequest, "script: %v", err)
	}
	script, scriptErr := resolveReleaseScript(cfg.workspaceDir, named, p.Script)
	if scriptErr != nil {
		return nil, scriptErr
	}
	if digestErr := checkReleaseScriptDigest(script, p.ScriptSHA256, p.Script); digestErr != nil {
		return nil, digestErr
	}

	secrets, secretErr := checkReleaseSecrets(p.Secrets)
	if secretErr != nil {
		return nil, secretErr
	}

	// The three non-secret knobs the script reads. Each is a number or a fixed
	// word; none is a credential, so the environment is where they belong.
	knobs := []string{}
	if p.BuildNumber > 0 {
		knobs = append(knobs, fmt.Sprintf("BUILD_NUMBER=%d", p.BuildNumber))
	}
	if rollout := strings.TrimSpace(p.Rollout); rollout != "" {
		if !releaseRollout.MatchString(rollout) {
			return nil, failure(codeBadRequest, "rollout %q is not a fraction", rollout)
		}
		knobs = append(knobs, "PLAY_ROLLOUT_FRACTION="+rollout)
	}
	if p.SkipUpload {
		knobs = append(knobs, "MOBILE_RELEASE_UPLOAD=false")
	}
	sort.Strings(knobs)

	timeout := releaseTimeout
	if p.TimeoutMS > 0 && time.Duration(p.TimeoutMS)*time.Millisecond < timeout {
		timeout = time.Duration(p.TimeoutMS) * time.Millisecond
	}

	return &preparedRelease{
		script:   script,
		project:  filepath.Dir(filepath.Dir(script)),
		channel:  channel,
		secrets:  secrets,
		env:      releaseEnviron(cfg, knobs),
		timeout:  timeout,
		platform: strings.ToLower(strings.TrimSpace(p.Platform)),
	}, nil
}

// releaseRollout is Play's staged fraction: "0", "1", "0.25". Checked because
// it becomes an environment value the script passes to fastlane.
var releaseRollout = regexp.MustCompile(`^(0|1|0?\.[0-9]{1,4})$`)

// releaseScriptDigest is the shape of the digest a caller sends: lowercase hex
// sha-256 and nothing else, so a mistyped one is a refusal rather than a
// comparison that can never match.
var releaseScriptDigest = regexp.MustCompile(`^[0-9a-f]{64}$`)

// resolveReleaseScript answers "is this the file, and is the file inside the
// workspace" for a path a caller named — which resolveInWorkspace alone cannot,
// because it deliberately does not resolve symlinks.
//
// That combination was a hole and not a theoretical one. resolveInWorkspace
// checks the SPELLING of a path (it has to: workspace.prepare names a directory
// that does not exist yet, and EvalSymlinks on a missing path fails), and the
// old os.Stat here FOLLOWED links. So `repos/app/scripts/mobile-release.sh ->
// /Users/you/evil.sh` passed containment, passed the executable-bit check, and
// was then sourced by the wrapper with a distribution certificate in scope.
//
// Two separate checks, because they catch two different shapes:
//
//   - The script itself must not BE a link. Refused outright rather than
//     followed to a target that happens to be inside the workspace: this file
//     is one TaskTrooper generated into the checkout, so a link standing where
//     it should be is already not the arrangement, and "the link points
//     somewhere acceptable today" is not a property a run can rely on.
//   - A PARENT may still be one, and then the file is an ordinary file at a
//     path that leaves the root. filepath.EvalSymlinks resolves the whole thing
//     and the result goes back through resolveInWorkspace, which stays the only
//     function that decides what "inside the workspace" means.
//
// The ROOT is resolved too, and that is not tidiness: on macOS /var is a link
// to /private/var, so every path under a temp-dir workspace resolves to
// something that is outside an unresolved root by construction.
func resolveReleaseScript(root, script, named string) (string, *rpcError) {
	missing := failure(codeBadRequest,
		"%s is not in this checkout. The release script is generated into the repository by TaskTrooper; if it is missing, the store target has not been saved for this app", named)

	info, err := os.Lstat(script)
	if err != nil {
		return "", missing
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", failure(codeBadRequest,
			"%s is a symbolic link. The release script is a file TaskTrooper generates into the checkout; a link standing in for it would run a program from somewhere else with this app's signing material attached", named)
	}

	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", failure(codeInternal, "the workspace folder could not be resolved: %v", err)
	}
	actual, err := filepath.EvalSymlinks(script)
	if err != nil {
		return "", missing
	}
	inside, err := filepath.Rel(rootReal, actual)
	if err != nil {
		return "", failure(codeBadRequest, "%s resolves outside the workspace folder", named)
	}
	resolved, err := resolveInWorkspace(rootReal, inside)
	if err != nil {
		return "", failure(codeBadRequest, "%s: %v", named, err)
	}

	info, err = os.Stat(resolved)
	if err != nil {
		return "", missing
	}
	if info.IsDir() || info.Mode()&0o111 == 0 {
		// Not chmod'd here. A release script that arrived without its execute
		// bit is a checkout that lost it, and quietly fixing the symptom is how
		// the next person inherits a mystery.
		return "", failure(codeBadRequest, "%s is not an executable file", named)
	}
	return resolved, nil
}

// checkReleaseScriptDigest refuses to source a script that is not the one the
// caller meant.
//
// The workspace is a directory a Claude Code session has been editing, with a
// Bash tool, for as long as the task took; "the script is ours because we
// generated it" stopped being true the moment anything else could write there.
// The generator runs inside the process that makes this call, so the digest is
// something the caller HAS rather than something it must compute about a Mac it
// cannot see.
//
// Checked before the 200, like everything else that can be refused. The window
// between this and the wrapper sourcing the file is the same one the
// executable-bit check has always had, and it is bounded by the same fact: only
// this user can write into the workspace, and this is the check that stops the
// last hour of that writing from being handed a certificate.
func checkReleaseScriptDigest(script, want, named string) *rpcError {
	want = strings.ToLower(strings.TrimSpace(want))
	if want == "" {
		return failure(codeBadRequest,
			"script_sha256 is required: this runner will not source %s without the digest of the file it is supposed to be. The caller generated that script and already knows the digest", named)
	}
	if !releaseScriptDigest.MatchString(want) {
		return failure(codeBadRequest, "script_sha256 is not a lowercase hex sha-256 digest")
	}
	f, err := os.Open(script)
	if err != nil {
		return failure(codeBadRequest, "%s could not be read: %v", named, err)
	}
	defer func() { _ = f.Close() }()
	sum := sha256.New()
	if _, err := io.Copy(sum, f); err != nil {
		return failure(codeInternal, "%s could not be read: %v", named, err)
	}
	if got := hex.EncodeToString(sum.Sum(nil)); got != want {
		return failure(codeBadRequest,
			"%s is not the script this release is for: it hashes to %s and the call named %s. Something in this checkout changed the release script; re-save the store target to regenerate it", named, got, want)
	}
	return nil
}

// releaseInheritedEnv is the whole of what a release keeps from THIS process's
// environment. Everything else is dropped, and an allowlist is the only shape
// that can promise that — the same argument claude.run's `env` settles, and for
// a sharper reason here, because this child is the one holding a distribution
// certificate.
//
// What the old `append(os.Environ(), …)` handed a wrapper whose first act is to
// source a file of signing material, all three verified locally on this Mac:
//
//   - BASH_ENV. A non-interactive bash SOURCES it at startup. The injected file
//     ran inside the wrapper's own shell, one line before the secrets were read
//     into that same shell.
//   - SHELLOPTS=xtrace. bash turns xtrace on at startup and the script's own
//     `set -euo pipefail` does not turn it off, so every command it ran was
//     echoed to stderr — including `security import … -P <the password>`.
//   - GITHUB_ACTIONS. The script's mask_lines only masks under Actions; with
//     the variable inherited it prints the DECODED .p8 and play_sa.json to
//     stdout line by line, and the redactor holds the at-rest base64 rather
//     than the plaintext those lines carry.
//
// `launchctl setenv BASH_ENV /tmp/x` is all any of that took, and this Mac runs
// semi-trusted agent code as this user by design.
//
// Notable absences, each deliberate:
//
//   - SSH_AUTH_SOCK. Nothing the release script does reaches the network over
//     ssh; it builds, signs and uploads over https. Add it here if a Gemfile
//     ever grows a git source, and not before — an agent socket is a credential.
//   - JAVA_HOME is PRESENT, and it is the one judgement call in this list. An
//     Android build runs ./gradlew, which finds its JDK there or not at all,
//     and this runner has no detected JDK to substitute — dropping it would not
//     be "safer", it would be "Android releases fail on most Macs", and a
//     control that makes the feature unusable is a control somebody turns off.
//     It is validated below to a shape, which is what separates it from
//     BASH_ENV: it names a directory, it cannot name code to load, and a wrong
//     one is an early refusal rather than a confusing gradle failure.
var releaseInheritedEnv = []string{"HOME", "USER", "LOGNAME", "LANG", "TMPDIR", "JAVA_HOME"}

// releasePathBase is the child's PATH, fixed here rather than inherited, in
// this platform's list form.
//
// The wrapper's shebang resolves bash through it, which is why an inherited
// PATH was the same hole as BASH_ENV wearing a different name — and it directly
// contradicted the reason securityBin is spelled out absolutely a few lines
// down. The two Homebrew prefixes are CONSTANTS, not a lookup: fastlane, bundle
// and a linked JDK live in one of them on every Mac that has them, and a
// constant an attacker cannot aim is the property that matters.
var releasePathBase = strings.Join([]string{
	"/usr/bin", "/bin", "/usr/sbin", "/sbin", "/opt/homebrew/bin", "/usr/local/bin",
}, string(os.PathListSeparator))

// releaseEnviron builds that environment. The knobs go last so os/exec's
// last-occurrence-wins makes them beat anything of the same name, and
// TT_RELEASE_WORKDIR is added by runRelease because only it knows the run's
// directory.
func releaseEnviron(cfg config, knobs []string) []string {
	env := make([]string, 0, len(releaseInheritedEnv)+len(knobs)+4)
	for _, name := range releaseInheritedEnv {
		value, ok := os.LookupEnv(name)
		if !ok || value == "" {
			continue
		}
		if name == "JAVA_HOME" && !usableJavaHome(value) {
			log.Warn().Str("java_home", value).
				Msg("JAVA_HOME does not name a JDK and was not passed to the release; an Android build will say so itself")
			continue
		}
		env = append(env, name+"="+value)
	}
	env = append(env, "PATH="+releasePath(cfg))
	// Derived from the adb this Mac was CONFIGURED with rather than inherited,
	// which is the same rule every other binary here follows: <sdk>/platform-tools/adb.
	// Gradle's Android plugin looks these up before local.properties, and an
	// inherited one would be a directory an attacker chose for a build that is
	// about to open a keystore.
	if cfg.adbBin != "" {
		if sdk := filepath.Dir(filepath.Dir(cfg.adbBin)); filepath.IsAbs(sdk) {
			env = append(env, "ANDROID_HOME="+sdk, "ANDROID_SDK_ROOT="+sdk)
		}
	}
	return append(env, knobs...)
}

// usableJavaHome is the shape check on the one inherited value that names a
// program. A directory with an executable bin/java in it, or nothing.
func usableJavaHome(dir string) bool {
	if !filepath.IsAbs(dir) {
		return false
	}
	info, err := os.Stat(filepath.Join(dir, "bin", "java"))
	return err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0
}

// releasePath is releasePathBase plus the directories of the binaries this Mac
// was configured with, deduplicated and in that order — the fixed part first,
// so a detected directory can never shadow /usr/bin.
func releasePath(cfg config) string {
	seen := make(map[string]bool)
	dirs := make([]string, 0, 8)
	add := func(dir string) {
		if dir == "" || !filepath.IsAbs(dir) || seen[dir] {
			return
		}
		seen[dir] = true
		dirs = append(dirs, dir)
	}
	for _, dir := range filepath.SplitList(releasePathBase) {
		add(dir)
	}
	for _, bin := range []string{cfg.gitBin, cfg.xcrunBin, cfg.adbBin, cfg.emulatorBin} {
		if bin != "" {
			add(filepath.Dir(bin))
		}
	}
	return strings.Join(dirs, string(os.PathListSeparator))
}

// releaseRedactMinLen is the length below which a value is not searched for in
// the transcript. See newSecretRedactor for what it is protecting and
// releaseAlwaysRedact for who is exempt.
const releaseRedactMinLen = 8

// releaseAlwaysRedact are name shapes whose value is scrubbed at ANY length.
//
// The threshold's old comment said "nothing agent-server mints is that short",
// and that was simply wrong about who mints these: they are values the USER put
// in the vault. ANDROID_KEY_ALIAS is `key0` (4) out of Android Studio or
// `upload` (6) out of Play's own instructions, keytool takes a six-character
// password, and IOS_CERT_PASSWORD is whatever was typed into Keychain Access
// during the .p12 export. Every one of those was under the threshold and
// therefore printed.
var releaseAlwaysRedact = []string{"_PASSWORD", "_SECRET", "_ALIAS"}

// releasePasswordClass is the subset that must not be short in the FIRST place.
// An alias is not a password and cannot be changed without regenerating the
// key, so it is exempt from the threshold but never refused; a password can
// always be re-exported, and one too short to scrub is one that ends up in a
// build log an operator pastes into a ticket.
var releasePasswordClass = []string{"_PASSWORD", "_SECRET"}

func hasReleaseSuffix(name string, suffixes []string) bool {
	for _, suffix := range suffixes {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

func checkReleaseSecrets(secrets map[string]string) (map[string]string, *rpcError) {
	if len(secrets) == 0 {
		return nil, nil
	}
	if len(secrets) > maxReleaseSecrets {
		return nil, failure(codeBadRequest, "secrets has %d entries, more than the %d this runner will materialise", len(secrets), maxReleaseSecrets)
	}
	total := 0
	out := make(map[string]string, len(secrets))
	for name, value := range secrets {
		if !releaseSecretName.MatchString(name) {
			return nil, failure(codeBadRequest, "secret name %q is not one the release script reads", name)
		}
		if value == "" {
			return nil, failure(codeBadRequest, "secret %s has no value", name)
		}
		// A NUL cannot be written into a file the shell will source and read
		// back as the same bytes; the script would decode a truncated key and
		// fail at signing time saying something unrelated.
		if strings.ContainsRune(value, 0) {
			return nil, failure(codeBadRequest, "secret %s contains a NUL byte", name)
		}
		// Refused rather than released-and-printed. A signing password shorter
		// than the redactor's floor is one that either turns the whole
		// transcript into markers or goes through it in the clear, and unlike
		// a key alias a password is a value the person can re-export.
		if hasReleaseSuffix(name, releasePasswordClass) && len(value) < releaseRedactMinLen {
			return nil, failure(codeBadRequest,
				"secret %s is %d characters, and this runner will not run a release with a signing password shorter than %d: a value that short cannot be scrubbed out of a build log without matching ordinary words in it, so it would reach the caller in the clear. Re-export the certificate or keystore with a longer password and save it again",
				name, len(value), releaseRedactMinLen)
		}
		total += len(name) + len(value)
		if total > maxReleaseSecretBytes {
			return nil, failure(codeBadRequest, "the signing material is larger than the %d bytes this runner will materialise", maxReleaseSecretBytes)
		}
		out[name] = value
	}
	return out, nil
}

// releaseWrapper is the generated per-run wrapper, exec'd directly through its
// own shebang. Its three arguments are the release script, the channel and the
// signing-material file.
//
// Every line is one of the properties argued in the file header, so none of
// them can be dropped as tidying:
//
//   - It removes ITSELF before it does anything else. What is left after that
//     line is an open file descriptor with no name, so no ending — not a
//     SIGKILL, not a power cut — can leave a stray executable in somebody's
//     checkout.
//   - It SOURCES the signing material rather than exporting it, then unlinks
//     it. The values become plain shell variables, which is all `${!name}` in
//     the release script needs, and they enter no process's environment block.
//   - It SOURCES the release script rather than running it, because a new
//     process would inherit only exported variables — which is exactly what
//     this arrangement refuses to create. `$0` stays this wrapper's path, which
//     is why the wrapper is written into the script's own directory: the script
//     finds its project with `dirname "$0"/..`.
//   - Its shebang is an ABSOLUTE path. `#!/usr/bin/env bash` resolves the
//     interpreter through the inherited PATH, which is the same hole
//     releaseEnviron closes from the other side and which contradicted the
//     reason securityBin is spelled out absolutely. Two independent fixes for
//     one vector, on purpose: either alone would be undone by a later change to
//     the other.
const releaseWrapper = `#!/bin/bash
# GENERATED per release run by the TaskTrooper runner, and deleted by its own
# first line. If you are reading this in a checkout, a release was SIGKILLed
# between exec and that line; it is safe to delete.
set -eu
tt_script="$1"
tt_channel="$2"
tt_secrets="$3"
rm -f "$0"
. "$tt_secrets"
rm -f "$tt_secrets"
set -- "$tt_channel"
. "$tt_script"
`

// writeReleaseSecrets materialises one run's signing material and returns the
// path together with the removal that must follow it.
//
// The removal is ALWAYS non-nil, including on every error path, so the caller
// can defer it before it looks at the error: a half-written file is still a
// file with a signing key in it. This is writeMCPRun's shape, deliberately —
// down to crypto/rand naming the directory rather than os.MkdirTemp, so an
// unguessable name does not depend on what MkdirTemp's randomness happens to
// be in a given Go release, and to os.Mkdir rather than MkdirAll so the call
// can never adopt a directory, or follow a symlink, something else put there.
func writeReleaseRun(root, callID, scriptsDir string, secrets map[string]string) (releaseFiles, func(), *rpcError) {
	noop := func() {}
	var files releaseFiles
	if root == "" {
		root = os.TempDir()
	}

	var seed [16]byte
	if _, err := rand.Read(seed[:]); err != nil {
		return files, noop, failure(codeInternal, "could not name a directory for the signing material: %v", err)
	}
	name := releaseDirPrefix + hex.EncodeToString(seed[:])
	dir := filepath.Join(root, name)
	if err := os.Mkdir(dir, 0o700); err != nil {
		return files, noop, failure(codeInternal, "could not create the signing material directory: %v", err)
	}
	files.wrapper = filepath.Join(scriptsDir, name+".sh")
	files.workdir = filepath.Join(dir, releaseWorkDirName)
	remove := func() {
		if err := os.RemoveAll(dir); err != nil {
			// Loud, because the failure is a signing key still sitting on
			// somebody's disk with nothing left to clean it up but the sweep on
			// the next start.
			log.Error().Err(err).Str("call", callID).Str("dir", dir).Msg("could not remove the run's signing material; a key is still on disk")
		}
		// Ordinarily already gone: the wrapper unlinks itself on its first
		// line. This covers the run that never got as far as exec.
		if err := os.Remove(files.wrapper); err != nil && !os.IsNotExist(err) {
			log.Warn().Err(err).Str("call", callID).Str("path", files.wrapper).Msg("could not remove the run's wrapper script")
		}
	}

	// The script's own working directory, INSIDE this run's directory rather
	// than wherever its `mktemp -d` would have landed. That is the whole of
	// finding "the decoded secrets are swept by nobody": what the script writes
	// there is the DECODED material — dist.p12, upload.keystore, play_sa.json,
	// the temporary keychain — and while it lived in a `tmp.XXXXXX` of its own,
	// neither the deferred removal below nor sweepReleaseSecrets could name it.
	// The script takes this as TT_RELEASE_WORKDIR and falls back to its own
	// `mktemp -d` when it is unset, which is what keeps GitHub Actions working
	// unchanged; it refuses to start if the value is set and is not a directory.
	// That makes TT_RELEASE_WORKDIR the second coordinated cross-repo constant
	// in this file, beside releaseKeychainName — its other half is in
	// agent-server's internal/application/storeops/pipeline/script.go, and the
	// keychain now lives inside this directory because the script builds its
	// KEYCHAIN path out of that WORKDIR.
	if err := os.Mkdir(files.workdir, 0o700); err != nil {
		return files, remove, failure(codeInternal, "could not create the release's working directory: %v", err)
	}

	var body bytes.Buffer
	// Sorted, so two identical calls produce an identical file and a diff
	// between two runs is about what changed rather than about map order.
	names := make([]string, 0, len(secrets))
	for name := range secrets {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		// Single-quoted with the one escape a single-quoted shell word has.
		// Newlines inside are fine and are expected: a base64 secret arrives
		// wrapped.
		body.WriteString(name)
		body.WriteString("='")
		body.WriteString(strings.ReplaceAll(secrets[name], "'", `'\''`))
		body.WriteString("'\n")
	}

	files.secrets = filepath.Join(dir, releaseSecretsFile)
	// O_EXCL and 0600: created here or not at all, and nothing but this user
	// can read what is in it.
	f, err := os.OpenFile(files.secrets, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return files, remove, failure(codeInternal, "could not create the signing material file: %v", err)
	}
	if _, err := f.Write(body.Bytes()); err != nil {
		_ = f.Close()
		return files, remove, failure(codeInternal, "could not write the signing material: %v", err)
	}
	if err := f.Close(); err != nil {
		return files, remove, failure(codeInternal, "could not write the signing material: %v", err)
	}

	// 0700 and O_EXCL for the same reasons, even though it holds no secret: it
	// is a program this runner is about to execute, and one another process
	// could have put there is one this runner would execute instead.
	w, err := os.OpenFile(files.wrapper, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o700)
	if err != nil {
		return files, remove, failure(codeInternal, "could not create the run's wrapper script: %v", err)
	}
	if _, err := w.WriteString(releaseWrapper); err != nil {
		_ = w.Close()
		return files, remove, failure(codeInternal, "could not write the run's wrapper script: %v", err)
	}
	if err := w.Close(); err != nil {
		return files, remove, failure(codeInternal, "could not write the run's wrapper script: %v", err)
	}

	// The NAMES, never a value. There is a test for that.
	log.Debug().Str("call", callID).Strs("secrets", names).Msg("the release was given its signing material")
	return files, remove, nil
}

// releaseFiles is what one run put on disk: the signing material in the user's
// temp directory, and the wrapper beside the release script it has to source.
type releaseFiles struct {
	secrets string
	wrapper string
	// workdir is the script's own scratch directory, handed to it as
	// TT_RELEASE_WORKDIR so everything it decodes is inside this run's 0700
	// directory and inside the two things that clean that up.
	workdir string
}

// sweepReleaseSecrets removes signing-material directories an earlier run of
// this program left behind, and is called once at startup beside
// sweepMCPConfigs — same reasoning, same ownership check, same safety: the
// desktop app runs exactly one runner and drains it before starting another,
// so these can only be a dead process's.
//
// It covers the DECODED material as well as the at-rest material, and only
// because writeReleaseRun puts the script's WORKDIR inside this directory. It
// used to see the run's `signing.env` and nothing else: dist.p12,
// upload.keystore, play_sa.json and the temporary keychain were in a `mktemp -d`
// of the script's own, under a name with no prefix to recognise, and were swept
// by nothing at all.
func sweepReleaseSecrets(root string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		log.Debug().Err(err).Str("dir", root).Msg("could not scan for stale signing material")
		return
	}
	swept := 0
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), releaseDirPrefix) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if !ownedByThisUser(info) {
			continue
		}
		if err := os.RemoveAll(filepath.Join(root, entry.Name())); err != nil {
			log.Warn().Err(err).Str("dir", entry.Name()).Msg("could not remove stale signing material")
			continue
		}
		swept++
	}
	if swept > 0 {
		log.Info().Int("count", swept).Msg("removed signing material an unclean exit left behind")
	}
}

// sweepReleaseKeychains removes signing keychains a killed release left in the
// user's keychain search list.
//
// The release script creates that keychain and deletes it in its own EXIT
// trap, which covers every ending the script controls. It does not cover the
// process GROUP being SIGKILLed — which is what a cancelled or drained run
// does — and what that leaves is a keychain holding a distribution identity,
// in the search list of every codesign this user runs afterwards.
//
// EVERY keychain with this basename is deleted, and the criterion used to be
// the opposite: "delete the ones whose file is already gone". That was backwards
// in a way that made this function do nothing on the only path it exists for.
// The single thing that removes the file is the script's own trap, and the trap
// also runs `security delete-keychain` — so a keychain whose file is gone is one
// that is already out of the search list, and the SIGKILL case this is named
// for leaves the file exactly where it is and was skipped.
//
// "Every one of them is dead" is a true proposition HERE and only here: this
// runs once at startup, and the desktop app runs one runner and drains it
// before starting another — the same assumption sweepMCPConfigs and
// sweepReleaseSecrets are already built on. Nothing else on this Mac creates a
// keychain with this name; it is a constant this program and the generator
// share.
func sweepReleaseKeychains(security string) {
	if runtime.GOOS != "darwin" || security == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	listed, err := exec.CommandContext(ctx, security, "list-keychains", "-d", "user").Output()
	if err != nil {
		log.Debug().Err(err).Msg("could not read the keychain search list")
		return
	}
	swept := 0
	for _, line := range strings.Split(string(listed), "\n") {
		path := strings.Trim(strings.TrimSpace(line), `"`)
		if path == "" || filepath.Base(path) != releaseKeychainName {
			continue
		}
		if err := exec.CommandContext(ctx, security, "delete-keychain", path).Run(); err != nil {
			log.Warn().Err(err).Str("keychain", path).Msg("could not remove a stale release keychain")
			continue
		}
		swept++
	}
	if swept > 0 {
		log.Info().Int("count", swept).Msg("removed release keychains an unclean exit left behind")
	}
}

// sweepReleaseWrappers removes generated wrapper scripts an earlier run left in
// a checkout, and is the third of the startup sweeps.
//
// The wrapper deletes itself on its own first line, so this covers exactly one
// window: a SIGKILL between exec and that line. What is left then is a 0700
// file in a git working copy, which the next `git add -A` in that repository
// would commit. It holds no secret — only two paths — but a generated file
// arriving in somebody's pull request is a bug report nobody can explain.
//
// Bounded rather than a full walk of the workspace: a workspace is somebody's
// checkouts, node_modules included, and a startup sweep must not become a
// minute of disk. The wrapper lives beside the release script, which lives in
// the repository, so a handful of levels reaches every real layout.
func sweepReleaseWrappers(root string) {
	if root == "" {
		return
	}
	const maxDepth = 8
	skip := map[string]bool{"node_modules": true, "vendor": true, "Pods": true, "build": true, "DerivedData": true}
	swept := 0

	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			// A directory this user cannot read is not this sweep's problem.
			return nil //nolint:nilerr // an unreadable subtree is skipped, not fatal
		}
		if entry.IsDir() {
			if path == root {
				return nil
			}
			name := entry.Name()
			if skip[name] || strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			if strings.Count(strings.TrimPrefix(path, root), string(filepath.Separator)) >= maxDepth {
				return filepath.SkipDir
			}
			return nil
		}
		name := entry.Name()
		if !strings.HasPrefix(name, releaseDirPrefix) || !strings.HasSuffix(name, ".sh") {
			return nil
		}
		if removeErr := os.Remove(path); removeErr != nil {
			log.Warn().Err(removeErr).Str("path", path).Msg("could not remove a stale release wrapper")
			return nil
		}
		swept++
		return nil
	})
	if err != nil {
		log.Debug().Err(err).Str("dir", root).Msg("could not scan the workspace for stale release wrappers")
	}
	if swept > 0 {
		log.Info().Int("count", swept).Msg("removed release wrappers an unclean exit left in a checkout")
	}
}

// newSecretRedactor returns the scrubbing every byte of this call's response
// passes through, or nil when there is nothing to scrub.
//
// It works on the MARSHALLED frame rather than on each string before encoding,
// because that is the one place everything converges: output events, the
// terminal frame, and the error message inside it. A value that reached any of
// them by a path nobody thought of is still caught here.
//
// The script masks nothing outside GitHub Actions — ::add-mask:: is an Actions
// workflow command and printing it on a laptop would put the key on the
// operator's terminal instead of hiding it — so on this path the scrubbing is
// entirely this program's job.
func newSecretRedactor(secrets map[string]string) func([]byte) []byte {
	if len(secrets) == 0 {
		return nil
	}
	type pair struct{ from, to []byte }
	pairs := make([]pair, 0, 4*len(secrets))
	for name, value := range secrets {
		marker := []byte("[redacted " + name + "]")
		minLen := releaseRedactMinLen
		if hasReleaseSuffix(name, releaseAlwaysRedact) {
			minLen = 1
		}

		kept, skipped := 0, 0
		seen := make(map[string]bool)
		for _, part := range releaseSecretParts(value) {
			if seen[part] {
				continue
			}
			seen[part] = true
			// Short values are skipped: a three-character secret matches half
			// the words in a build log, and a transcript that is all redaction
			// markers hides the diagnosis rather than the key.
			if len(part) < minLen {
				skipped++
				continue
			}
			kept++
			pairs = append(pairs, pair{from: []byte(part), to: marker})
			// The JSON-encoded form as well as the raw one, because these are
			// searched for in the MARSHALLED frame: a quote or a backslash in a
			// password is `\"` there and the raw bytes would not match.
			if encoded, err := json.Marshal(part); err == nil && len(encoded) > 2 {
				if inner := encoded[1 : len(encoded)-1]; !bytes.Equal(inner, []byte(part)) {
					pairs = append(pairs, pair{from: inner, to: marker})
				}
			}
		}
		// Said out loud, because it used to be silent. A secret nothing could
		// be registered for is one that will go through the transcript in the
		// clear, and an operator has to be able to learn that from the log
		// rather than from the ticket somebody pastes it into.
		switch {
		case kept == 0:
			log.Warn().Str("secret", name).Int("length", len(value)).
				Msg("this secret is too short to scrub out of a build log and will reach the caller unredacted")
		case skipped > 0:
			log.Warn().Str("secret", name).Int("lines", skipped).
				Msg("some lines of this secret are too short to scrub and will reach the caller unredacted")
		}
	}
	if len(pairs) == 0 {
		return nil
	}
	// Longest first, so a value that contains another is replaced whole.
	sort.Slice(pairs, func(i, j int) bool { return len(pairs[i].from) > len(pairs[j].from) })
	return func(line []byte) []byte {
		for _, p := range pairs {
			line = bytes.ReplaceAll(line, p.from, p.to)
		}
		return line
	}
}

// releaseSecretParts is a secret value and every physical LINE of it.
//
// The lines are the point, and their absence was the worst of the findings this
// file was reworked for. `forward` splits the child's output on \n and makes
// each physical line its OWN frame, so a wrapped multi-line secret is never
// present whole in anything the redactor is handed — it arrives as three
// consecutive frames holding three thirds of a key, and a replacer looking for
// the whole value matched none of them. That is the entire primary class:
// IOS_DIST_CERT_P12, ASC_KEY_P8, ANDROID_UPLOAD_KEYSTORE_B64, PLAY_SA_JSON —
// every secret a base64 wrap makes multi-line, which writeReleaseRun's own
// comment already calls the expected case.
//
// The whole value is kept as well: a frame that does carry it — a terminal
// frame, an error quoting a body — has it JSON-escaped with `\n` in the middle,
// which no single line matches.
//
// A trailing \r goes, so a secret stored with CRLF line endings is matched by
// its content rather than missed by one byte.
func releaseSecretParts(value string) []string {
	parts := []string{value}
	if strings.Contains(value, "\n") {
		for _, line := range strings.Split(value, "\n") {
			if line = strings.TrimRight(line, "\r"); line != "" {
				parts = append(parts, line)
			}
		}
	}
	return parts
}

// runRelease spawns the wrapper and streams it. Structured like spawnClaude and
// for the same reasons, which are written out there: not CommandContext,
// because its cancellation kills the process and not the group; its own process
// group, because a release spawns gradle daemons, xcodebuild workers and a
// fastlane ruby; both pipes drained to EOF before Wait.
func runRelease(ctx context.Context, c *call, p *preparedRelease, files releaseFiles) (any, *rpcError) {
	// The wrapper is exec'd directly through its own shebang — no interpreter
	// named here, because this package never runs a shell. See the file header.
	cmd := exec.Command(files.wrapper, p.script, p.channel, files.secrets)
	cmd.Dir = p.project
	// The child's WHOLE environment, built from an allowlist by releaseEnviron
	// rather than inherited. Two things have to stay true on this line: the
	// signing material is not here (see the file header), and neither is
	// anything this process was started with that nobody chose — os.Environ()
	// here is what handed a wrapper BASH_ENV, SHELLOPTS=xtrace and
	// GITHUB_ACTIONS, and all three were reachable with one `launchctl setenv`.
	cmd.Env = append(append([]string(nil), p.env...), "TT_RELEASE_WORKDIR="+files.workdir)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, failure(codeInternal, "could not open the release's stdout: %v", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, failure(codeInternal, "could not open the release's stderr: %v", err)
	}
	// Closed rather than left inherited: a release that stopped for input would
	// wait for a terminal that is not there, and read EOF instead.
	cmd.Stdin = nil

	started := time.Now()
	group, err := startProcessGroup(cmd)
	if err != nil {
		return nil, failure(codeInternal, "could not start the release script: %v", err)
	}
	defer group.release()
	log.Info().Str("call", c.id).Int("pid", cmd.Process.Pid).
		Str("platform", p.platform).Str("channel", p.channel).Msg("release started")

	forwardCtx, stopForwarding := context.WithCancel(ctx)
	defer stopForwarding()

	var streams sync.WaitGroup
	streams.Add(2)
	go func() { defer streams.Done(); forward(c, "stdout", stdout, stopForwarding) }()
	go func() { defer streams.Done(); forward(c, "stderr", stderr, stopForwarding) }()

	exited := make(chan struct{})
	killed := make(chan struct{})
	go func() {
		select {
		case <-forwardCtx.Done():
			close(killed)
			killProcessGroup(group, claudeGrace, exited)
		case <-exited:
		}
	}()

	streams.Wait()

	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()

	var runErr error
	select {
	case runErr = <-waitErr:
	case <-time.After(claudeReapTimeout):
		close(exited)
		return nil, failure(codeInternal, "the release would not exit %s after being killed", claudeReapTimeout)
	}
	close(exited)

	result := mobileReleaseResult{DurationMS: time.Since(started).Milliseconds()}
	var exitErr *exec.ExitError
	switch {
	case runErr == nil:
		result.ExitCode = 0
	case errors.As(runErr, &exitErr):
		result.ExitCode = exitErr.ExitCode()
	default:
		return nil, failure(codeInternal, "waiting for the release: %v", runErr)
	}
	// Read AFTER the run whatever it managed to produce, including on a failure:
	// a refused upload still leaves a signed build, and the first publish on
	// either store is uploaded by hand from exactly that file.
	result.Artifact = findReleaseArtifact(p.project, started)

	select {
	case <-killed:
		if ctx.Err() != nil && c.ctx.Err() == nil {
			return nil, failure(codeCancelled, "the release passed its limit and was stopped after %dms", time.Since(started).Milliseconds())
		}
	default:
	}

	log.Info().Str("call", c.id).Int("exit", result.ExitCode).Dur("took", time.Since(started)).Msg("release finished")
	return result, nil
}

// findReleaseArtifact reports the signed build the run produced.
//
// Read off the DIRECTORY the script writes into rather than parsed out of its
// log: the log is prose that changes whenever a message is reworded, and the
// path is a contract between the generator and this file. Bounded to files this
// run touched — see below.
func findReleaseArtifact(project string, started time.Time) string {
	dir := filepath.Join(project, "build", "mobile-release")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	newest, newestAt := "", time.Time{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		switch strings.ToLower(filepath.Ext(entry.Name())) {
		case ".ipa", ".aab":
		default:
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		// Only what THIS run wrote. Without the comparison a build that failed
		// before it produced anything reported the previous run's signed
		// binary as its own output — onto a board, and into the hands of the
		// person who uploads the first release of an app to a store by hand.
		if !info.ModTime().After(started) {
			continue
		}
		if info.ModTime().After(newestAt) {
			newest, newestAt = filepath.Join(dir, entry.Name()), info.ModTime()
		}
	}
	return newest
}

// securityBin is macOS's keychain tool. An absolute path rather than a PATH
// lookup: this runs a command that DELETES a keychain, and a PATH the user
// controls is not where that decision should come from.
const securityBin = "/usr/bin/security"

// handleMobileRelease is the streaming half of this file: the same shape
// handleClaudeRun has, because it makes the same four promises — refuse
// everything refusable before the 200, be cancellable by id from the first
// instant, take a session slot so a release and a Claude run do not fight over
// one Mac, and put the run's credentials on disk only inside a deferred
// removal.
func (s *runnerServer) handleMobileRelease(w http.ResponseWriter, r *http.Request) {
	body, readErr := readBody(r)
	if readErr != nil {
		writeError(w, readErr)
		return
	}
	var req mobileReleaseRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, failure(codeBadRequest, "the request body is not the expected object: %v", err))
		return
	}

	prepared, prepErr := prepareRelease(s.cfg, req.mobileReleaseParams)
	if prepErr != nil {
		writeError(w, prepErr)
		return
	}

	id := req.ID
	if id == "" {
		id = newCallID()
	}

	runCtx, cancel := context.WithCancel(r.Context())
	defer cancel()
	if !s.register(id, cancel) {
		writeError(w, failure(codeBadRequest, "a call with id %q is already running on this machine", id))
		return
	}
	defer s.unregister(id)

	if !s.enterRun() {
		writeError(w, failure(codeCancelled, "this machine's tunnel session is shutting down and is not taking new runs"))
		return
	}
	defer s.leaveRun()

	if runCtx.Err() != nil {
		writeError(w, failure(codeCancelled, "the call was cancelled before it started"))
		return
	}
	select {
	case s.sem <- struct{}{}:
		if runCtx.Err() != nil {
			<-s.sem
			writeError(w, failure(codeCancelled, "the call was cancelled while it was waiting for a free session slot"))
			return
		}
		defer func() { <-s.sem }()
	case <-runCtx.Done():
		writeError(w, failure(codeCancelled, "the call was cancelled while it was waiting for a free session slot"))
		return
	}

	// The signing material becomes a file for exactly as long as this run, and
	// the defer is the whole guarantee: it fires however the call ends — a
	// return from here, the script finishing or failing, a POST /cancel, the
	// tunnel dropping and cancelling the request context under it, or a panic
	// unwinding the handler. Registered BEFORE the error is looked at, because a
	// half-written file is still a file with a key in it, and BEFORE the 200, so
	// a disk that refused is a status rather than a frame nobody reads.
	//
	// Registered after `defer s.leaveRun()` so it runs first: by the time a
	// teardown reports "tunnel detached", the keys of the runs it was serving
	// are already off the disk.
	files, removeRunFiles, filesErr := writeReleaseRun(s.mcpRoot, id, filepath.Dir(prepared.script), prepared.secrets)
	defer removeRunFiles()
	if filesErr != nil {
		writeError(w, filesErr)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, failure(codeInternal, "this response cannot be streamed"))
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	c := &call{
		ctx:   runCtx,
		id:    id,
		cfg:   s.cfg,
		state: s.state,
		// Deliberately nil, where every other method keeps the body. Nothing on
		// this path reads it — prepareRelease already has the parsed params —
		// and what it is is the plaintext JSON of a distribution certificate,
		// which would otherwise stay live in this struct for the eighty-eight
		// minutes the run is allowed. A core dump or a crash reporter reads
		// exactly that.
		params: nil,
		w:      w,
		flush:  flusher.Flush,
		// Set before the FIRST frame, so there is no window — not even the
		// `started` line — in which this response is unscrubbed.
		redact: newSecretRedactor(prepared.secrets),
	}

	if err := c.emit(startedEvent{V: protocolVersion, ID: id, Event: "started"}); err != nil {
		log.Debug().Str("call", id).Err(err).Msg("caller went away before the release started")
		return
	}

	spawnCtx, stopTimeout := context.WithTimeout(runCtx, prepared.timeout)
	defer stopTimeout()

	result, callErr := runRelease(spawnCtx, c, prepared, files)
	if callErr == nil && runCtx.Err() != nil {
		callErr = failure(codeCancelled, "the call was cancelled")
	}
	c.finish(result, callErr)
}
