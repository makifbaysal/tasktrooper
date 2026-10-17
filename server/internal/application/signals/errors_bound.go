package signals

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

var _ port.BoundErrorReader = (*Service)(nil)

// ErrorBindings answers the confirmed errors.issues bindings of env on a
// source whose kind lists error groups, leaving out those of dismissed
// components and of sources that are gone.
func (s *Service) ErrorBindings(ctx context.Context, env domain.DeployEnvironment) ([]domain.SignalBindingView, error) {
	if !env.Valid() {
		return nil, fmt.Errorf("unknown environment %q: %w", env, ErrInvalidInput)
	}
	bound, err := s.confirmedOf(ctx, env, domain.SignalErrorsIssues)
	if err != nil || len(bound) == 0 {
		return []domain.SignalBindingView{}, err
	}
	components, err := s.components.ListAllComponents(ctx)
	if err != nil {
		return nil, err
	}
	dismissed := map[uuid.UUID]bool{}
	for _, c := range components {
		if c.Status == domain.ComponentStatusDismissed {
			dismissed[c.ID] = true
		}
	}
	l, err := s.lookup(ctx)
	if err != nil {
		return nil, err
	}
	out := []domain.SignalBindingView{}
	for _, b := range bound {
		ds, ok := l.sources[b.DataSourceID]
		if dismissed[b.ComponentID] || !ok || !s.sources.CanReadErrors(ds.Kind) {
			continue
		}
		out = append(out, s.view(ctx, l, b))
	}
	return out, nil
}

// ReadErrors reads one errors.issues binding: its query rendered, the
// window and limit q's. A binding still missing a param is ErrInvalidInput.
func (s *Service) ReadErrors(ctx context.Context, b domain.SignalBinding, q domain.ErrorsQuery) ([]domain.RuntimeErrorGroup, error) {
	l, err := s.lookup(ctx)
	if err != nil {
		return nil, err
	}
	query, err := renderBinding(l, b)
	if err != nil {
		return nil, err
	}
	q.Query = query
	return s.reader(l).batch.Errors(ctx, b.DataSourceID, q)
}
