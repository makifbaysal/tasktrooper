package release

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// ProposeCut records the version and notes an agent prepared for a draft
// batch release. It tags, builds and ships nothing: a human reviews the
// proposal and cuts the release, or dismisses it.
func (s *Service) ProposeCut(ctx context.Context, releaseID uuid.UUID, actor domain.ReleaseActor, req domain.ReleaseCutRequest) (domain.Release, error) {
	r, err := s.store.Get(ctx, releaseID)
	if err != nil {
		return domain.Release{}, err
	}
	if r.Status != domain.ReleaseDraft {
		return domain.Release{}, fmt.Errorf("%w: a cut can only be proposed for a draft release (this one is %s)",
			domain.ErrReleaseWrongStatus, r.Status)
	}
	if len(r.Tasks) == 0 {
		return domain.Release{}, domain.ErrReleaseEmpty
	}
	if _, err := s.currentBatchProfile(ctx, r); err != nil {
		return domain.Release{}, err
	}

	version := strings.TrimSpace(req.Version)
	if version == "" {
		repo, rerr := s.repo(ctx, r.RepositoryID)
		if rerr != nil {
			return domain.Release{}, rerr
		}
		version = suggestNextVersion(s.previousVersion(ctx, repo, r), r.Tasks)
	}
	if verr := domain.ValidReleaseVersion(version); verr != nil {
		return domain.Release{}, verr
	}
	notes := strings.TrimSpace(req.Notes)
	if notes == "" {
		notes = generateCutNotes(version, r.Tasks)
	}

	proposal := &domain.CutProposal{ProposedAt: s.now().UTC(), By: actor, Version: version, Notes: notes}
	if err := s.store.SetCutProposal(ctx, r.ID, proposal); err != nil {
		return domain.Release{}, err
	}
	r.CutProposal = proposal
	return s.Annotate(ctx, r), nil
}

// DismissCutProposal is a human declining a proposed cut; the draft stays
// open and keeps collecting merges.
func (s *Service) DismissCutProposal(ctx context.Context, releaseID uuid.UUID) (domain.Release, error) {
	r, err := s.store.Get(ctx, releaseID)
	if err != nil {
		return domain.Release{}, err
	}
	if r.CutProposal != nil {
		if err := s.store.SetCutProposal(ctx, r.ID, nil); err != nil {
			return domain.Release{}, err
		}
		r.CutProposal = nil
	}
	return s.Annotate(ctx, r), nil
}
