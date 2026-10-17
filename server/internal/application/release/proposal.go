package release

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// rollbackMaxAge is how long after it finished a released release may still
// be rolled back.
const rollbackMaxAge = 24 * time.Hour

const (
	attributedIncidentTTL     = 24 * time.Hour
	attributedIncidentSweepAt = 256
	proposalClearLookback     = 50
)

type attributedIncident struct {
	id uuid.UUID
	at time.Time
}

// Annotate fills what a release's rollback would answer right now
// (RollbackAllowed, RollbackRefusal).
func (s *Service) Annotate(ctx context.Context, r domain.Release) domain.Release {
	out := s.annotateAll(ctx, []domain.Release{r})
	return out[0]
}

type componentKey struct {
	repository uuid.UUID
	component  uuid.UUID
}

func keyOf(r domain.Release) componentKey {
	k := componentKey{repository: r.RepositoryID}
	if r.ComponentID != nil {
		k.component = *r.ComponentID
	}
	return k
}

// annotateAll reads the store once per component the releases belong to,
// however many of its releases are listed.
func (s *Service) annotateAll(ctx context.Context, releases []domain.Release) []domain.Release {
	now := s.now()
	facts := map[componentKey]rollbackFacts{}
	for i := range releases {
		r := &releases[i]
		var f rollbackFacts
		if needsRollbackFacts(*r, now) {
			k := keyOf(*r)
			cached, ok := facts[k]
			if !ok {
				cached = s.rollbackFactsFor(ctx, r.RepositoryID, r.ComponentID)
				facts[k] = cached
			}
			f = cached
		}
		r.RollbackRefusal, _ = s.rollbackRefusal(*r, f)
		r.RollbackAllowed = r.RollbackRefusal == ""
	}
	return releases
}

// DismissRollbackProposal is a human declining the release engineer's
// proposed rollback. The dismissal is kept, with the incident the proposal
// answered, so the release engineer does not propose it again until another
// incident is attributed to the release. A release without a proposal, or
// whose proposal was replaced while this ran, is answered as it is.
func (s *Service) DismissRollbackProposal(ctx context.Context, releaseID uuid.UUID) (domain.Release, error) {
	r, err := s.store.Get(ctx, releaseID)
	if err != nil {
		return domain.Release{}, err
	}
	if p := r.RollbackProposal; p != nil {
		d := domain.RollbackDismissal{DismissedAt: s.now().UTC(), IncidentID: p.IncidentID}
		ok, err := s.store.DismissRollbackProposal(ctx, r.ID, p.ProposedAt, d)
		if err != nil {
			return domain.Release{}, err
		}
		if !ok {
			if r, err = s.store.Get(ctx, releaseID); err != nil {
				return domain.Release{}, err
			}
			return s.Annotate(ctx, r), nil
		}
		r.RollbackProposal, r.RollbackDismissal = nil, &d
	}
	return s.Annotate(ctx, r), nil
}

// dismissedWithoutNewIncident reports whether a person dismissed the
// release's rollback proposal and no incident other than the one it answered
// has been attributed to the release since.
func dismissedWithoutNewIncident(d *domain.RollbackDismissal, attributed *uuid.UUID) bool {
	if d == nil {
		return false
	}
	return attributed == nil || (d.IncidentID != nil && *d.IncidentID == *attributed)
}

// clearOlderProposals drops the proposals of the component's releases older
// than shipped: once a newer release is live, rolling one of them back would
// redeploy past it.
func (s *Service) clearOlderProposals(ctx context.Context, shipped domain.Release) {
	releases, err := s.store.List(ctx, domain.ReleaseListFilter{
		RepositoryID: &shipped.RepositoryID,
		ComponentID:  shipped.ComponentID,
		Statuses:     []domain.ReleaseStatus{domain.ReleaseAwaitingVerdict, domain.ReleaseFailed, domain.ReleaseReleased},
		Limit:        proposalClearLookback,
	})
	if err != nil {
		log.Warn().Err(err).Str("release_id", shipped.ID.String()).Msg("release: listing older releases to clear their rollback proposals failed")
		return
	}
	for _, r := range releases {
		if r.ID == shipped.ID || r.RollbackProposal == nil || keyOf(r) != keyOf(shipped) || !r.CreatedAt.Before(shipped.CreatedAt) {
			continue
		}
		if err := s.store.SetRollbackProposal(ctx, r.ID, nil); err != nil {
			log.Warn().Err(err).Str("release_id", r.ID.String()).Msg("release: clearing a superseded rollback proposal failed")
		}
	}
}

func (s *Service) rememberIncident(releaseID, incidentID uuid.UUID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if len(s.attributed) > attributedIncidentSweepAt {
		for id, a := range s.attributed {
			if now.Sub(a.at) > attributedIncidentTTL {
				delete(s.attributed, id)
			}
		}
	}
	s.attributed[releaseID] = attributedIncident{id: incidentID, at: now}
}

// attributedIncident is the incident this process last attributed to the
// release, within a day of it.
func (s *Service) attributedIncident(releaseID uuid.UUID) *uuid.UUID {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.attributed[releaseID]
	if !ok || s.now().Sub(a.at) > attributedIncidentTTL {
		return nil
	}
	id := a.id
	return &id
}
