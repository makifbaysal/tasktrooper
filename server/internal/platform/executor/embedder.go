package executor

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/llm"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

var errEmbedderDoesNotChat = errors.New("the embedding engine answers embeddings only")

// localEmbedder is the desktop's embedding engine as the local index sees it:
// the OpenAI-compatible endpoint at embeddings_base_url, pinned to the one
// model the engine says it serves. Every vector the index holds comes from
// it, so its provenance names the engine as well as the model. The endpoint
// can move while the executor runs; a call in flight finishes against the
// old one and the indexer retries it against the new.
type localEmbedder struct {
	timeout time.Duration

	mu      sync.Mutex
	baseURL string
	source  string
	client  port.LLMClient
	model   string
}

var (
	_ port.EmbeddingsEndpoint = (*localEmbedder)(nil)
	_ port.LLMClient          = (*localEmbedder)(nil)
)

func newLocalEmbedder(baseURL, source string, timeout time.Duration) *localEmbedder {
	e := &localEmbedder{timeout: timeout}
	e.set(baseURL, source)
	return e
}

func (e *localEmbedder) set(baseURL, source string) {
	if source == "" {
		source = domain.EmbeddingSourceONNXInt8
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if baseURL != e.baseURL || source != e.source {
		e.model = ""
	}
	e.baseURL, e.source, e.client = baseURL, source, nil
	if baseURL != "" {
		e.client = llm.NewOpenAICompatClient(baseURL, "", "", e.timeout)
	}
}

func (e *localEmbedder) SetEmbeddingsEndpoint(baseURL, source string) error {
	normalized, err := loopbackEmbeddings(baseURL)
	if err != nil {
		return fmt.Errorf("%w: %w", port.ErrLocalIndexInvalid, err)
	}
	e.set(normalized, strings.TrimSpace(source))
	got, gotSource := e.EmbeddingsEndpoint()
	log.Info().Str("embeddings_base_url", got).Str("embeddings_source", gotSource).Msg("executor: the embedding engine moved")
	return nil
}

func (e *localEmbedder) EmbeddingsEndpoint() (string, string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.baseURL, e.source
}

func (e *localEmbedder) Configured() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.client != nil
}

func (e *localEmbedder) current() (port.LLMClient, string, string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.client == nil {
		return nil, "", "", fmt.Errorf("%w: no embedding engine is configured on this computer (embeddings_base_url)", port.ErrLocalIndexUnavailable)
	}
	return e.client, e.model, e.source, nil
}

// pinned asks the engine once which model it serves. A failed answer is not
// remembered: the engine binds its port before its model is on disk.
func (e *localEmbedder) pinned(ctx context.Context) (port.LLMClient, string, string, error) {
	client, model, source, err := e.current()
	if err != nil || model != "" {
		return client, model, source, err
	}
	models, err := client.Models(ctx)
	if err != nil {
		return nil, "", "", fmt.Errorf("%w: ask the embedding engine which model it serves: %w", port.ErrLocalIndexEmbedder, err)
	}
	for _, m := range models {
		if m = strings.TrimSpace(m); m != "" {
			model = m
			break
		}
	}
	if model == "" {
		return nil, "", "", fmt.Errorf("%w: the embedding engine lists no model", port.ErrLocalIndexEmbedder)
	}
	e.mu.Lock()
	if e.client == client {
		e.model = model
	}
	e.mu.Unlock()
	return client, model, source, nil
}

func (e *localEmbedder) Embed(ctx context.Context, input, _ string) ([]float32, error) {
	client, model, _, err := e.pinned(ctx)
	if err != nil {
		return nil, err
	}
	vec, err := client.Embed(ctx, input, model)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", port.ErrLocalIndexEmbedder, err)
	}
	return vec, nil
}

func (e *localEmbedder) ResolvedEmbedding(ctx context.Context) (string, int, error) {
	_, model, source, err := e.pinned(ctx)
	if err != nil {
		return "", 0, err
	}
	dims := 0
	if model == domain.PinnedLocalEmbeddingModel {
		dims = domain.PinnedLocalEmbeddingDimensions
	}
	return domain.EmbeddingProvenance(model, source), dims, nil
}

func (e *localEmbedder) Models(ctx context.Context) ([]string, error) {
	_, model, _, err := e.pinned(ctx)
	if err != nil {
		return nil, err
	}
	return []string{model}, nil
}

func (e *localEmbedder) Chat(context.Context, domain.AgentRequest) (domain.AgentResponse, error) {
	return domain.AgentResponse{}, errEmbedderDoesNotChat
}

func (e *localEmbedder) ChatStream(context.Context, domain.AgentRequest, func(string)) (domain.AgentResponse, error) {
	return domain.AgentResponse{}, errEmbedderDoesNotChat
}
