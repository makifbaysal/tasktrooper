package usage

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestMeter_RecordsNonEmbeddingIntoContextTotals(t *testing.T) {
	store := &fakeUsageStore{}
	m := NewMeter(store)
	ctx, tokens := ContextWithTokenUsage(context.Background())

	m.Record(ctx, domain.LLMUsageRecord{
		Kind: domain.LLMUsageKindCLI, Model: "opus", PromptTokens: 100, CompletionTokens: 20,
	})

	totals := tokens.Totals()
	require.Equal(t, 1, totals.LLMCalls)
	require.Equal(t, int64(100), totals.PromptTokens)
	require.Equal(t, int64(20), totals.CompletionTokens)

	require.Eventually(t, func() bool { return len(store.snapshot()) == 1 }, time.Second, 5*time.Millisecond)
}

func TestMeter_EmbeddingNotAddedToContext(t *testing.T) {
	store := &fakeUsageStore{}
	m := NewMeter(store)
	ctx, tokens := ContextWithTokenUsage(context.Background())

	m.Record(ctx, domain.LLMUsageRecord{
		Kind: domain.LLMUsageKindEmbedding, Model: "text-embedding-3-small", PromptTokens: 50,
	})

	require.Eventually(t, func() bool { return len(store.snapshot()) == 1 }, time.Second, 5*time.Millisecond)
	require.Zero(t, tokens.Totals().LLMCalls, "an embedding call must never accrue against the run's context totals")
}

func TestMeter_NilMeterStillAddsToContextAndDoesNotPanic(t *testing.T) {
	var m *Meter
	ctx, tokens := ContextWithTokenUsage(context.Background())

	require.NotPanics(t, func() {
		m.Record(ctx, domain.LLMUsageRecord{Kind: domain.LLMUsageKindCLI, PromptTokens: 10, CompletionTokens: 5})
	})
	require.Equal(t, int64(10), tokens.Totals().PromptTokens)
}

func TestMeter_NilStoreStillAddsToContextAndDoesNotPanic(t *testing.T) {
	m := NewMeter(nil)
	ctx, tokens := ContextWithTokenUsage(context.Background())

	require.NotPanics(t, func() {
		m.Record(ctx, domain.LLMUsageRecord{Kind: domain.LLMUsageKindAPI, PromptTokens: 10, CompletionTokens: 5})
	})
	require.Equal(t, int64(10), tokens.Totals().PromptTokens)
}

func TestMeter_ZeroTokenCallsNotPersisted(t *testing.T) {
	store := &fakeUsageStore{}
	m := NewMeter(store)
	ctx := context.Background()

	m.Record(ctx, domain.LLMUsageRecord{Kind: domain.LLMUsageKindAPI, Model: "m1"})

	time.Sleep(20 * time.Millisecond)
	require.Empty(t, store.snapshot())
}

func TestMeter_FlagsCLIRowsOfARunOnTheDefaultModel(t *testing.T) {
	store := &fakeUsageStore{}
	m := NewMeter(store)

	m.Record(WithDefaultModel(context.Background()), domain.LLMUsageRecord{
		Kind: domain.LLMUsageKindCLI, Model: "claude-sonnet-5", PromptTokens: 10, CompletionTokens: 5,
	})
	m.Record(WithDefaultModel(context.Background()), domain.LLMUsageRecord{
		Kind: domain.LLMUsageKindAPI, Model: "gpt-4o", PromptTokens: 10, CompletionTokens: 5,
	})
	m.Record(context.Background(), domain.LLMUsageRecord{
		Kind: domain.LLMUsageKindCLI, Model: "opus", PromptTokens: 10, CompletionTokens: 5,
	})

	require.Eventually(t, func() bool { return len(store.snapshot()) == 3 }, time.Second, 5*time.Millisecond)
	byModel := map[string]bool{}
	for _, r := range store.snapshot() {
		byModel[r.Model] = r.ModelFromDefault
	}
	require.True(t, byModel["claude-sonnet-5"])
	require.False(t, byModel["gpt-4o"], "only CLI sessions are badged")
	require.False(t, byModel["opus"])
}
