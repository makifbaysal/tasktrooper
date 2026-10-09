package llmprovider

import (
	"context"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// SetBundledEmbedder names the engine behind EMBEDDINGS_BASE_URL. Nothing
// stored says which engine an OpenAI-compatible URL is, and two engines answer
// under the same model name with vectors that do not compare.
func (s *Service) SetBundledEmbedder(baseURL, source string) {
	s.bundledEmbedderURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	s.bundledEmbedderSource = strings.TrimSpace(source)
}

// ResolvedEmbeddingSource names the engine only while embeddings still go to
// the bundled embedder: the user may have moved them to another provider or
// pointed the local row elsewhere since boot.
func (s *Service) ResolvedEmbeddingSource(ctx context.Context) (string, string, error) {
	model, _, err := s.ResolvedEmbedding(ctx)
	if err != nil {
		return "", "", err
	}
	if s.bundledEmbedderURL == "" || s.bundledEmbedderSource == "" {
		return model, "", nil
	}
	provider, err := s.store.GetEmbeddingProvider(ctx)
	if err != nil {
		return "", "", err
	}
	if provider != domain.LLMProviderLocal {
		return model, "", nil
	}
	row, err := s.store.Get(ctx, domain.LLMProviderLocal)
	if err != nil {
		return "", "", err
	}
	if strings.TrimRight(strings.TrimSpace(row.BaseURL), "/") != s.bundledEmbedderURL {
		return model, "", nil
	}
	return model, s.bundledEmbedderSource, nil
}
