package cloud

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	defaultHealthSweepInterval = 10 * time.Minute
	defaultHealthFirstDelay    = time.Minute
	defaultHealthRateLimit     = time.Second
)

// SetHealthIntervals overrides the sweep cadence Start uses; tests shrink
// these to milliseconds instead of waiting on the real 10-minute cadence.
// Zero values are ignored, so a test can override only the interval it cares
// about.
func (s *Service) SetHealthIntervals(sweep, firstDelay, rateLimit time.Duration) {
	if sweep > 0 {
		s.healthSweepInterval = sweep
	}
	if firstDelay > 0 {
		s.healthFirstDelay = firstDelay
	}
	if rateLimit > 0 {
		s.healthRateLimit = rateLimit
	}
}

// Start runs the background health sweep: every confirmed, bound
// environment's resource status, 24h error count and latest deployment time,
// refreshed on an interval so list views never call a provider on load. It
// returns immediately; the sweep runs under ctx until ctx is cancelled.
func (s *Service) Start(ctx context.Context) {
	go s.healthLoop(ctx)
}

func (s *Service) healthLoop(ctx context.Context) {
	timer := time.NewTimer(s.healthFirstDelay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			s.healthSweep(ctx)
			timer.Reset(s.healthSweepInterval)
		}
	}
}

// healthSweep rate-limits to one environment per healthRateLimit, so a large
// workspace never bursts a provider's API on every sweep.
func (s *Service) healthSweep(ctx context.Context) {
	envs, err := s.environments.ListAllEnvironments(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("cloud: health sweep: listing environments failed")
		return
	}
	for _, e := range envs {
		if e.Status != domain.LinkConfirmed || !e.Bound() {
			continue
		}
		s.healthCheckOne(ctx, e)
		select {
		case <-ctx.Done():
			return
		case <-time.After(s.healthRateLimit):
		}
	}
}

func (s *Service) healthCheckOne(ctx context.Context, e domain.ComponentEnvironment) {
	provider, ok := s.providerFor(e.Provider)
	if !ok {
		return
	}
	cred, err := s.accounts.CloudCredential(ctx, *e.AccountID)
	if err != nil {
		s.saveHealth(ctx, e, domain.EnvironmentHealth{Status: domain.CloudStatusUnknown, Detail: err.Error(), CheckedAt: s.now()})
		return
	}
	if e.PerBranch() {
		s.saveHealth(ctx, e, s.previewHealth(ctx, provider, cred, e))
		return
	}
	detail, err := provider.Resource(ctx, cred, *e.Resource)
	if err != nil {
		s.saveHealth(ctx, e, domain.EnvironmentHealth{Status: domain.CloudStatusUnknown, Detail: err.Error(), CheckedAt: s.now()})
		return
	}

	errCount := 0
	if s.errorsSupported(e) {
		if groups, err := s.Errors(ctx, e.ID, s.now().Add(-24*time.Hour)); err == nil {
			for _, g := range groups {
				errCount += g.Count
			}
		}
	}

	health := domain.EnvironmentHealth{Status: detail.Status, ErrorCount24h: errCount, CheckedAt: s.now(), Detail: detail.StatusDetail}
	if detail.LatestDeployment != nil {
		t := detail.LatestDeployment.CreatedAt
		health.LastDeployAt = &t
	}
	s.saveHealth(ctx, e, health)
}

// previewHealth is a per-branch environment's health: it has no address of
// its own to probe and the resource's status is production's, so it is the
// state of the newest preview build.
func (s *Service) previewHealth(ctx context.Context, provider port.CloudProvider, cred domain.CloudCredential, e domain.ComponentEnvironment) domain.EnvironmentHealth {
	deployments, err := provider.Deployments(ctx, cred, *e.Resource, e.Environment, 1)
	if err != nil {
		return domain.EnvironmentHealth{Status: domain.CloudStatusUnknown, Detail: err.Error(), CheckedAt: s.now()}
	}
	if len(deployments) == 0 {
		return domain.EnvironmentHealth{Status: domain.CloudStatusUnknown, Detail: "no preview deployment yet", CheckedAt: s.now()}
	}
	newest := deployments[0]
	createdAt := newest.CreatedAt
	return domain.EnvironmentHealth{Status: previewHealthStatus(newest.Status), LastDeployAt: &createdAt, CheckedAt: s.now()}
}

func (s *Service) saveHealth(ctx context.Context, e domain.ComponentEnvironment, h domain.EnvironmentHealth) {
	e.Health = &h
	if _, err := s.environments.SaveEnvironment(ctx, e); err != nil {
		log.Warn().Err(err).Str("environment_id", e.ID.String()).Msg("cloud: health sweep: saving health failed")
	}
}
