package datasource

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	DefaultLogLimit   = 100
	MaxLogLimit       = 1000
	DefaultTraceLimit = 20
	MaxTraceLimit     = 100
	// MaxTraceWindow is Tempo's own default search ceiling (max_duration).
	MaxTraceWindow = 7 * 24 * time.Hour
	// MaxTraceMinDuration bounds a search's minimum span duration; no trace
	// a window holds runs longer.
	MaxTraceMinDuration = 24 * time.Hour

	maxLabelValues = 5000
	maxTraceSpans  = 20000
)

// Logs reads log lines from a source whose kind implements port.LogsQuerier.
// A zero Limit is DefaultLogLimit; entries past it are dropped, Truncated.
func (b *Batch) Logs(ctx context.Context, id uuid.UUID, q domain.LogQuery) (domain.RuntimeLogPage, error) {
	q.Query = strings.TrimSpace(q.Query)
	switch {
	case q.Query == "":
		return domain.RuntimeLogPage{}, FieldErrors{"query": FieldRequired}
	case len(q.Query) > maxQueryLen:
		return domain.RuntimeLogPage{}, FieldErrors{"query": FieldTooLong}
	case q.Since.IsZero() || q.Until.IsZero() || !q.Until.After(q.Since):
		return domain.RuntimeLogPage{}, FieldErrors{"since": FieldInvalid}
	case q.Limit < 0:
		return domain.RuntimeLogPage{}, FieldErrors{"limit": FieldInvalid}
	}
	if q.Limit == 0 {
		q.Limit = DefaultLogLimit
	}
	q.Limit = min(q.Limit, MaxLogLimit)
	l, err := b.load(ctx, id)
	if err != nil {
		return domain.RuntimeLogPage{}, err
	}
	querier, ok := l.adapter.(port.LogsQuerier)
	if !ok {
		return domain.RuntimeLogPage{}, fmt.Errorf("kind %q cannot read logs: %w", l.ds.Kind, ErrUnsupported)
	}
	uctx, cancel := context.WithTimeout(ctx, upstreamTimeout)
	defer cancel()
	page, err := querier.Logs(uctx, l.ds, l.secrets, q)
	if err != nil {
		return domain.RuntimeLogPage{}, upstream(err, l.maskSet(ctx, b.svc))
	}
	if len(page.Entries) > q.Limit {
		page.Entries, page.Truncated = page.Entries[:q.Limit], true
	}
	page.Entries = nonNilSlice(page.Entries)
	return page, nil
}

// SearchTraces runs a trace search on a source whose kind implements
// port.TracesQuerier. A zero Limit is DefaultTraceLimit.
func (b *Batch) SearchTraces(ctx context.Context, id uuid.UUID, q domain.TraceQuery) ([]domain.TraceSummary, error) {
	q.Query = strings.TrimSpace(q.Query)
	switch {
	case q.Query == "":
		return nil, FieldErrors{"query": FieldRequired}
	case len(q.Query) > maxQueryLen:
		return nil, FieldErrors{"query": FieldTooLong}
	case q.Since.IsZero() || q.Until.IsZero() || !q.Until.After(q.Since):
		return nil, FieldErrors{"since": FieldInvalid}
	case q.Until.Sub(q.Since) > MaxTraceWindow:
		return nil, FieldErrors{"since": FieldTooLong}
	case q.Limit < 0:
		return nil, FieldErrors{"limit": FieldInvalid}
	case q.MinDuration < 0:
		return nil, FieldErrors{"min_duration_ms": FieldInvalid}
	}
	if q.Limit == 0 {
		q.Limit = DefaultTraceLimit
	}
	q.Limit = min(q.Limit, MaxTraceLimit)
	l, querier, err := b.traces(ctx, id)
	if err != nil {
		return nil, err
	}
	uctx, cancel := context.WithTimeout(ctx, upstreamTimeout)
	defer cancel()
	found, err := querier.SearchTraces(uctx, l.ds, l.secrets, q)
	if err != nil {
		return nil, upstream(err, l.maskSet(ctx, b.svc))
	}
	if len(found) > q.Limit {
		found = found[:q.Limit]
	}
	return nonNilSlice(found), nil
}

// Trace reads one trace; one the source does not hold is port.ErrNotFound,
// and a malformed id ErrInvalidInput, which never reaches the source.
func (b *Batch) Trace(ctx context.Context, id uuid.UUID, traceID string) (domain.Trace, error) {
	norm, ok := domain.NormalizeTraceID(traceID)
	if !ok {
		return domain.Trace{}, fmt.Errorf("trace id %q is not hex: %w", traceID, ErrInvalidInput)
	}
	l, querier, err := b.traces(ctx, id)
	if err != nil {
		return domain.Trace{}, err
	}
	uctx, cancel := context.WithTimeout(ctx, upstreamTimeout)
	defer cancel()
	t, err := querier.Trace(uctx, l.ds, l.secrets, norm)
	switch {
	case errors.Is(err, port.ErrNotFound):
		return domain.Trace{}, fmt.Errorf("trace %s: %w", norm, port.ErrNotFound)
	case err != nil:
		return domain.Trace{}, upstream(err, l.maskSet(ctx, b.svc))
	}
	if t.ID == "" {
		t.ID = norm
	}
	if len(t.Spans) > maxTraceSpans {
		t.Spans, t.Truncated = t.Spans[:maxTraceSpans], true
	}
	t.Spans = nonNilSlice(t.Spans)
	for i := range t.Spans {
		if t.Spans[i].Attributes == nil {
			t.Spans[i].Attributes = map[string]string{}
		}
	}
	return t, nil
}

func (b *Batch) traces(ctx context.Context, id uuid.UUID) (*loaded, port.TracesQuerier, error) {
	l, err := b.load(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	querier, ok := l.adapter.(port.TracesQuerier)
	if !ok {
		return nil, nil, fmt.Errorf("kind %q cannot read traces: %w", l.ds.Kind, ErrUnsupported)
	}
	return l, querier, nil
}

// LabelValues lists the values a label (field, tag) of a source holds, for
// a kind that implements port.LabelValuesLister.
func (b *Batch) LabelValues(ctx context.Context, id uuid.UUID, label string) ([]string, error) {
	l, err := b.load(ctx, id)
	if err != nil {
		return nil, err
	}
	lister, ok := l.adapter.(port.LabelValuesLister)
	if !ok {
		return nil, fmt.Errorf("kind %q cannot list label values: %w", l.ds.Kind, ErrUnsupported)
	}
	uctx, cancel := context.WithTimeout(ctx, upstreamTimeout)
	defer cancel()
	values, err := lister.LabelValues(uctx, l.ds, l.secrets, label)
	if err != nil {
		return nil, upstream(err, l.maskSet(ctx, b.svc))
	}
	if len(values) > maxLabelValues {
		values = values[:maxLabelValues]
	}
	return values, nil
}

// CanDiscover reports whether a kind lists what a source holds.
func (s *Service) CanDiscover(kind domain.DataSourceKind) bool {
	_, ok := s.kinds[kind].(port.Discoverer)
	return ok
}

// CanListLabelValues reports whether a kind lists one label's values.
func (s *Service) CanListLabelValues(kind domain.DataSourceKind) bool {
	_, ok := s.kinds[kind].(port.LabelValuesLister)
	return ok
}
