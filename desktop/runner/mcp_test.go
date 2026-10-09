//go:build !windows

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The MCP configuration, tested against real processes and a real socket, for
// the same reason cancellation is: every claim worth making here is about what
// exists on disk and what a child process was actually given, and none of them
// can be checked by reading the code.
//
// What is asserted, and why each one is worth a test rather than a comment:
//
//   - The token is in the FILE and never in argv. argv is world-readable on
//     macOS. The fake CLI echoes its own arguments AND the file it was pointed
//     at, so both halves are observed from where the CLI stands rather than
//     from where the runner does.
//   - `--strict-mcp-config` is passed on every run. Without it a task picks up
//     whatever MCP servers the person who owns the Mac configured for
//     themselves, which is a surprise at best and a way out of the task's scope
//     at worst.
//   - The file is there while the run is and gone after it, for all four ways a
//     run ends. A token file that outlives its run is a credential left on
//     somebody's disk, and three of those four endings are exactly the ones a
//     future change breaks without noticing.
//   - A malformed `mcp` is a 400 BEFORE the 200, not a broken config file
//     handed to the CLI and a failure minutes later.

const testMCPToken = "tt-run-4a91f3c07b-secret" //nolint:gosec // a fixture, not a credential

const testMCPURL = "https://tasktrooper.example/api/mcp"

// mcpConfigSeenFile is where mcpEchoingClaude copies the config file it was
// given, alongside the fake binary. The runner's redactor now scrubs a run's
// own MCP token out of anything streamed back to the caller (see redact.go),
// so a token this fake echoed into "config:" on stdout would arrive redacted —
// correctly, for a real CLI, but not useful for a test that wants the actual
// bytes the CLI read. This sidecar is read directly by the test, off the
// transcript entirely.
const mcpConfigSeenFile = "config-seen.json"

// mcpEchoingClaude writes a fake CLI that reports what it was given: its
// arguments, then the contents of the file `--mcp-config` named, both on
// stdout (redacted the same as a real run's would be) and copied verbatim to
// mcpConfigSeenFile beside the binary (not redacted — read directly by the
// test, never through the runner). `tail` is whatever the fake should do
// afterwards — exit, fail, or refuse to die.
//
// Reading the config file from inside the child is the point. It proves the
// file existed, was readable by the process that needed it, and contained what
// the caller sent — three facts a stat from the test would only guess at.
func mcpEchoingClaude(t *testing.T, tail string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "claude")
	script := "#!/bin/sh\n" +
		"cat >/dev/null\n" + // the prompt, which arrives on stdin
		"echo \"argv:$*\"\n" +
		"prev=\n" +
		"for a in \"$@\"; do\n" +
		"  if [ \"$prev\" = \"--mcp-config\" ]; then echo \"config:$(tr -d '\\n' < \"$a\")\"; cp \"$a\" \"$(dirname \"$0\")/" + mcpConfigSeenFile + "\"; fi\n" +
		"  prev=$a\n" +
		"done\n" +
		tail
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the fake claude: %v", err)
	}
	return path
}

// newMCPHarness is newRunHarness with the MCP root pointed somewhere the test
// can watch. In the shipped binary that root is os.TempDir(); here it is a
// directory this test owns, so "nothing was left behind" is a statement about
// an empty directory rather than a search through everybody's temp files.
func newMCPHarness(t *testing.T, claudeBin, workspace, mcpRoot string) *runHarness {
	t.Helper()
	cfg := config{
		claudeBin:         claudeBin,
		gitBin:            "/usr/bin/git",
		workspaceDir:      workspace,
		embeddingsBaseURL: "http://127.0.0.1:1234/v1",
		embeddingModel:    "nomic-embed-text-v1.5",
	}
	runner := newRunnerServer(cfg, newState())
	runner.mcpRoot = mcpRoot
	srv := httptest.NewServer(runner.handler())
	t.Cleanup(srv.Close)
	return &runHarness{srv: srv, client: srv.Client()}
}

// awaitData reads frames until one arrives whose data line starts with prefix,
// and returns the rest of that line.
func awaitData(t *testing.T, ch <-chan map[string]any, prefix string, within time.Duration) string {
	t.Helper()
	deadline := time.After(within)
	var seen []map[string]any
	for {
		select {
		case f, ok := <-ch:
			if !ok {
				t.Fatalf("the response ended before a %q line arrived; frames: %v", prefix, seen)
			}
			seen = append(seen, f)
			data, _ := f["data"].(string)
			if rest, found := strings.CutPrefix(data, prefix); found {
				return rest
			}
		case <-deadline:
			t.Fatalf("no %q line arrived within %s; frames: %v", prefix, within, seen)
		}
	}
}

// mcpConfigPathFrom pulls the path out of the argv line the fake CLI echoed —
// the child's own view of what it was told to read.
func mcpConfigPathFrom(t *testing.T, argv string) string {
	t.Helper()
	fields := strings.Fields(argv)
	for i, f := range fields {
		if f == "--mcp-config" && i+1 < len(fields) {
			return fields[i+1]
		}
	}
	t.Fatalf("the session was not given --mcp-config; argv was %q", argv)
	return ""
}

func awaitPathGone(t *testing.T, path string, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s still exists %s after the run ended — a bearer token has been left on disk", path, within)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func assertRootIsEmpty(t *testing.T, root string) {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("reading %s: %v", root, err)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("the MCP root still holds %v; every run removes its own directory", names)
	}
}

// The whole shape of it, on the ordinary path: the flags the CLI is given, the
// file it reads, and where the token is and is not.
func TestAnMCPRunPutsTheTokenInTheFileAndTheFlagsInArgv(t *testing.T) {
	logs := captureLogs(t)
	workspace := emptyWorkspace(t)
	mcpRoot := t.TempDir()
	claudeBin := mcpEchoingClaude(t, "exit 0\n")
	h := newMCPHarness(t, claudeBin, workspace, mcpRoot)

	res := h.start(t, `{"id":"c-mcp","workspace":"repo","prompt":"go","mcp":{`+
		`"url":"`+testMCPURL+`","token":"`+testMCPToken+`","server_name":"tasktrooper"}}`)
	defer func() { _ = res.Body.Close() }()
	ch := frames(res)

	argv := awaitData(t, ch, "argv:", 15*time.Second)

	// The rule that costs the most if it is ever quietly broken: argv on macOS
	// is readable by every process on the machine, so a token there is the
	// task's credential in every `ps` for the length of the run.
	if strings.Contains(argv, testMCPToken) {
		t.Fatalf("the token reached argv: %q", argv)
	}
	// And the flag that keeps the user's own MCP servers out of a work task.
	if !strings.Contains(argv, "--strict-mcp-config") {
		t.Fatalf("argv %q has no --strict-mcp-config; this run would also load the servers the user configured for themselves", argv)
	}
	configPath := mcpConfigPathFrom(t, argv)

	// What the CLI actually read, reported by the CLI over the transcript —
	// which is redacted the same as a real run's would be, so the token
	// itself is checked below, off the transcript, against the sidecar the
	// fake CLI copied the file to.
	var doc mcpDocument
	raw := awaitData(t, ch, "config:", 15*time.Second)
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("the session read a config that is not JSON (%q): %v", raw, err)
	}
	server, named := doc.Servers["tasktrooper"]
	if !named {
		t.Fatalf("the config has no server called tasktrooper: %q", raw)
	}
	if server.Type != "http" || server.URL != testMCPURL {
		t.Fatalf("server = %+v, want an http server at %s", server, testMCPURL)
	}
	if got := server.Headers["Authorization"]; got != "Bearer [redacted MCP_TOKEN]" {
		t.Fatalf("Authorization on the transcript = %q, want the per-run token scrubbed out of it", got)
	}

	if done, count := drainToDone(t, ch, 30*time.Second); count != 1 || done["ok"] != true {
		t.Fatalf("done = %v (%d of them), want one success", done, count)
	}

	// Read only after the run has ended: the sidecar `cp` is a command after
	// the `echo` on the same script line, so it is not guaranteed to have run
	// by the time the "config:" line this process just read arrived over the
	// stream — but the process HAS exited by the time drainToDone returns,
	// which it cannot do until every command in the script, cp included, is
	// done.
	seen, err := os.ReadFile(filepath.Join(filepath.Dir(claudeBin), mcpConfigSeenFile))
	if err != nil {
		t.Fatalf("reading the sidecar copy of the config the CLI actually read: %v", err)
	}
	var seenDoc mcpDocument
	if err := json.Unmarshal(seen, &seenDoc); err != nil {
		t.Fatalf("the sidecar config is not JSON: %v", err)
	}
	if got := seenDoc.Servers["tasktrooper"].Headers["Authorization"]; got != "Bearer "+testMCPToken {
		t.Fatalf("Authorization in the file the CLI read = %q, want the per-run token as a bearer credential", got)
	}

	awaitPathGone(t, filepath.Dir(configPath), 5*time.Second)
	assertRootIsEmpty(t, mcpRoot)

	// The token is not in the log either. The runner token has had this test
	// since the tunnel was written; a per-run token is no less a credential.
	if strings.Contains(logs.String(), testMCPToken) {
		t.Fatalf("the MCP token was logged:\n%s", logs.String())
	}
	if !strings.Contains(logs.String(), "tasktrooper.example") {
		t.Fatalf("the log says nothing about the tools this session was given:\n%s", logs.String())
	}
}

// The deletion guarantee, stated once for each way a run can end.
//
// Three of these four are the ones a refactor breaks silently: a cancelled run,
// a failed run and a dropped tunnel all leave through a different path than the
// happy one, and a file that is only removed on the happy path is a token left
// on somebody's disk exactly when something has already gone wrong.
func TestTheMCPConfigIsRemovedHoweverTheRunEnds(t *testing.T) {
	body := `{"id":"c-end","workspace":"repo","prompt":"go","mcp":{` +
		`"url":"` + testMCPURL + `","token":"` + testMCPToken + `","server_name":"tasktrooper"}}`

	// The two that end by themselves pause first, so "the file is there while
	// the run is" is an assertion and not a race with the child's own exit.
	t.Run("it finishes", func(t *testing.T) {
		mcpRoot := t.TempDir()
		h := newMCPHarness(t, mcpEchoingClaude(t, "sleep 2\nexit 0\n"), emptyWorkspace(t), mcpRoot)
		res := h.start(t, body)
		defer func() { _ = res.Body.Close() }()
		ch := frames(res)

		path := mcpConfigPathFrom(t, awaitData(t, ch, "argv:", 15*time.Second))
		assertConfigIsPrivate(t, path)

		if _, count := drainToDone(t, ch, 30*time.Second); count != 1 {
			t.Fatalf("the response carried %d done frames, want one", count)
		}
		awaitPathGone(t, filepath.Dir(path), 5*time.Second)
		assertRootIsEmpty(t, mcpRoot)
	})

	t.Run("it fails", func(t *testing.T) {
		mcpRoot := t.TempDir()
		h := newMCPHarness(t, mcpEchoingClaude(t, "sleep 2\nexit 3\n"), emptyWorkspace(t), mcpRoot)
		res := h.start(t, body)
		defer func() { _ = res.Body.Close() }()
		ch := frames(res)

		path := mcpConfigPathFrom(t, awaitData(t, ch, "argv:", 15*time.Second))
		assertConfigIsPrivate(t, path)

		done, count := drainToDone(t, ch, 30*time.Second)
		if count != 1 {
			t.Fatalf("the response carried %d done frames, want one", count)
		}
		result, _ := done["result"].(map[string]any)
		if result == nil || result["exit_code"] != float64(3) {
			t.Fatalf("result = %v, want the session's non-zero exit", done["result"])
		}
		awaitPathGone(t, filepath.Dir(path), 5*time.Second)
		assertRootIsEmpty(t, mcpRoot)
	})

	// The session that will not die: it ignores SIGTERM and spawns a child, so
	// the run only ends after the escalation to SIGKILL on the process group.
	// The config has to be gone after THAT too.
	t.Run("it is cancelled", func(t *testing.T) {
		mcpRoot := t.TempDir()
		claudeBin := mcpEchoingClaude(t, "trap '' TERM\nsleep 300 &\necho grandchild:$!\nwait\n")
		h := newMCPHarness(t, claudeBin, emptyWorkspace(t), mcpRoot)
		res := h.start(t, body)
		defer func() { _ = res.Body.Close() }()
		ch := frames(res)

		path := mcpConfigPathFrom(t, awaitData(t, ch, "argv:", 15*time.Second))
		assertConfigIsPrivate(t, path)
		child, _ := awaitGrandchild(t, ch, 15*time.Second)

		h.cancel(t, "c-end")

		done, count := drainToDone(t, ch, claudeGrace+claudeReapTimeout+10*time.Second)
		if count != 1 {
			t.Fatalf("the response carried %d done frames, want one", count)
		}
		if e, _ := done["error"].(map[string]any); e == nil || e["code"] != codeCancelled {
			t.Fatalf("done = %v, want a cancelled call", done)
		}
		awaitGone(t, child, 5*time.Second)
		awaitPathGone(t, filepath.Dir(path), 5*time.Second)
		assertRootIsEmpty(t, mcpRoot)
	})

	// The tunnel dropping, which from this side is a caller that stopped
	// reading. There is no `done` to wait for here — nobody is listening for
	// one — so the only evidence that the run ended is the process tree dying
	// and the token file disappearing.
	t.Run("the tunnel drops", func(t *testing.T) {
		mcpRoot := t.TempDir()
		claudeBin := mcpEchoingClaude(t, "trap '' TERM\nsleep 300 &\necho grandchild:$!\nwait\n")
		h := newMCPHarness(t, claudeBin, emptyWorkspace(t), mcpRoot)
		res := h.start(t, body)
		ch := frames(res)

		path := mcpConfigPathFrom(t, awaitData(t, ch, "argv:", 15*time.Second))
		assertConfigIsPrivate(t, path)
		child, _ := awaitGrandchild(t, ch, 15*time.Second)

		if err := res.Body.Close(); err != nil {
			t.Fatalf("closing the response body: %v", err)
		}

		awaitGone(t, child, claudeGrace+claudeReapTimeout+10*time.Second)
		awaitPathGone(t, filepath.Dir(path), 10*time.Second)
		assertRootIsEmpty(t, mcpRoot)
	})
}

// assertConfigIsPrivate checks the file is there while the run is, and that
// nobody but this user can read it. Both halves matter: a token file the group
// can read is a token every process on a shared Mac can read.
func assertConfigIsPrivate(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("the MCP config is not on disk while the run is using it: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("the MCP config is mode %o, want 600 — it holds a bearer token", perm)
	}
	dir, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("stat of the config's directory: %v", err)
	}
	if perm := dir.Mode().Perm(); perm != 0o700 {
		t.Fatalf("the MCP config directory is mode %o, want 700", perm)
	}
	if !strings.HasPrefix(filepath.Base(filepath.Dir(path)), mcpDirPrefix) {
		t.Fatalf("%s is not named so the startup sweep can find it", filepath.Dir(path))
	}
}

// A run with no `mcp` is a valid run with no TaskTrooper tools — and it still
// gets --strict-mcp-config, because "no tools from us" must not quietly mean
// "whatever this person configured for themselves". Somebody's personal MCP
// servers inside a work task is a surprise and a scope nobody granted.
func TestARunWithNoMCPGetsNoServersAndStillRefusesTheUsersOwn(t *testing.T) {
	mcpRoot := t.TempDir()
	h := newMCPHarness(t, mcpEchoingClaude(t, "exit 0\n"), emptyWorkspace(t), mcpRoot)

	res := h.start(t, `{"id":"c-nomcp","workspace":"repo","prompt":"go"}`)
	defer func() { _ = res.Body.Close() }()
	ch := frames(res)

	argv := awaitData(t, ch, "argv:", 15*time.Second)
	if !strings.Contains(argv, "--strict-mcp-config") {
		t.Fatalf("argv %q has no --strict-mcp-config; a run with no mcp would load the user's own servers", argv)
	}
	path := mcpConfigPathFrom(t, argv)

	raw := awaitData(t, ch, "config:", 15*time.Second)
	var doc mcpDocument
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("the session read a config that is not JSON (%q): %v", raw, err)
	}
	if len(doc.Servers) != 0 {
		t.Fatalf("a run with no mcp was given %v; absent means no servers, not a default", doc.Servers)
	}

	if _, count := drainToDone(t, ch, 30*time.Second); count != 1 {
		t.Fatalf("the response carried %d done frames, want one", count)
	}
	awaitPathGone(t, filepath.Dir(path), 5*time.Second)
	assertRootIsEmpty(t, mcpRoot)
}

// Everything malformed is refused with a status, in front of the 200 — and
// without leaving a file behind.
//
// It has to be a status rather than a `done` frame: the caller would otherwise
// have to read a stream to discover that the object it sent was never usable,
// and the CLI would have been handed a config file built out of it in the
// meantime.
func TestAMalformedMCPObjectIsRefusedBeforeTheStream(t *testing.T) {
	workspace := emptyWorkspace(t)
	valid := `"url":"` + testMCPURL + `","token":"` + testMCPToken + `","server_name":"tasktrooper"`

	cases := []struct {
		name string
		mcp  string
		want string
	}{
		{"no url", `"token":"t","server_name":"tasktrooper"`, "mcp.url is required"},
		{"http rather than https", `"url":"http://tasktrooper.example/api/mcp","token":"t","server_name":"tasktrooper"`, "absolute https"},
		{"a relative url", `"url":"/api/mcp","token":"t","server_name":"tasktrooper"`, "absolute https"},
		{"a url with credentials in it", `"url":"https://u:p@tasktrooper.example/api/mcp","token":"t","server_name":"tasktrooper"`, "userinfo"},
		{"no token", `"url":"` + testMCPURL + `","server_name":"tasktrooper"`, "mcp.token is required"},
		{"a token with a newline in it", `"url":"` + testMCPURL + `","token":"a\nX-Evil: 1","server_name":"tasktrooper"`, "printable ASCII"},
		{"a token with a space in it", `"url":"` + testMCPURL + `","token":"a b","server_name":"tasktrooper"`, "printable ASCII"},
		{"no server name", `"url":"` + testMCPURL + `","token":"t"`, "mcp.server_name is required"},
		{"a server name with a path in it", `"url":"` + testMCPURL + `","token":"t","server_name":"../../etc"`, "plain identifier"},
		{"a server name that is a flag", `"url":"` + testMCPURL + `","token":"t","server_name":"--mcp-config"`, "plain identifier"},
		{"a server name with a dot in it", `"url":"` + testMCPURL + `","token":"t","server_name":"task.trooper"`, "plain identifier"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mcpRoot := t.TempDir()
			h := newMCPHarness(t, mcpEchoingClaude(t, "exit 0\n"), workspace, mcpRoot)
			res, err := h.client.Post(h.srv.URL+"/claude.run", "application/json",
				strings.NewReader(`{"workspace":"repo","prompt":"go","mcp":{`+tc.mcp+`}}`))
			if err != nil {
				t.Fatalf("POST /claude.run: %v", err)
			}
			defer func() { _ = res.Body.Close() }()
			if res.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 — a bad mcp object is refused before the stream starts", res.StatusCode)
			}
			var body errorBody
			if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
				t.Fatalf("decoding the error: %v", err)
			}
			if body.Error.Code != codeBadRequest {
				t.Fatalf("code = %q, want %q", body.Error.Code, codeBadRequest)
			}
			if !strings.Contains(body.Error.Message, tc.want) {
				t.Fatalf("message = %q, want it to mention %q", body.Error.Message, tc.want)
			}
			// Nothing was written for a call that never ran.
			assertRootIsEmpty(t, mcpRoot)
		})
	}

	// And the same request with a good object is not refused, so the table
	// above is testing the object rather than the shape of the request.
	mcpRoot := t.TempDir()
	h := newMCPHarness(t, mcpEchoingClaude(t, "exit 0\n"), workspace, mcpRoot)
	res := h.start(t, `{"workspace":"repo","prompt":"go","mcp":{`+valid+`}}`)
	defer func() { _ = res.Body.Close() }()
	if _, count := drainToDone(t, frames(res), 30*time.Second); count != 1 {
		t.Fatalf("a valid mcp object did not produce one done frame")
	}
	assertRootIsEmpty(t, mcpRoot)
}

// The one ending that cannot remove its own file: this process being killed
// outright. What that leaves behind is a bearer token in a file, and the next
// start is the only thing left to clear it.
func TestStaleMCPConfigsAreSweptAtStartup(t *testing.T) {
	root := t.TempDir()

	stale := filepath.Join(root, mcpDirPrefix+"deadbeefdeadbeef")
	if err := os.Mkdir(stale, 0o700); err != nil {
		t.Fatalf("creating a stale config directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stale, mcpFileName), []byte(`{"mcpServers":{}}`), 0o600); err != nil {
		t.Fatalf("writing the stale config: %v", err)
	}

	// Two things the sweep must not touch: somebody else's directory, and a
	// file that merely shares the prefix. This runs inside the user's temp
	// directory in the shipped binary, which is shared with everything else the
	// user runs.
	other := filepath.Join(root, "someone-elses-work")
	if err := os.Mkdir(other, 0o700); err != nil {
		t.Fatalf("creating the unrelated directory: %v", err)
	}
	notADir := filepath.Join(root, mcpDirPrefix+"not-a-directory")
	if err := os.WriteFile(notADir, []byte("x"), 0o600); err != nil {
		t.Fatalf("writing the unrelated file: %v", err)
	}

	sweepMCPConfigs(root)

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("%s survived the sweep — a token from a killed run is still on disk", stale)
	}
	for _, keep := range []string{other, notADir} {
		if _, err := os.Stat(keep); err != nil {
			t.Fatalf("the sweep removed %s, which is not ours: %v", keep, err)
		}
	}
}

// checkMCP is the only way an mcpConfig can be built, which is what makes
// "validated before the 200" a property of the type rather than of the order
// two functions happen to be called in.
func TestCheckMCPAcceptsTheContractAndNothingElse(t *testing.T) {
	// Absent is valid: a run that prepares a checkout or answers a question
	// needs no tools, and inventing a default here would be inventing a token.
	got, err := checkMCP(nil)
	if err != nil || got != nil {
		t.Fatalf("checkMCP(nil) = %v, %v; absent mcp is a valid run with no tools", got, err)
	}

	good, err := checkMCP(&mcpParams{URL: testMCPURL, Token: testMCPToken, ServerName: "tasktrooper"})
	if err != nil {
		t.Fatalf("checkMCP on the pinned contract: %s", err.Message)
	}
	if good.url != testMCPURL || good.token != testMCPToken || good.serverName != "tasktrooper" {
		t.Fatalf("checkMCP = %+v, want the object it was given", good)
	}
}

// http is refused everywhere except loopback: a bearer token over plain http
// never leaves the machine there, but does everywhere else.
func TestCheckMCPAllowsHTTPOnLoopbackOnly(t *testing.T) {
	for _, host := range []string{"127.0.0.1", "localhost", "[::1]"} {
		got, err := checkMCP(&mcpParams{URL: "http://" + host + ":9999/mcp", Token: testMCPToken, ServerName: "tasktrooper"})
		if err != nil {
			t.Fatalf("checkMCP on http://%s: %s", host, err.Message)
		}
		if got.url != "http://"+host+":9999/mcp" {
			t.Fatalf("checkMCP = %+v, want the loopback url preserved", got)
		}
	}

	_, err := checkMCP(&mcpParams{URL: "http://tasktrooper.example/mcp", Token: testMCPToken, ServerName: "tasktrooper"})
	if err == nil || !strings.Contains(err.Message, "absolute https") {
		t.Fatalf("checkMCP on a non-loopback http url = %v, want it refused for not being https", err)
	}
}
