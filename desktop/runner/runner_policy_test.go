//go:build !windows

package main

import (
	"net/http"
	"strings"
	"testing"
)

// The rules policy.go keeps behind configuration: absent means strict, and a
// service that wants something else has to say so in the stdin document.

func TestAbsentPolicyKeepsTheStrictDefaults(t *testing.T) {
	cfg, err := loadConfig([]byte(validConfig))
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.policy.opencodeRefused != nil || cfg.policy.keepCredentialsInTranscript {
		t.Fatalf("policy = %+v, want the zero value", cfg.policy)
	}
	for _, provider := range []string{"anthropic", "Google"} {
		if cfg.policy.opencodeRefusal(provider+"/m", provider) == nil {
			t.Errorf("%s was allowed with no policy sent", provider)
		}
	}
}

func TestPolicyReplacesTheRefusedProviders(t *testing.T) {
	cfg, err := loadConfig([]byte(configWith(t, map[string]any{
		"policy": map[string]any{"opencode_refused_providers": []string{"OpenRouter"}},
	})))
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if refusal := cfg.policy.opencodeRefusal("anthropic/claude", "anthropic"); refusal != nil {
		t.Fatalf("anthropic refused under a list that does not name it: %s", refusal.Message)
	}
	refusal := cfg.policy.opencodeRefusal("openrouter/x", "openrouter")
	if refusal == nil || refusal.Code != codeBadRequest || !strings.Contains(refusal.Message, "policy") {
		t.Fatalf("refusal = %+v, want a bad_request naming the policy", refusal)
	}
}

func TestAnEmptyRefusedListAllowsAnthropicThroughOpencode(t *testing.T) {
	cfg := config{
		opencodeBin:  exitingCLI(t),
		gitBin:       "/usr/bin/git",
		workspaceDir: emptyWorkspace(t),
		policy:       runnerPolicy{opencodeRefused: []string{}},
	}
	res := request(t, cfg, newState(), http.MethodPost, "/opencode.run",
		`{"workspace":"repo","prompt":"go","model":"anthropic/claude-sonnet-4.5"}`)
	if res.status != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200 when the policy refuses nothing", res.status, res.body)
	}
}

func TestPolicyRefusesAMalformedProviderOrAnUnknownField(t *testing.T) {
	for name, policy := range map[string]any{
		"leading dash":  map[string]any{"opencode_refused_providers": []string{"-x"}},
		"unknown field": map[string]any{"antigravity": true},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := loadConfig([]byte(configWith(t, map[string]any{"policy": policy}))); err == nil {
				t.Fatal("loadConfig accepted it")
			}
		})
	}
}

func TestRedactionOffKeepsEnvValuesButNeverWhatTheRunnerHolds(t *testing.T) {
	clearCredentialEnv(t)
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-0123456789abcdef")
	token := "tt-run-abcdef0123456789-secret"

	off, err := loadPolicy(&wirePolicy{RedactCredentials: new(bool)})
	if err != nil {
		t.Fatalf("loadPolicy: %v", err)
	}
	redact := credentialRedactor(off, token)
	if redact == nil {
		t.Fatal("no redactor although the run holds an mcp token")
	}
	got := string(redact([]byte(`{"data":"sk-ant-0123456789abcdef ` + token + `"}`)))
	if !strings.Contains(got, "sk-ant-0123456789abcdef") {
		t.Fatalf("the env value was scrubbed with redaction off: %s", got)
	}
	if strings.Contains(got, token) {
		t.Fatalf("the run's own mcp token reached the output: %s", got)
	}
}
