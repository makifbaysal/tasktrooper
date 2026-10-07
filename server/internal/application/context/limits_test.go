package context

import (
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type LimitsSuite struct {
	suite.Suite
}

func TestLimitsSuite(t *testing.T) {
	suite.Run(t, new(LimitsSuite))
}

func (s *LimitsSuite) TestLimitsForKnownModels() {
	tests := []struct {
		name     string
		provider domain.LLMProviderType
		model    string
		window   int
		output   int
		max      int
	}{
		{"claude opus 5.5", domain.LLMProviderAnthropic, "claude-opus-5-5", 1_000_000, 16_384, 128_000},
		{"claude sonnet 4.6 dated", domain.LLMProviderAnthropic, "claude-sonnet-4-6-20260101", 1_000_000, 16_384, 128_000},
		{"claude haiku 4.5", domain.LLMProviderAnthropic, "claude-haiku-4-5-20251001", 200_000, 16_384, 64_000},
		{"claude opus 4.1", domain.LLMProviderAnthropic, "claude-opus-4-1-20250805", 200_000, 16_384, 32_000},
		{"claude opus 4.5 is not opus 4", domain.LLMProviderAnthropic, "claude-opus-4-5", 200_000, 16_384, 64_000},
		{"claude 3.5 sonnet", domain.LLMProviderAnthropic, "claude-3-5-sonnet-20241022", 200_000, 8_192, 8_192},
		{"claude 3 haiku", domain.LLMProviderAnthropic, "claude-3-haiku-20240307", 200_000, 4_096, 4_096},
		{"bedrock-routed claude", domain.LLMProviderOpenAI, "us.anthropic.claude-sonnet-4-5-20250929-v1:0", 200_000, 16_384, 64_000},
		{"openrouter dotted claude", domain.LLMProviderOpenAI, "anthropic/claude-sonnet-4.5", 200_000, 16_384, 64_000},
		{"gpt-4o", domain.LLMProviderOpenAI, "gpt-4o", 128_000, 16_384, 16_384},
		{"gpt-4.1 mini", domain.LLMProviderOpenAI, "gpt-4.1-mini", 1_047_576, 16_384, 32_768},
		{"gpt-5", domain.LLMProviderOpenAI, "gpt-5-mini", 400_000, 16_384, 128_000},
		{"o3", domain.LLMProviderOpenAI, "o3-mini", 200_000, 16_384, 100_000},
		{"gemini 2.5 flash", domain.LLMProviderGemini, "gemini-2.5-flash", 1_048_576, 16_384, 65_536},
		{"gemini with models prefix", domain.LLMProviderGemini, "models/gemini-2.0-flash", 1_048_576, 8_192, 8_192},
		{"groq llama", domain.LLMProviderGroq, "llama-3.3-70b-versatile", 131_072, 16_384, 32_768},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			got := LimitsFor(tt.provider, tt.model)
			s.Equal(ModelLimits{ContextWindow: tt.window, DefaultOutput: tt.output, MaxOutput: tt.max}, got)
		})
	}
}

func (s *LimitsSuite) TestUnknownAndLocalModelsGetTheConservativeLimits() {
	tests := []struct {
		name     string
		provider domain.LLMProviderType
		model    string
	}{
		{"unknown remote model", domain.LLMProviderOpenAI, "acme-reasoner-7"},
		{"empty model", domain.LLMProviderOpenAI, ""},
		{"local endpoint even for a known family", domain.LLMProviderLocal, "qwen2.5-coder-7b-instruct"},
		{"local endpoint serving a claude name", domain.LLMProviderLocal, "claude-opus-5"},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.Equal(conservativeLimits, LimitsFor(tt.provider, tt.model))
		})
	}
}

func (s *LimitsSuite) TestUnknownModelOnAKnownProviderUsesThatProvidersFallback() {
	s.Equal(200_000, LimitsFor(domain.LLMProviderAnthropic, "some-future-alias").ContextWindow)
	s.Equal(1_048_576, LimitsFor(domain.LLMProviderGemini, "experimental-thing").ContextWindow)
}

func (s *LimitsSuite) TestResolveBudgetTakesEveryZeroFieldFromTheModel() {
	limits := ModelLimits{ContextWindow: 200_000, DefaultOutput: 16_384, MaxOutput: 64_000}

	b := ResolveBudget(Budget{KeepRecentMessages: 10}, limits)

	s.Equal(Budget{MaxTokens: 200_000, ReserveOutput: 16_384, SummarizeThreshold: 90_000, KeepRecentMessages: 10}, b)
}

func (s *LimitsSuite) TestResolveBudgetKeepsOperatorOverrides() {
	limits := ModelLimits{ContextWindow: 1_000_000, DefaultOutput: 16_384, MaxOutput: 128_000}

	b := ResolveBudget(Budget{MaxTokens: 100_000, ReserveOutput: 8_000, SummarizeThreshold: 30_000, KeepRecentMessages: 6}, limits)

	s.Equal(Budget{MaxTokens: 100_000, ReserveOutput: 8_000, SummarizeThreshold: 30_000, KeepRecentMessages: 6}, b)
}

func (s *LimitsSuite) TestResolveBudgetDerivesTheThresholdFromAnOverriddenWindow() {
	b := ResolveBudget(Budget{MaxTokens: 100_000}, LimitsFor(domain.LLMProviderAnthropic, "claude-opus-5"))

	s.Equal(45_000, b.SummarizeThreshold)
}

func (s *LimitsSuite) TestResolveBudgetNeverAsksForMoreOutputThanTheModelHas() {
	b := ResolveBudget(Budget{ReserveOutput: 50_000}, ModelLimits{ContextWindow: 128_000, DefaultOutput: 4_096, MaxOutput: 16_384})

	s.Equal(16_384, b.ReserveOutput)
}

func (s *LimitsSuite) TestApplyWithoutAWindowTrimsNothing() {
	messages := []domain.Message{{Role: domain.RoleUser, Content: repeat("x", 4000)}, {Role: domain.RoleUser, Content: "last"}}

	s.Equal(messages, Budget{KeepRecentMessages: 1}.Apply(messages))
}

// A 1M window is not a reason to resend 1M tokens every turn: the default
// history stops at the ceiling unless the operator raises context.max_tokens.
func (s *LimitsSuite) TestResolveBudgetCapsAHugeWindowByDefault() {
	b := ResolveBudget(Budget{}, ModelLimits{ContextWindow: 1_000_000, DefaultOutput: 16_384, MaxOutput: 128_000})
	s.Equal(200_000, b.MaxTokens)
	s.Equal(90_000, b.SummarizeThreshold)

	raised := ResolveBudget(Budget{MaxTokens: 600_000}, ModelLimits{ContextWindow: 1_000_000, DefaultOutput: 16_384, MaxOutput: 128_000})
	s.Equal(600_000, raised.MaxTokens)
}
