package domain

import "time"

// LLMUsageKind distinguishes what a llm_usage row actually paid for. The
// budget gate (see BillingStore.UsdSpentSince) excludes LLMUsageKindCLI: a
// host CLI session is paid by the CLI's own subscription, not this install's
// configured model prices.
type LLMUsageKind string

const (
	LLMUsageKindAPI       LLMUsageKind = "api"
	LLMUsageKindCLI       LLMUsageKind = "cli"
	LLMUsageKindEmbedding LLMUsageKind = "embedding"
)

// LLMUsageRecord is one LLM call's token usage. The cache fields are subsets of
// PromptTokens, never additions to it — see Usage for the full contract.
type LLMUsageRecord struct {
	Kind             LLMUsageKind `json:"kind"`
	Provider         string       `json:"provider"`
	Model            string       `json:"model"`
	ModelFromDefault bool         `json:"model_from_default,omitempty"`
	PromptTokens     int          `json:"prompt_tokens"`
	CompletionTokens int          `json:"completion_tokens"`
	CacheReadTokens  int          `json:"cache_read_tokens"`
	CacheWriteTokens int          `json:"cache_write_tokens"`
	CreatedAt        time.Time    `json:"created_at"`
}

// LLMUsageTotals aggregates token counts over a group (day, model, or all).
// PromptTokens is always the TOTAL prompt size; CacheReadTokens and
// CacheWriteTokens are subsets of it, never additions.
type LLMUsageTotals struct {
	Calls            int   `json:"calls"`
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	CacheReadTokens  int64 `json:"cache_read_tokens"`
	CacheWriteTokens int64 `json:"cache_write_tokens"`
}

type LLMUsageByModel struct {
	Kind     string `json:"kind"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
	// ModelFromDefault: the agent named no model, so Model is the provider
	// default or whatever the CLI reported it ran on.
	ModelFromDefault bool `json:"model_from_default"`
	LLMUsageTotals
}

type LLMUsageByDay struct {
	Day string `json:"day"` // YYYY-MM-DD in the requested timezone
	LLMUsageTotals
}

// LLMUsageSummary is the dashboard payload for the requested window.
// Generation covers kind IN ('api','cli') — real model spend, whether it went
// through this install's own LLM client or a host CLI's own subscription.
// Embedding is broken out separately because it is a background maintenance
// cost, not conversation spend, and dwarfs it in call volume.
type LLMUsageSummary struct {
	Days       int               `json:"days"`
	Timezone   string            `json:"timezone"`
	From       string            `json:"from"`
	To         string            `json:"to"`
	Generation LLMUsageTotals    `json:"generation"`
	Embedding  LLMUsageTotals    `json:"embedding"`
	ByModel    []LLMUsageByModel `json:"by_model"`
	Daily      []LLMUsageByDay   `json:"daily"`
}
