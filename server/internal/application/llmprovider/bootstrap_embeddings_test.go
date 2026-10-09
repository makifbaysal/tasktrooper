package llmprovider_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/llmprovider"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const embedderURL = "http://127.0.0.1:51545"

func TestBootstrapEmbeddings_ConfiguresTheBundledEmbedderOnAFreshInstall(t *testing.T) {
	store := newProviderStore()
	svc := llmprovider.NewService(store, nil, nil, 0, nil)

	require.NoError(t, svc.BootstrapEmbeddings(context.Background(), embedderURL))

	row := store.configs[domain.LLMProviderLocal]
	require.True(t, row.Configured)
	require.Equal(t, embedderURL, row.BaseURL)
	require.Equal(t, domain.PinnedLocalEmbeddingModel, row.DefaultModel)
	require.Equal(t, domain.LLMProviderLocal, store.embeddingProvider)
	require.Equal(t, domain.PinnedLocalEmbeddingModel, store.embeddingModel)
}

func TestBootstrapEmbeddings_FollowsTheEmbedderToItsNewPort(t *testing.T) {
	store := newProviderStore()
	store.configs[domain.LLMProviderLocal] = domain.LLMProviderConfig{
		ProviderType: domain.LLMProviderLocal,
		BaseURL:      "http://127.0.0.1:40000",
		DefaultModel: domain.PinnedLocalEmbeddingModel,
		Configured:   true,
	}
	svc := llmprovider.NewService(store, nil, nil, 0, nil)

	require.NoError(t, svc.BootstrapEmbeddings(context.Background(), embedderURL))

	require.Equal(t, embedderURL, store.configs[domain.LLMProviderLocal].BaseURL)
}

func TestBootstrapEmbeddings_LeavesAUserConfiguredEndpointAlone(t *testing.T) {
	store := newProviderStore()
	mine := domain.LLMProviderConfig{
		ProviderType: domain.LLMProviderLocal,
		BaseURL:      "http://192.168.1.20:11434/v1",
		DefaultModel: "qwen2.5-coder:32b",
		Configured:   true,
	}
	store.configs[domain.LLMProviderLocal] = mine
	svc := llmprovider.NewService(store, nil, nil, 0, nil)

	require.NoError(t, svc.BootstrapEmbeddings(context.Background(), embedderURL))

	require.Equal(t, mine, store.configs[domain.LLMProviderLocal])
	require.Empty(t, store.embeddingProvider)
}

func TestResolvedEmbeddingSource_NamesTheEngineOnlyWhileEmbeddingsGoToTheBundledEmbedder(t *testing.T) {
	tests := []struct {
		name       string
		bundled    string
		moveTo     domain.LLMProviderType
		repoint    string
		wantSource string
	}{
		{"the bundled embedder", domain.EmbeddingSourceONNXInt8, "", "", domain.EmbeddingSourceONNXInt8},
		{"no engine named at boot", "", "", "", ""},
		{"embeddings moved to another provider", domain.EmbeddingSourceONNXInt8, domain.LLMProviderOpenAI, "", ""},
		{"the local row pointed elsewhere", domain.EmbeddingSourceONNXInt8, "", "http://127.0.0.1:1234/v1", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			store := newProviderStore()
			svc := llmprovider.NewService(store, nil, nil, 0, nil)
			require.NoError(t, svc.BootstrapEmbeddings(ctx, embedderURL))
			svc.SetBundledEmbedder(embedderURL+"/", tt.bundled)
			if tt.moveTo != "" {
				store.embeddingProvider = tt.moveTo
			}
			if tt.repoint != "" {
				row := store.configs[domain.LLMProviderLocal]
				row.BaseURL = tt.repoint
				store.configs[domain.LLMProviderLocal] = row
			}

			model, source, err := svc.ResolvedEmbeddingSource(ctx)

			require.NoError(t, err)
			require.Equal(t, domain.PinnedLocalEmbeddingModel, model)
			require.Equal(t, tt.wantSource, source)
		})
	}
}
