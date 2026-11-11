package usage

import (
	"context"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// Meter is the one place every LLM call's usage lands, whatever executed it:
// the HTTP LLM client (RecordingClient) or a host CLI session (claudecode,
// core.Family). Generation calls also feed the per-run context accumulator
// that ends up on task_agent_runs; embeddings stay out of it because they are
// indexing cost, not the run's own spend. Every call with tokens is persisted
// for the usage dashboard.
type Meter struct {
	store port.LLMUsageStore
}

func NewMeter(store port.LLMUsageStore) *Meter {
	return &Meter{store: store}
}

// Record must run for every call site that used to call
// TokenUsageFromContext(ctx).Add directly, including a failed or timed-out CLI
// session — it burned tokens regardless of how the run ended. Nil-receiver
// safe so a caller holding a zero-value *Meter (an executor built without one,
// e.g. in a test) still keeps the context counters working.
func (m *Meter) Record(ctx context.Context, rec domain.LLMUsageRecord) {
	if rec.Kind != domain.LLMUsageKindEmbedding {
		TokenUsageFromContext(ctx).Add(domain.Usage{
			PromptTokens:     rec.PromptTokens,
			CompletionTokens: rec.CompletionTokens,
			CacheReadTokens:  rec.CacheReadTokens,
			CacheWriteTokens: rec.CacheWriteTokens,
		})
	}
	if m == nil || m.store == nil {
		return
	}
	if rec.PromptTokens == 0 && rec.CompletionTokens == 0 {
		return
	}
	if rec.Model == "" {
		rec.Model = "(default)"
	}
	if rec.Kind == domain.LLMUsageKindCLI && defaultModelFromContext(ctx) {
		rec.ModelFromDefault = true
	}
	store := m.store
	go func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), recordTimeout)
		defer cancel()
		if err := store.Record(ctx, rec); err != nil {
			log.Warn().Err(err).Msg("llm usage record failed")
		}
	}()
}

type defaultModelKey struct{}

// WithDefaultModel marks a run whose agent named no model, so the usage rows
// its CLI session records are badged as running on the default.
func WithDefaultModel(ctx context.Context) context.Context {
	return context.WithValue(ctx, defaultModelKey{}, true)
}

func defaultModelFromContext(ctx context.Context) bool {
	v, _ := ctx.Value(defaultModelKey{}).(bool)
	return v
}
