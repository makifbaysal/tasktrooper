package release

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// verifyTarget is where the soak window probes: the component's bound
// production environment, or the legacy deploy target for env "prod".
type verifyTarget struct {
	EnvironmentID *uuid.UUID
	BaseURL       string
	HealthURL     string
}

// prodEnvironment finds the component's confirmed production environment,
// the "bound production environment" both the verify target and the vercel
// deploy-status match key off.
func (s *Service) prodEnvironment(ctx context.Context, repositoryID uuid.UUID, componentID *uuid.UUID) (domain.ComponentEnvironment, bool) {
	if s.environments == nil || componentID == nil {
		return domain.ComponentEnvironment{}, false
	}
	envs, err := s.environments.ListEnvironments(ctx, repositoryID)
	if err != nil {
		log.Warn().Err(err).Str("repository_id", repositoryID.String()).Msg("release: listing environments failed")
		return domain.ComponentEnvironment{}, false
	}
	for _, e := range envs {
		if e.ComponentID == *componentID && e.Environment == domain.EnvironmentProduction && e.Status == domain.LinkConfirmed {
			return e, true
		}
	}
	return domain.ComponentEnvironment{}, false
}

// resolveVerifyTarget prefers the bound production environment, else the
// legacy deploy target for prod; gaps are recorded on notes rather than
// silently probing nothing.
func (s *Service) resolveVerifyTarget(ctx context.Context, r domain.Release) (verifyTarget, []string) {
	return s.resolveVerifyTargetFor(ctx, r.RepositoryID, r.ComponentID)
}

// resolveVerifyTargetFor is resolveVerifyTarget's resolution rule taken by
// repository/component id directly, so a caller with no domain.Release (the
// ad-hoc smoke-check test endpoint, TestSmoke) can resolve the same base URL
// a release's verify window would.
func (s *Service) resolveVerifyTargetFor(ctx context.Context, repositoryID uuid.UUID, componentID *uuid.UUID) (verifyTarget, []string) {
	var notes []string

	if env, ok := s.prodEnvironment(ctx, repositoryID, componentID); ok {
		id := env.ID
		target := verifyTarget{EnvironmentID: &id, BaseURL: env.URL, HealthURL: env.HealthURL}
		if target.HealthURL == "" {
			notes = append(notes, "no health URL — health not probed")
		}
		if target.BaseURL == "" {
			notes = append(notes, "no base URL — relative smoke paths skipped")
		}
		return target, notes
	}

	notes = append(notes, "no bound environment — runtime errors not read")

	if s.legacy != nil {
		if t, err := s.legacy.Get(ctx, repositoryID, "", domain.DeployEnvProd); err == nil {
			target := verifyTarget{BaseURL: t.BaseURL, HealthURL: t.HealthURL}
			if target.HealthURL == "" {
				notes = append(notes, "no health URL — health not probed")
			}
			if target.BaseURL == "" {
				notes = append(notes, "no base URL — relative smoke paths skipped")
			}
			return target, notes
		} else if !errors.Is(err, port.ErrNotFound) {
			log.Warn().Err(err).Str("repository_id", repositoryID.String()).Msg("release: reading the legacy deploy target failed")
		}
	}

	notes = append(notes, "no health URL — health not probed", "no base URL — relative smoke paths skipped")
	return verifyTarget{}, notes
}

// ProductionBaseURL resolves the same production base URL TestSmoke would
// probe against, without needing any checks to run yet — the smoke-check
// generator's "what host is this" question, asked before it has proposed
// anything to test.
func (s *Service) ProductionBaseURL(ctx context.Context, componentID uuid.UUID) (string, error) {
	if s.components == nil {
		return "", port.ErrNotFound
	}
	comp, err := s.components.GetComponent(ctx, componentID)
	if err != nil {
		return "", err
	}
	target, _ := s.resolveVerifyTargetFor(ctx, comp.RepositoryID, &comp.ID)
	return target.BaseURL, nil
}

// boundVercelEnvironment is the stricter condition the vercel deploy-status
// match needs: a bound production environment with an account and resource,
// so cloud.Deployments can actually be called.
func (s *Service) boundVercelEnvironment(ctx context.Context, repositoryID uuid.UUID, componentID *uuid.UUID) (domain.ComponentEnvironment, bool) {
	env, ok := s.prodEnvironment(ctx, repositoryID, componentID)
	if !ok || env.AccountID == nil || env.Resource == nil {
		return domain.ComponentEnvironment{}, false
	}
	return env, true
}
