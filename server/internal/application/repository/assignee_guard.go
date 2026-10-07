package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// roleHolders is the optional half of the RoleResolver: workflow.Service has
// it, the narrower resolvers other packages' tests hand in do not, and those
// leave every agent assignable.
type roleHolders interface {
	AgentHoldsRole(ctx context.Context, agentID uuid.UUID, roleKey string) (bool, error)
}

func (s *Service) isProductManager(ctx context.Context, agentID uuid.UUID) (bool, error) {
	holders, ok := s.roles.(roleHolders)
	if !ok {
		return false, nil
	}
	held, err := holders.AgentHoldsRole(ctx, agentID, domain.RoleKeyProductManager)
	if err != nil {
		return false, fmt.Errorf("check assignee role: %w", err)
	}
	return held, nil
}

func (s *Service) requireAssignable(ctx context.Context, agentID *uuid.UUID) error {
	if agentID == nil {
		return nil
	}
	pm, err := s.isProductManager(ctx, *agentID)
	if err != nil {
		return err
	}
	if pm {
		return domain.ErrAssigneeNotAssignable
	}
	return nil
}

// requestedAssignee vets CreateTask's requested assignee. A system-opened task
// takes its assignee from a role purpose, which is configuration rather than a
// choice anybody made, so a PM there leaves the task unassigned instead of
// failing the flow that opened it.
func (s *Service) requestedAssignee(ctx context.Context, req domain.CreateBoardTaskRequest) (*uuid.UUID, error) {
	err := s.requireAssignable(ctx, req.AssigneeAgentID)
	if err == nil {
		return req.AssigneeAgentID, nil
	}
	if req.CreatedBy == "system" && errors.Is(err, domain.ErrAssigneeNotAssignable) {
		return nil, nil
	}
	return nil, err
}
