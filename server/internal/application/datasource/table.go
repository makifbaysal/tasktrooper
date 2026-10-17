package datasource

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	MaxTableRows = 500
	maxQueues    = 1000
)

// Table runs an instant table read on a source whose kind implements
// port.TableQuerier; a zero At is now. Rows past MaxTableRows are dropped
// (Truncated): where rows carry one value each (domain.TableValueKey), the
// smallest values go, so a cut never hides the busiest route or the longest
// backlog behind ones that sort first by name. A number JSON cannot carry
// (NaN, ±Inf) becomes nil.
func (b *Batch) Table(ctx context.Context, id uuid.UUID, q domain.TableQuery) (domain.TableResult, error) {
	q.Query = strings.TrimSpace(q.Query)
	switch {
	case q.Query == "":
		return domain.TableResult{}, FieldErrors{"query": FieldRequired}
	case len(q.Query) > maxQueryLen:
		return domain.TableResult{}, FieldErrors{"query": FieldTooLong}
	}
	if q.At.IsZero() {
		q.At = b.svc.now()
	}
	l, err := b.load(ctx, id)
	if err != nil {
		return domain.TableResult{}, err
	}
	querier, ok := l.adapter.(port.TableQuerier)
	if !ok {
		return domain.TableResult{}, fmt.Errorf("kind %q cannot read a table: %w", l.ds.Kind, ErrUnsupported)
	}
	uctx, cancel := context.WithTimeout(ctx, upstreamTimeout)
	defer cancel()
	res, err := querier.Table(uctx, l.ds, l.secrets, q)
	if err != nil {
		return domain.TableResult{}, upstream(err, l.maskSet(ctx, b.svc))
	}
	res.Columns = nonNilSlice(res.Columns)
	if len(res.Rows) > MaxTableRows {
		if slices.ContainsFunc(res.Rows, func(r map[string]any) bool { _, ok := r[domain.TableValueKey]; return ok }) {
			res.Rows = slices.Clone(res.Rows)
			RankByValue(res.Rows, domain.TableValueKey)
		}
		res.Rows, res.Truncated = res.Rows[:MaxTableRows], true
	}
	res.Rows = nonNilSlice(res.Rows)
	for i, row := range res.Rows {
		if row == nil {
			res.Rows[i] = map[string]any{}
			continue
		}
		for k, v := range row {
			if v != nil && finiteValue(v) == nil {
				row[k] = nil
			}
		}
	}
	return res, nil
}

// RankByValue orders rows by one column, largest first; a row without a
// finite number there goes last, and ties keep their order.
func RankByValue(rows []map[string]any, column string) {
	slices.SortStableFunc(rows, func(a, b map[string]any) int {
		fa, okA := a[column].(float64)
		fb, okB := b[column].(float64)
		okA = okA && !math.IsNaN(fa)
		okB = okB && !math.IsNaN(fb)
		switch {
		case okA && okB:
			return cmp.Compare(fb, fa)
		case okA:
			return -1
		case okB:
			return 1
		}
		return 0
	})
}

func finiteValue(v any) any {
	switch n := v.(type) {
	case float64:
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return nil
		}
	case float32:
		if math.IsNaN(float64(n)) || math.IsInf(float64(n), 0) {
			return nil
		}
	}
	return v
}

// DiscoverQueues lists the consumers a broker source holds, for a kind that
// implements port.QueueDiscoverer.
func (b *Batch) DiscoverQueues(ctx context.Context, id uuid.UUID) ([]domain.QueueTarget, error) {
	l, err := b.load(ctx, id)
	if err != nil {
		return nil, err
	}
	finder, ok := l.adapter.(port.QueueDiscoverer)
	if !ok {
		return nil, fmt.Errorf("kind %q cannot list consumers: %w", l.ds.Kind, ErrUnsupported)
	}
	uctx, cancel := context.WithTimeout(ctx, upstreamTimeout)
	defer cancel()
	targets, err := finder.DiscoverQueues(uctx, l.ds, l.secrets)
	if err != nil {
		return nil, upstream(err, l.maskSet(ctx, b.svc))
	}
	if len(targets) > maxQueues {
		targets = targets[:maxQueues]
	}
	return targets, nil
}

// CanDiscoverQueues reports whether a kind lists the consumers on its broker.
func (s *Service) CanDiscoverQueues(kind domain.DataSourceKind) bool {
	_, ok := s.kinds[kind].(port.QueueDiscoverer)
	return ok
}
