package release

import (
	"fmt"

	"context"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// Deploy triggers a pending release's deploy: a dispatch-mode release's deploy
// workflow at the release tag, or (batch) whichever of the three cut
// executors the profile names. on_merge releases have no deploy step: the
// merge itself deployed.
func (s *Service) Deploy(ctx context.Context, releaseID uuid.UUID, actor domain.ReleaseActor) (domain.Release, error) {
	r, err := s.store.Get(ctx, releaseID)
	if err != nil {
		return domain.Release{}, err
	}
	if r.Status != domain.ReleasePending {
		return domain.Release{}, fmt.Errorf("%w: deploy_release only applies to a pending release (this one is %s)",
			domain.ErrReleaseWrongStatus, r.Status)
	}
	switch r.Mode {
	case domain.DeliveryDispatch:
		if pending := s.pendingDeployDependencies(ctx, r.TaskIDs()); len(pending) > 0 {
			if len(r.Tasks) > 0 {
				s.commentDeployDependencies(ctx, r.RepositoryID, r.Tasks[len(r.Tasks)-1].ID, pending)
			}
			return domain.Release{}, deployDependencyError(pending)
		}
		if gateErr := s.beforeDeployGate(ctx, r); gateErr != nil {
			return domain.Release{}, gateErr
		}
		return s.deployDispatch(ctx, r, actor)
	case domain.DeliveryBatch:
		return s.deployBatch(ctx, r, actor)
	default:
		return domain.Release{}, fmt.Errorf("%w: %s releases have no deploy step to trigger — the merge (or its own executor) deploys them",
			domain.ErrReleaseNoDeploy, r.Mode)
	}
}

// deployDispatch claims the release (pending -> deploying, DeployStartedAt)
// BEFORE tagging/dispatching: a second concurrent Deploy call then reads the
// claim's conditional Update fail with ErrReleaseWrongStatus and does
// nothing, instead of both calls racing to dispatch the same workflow twice.
// A dispatch failure after the claim is recorded as failed (expect=deploying)
// rather than left silently pending — see failClaimed.
func (s *Service) deployDispatch(ctx context.Context, r domain.Release, actor domain.ReleaseActor) (domain.Release, error) {
	repo, err := s.repo(ctx, r.RepositoryID)
	if err != nil {
		return domain.Release{}, err
	}

	now := s.now()
	claim := r
	claim.Status = domain.ReleaseDeploying
	claim.Tag = domain.ReleaseTagForCommit(r.CommitSHA)
	claim.DeployStartedAt = &now
	claimed, err := s.store.Update(ctx, claim, domain.ReleasePending)
	if err != nil {
		return domain.Release{}, err
	}

	_, ciUnavailable, dispatchErr := s.createAndDispatch(ctx, repo, claimed.Profile.Workflow, claimed.CommitSHA)
	if dispatchErr != nil {
		reason := dispatchErr.Error()
		if ciUnavailable {
			reason = "the deploy dispatch was refused: " + reason
		}
		return s.failClaimed(ctx, claimed, reason)
	}
	_ = actor
	return claimed, nil
}

// deployBatchGitHubActions tags the cut commit and stops: the repository's own
// tag-triggered workflow does the build/publish, and a batch release never
// dispatches. Unlike dispatch mode, a tag that already exists is refused
// rather than treated as success — a batch version is picked once at cut time
// and must not be silently re-used for a different commit.
func (s *Service) deployBatchGitHubActions(ctx context.Context, r domain.Release) (domain.Release, error) {
	repo, err := s.repo(ctx, r.RepositoryID)
	if err != nil {
		return domain.Release{}, err
	}
	if s.actions == nil || s.coords == nil {
		return domain.Release{}, fmt.Errorf("this deployment has no GitHub Actions access configured")
	}
	owner, name, err := s.coords(ctx, repo)
	if err != nil {
		return domain.Release{}, fmt.Errorf("resolving the repository's GitHub owner/name: %w", err)
	}
	if terr := s.actions.CreateTag(ctx, owner, name, r.Tag, r.CommitSHA); terr != nil {
		if s.refAlreadyExists(terr) {
			// Deliberately left pending (not claimed/failed): the version was
			// never usable, nothing was deployed, and Cut() accepts a pending
			// release with no DeployStartedAt back for exactly this — the human
			// picks a different version instead of the release being stuck.
			return domain.Release{}, fmt.Errorf("%w: %s already exists — re-cut this release with a new version", domain.ErrReleaseTagExists, r.Tag)
		}
		return domain.Release{}, fmt.Errorf("tag the release commit: %w", terr)
	}

	now := s.now()
	r.Status = domain.ReleaseDeploying
	r.DeployStartedAt = &now
	return s.store.Update(ctx, r, domain.ReleasePending)
}

// failClaimed records a deploy-side-effect failure on a release this call
// already claimed into deploying (DeployStartedAt set): the release is a
// normal, informative "failed" instead of returning an API error while
// silently leaving it stuck deploying forever.
func (s *Service) failClaimed(ctx context.Context, r domain.Release, reason string) (domain.Release, error) {
	r.Status = domain.ReleaseFailed
	r.FailureReason = reason
	updated, err := s.store.Update(ctx, r, domain.ReleaseDeploying)
	if err != nil {
		return domain.Release{}, err
	}
	s.handBack(ctx, updated)
	return updated, nil
}

// createAndDispatch tags the commit (an "already exists" answer counts as
// success — the tag names the commit, not the attempt) and dispatches the
// workflow at that tag.
func (s *Service) createAndDispatch(ctx context.Context, repo domain.Repository, workflow, sha string) (tag string, ciUnavailable bool, err error) {
	if s.actions == nil || s.coords == nil {
		return "", false, fmt.Errorf("this deployment has no GitHub Actions access configured")
	}
	owner, name, err := s.coords(ctx, repo)
	if err != nil {
		return "", false, fmt.Errorf("resolving the repository's GitHub owner/name: %w", err)
	}
	tag = domain.ReleaseTagForCommit(sha)
	if terr := s.actions.CreateTag(ctx, owner, name, tag, sha); terr != nil && !s.refAlreadyExists(terr) {
		return tag, s.ciUnavailable(terr.Error()), fmt.Errorf("tag the release commit: %w", terr)
	}
	if derr := s.actions.DispatchWorkflow(ctx, owner, name, workflow, tag); derr != nil {
		return tag, s.ciUnavailable(derr.Error()), fmt.Errorf("dispatch %s: %w", workflow, derr)
	}
	return tag, false, nil
}

func (s *Service) repo(ctx context.Context, repositoryID uuid.UUID) (domain.Repository, error) {
	if s.repos == nil {
		return domain.Repository{}, fmt.Errorf("no repository resolver is configured")
	}
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		log.Warn().Err(err).Str("repository_id", repositoryID.String()).Msg("release: loading the repository failed")
	}
	return repo, err
}
