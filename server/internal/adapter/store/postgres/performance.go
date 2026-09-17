package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type PerformanceStore struct {
	pool *DB
}

func NewPerformanceStore(pool *DB) *PerformanceStore {
	return &PerformanceStore{pool: pool}
}

func (s *PerformanceStore) GetScore(ctx context.Context, agentID uuid.UUID) (domain.AgentPerformanceScore, error) {
	var sc domain.AgentPerformanceScore
	err := s.pool.QueryRow(ctx, `
		SELECT id, agent_id, score, runs_total, runs_passed, runs_revised, updated_at
		FROM agent_performance_scores
		WHERE agent_id = $1
	`, agentID).Scan(&sc.ID, &sc.AgentID, &sc.Score, &sc.RunsTotal, &sc.RunsPassed, &sc.RunsRevised, &sc.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.AgentPerformanceScore{AgentID: agentID, Score: 100.0}, nil
	}
	if err != nil {
		return domain.AgentPerformanceScore{}, fmt.Errorf("get score: %w", err)
	}
	return sc, nil
}

func (s *PerformanceStore) ApplyDelta(ctx context.Context, input domain.ApplyScoreInput) (domain.AgentPerformanceScore, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.AgentPerformanceScore{}, err
	}
	defer tx.Rollback(ctx)

	passedDelta, revisedDelta := 0, 0
	if input.Delta > 0 {
		passedDelta = 1
	} else if input.Delta < 0 {
		revisedDelta = 1
	}

	var sc domain.AgentPerformanceScore
	err = tx.QueryRow(ctx, `
		INSERT INTO agent_performance_scores (agent_id, score, runs_total, runs_passed, runs_revised)
		VALUES ($1, GREATEST(0, 100.0 + $2::numeric), 1, $3, $4)
		ON CONFLICT (agent_id) DO UPDATE SET
			score        = GREATEST(0, agent_performance_scores.score + $2::numeric),
			runs_total   = agent_performance_scores.runs_total + 1,
			runs_passed  = agent_performance_scores.runs_passed + $3,
			runs_revised = agent_performance_scores.runs_revised + $4,
			updated_at   = now()
		RETURNING id, agent_id, score, runs_total, runs_passed, runs_revised, updated_at
	`, input.AgentID, input.Delta, passedDelta, revisedDelta).Scan(
		&sc.ID, &sc.AgentID, &sc.Score, &sc.RunsTotal, &sc.RunsPassed, &sc.RunsRevised, &sc.UpdatedAt)
	if err != nil {
		return domain.AgentPerformanceScore{}, fmt.Errorf("upsert score: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO agent_score_events (agent_id, task_id, event_type, delta, score_after, reason)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, input.AgentID, input.TaskID, input.EventType, input.Delta, sc.Score, input.Reason)
	if err != nil {
		return domain.AgentPerformanceScore{}, fmt.Errorf("insert score event: %w", err)
	}

	return sc, tx.Commit(ctx)
}

func (s *PerformanceStore) RecentEvents(ctx context.Context, agentID uuid.UUID, limit int) ([]domain.AgentScoreEvent, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, agent_id, task_id, event_type, delta, score_after, reason, created_at
		FROM agent_score_events
		WHERE agent_id = $1
		ORDER BY created_at DESC LIMIT $2
	`, agentID, limit)
	if err != nil {
		return nil, fmt.Errorf("recent events: %w", err)
	}
	defer rows.Close()
	return scanScoreEvents(rows)
}

func (s *PerformanceStore) EventsInWindow(ctx context.Context, agentID uuid.UUID, from, to time.Time) ([]domain.AgentScoreEvent, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, agent_id, task_id, event_type, delta, score_after, reason, created_at
		FROM agent_score_events
		WHERE agent_id = $1
		  AND created_at >= $2 AND created_at < $3
		ORDER BY created_at ASC
	`, agentID, from, to)
	if err != nil {
		return nil, fmt.Errorf("events in window: %w", err)
	}
	defer rows.Close()
	return scanScoreEvents(rows)
}

func (s *PerformanceStore) HasEventForTask(ctx context.Context, taskID uuid.UUID, eventType string) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM agent_score_events WHERE task_id = $1 AND event_type = $2)
	`, taskID, eventType).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("has event for task: %w", err)
	}
	return exists, nil
}

func scanScoreEvents(rows pgx.Rows) ([]domain.AgentScoreEvent, error) {
	var events []domain.AgentScoreEvent
	for rows.Next() {
		var e domain.AgentScoreEvent
		if err := rows.Scan(&e.ID, &e.AgentID, &e.TaskID, &e.EventType, &e.Delta, &e.ScoreAfter, &e.Reason, &e.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}
