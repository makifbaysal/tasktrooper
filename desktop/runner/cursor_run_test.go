//go:build !windows

package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// cursor.run — argv shape, the workspace MCP file's lifecycle, not_ready
// without the binary, and cancellation. The shape of these tests mirrors
// session_test.go and mcp_test.go's for claude.run, because the properties
// under test are the same ones: what reaches argv, what a caller can and
// cannot see on disk, and that a cancelled call actually stops the process
// group.

func TestCursorRunNotReadyWithoutTheBinary(t *testing.T) {
	cfg := config{gitBin: "/usr/bin/git", workspaceDir: emptyWorkspace(t)}
	res := request(t, cfg, newState(), http.MethodPost, "/cursor.run", `{"workspace":"repo","prompt":"go"}`)
	if res.status != http.StatusConflict {
		t.Fatalf("status = %d, want 409", res.status)
	}
	if res.code() != codeNotReady {
		t.Fatalf("code = %q, want %s", res.code(), codeNotReady)
	}
}

// cursorReportingCLI writes a fake cursor-agent that reports its own argv,
// exactly like reportingClaude (policy_test.go) does for claude.
func cursorReportingCLI(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "cursor-agent")
	script := "#!/bin/sh\n" +
		"cat >/dev/null\n" +
		"echo \"argv:$*\"\n" +
		"exit 0\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the fake cursor-agent: %v", err)
	}
	return path
}

// The argv contract cursor-agent's own --help documents: -p is the print
// flag (boolean, no value), --force and --output-format stream-json follow
// unconditionally, --model and --resume are added only when asked for, and
// the prompt is the LAST thing on the line, preceded by a literal `--` — the
// one guard this file adds beyond mirroring the local executor, because a
// prompt beginning with `-` is otherwise parsed as an option by cursor-agent
// itself (verified directly against the installed binary; see cursor_run.go's
// header).
func TestCursorRunArgvShape(t *testing.T) {
	bin := cursorReportingCLI(t)
	cfg := config{cursorAgentBin: bin, gitBin: "/usr/bin/git", workspaceDir: emptyWorkspace(t)}

	// No spaces in the prompt: the fake CLI reports argv by joining $* with
	// spaces, so a prompt containing one would be indistinguishable from two
	// separate arguments once echoed back. What matters here — that a
	// dash-leading prompt survives as ONE argument, last, after `--` — is
	// fully exercised without needing one.
	const prompt = "--dangerous-looking-prompt"
	body := `{"workspace":"repo","prompt":"` + prompt + `","model":"sonnet-4-thinking","resume":"chat-123"}`
	res := request(t, cfg, newState(), http.MethodPost, "/cursor.run", body)
	if res.status != http.StatusOK {
		t.Fatalf("status = %d body=%s", res.status, res.body)
	}

	var argv string
	for _, d := range outputData(t, res.body) {
		if rest, ok := strings.CutPrefix(d, "argv:"); ok {
			argv = rest
			break
		}
	}
	if argv == "" {
		t.Fatalf("no argv line was reported; body=%s", res.body)
	}
	fields := strings.Fields(argv)

	if len(fields) < 2 || fields[0] != "-p" {
		t.Fatalf("argv = %q, want it to start with -p", argv)
	}
	want := [][2]string{{"--force", ""}, {"--output-format", "stream-json"}, {"--model", "sonnet-4-thinking"}, {"--resume", "chat-123"}}
	for _, w := range want {
		i := indexOf(fields, w[0])
		if i < 0 {
			t.Fatalf("argv = %q, missing %s", argv, w[0])
		}
		if w[1] != "" && (i+1 >= len(fields) || fields[i+1] != w[1]) {
			t.Fatalf("argv = %q, want %s followed by %s", argv, w[0], w[1])
		}
	}
	if len(fields) < 2 || fields[len(fields)-2] != "--" || fields[len(fields)-1] != prompt {
		t.Fatalf("argv = %q, want it to end with -- %s", argv, prompt)
	}
}

func indexOf(fields []string, want string) int {
	for i, f := range fields {
		if f == want {
			return i
		}
	}
	return -1
}

// cursorFileEchoingCLI writes a fake cursor-agent that, like
// mcpEchoingClaude, copies whatever MCP config it can see (relative to its
// own working directory — cmd.Dir is the workspace, exactly as a real
// cursor-agent's would be) to a sidecar file beside itself, off the
// transcript entirely, because the runner's redactor now scrubs the run's own
// MCP token out of anything streamed back — see redact.go and mcp_test.go's
// mcpConfigSeenFile for the same pattern applied to claude.run.
const cursorMCPSeenFile = "mcp-seen.json"

func cursorFileEchoingCLI(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "cursor-agent")
	script := "#!/bin/sh\n" +
		"cat >/dev/null\n" +
		"echo \"argv:$*\"\n" +
		"cp \"" + cursorMCPRelPath + "\" \"$(dirname \"$0\")/" + cursorMCPSeenFile + "\" 2>/dev/null\n" +
		"exit 0\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the fake cursor-agent: %v", err)
	}
	return path
}

// No `--mcp-config` flag exists for cursor-agent (cursor_run.go's header): the
// server has to land in the workspace's own .cursor/mcp.json, and it has to
// be gone from there again once the run ends, because nothing else on this
// Mac will ever clean it up.
func TestCursorRunWritesAndRemovesTheWorkspaceMCPConfigWhenNoneExisted(t *testing.T) {
	bin := cursorFileEchoingCLI(t)
	workspace := emptyWorkspace(t)
	repoDir := filepath.Join(workspace, "repo")
	cfg := config{cursorAgentBin: bin, gitBin: "/usr/bin/git", workspaceDir: workspace}

	body := `{"workspace":"repo","prompt":"go","mcp":{"url":"` + testMCPURL + `","token":"` + testMCPToken + `","server_name":"tasktrooper"}}`
	res := request(t, cfg, newState(), http.MethodPost, "/cursor.run", body)
	if res.status != http.StatusOK {
		t.Fatalf("status = %d body=%s", res.status, res.body)
	}

	seen, err := os.ReadFile(filepath.Join(filepath.Dir(bin), cursorMCPSeenFile))
	if err != nil {
		t.Fatalf("reading the sidecar copy cursor-agent made of the config it read: %v", err)
	}
	var doc mcpDocument
	if err := json.Unmarshal(seen, &doc); err != nil {
		t.Fatalf("the sidecar config is not JSON: %v", err)
	}
	server, named := doc.Servers["tasktrooper"]
	if !named || server.URL != testMCPURL || server.Headers["Authorization"] != "Bearer "+testMCPToken {
		t.Fatalf("cursor-agent did not read the expected server: %+v", doc.Servers)
	}

	awaitPathGone(t, filepath.Join(repoDir, cursorMCPRelPath), 5*time.Second)
}

// A developer's OWN .cursor/mcp.json — servers they configured for
// themselves in this checkout — is merged with, and restored byte for byte
// after, the run's server. Losing it would be this runner quietly replacing
// somebody's own MCP configuration; leaving the merged copy behind would be a
// bearer token sitting in a file `git status` can see.
func TestCursorRunMergesAndRestoresAPreexistingWorkspaceMCPConfig(t *testing.T) {
	bin := cursorFileEchoingCLI(t)
	workspace := emptyWorkspace(t)
	repoDir := filepath.Join(workspace, "repo")
	mcpPath := filepath.Join(repoDir, cursorMCPRelPath)
	if err := os.MkdirAll(filepath.Dir(mcpPath), 0o755); err != nil {
		t.Fatalf("creating .cursor: %v", err)
	}
	const original = `{"mcpServers":{"someone-elses":{"url":"https://example.com/mcp"}}}`
	if err := os.WriteFile(mcpPath, []byte(original), 0o644); err != nil {
		t.Fatalf("seeding the developer's own config: %v", err)
	}

	cfg := config{cursorAgentBin: bin, gitBin: "/usr/bin/git", workspaceDir: workspace}
	body := `{"workspace":"repo","prompt":"go","mcp":{"url":"` + testMCPURL + `","token":"` + testMCPToken + `","server_name":"tasktrooper"}}`
	res := request(t, cfg, newState(), http.MethodPost, "/cursor.run", body)
	if res.status != http.StatusOK {
		t.Fatalf("status = %d body=%s", res.status, res.body)
	}

	seen, err := os.ReadFile(filepath.Join(filepath.Dir(bin), cursorMCPSeenFile))
	if err != nil {
		t.Fatalf("reading the sidecar: %v", err)
	}
	var doc mcpDocument
	if err := json.Unmarshal(seen, &doc); err != nil {
		t.Fatalf("the sidecar config is not JSON: %v", err)
	}
	if _, named := doc.Servers["tasktrooper"]; !named {
		t.Fatalf("the merged config cursor-agent read lost the run's own server: %+v", doc.Servers)
	}
	if _, kept := doc.Servers["someone-elses"]; !kept {
		t.Fatalf("the merged config cursor-agent read dropped the developer's own server: %+v", doc.Servers)
	}

	awaitFileContent(t, mcpPath, original, 5*time.Second)
}

// awaitFileContent polls for a file to settle at exactly want, the same way
// awaitPathGone (mcp_test.go) polls for one to disappear — the restore is a
// deferred call racing this test's read of the response, not something
// guaranteed to have happened by the time request() returns.
func awaitFileContent(t *testing.T, path, want string, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	var last string
	for time.Now().Before(deadline) {
		b, err := os.ReadFile(path)
		if err == nil {
			last = string(b)
			if last == want {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s did not settle at the original content within %s; last seen: %s", path, within, last)
}

// Cancellation, against a cursor-agent stand-in that behaves like the worst
// real one — ignores SIGTERM, spawns a child of its own — the same fixture
// session_test.go uses for claude.run (stubbornClaude), because nothing about
// this property is claude-specific: it is what running any long-lived CLI
// session over this tunnel safely requires. cursor-agent never reads stdin
// (see cursor_run.go's header), which this fixture already does not rely on.
func TestCursorRunCancelKillsTheProcessGroup(t *testing.T) {
	workspace := emptyWorkspace(t)
	cfg := config{cursorAgentBin: stubbornClaude(t), gitBin: "/usr/bin/git", workspaceDir: workspace}
	srv := httptest.NewServer(newRunnerServer(cfg, newState()).handler())
	t.Cleanup(srv.Close)
	client := srv.Client()

	res, err := client.Post(srv.URL+"/cursor.run", "application/json", strings.NewReader(`{"id":"cur-1","workspace":"repo","prompt":"go"}`))
	if err != nil {
		t.Fatalf("POST /cursor.run: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", res.StatusCode)
	}
	ch := frames(res)

	child, _ := awaitGrandchild(t, ch, 15*time.Second)
	if !alive(child) {
		t.Fatalf("the fake session's child (%d) was not running before the cancel", child)
	}

	cres, err := client.Post(srv.URL+"/cancel", "application/json", strings.NewReader(`{"id":"cur-1"}`))
	if err != nil {
		t.Fatalf("POST /cancel: %v", err)
	}
	defer func() { _ = cres.Body.Close() }()
	var parsed cancelResponse
	if err := json.NewDecoder(cres.Body).Decode(&parsed); err != nil {
		t.Fatalf("decoding the cancel response: %v", err)
	}
	if !parsed.Cancelled {
		t.Fatal("POST /cancel did not find the running call")
	}

	done, count := drainToDone(t, ch, claudeGrace+claudeReapTimeout+10*time.Second)
	if count != 1 {
		t.Fatalf("the response carried %d done frames, want one", count)
	}
	if e, _ := done["error"].(map[string]any); e == nil || e["code"] != codeCancelled {
		t.Fatalf("done = %v, want a cancelled call", done)
	}
	awaitGone(t, child, 5*time.Second)
}

func TestCursorMCPIsExcludedFromGit(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git", "info"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".git", "info", "exclude"), []byte("# existing"), 0o644); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		restore, rpcErr := writeCursorMCP(dir, &mcpConfig{url: "https://example.test/api/mcp?t=x", token: "tok-abcdefgh", serverName: "tasktrooper"})
		if rpcErr != nil {
			t.Fatalf("writeCursorMCP: %v", rpcErr)
		}
		restore()
	}
	got, err := os.ReadFile(filepath.Join(dir, ".git", "info", "exclude"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "# existing\n/.cursor/mcp.json\n"; string(got) != want {
		t.Fatalf("exclude = %q, want %q", got, want)
	}
}

func TestCursorMCPLeavesNonGitWorkspaceAlone(t *testing.T) {
	dir := t.TempDir()
	restore, rpcErr := writeCursorMCP(dir, &mcpConfig{url: "https://example.test/api/mcp?t=x", token: "tok-abcdefgh", serverName: "tasktrooper"})
	if rpcErr != nil {
		t.Fatalf("writeCursorMCP: %v", rpcErr)
	}
	restore()
	if _, err := os.Stat(filepath.Join(dir, ".git")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf(".git was created in a non-git workspace: %v", err)
	}
}
