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
// Step one giving about 120 points. No Signals means every signal of the
// component's role.
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
// shifted onto this one's timestamps. A signal that cannot be read carries
// its error; it never fails the others.
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
	out := domain.ComponentSeries{From: from, To: to, StepSeconds: int64(step / time.Second), Signals: make([]domain.SignalSeries, len(ids))}
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

	results := fanOut(ctx, len(jobs), s.concurrency, s.queryTimeout, func(ctx context.Context, i int) ([]domain.Series, error) {
		j := jobs[i]
		jf, jt, shift := from, to, time.Duration(0)
		if j.previous {
			jf, jt, shift = from.Add(-span), to.Add(-span), span
		}
		series, _, err := s.read(ctx, j.binding, j.variant.value, jf, jt, step)
		if err != nil {
			return nil, err
		}
		return labelQuantile(shifted(series, shift), j.variant), nil
	})
	for i, r := range results {
		j := jobs[i]
		res := &out.Signals[j.signal]
		if res.Error != nil {
			continue
		}
		if r.err != nil {
			res.Error = readError(r.err, s.queryTimeout)
			res.Series, res.Previous = []domain.Series{}, []domain.Series{}
			continue
		}
		if j.previous {
			res.Previous = append(res.Previous, r.value...)
		} else {
			res.Series = append(res.Series, r.value...)
		}
	}
	return out, nil
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
	out := domain.SignalPreview{Series: []domain.Series{}, Warnings: []string{}}
	for _, v := range variants(s.signalIndex[signal], template(l, b)) {
		series, rendered, err := s.readWithWarnings(ctx, b, v.value, from, to, step, &out.Warnings)
		if err != nil {
			return domain.SignalPreview{}, err
		}
		if v.value == 0 || v.value == headlineQuantile {
			out.RenderedQuery = rendered
		}
		out.Series = append(out.Series, labelQuantile(series, v)...)
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

func (s *Service) read(ctx context.Context, b domain.SignalBinding, quantile float64, from, to time.Time, step time.Duration) ([]domain.Series, string, error) {
	var warnings []string
	return s.readWithWarnings(ctx, b, quantile, from, to, step, &warnings)
}

func (s *Service) readWithWarnings(ctx context.Context, b domain.SignalBinding, quantile float64, from, to time.Time, step time.Duration, warnings *[]string) ([]domain.Series, string, error) {
	query, err := s.render(ctx, b, step, quantile)
	if err != nil {
		return nil, "", err
	}
	res, err := s.sources.Query(ctx, b.DataSourceID, domain.SeriesQuery{Query: query, From: from, To: to, Step: step})
	if err != nil {
		return nil, query, err
	}
	*warnings = append(*warnings, res.Warnings...)
	return res.Series, query, nil
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
		if relevant[sig.ID] {
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

func readError(err error, timeout time.Duration) *domain.SignalReadError {
	code := domain.SignalErrInternal
	msg := err.Error()
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		code, msg = domain.SignalErrTimeout, fmt.Sprintf("no answer within %s", timeout)
	case errors.Is(err, port.ErrDataSourceQuery), errors.Is(err, datasource.ErrInvalidInput):
		code = domain.SignalErrBadQuery
	case errors.Is(err, datasource.ErrUnsupported):
		code = domain.SignalErrUnsupported
	case errors.Is(err, datasource.ErrUpstream):
		code = domain.SignalErrUpstream
	case errors.Is(err, port.ErrNotFound):
		code, msg = domain.SignalErrNotFound, "the data source no longer exists"
	}
	return &domain.SignalReadError{Code: code, Message: msg}
}
