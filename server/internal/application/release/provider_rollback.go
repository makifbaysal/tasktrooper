package release

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
// deploy started. Only production-target deployments are considered —
// a preview build, or a deployment of a commit this very release carries, is
// never a valid rollback target even if it happens to be the newest READY
// one before the release deployed.
func (s *Service) providerRollbackTarget(ctx context.Context, r domain.Release, envID uuid.UUID) (domain.CloudDeployment, bool) {
	deployments, err := s.environments.Deployments(ctx, envID, providerDeploymentLookback)
	if err != nil {
		log.Warn().Err(err).Str("release_id", r.ID.String()).Msg("release: reading deployments for a provider rollback target failed")
		return domain.CloudDeployment{}, false
	}
	deployments = productionDeployments(deployments)
	excluded := releaseCommitSHAs(r)

	if prev, err := s.store.LastReleased(ctx, r.RepositoryID, r.ComponentID, r.CreatedAt); err == nil {
		if d, ok := matchVercelDeployment(deployments, prev.CommitSHA); ok && !inReleaseCommits(d.CommitSHA, excluded) {
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
		if inReleaseCommits(d.CommitSHA, excluded) {
			continue
		}
		if !found || d.CreatedAt.After(best.CreatedAt) {
			best = d
			found = true
		}
	}
	return best, found
}

// productionDeployments keeps only deployments the provider itself marked as
// serving production — a provider rollback target must never be a preview
// build the provider happened to list alongside production ones.
func productionDeployments(deployments []domain.CloudDeployment) []domain.CloudDeployment {
	out := make([]domain.CloudDeployment, 0, len(deployments))
	for _, d := range deployments {
		if d.Environment == domain.EnvironmentProduction {
			out = append(out, d)
		}
	}
	return out
}

// releaseCommitSHAs collects every commit the release itself carries (its own
// CommitSHA plus every task's merge commit), so a provider rollback target is
// never mistaken for "the previous good release" just because a deployment
// exists for one of the release's own (bad) commits.
func releaseCommitSHAs(r domain.Release) []string {
	shas := make([]string, 0, len(r.Tasks)+1)
	if sha := strings.TrimSpace(r.CommitSHA); sha != "" {
		shas = append(shas, sha)
	}
	for _, t := range r.Tasks {
		if sha := strings.TrimSpace(t.MergeCommitSHA); sha != "" {
			shas = append(shas, sha)
		}
	}
	return shas
}

func inReleaseCommits(deploymentSHA string, releaseSHAs []string) bool {
	deploymentSHA = strings.TrimSpace(deploymentSHA)
	if deploymentSHA == "" {
		return false
	}
	for _, sha := range releaseSHAs {
		if commitPrefixMatch(deploymentSHA, sha) {
			return true
		}
	}
	return false
}

// sweepRollingBackProvider watches a provider-mechanism rollback. RollbackTo
// having returned success (recorded when Mechanism was set to
// RollbackMechanismProvider) IS the confirmation that production is
// restored — Vercel has no reliable read of "which deployment production
// serves right now" (readySubstate=PROMOTED only means "has ever taken
// production traffic", a history flag), so nothing here re-checks it.
// Dispatch has nothing left to wait for: no redeploy of the previous good
// release was made (Rollback skipped it once the provider succeeded). An
// on_merge release still has to get the revert's own deployment live and
// promoted, or automatic production assignment stays off.
func (s *Service) sweepRollingBackProvider(ctx context.Context, r domain.Release) {
	if r.Mode != domain.DeliveryOnMerge {
		s.finishRollback(ctx, r)
		return
	}

	elapsed := s.now().Sub(r.Rollback.StartedAt)
	env, ok := s.prodEnvironment(ctx, r.RepositoryID, r.ComponentID)
	if !ok {
		log.Warn().Str("release_id", r.ID.String()).Msg("release sweeper: the bound production environment for a provider rollback is gone")
		if elapsed > rollbackTimeout {
			s.failRollbackSafe(ctx, r, "production is SAFE on the earlier deployment, but the bound production environment could not be resolved to promote the revert")
		}
		return
	}
	s.sweepRollingBackPromote(ctx, r, env.ID, elapsed)
}

// failRollbackSafe fails a rollback whose provider mechanism already
// confirmed production restored — unlike failRollback, the message must
// never say production "may still run the bad release": that provider
// success is exactly what makes it not true here.
func (s *Service) failRollbackSafe(ctx context.Context, r domain.Release, reason string) {
	r.Status = domain.ReleaseFailed
	r.FailureReason = reason
	updated, err := s.store.Update(ctx, r, domain.ReleaseRollingBack)
	if err != nil {
		s.logSweepUpdate(err, r.ID)
		return
	}
	s.handBack(ctx, updated)
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
			s.failRollbackSafe(ctx, r, "production is SAFE on the earlier deployment, but reading deployments to find the revert failed: "+err.Error())
		}
		return
	}
	revertDeployment, ok := s.newestRestoringDeployment(ctx, r, deployments)
	if !ok {
		if elapsed > rollbackTimeout {
			s.failRollbackSafe(ctx, r, "production is SAFE on the earlier deployment, but the revert has not deployed — "+
				"automatic production assignment stays off until a deployment is promoted in the provider's console")
		}
		return
	}

	if err := s.environments.PromoteDeployment(ctx, envID, revertDeployment.ID); err != nil {
		s.failRollbackSafe(ctx, r, "production is SAFE (still on the earlier deployment) but automatic production assignment is off "+
			"until someone promotes a deployment in the provider's console: "+err.Error())
		return
	}

	r.Rollback.PromotedDeploymentID = revertDeployment.ID
	s.finishRollback(ctx, r)
}

// unresolvedProviderPin finds a rollback of the same component that pinned
// production to an earlier deployment and has not promoted anything since:
// until it does, a newer deployment can be READY without ever serving.
func (s *Service) unresolvedProviderPin(ctx context.Context, r domain.Release) (domain.Release, bool) {
	if r.ComponentID == nil {
		return domain.Release{}, false
	}
	releases, err := s.store.List(ctx, domain.ReleaseListFilter{
		RepositoryID: &r.RepositoryID,
		ComponentID:  r.ComponentID,
		Statuses:     []domain.ReleaseStatus{domain.ReleaseRollingBack, domain.ReleaseFailed},
		Limit:        20,
	})
	if err != nil {
		log.Warn().Err(err).Str("release_id", r.ID.String()).Msg("release sweeper: checking for a provider pin failed")
		return domain.Release{}, false
	}
	for _, other := range releases {
		if other.ID == r.ID || other.Rollback == nil {
			continue
		}
		if other.Rollback.Mechanism == domain.RollbackMechanismProvider && other.Rollback.PromotedDeploymentID == "" {
			return other, true
		}
	}
	return domain.Release{}, false
}

// newestRestoringDeployment is the deployment to promote after a provider
// rollback: the newest READY production deployment whose commit is the
// revert or builds on it. Promoting exactly the revert would put an older
// build live than a merge that landed while the rollback was running.
func (s *Service) newestRestoringDeployment(ctx context.Context, r domain.Release, deployments []domain.CloudDeployment) (domain.CloudDeployment, bool) {
	revert := strings.TrimSpace(r.Rollback.RestoredRef)
	var root string
	if s.git != nil {
		if repo, err := s.repo(ctx, r.RepositoryID); err == nil {
			root = repo.RootPath
		}
	}
	var best domain.CloudDeployment
	found := false
	for _, d := range deployments {
		if d.Status != domain.CloudDeployReady || (d.Environment != "" && d.Environment != domain.EnvironmentProduction) {
			continue
		}
		commit := strings.TrimSpace(d.CommitSHA)
		restores := commitPrefixMatch(commit, revert)
		if !restores && root != "" && commit != "" {
			if ok, err := s.git.IsAncestor(ctx, root, revert, commit); err == nil && ok {
				restores = true
			}
		}
		if restores && (!found || d.CreatedAt.After(best.CreatedAt)) {
			best, found = d, true
		}
	}
	return best, found
}
