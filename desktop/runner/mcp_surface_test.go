//go:build !windows

package main

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A CLI run with a cloud `mcp` block is handed the executor's local surface:
// the CLI sees the surface's URL and bearer, never the cloud's; the executor
// is asked with the cloud's, and told to close when the run ends.

type surfaceRig struct {
	h   *runHarness
	ex  *executorHarness
	cfg config
}

func surfaceRunHarness(t *testing.T, mode, claudeTail string) surfaceRig {
	t.Helper()
	ex := startFakeExecutor(t, mode, fastExecutorTimings)
	ex.awaitReady(t)
	cfg := ex.cfg
	cfg.claudeBin = mcpEchoingClaude(t, claudeTail)
	cfg.cursorAgentBin = cursorFileEchoingCLI(t)
	cfg.gitBin = "/usr/bin/git"
	if err := os.MkdirAll(filepath.Join(cfg.workspaceDir, "repo"), 0o755); err != nil {
		t.Fatal(err)
	}
	runner := newRunnerServer(cfg, ex.st)
	runner.mcpRoot = t.TempDir()
	srv := httptest.NewServer(runner.handler())
	t.Cleanup(srv.Close)
	t.Cleanup(ex.st.runs.close)
	return surfaceRig{h: &runHarness{srv: srv, client: srv.Client(), st: ex.st}, ex: ex, cfg: cfg}
}

const surfaceRunMCP = `"mcp":{"url":"` + testMCPURL + `","token":"` + testMCPToken + `","server_name":"tasktrooper",` +
	`"index":{"repo_key":"repo-1","branch":"tt/task-1"}}`

func surfaceRunBody(id string) string {
	return `{"id":"` + id + `","workspace":"repo","prompt":"go","timeout_ms":60000,` +
		`"tools":["Read","mcp__tasktrooper__codebase_search","mcp__tasktrooper__list_board_tasks"],` +
		`"env":{"GOTOOLCHAIN":"go1.24.0"},` + surfaceRunMCP + `}`
}

// runToDone starts a call on path and reads it to its done.
func (r surfaceRig) runToDone(t *testing.T, path, body string) (map[string]any, string) {
	t.Helper()
	res, err := r.h.client.Post(r.h.srv.URL+path, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer func() { _ = res.Body.Close() }()
	got := readFrames(t, res.Body)
	if len(got) == 0 || got[len(got)-1]["event"] != "done" {
		t.Fatalf("%s did not end with a done: %v", path, got)
	}
	raw, _ := json.Marshal(got)
	return got[len(got)-1], string(raw)
}

func seenConfig(t *testing.T, cliBin, file string) mcpDocument {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(cliBin), file))
	if err != nil {
		t.Fatalf("the CLI never saw an MCP config: %v", err)
	}
	var doc mcpDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("the CLI's MCP config is not JSON: %v\n%s", err, raw)
	}
	return doc
}

func awaitLines(t *testing.T, ex *executorHarness, name string, within time.Duration) []string {
	t.Helper()
	deadline := time.Now().Add(within)
	for {
		if lines := ex.lines(t, name); len(lines) > 0 {
			return lines
		}
		if time.Now().After(deadline) {
			t.Fatalf("the executor never saw %s", name)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestAClaudeRunIsHandedTheLocalSurfaceInsteadOfTheCloud(t *testing.T) {
	logs := captureLogs(t)
	rig := surfaceRunHarness(t, "ok", "echo leak:"+testMCPToken+":"+fakeSurfaceToken+"\nexit 0\n")

	done, transcript := rig.runToDone(t, "/claude.run", surfaceRunBody("c-surface"))
	if done["ok"] != true {
		t.Fatalf("done = %v", done)
	}

	server := seenConfig(t, rig.cfg.claudeBin, mcpConfigSeenFile).Servers["tasktrooper"]
	if !strings.HasPrefix(server.URL, "http://127.0.0.1:") || server.Headers["Authorization"] != "Bearer "+fakeSurfaceToken {
		t.Fatalf("the CLI was handed %+v, want the local surface and its bearer", server)
	}

	var opened struct {
		RunID      string            `json:"run_id"`
		Workspace  string            `json:"workspace"`
		ToolPolicy map[string]any    `json:"tool_policy"`
		Index      map[string]any    `json:"index"`
		Env        map[string]string `json:"env"`
		TimeoutMS  int64             `json:"timeout_ms"`
		CloudMCP   surfaceCloud      `json:"cloud_mcp"`
	}
	if err := json.Unmarshal([]byte(awaitLines(t, rig.ex, "surface-opens", time.Second)[0]), &opened); err != nil {
		t.Fatal(err)
	}
	if opened.RunID != "c-surface" || opened.Workspace != "repo" || opened.CloudMCP.Token != testMCPToken || opened.CloudMCP.URL != testMCPURL {
		t.Fatalf("mcp.open = %+v, want the run's id, workspace and the cloud's endpoint", opened)
	}
	if allow, _ := json.Marshal(opened.ToolPolicy["allow_tools"]); string(allow) != `["codebase_search","list_board_tasks"]` {
		t.Fatalf("tool_policy = %v, want the MCP half of the run's tools without the server prefix", opened.ToolPolicy)
	}
	if opened.Index["repo_key"] != "repo-1" || opened.Env["GOTOOLCHAIN"] != "go1.24.0" {
		t.Fatalf("mcp.open = %+v, want the run's index and env passed on", opened)
	}
	if opened.TimeoutMS != (60*time.Second + drainBudget).Milliseconds() {
		t.Fatalf("timeout_ms = %d, want the run's own plus the drain budget", opened.TimeoutMS)
	}
	if closes := awaitLines(t, rig.ex, "surface-closes", 5*time.Second); !strings.Contains(closes[0], `"c-surface"`) {
		t.Fatalf("mcp.close = %v, want the run's id", closes)
	}

	for _, where := range map[string]string{"the frames": transcript, "the log": logs.String()} {
		if strings.Contains(where, testMCPToken) || strings.Contains(where, fakeSurfaceToken) {
			t.Fatalf("a token reached %s:\n%s", where, where)
		}
	}
	if !strings.Contains(transcript, "[redacted MCP_TOKEN]") || !strings.Contains(transcript, "[redacted MCP_LOCAL_TOKEN]") {
		t.Fatalf("the transcript does not show both tokens scrubbed:\n%s", transcript)
	}
}

func TestACursorRunIsHandedTheLocalSurfaceToo(t *testing.T) {
	rig := surfaceRunHarness(t, "ok", "exit 0\n")
	done, _ := rig.runToDone(t, "/cursor.run", `{"id":"c-cursor","workspace":"repo","prompt":"go",`+surfaceRunMCP+`}`)
	if done["ok"] != true {
		t.Fatalf("done = %v", done)
	}
	server := seenConfig(t, rig.cfg.cursorAgentBin, cursorMCPSeenFile).Servers["tasktrooper"]
	if server.Headers["Authorization"] != "Bearer "+fakeSurfaceToken {
		t.Fatalf("cursor-agent was handed %+v, want the local surface", server)
	}
}

// An executor that predates mcp.open, or one that cannot open a surface: the
// run goes ahead on the cloud's URL, as it always did, and the log does not
// carry the token the executor's refusal quoted.
func TestTheRunFallsBackToTheCloudWhenTheExecutorCannotServeIt(t *testing.T) {
	for _, mode := range []string{"nomcp", "mcpfail"} {
		t.Run(mode, func(t *testing.T) {
			logs := captureLogs(t)
			rig := surfaceRunHarness(t, mode, "exit 0\n")
			done, _ := rig.runToDone(t, "/claude.run", surfaceRunBody("c-"+mode))
			if done["ok"] != true {
				t.Fatalf("done = %v", done)
			}
			server := seenConfig(t, rig.cfg.claudeBin, mcpConfigSeenFile).Servers["tasktrooper"]
			if server.URL != testMCPURL || server.Headers["Authorization"] != "Bearer "+testMCPToken {
				t.Fatalf("the CLI was handed %+v, want the cloud's endpoint", server)
			}
			if lines := rig.ex.lines(t, "surface-closes"); len(lines) != 0 {
				t.Fatalf("a surface that never opened was closed: %v", lines)
			}
			if strings.Contains(logs.String(), testMCPToken) {
				t.Fatalf("the cloud token reached the log:\n%s", logs.String())
			}
		})
	}
}

func TestTheRunUsesTheCloudWhenThereIsNoExecutor(t *testing.T) {
	claudeBin := mcpEchoingClaude(t, "exit 0\n")
	h := newMCPHarness(t, claudeBin, emptyWorkspace(t), t.TempDir())
	res := h.start(t, surfaceRunBody("c-noexec"))
	if done, _ := drainToDone(t, frames(res), 30*time.Second); done["ok"] != true {
		t.Fatalf("done = %v", done)
	}
	_ = res.Body.Close()
	if server := seenConfig(t, claudeBin, mcpConfigSeenFile).Servers["tasktrooper"]; server.URL != testMCPURL {
		t.Fatalf("the CLI was handed %+v, want the cloud's endpoint", server)
	}
}

func TestPreflightAdvertisesTheExecutorsLocalTools(t *testing.T) {
	rig := surfaceRunHarness(t, "ok", "exit 0\n")
	rig.h.st.setPreflight(json.RawMessage(`{"ready":true,"items":[]}`))
	res := request(t, rig.cfg, rig.h.st, "GET", "/preflight.report", "")
	var report struct {
		LocalTools []string `json:"local_tools"`
	}
	if err := json.Unmarshal([]byte(res.body), &report); err != nil || strings.Join(report.LocalTools, ",") != strings.Join(fakeLocalTools, ",") {
		t.Fatalf("report = %s (%v), want the executor's local tools", res.body, err)
	}

	none := surfaceRunHarness(t, "nomcp", "exit 0\n")
	none.h.st.setPreflight(json.RawMessage(`{"ready":true,"items":[]}`))
	if res := request(t, none.cfg, none.h.st, "GET", "/preflight.report", ""); strings.Contains(res.body, "local_tools") {
		t.Fatalf("an executor with no surfaces advertised local tools: %s", res.body)
	}
}
