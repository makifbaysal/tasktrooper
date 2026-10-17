package signals

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/datasource"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	DefaultTraceWindow = time.Hour
	// maxTraceSources bounds how many sources a trace id is looked up in
	// when no component says which one holds it, traceLookups how many at
	// once.
	maxTraceSources = 8
	traceLookups    = 4
)

var _ port.BoundLogReader = (*Service)(nil)

// LogBindings answers the confirmed logs.lines bindings of env, leaving out
// those of dismissed components, which no component picker offers.
func (s *Service) LogBindings(ctx context.Context, env domain.DeployEnvironment) ([]domain.SignalBinding, error) {
	if !env.Valid() {
		return nil, fmt.Errorf("unknown environment %q: %w", env, ErrInvalidInput)
	}
	bound, err := s.confirmedOf(ctx, env, domain.SignalLogsLines)
	if err != nil {
		return nil, err
	}
	return s.withoutDismissed(ctx, bound)
}

func (s *Service) withoutDismissed(ctx context.Context, bound []domain.SignalBinding) ([]domain.SignalBinding, error) {
	if len(bound) == 0 {
		return bound, nil
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
	out := bound[:0]
	for _, b := range bound {
		if !dismissed[b.ComponentID] {
			out = append(out, b)
		}
	}
	return out, nil
}

func (s *Service) confirmedOf(ctx context.Context, env domain.DeployEnvironment, signal string) ([]domain.SignalBinding, error) {
	all, err := s.bindings.ListConfirmedSignalBindings(ctx, env)
	if err != nil {
		return nil, err
	}
	out := []domain.SignalBinding{}
	for _, b := range all {
		if b.Signal == signal && b.Status == domain.SignalBindingConfirmed {
			out = append(out, b)
		}
	}
	return out, nil
}

// ReadLogs reads one logs binding: its query rendered, q's text and severity
// added by the source's kind. A binding still missing a param is
// ErrInvalidInput.
func (s *Service) ReadLogs(ctx context.Context, b domain.SignalBinding, q domain.LogQuery) (domain.RuntimeLogPage, error) {
	l, err := s.lookup(ctx)
	if err != nil {
		return domain.RuntimeLogPage{}, err
	}
	query, err := renderBinding(l, b)
	if err != nil {
		return domain.RuntimeLogPage{}, err
	}
	q.Query = query
	return s.reader(l).batch.Logs(ctx, b.DataSourceID, q)
}

func renderBinding(l lookup, b domain.SignalBinding) (string, error) {
	if missing := needsParams(l, b); len(missing) > 0 {
		return "", fmt.Errorf("the %s binding has no value for %s: %w", b.Signal, strings.Join(missing, ", "), datasource.ErrInvalidInput)
	}
	return renderIn(l, b, displayStep, headlineQuantile)
}

// TracesQuery: a zero Until is now, a zero Since DefaultTraceWindow before
// it; a zero Limit the source's default.
type TracesQuery struct {
	Environment domain.DeployEnvironment
	Since       time.Time
	Until       time.Time
	MinDuration time.Duration
	Limit       int
}

// Traces searches the component's confirmed traces.spans binding over the
// window. An unbound component answers Bound false and no traces; a binding
// that cannot be read answers its Error, never a failed request.
func (s *Service) Traces(ctx context.Context, componentID uuid.UUID, q TracesQuery) (domain.ComponentTraces, error) {
	if !q.Environment.Valid() {
		return domain.ComponentTraces{}, fmt.Errorf("unknown environment %q: %w", q.Environment, ErrInvalidInput)
	}
	until := q.Until
	if until.IsZero() {
		until = s.now()
	}
	since := q.Since
	if since.IsZero() {
		since = until.Add(-DefaultTraceWindow)
	}
	fe := datasource.FieldErrors{}
	switch {
	case !until.After(since):
		fe["since"] = datasource.FieldInvalid
	case until.Sub(since) > datasource.MaxTraceWindow:
		fe["since"] = datasource.FieldTooLong
	}
	if q.Limit < 0 || q.Limit > datasource.MaxTraceLimit {
		fe["limit"] = datasource.FieldInvalid
	}
	if q.MinDuration < 0 || q.MinDuration > datasource.MaxTraceMinDuration {
		fe["min_duration_ms"] = datasource.FieldInvalid
	}
	if len(fe) > 0 {
		return domain.ComponentTraces{}, fe
	}
	if _, err := s.components.GetComponent(ctx, componentID); err != nil {
		return domain.ComponentTraces{}, err
	}
	out := domain.ComponentTraces{ComponentID: componentID, Environment: q.Environment, Traces: []domain.TraceSummary{}}
	b, ok, err := s.confirmedFor(ctx, componentID, q.Environment, domain.SignalTracesSpans)
	if err != nil || !ok {
		return out, err
	}
	out.Bound, out.DataSourceID = true, &b.DataSourceID
	l, err := s.lookup(ctx)
	if err != nil {
		return domain.ComponentTraces{}, err
	}
	if missing := needsParams(l, b); len(missing) > 0 {
		out.Error = &domain.SignalReadError{Code: domain.SignalErrNeedsParams, Message: "no value for " + strings.Join(missing, ", ")}
		return out, nil
	}
	query, err := renderIn(l, b, displayStep, headlineQuantile)
	if err != nil {
		out.Error = readError(err, s.queryTimeout)
		return out, nil
	}
	found, err := within(ctx, s.queryTimeout, func(ctx context.Context) ([]domain.TraceSummary, error) {
		return s.reader(l).batch.SearchTraces(ctx, b.DataSourceID, domain.TraceQuery{
			Query: query, Since: since, Until: until, Limit: q.Limit, MinDuration: q.MinDuration,
		})
	})
	if err != nil {
		out.Error = readError(err, s.queryTimeout)
		return out, nil
	}
	out.Traces = found
	return out, nil
}

// Trace reads one trace through the component's confirmed traces.spans
// binding: ErrUnbound without one, port.ErrNotFound when the source does not
// hold the trace (or the component is unknown).
func (s *Service) Trace(ctx context.Context, componentID uuid.UUID, env domain.DeployEnvironment, traceID string) (domain.TraceView, error) {
	id, err := checkTraceRead(env, traceID)
	if err != nil {
		return domain.TraceView{}, err
	}
	if _, err := s.components.GetComponent(ctx, componentID); err != nil {
		return domain.TraceView{}, err
	}
	b, ok, err := s.confirmedFor(ctx, componentID, env, domain.SignalTracesSpans)
	switch {
	case err != nil:
		return domain.TraceView{}, err
	case !ok:
		return domain.TraceView{}, ErrUnbound
	}
	l, err := s.lookup(ctx)
	if err != nil {
		return domain.TraceView{}, err
	}
	return s.readTrace(ctx, l, b, id)
}

// FindTrace looks a trace id up in each source the environment's confirmed
// traces.spans bindings read (a dismissed component's left out), at most
// maxTraceSources of them, traceLookups at once and all within one query
// timeout: the first in binding order that holds it answers. ErrUnbound when
// no component has one; port.ErrNotFound when none holds it and none failed,
// else the last failure.
func (s *Service) FindTrace(ctx context.Context, env domain.DeployEnvironment, traceID string) (domain.TraceView, error) {
	id, err := checkTraceRead(env, traceID)
	if err != nil {
		return domain.TraceView{}, err
	}
	bound, err := s.confirmedOf(ctx, env, domain.SignalTracesSpans)
	if err == nil {
		bound, err = s.withoutDismissed(ctx, bound)
	}
	if err != nil {
		return domain.TraceView{}, err
	}
	if len(bound) == 0 {
		return domain.TraceView{}, ErrUnbound
	}
	l, err := s.lookup(ctx)
	if err != nil {
		return domain.TraceView{}, err
	}
	tried := map[uuid.UUID]bool{}
	var picked []domain.SignalBinding
	for _, b := range bound {
		if !tried[b.DataSourceID] && len(picked) < maxTraceSources {
			tried[b.DataSourceID] = true
			picked = append(picked, b)
		}
	}
	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()
	rd := s.reader(l)
	results := fanOut(ctx, len(picked), traceLookups, s.queryTimeout,
		func(i int) uuid.UUID { return picked[i].DataSourceID },
		func(ctx context.Context, i int) (domain.Trace, error) {
			return rd.batch.Trace(ctx, picked[i].DataSourceID, id)
		})
	var last error
	for i, r := range results {
		b := picked[i]
		switch {
		case r.err == nil:
			return domain.TraceView{Trace: r.value, ComponentID: b.ComponentID, DataSourceID: b.DataSourceID}, nil
		case errors.Is(r.err, port.ErrNotFound):
		default:
			log.Debug().Err(r.err).Str("data_source", b.DataSourceID.String()).Msg("signals: looking a trace up failed")
			last = r.err
		}
	}
	if last != nil {
		return domain.TraceView{}, last
	}
	return domain.TraceView{}, fmt.Errorf("no traces source of %s holds trace %s: %w", env, id, port.ErrNotFound)
}

func (s *Service) readTrace(ctx context.Context, l lookup, b domain.SignalBinding, id string) (domain.TraceView, error) {
	t, err := within(ctx, s.queryTimeout, func(ctx context.Context) (domain.Trace, error) {
		return s.reader(l).batch.Trace(ctx, b.DataSourceID, id)
	})
	if err != nil {
		return domain.TraceView{}, err
	}
	return domain.TraceView{Trace: t, ComponentID: b.ComponentID, DataSourceID: b.DataSourceID}, nil
}

func checkTraceRead(env domain.DeployEnvironment, traceID string) (string, error) {
	if !env.Valid() {
		return "", fmt.Errorf("unknown environment %q: %w", env, ErrInvalidInput)
	}
	id, ok := domain.NormalizeTraceID(traceID)
	if !ok {
		return "", fmt.Errorf("trace id %q is not 1 to 32 hex digits: %w", traceID, ErrInvalidInput)
	}
	return id, nil
}

func (s *Service) confirmedFor(ctx context.Context, componentID uuid.UUID, env domain.DeployEnvironment, signal string) (domain.SignalBinding, bool, error) {
	bindings, err := s.bindings.ListSignalBindings(ctx, componentID, env)
	if err != nil {
		return domain.SignalBinding{}, false, err
	}
	for _, b := range bindings {
		if b.Signal == signal && b.Status == domain.SignalBindingConfirmed {
			return b, true, nil
		}
	}
	return domain.SignalBinding{}, false, nil
}
