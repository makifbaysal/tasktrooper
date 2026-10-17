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
	"sort"
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
	// headlineQuantile is the one quantile a quantile signal is read at:
	// nothing else of it is judged.
	headlineQuantile = 0.95
	cacheSize        = 256
	unitMS           = "ms"
)

// Signals is satisfied by signals.Service.
type Signals interface {
	ReadSignal(ctx context.Context, componentID uuid.UUID, env domain.DeployEnvironment, signal string, quantile float64, from, to time.Time, step time.Duration) ([]domain.Series, error)
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
		thresholds: d.Thresholds.WithDefaults(),
		now:        d.Clock,
		cache:      map[cacheKey]domain.DeployImpact{},
	}
	if s.now == nil {
		s.now = func() time.Time { return time.Now().UTC() }
	}
	return s
}

// Thresholds is what this service judges by, defaults filled in.
func (s *Service) Thresholds() domain.DeployImpactConfig { return s.thresholds }

// read is one signal's two reads: the hour around the deploy and, once the
// after-window has begun, yesterday's same stretch.
type read struct {
	signal         string
	current, prior []domain.Series
	currErr, prErr error
	unbound        bool
	withBaseline   bool
}

// ForRelease reads the release's production impact. A release with no
// deploy time, no component, a component gone or a role with no golden
// signals answers unknown with no signals; so does one whose golden signals
// are all unbound. A signal that cannot be read is listed as unknown and
// never fails the others; every bound signal failing is the answer's error.
func (s *Service) ForRelease(ctx context.Context, r domain.Release) (domain.DeployImpact, error) {
	return s.forRelease(ctx, r, true)
}

// ForReleaseFresh is ForRelease read from the sources even when this
// minute's answer is cached: the soak's last word on a release.
func (s *Service) ForReleaseFresh(ctx context.Context, r domain.Release) (domain.DeployImpact, error) {
	return s.forRelease(ctx, r, false)
}

func (s *Service) forRelease(ctx context.Context, r domain.Release, useCache bool) (domain.DeployImpact, error) {
	now := s.now().UTC()
	out := domain.NewDeployImpact(r, now)
	if out.DeployedAt == nil || r.ComponentID == nil || s.signals == nil || s.components == nil {
		return out, nil
	}
	key := cacheKey{release: r.ID, minute: now.Truncate(time.Minute).Unix()}
	if cached, ok := s.cached(key); ok && useCache {
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
	reads := s.readAll(ctx, comp.ID, ids, deployed, to, afterEnd)

	verdicts := make([]domain.ImpactVerdict, 0, len(reads))
	var firstErr error
	complete, bound, failed := true, 0, 0
	for _, rd := range reads {
		if rd.unbound {
			continue
		}
		bound++
		sig, _ := s.signals.Signal(rd.signal)
		si := domain.SignalImpact{
			Signal:         rd.signal,
			Unit:           sig.Unit,
			HigherIsBetter: sig.Better == domain.SignalHigherIsBetter,
			Verdict:        domain.ImpactUnknown,
		}
		if rd.currErr != nil {
			complete = false
			failed++
			if firstErr == nil {
				firstErr = rd.currErr
			}
		} else {
			points := headline(rd.current, sig)
			si.Before, _ = mean(points, deployed.Add(-domain.DefaultImpactWindow), deployed, false)
			if afterEnd.After(deployed) {
				si.After, si.AfterPoints = mean(points, deployed, afterEnd, true)
			}
			if rd.withBaseline && rd.prErr == nil {
				si.Baseline, _ = mean(headline(rd.prior, sig), deployed.Add(-baselineShift), afterEnd.Add(-baselineShift), true)
			} else if rd.prErr != nil {
				complete = false
			}
			si.Delta, si.DeltaPct = deltas(si.Before, si.After)
			si.Verdict = s.verdict(sig, si.Before, si.After, si.Baseline)
		}
		verdicts = append(verdicts, si.Verdict)
		out.Signals = append(out.Signals, si)
	}
	if bound > 0 && failed == bound {
		return domain.DeployImpact{}, firstErr
	}
	out.Verdict = domain.WorstImpact(verdicts)
	if complete {
		s.store(key, out)
	}
	return out, nil
}

// readAll reads every golden signal's two windows at once, so the slowest
// source bounds the answer rather than the sum of them. A signal with no
// confirmed binding is marked unbound.
func (s *Service) readAll(ctx context.Context, componentID uuid.UUID, ids []string, deployed, to, afterEnd time.Time) []read {
	reads := make([]read, len(ids))
	var wg sync.WaitGroup
	for i, id := range ids {
		reads[i] = read{signal: id, withBaseline: afterEnd.After(deployed)}
		quantile := s.quantileOf(id)
		wg.Add(1)
		go func(rd *read) {
			defer wg.Done()
			rd.current, rd.currErr = s.signals.ReadSignal(ctx, componentID, domain.EnvironmentProduction, rd.signal, quantile,
				deployed.Add(-domain.DefaultImpactWindow), to, step)
			if errors.Is(rd.currErr, signals.ErrUnbound) {
				rd.unbound, rd.currErr = true, nil
			}
		}(&reads[i])
		if !reads[i].withBaseline {
			continue
		}
		wg.Add(1)
		go func(rd *read) {
			defer wg.Done()
			rd.prior, rd.prErr = s.signals.ReadSignal(ctx, componentID, domain.EnvironmentProduction, rd.signal, quantile,
				deployed.Add(-baselineShift), afterEnd.Add(-baselineShift), step)
			if errors.Is(rd.prErr, signals.ErrUnbound) {
				rd.prErr = nil
			}
		}(&reads[i])
	}
	wg.Wait()
	return reads
}

func (s *Service) quantileOf(id string) float64 {
	if sig, ok := s.signals.Signal(id); ok && sig.Aggregation == domain.AggregationQuantile {
		return headlineQuantile
	}
	return 0
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
// the worse, 0 inside it. From zero any change past the absolute floor
// counts: there is no percentage of nothing.
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
	default:
		if from != 0 && math.Abs(delta)/math.Abs(from)*100 < s.changePct(sig) {
			return 0
		}
		if math.Abs(delta) < s.floor(sig, from, to) {
			return 0
		}
	}
	if (delta > 0) == (sig.Better == domain.SignalHigherIsBetter) {
		return 1
	}
	return -1
}

func (s *Service) changePct(sig domain.Signal) float64 {
	if sig.Aggregation == domain.AggregationRate && sig.Better == domain.SignalHigherIsBetter {
		return s.thresholds.ThroughputChangePct
	}
	return s.thresholds.ChangePct
}

// floor is the smallest absolute move that counts: a fraction of an error a
// second or of a queued message is noise, however large it is in percent.
func (s *Service) floor(sig domain.Signal, from, to float64) float64 {
	larger := math.Max(math.Abs(from), math.Abs(to))
	switch {
	case sig.Aggregation == domain.AggregationQuantile && sig.Unit == unitMS:
		return s.thresholds.LatencyFloorMS
	case sig.Better != domain.SignalLowerIsBetter:
		return 0
	case sig.Aggregation == domain.AggregationGauge:
		return math.Max(s.thresholds.CountFloor, larger*s.thresholds.FloorPct/100)
	case sig.Aggregation == domain.AggregationRate:
		return math.Max(s.thresholds.RateFloor, larger*s.thresholds.FloorPct/100)
	}
	return 0
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

// headline is the series a signal is judged by: at each instant the worst of
// the series it answered — the highest where lower is better, the lowest
// where higher is — as the alert evaluator judges a query answering several.
func headline(series []domain.Series, sig domain.Signal) []domain.SeriesPoint {
	if len(series) == 1 {
		return series[0].Points
	}
	lowIsWorse := sig.Better == domain.SignalHigherIsBetter
	byTime := map[int64]domain.SeriesPoint{}
	for _, sr := range series {
		for _, p := range sr.Points {
			if p.Value == nil {
				continue
			}
			k := p.Time.UnixNano()
			cur, seen := byTime[k]
			if !seen || (lowIsWorse && *p.Value < *cur.Value) || (!lowIsWorse && *p.Value > *cur.Value) {
				byTime[k] = p
			}
		}
	}
	out := make([]domain.SeriesPoint, 0, len(byTime))
	for _, p := range byTime {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Time.Before(out[j].Time) })
	return out
}

// mean of the non-null points between from and to and how many there were:
// [from, to) for the window before the deploy, (from, to] for one after it,
// whose sample at the deploy itself still describes the release before.
func mean(points []domain.SeriesPoint, from, to time.Time, after bool) (*float64, int) {
	sum, n := 0.0, 0
	for _, p := range points {
		if p.Value == nil || p.Time.Before(from) || p.Time.After(to) {
			continue
		}
		if (after && !p.Time.After(from)) || (!after && p.Time.Equal(to)) {
			continue
		}
		sum += *p.Value
		n++
	}
	if n == 0 {
		return nil, 0
	}
	m := sum / float64(n)
	return &m, n
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
