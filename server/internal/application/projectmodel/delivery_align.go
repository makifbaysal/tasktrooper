package projectmodel

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// AlignDeliveryToProduction implements cloud.DeliveryRefresher: when a human
// binds or confirms a component's production environment to provider, a
// delivery override that hides detection would otherwise keep pointing
// somewhere that no longer deploys anything (detection itself already
// follows the environment on the RefreshDelivery a bind triggers next).
func (s *Service) AlignDeliveryToProduction(ctx context.Context, componentID uuid.UUID, provider domain.CloudProviderKind) error {
	if provider != domain.CloudVercel {
		return nil
	}
	comp, err := s.store.GetComponent(ctx, componentID)
	if err != nil {
		return fmt.Errorf("align delivery to production: %w", err)
	}
	if comp.Status != domain.ComponentStatusActive || comp.Role.Get() == domain.ComponentRoleMobile {
		return nil
	}
	override := comp.Delivery.Override
	if override == nil {
		return nil
	}
	// A Vercel executor is already aligned; GitHub Actions deploying to
	// Vercel (a custom workflow, not the git integration) is a valid setup
	// this must not clobber.
	if override.Executor == domain.ExecutorVercel || override.Executor == domain.ExecutorGitHubActions {
		return nil
	}

	aligned := *override
	aligned.Mode = domain.DeliveryOnMerge
	aligned.Executor = domain.ExecutorVercel
	aligned.Workflow = ""
	aligned.TagPattern = ""
	aligned.LocalCommand = ""

	_, err = s.UpdateComponent(ctx, componentID, domain.ComponentPatch{
		Delivery: domain.Patch[domain.ComponentDelivery]{Set: true, Value: &aligned},
	})
	if err != nil {
		return fmt.Errorf("align delivery to production: %w", err)
	}
	return nil
}
