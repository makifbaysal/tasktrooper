package main

import "os"

// The redaction claude.run, opencode.run and cursor.run share.
//
// All three inherit this process's FULL environment (session.go,
// opencode_run.go, cursor_run.go) rather than a curated one: `claude` has to
// authenticate exactly as it would running locally, and the other two CLIs are
// under the same rule for the same reason — stripping their env to keep a
// secret out of the child would also strip the login that makes them work.
// What is scrubbed instead is the TRANSCRIPT: a tool on the far side of one of
// these CLIs that echoes its own inputs — a misbehaving MCP client, a `printenv`
// a task happened to run — would otherwise put one of these values in a log the
// control plane relays and somebody eventually pastes into a ticket.
//
// credentialRedactor builds on newSecretRedactor (mobile_release.go) rather
// than a second scrubbing pass: the same short-value floor (see
// releaseRedactMinLen), the same per-physical-line matching for a value that
// arrives wrapped across two forwarded frames, and the same JSON-escaped
// variant apply here for the same reasons they apply to a release's signing
// material.

// credentialEnvNames are the environment variables this process's own
// environment may carry a credential in, for the CLIs it spawns unmodified.
// Not a claim that this list is closed — env is not the allowlisted parameter
// here, the transcript is what is being defended — so a value this program
// does not recognise by name still cannot leave through it if a caller
// includes it as a name here; the fixed set is what makes the redactor's
// output the same on every run rather than a function of whatever happens to
// be exported on one person's Mac.
var credentialEnvNames = []string{
	"CLAUDE_CODE_OAUTH_TOKEN",
	"ANTHROPIC_API_KEY",
	"ANTHROPIC_AUTH_TOKEN",
	"OPENAI_API_KEY",
	"GEMINI_API_KEY",
	"GOOGLE_API_KEY",
	"CURSOR_API_KEY",
	"GITHUB_TOKEN",
	"GH_TOKEN",
}

// mcpRunTokenLabel is the map key the run's own MCP bearer token is redacted
// under. It is not an environment variable name — the token never becomes
// one — just a label for the one secret this call carries that is not already
// in this process's environment.
const mcpRunTokenLabel = "MCP_TOKEN"

// credentialRedactor builds the scrubbing for one call's output: this
// process's own credential-bearing environment values, present, plus — when
// the call carries one — the MCP bearer token minted for this run.
//
// Built once per call rather than once at startup, because the token argument
// is the call's own and a call with none must not scrub an empty string (see
// newSecretRedactor: a redactor over no secrets is nil, so a call with no MCP
// token and nothing set in this process's own environment attaches none at
// all).
func credentialRedactor(policy runnerPolicy, mcpToken string) func([]byte) []byte {
	return heldSecretRedactor(policy, map[string]string{mcpRunTokenLabel: mcpToken})
}

// heldSecretRedactor is credentialRedactor over any set of secrets this
// program holds for the call, keyed by the label the marker names. Those are
// scrubbed whatever the policy says; only the environment half is the
// policy's to turn off.
func heldSecretRedactor(policy runnerPolicy, held map[string]string) func([]byte) []byte {
	secrets := make(map[string]string, len(credentialEnvNames)+len(held))
	if !policy.keepCredentialsInTranscript {
		for _, name := range credentialEnvNames {
			if v, ok := os.LookupEnv(name); ok && v != "" {
				secrets[name] = v
			}
		}
	}
	for label, value := range held {
		if value != "" {
			secrets[label] = value
		}
	}
	return newSecretRedactor(secrets)
}
