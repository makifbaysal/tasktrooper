package datasource

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	DefaultErrorGroups = 100
	MaxErrorGroups     = 500
)

// Errors lists the error groups of a source whose kind implements
// port.ErrorsQuerier. A zero Limit is DefaultErrorGroups; groups past it are
// dropped.
func (b *Batch) Errors(ctx context.Context, id uuid.UUID, q domain.ErrorsQuery) ([]domain.RuntimeErrorGroup, error) {
	q.Query = strings.TrimSpace(q.Query)
	switch {
	case q.Query == "":
		return nil, FieldErrors{"query": FieldRequired}
	case len(q.Query) > maxQueryLen:
		return nil, FieldErrors{"query": FieldTooLong}
	case q.Since.IsZero() || q.Until.IsZero() || !q.Until.After(q.Since):
		return nil, FieldErrors{"since": FieldInvalid}
	case q.Limit < 0:
		return nil, FieldErrors{"limit": FieldInvalid}
	}
	if q.Limit == 0 {
		q.Limit = DefaultErrorGroups
	}
	q.Limit = min(q.Limit, MaxErrorGroups)
	l, err := b.load(ctx, id)
	if err != nil {
		return nil, err
	}
	querier, ok := l.adapter.(port.ErrorsQuerier)
	if !ok {
		return nil, fmt.Errorf("kind %q cannot list errors: %w", l.ds.Kind, ErrUnsupported)
	}
	uctx, cancel := context.WithTimeout(ctx, upstreamTimeout)
	defer cancel()
	groups, err := querier.Errors(uctx, l.ds, l.secrets, q)
	if err != nil {
		return nil, upstream(err, l.maskSet(ctx, b.svc))
	}
	if len(groups) > q.Limit {
		groups = groups[:q.Limit]
	}
	masks := l.maskSet(ctx, b.svc)
	for i := range groups {
		groups[i].Message = redact(groups[i].Message, masks)
		groups[i].Sample = redact(groups[i].Sample, masks)
		groups[i].Source = redact(groups[i].Source, masks)
	}
	return nonNilSlice(groups), nil
}

// CanReadErrors reports whether a kind lists error groups.
func (s *Service) CanReadErrors(kind domain.DataSourceKind) bool {
	_, ok := s.kinds[kind].(port.ErrorsQuerier)
	return ok
}
