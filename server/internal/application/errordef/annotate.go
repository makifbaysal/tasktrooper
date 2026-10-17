package errordef

import (
	"context"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// Annotate sets each group's definition and, unless includeIgnored, leaves
// out the groups an ignore definition matches, counting them in
// IgnoredCount. Definitions that cannot be read leave the answer as it was:
// the inbox never fails over its annotations.
func (s *Service) Annotate(ctx context.Context, in domain.OpsErrors, includeIgnored bool) domain.OpsErrors {
	if len(in.Groups) == 0 {
		return in
	}
	defs, err := s.active(ctx, repositoriesOf(in.Groups))
	if err != nil {
		log.Warn().Err(err).Msg("errordef: annotating the errors inbox failed")
		return in
	}
	out := in
	out.Groups = make([]domain.OpsErrorGroup, 0, len(in.Groups))
	for _, g := range in.Groups {
		g.Definition = nil
		if d := s.best(defs[g.RepositoryID], g); d != nil {
			g.Definition = d.Ref()
			if d.Action == domain.ErrorActionIgnore && !includeIgnored {
				out.IgnoredCount++
				continue
			}
		}
		out.Groups = append(out.Groups, g)
	}
	return out
}

// Match is the active definition a group matches, or nil.
func (s *Service) Match(ctx context.Context, g domain.OpsErrorGroup) (*domain.ErrorDefinition, error) {
	defs, err := s.active(ctx, []uuid.UUID{g.RepositoryID})
	if err != nil {
		return nil, err
	}
	return s.best(defs[g.RepositoryID], g), nil
}

func repositoriesOf(groups []domain.OpsErrorGroup) []uuid.UUID {
	seen := map[uuid.UUID]bool{}
	var out []uuid.UUID
	for _, g := range groups {
		if !seen[g.RepositoryID] {
			seen[g.RepositoryID] = true
			out = append(out, g.RepositoryID)
		}
	}
	return out
}

// best picks among the definitions a group matches: a component's own over
// a repository-wide one, then the more specific match, then the most
// recently edited.
func (s *Service) best(defs []domain.ErrorDefinition, g domain.OpsErrorGroup) *domain.ErrorDefinition {
	var f *facts
	var pick *domain.ErrorDefinition
	for i := range defs {
		d := &defs[i]
		if d.ComponentID != nil && *d.ComponentID != g.ComponentID {
			continue
		}
		if f == nil {
			v := factsOf(g)
			f = &v
		}
		if !s.regexes.matches(d.Match, *f) {
			continue
		}
		if pick == nil || better(d, pick) {
			pick = d
		}
	}
	return pick
}

func better(a, b *domain.ErrorDefinition) bool {
	if (a.ComponentID != nil) != (b.ComponentID != nil) {
		return a.ComponentID != nil
	}
	if sa, sb := a.Match.Specificity(), b.Match.Specificity(); sa != sb {
		return sa > sb
	}
	if !a.UpdatedAt.Equal(b.UpdatedAt) {
		return a.UpdatedAt.After(b.UpdatedAt)
	}
	return a.ID.String() < b.ID.String()
}
