package localindex

import (
	"context"
	"errors"
	"sync"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type progressKey struct{}

type progressFunc func(processed, total int)

// withProgress rides on the pass's context because the indexer reports
// progress only by writing it to its store, and its detached contexts keep
// values: the store is where the report can be heard.
func withProgress(ctx context.Context, fn progressFunc) context.Context {
	return context.WithValue(ctx, progressKey{}, fn)
}

type reportingStore struct {
	port.LocalIndexStore
}

func (s reportingStore) UpdateIndexProgress(ctx context.Context, indexID uuid.UUID, processed, total int) error {
	err := s.LocalIndexStore.UpdateIndexProgress(ctx, indexID, processed, total)
	if fn, ok := ctx.Value(progressKey{}).(progressFunc); ok {
		fn(processed, total)
	}
	return err
}

// SetEmbeddingResolver passes the indexer's resolver on to the store, which
// guards every search with it; embedding the store hides the method.
func (s reportingStore) SetEmbeddingResolver(r port.EmbeddingProvenanceResolver) {
	if aware, ok := s.LocalIndexStore.(interface {
		SetEmbeddingResolver(port.EmbeddingProvenanceResolver)
	}); ok {
		aware.SetEmbeddingResolver(r)
	}
}

type chatKey struct{}

func withChat(ctx context.Context, chat port.LLMClient) context.Context {
	return context.WithValue(ctx, chatKey{}, chat)
}

var errNoRewriteModel = errors.New("no model is available to rewrite the query")

// indexClient is the indexer's and the injector's model: the embedding engine
// for vectors, and for the injector's query rewrite whichever model the run
// that asked for the injection is on. A pass never chats, and a run that
// brings no model gets its query searched as written.
type indexClient struct {
	embedder port.LLMClient
}

func (c indexClient) Embed(ctx context.Context, input, model string) ([]float32, error) {
	return c.embedder.Embed(ctx, input, model)
}

func (c indexClient) Chat(ctx context.Context, req domain.AgentRequest) (domain.AgentResponse, error) {
	chat, ok := ctx.Value(chatKey{}).(port.LLMClient)
	if !ok || chat == nil {
		return domain.AgentResponse{}, errNoRewriteModel
	}
	return chat.Chat(ctx, req)
}

func (c indexClient) ChatStream(ctx context.Context, req domain.AgentRequest, _ func(string)) (domain.AgentResponse, error) {
	return c.Chat(ctx, req)
}

func (c indexClient) Models(ctx context.Context) ([]string, error) {
	return c.embedder.Models(ctx)
}

// keyLocks serialises passes over one index. A second ensure of the same
// branch waits for the first and then finds little or nothing left to embed.
type keyLocks struct {
	mu   sync.Mutex
	held map[string]chan struct{}
}

func (k *keyLocks) lock(ctx context.Context, key string, onWait func()) (func(), error) {
	for {
		k.mu.Lock()
		if k.held == nil {
			k.held = make(map[string]chan struct{})
		}
		busy, taken := k.held[key]
		if !taken {
			released := make(chan struct{})
			k.held[key] = released
			k.mu.Unlock()
			return func() {
				k.mu.Lock()
				delete(k.held, key)
				k.mu.Unlock()
				close(released)
			}, nil
		}
		k.mu.Unlock()
		if onWait != nil {
			onWait()
			onWait = nil
		}
		select {
		case <-busy:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}
