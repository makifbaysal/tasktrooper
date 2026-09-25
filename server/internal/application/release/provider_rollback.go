package release

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// providerDeploymentLookback bounds how many of the environment's deployments
// a provider-rollback target search and a promote-watch consider — the same
// count the vercel deploy-status match already uses.
const providerDeploymentLookback = 20

// providerRollbackAttempt is the outcome of trying the hosting provider's
// native rollback ahead of the pushed revert; detail is set whenever there is
// something worth recording (skipped, denied, unsupported, failed, or a
// success note), success only when RollbackEnvironment actually succeeded.
type providerRollbackAttempt struct {
	success  bool
	targetID string
	detail   string
}

// attemptProviderRollback tries the component's bound production
// environment's native rollback before the revert lands. It never fails the
// caller: every outcome (no bound env, provider lacks the capability, no
// target found, the call itself erroring) is folded into detail and the F1
// revert mechanism proceeds regardless.
func (s *Service) attemptProviderRollback(ctx context.Context, r domain.Release) providerRollbackAttempt {
	if s.environments == nil {
		return providerRollbackAttempt{}
	}
	env, ok := s.prodEnvironment(ctx, r.RepositoryID, r.ComponentID)
	if !ok {
		return providerRollbackAttempt{}
	}
	if !s.environments.CanRollback(ctx, env.ID) {
		return providerRollbackAttempt{}
	}

	target, ok := s.providerRollbackTarget(ctx, r, env.ID)
	if !ok {
		return providerRollbackAttempt{detail: "no earlier production deployment was found for a provider rollback"}
	}

	if err := s.environments.RollbackEnvironment(ctx, env.ID, target.ID); err != nil {
		return providerRollbackAttempt{detail: "provider rollback " + providerRollbackErrorDetail(err)}
	}
	return providerRollbackAttempt{
		success:  true,
		targetID: target.ID,
		detail:   fmt.Sprintf("production was rolled back to the provider's earlier deployment %s", target.ID),
	}
}

func providerRollbackErrorDetail(err error) string {
	switch {
	case errors.Is(err, port.ErrCloudWriteDenied):
		return "denied: " + err.Error()
	case errors.Is(err, port.ErrUnsupported):
		return "not supported by this provider"
	default:
		return "failed: " + err.Error()
	}
}

// providerRollbackTarget is the environment deployment the provider should
// serve again: the one whose commit matches the previous released release's
// CommitSHA (prefix match, as the sweeper's vercel status match already
// does), else the newest READY deployment created before this release's own
// deploy started.
func (s *Service) providerRollbackTarget(ctx context.Context, r domain.Release, envID uuid.UUID) (domain.CloudDeployment, bool) {
	deployments, err := s.environments.Deployments(ctx, envID, providerDeploymentLookback)
	if err != nil {
		log.Warn().Err(err).Str("release_id", r.ID.String()).Msg("release: reading deployments for a provider rollback target failed")
		return domain.CloudDeployment{}, false
	}

	if prev, err := s.store.LastReleased(ctx, r.RepositoryID, r.ComponentID, r.CreatedAt); err == nil {
		if d, ok := matchVercelDeployment(deployments, prev.CommitSHA); ok {
			return d, true
		}
	} else if !errors.Is(err, domain.ErrReleaseNotFound) {
		log.Warn().Err(err).Str("release_id", r.ID.String()).Msg("release: finding the previous released release for a provider rollback target failed")
	}

	before := s.now()
	if r.DeployStartedAt != nil {
		before = *r.DeployStartedAt
	}
	var best domain.CloudDeployment
	found := false
	for _, d := range deployments {
		if d.Status != domain.CloudDeployReady || !d.CreatedAt.Before(before) {
			continue
		}
		if !found || d.CreatedAt.After(best.CreatedAt) {
			best = d
			found = true
		}
	}
	return best, found
}

// sweepRollingBackProvider watches a provider-mechanism rollback: production
// is safe as soon as the provider serves ProviderDeploymentID again; an
// on_merge release then also has to wait for the revert push's own
// deployment and promote it, or automatic production assignment stays off.
func (s *Service) sweepRollingBackProvider(ctx context.Context, r domain.Release) {
	now := s.now()
	elapsed := now.Sub(r.Rollback.StartedAt)

	env, ok := s.prodEnvironment(ctx, r.RepositoryID, r.ComponentID)
	if !ok || s.environments == nil {
		log.Warn().Str("release_id", r.ID.String()).Msg("release sweeper: the bound production environment for a provider rollback is gone")
		if elapsed > rollbackTimeout {
			s.failRollback(ctx, r, "the bound production environment could not be resolved to verify the provider rollback")
		}
		return
	}

	current, err := s.environments.CurrentDeployment(ctx, env.ID)
	if err != nil {
		log.Warn().Err(err).Str("release_id", r.ID.String()).Msg("release sweeper: reading the provider's current deployment failed")
		if elapsed > rollbackTimeout {
			s.failRollback(ctx, r, "checking the provider's current deployment failed: "+err.Error())
		}
		return
	}
	if current.ID != r.Rollback.ProviderDeploymentID {
		if elapsed > rollbackTimeout {
			s.failRollback(ctx, r, "the provider rollback did not take effect within 30 minutes — production may still run the bad release")
		}
		return
	}

	if r.Mode != domain.DeliveryOnMerge {
		// dispatch: the provider already serves the earlier deployment; no
		// redeploy of the previous good release was made (Rollback skipped
		// it), so there is nothing left to wait for.
		s.finishRollback(ctx, r)
		return
	}
	s.sweepRollingBackPromote(ctx, r, env.ID, elapsed)
}

// sweepRollingBackPromote waits for the revert commit's own deployment to go
// READY and promotes it — on Vercel this is what re-enables automatic
// production assignment after RollbackTo pinned production to a specific
// deployment; without it every later merge would build but never go live.
func (s *Service) sweepRollingBackPromote(ctx context.Context, r domain.Release, envID uuid.UUID, elapsed time.Duration) {
	deployments, err := s.environments.Deployments(ctx, envID, providerDeploymentLookback)
	if err != nil {
		log.Warn().Err(err).Str("release_id", r.ID.String()).Msg("release sweeper: reading deployments to find the revert commit's deployment failed")
		if elapsed > rollbackTimeout {
			s.failRollback(ctx, r, "reading deployments to find the revert commit's deployment failed: "+err.Error())
		}
		return
	}
	revertDeployment, ok := matchVercelDeployment(deployments, r.Rollback.RestoredRef)
	if !ok || revertDeployment.Status != domain.CloudDeployReady {
		if elapsed > rollbackTimeout {
			s.failRollback(ctx, r, "the revert commit's deployment did not become ready within 30 minutes to promote")
		}
		return
	}

	if err := s.environments.PromoteDeployment(ctx, envID, revertDeployment.ID); err != nil {
		r.Status = domain.ReleaseFailed
		r.FailureReason = "production is SAFE (still on the earlier deployment) but automatic production assignment is off " +
			"until someone promotes a deployment in the provider's console: " + err.Error()
		updated, uerr := s.store.Update(ctx, r, domain.ReleaseRollingBack)
		if uerr != nil {
			s.logSweepUpdate(uerr, r.ID)
			return
		}
		s.handBack(ctx, updated)
		return
	}

	r.Rollback.PromotedDeploymentID = revertDeployment.ID
	s.finishRollback(ctx, r)
}
