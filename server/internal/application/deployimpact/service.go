// Package deployimpact judges what a release did to its component in
// production: each golden signal with a confirmed binding, read over the hour
// before the deploy, the hour after it and that same hour a day earlier, and
// called improved, neutral or worse against configurable thresholds. The
// release soak holds it to the delivery profile's impact thresholds; the
// Operations console and the release engineer read it as it is.
package deployimpact

import (
	"context"
	"errors"
	"math"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/signals"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	step          = time.Minute
	baselineShift = 24 * time.Hour
	headlineLabel = "0.95"
	cacheSize     = 256
)

type Signals interface {
	Series(ctx context.Context, componentID uuid.UUID, q signals.SeriesQuery) (domain.ComponentSeries, error)
	GoldenSignals(role domain.ComponentRole) []string
	Signal(id string) (domain.Signal, bool)
}

type Components interface {
	GetComponent(ctx context.Context, id uuid.UUID) (domain.Component, error)
}

type Deps struct {
	Signals    Signals
	Components Components
	Thresholds domain.DeployImpactConfig
	Clock      func() time.Time
}

type Service struct {
	signals    Signals
	components Components
	thresholds domain.DeployImpactConfig
	now        func() time.Time

	mu    sync.Mutex
	cache map[cacheKey]domain.DeployImpact
}

// cacheKey: an answer holds for the minute it was computed in, so the soak's
// sweeps and the console's refreshes inside one minute read the source once.
type cacheKey struct {
	release uuid.UUID
	minute  int64
}

func New(d Deps) *Service {
	s := &Service{
		signals:    d.Signals,
		components: d.Components,
		thresholds: d.Thresholds,
		now:        d.Clock,
		cache:      map[cacheKey]domain.DeployImpact{},
	}
	if s.now == nil {
		s.now = func() time.Time { return time.Now().UTC() }
	}
	if s.thresholds.ChangePct <= 0 {
		s.thresholds.ChangePct = domain.DefaultImpactChangePct
	}
	if s.thresholds.RatioPoints <= 0 {
		s.thresholds.RatioPoints = domain.DefaultImpactRatioPoints
	}
	return s
}

// ForRelease reads the release's production impact. A release with no
// deploy time, no component, a component gone or a role with no golden
// signals answers unknown with no signals; so does one whose golden signals
// are all unbound. A signal that cannot be read is listed as unknown and
// never fails the others.
func (s *Service) ForRelease(ctx context.Context, r domain.Release) (domain.DeployImpact, error) {
	now := s.now().UTC()
	out := domain.NewDeployImpact(r, now)
	if out.DeployedAt == nil || r.ComponentID == nil || s.signals == nil || s.components == nil {
		return out, nil
	}
	key := cacheKey{release: r.ID, minute: now.Truncate(time.Minute).Unix()}
	if cached, ok := s.cached(key); ok {
		return cached, nil
	}

	comp, err := s.components.GetComponent(ctx, *r.ComponentID)
	if err != nil {
		if errors.Is(err, port.ErrNotFound) {
			return out, nil
		}
		return domain.DeployImpact{}, err
	}
	ids := s.signals.GoldenSignals(comp.Role.Get())
	if len(ids) == 0 {
		s.store(key, out)
		return out, nil
	}

	deployed := *out.DeployedAt
	afterEnd := deployed.Add(domain.DefaultImpactWindow)
	if afterEnd.After(now) {
		afterEnd = now
	}
	to := afterEnd
	if !to.After(deployed) {
		to = deployed
	}
	current, err := s.signals.Series(ctx, comp.ID, signals.SeriesQuery{
		Environment: domain.EnvironmentProduction,
		Signals:     ids,
		From:        deployed.Add(-domain.DefaultImpactWindow),
		To:          to,
		Step:        step,
	})
	if err != nil {
		return domain.DeployImpact{}, err
	}
	baseline := map[string]domain.SignalSeries{}
	if afterEnd.After(deployed) {
		prior, err := s.signals.Series(ctx, comp.ID, signals.SeriesQuery{
			Environment: domain.EnvironmentProduction,
			Signals:     ids,
			From:        deployed.Add(-baselineShift),
			To:          afterEnd.Add(-baselineShift),
			Step:        step,
		})
		if err != nil {
			return domain.DeployImpact{}, err
		}
		for _, ss := range prior.Signals {
			baseline[ss.Signal] = ss
		}
	}

	verdicts := make([]domain.ImpactVerdict, 0, len(current.Signals))
	for _, ss := range current.Signals {
		if !ss.Bound {
			continue
		}
		sig, _ := s.signals.Signal(ss.Signal)
		si := domain.SignalImpact{
			Signal:         ss.Signal,
			Unit:           ss.Unit,
			HigherIsBetter: sig.Better == domain.SignalHigherIsBetter,
			Verdict:        domain.ImpactUnknown,
		}
		if ss.Error == nil {
			points := headline(ss.Series)
			si.Before = mean(points, deployed.Add(-domain.DefaultImpactWindow), deployed, false)
			if afterEnd.After(deployed) {
				si.After = mean(points, deployed, afterEnd, true)
			}
			if prior, ok := baseline[ss.Signal]; ok && prior.Error == nil {
				si.Baseline = mean(headline(prior.Series), deployed.Add(-baselineShift), afterEnd.Add(-baselineShift), true)
			}
			si.Delta, si.DeltaPct = deltas(si.Before, si.After)
			si.Verdict = s.verdict(sig, si.Before, si.After, si.Baseline)
		}
		verdicts = append(verdicts, si.Verdict)
		out.Signals = append(out.Signals, si)
	}
	out.Verdict = domain.WorstImpact(verdicts)
	s.store(key, out)
	return out, nil
}

// verdict needs the move from before to after to clear the threshold, and,
// when yesterday's same hour was read, the move from it to after to clear it
// the same way: a change that hour also shows every day is the time of day,
// not the deploy.
func (s *Service) verdict(sig domain.Signal, before, after, baseline *float64) domain.ImpactVerdict {
	if sig.Better == "" || before == nil || after == nil {
		return domain.ImpactUnknown
	}
	dir := s.move(sig, *before, *after)
	if dir == 0 {
		return domain.ImpactNeutral
	}
	if baseline != nil && s.move(sig, *baseline, *after) != dir {
		return domain.ImpactNeutral
	}
	if dir > 0 {
		return domain.ImpactImproved
	}
	return domain.ImpactWorse
}

// move is +1 for a change for the better past the threshold, -1 for one for
// the worse, 0 inside it. From zero any change counts: there is no
// percentage of nothing.
func (s *Service) move(sig domain.Signal, from, to float64) int {
	delta := to - from
	if delta == 0 {
		return 0
	}
	switch {
	case sig.Aggregation == domain.AggregationRatio:
		if math.Abs(delta) < s.thresholds.RatioPoints {
			return 0
		}
	case from != 0:
		if math.Abs(delta)/math.Abs(from)*100 < s.thresholds.ChangePct {
			return 0
		}
	}
	if (delta > 0) == (sig.Better == domain.SignalHigherIsBetter) {
		return 1
	}
	return -1
}

func deltas(before, after *float64) (*float64, *float64) {
	if before == nil || after == nil {
		return nil, nil
	}
	d := *after - *before
	if *before == 0 {
		return &d, nil
	}
	pct := d / math.Abs(*before) * 100
	return &d, &pct
}

// headline is the series a signal is judged by: its p95 when it was read
// per quantile, else the first it answered.
func headline(series []domain.Series) []domain.SeriesPoint {
	for _, sr := range series {
		if sr.Labels["quantile"] == headlineLabel {
			return sr.Points
		}
	}
	if len(series) == 0 {
		return nil
	}
	return series[0].Points
}

// mean of the non-null points from from up to to, to itself included only
// when closed.
func mean(points []domain.SeriesPoint, from, to time.Time, closed bool) *float64 {
	sum, n := 0.0, 0
	for _, p := range points {
		if p.Value == nil || p.Time.Before(from) || p.Time.After(to) || (!closed && p.Time.Equal(to)) {
			continue
		}
		sum += *p.Value
		n++
	}
	if n == 0 {
		return nil
	}
	m := sum / float64(n)
	return &m
}

func (s *Service) cached(key cacheKey) (domain.DeployImpact, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.cache[key]
	return v, ok
}

func (s *Service) store(key cacheKey, v domain.DeployImpact) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k := range s.cache {
		if k.minute < key.minute {
			delete(s.cache, k)
		}
	}
	if len(s.cache) >= cacheSize {
		clear(s.cache)
	}
	s.cache[key] = v
}
