//go:build !windows

package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The redaction shared across claude.run, opencode.run and cursor.run: this
// process's own credential-bearing environment, plus a run's own MCP token,
// scrubbed out of everything streamed back to the caller. See redact.go.

// clearCredentialEnv gives a test a known-empty slate for every name
// credentialRedactor looks at, restored automatically by t.Setenv — so a
// developer's own ANTHROPIC_API_KEY sitting in the shell this suite runs in
// can never make a test pass or fail by accident.
func clearCredentialEnv(t *testing.T) {
	t.Helper()
	for _, name := range credentialEnvNames {
		t.Setenv(name, "")
	}
}

func TestCredentialRedactorNilWithNothingToScrub(t *testing.T) {
	clearCredentialEnv(t)
	if r := credentialRedactor(runnerPolicy{}, ""); r != nil {
		t.Fatal("credentialRedactor returned a scrubber with no credential and no mcp token")
	}
}

func TestCredentialRedactorScrubsEnvValuesAndLeavesShortOnesAlone(t *testing.T) {
	clearCredentialEnv(t)
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-0123456789abcdef")
	// Seven characters: one under releaseRedactMinLen. A value that short
	// would match half the words in a transcript, so it is left alone rather
	// than turning the output into noise.
	t.Setenv("GITHUB_TOKEN", "abcdefg")

	redact := credentialRedactor(runnerPolicy{}, "")
	if redact == nil {
		t.Fatal("credentialRedactor returned nil with a credential set")
	}
	line := []byte(`{"event":"output","data":"key=sk-ant-0123456789abcdef token=abcdefg"}`)
	got := string(redact(line))
	if strings.Contains(got, "sk-ant-0123456789abcdef") {
		t.Fatalf("the long credential reached the output: %s", got)
	}
	if !strings.Contains(got, "[redacted ANTHROPIC_API_KEY]") {
		t.Fatalf("output = %s, want the redaction marker", got)
	}
	if !strings.Contains(got, "abcdefg") {
		t.Fatalf("output = %s, want the sub-floor value left untouched", got)
	}
}

func TestCredentialRedactorScrubsTheRunsMCPToken(t *testing.T) {
	clearCredentialEnv(t)
	token := "tt-run-abcdef0123456789-secret"
	redact := credentialRedactor(runnerPolicy{}, token)
	if redact == nil {
		t.Fatal("credentialRedactor returned nil with an mcp token set")
	}
	line := []byte(`{"event":"output","data":"Authorization: Bearer ` + token + `"}`)
	got := string(redact(line))
	if strings.Contains(got, token) {
		t.Fatalf("the mcp token reached the output: %s", got)
	}
	if !strings.Contains(got, "[redacted "+mcpRunTokenLabel+"]") {
		t.Fatalf("output = %s, want the redaction marker", got)
	}
}

// outputData pulls every "output" event's data field out of one of these
// methods' NDJSON response, in order. request() (methods_test.go) reads a
// response whole, which a streaming method's body still is by the time it
// returns — this just parses the lines back out of it.
func outputData(t *testing.T, body string) []string {
	t.Helper()
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(body), "\n") {
		if line == "" {
			continue
		}
		var ev outputEvent
		if err := json.Unmarshal([]byte(line), &ev); err == nil && ev.Event == "output" {
			out = append(out, ev.Data)
		}
	}
	return out
}

func allOutputData(t *testing.T, body string) string {
	t.Helper()
	return strings.Join(outputData(t, body), "\n")
}

// envPrintingCLI writes a POSIX fake CLI that prints one line naming an
// environment variable's value, then exits. It reads from stdin so a caller
// that pipes a prompt to it (claude, opencode) is not left writing into a
// closed pipe, and it works unchanged as a cursor-agent stand-in, which never
// touches stdin at all — a read from /dev/null returns immediately either way.
func envPrintingCLI(t *testing.T, varName string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "agent")
	script := "#!/bin/sh\n" +
		"cat >/dev/null\n" +
		"echo \"secret:$" + varName + "\"\n" +
		"exit 0\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the fake CLI: %v", err)
	}
	return path
}

func TestClaudeRunRedactsACredentialEnvValue(t *testing.T) {
	clearCredentialEnv(t)
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-0123456789abcdef")

	cfg := config{claudeBin: envPrintingCLI(t, "ANTHROPIC_API_KEY"), gitBin: "/usr/bin/git", workspaceDir: emptyWorkspace(t)}
	res := request(t, cfg, newState(), http.MethodPost, "/claude.run", `{"workspace":"repo","prompt":"go"}`)
	if res.status != http.StatusOK {
		t.Fatalf("status = %d body=%s", res.status, res.body)
	}
	out := allOutputData(t, res.body)
	if strings.Contains(out, "sk-ant-0123456789abcdef") {
		t.Fatalf("the credential reached the output: %s", out)
	}
	if !strings.Contains(out, "[redacted ANTHROPIC_API_KEY]") {
		t.Fatalf("output = %s, want the redaction marker", out)
	}
}

func TestOpencodeRunRedactsACredentialEnvValue(t *testing.T) {
	clearCredentialEnv(t)
	t.Setenv("OPENAI_API_KEY", "sk-openai-0123456789abcdef")

	cfg := config{opencodeBin: envPrintingCLI(t, "OPENAI_API_KEY"), gitBin: "/usr/bin/git", workspaceDir: emptyWorkspace(t)}
	res := request(t, cfg, newState(), http.MethodPost, "/opencode.run", `{"workspace":"repo","prompt":"go","model":"openai/gpt-4.1"}`)
	if res.status != http.StatusOK {
		t.Fatalf("status = %d body=%s", res.status, res.body)
	}
	out := allOutputData(t, res.body)
	if strings.Contains(out, "sk-openai-0123456789abcdef") {
		t.Fatalf("the credential reached the output: %s", out)
	}
	if !strings.Contains(out, "[redacted OPENAI_API_KEY]") {
		t.Fatalf("output = %s, want the redaction marker", out)
	}
}

func TestCursorRunRedactsACredentialEnvValue(t *testing.T) {
	clearCredentialEnv(t)
	t.Setenv("CURSOR_API_KEY", "cur-0123456789abcdef-secret")

	cfg := config{cursorAgentBin: envPrintingCLI(t, "CURSOR_API_KEY"), gitBin: "/usr/bin/git", workspaceDir: emptyWorkspace(t)}
	res := request(t, cfg, newState(), http.MethodPost, "/cursor.run", `{"workspace":"repo","prompt":"go"}`)
	if res.status != http.StatusOK {
		t.Fatalf("status = %d body=%s", res.status, res.body)
	}
	out := allOutputData(t, res.body)
	if strings.Contains(out, "cur-0123456789abcdef-secret") {
		t.Fatalf("the credential reached the output: %s", out)
	}
	if !strings.Contains(out, "[redacted CURSOR_API_KEY]") {
		t.Fatalf("output = %s, want the redaction marker", out)
	}
}
