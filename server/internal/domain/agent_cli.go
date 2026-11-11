package domain

import (
	"errors"
	"fmt"
	"time"
)

// AgentCLIFlavor names one local agent CLI by the on-disk conventions its
// catalog is rendered in — deliberately the file layout, not the provider:
// a flavor is a file layout (.claude/skills/... vs .cursor/rules/...) and a
// provider is an execution engine, and the two are not guaranteed to stay
// one-to-one.
type AgentCLIFlavor string

const (
	AgentCLIFlavorClaude      AgentCLIFlavor = "claude"
	AgentCLIFlavorCursor      AgentCLIFlavor = "cursor"
	AgentCLIFlavorAntigravity AgentCLIFlavor = "antigravity"
	AgentCLIFlavorOpencode    AgentCLIFlavor = "opencode"
)

// Revealed in this order: it is what the settings page renders, and a set that
// came out of a map would reshuffle the cards between page loads.
var agentCLIFlavors = []AgentCLIFlavor{AgentCLIFlavorClaude, AgentCLIFlavorCursor, AgentCLIFlavorAntigravity, AgentCLIFlavorOpencode}

// AgentCLIFlavors returns every flavor the product declares, available or not.
func AgentCLIFlavors() []AgentCLIFlavor {
	out := make([]AgentCLIFlavor, len(agentCLIFlavors))
	copy(out, agentCLIFlavors)
	return out
}

var agentCLIProviders = map[AgentCLIFlavor]LLMProviderType{
	AgentCLIFlavorClaude:      LLMProviderClaudeCode,
	AgentCLIFlavorCursor:      LLMProviderCursorAgent,
	AgentCLIFlavorAntigravity: LLMProviderAntigravity,
	AgentCLIFlavorOpencode:    LLMProviderOpencode,
}

func AgentCLIProviderFor(f AgentCLIFlavor) (LLMProviderType, bool) {
	p, ok := agentCLIProviders[f]
	return p, ok
}

// The reverse: which CLI a provider's runs are executed by. False for every
// provider that is an HTTP endpoint.
func AgentCLIFlavorFor(t LLMProviderType) (AgentCLIFlavor, bool) {
	for flavor, provider := range agentCLIProviders {
		if provider == t {
			return flavor, true
		}
	}
	return "", false
}

// A flavor that is merely unknown is a 400 with a different sentence from one
// that is known and unavailable, so the caller needs to tell them apart.
func ValidAgentCLIFlavor(f AgentCLIFlavor) bool {
	_, ok := agentCLIProviders[f]
	return ok
}

// AgentCLIConnection is one local agent CLI this install has connected. An
// install may hold several at once, one per flavor: connecting a flavor no
// longer disconnects another, and the flavors do not collide on disk because
// agentfs materialises a TASK's catalog into its own workspace. The fields
// past the flavor are evidence, not configuration — nothing reads them to run
// anything.
type AgentCLIConnection struct {
	Flavor        AgentCLIFlavor  `json:"flavor"`
	ProviderType  LLMProviderType `json:"provider_type"`
	BinaryPath    string          `json:"binary_path"`
	BinaryVersion string          `json:"binary_version"`
	// NOT what a board run reads: a run materialises its own agent into its
	// task workspace, from the database, at dispatch.
	CatalogPath string    `json:"catalog_path"`
	AgentCount  int       `json:"agent_count"`
	SkillCount  int       `json:"skill_count"`
	ConnectedAt time.Time `json:"connected_at"`
}

// AgentCLIFlavorView is one card on the settings page.
type AgentCLIFlavorView struct {
	Flavor       AgentCLIFlavor  `json:"flavor"`
	ProviderType LLMProviderType `json:"provider_type"`
	Label        string          `json:"label"`
	Available    bool            `json:"available"`
	Connected    bool            `json:"connected"`
	// DefaultModel is what an agent set to "default model" runs on; empty
	// means the CLI's own default.
	DefaultModel string `json:"default_model"`
}

// AgentCLIState is what every endpoint in this surface answers with, so the
// client never merges a mutation's result into a separate fetch.
type AgentCLIState struct {
	// Empty is a normal state: an install running entirely on API providers
	// never connects one.
	Connections []AgentCLIConnection `json:"connections"`
	Flavors     []AgentCLIFlavorView `json:"flavors"`
}

// ErrAgentCLIBinaryMissing marks the refusal for a CLI that is not installed
// where the sessions would run. A separate sentinel from
// ErrAgentCLIUnauthenticated because the two have different fixes; the wording
// "where the sessions run" rather than "on this host" is load-bearing (the
// host is a Linux pod, the CLI belongs on the user's Mac).
var ErrAgentCLIBinaryMissing = errors.New("agent cli binary is not installed where the sessions run")

// ErrAgentCLIUnauthenticated marks the refusal for a CLI that IS installed and
// has no usable subscription session.
var ErrAgentCLIUnauthenticated = errors.New("agent cli binary is installed but not signed in")

// ErrAgentCLINotConnected marks every refusal caused by dispatching work onto
// a host-executed provider whose CLI this install has not connected. Permanent:
// nothing about it becomes true on a retry.
var ErrAgentCLINotConnected = errors.New("agent cli flavor is not the connected one")

// ErrCLIFlavorNotConnected answers for the board runner when an agent's
// provider is a local CLI that is not itself connected. The sentence names
// which agent and which CLI it wants — the alternative dispatches onto a
// binary nobody verified.
func ErrCLIFlavorNotConnected(agentName string, want LLMProviderType) error {
	label := string(want)
	if def, ok := LLMProviderDefinitionFor(want); ok && def.Label != "" {
		label = def.Label
	}
	return fmt.Errorf("agent %q runs on %s, which is not connected: connect it under LLM settings, "+
		"or move the agent to an API-backed provider: %w", agentName, label, ErrAgentCLINotConnected)
}
