package signals

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/datasource"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	DefaultWindow = time.Hour
	MaxWindow     = 90 * 24 * time.Hour

	seriesPoints = 120
	minAutoStep  = 15 * time.Second
	// maxPoints is Prometheus' own per-series ceiling for query_range.
	maxPoints = 11000
)

// SeriesQuery: a zero To is now, a zero From DefaultWindow before To, a zero
// Step one giving about 120 points. No Signals means every metric signal of
// the component's role: a log or trace signal is no series.
type SeriesQuery struct {
	Environment domain.DeployEnvironment
	Signals     []string
	From        time.Time
	To          time.Time
	Step        time.Duration
	Compare     bool
}

// Series reads the confirmed binding of each signal over the window. A
// quantile signal whose query takes $__quantile answers one series per
// quantile (p50, p95, p99). With Compare, Previous is the window before,
// shifted onto this one's timestamps; a previous window that cannot be read
// carries PreviousError and leaves Series be. A signal that cannot be read
// carries its error; it never fails the others. StepSeconds is the coarsest
// spacing a source answered at, never finer than the step asked for.
func (s *Service) Series(ctx context.Context, componentID uuid.UUID, q SeriesQuery) (domain.ComponentSeries, error) {
	if !q.Environment.Valid() {
		return domain.ComponentSeries{}, fmt.Errorf("unknown environment %q: %w", q.Environment, ErrInvalidInput)
	}
	from, to, step, err := s.window(q.From, q.To, q.Step, seriesPoints, MaxWindow)
	if err != nil {
		return domain.ComponentSeries{}, err
	}
	comp, err := s.components.GetComponent(ctx, componentID)
	if err != nil {
		return domain.ComponentSeries{}, err
	}
	bindings, err := s.bindings.ListSignalBindings(ctx, componentID, q.Environment)
	if err != nil {
		return domain.ComponentSeries{}, err
	}
	confirmed := map[string]domain.SignalBinding{}
	for _, b := range bindings {
		if b.Status == domain.SignalBindingConfirmed {
			confirmed[b.Signal] = b
		}
	}
	l, err := s.lookup(ctx)
	if err != nil {
		return domain.ComponentSeries{}, err
	}

	ids := s.requestedSignals(comp.Role.Get(), q.Signals)
	out := domain.ComponentSeries{From: from, To: to, Signals: make([]domain.SignalSeries, len(ids))}
	span := to.Sub(from)
	type job struct {
		signal   int
		binding  domain.SignalBinding
		variant  variant
		previous bool
	}
	var jobs []job
	for i, id := range ids {
		res := domain.SignalSeries{Signal: id, Series: []domain.Series{}, Previous: []domain.Series{}}
		sig, known := s.signalIndex[id]
		b, isBound := confirmed[id]
		res.Unit, res.Bound = sig.Unit, isBound
		res.Live = isBound && domain.LiveOnlyKind(l.sources[b.DataSourceID].Kind)
		switch {
		case !known:
			res.Error = &domain.SignalReadError{Code: domain.SignalErrUnknown, Message: fmt.Sprintf("%q is not a catalog signal", id)}
		case !isBound:
		case sig.Kind != domain.SignalMetric:
			res.Error = &domain.SignalReadError{Code: domain.SignalErrUnsupported, Message: fmt.Sprintf("%s is a %s signal, not a metric", id, sig.Kind)}
		default:
			if missing := needsParams(l, b); len(missing) > 0 {
				res.Error = &domain.SignalReadError{Code: domain.SignalErrNeedsParams, Message: "no value for " + strings.Join(missing, ", ")}
				break
			}
			for _, v := range variants(sig, template(l, b)) {
				jobs = append(jobs, job{signal: i, binding: b, variant: v})
				if q.Compare {
					jobs = append(jobs, job{signal: i, binding: b, variant: v, previous: true})
				}
			}
		}
		out.Signals[i] = res
	}

	type seriesRead struct {
		series []domain.Series
		step   time.Duration
	}
	rd := s.reader(l)
	budget, cancel := context.WithTimeout(ctx, s.seriesBudget)
	defer cancel()
	results := fanOut(budget, len(jobs), s.concurrency, s.queryTimeout,
		func(i int) uuid.UUID { return jobs[i].binding.DataSourceID },
		func(ctx context.Context, i int) (seriesRead, error) {
			j := jobs[i]
			jf, jt, shift := from, to, time.Duration(0)
			if j.previous {
				jf, jt, shift = from.Add(-span), to.Add(-span), span
			}
			res, _, err := rd.read(ctx, j.binding, j.variant.value, jf, jt, step)
			if err != nil {
				return seriesRead{}, err
			}
			return seriesRead{series: labelQuantile(shifted(res.Series, shift), j.variant), step: res.Step}, nil
		})
	failed := make([]*domain.SignalReadError, len(ids))
	previousFailed := make([]*domain.SignalReadError, len(ids))
	effective := step
	for i, r := range results {
		j := jobs[i]
		res := &out.Signals[j.signal]
		switch {
		case r.err != nil && j.previous:
			if previousFailed[j.signal] == nil {
				previousFailed[j.signal] = s.jobError(r.err, r.late)
			}
		case r.err != nil:
			if failed[j.signal] == nil {
				failed[j.signal] = s.jobError(r.err, r.late)
			}
		case j.previous:
			res.Previous = append(res.Previous, r.value.series...)
		default:
			res.Series = append(res.Series, r.value.series...)
			effective = max(effective, r.value.step)
		}
	}
	for i := range out.Signals {
		res := &out.Signals[i]
		switch {
		case failed[i] != nil:
			res.Error = failed[i]
			res.Series, res.Previous = []domain.Series{}, []domain.Series{}
		case previousFailed[i] != nil:
			res.PreviousError = previousFailed[i]
			res.Previous = []domain.Series{}
		}
	}
	out.StepSeconds = int64(effective / time.Second)
	return out, nil
}

// jobError: late is a read the series budget cut off or never started.
func (s *Service) jobError(err error, late bool) *domain.SignalReadError {
	if late && errors.Is(err, context.DeadlineExceeded) {
		return &domain.SignalReadError{Code: domain.SignalErrTimeout, Message: fmt.Sprintf("not read within the %s a series read may take", s.seriesBudget)}
	}
	return readError(err, s.queryTimeout)
}

// PreviewRequest runs a binding request once for a signal before it is
// saved. Zero From/To/Step default as in SeriesQuery.
type PreviewRequest struct {
	BindingRequest
	Signal string
	From   time.Time
	To     time.Time
	Step   time.Duration
}

// Preview renders and runs what a binding would read. RenderedQuery is the
// p95 one for a quantile signal answered per quantile. Errors are the data
// source routes' own: field errors, a refused query, the source failing.
func (s *Service) Preview(ctx context.Context, componentID uuid.UUID, req PreviewRequest) (domain.SignalPreview, error) {
	if _, err := s.components.GetComponent(ctx, componentID); err != nil {
		return domain.SignalPreview{}, err
	}
	from, to, step, err := s.window(req.From, req.To, req.Step, seriesPoints, MaxWindow)
	if err != nil {
		return domain.SignalPreview{}, err
	}
	l, err := s.lookup(ctx)
	if err != nil {
		return domain.SignalPreview{}, err
	}
	signal := strings.TrimSpace(req.Signal)
	if signal == "" {
		return domain.SignalPreview{}, datasource.FieldErrors{"signal": datasource.FieldRequired}
	}
	b, err := s.validateTarget(ctx, l, signal, req.BindingRequest)
	if err != nil {
		return domain.SignalPreview{}, err
	}
	if sig := s.signalIndex[signal]; sig.Kind != domain.SignalMetric {
		return domain.SignalPreview{}, fmt.Errorf("%s is a %s signal, read in the Logs explorer or the traces view rather than as a series: %w", signal, sig.Kind, datasource.ErrUnsupported)
	}
	rd := s.reader(l)
	out := domain.SignalPreview{Series: []domain.Series{}, Warnings: []string{}}
	for _, v := range variants(s.signalIndex[signal], template(l, b)) {
		res, rendered, err := rd.read(ctx, b, v.value, from, to, step)
		if err != nil {
			return domain.SignalPreview{}, err
		}
		out.Warnings = append(out.Warnings, res.Warnings...)
		if v.value == 0 || v.value == headlineQuantile {
			out.RenderedQuery = rendered
		}
		out.Series = append(out.Series, labelQuantile(res.Series, v)...)
	}
	return out, nil
}

// variants: one read per quantile for a quantile signal whose query takes
// $__quantile, otherwise a single read as written.
func variants(sig domain.Signal, query string) []variant {
	if sig.Aggregation != domain.AggregationQuantile || !takesQuantile(query) {
		return []variant{{}}
	}
	return slices.Clone(quantiles)
}

func shifted(series []domain.Series, by time.Duration) []domain.Series {
	if by == 0 {
		return series
	}
	out := make([]domain.Series, len(series))
	for i, sr := range series {
		points := make([]domain.SeriesPoint, len(sr.Points))
		for k, p := range sr.Points {
			points[k] = domain.SeriesPoint{Time: p.Time.Add(by), Value: p.Value}
		}
		out[i] = domain.Series{Name: sr.Name, Labels: sr.Labels, Points: points}
	}
	return out
}

// labelQuantile names a per-quantile read by its quantile: "p95" for the one
// series a recipe's aggregation gives, "p95 <name>" when it gave several.
func labelQuantile(series []domain.Series, v variant) []domain.Series {
	if v.label == "" {
		return series
	}
	out := make([]domain.Series, len(series))
	for i, sr := range series {
		labels := maps.Clone(sr.Labels)
		if labels == nil {
			labels = map[string]string{}
		}
		labels[quantileLabel] = strconv.FormatFloat(v.value, 'f', -1, 64)
		name := v.label
		if len(series) > 1 && sr.Name != "" {
			name = v.label + " " + sr.Name
		}
		out[i] = domain.Series{Name: name, Labels: labels, Points: sr.Points}
	}
	return out
}

func (s *Service) requestedSignals(role domain.ComponentRole, asked []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, raw := range asked {
		id := strings.TrimSpace(raw)
		if id != "" && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	if len(out) > 0 {
		return out
	}
	relevant := s.relevant(role)
	for _, sig := range s.signals {
		if relevant[sig.ID] && sig.Kind == domain.SignalMetric {
			out = append(out, sig.ID)
		}
	}
	return out
}

// window fills in and checks a time range: To defaults to now, From to
// DefaultWindow before it, and a zero step to about points points.
func (s *Service) window(from, to time.Time, step time.Duration, points int, maxSpan time.Duration) (time.Time, time.Time, time.Duration, error) {
	if to.IsZero() {
		to = s.now()
	}
	if from.IsZero() {
		from = to.Add(-DefaultWindow)
	}
	from, to = from.UTC(), to.UTC()
	span := to.Sub(from)
	switch {
	case !to.After(from):
		return from, to, step, datasource.FieldErrors{"to": datasource.FieldInvalid}
	case span > maxSpan:
		return from, to, step, datasource.FieldErrors{"from": datasource.FieldTooLong}
	case step < 0:
		return from, to, step, datasource.FieldErrors{"step_seconds": datasource.FieldInvalid}
	}
	if step == 0 {
		step = max((span / time.Duration(points)).Round(time.Second), minAutoStep)
	}
	if step < time.Second {
		return from, to, step, datasource.FieldErrors{"step_seconds": datasource.FieldInvalid}
	}
	if span/step > maxPoints {
		return from, to, step, datasource.FieldErrors{"step_seconds": datasource.FieldTooLong}
	}
	return from, to, step, nil
}

// readError says what the person can act on. An error of no class here (a
// store or decryption failure, a panic) is this server's own: its detail is
// logged, not answered.
func readError(err error, timeout time.Duration) *domain.SignalReadError {
	msg := err.Error()
	switch {
	case errors.Is(err, errSourceTimedOut):
		return &domain.SignalReadError{Code: domain.SignalErrTimeout, Message: "skipped: the data source did not answer an earlier query of this read in time"}
	case errors.Is(err, context.DeadlineExceeded):
		return &domain.SignalReadError{Code: domain.SignalErrTimeout, Message: fmt.Sprintf("no answer within %s", timeout)}
	case errors.Is(err, port.ErrDataSourceQuery), errors.Is(err, datasource.ErrInvalidInput):
		return &domain.SignalReadError{Code: domain.SignalErrBadQuery, Message: msg}
	case errors.Is(err, datasource.ErrUnsupported):
		return &domain.SignalReadError{Code: domain.SignalErrUnsupported, Message: msg}
	case errors.Is(err, datasource.ErrUpstream):
		return &domain.SignalReadError{Code: domain.SignalErrUpstream, Message: msg}
	case errors.Is(err, port.ErrNotFound):
		return &domain.SignalReadError{Code: domain.SignalErrNotFound, Message: "the data source no longer exists"}
	}
	log.Warn().Err(err).Msg("signals: reading a signal failed")
	return &domain.SignalReadError{Code: domain.SignalErrInternal, Message: errPanicked.Error()}
}
