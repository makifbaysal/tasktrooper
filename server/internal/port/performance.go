package port

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type AgentPerformanceStore interface {
	GetScore(ctx context.Context, agentID uuid.UUID) (domain.AgentPerformanceScore, error)
	ApplyDelta(ctx context.Context, input domain.ApplyScoreInput) (domain.AgentPerformanceScore, error)
	RecentEvents(ctx context.Context, agentID uuid.UUID, limit int) ([]domain.AgentScoreEvent, error)
	EventsInWindow(ctx context.Context, agentID uuid.UUID, from, to time.Time) ([]domain.AgentScoreEvent, error)
	HasEventForTask(ctx context.Context, taskID uuid.UUID, eventType string) (bool, error)
}
