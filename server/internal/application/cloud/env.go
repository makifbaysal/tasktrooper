package cloud

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

func (s *Service) envBound(ctx context.Context, envID uuid.UUID) (runtimeBound, port.CloudEnvManager, error) {
	bound, err := s.resolveRuntimeBound(ctx, envID)
	if err != nil {
		return runtimeBound{}, nil, err
	}
	em, ok := bound.provider.(port.CloudEnvManager)
	if !ok {
		return runtimeBound{}, nil, port.ErrUnsupported
	}
	if _, err := em.EnvCapabilities(*bound.env.Resource); err != nil {
		return runtimeBound{}, nil, err
	}
	return bound, em, nil
}

// EnvCapabilities answers ErrUnsupported (or the binding's own error) for an
// environment whose variables cannot be managed — the only kind an env check
// skips.
func (s *Service) EnvCapabilities(ctx context.Context, envID uuid.UUID) (domain.EnvCapabilities, error) {
	bound, em, err := s.envBound(ctx, envID)
	if err != nil {
		return domain.EnvCapabilities{}, err
	}
	return em.EnvCapabilities(*bound.env.Resource)
}

func (s *Service) EnvVars(ctx context.Context, envID uuid.UUID) ([]domain.CloudEnvVar, error) {
	bound, em, err := s.envBound(ctx, envID)
	if err != nil {
		return nil, err
	}
	vars, err := em.ListEnvVars(ctx, bound.cred, *bound.env.Resource)
	if err != nil {
		s.noteAuthError(ctx, bound, err)
		return nil, err
	}
	return vars, nil
}

func (s *Service) WriteEnvVars(ctx context.Context, envID uuid.UUID, writes []domain.CloudEnvWrite) error {
	bound, em, err := s.envBound(ctx, envID)
	if err != nil {
		return err
	}
	if err := em.UpsertEnvVars(ctx, bound.cred, *bound.env.Resource, writes); err != nil {
		s.noteAuthError(ctx, bound, err)
		return err
	}
	return nil
}

func (s *Service) Redeploy(ctx context.Context, envID uuid.UUID) (domain.CloudDeployment, error) {
	bound, em, err := s.envBound(ctx, envID)
	if err != nil {
		return domain.CloudDeployment{}, err
	}
	d, err := em.Redeploy(ctx, bound.cred, *bound.env.Resource)
	if err != nil {
		s.noteAuthError(ctx, bound, err)
		return domain.CloudDeployment{}, err
	}
	return d, nil
}

func (s *Service) noteAuthError(ctx context.Context, bound runtimeBound, err error) {
	if errors.Is(err, port.ErrCloudAuth) && bound.env.AccountID != nil {
		s.markAccountError(ctx, *bound.env.AccountID, err)
	}
}
