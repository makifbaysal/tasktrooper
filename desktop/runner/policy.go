package main

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// runnerPolicy is what this runner refuses on behalf of the service it is
// paired with, and the one place those refusals can be changed.
//
// The zero value is the default, and the default is strict: it is what the
// TaskTrooper cloud has always enforced on a member's machine. A service with
// different rules sends `policy` in the stdin document; nothing here is a
// switch a caller on the tunnel can flip, because a security property the
// remote side may turn off per call is not one.
type runnerPolicy struct {
	// opencodeRefused names the providers opencode.run refuses, lower case.
	// nil means defaultOpencodeRefused; an empty, non-nil slice refuses none.
	opencodeRefused []string
	// keepCredentialsInTranscript stops scrubbing this process's own
	// credential environment values out of streamed output. What this program
	// itself holds — a run's MCP token, the providers' API keys — is scrubbed
	// regardless: switching that off would be this program leaking its own
	// secrets.
	keepCredentialsInTranscript bool
}

// wirePolicy is the stdin document's `policy` object. Every field optional;
// an absent field keeps that rule's default.
type wirePolicy struct {
	// OpencodeRefusedProviders replaces the default list when present. An
	// empty array refuses no provider.
	OpencodeRefusedProviders *[]string `json:"opencode_refused_providers,omitempty"`
	// RedactCredentials, false, keeps this process's credential environment
	// values in streamed output. Absent or true scrubs them.
	RedactCredentials *bool `json:"redact_credentials,omitempty"`
}

// defaultOpencodeRefused are the two providers whose subscription logins
// OpenCode reads as readily as an API key. A Claude subscription authenticates
// the unmodified `claude` binary and nothing else — Anthropic models run
// through claude.run — and Gemini subscription use is off for the same
// reason. Enforced on the model name rather than trusted to whichever
// credential happens to be configured.
var defaultOpencodeRefused = []string{"anthropic", "google"}

var policyProviderName = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

func loadPolicy(w *wirePolicy) (runnerPolicy, error) {
	var p runnerPolicy
	if w == nil {
		return p, nil
	}
	if w.OpencodeRefusedProviders != nil {
		refused := make([]string, 0, len(*w.OpencodeRefusedProviders))
		for _, raw := range *w.OpencodeRefusedProviders {
			name := strings.ToLower(strings.TrimSpace(raw))
			if !policyProviderName.MatchString(name) {
				return runnerPolicy{}, fmt.Errorf("policy.opencode_refused_providers: %q is not a provider name", raw)
			}
			refused = append(refused, name)
		}
		p.opencodeRefused = refused
	}
	if w.RedactCredentials != nil && !*w.RedactCredentials {
		p.keepCredentialsInTranscript = true
	}
	return p, nil
}

// opencodeRefusal answers whether opencode.run may use `provider`, and the
// sentence to put in front of whoever asked when it may not.
func (p runnerPolicy) opencodeRefusal(model, provider string) *rpcError {
	name := strings.ToLower(provider)
	refused := p.opencodeRefused
	if refused == nil {
		refused = defaultOpencodeRefused
	}
	if !slices.Contains(refused, name) {
		return nil
	}
	switch name {
	case "anthropic":
		return failure(codeBadRequest,
			"model %q uses the anthropic provider; Anthropic models run through Claude Code (claude.run), not OpenCode", model)
	case "google":
		return failure(codeBadRequest,
			"model %q uses the google provider; Gemini subscription use through OpenCode is refused by this runner's policy", model)
	}
	return failure(codeBadRequest, "model %q uses the %s provider, which this runner's policy refuses for OpenCode", model, name)
}
