package cloud

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// rollbackBound narrows runtimeBound to a provider that also implements
// port.CloudRollbacker — the release service only ever needs the bound
// environment and that capability, never the plain port.CloudProvider.
func (s *Service) rollbackBound(ctx context.Context, envID uuid.UUID) (runtimeBound, port.CloudRollbacker, error) {
	bound, err := s.resolveRuntimeBound(ctx, envID)
	if err != nil {
		return runtimeBound{}, nil, err
	}
	rb, ok := bound.provider.(port.CloudRollbacker)
	if !ok {
		return runtimeBound{}, nil, port.ErrUnsupported
	}
	return bound, rb, nil
}

// CanRollback reports whether the environment's provider implements
// CloudRollbacker and the environment is bound. It swallows every resolution
// error (unbound, no credential, missing provider) into false rather than
// making the caller distinguish "cannot" from "cannot right now" — the
// release service only needs a yes/no to decide whether to try the provider
// path before falling back to the pushed revert.
func (s *Service) CanRollback(ctx context.Context, envID uuid.UUID) bool {
	_, _, err := s.rollbackBound(ctx, envID)
	return err == nil
}

func (s *Service) CurrentDeployment(ctx context.Context, envID uuid.UUID) (domain.CloudDeployment, error) {
	bound, rb, err := s.rollbackBound(ctx, envID)
	if err != nil {
		return domain.CloudDeployment{}, err
	}
	d, err := rb.Current(ctx, bound.cred, *bound.env.Resource)
	if err != nil {
		if errors.Is(err, port.ErrCloudAuth) {
			s.markAccountError(ctx, *bound.env.AccountID, err)
		}
		return domain.CloudDeployment{}, err
	}
	return d, nil
}

func (s *Service) RollbackEnvironment(ctx context.Context, envID uuid.UUID, deploymentID string) error {
	bound, rb, err := s.rollbackBound(ctx, envID)
	if err != nil {
		return err
	}
	if err := rb.RollbackTo(ctx, bound.cred, *bound.env.Resource, deploymentID); err != nil {
		if errors.Is(err, port.ErrCloudAuth) {
			s.markAccountError(ctx, *bound.env.AccountID, err)
		}
		return err
	}
	return nil
}

func (s *Service) PromoteDeployment(ctx context.Context, envID uuid.UUID, deploymentID string) error {
	bound, rb, err := s.rollbackBound(ctx, envID)
	if err != nil {
		return err
	}
	if err := rb.Promote(ctx, bound.cred, *bound.env.Resource, deploymentID); err != nil {
		if errors.Is(err, port.ErrCloudAuth) {
			s.markAccountError(ctx, *bound.env.AccountID, err)
		}
		return err
	}
	return nil
}
