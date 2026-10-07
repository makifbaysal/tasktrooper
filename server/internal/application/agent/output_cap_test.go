package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestRaisedOutputCap(t *testing.T) {
	tests := []struct {
		name                                     string
		current, maxOutput, window, promptTokens int
		want                                     int
	}{
		{"doubles", 16_384, 128_000, 1_000_000, 50_000, 32_768},
		{"stops at the model ceiling", 16_384, 20_000, 1_000_000, 50_000, 20_000},
		{"already at the ceiling", 16_384, 16_384, 1_000_000, 50_000, 0},
		{"bounded by what the window has left", 16_384, 128_000, 200_000, 180_000, 20_000},
		{"no room left in the window", 16_384, 128_000, 200_000, 190_000, 0},
		{"unknown prompt size trusts the ceiling", 4_096, 8_192, 32_000, 0, 8_192},
		{"no cap on the request", 0, 8_192, 32_000, 1_000, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, raisedOutputCap(tt.current, tt.maxOutput, tt.window, tt.promptTokens))
		})
	}
}

func TestRunTokensWeighsCacheReads(t *testing.T) {
	assert.Equal(t, 290, runTokens(domain.Usage{PromptTokens: 1000, CacheReadTokens: 900, CompletionTokens: 100}))
	assert.Equal(t, 1100, runTokens(domain.Usage{PromptTokens: 1000, CompletionTokens: 100}))
	assert.Equal(t, 110, runTokens(domain.Usage{PromptTokens: 100, CacheReadTokens: 500, CompletionTokens: 100}), "a cache read larger than the prompt is clamped to it")
}
