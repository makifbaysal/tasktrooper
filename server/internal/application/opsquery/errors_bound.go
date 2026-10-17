package opsquery

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// maxBoundErrorGroups bounds the groups one errors.issues binding adds; a
// binding answering that many may hold more, so the answer is Truncated.
const maxBoundErrorGroups = 100

// SetBoundErrors hands the errors inbox the components' errors.issues
// bindings (a Sentry project each): their groups join the cloud
// environments' own, which stay. Without it the inbox reads the cloud only.
func (s *Service) SetBoundErrors(r port.BoundErrorReader) { s.boundErrors = r }

type boundErrors struct {
	groups      []domain.OpsErrorGroup
	unavailable []domain.OpsUnavailable
	truncated   bool
}

// readBoundErrors starts reading every selected errors.issues binding of
// the environment kind, at most MaxErrorEnvironments of them, beside the
// cloud fan-out; wait answers what they held. A binding that fails is
// unavailable, naming its data source; failing to list the bindings at all
// leaves them out of the answer, never failing it.
func (s *Service) readBoundErrors(ctx context.Context, q ErrorsQuery, since, until time.Time) (wait func() boundErrors) {
	done := make(chan boundErrors, 1)
	go func() { done <- s.boundErrorGroups(ctx, q, since, until) }()
	return func() boundErrors { return <-done }
}

func (s *Service) boundErrorGroups(ctx context.Context, q ErrorsQuery, since, until time.Time) boundErrors {
	var out boundErrors
	if s.boundErrors == nil {
		return out
	}
	bindings, err := s.boundErrors.ErrorBindings(ctx, q.Environment)
	if err != nil {
		log.Warn().Err(err).Msg("opsquery: listing the errors bindings failed; the inbox reads the cloud only")
		return out
	}
	wanted := make(map[uuid.UUID]bool, len(q.ComponentIDs))
	for _, id := range q.ComponentIDs {
		wanted[id] = true
	}
	var selected []domain.SignalBindingView
	for _, b := range bindings {
		if len(wanted) == 0 || wanted[b.ComponentID] {
			selected = append(selected, b)
		}
	}
	if len(selected) > MaxErrorEnvironments {
		selected, out.truncated = selected[:MaxErrorEnvironments], true
	}
	repos := s.repositories(ctx)
	eq := domain.ErrorsQuery{Since: since, Until: until, Limit: maxBoundErrorGroups}
	results := fanOut(ctx, len(selected), s.concurrency, s.envTimeout, func(ctx context.Context, i int) ([]domain.RuntimeErrorGroup, error) {
		return s.boundErrors.ReadErrors(ctx, selected[i].SignalBinding, eq)
	})
	for i, r := range results {
		b := selected[i]
		if r.err != nil {
			out.unavailable = append(out.unavailable, s.bindingUnavailable(b.SignalBinding, r.err))
			continue
		}
		if len(r.value) >= maxBoundErrorGroups {
			out.truncated = true
		}
		id := b.DataSourceID
		for _, g := range r.value {
			out.groups = append(out.groups, domain.OpsErrorGroup{
				ComponentID:       b.ComponentID,
				RepositoryID:      repos[b.ComponentID],
				DataSourceID:      &id,
				Provider:          domain.CloudProviderKind(b.DataSourceKind),
				RuntimeErrorGroup: g,
			})
		}
	}
	return out
}

// repositories maps each component to its repository; empty without the
// component store, or when it cannot be read.
func (s *Service) repositories(ctx context.Context) map[uuid.UUID]uuid.UUID {
	out := map[uuid.UUID]uuid.UUID{}
	if s.components == nil {
		return out
	}
	all, err := s.components.ListAllComponents(ctx)
	if err != nil {
		log.Debug().Err(err).Msg("opsquery: listing components for the errors bindings failed")
		return out
	}
	for _, c := range all {
		out[c.ID] = c.RepositoryID
	}
	return out
}
