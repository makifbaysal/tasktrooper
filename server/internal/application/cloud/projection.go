package cloud

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// legacyEnvFor maps the four domain.DeployEnvironment values onto the two
// legacy deploy envs that still exist as concepts (prod/stage); preview and
// development have no legacy equivalent and are never projected.
func legacyEnvFor(env domain.DeployEnvironment) string {
	switch env {
	case domain.EnvironmentProduction:
		return domain.DeployEnvProd
	case domain.EnvironmentStaging:
		return domain.DeployEnvStage
	}
	return ""
}

// legacyProviderFor is keyed off the resource kind, not the cloud provider,
// because one cloud provider (gcp) covers several legacy deploy providers
// depending on what kind of resource it is. "" means "no legacy provider
// covers this precisely", which project() reads as "leave it alone".
func legacyProviderFor(provider domain.CloudProviderKind, resource *domain.CloudResourceRef) string {
	if provider == domain.CloudVercel {
		return domain.DeployProviderVercel
	}
	if resource == nil {
		return ""
	}
	switch resource.Kind {
	case domain.CloudResourceCloudRunService, domain.CloudResourceCloudRunJob:
		return domain.DeployProviderGCPCloudRun
	case domain.CloudResourceECSService:
		return domain.DeployProviderAWSECS
	case domain.CloudResourceLambdaFunction:
		return domain.DeployProviderAWSLambda
	case domain.CloudResourceAppRunnerService:
		// No legacy AWS App Runner provider exists; custom is the closest fit.
		return domain.DeployProviderCustom
	}
	return ""
}

// project keeps repository_deploy_targets in sync with this repository's
// confirmed environments, so get_deploy_target, the prod health probe and the
// deploy watch keep working unmodified. It only ever upserts BaseURL,
// HealthURL and Provider — Vars, templates and everything else a human or an
// agent set on the legacy target survive untouched — and it never deletes a
// target: an environment being dismissed or unbound is not evidence the
// deploy stopped existing.
func (s *Service) project(ctx context.Context, repositoryID uuid.UUID) error {
	envs, err := s.environments.ListEnvironments(ctx, repositoryID)
	if err != nil {
		return fmt.Errorf("projection: list environments: %w", err)
	}
	components, err := s.components.ListComponents(ctx, repositoryID)
	if err != nil {
		return fmt.Errorf("projection: list components: %w", err)
	}

	componentByID := make(map[uuid.UUID]domain.Component, len(components))
	activeCount := 0
	for _, c := range components {
		componentByID[c.ID] = c
		if c.Status == domain.ComponentStatusActive {
			activeCount++
		}
	}

	for _, e := range envs {
		if e.Status != domain.LinkConfirmed {
			continue
		}
		legacyEnv := legacyEnvFor(e.Environment)
		if legacyEnv == "" {
			continue
		}
		comp, ok := componentByID[e.ComponentID]
		if !ok {
			continue
		}
		subProjectPath := ""
		if activeCount > 1 {
			subProjectPath = comp.Path
		}
		s.projectOne(ctx, repositoryID, subProjectPath, legacyEnv, e)
	}
	return nil
}

func (s *Service) projectOne(ctx context.Context, repositoryID uuid.UUID, subProjectPath, legacyEnv string, e domain.ComponentEnvironment) {
	target, err := s.deployTargets.Get(ctx, repositoryID, subProjectPath, legacyEnv)
	if err != nil {
		if !errors.Is(err, port.ErrNotFound) {
			log.Warn().Err(err).Str("repository_id", repositoryID.String()).Msg("cloud: projection: reading deploy target failed")
			return
		}
		target = domain.DeployTarget{RepositoryID: repositoryID, SubProjectPath: subProjectPath, Env: legacyEnv}
	}

	target.BaseURL = e.URL
	if target.HealthURL == "" {
		target.HealthURL = e.HealthURL
	}
	if provider := legacyProviderFor(e.Provider, e.Resource); provider != "" {
		target.Provider = provider
	} else if target.Provider == "" {
		target.Provider = domain.DeployProviderCustom
	}

	if _, err := s.deployTargets.Save(ctx, target); err != nil {
		log.Warn().Err(err).Str("repository_id", repositoryID.String()).Msg("cloud: projection: saving deploy target failed")
	}
}
