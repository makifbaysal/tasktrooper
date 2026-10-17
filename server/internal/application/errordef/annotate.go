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

// actionClass groups the actions that act on a group: an alert and an
// incident both open an incident, so they compete; a task opens a task
// beside either. none and ignore act on nothing.
type actionClass int

const (
	classNone actionClass = iota
	classIncident
	classTask
)

func classOf(a domain.ErrorAction) actionClass {
	switch {
	case a.Thresholded():
		return classIncident
	case a == domain.ErrorActionTask:
		return classTask
	}
	return classNone
}

// acting is what acts on one group: the best matching definition of each
// action class, unless a better matching ignore definition silences it. A
// none definition labels the group and silences nothing.
type acting struct {
	incident *domain.ErrorDefinition
	task     *domain.ErrorDefinition
}

func (a acting) of(c actionClass) *domain.ErrorDefinition {
	switch c {
	case classIncident:
		return a.incident
	case classTask:
		return a.task
	}
	return nil
}

func (s *Service) actingOn(defs []domain.ErrorDefinition, g domain.OpsErrorGroup) acting {
	var f *facts
	var ignore, incident, task *domain.ErrorDefinition
	for i := range defs {
		d := &defs[i]
		if d.ComponentID != nil && *d.ComponentID != g.ComponentID {
			continue
		}
		var slot **domain.ErrorDefinition
		switch {
		case d.Action == domain.ErrorActionIgnore:
			slot = &ignore
		case classOf(d.Action) == classIncident:
			slot = &incident
		case classOf(d.Action) == classTask:
			slot = &task
		default:
			continue
		}
		if f == nil {
			v := factsOf(g)
			f = &v
		}
		if !s.regexes.matches(d.Match, *f) {
			continue
		}
		if *slot == nil || better(d, *slot) {
			*slot = d
		}
	}
	out := acting{incident: incident, task: task}
	if ignore != nil {
		if out.incident != nil && !better(out.incident, ignore) {
			out.incident = nil
		}
		if out.task != nil && !better(out.task, ignore) {
			out.task = nil
		}
	}
	return out
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
