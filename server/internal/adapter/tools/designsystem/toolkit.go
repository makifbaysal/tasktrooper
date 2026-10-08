// Package designsystem exposes the design system to agents: get_design_system
// for every agent that builds UI, propose_design_system for the designer's
// design tasks.
package designsystem

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	appdesignsystem "github.com/makifbaysal/tasktrooper/server/internal/application/designsystem"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type Service interface {
	Effective(ctx context.Context, repositoryID uuid.UUID) (domain.EffectiveDesignSystem, error)
	ProjectView(ctx context.Context, projectID uuid.UUID) (appdesignsystem.ProjectView, error)
	Propose(ctx context.Context, p domain.DesignSystemProposal) (domain.DesignSystem, error)
	RequestForProject(ctx context.Context, projectID uuid.UUID, notes string) (appdesignsystem.RequestResult, error)
	RequestForRepository(ctx context.Context, repositoryID uuid.UUID, notes string) (appdesignsystem.RequestResult, error)
	Files(ctx context.Context, repositoryID uuid.UUID) ([]domain.DesignSystemFile, error)
}

// TaskReader resolves the key of the task a run is working, for the version's
// "proposed by" line.
type TaskReader interface {
	GetTask(ctx context.Context, repositoryID, taskID uuid.UUID) (domain.BoardTask, error)
}

type ToolKit struct {
	Service Service
	Tasks   TaskReader
}

func NewExecutors(k *ToolKit) []port.ToolExecutor {
	if k == nil || k.Service == nil {
		return nil
	}
	return []port.ToolExecutor{
		&getTool{kit: k},
		&proposeTool{kit: k},
		&requestTool{kit: k},
	}
}

func toolError(name, message string) domain.ToolResult {
	return domain.ToolResult{Name: name, Content: message, IsError: true}
}

func toolJSON(name string, payload any) domain.ToolResult {
	raw, err := json.Marshal(payload)
	if err != nil {
		return toolError(name, fmt.Sprintf("marshal response: %v", err))
	}
	return domain.ToolResult{Name: name, Content: string(raw), IsError: false}
}

func parseOptionalUUID(raw, field string) (*uuid.UUID, error) {
	if raw == "" {
		return nil, nil
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid %s: %w", field, err)
	}
	return &id, nil
}
