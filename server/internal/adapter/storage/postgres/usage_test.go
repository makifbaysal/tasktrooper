package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/storage/postgres"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/database"
)

// UsageSummarySuite covers the shape of the usage dashboard payload: the
// generation/embedding kind split, the by_model grouping and ordering, and
// bucketing days in the caller's own timezone rather than the server's.
type UsageSummarySuite struct {
	suite.Suite
	ctx    context.Context
	cancel context.CancelFunc
	pg     *database.Embedded
	pool   *pgxpool.Pool
	db     *postgres.DB
	store  *postgres.LLMUsageStore
}

func TestUsageSummarySuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(UsageSummarySuite))
}

func (s *UsageSummarySuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 3*time.Minute)
	pg, err := newTestDatabase(s.ctx)
	s.Require().NoError(err)
	s.pg = pg
	pool, err := pgxpool.New(s.ctx, pg.DSN())
	s.Require().NoError(err)
	s.pool = pool
	s.db = postgres.NewDB(pool)
	s.store = postgres.NewLLMUsageStore(s.db)
}

func (s *UsageSummarySuite) TearDownSuite() {
	if s.pool != nil {
		s.pool.Close()
	}
	if s.pg != nil {
		_ = s.pg.Stop()
	}
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *UsageSummarySuite) SetupTest() {
	_, err := s.db.Exec(s.ctx, `DELETE FROM llm_usage`)
	s.Require().NoError(err)
}

func (s *UsageSummarySuite) insert(kind domain.LLMUsageKind, provider, model string, prompt, completion int, at time.Time) {
	s.Require().NoError(s.store.Record(s.ctx, domain.LLMUsageRecord{
		Kind: kind, Provider: provider, Model: model, PromptTokens: prompt, CompletionTokens: completion,
	}))
	_, err := s.db.Exec(s.ctx, `
		UPDATE llm_usage SET created_at = $1
		WHERE id = (SELECT id FROM llm_usage ORDER BY created_at DESC LIMIT 1)
	`, at)
	s.Require().NoError(err)
}

func (s *UsageSummarySuite) TestEmptyReturnsEmptySlicesNotNull() {
	sum, err := s.store.Summary(s.ctx, time.Now().Add(-24*time.Hour), time.UTC)
	s.Require().NoError(err)
	s.NotNil(sum.ByModel)
	s.NotNil(sum.Daily)
	s.Empty(sum.ByModel)
	s.Empty(sum.Daily)
	s.Zero(sum.Generation.Calls)
	s.Zero(sum.Embedding.Calls)
}

func (s *UsageSummarySuite) TestGenerationAndEmbeddingAreSplit() {
	now := time.Now().UTC()
	s.insert(domain.LLMUsageKindAPI, "anthropic", "claude-sonnet-5", 100, 50, now)
	s.insert(domain.LLMUsageKindCLI, "claude_code", "claude-opus-5", 2000, 300, now)
	s.insert(domain.LLMUsageKindEmbedding, "openai", "text-embedding-3-small", 40, 0, now)

	sum, err := s.store.Summary(s.ctx, now.Add(-time.Hour), time.UTC)
	s.Require().NoError(err)

	s.Equal(2, sum.Generation.Calls, "api and cli both count as generation")
	s.Equal(int64(2100), sum.Generation.PromptTokens)
	s.Equal(int64(350), sum.Generation.CompletionTokens)

	s.Equal(1, sum.Embedding.Calls)
	s.Equal(int64(40), sum.Embedding.PromptTokens)

	s.Len(sum.ByModel, 3)
	s.Equal("cli", sum.ByModel[0].Kind, "cli sorts ahead of api and embedding")
}

func (s *UsageSummarySuite) TestDailyBucketsInRequestedTimezoneOnlyCoverGeneration() {
	loc, err := time.LoadLocation("Europe/Istanbul")
	s.Require().NoError(err)

	// 22:00 UTC on the 26th is already 01:00 on the 27th in Istanbul (UTC+3).
	crossesMidnight := time.Date(2026, 9, 26, 22, 0, 0, 0, time.UTC)
	s.insert(domain.LLMUsageKindAPI, "anthropic", "claude-sonnet-5", 10, 5, crossesMidnight)
	s.insert(domain.LLMUsageKindEmbedding, "openai", "text-embedding-3-small", 999, 0, crossesMidnight)

	sum, err := s.store.Summary(s.ctx, crossesMidnight.Add(-time.Hour), loc)
	s.Require().NoError(err)

	s.Require().Len(sum.Daily, 1, "daily excludes embedding rows, and buckets by the LOCAL day, not the UTC one")
	s.Equal("2026-09-27", sum.Daily[0].Day)
	s.Equal(int64(10), sum.Daily[0].PromptTokens)
}

func (s *UsageSummarySuite) TestDefaultModelRowsAreGroupedApartAndFlagged() {
	now := time.Now().UTC()
	for _, fromDefault := range []bool{true, false} {
		s.Require().NoError(s.store.Record(s.ctx, domain.LLMUsageRecord{
			Kind: domain.LLMUsageKindCLI, Provider: "opencode", Model: "anthropic/claude-sonnet-5",
			ModelFromDefault: fromDefault, PromptTokens: 10, CompletionTokens: 5,
		}))
	}
	sum, err := s.store.Summary(s.ctx, now.Add(-time.Hour), time.UTC)
	s.Require().NoError(err)
	s.Require().Len(sum.ByModel, 2)
	flags := []bool{sum.ByModel[0].ModelFromDefault, sum.ByModel[1].ModelFromDefault}
	s.ElementsMatch([]bool{true, false}, flags)
}
