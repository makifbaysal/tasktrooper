package postgres

import (
	"context"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type LLMUsageStore struct {
	pool *DB
}

func NewLLMUsageStore(pool *DB) *LLMUsageStore {
	return &LLMUsageStore{pool: pool}
}

func (s *LLMUsageStore) Record(ctx context.Context, rec domain.LLMUsageRecord) error {
	kind := rec.Kind
	if kind == "" {
		kind = domain.LLMUsageKindAPI
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO llm_usage (kind, provider, model, prompt_tokens, completion_tokens, cache_read_tokens, cache_write_tokens, model_from_default)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		kind, rec.Provider, rec.Model, rec.PromptTokens, rec.CompletionTokens, rec.CacheReadTokens, rec.CacheWriteTokens, rec.ModelFromDefault)
	return err
}

// Summary buckets by kind (generation = api+cli, embedding on its own), by
// (kind, provider, model), and by local calendar day in loc — a CLI session's
// created_at is stamped when the run finished, in UTC, so "today" only means
// the browser's today once it is reinterpreted in the browser's own timezone.
func (s *LLMUsageStore) Summary(ctx context.Context, since time.Time, loc *time.Location) (domain.LLMUsageSummary, error) {
	if loc == nil {
		loc = time.UTC
	}
	sum := domain.LLMUsageSummary{
		Timezone: loc.String(),
		ByModel:  []domain.LLMUsageByModel{},
		Daily:    []domain.LLMUsageByDay{},
	}

	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FILTER (WHERE kind IN ('api', 'cli')),
		       COALESCE(SUM(prompt_tokens) FILTER (WHERE kind IN ('api', 'cli')), 0),
		       COALESCE(SUM(completion_tokens) FILTER (WHERE kind IN ('api', 'cli')), 0),
		       COALESCE(SUM(cache_read_tokens) FILTER (WHERE kind IN ('api', 'cli')), 0),
		       COALESCE(SUM(cache_write_tokens) FILTER (WHERE kind IN ('api', 'cli')), 0),
		       COUNT(*) FILTER (WHERE kind = 'embedding'),
		       COALESCE(SUM(prompt_tokens) FILTER (WHERE kind = 'embedding'), 0),
		       COALESCE(SUM(completion_tokens) FILTER (WHERE kind = 'embedding'), 0),
		       COALESCE(SUM(cache_read_tokens) FILTER (WHERE kind = 'embedding'), 0),
		       COALESCE(SUM(cache_write_tokens) FILTER (WHERE kind = 'embedding'), 0)
		FROM llm_usage
		WHERE created_at >= $1`, since).
		Scan(&sum.Generation.Calls, &sum.Generation.PromptTokens, &sum.Generation.CompletionTokens,
			&sum.Generation.CacheReadTokens, &sum.Generation.CacheWriteTokens,
			&sum.Embedding.Calls, &sum.Embedding.PromptTokens, &sum.Embedding.CompletionTokens,
			&sum.Embedding.CacheReadTokens, &sum.Embedding.CacheWriteTokens)
	if err != nil {
		return domain.LLMUsageSummary{}, err
	}

	rows, err := s.pool.Query(ctx, `
		SELECT kind, provider, model, model_from_default, COUNT(*), SUM(prompt_tokens), SUM(completion_tokens),
		       SUM(cache_read_tokens), SUM(cache_write_tokens)
		FROM llm_usage
		WHERE created_at >= $1
		GROUP BY kind, provider, model, model_from_default
		ORDER BY CASE kind WHEN 'cli' THEN 0 WHEN 'api' THEN 1 ELSE 2 END,
		         SUM(prompt_tokens + completion_tokens) DESC`, since)
	if err != nil {
		return domain.LLMUsageSummary{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var m domain.LLMUsageByModel
		if err := rows.Scan(&m.Kind, &m.Provider, &m.Model, &m.ModelFromDefault, &m.Calls, &m.PromptTokens, &m.CompletionTokens,
			&m.CacheReadTokens, &m.CacheWriteTokens); err != nil {
			return domain.LLMUsageSummary{}, err
		}
		sum.ByModel = append(sum.ByModel, m)
	}
	if err := rows.Err(); err != nil {
		return domain.LLMUsageSummary{}, err
	}

	dayRows, err := s.pool.Query(ctx, `
		SELECT to_char(date_trunc('day', created_at AT TIME ZONE $2::text), 'YYYY-MM-DD'),
		       COUNT(*), SUM(prompt_tokens), SUM(completion_tokens),
		       SUM(cache_read_tokens), SUM(cache_write_tokens)
		FROM llm_usage
		WHERE created_at >= $1 AND kind IN ('api', 'cli')
		GROUP BY 1
		ORDER BY 1`, since, loc.String())
	if err != nil {
		return domain.LLMUsageSummary{}, err
	}
	defer dayRows.Close()
	for dayRows.Next() {
		var d domain.LLMUsageByDay
		if err := dayRows.Scan(&d.Day, &d.Calls, &d.PromptTokens, &d.CompletionTokens,
			&d.CacheReadTokens, &d.CacheWriteTokens); err != nil {
			return domain.LLMUsageSummary{}, err
		}
		sum.Daily = append(sum.Daily, d)
	}
	return sum, dayRows.Err()
}
