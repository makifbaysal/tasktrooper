package cloud

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func validDeployEnvironment(env domain.DeployEnvironment) bool {
	switch env {
	case domain.EnvironmentProduction, domain.EnvironmentStaging, domain.EnvironmentPreview, domain.EnvironmentDevelopment:
		return true
	}
	return false
}

func (s *Service) ListEnvironments(ctx context.Context, repoID uuid.UUID) ([]domain.ComponentEnvironment, error) {
	envs, err := s.environments.ListEnvironments(ctx, repoID)
	if err != nil {
		return nil, err
	}
	if envs == nil {
		envs = []domain.ComponentEnvironment{}
	}
	return envs, nil
}

// resourceURL looks up a specific resource's URL from the account's (cached)
// listing, so BindEnvironment can default URL without the caller having to
// paste it in from the resource picker.
func (s *Service) resourceURL(ctx context.Context, accountID uuid.UUID, ref domain.CloudResourceRef) string {
	resources, err := s.ListResources(ctx, accountID, false)
	if err != nil {
		return ""
	}
	for _, r := range resources {
		if r.Ref.Kind == ref.Kind && r.Ref.ID == ref.ID {
			return r.URL
		}
	}
	return ""
}

// BindEnvironment is the human's own binding: an account+resource pair, or a
// custom (no-account) address the platform can only probe, never confirmed
// automatically by MatchScan.
func (s *Service) BindEnvironment(ctx context.Context, componentID uuid.UUID, env domain.DeployEnvironment, req domain.SaveEnvironmentRequest) (domain.ComponentEnvironment, error) {
	if !validDeployEnvironment(env) {
		return domain.ComponentEnvironment{}, fmt.Errorf("unknown environment %q: %w", env, ErrInvalidInput)
	}
	comp, err := s.components.GetComponent(ctx, componentID)
	if err != nil {
		return domain.ComponentEnvironment{}, err
	}
	if comp.Status != domain.ComponentStatusActive {
		return domain.ComponentEnvironment{}, fmt.Errorf("component is not active: %w", ErrInvalidInput)
	}

	row := domain.ComponentEnvironment{
		ID:           uuid.New(),
		RepositoryID: comp.RepositoryID,
		ComponentID:  componentID,
		Environment:  env,
		Status:       domain.LinkConfirmed,
		Source:       domain.LinkSourceUser,
		Confidence:   domain.ConfidenceExact,
	}

	if req.AccountID != nil {
		acct, err := s.accounts.GetCloudAccount(ctx, *req.AccountID)
		if err != nil {
			return domain.ComponentEnvironment{}, err
		}
		if req.Resource == nil {
			return domain.ComponentEnvironment{}, fmt.Errorf("resource is required when account_id is set: %w", ErrInvalidInput)
		}
		row.Provider = acct.Provider
		row.AccountID = req.AccountID
		row.Resource = req.Resource
		row.URL = strings.TrimSpace(req.URL)
		if row.URL == "" {
			row.URL = s.resourceURL(ctx, *req.AccountID, *req.Resource)
		}
		row.HealthURL = strings.TrimSpace(req.HealthURL)
	} else {
		row.URL = strings.TrimSpace(req.URL)
		if row.URL == "" {
			return domain.ComponentEnvironment{}, fmt.Errorf("url is required for a custom environment: %w", ErrInvalidInput)
		}
		row.HealthURL = strings.TrimSpace(req.HealthURL)
	}

	saved, err := s.environments.SaveEnvironment(ctx, row)
	if err != nil {
		return domain.ComponentEnvironment{}, err
	}
	if err := s.project(ctx, comp.RepositoryID); err != nil {
		log.Warn().Err(err).Str("repository_id", comp.RepositoryID.String()).Msg("cloud: projecting after bind failed")
	}
	s.refreshDelivery(ctx, comp.RepositoryID)
	s.triggerRelink()
	return saved, nil
}

// EnvironmentPatch is domain-free on purpose: PatchEnvironment is a partial
// update over an existing row, not a re-derivation of one from a request type
// shaped for creation.
type EnvironmentPatch struct {
	Status    *domain.LinkStatus
	AccountID *uuid.UUID
	Resource  *domain.CloudResourceRef
}

// PatchEnvironment confirms or dismisses a suggested row, or resolves it by
// picking one of its Candidates. AutoConfirmed is always cleared: from here
// on a human decided, which is what keeps a later MatchScan from silently
// overwriting the choice (see match.go's matchableForRematch).
func (s *Service) PatchEnvironment(ctx context.Context, id uuid.UUID, patch EnvironmentPatch) (domain.ComponentEnvironment, error) {
	row, err := s.environments.GetEnvironment(ctx, id)
	if err != nil {
		return domain.ComponentEnvironment{}, err
	}

	if patch.AccountID != nil || patch.Resource != nil {
		if patch.AccountID == nil || patch.Resource == nil {
			return domain.ComponentEnvironment{}, fmt.Errorf("account_id and resource must be set together: %w", ErrInvalidInput)
		}
		acct, err := s.accounts.GetCloudAccount(ctx, *patch.AccountID)
		if err != nil {
			return domain.ComponentEnvironment{}, err
		}
		row.Provider = acct.Provider
		row.AccountID = patch.AccountID
		row.Resource = patch.Resource
		row.URL = s.resourceURL(ctx, *patch.AccountID, *patch.Resource)
		row.Status = domain.LinkConfirmed
		row.Candidates = nil
		row.AutoConfirmed = false
	}

	if patch.Status != nil {
		row.Status = *patch.Status
		row.AutoConfirmed = false
		if row.Status == domain.LinkDismissed {
			row.Candidates = nil
		}
	}

	saved, err := s.environments.SaveEnvironment(ctx, row)
	if err != nil {
		return domain.ComponentEnvironment{}, err
	}
	if err := s.project(ctx, saved.RepositoryID); err != nil {
		log.Warn().Err(err).Str("repository_id", saved.RepositoryID.String()).Msg("cloud: projecting after patch failed")
	}
	s.refreshDelivery(ctx, saved.RepositoryID)
	if saved.Status == domain.LinkConfirmed {
		s.triggerRelink()
	}
	return saved, nil
}

// DeleteEnvironment forgets a human-made binding outright; a scan-sourced row
// is only dismissed, so a later MatchScan never resurrects what the human
// just rejected (see match.go's matchableForRematch).
func (s *Service) DeleteEnvironment(ctx context.Context, id uuid.UUID) error {
	existing, err := s.environments.GetEnvironment(ctx, id)
	if err != nil {
		return err
	}
	if existing.Source == domain.LinkSourceUser {
		if err := s.environments.DeleteEnvironment(ctx, id); err != nil {
			return err
		}
	} else {
		existing.Status = domain.LinkDismissed
		existing.Candidates = nil
		if _, err := s.environments.SaveEnvironment(ctx, existing); err != nil {
			return err
		}
	}
	if err := s.project(ctx, existing.RepositoryID); err != nil {
		log.Warn().Err(err).Str("repository_id", existing.RepositoryID.String()).Msg("cloud: projecting after delete failed")
	}
	s.refreshDelivery(ctx, existing.RepositoryID)
	return nil
}
