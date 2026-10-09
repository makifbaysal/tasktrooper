//go:build !windows

package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The release path is tested against a real process and a real disk, for the
// same reason the MCP config is: every claim worth making here is about what a
// child was actually handed and what is left on somebody's Mac afterwards, and
// none of them can be checked by reading the code.
//
// What is asserted, and why each is worth a test rather than a comment:
//
//   - The signing material REACHES the script — a run that could not sign is a
//     run that fails an hour in — and reaches it as a shell variable, so it is
//     in no environment block and no argv. Both halves are observed from where
//     the script stands, not from where the runner does.
//   - No secret value appears anywhere in the response. Not in an output line,
//     not in the terminal frame, not in an error. The script masks nothing off
//     GitHub Actions, deliberately, so this is the only thing standing between
//     a distribution key and a log somebody pastes into a ticket.
//   - Nothing is left behind, on every ending — including the SIGKILL one,
//     which is the one a future change breaks without noticing.

// releaseKey is a stand-in for the worst thing in the vault, and it is
// WRAPPED, which is the shape that matters.
//
// It used to be one line, and a one-line fixture is why the redactor's worst
// hole survived a test suite that looked like it covered redaction. `forward`
// splits a child's output on \n and emits each physical line as its own NDJSON
// frame, so a multi-line secret is never whole in anything the redactor sees:
// it arrives as three consecutive frames holding three thirds of a key. Every
// primary secret is this shape — IOS_DIST_CERT_P12, ASC_KEY_P8,
// ANDROID_UPLOAD_KEYSTORE_B64, PLAY_SA_JSON — because base64 arrives wrapped,
// which writeReleaseRun's own comment already says it expects.
//
//nolint:gosec // a fixture, not a credential
const releaseKey = "-----BEGIN PRIVATE KEY-----\n" +
	"MIGTAgEAMBMGByqGSM49AgEGCCqGSM49AwEHBHkwdwIBAQQgQ2xhdWRlQ29kZUZp\n" +
	"eHR1cmVOb3RBQ3JlZGVudGlhbEF0QWxsb0F0YWxsoAoGCCqGSM49AwEHoUQDQgAE\n" +
	"dGVzdGZpeHR1cmVvbmx5\n" +
	"-----END PRIVATE KEY-----"

// releaseKeyLines is what actually has to be absent from a response: the
// redactor is handed one physical line at a time, so "the whole value did not
// appear" is a claim the pipeline satisfies by construction and proves nothing.
func releaseKeyLines() []string { return strings.Split(releaseKey, "\n") }

// assertNoKeyInTranscript is the end-to-end leak assertion, driven by what the
// pipeline produced rather than by a frame a test wrote by hand.
func assertNoKeyInTranscript(t *testing.T, raw string) {
	t.Helper()
	for i, line := range releaseKeyLines() {
		if strings.Contains(raw, line) {
			t.Errorf("line %d of a signing key reached the caller unredacted (%q); the whole response is a log somebody pastes into a ticket", i+1, line)
		}
	}
	// The JSON-escaped whole value, for a frame that does carry it end to end.
	if strings.Contains(raw, strings.ReplaceAll(releaseKey, "\n", `\n`)) {
		t.Error("the JSON-escaped form of a signing key reached the caller")
	}
}

// reportingReleaseScript is a stand-in for the generated release script. It
// records what it was given from INSIDE the run — the project it resolved, the
// channel, the secret it can see, its own environment and its own argv — and
// then behaves however the test needs.
func reportingReleaseScript(tail string) string {
	return `#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
printf '%s\n' "$PWD" > project.txt
printf '%s\n' "${1:-}" > channel.txt
printf '%s\n' "${ASC_KEY_P8:-MISSING}" > secret.txt
printf '%s\n' "${BUILD_NUMBER:-MISSING}" > buildnumber.txt
printf '%s\n' "${TT_RELEASE_WORKDIR:-MISSING}" > workdir.txt
env > env.txt
ps -o command= -p $$ > argv.txt 2>/dev/null || true
# What a real release decodes into its WORKDIR: the plaintext of everything the
# vault holds at rest as base64. Nothing swept this while the script chose the
# directory itself.
printf 'decoded distribution certificate' > "${TT_RELEASE_WORKDIR:-.}/dist.p12"
mkdir -p build/mobile-release
: > build/mobile-release/App.ipa
echo "stdout carries it: ${ASC_KEY_P8:-MISSING}"
echo "so does stderr: ${ASC_KEY_P8:-MISSING}" >&2
` + tail
}

// newReleaseWorkspace lays out a checkout the way workspace.prepare would, with
// the release script where the generator puts it.
func newReleaseWorkspace(t *testing.T, script string) (workspace, project string) {
	t.Helper()
	// Resolved, because prepareRelease resolves the script's symlinks and
	// compares the result against a resolved root — on macOS t.TempDir() is
	// under /var, which is a link to /private/var, so an unresolved workspace
	// here would be comparing two spellings of one directory.
	workspace, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolving the workspace: %v", err)
	}
	project = filepath.Join(workspace, "repos", "app")
	scripts := filepath.Join(project, "scripts")
	if err := os.MkdirAll(scripts, 0o755); err != nil {
		t.Fatalf("laying out the checkout: %v", err)
	}
	if err := os.WriteFile(filepath.Join(scripts, "mobile-release.sh"), []byte(script), 0o755); err != nil {
		t.Fatalf("writing the release script: %v", err)
	}
	return workspace, project
}

func newReleaseHarness(t *testing.T, workspace, root string) *runHarness {
	t.Helper()
	cfg := config{
		claudeBin:         "/usr/bin/true",
		gitBin:            "/usr/bin/git",
		workspaceDir:      workspace,
		embeddingsBaseURL: "http://127.0.0.1:1234/v1",
		embeddingModel:    "nomic-embed-text-v1.5",
	}
	runner := newRunnerServer(cfg, newState())
	runner.mcpRoot = root
	srv := httptest.NewServer(runner.handler())
	t.Cleanup(srv.Close)
	return &runHarness{srv: srv, client: srv.Client()}
}

// releaseScriptDigest is what the caller sends as script_sha256. Computed off
// the file on disk, the way the generator computes it in the process that makes
// the call, so the test exercises the comparison rather than a constant.
func releaseScriptDigestOf(t *testing.T, project string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(project, "scripts", "mobile-release.sh"))
	if err != nil {
		t.Fatalf("reading the release script: %v", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func releaseBody(t *testing.T, project string, overrides map[string]any) string {
	t.Helper()
	body := map[string]any{
		"id":            "rel-1",
		"workspace":     "repos/app",
		"platform":      "android",
		"channel":       "stage",
		"script":        "scripts/mobile-release.sh",
		"script_sha256": releaseScriptDigestOf(t, project),
		"build_number":  214,
		"secrets": map[string]string{
			"ASC_KEY_P8": releaseKey,
			"ASC_KEY_ID": "HVM8HKJ5G5",
		},
	}
	for k, v := range overrides {
		if v == nil {
			delete(body, k)
			continue
		}
		body[k] = v
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("encoding the release body: %v", err)
	}
	return string(raw)
}

// postRelease runs one release and returns the RAW response bytes together with
// the terminal frame. Raw, because the strongest form of "no secret leaked" is
// a statement about every byte the caller received rather than about the fields
// this test remembered to look at.
func postRelease(t *testing.T, h *runHarness, body string) (string, map[string]any) {
	t.Helper()
	res, err := h.client.Post(h.srv.URL+"/mobile.release", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST /mobile.release: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("POST /mobile.release answered %d", res.StatusCode)
	}

	var raw strings.Builder
	var done map[string]any
	scanner := bufio.NewScanner(res.Body)
	scanner.Buffer(make([]byte, 0, 64<<10), 8<<20)
	for scanner.Scan() {
		line := scanner.Text()
		raw.WriteString(line)
		raw.WriteString("\n")
		var frame map[string]any
		if err := json.Unmarshal([]byte(line), &frame); err != nil {
			t.Fatalf("a line of the response is not a frame: %q", line)
		}
		if frame["event"] == "done" {
			done = frame
		}
	}
	if done == nil {
		t.Fatalf("the response ended with no terminal frame:\n%s", raw.String())
	}
	return raw.String(), done
}

func awaitPathExists(t *testing.T, path string, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s never appeared within %s", path, within)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func readRunFile(t *testing.T, project, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(project, name))
	if err != nil {
		t.Fatalf("the release script did not write %s: %v", name, err)
	}
	return strings.TrimSpace(string(raw))
}

// The whole arrangement, observed from inside the run: the script resolved its
// own project, took the channel as its only argument, could read the signing
// material — and that material was in neither its environment nor its argv,
// which is where a same-user process on macOS would have read it.
func TestMobileReleaseHandsTheScriptItsSecretsButNoEnvironmentOrArgv(t *testing.T) {
	workspace, project := newReleaseWorkspace(t, reportingReleaseScript("exit 0\n"))
	root := t.TempDir()
	h := newReleaseHarness(t, workspace, root)

	raw, done := postRelease(t, h, releaseBody(t, project, nil))
	if ok, _ := done["ok"].(bool); !ok {
		t.Fatalf("the release failed: %v", done)
	}

	// It arrived. A release that cannot sign is a release that fails an hour in.
	if got := readRunFile(t, project, "secret.txt"); got != releaseKey {
		t.Fatalf("the script saw %q as ASC_KEY_P8; the signing material did not reach it", got)
	}
	// $0 is the wrapper, and the wrapper is beside the script for exactly this
	// reason: `dirname "$0"/..` has to be the project.
	if got := readRunFile(t, project, "project.txt"); got != project {
		t.Fatalf("the script resolved its project as %q, want %q", got, project)
	}
	if got := readRunFile(t, project, "channel.txt"); got != "stage" {
		t.Fatalf("the script's only argument was %q, want stage", got)
	}
	// The non-secret knobs DO travel in the environment, which is where they
	// belong: a build number is not a credential.
	if got := readRunFile(t, project, "buildnumber.txt"); got != "214" {
		t.Fatalf("BUILD_NUMBER was %q, want 214", got)
	}

	// The two places a same-user process can read.
	if env := readRunFile(t, project, "env.txt"); strings.Contains(env, releaseKey) {
		t.Error("a signing key is in the child's environment block; ps eww can read it")
	}
	if argv := readRunFile(t, project, "argv.txt"); strings.Contains(argv, releaseKey) {
		t.Error("a signing key is in the child's argv, which is world-readable on macOS")
	}

	// And nowhere in what went back to the caller, even though the script
	// printed it on both streams — which, because the value is wrapped, is
	// several frames each holding one line of it.
	assertNoKeyInTranscript(t, raw)
	if !strings.Contains(raw, "[redacted ASC_KEY_P8]") {
		t.Error("the redaction did not name what it removed, so the log is unreadable rather than merely safe")
	}
	if !strings.Contains(raw, "stdout carries it") || !strings.Contains(raw, "so does stderr") {
		t.Error("the transcript around the redaction was lost; a scrub that hides the diagnosis is worse than the leak")
	}
}

// A failed build is still a result, and its error text is scrubbed like
// everything else: a refusal quoting what it could not import is exactly the
// message a secret escapes in.
func TestMobileReleaseScrubsAFailedRunToo(t *testing.T) {
	workspace, project := newReleaseWorkspace(t, reportingReleaseScript(
		"echo \"error: could not import ${ASC_KEY_P8}\" >&2\nexit 65\n"))
	h := newReleaseHarness(t, workspace, t.TempDir())

	raw, done := postRelease(t, h, releaseBody(t, project, nil))
	if ok, _ := done["ok"].(bool); !ok {
		t.Fatalf("a non-zero script is a RESULT, not a broken call: %v", done)
	}
	result, _ := done["result"].(map[string]any)
	if code, _ := result["exit_code"].(float64); code != 65 {
		t.Fatalf("exit_code = %v, want 65", result["exit_code"])
	}
	assertNoKeyInTranscript(t, raw)
}

// The build is read off the directory the script writes into, so a person can
// find it — the first publish on either store is uploaded by hand from exactly
// that path.
func TestMobileReleaseReportsWhatTheRunProduced(t *testing.T) {
	workspace, project := newReleaseWorkspace(t, reportingReleaseScript("exit 0\n"))
	h := newReleaseHarness(t, workspace, t.TempDir())

	_, done := postRelease(t, h, releaseBody(t, project, nil))
	result, _ := done["result"].(map[string]any)
	want := filepath.Join(project, "build", "mobile-release", "App.ipa")
	if got, _ := result["artifact"].(string); got != want {
		t.Fatalf("artifact = %q, want %q", got, want)
	}
}

// Nothing is left behind. The wrapper deletes itself on its first line and the
// signing material goes with the run's deferred removal, so both statements are
// about an empty directory rather than about a function having been called.
func TestMobileReleaseLeavesNothingOnDisk(t *testing.T) {
	workspace, project := newReleaseWorkspace(t, reportingReleaseScript("exit 0\n"))
	root := t.TempDir()
	h := newReleaseHarness(t, workspace, root)

	workdir := ""
	func() {
		postRelease(t, h, releaseBody(t, project, nil))
		workdir = readRunFile(t, project, "workdir.txt")
	}()

	// The script's own working directory was OURS to name, so what it decoded
	// into it is inside the run directory the removal below covers. While the
	// script chose it with `mktemp -d`, dist.p12, upload.keystore and
	// play_sa.json were in a directory with no prefix any sweep could
	// recognise, and nothing ever removed them.
	if workdir == "" || workdir == "MISSING" {
		t.Fatal("the script was not given a TT_RELEASE_WORKDIR; it fell back to a mktemp directory no sweep can find")
	}
	if !strings.HasPrefix(workdir, root+string(filepath.Separator)) {
		t.Fatalf("TT_RELEASE_WORKDIR was %q, which is not inside this run's directory under %q", workdir, root)
	}
	if _, err := os.Stat(filepath.Join(workdir, "dist.p12")); err == nil {
		t.Error("the decoded signing material survived the run")
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("reading the run root: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), releaseDirPrefix) {
			t.Fatalf("%s survived the run — signing material is still on disk", entry.Name())
		}
	}
	scripts, err := os.ReadDir(filepath.Join(project, "scripts"))
	if err != nil {
		t.Fatalf("reading the scripts directory: %v", err)
	}
	for _, entry := range scripts {
		if strings.HasPrefix(entry.Name(), releaseDirPrefix) {
			t.Fatalf("%s survived the run — a generated wrapper is still in the checkout", entry.Name())
		}
	}
}

// The ending that cannot run a deferred function is the one that matters. A
// script that ignores SIGTERM is killed with its group; the run's files have to
// be gone anyway.
func TestMobileReleaseLeavesNothingWhenItIsCancelled(t *testing.T) {
	// `ready` is written by the script AFTER it has taken everything it needs
	// and immediately before it stops answering SIGTERM. Cancelling on the
	// `started` frame instead would race the spawn and pass without ever
	// reaching the escalation this test is about.
	workspace, project := newReleaseWorkspace(t, reportingReleaseScript(
		": > ready\ntrap '' TERM\nwhile true; do sleep 1; done\n"))
	root := t.TempDir()
	h := newReleaseHarness(t, workspace, root)

	finished := make(chan struct{})
	go func() {
		defer close(finished)
		res, err := h.client.Post(h.srv.URL+"/mobile.release", "application/json",
			strings.NewReader(releaseBody(t, project, nil)))
		if err != nil {
			return
		}
		defer func() { _ = res.Body.Close() }()
		scanner := bufio.NewScanner(res.Body)
		for scanner.Scan() {
		}
	}()

	awaitPathExists(t, filepath.Join(project, "ready"), 30*time.Second)
	// The secrets file is already gone — the wrapper unlinks it the moment it
	// has been read — so what this proves is that the DIRECTORY goes too, after
	// a process group that had to be SIGKILLed.
	h.cancel(t, "rel-1")

	select {
	case <-finished:
	case <-time.After(drainBudget + 30*time.Second):
		t.Fatal("the cancelled release never ended")
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("reading the run root: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), releaseDirPrefix) {
			t.Fatalf("%s survived a cancelled run — signing material is still on disk", entry.Name())
		}
	}
	scripts, _ := os.ReadDir(filepath.Join(project, "scripts"))
	for _, entry := range scripts {
		if strings.HasPrefix(entry.Name(), releaseDirPrefix) {
			t.Fatalf("%s survived a cancelled run", entry.Name())
		}
	}
}

// The file's permissions and its shape, from the one place both can be seen.
func TestReleaseSecretsFileIsPrivateAndSourceable(t *testing.T) {
	root := t.TempDir()
	scripts := t.TempDir()
	files, remove, callErr := writeReleaseRun(root, "rel-1", scripts, map[string]string{
		// A value with the two things that break a naive writer: a quote and a
		// newline. Every wrapped base64 secret has the second.
		"ASC_KEY_P8": "line one\nit's line two\n",
	})
	defer remove()
	if callErr != nil {
		t.Fatalf("writing the run's files: %v", callErr)
	}

	info, err := os.Stat(files.secrets)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("the signing material is mode %o, want 600", perm)
	}
	dirInfo, err := os.Stat(filepath.Dir(files.secrets))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0o700 {
		t.Errorf("the run directory is mode %o, want 700", perm)
	}
	// The script's own WORKDIR, which is the whole reason the DECODED material
	// is inside something the removal and the sweep can name.
	workInfo, err := os.Stat(files.workdir)
	if err != nil {
		t.Fatalf("the run's working directory was not created: %v", err)
	}
	if perm := workInfo.Mode().Perm(); perm != 0o700 {
		t.Errorf("the working directory is mode %o, want 700", perm)
	}
	if filepath.Dir(files.workdir) != filepath.Dir(files.secrets) {
		t.Errorf("the working directory is %q, which is not inside this run's directory", files.workdir)
	}

	body, err := os.ReadFile(files.secrets)
	if err != nil {
		t.Fatalf("reading the signing material: %v", err)
	}
	// Single-quoted with the one escape a single-quoted shell word has, so a
	// quote in a key cannot end the assignment and start a command.
	if got, want := string(body), "ASC_KEY_P8='line one\nit'\\''s line two\n'\n"; got != want {
		t.Errorf("the file is\n%q\nwant\n%q", got, want)
	}

	wrapper, err := os.Stat(files.wrapper)
	if err != nil {
		t.Fatalf("the wrapper was not written: %v", err)
	}
	if perm := wrapper.Mode().Perm(); perm != 0o700 {
		t.Errorf("the wrapper is mode %o, want 700", perm)
	}
}

// Everything refusable is refused before the 200, so a caller sees a status
// rather than a frame it has to read a stream to reach.
func TestMobileReleaseRefusesBeforeTheStream(t *testing.T) {
	workspace, project := newReleaseWorkspace(t, reportingReleaseScript("exit 0\n"))
	h := newReleaseHarness(t, workspace, t.TempDir())

	cases := map[string]map[string]any{
		"a channel that is neither":     {"channel": "canary"},
		"a platform this cannot ship":   {"platform": "web"},
		"a script outside the checkout": {"script": "../../../../usr/bin/true"},
		"a script that is not there":    {"script": "scripts/absent.sh"},
		"no workspace":                  {"workspace": ""},
		"a secret name that is not one": {"secrets": map[string]string{"PATH; rm -rf /": "value"}},
		"a secret with no value":        {"secrets": map[string]string{"ASC_KEY_P8": ""}},
		"a rollout that is not one":     {"rollout": "$(whoami)"},
		// The script is only "the file we generated" if somebody checked.
		"no digest for the script": {"script_sha256": ""},
		"a digest that is not the script's": {
			"script_sha256": "0000000000000000000000000000000000000000000000000000000000000000",
		},
		"a digest that is not a digest": {"script_sha256": "not-hex"},
		// A password too short to scrub is one that reaches the caller in the
		// clear, and unlike a key alias it is a value the person can re-export.
		"a signing password too short to redact": {
			"secrets": map[string]string{"ASC_KEY_P8": releaseKey, "ANDROID_KEYSTORE_PASSWORD": "hunter2"},
		},
	}
	for name, override := range cases {
		t.Run(name, func(t *testing.T) {
			res, err := h.client.Post(h.srv.URL+"/mobile.release", "application/json",
				strings.NewReader(releaseBody(t, project, override)))
			if err != nil {
				t.Fatalf("POST /mobile.release: %v", err)
			}
			defer func() { _ = res.Body.Close() }()
			if res.StatusCode == http.StatusOK {
				t.Fatalf("answered 200; a refusal after the stream starts is one nobody reads")
			}
		})
	}
}

// The one ending no defer can cover: the process killed outright. What it
// leaves is a file of signing material, and this is what clears it.
func TestSweepReleaseSecretsRemovesWhatAKillLeftBehind(t *testing.T) {
	root := t.TempDir()
	stale := filepath.Join(root, releaseDirPrefix+"deadbeef")
	if err := os.Mkdir(stale, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stale, releaseSecretsFile), []byte("ASC_KEY_P8='x'\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Somebody else's directory, which this must not touch.
	other := filepath.Join(root, "someone-elses-work")
	if err := os.Mkdir(other, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	sweepReleaseSecrets(root)

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("the stale run directory survived the sweep; a signing key is still on disk")
	}
	if _, err := os.Stat(other); err != nil {
		t.Error("the sweep removed a directory that is not ours")
	}
}

// The redactor is what stands between a key and a log, so its edges are worth
// stating one at a time. The first of them is the one that was missing: a
// secret is scrubbed LINE BY LINE, because a frame never carries more than one
// line of it.
func TestSecretRedactorWorksOnTheLinesAFrameActuallyCarries(t *testing.T) {
	redact := newSecretRedactor(map[string]string{
		"ASC_KEY_P8":         "first line of it\nsecond line of it",
		"SHORT":              "abc",
		"IOS_CERT_PASSWORD":  "s3cr3t!!",
		"ANDROID_KEY_ALIAS":  "key0",
		"ANDROID_KEY_QUOTED": `has "quotes" in it`,
	})
	if redact == nil {
		t.Fatal("a redactor was expected")
	}
	frameFor := func(text string) string {
		t.Helper()
		raw, err := json.Marshal(map[string]string{"data": text})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		return string(redact(raw))
	}

	// One physical line of a wrapped secret, which is what `forward` emits.
	if got := frameFor("second line of it"); strings.Contains(got, "second line of it") {
		t.Errorf("a single line of a multi-line secret survived: %s", got)
	} else if !strings.Contains(got, "[redacted ASC_KEY_P8]") {
		t.Errorf("the line was not named: %s", got)
	}
	// And the whole value in one frame, JSON-escaped, still goes.
	if got := frameFor("key=first line of it\nsecond line of it"); strings.Contains(got, `first line of it\nsecond`) {
		t.Errorf("the JSON-escaped whole value survived: %s", got)
	}
	// A quote in a value is `\"` in the frame, so the encoded form has to be in
	// the dictionary as well as the raw one.
	if got := frameFor(`has "quotes" in it`); strings.Contains(got, `quotes`) {
		t.Errorf("a value with a quote in it survived its own encoding: %s", got)
	}
	// A short value of no particular class stays, or the transcript becomes
	// markers and hides the diagnosis rather than the key.
	if got := frameFor("abc def"); !strings.Contains(got, "abc") {
		t.Errorf("a three-character value was redacted: %s", got)
	}
	// A password and an alias are scrubbed at ANY length. These are values the
	// USER put in the vault — `key0` out of Android Studio, whatever was typed
	// into Keychain Access during a .p12 export — not values this side mints,
	// which is what the old eight-character floor got wrong about them.
	if got := frameFor("keytool -alias key0"); strings.Contains(got, "key0") {
		t.Errorf("a four-character key alias survived the floor: %s", got)
	}
	if got := frameFor("security import -P s3cr3t!!"); strings.Contains(got, "s3cr3t!!") {
		t.Errorf("an eight-character signing password survived: %s", got)
	}

	if newSecretRedactor(nil) != nil {
		t.Error("a release with no signing material needs no redactor")
	}
}

// The environment the release child is given, read from inside it. Every name
// below was verified to be exploitable against the version of this file that
// passed os.Environ() straight through.
func TestMobileReleaseGivesTheChildACuratedEnvironment(t *testing.T) {
	workspace, project := newReleaseWorkspace(t, reportingReleaseScript("exit 0\n"))
	h := newReleaseHarness(t, workspace, t.TempDir())

	// BASH_ENV is the sharpest of the three: a non-interactive bash SOURCES it
	// at startup, so this file would run inside the wrapper's own shell one
	// line before the signing material is read into that same shell.
	pwn := filepath.Join(t.TempDir(), "pwn.sh")
	marker := filepath.Join(project, "PWNED")
	if err := os.WriteFile(pwn, []byte(": > "+marker+"\n"), 0o700); err != nil {
		t.Fatalf("writing the BASH_ENV payload: %v", err)
	}
	t.Setenv("BASH_ENV", pwn)
	// bash turns xtrace on at startup from this, and `set -euo pipefail` does
	// not turn it off: every command reaches stderr, `security import … -P
	// <password>` included.
	t.Setenv("SHELLOPTS", "xtrace")
	// With this inherited the script's mask_lines prints the DECODED .p8 and
	// play_sa.json to stdout line by line, which the redactor's dictionary —
	// built from the at-rest base64 — does not hold.
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("CDPATH", "/tmp")
	t.Setenv("DYLD_INSERT_LIBRARIES", "/tmp/evil.dylib")
	// The shebang used to resolve bash through this.
	t.Setenv("PATH", "/tmp/evil:"+os.Getenv("PATH"))

	_, done := postRelease(t, h, releaseBody(t, project, nil))
	if ok, _ := done["ok"].(bool); !ok {
		t.Fatalf("the release failed: %v", done)
	}

	if _, err := os.Stat(marker); err == nil {
		t.Error("BASH_ENV ran: injected code executed inside the wrapper, one line before it sourced the signing material")
	}
	env := readRunFile(t, project, "env.txt")
	for _, name := range []string{"BASH_ENV", "SHELLOPTS", "BASHOPTS", "BASH_XTRACEFD", "ENV", "GITHUB_ACTIONS", "CDPATH", "IFS", "DYLD_INSERT_LIBRARIES"} {
		if strings.Contains(env, "\n"+name+"=") || strings.HasPrefix(env, name+"=") {
			t.Errorf("%s reached the release child; its environment is an allowlist, not what this process happened to be started with", name)
		}
	}
	if want := "PATH=" + releasePathBase; !strings.Contains(env, want) {
		t.Errorf("the child's PATH is not the fixed one; env was:\n%s", env)
	}
	if strings.Contains(env, "/tmp/evil") {
		t.Error("an inherited PATH entry survived; the wrapper's own interpreter resolves through PATH")
	}
}

// A link standing where the generated release script should be runs a program
// from outside the workspace with this app's signing material attached — which
// is the containment rule this whole package is built on.
func TestMobileReleaseRefusesAScriptThatIsASymlink(t *testing.T) {
	workspace, project := newReleaseWorkspace(t, reportingReleaseScript("exit 0\n"))
	body := releaseBody(t, project, nil)

	outside := filepath.Join(t.TempDir(), "evil.sh")
	if err := os.WriteFile(outside, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatalf("writing the target: %v", err)
	}
	script := filepath.Join(project, "scripts", "mobile-release.sh")
	if err := os.Remove(script); err != nil {
		t.Fatalf("removing the real script: %v", err)
	}
	if err := os.Symlink(outside, script); err != nil {
		t.Fatalf("linking: %v", err)
	}

	h := newReleaseHarness(t, workspace, t.TempDir())
	res, err := h.client.Post(h.srv.URL+"/mobile.release", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST /mobile.release: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode == http.StatusOK {
		t.Fatal("a symlinked release script was accepted; it runs a program outside the workspace with a distribution certificate in scope")
	}
}

// A build that produced nothing must report nothing. The previous run's signed
// binary is still in that directory, and it is the path a person is handed for
// the first upload of an app to a store.
func TestMobileReleaseDoesNotReportAnEarlierRunsArtifact(t *testing.T) {
	workspace, project := newReleaseWorkspace(t, "#!/bin/bash\nexit 3\n")
	h := newReleaseHarness(t, workspace, t.TempDir())

	dir := filepath.Join(project, "build", "mobile-release")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	stale := filepath.Join(dir, "App.ipa")
	if err := os.WriteFile(stale, []byte("last week's signed build"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	old := time.Now().Add(-24 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	_, done := postRelease(t, h, releaseBody(t, project, nil))
	result, _ := done["result"].(map[string]any)
	if got, _ := result["artifact"].(string); got != "" {
		t.Fatalf("artifact = %q; a failed build reported a binary it did not produce", got)
	}
}

// The wrapper deletes itself on its first line, so what this covers is the one
// window that line cannot: a SIGKILL between exec and it. What is left is a
// 0700 file in a git working copy that the next `git add -A` would commit.
func TestSweepReleaseWrappersClearsTheCheckout(t *testing.T) {
	root := t.TempDir()
	scripts := filepath.Join(root, "repos", "app", "scripts")
	if err := os.MkdirAll(scripts, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	stray := filepath.Join(scripts, releaseDirPrefix+"deadbeef.sh")
	if err := os.WriteFile(stray, []byte("#!/bin/bash\n"), 0o700); err != nil {
		t.Fatalf("write: %v", err)
	}
	// The repository's own scripts, which this must not touch.
	theirs := filepath.Join(scripts, "mobile-release.sh")
	if err := os.WriteFile(theirs, []byte("#!/bin/bash\n"), 0o755); err != nil {
		t.Fatalf("write: %v", err)
	}

	sweepReleaseWrappers(root)

	if _, err := os.Stat(stray); !os.IsNotExist(err) {
		t.Error("a generated wrapper survived in the checkout; the next `git add -A` commits it")
	}
	if _, err := os.Stat(theirs); err != nil {
		t.Error("the sweep removed a script that is the repository's own")
	}
}

// The other thing a SIGKILL leaves behind, and the one the script cannot clean
// up for itself: a keychain in the user's search list holding a distribution
// identity, because an EXIT trap does not run when the process group is killed.
//
// This test used to build the state a SIGKILL cannot produce — a keychain whose
// FILE was already gone — and passed while the function did nothing on the only
// path it exists for. The trap is the single thing that removes that file, and
// the trap also runs `security delete-keychain`; so a file that is gone means a
// keychain already out of the list, and the ending this is named for leaves the
// file exactly where it is.
//
// What is set up here is therefore the real one: the process group was killed,
// the trap never ran, and BOTH the file and the search-list entry are still
// there. Every keychain with this name is dead by the time this runs — it runs
// once at startup, and the desktop app runs one runner and drains it before
// starting another — while the user's own keychains are never touched.
func TestSweepReleaseKeychainsRemovesWhatASIGKILLLeftBehind(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("keychains are macOS's")
	}
	dir := t.TempDir()
	deleted := filepath.Join(dir, "deleted.txt")
	// A killed run: the trap never fired, so the file is still on disk and the
	// keychain is still in the search list.
	killed := filepath.Join(dir, releaseKeychainName)
	if err := os.WriteFile(killed, []byte("a signing identity nobody cleaned up"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	login := filepath.Join(dir, "login.keychain-db")
	if err := os.WriteFile(login, []byte("the user's own"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	fake := filepath.Join(dir, "security")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"list-keychains\" ]; then\n" +
		"  printf '    \"%s\"\\n    \"%s\"\\n' " +
		"'" + killed + "' '" + login + "'\n" +
		"  exit 0\n" +
		"fi\n" +
		"if [ \"$1\" = \"delete-keychain\" ]; then echo \"$2\" >> " + deleted + "; fi\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the fake security: %v", err)
	}

	sweepReleaseKeychains(fake)

	raw, err := os.ReadFile(deleted)
	if err != nil {
		t.Fatal("the sweep deleted nothing; after a SIGKILL a distribution identity stays in the search list of every codesign this user runs")
	}
	got := strings.TrimSpace(string(raw))
	if got != killed {
		t.Fatalf("the sweep deleted %q, want only %q", got, killed)
	}
}
