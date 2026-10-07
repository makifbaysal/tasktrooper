package context

import (
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// ModelLimits is what one model accepts: the whole context window, the output
// cap a turn asks for by default, and the most it may ever be asked for.
type ModelLimits struct {
	ContextWindow int
	DefaultOutput int
	MaxOutput     int
}

// SummarizeFraction is where a trimmed history lands, as a share of the
// window: low enough that the next trim is many turns away, high enough that
// the run keeps most of what it read.
const SummarizeFraction = 0.45

// A turn's default cap stays at what a non-streaming request finishes inside
// the HTTP timeout; the truncation retry raises it toward MaxOutput only when
// a turn actually needs the room.
const defaultOutputCeiling = 16_384

// Local endpoints load a model with whatever context the server was started
// with — often far below the model's nominal window — so a local model is
// never trusted with more than the old fixed budget.
var conservativeLimits = ModelLimits{ContextWindow: 32_000, DefaultOutput: 4_096, MaxOutput: 8_192}

type limitRule struct {
	match  func(id string) bool
	limits ModelLimits
}

func limitsOf(window, maxOutput int) ModelLimits {
	return ModelLimits{ContextWindow: window, DefaultOutput: min(defaultOutputCeiling, maxOutput), MaxOutput: maxOutput}
}

func prefixed(prefixes ...string) func(string) bool {
	return func(id string) bool {
		for _, p := range prefixes {
			if strings.HasPrefix(id, p) {
				return true
			}
		}
		return false
	}
}

func containing(parts ...string) func(string) bool {
	return func(id string) bool {
		for _, p := range parts {
			if strings.Contains(id, p) {
				return true
			}
		}
		return false
	}
}

// Order matters: the first rule that matches wins, so a family's newer,
// longer ids come before the shorter prefix of its older generation.
var claudeRules = []limitRule{
	{containing("claude-fable-5", "claude-mythos-5", "claude-opus-5", "claude-sonnet-5"), limitsOf(1_000_000, 128_000)},
	{containing("claude-opus-4-6", "claude-opus-4-7", "claude-opus-4-8", "claude-sonnet-4-6"), limitsOf(1_000_000, 128_000)},
	{containing("claude-opus-4-5", "claude-sonnet-4-5", "claude-haiku-4-5"), limitsOf(200_000, 64_000)},
	{containing("claude-opus-4"), limitsOf(200_000, 32_000)},
	{containing("claude-sonnet-4", "claude-3-7-sonnet"), limitsOf(200_000, 64_000)},
	{containing("claude-3-5-sonnet", "claude-3-5-haiku"), limitsOf(200_000, 8_192)},
	{containing("claude-3-"), limitsOf(200_000, 4_096)},
	{containing("claude"), limitsOf(200_000, 32_000)},
}

var openAIRules = []limitRule{
	{prefixed("gpt-5"), limitsOf(400_000, 128_000)},
	{prefixed("gpt-4.1"), limitsOf(1_047_576, 32_768)},
	{prefixed("gpt-4o"), limitsOf(128_000, 16_384)},
	{prefixed("gpt-4-turbo"), limitsOf(128_000, 4_096)},
	{prefixed("o1", "o3", "o4"), limitsOf(200_000, 100_000)},
	{prefixed("gpt-oss"), limitsOf(131_072, 32_768)},
	{prefixed("gpt-3.5-turbo"), limitsOf(16_385, 4_096)},
}

var geminiRules = []limitRule{
	{prefixed("gemini-3", "gemini-2.5"), limitsOf(1_048_576, 65_536)},
	{prefixed("gemini-1.5-pro"), limitsOf(2_097_152, 8_192)},
	{prefixed("gemini"), limitsOf(1_048_576, 8_192)},
}

var openWeightRules = []limitRule{
	{prefixed("llama-4", "meta-llama-4"), limitsOf(131_072, 8_192)},
	{prefixed("llama-3.1", "llama-3.2", "llama-3.3", "llama3.1", "llama3.2", "llama3.3"), limitsOf(131_072, 32_768)},
	{prefixed("kimi-k2"), limitsOf(131_072, 16_384)},
	{prefixed("qwen3", "qwen-3", "qwen2.5", "qwen-2.5"), limitsOf(131_072, 32_768)},
	{prefixed("codestral"), limitsOf(256_000, 32_768)},
	{prefixed("devstral", "mistral", "magistral"), limitsOf(128_000, 32_768)},
	{prefixed("deepseek"), limitsOf(128_000, 8_192)},
}

// LimitsFor resolves a model's limits from its id, which is how every
// provider this product talks to names them; providers do not report limits
// on the endpoints in use. An id no rule recognises gets conservativeLimits,
// and so does anything served by a local endpoint.
func LimitsFor(provider domain.LLMProviderType, model string) ModelLimits {
	if provider == domain.LLMProviderLocal {
		return conservativeLimits
	}
	id := normalizeModelID(model)
	if id == "" {
		return conservativeLimits
	}
	if strings.Contains(id, "claude") {
		return firstMatch(claudeRules, strings.ReplaceAll(id, ".", "-"), conservativeLimits)
	}
	for _, rules := range [][]limitRule{openAIRules, geminiRules, openWeightRules} {
		if l, ok := match(rules, id); ok {
			return l
		}
	}
	switch provider {
	case domain.LLMProviderAnthropic:
		return firstMatch(claudeRules, "claude", conservativeLimits)
	case domain.LLMProviderGemini:
		return firstMatch(geminiRules, "gemini", conservativeLimits)
	}
	return conservativeLimits
}

// normalizeModelID drops the routing prefixes in front of the model's own name:
// "models/gemini-2.5-pro", "openai/gpt-4o", "us.anthropic.claude-sonnet-4-5-…".
func normalizeModelID(model string) string {
	id := strings.ToLower(strings.TrimSpace(model))
	if i := strings.LastIndex(id, "/"); i >= 0 {
		id = id[i+1:]
	}
	if i := strings.Index(id, "claude-"); i > 0 {
		id = id[i:]
	}
	return id
}

func match(rules []limitRule, id string) (ModelLimits, bool) {
	for _, r := range rules {
		if r.match(id) {
			return r.limits, true
		}
	}
	return ModelLimits{}, false
}

func firstMatch(rules []limitRule, id string, fallback ModelLimits) ModelLimits {
	if l, ok := match(rules, id); ok {
		return l
	}
	return fallback
}

// defaultHistoryCeiling caps the history a run carries when the operator has
// not set context.max_tokens. Every turn resends the whole history, so time to
// first token and cache-write cost grow with it; a 1M-token window used to the
// brim makes each turn slower and dearer without making the run better.
const defaultHistoryCeiling = 200_000

// ResolveBudget is the history budget for one model: every non-zero field of
// overrides (the operator's context.* config) wins, every zero one comes from
// the model. ReserveOutput is also the cap each turn asks for, so it never
// exceeds what the model can produce.
func ResolveBudget(overrides Budget, limits ModelLimits) Budget {
	b := overrides
	if b.MaxTokens <= 0 {
		b.MaxTokens = min(limits.ContextWindow, defaultHistoryCeiling)
	}
	if b.ReserveOutput <= 0 {
		b.ReserveOutput = limits.DefaultOutput
	}
	if limits.MaxOutput > 0 && b.ReserveOutput > limits.MaxOutput {
		b.ReserveOutput = limits.MaxOutput
	}
	if b.SummarizeThreshold <= 0 {
		b.SummarizeThreshold = int(float64(b.MaxTokens) * SummarizeFraction)
	}
	return b
}
