package release

import (
	"fmt"

	"context"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// Deploy dispatches a dispatch-mode release's deploy workflow at the release
// tag. on_merge releases have no deploy step: the merge itself deployed.
func (s *Service) Deploy(ctx context.Context, releaseID uuid.UUID, actor domain.ReleaseActor) (domain.Release, error) {
	r, err := s.store.Get(ctx, releaseID)
	if err != nil {
		return domain.Release{}, err
	}
	if r.Status != domain.ReleasePending {
		return domain.Release{}, fmt.Errorf("%w: deploy_release only applies to a pending release (this one is %s)",
			domain.ErrReleaseWrongStatus, r.Status)
	}
	if r.Mode != domain.DeliveryDispatch {
		return domain.Release{}, fmt.Errorf("%w: %s releases have no deploy step to trigger — the merge (or its own executor) deploys them",
			domain.ErrReleaseNoDeploy, r.Mode)
	}

	repo, err := s.repo(ctx, r.RepositoryID)
	if err != nil {
		return domain.Release{}, err
	}

	tag, ciUnavailable, dispatchErr := s.createAndDispatch(ctx, repo, r.Profile.Workflow, r.CommitSHA)
	if dispatchErr != nil {
		if ciUnavailable {
			return s.failPending(ctx, r, "the deploy dispatch was refused: "+dispatchErr.Error())
		}
		return domain.Release{}, dispatchErr
	}

	now := s.now()
	r.Status = domain.ReleaseDeploying
	r.Tag = tag
	r.DeployStartedAt = &now
	updated, err := s.store.Update(ctx, r, domain.ReleasePending)
	if err != nil {
		return domain.Release{}, err
	}
	_ = actor
	return updated, nil
}

func (s *Service) failPending(ctx context.Context, r domain.Release, reason string) (domain.Release, error) {
	r.Status = domain.ReleaseFailed
	r.FailureReason = reason
	updated, err := s.store.Update(ctx, r, domain.ReleasePending)
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
