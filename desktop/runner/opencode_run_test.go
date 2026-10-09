//go:build !windows

package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// OpenCode never runs on a Claude or Gemini subscription — the rule in
// CLAUDE.md and the root repository's own subscription rules, enforced here on
// the model name rather than trusted to whatever credential happens to be
// configured, because OpenCode reads ~/.claude's and Google's own stored
// credentials as readily as it reads an API key.

// exitingCLI writes a POSIX fake CLI that reads its stdin and exits 0 — enough
// to prove a request got PAST validation and into a real spawn, without
// asserting anything about its argv.
func exitingCLI(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "opencode")
	if err := os.WriteFile(path, []byte("#!/bin/sh\ncat >/dev/null\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("writing the fake CLI: %v", err)
	}
	return path
}

func TestOpencodeRunRequiresAModel(t *testing.T) {
	cfg := config{opencodeBin: exitingCLI(t), gitBin: "/usr/bin/git", workspaceDir: emptyWorkspace(t)}
	res := request(t, cfg, newState(), http.MethodPost, "/opencode.run", `{"workspace":"repo","prompt":"go"}`)
	if res.status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", res.status)
	}
	if res.code() != codeBadRequest {
		t.Fatalf("code = %q, want %s", res.code(), codeBadRequest)
	}
	if !strings.Contains(res.body, "model is required") {
		t.Fatalf("message = %q, want it to say a model is required", res.body)
	}
}

func TestOpencodeRunRequiresProviderSlashModelForm(t *testing.T) {
	cfg := config{opencodeBin: exitingCLI(t), gitBin: "/usr/bin/git", workspaceDir: emptyWorkspace(t)}
	res := request(t, cfg, newState(), http.MethodPost, "/opencode.run", `{"workspace":"repo","prompt":"go","model":"big-pickle"}`)
	if res.status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", res.status)
	}
	if !strings.Contains(res.body, "provider/model") {
		t.Fatalf("message = %q, want it to explain the provider/model form", res.body)
	}
}

func TestOpencodeRunRefusesAnthropicAndGoogleProviders(t *testing.T) {
	cfg := config{opencodeBin: exitingCLI(t), gitBin: "/usr/bin/git", workspaceDir: emptyWorkspace(t)}

	cases := []struct {
		name  string
		model string
		want  string
	}{
		{"anthropic", "anthropic/claude-sonnet-4.5", "Claude Code"},
		{"anthropic, mixed case", "Anthropic/claude-sonnet-4.5", "Claude Code"},
		{"google", "google/gemini-2.5-pro", "Gemini subscription"},
		{"google, upper case", "GOOGLE/gemini-2.5-pro", "Gemini subscription"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := request(t, cfg, newState(), http.MethodPost, "/opencode.run",
				`{"workspace":"repo","prompt":"go","model":"`+tc.model+`"}`)
			if res.status != http.StatusBadRequest {
				t.Fatalf("status = %d body=%s, want 400 before the stream", res.status, res.body)
			}
			if res.code() != codeBadRequest {
				t.Fatalf("code = %q, want %s", res.code(), codeBadRequest)
			}
			if !strings.Contains(res.body, tc.want) {
				t.Fatalf("message = %q, want it to mention %q", res.body, tc.want)
			}
		})
	}
}

// The allowed side of the same rule: a provider that is neither anthropic nor
// google passes validation and reaches a real spawn.
func TestOpencodeRunAllowsNonAnthropicNonGoogleProviders(t *testing.T) {
	cfg := config{opencodeBin: exitingCLI(t), gitBin: "/usr/bin/git", workspaceDir: emptyWorkspace(t)}

	for _, model := range []string{
		"openai/gpt-4.1",
		"opencode/big-pickle",
		"github-copilot/claude-sonnet-4.5",
		"openrouter/anthropic/claude-3.5-sonnet",
	} {
		t.Run(model, func(t *testing.T) {
			res := request(t, cfg, newState(), http.MethodPost, "/opencode.run",
				`{"workspace":"repo","prompt":"go","model":"`+model+`"}`)
			if res.status != http.StatusOK {
				t.Fatalf("status = %d body=%s, want 200 for an allowed provider", res.status, res.body)
			}
		})
	}
}
