package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestMuxWithDefaultModel(t *testing.T) {
	m := &muxExecutor{defaultModel: func(_ context.Context, p domain.LLMProviderType) string {
		if p == domain.LLMProviderOpencode {
			return "anthropic/claude-sonnet-5"
		}
		return ""
	}}

	_, model := m.withDefaultModel(context.Background(), domain.LLMProviderOpencode, "")
	require.Equal(t, "anthropic/claude-sonnet-5", model)

	_, model = m.withDefaultModel(context.Background(), domain.LLMProviderOpencode, "openai/gpt-5")
	require.Equal(t, "openai/gpt-5", model, "a model named on the agent is never replaced")

	_, model = m.withDefaultModel(context.Background(), domain.LLMProviderClaudeCode, "")
	require.Empty(t, model, "no configured default leaves the CLI to pick")

	bare := &muxExecutor{}
	_, model = bare.withDefaultModel(context.Background(), domain.LLMProviderOpencode, "")
	require.Empty(t, model)
}
