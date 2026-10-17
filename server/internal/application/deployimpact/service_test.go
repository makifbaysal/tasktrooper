package deployimpact_test

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/deployimpact"
	"github.com/makifbaysal/tasktrooper/server/internal/application/signals"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

var catalog = map[string]domain.Signal{
	"http.requests":             {ID: "http.requests", Unit: "req/s", Aggregation: domain.AggregationRate, Better: domain.SignalHigherIsBetter},
	domain.SignalHTTPErrorRatio: {ID: domain.SignalHTTPErrorRatio, Unit: "%", Aggregation: domain.AggregationRatio, Better: domain.SignalLowerIsBetter},
	domain.SignalHTTPLatency:    {ID: domain.SignalHTTPLatency, Unit: "ms", Aggregation: domain.AggregationQuantile, Better: domain.SignalLowerIsBetter},
	"queue.dead_letter":         {ID: "queue.dead_letter", Unit: "messages", Aggregation: domain.AggregationGauge, Better: domain.SignalLowerIsBetter},
	"process.errors":            {ID: "process.errors", Unit: "errors/s", Aggregation: domain.AggregationRate, Better: domain.SignalLowerIsBetter},
}

type signalRead struct {
	signal   string
	quantile float64
	from, to time.Time
}

// fakeSignals answers each signal from a function of time, so a window's mean
// is whatever the test says that stretch of time held. Unbound signals and
// read errors are per signal, the way signals.Service.ReadSignal reports them;
// extra adds series a signal answers beside its own.
type fakeSignals struct {
	mu      sync.Mutex
	values  map[string]func(time.Time) float64
	extra   map[string][]func(time.Time) float64
	unbound map[string]bool
	readErr map[string]bool
	err     error
	reads   []signalRead
}

func (f *fakeSignals) ReadSignal(_ context.Context, _ uuid.UUID, _ domain.DeployEnvironment, signal string, quantile float64, from, to time.Time, step time.Duration) ([]domain.Series, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads = append(f.reads, signalRead{signal: signal, quantile: quantile, from: from, to: to})
	switch {
	case f.err != nil:
		return nil, f.err
	case f.unbound[signal]:
		return nil, signals.ErrUnbound
	case f.readErr[signal]:
		return nil, context.DeadlineExceeded
	}
	series := func(value func(time.Time) float64) domain.Series {
		var points []domain.SeriesPoint
		for t := from; !t.After(to); t = t.Add(step) {
			points = append(points, domain.FinitePoint(t, value(t)))
		}
		return domain.Series{Points: points}
	}
	out := []domain.Series{series(f.values[signal])}
	for _, value := range f.extra[signal] {
		out = append(out, series(value))
	}
	return out, nil
}

func (f *fakeSignals) readsOf(signal string) []signalRead {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []signalRead
	for _, r := range f.reads {
		if r.signal == signal {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].from.After(out[j].from) })
	return out
}

func (f *fakeSignals) readCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.reads)
}

func (f *fakeSignals) GoldenSignals(role domain.ComponentRole) []string {
	switch role {
	case domain.ComponentRoleBackend:
		return []string{"http.requests", domain.SignalHTTPErrorRatio, domain.SignalHTTPLatency}
	case domain.ComponentRoleWorker:
		return []string{"queue.dead_letter", "process.errors"}
	}
	return nil
}

func (f *fakeSignals) Signal(id string) (domain.Signal, bool) {
	sig, ok := catalog[id]
	return sig, ok
}

type fakeComponents map[uuid.UUID]domain.Component

func (f fakeComponents) GetComponent(_ context.Context, id uuid.UUID) (domain.Component, error) {
	c, ok := f[id]
	if !ok {
		return domain.Component{}, port.ErrNotFound
	}
	return c, nil
}

// stepAt is a signal that reads before until at, then after.
func stepAt(at time.Time, before, after float64) func(time.Time) float64 {
	return func(t time.Time) float64 {
		if t.Before(at) {
			return before
		}
		return after
	}
}

func flat(v float64) func(time.Time) float64 { return func(time.Time) float64 { return v } }

type ImpactSuite struct {
	suite.Suite
	ctx        context.Context
	deployedAt time.Time
	now        time.Time
	component  domain.Component
	signals    *fakeSignals
	svc        *deployimpact.Service
}

func TestImpactSuite(t *testing.T) { suite.Run(t, new(ImpactSuite)) }

func (s *ImpactSuite) SetupTest() {
	s.ctx = context.Background()
	s.deployedAt = time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	s.now = s.deployedAt.Add(3 * time.Hour)
	s.component = domain.Component{ID: uuid.New(), Role: domain.Fact[domain.ComponentRole]{Override: ptr(domain.ComponentRoleBackend)}}
	s.signals = &fakeSignals{
		values: map[string]func(time.Time) float64{
			"http.requests":             flat(100),
			domain.SignalHTTPErrorRatio: flat(0.4),
			domain.SignalHTTPLatency:    flat(200),
			"queue.dead_letter":         flat(0),
			"process.errors":            flat(0.01),
		},
		extra:   map[string][]func(time.Time) float64{},
		unbound: map[string]bool{},
		readErr: map[string]bool{},
	}
	s.svc = deployimpact.New(deployimpact.Deps{
		Signals:    s.signals,
		Components: fakeComponents{s.component.ID: s.component},
		Clock:      func() time.Time { return s.now },
	})
}

func ptr[T any](v T) *T { return &v }

func (s *ImpactSuite) release() domain.Release {
	return domain.Release{ID: uuid.New(), ComponentID: &s.component.ID, DeployedAt: &s.deployedAt}
}

func (s *ImpactSuite) signal(impact domain.DeployImpact, id string) domain.SignalImpact {
	si, ok := impact.Signal(id)
	s.Require().True(ok, "%s missing from %+v", id, impact.Signals)
	return si
}

func (s *ImpactSuite) TestAnErrorRateThatJumpsAtTheDeployIsWorse() {
	s.signals.values[domain.SignalHTTPErrorRatio] = stepAt(s.deployedAt, 0.4, 2.5)

	impact, err := s.svc.ForRelease(s.ctx, s.release())
	s.Require().NoError(err)

	s.Equal(domain.ImpactWorse, impact.Verdict)
	s.False(impact.Pending)
	s.Equal(60, impact.WindowMinutes)
	s.Equal(domain.EnvironmentProduction, impact.Environment)
	errs := s.signal(impact, domain.SignalHTTPErrorRatio)
	s.InDelta(0.4, *errs.Before, 1e-9)
	s.InDelta(2.5, *errs.After, 1e-9)
	s.InDelta(0.4, *errs.Baseline, 1e-9)
	s.InDelta(2.1, *errs.Delta, 1e-9)
	s.InDelta(525, *errs.DeltaPct, 1e-9)
	s.False(errs.HigherIsBetter)
	s.Equal(domain.ImpactWorse, errs.Verdict)
	s.Equal(domain.ImpactNeutral, s.signal(impact, "http.requests").Verdict)
}

func (s *ImpactSuite) TestLatencyIsJudgedByItsP95() {
	s.signals.values[domain.SignalHTTPLatency] = stepAt(s.deployedAt, 200, 150)

	impact, err := s.svc.ForRelease(s.ctx, s.release())
	s.Require().NoError(err)

	lat := s.signal(impact, domain.SignalHTTPLatency)
	s.InDelta(200, *lat.Before, 1e-9)
	s.InDelta(150, *lat.After, 1e-9)
	s.InDelta(-25, *lat.DeltaPct, 1e-9)
	s.Equal(domain.ImpactImproved, lat.Verdict)
	s.Equal(domain.ImpactNeutral, impact.Verdict, "the worst of improved and neutral is neutral")
}

func (s *ImpactSuite) TestThroughputThatDropsIsWorseBecauseHigherIsBetter() {
	s.signals.values["http.requests"] = stepAt(s.deployedAt, 100, 60)

	impact, err := s.svc.ForRelease(s.ctx, s.release())
	s.Require().NoError(err)

	req := s.signal(impact, "http.requests")
	s.True(req.HigherIsBetter)
	s.Equal(domain.ImpactWorse, req.Verdict)
}

func (s *ImpactSuite) TestAMoveYesterdaysSameHourAlsoShowsIsTheTimeOfDay() {
	daily := func(t time.Time) float64 {
		if h := t.Hour(); h == 12 || (h == 13 && t.Minute() == 0) {
			return 60
		}
		return 100
	}
	s.signals.values["http.requests"] = daily

	impact, err := s.svc.ForRelease(s.ctx, s.release())
	s.Require().NoError(err)

	req := s.signal(impact, "http.requests")
	s.InDelta(60, *req.Baseline, 1e-9)
	s.Equal(domain.ImpactNeutral, req.Verdict)
}

func (s *ImpactSuite) TestChangesInsideTheThresholdsAreNeutral() {
	tests := []struct {
		name   string
		signal string
		before float64
		after  float64
	}{
		{"error rate under half a point", domain.SignalHTTPErrorRatio, 0.4, 0.8},
		{"latency under ten percent", domain.SignalHTTPLatency, 200, 215},
		{"throughput under ten percent", "http.requests", 100, 95},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.SetupTest()
			s.signals.values[tt.signal] = stepAt(s.deployedAt, tt.before, tt.after)
			impact, err := s.svc.ForRelease(s.ctx, s.release())
			s.Require().NoError(err)
			s.Equal(domain.ImpactNeutral, s.signal(impact, tt.signal).Verdict)
		})
	}
}

func (s *ImpactSuite) TestThresholdsAreConfigurable() {
	svc := deployimpact.New(deployimpact.Deps{
		Signals:    s.signals,
		Components: fakeComponents{s.component.ID: s.component},
		Thresholds: domain.DeployImpactConfig{ChangePct: 5, RatioPoints: 0.2},
		Clock:      func() time.Time { return s.now },
	})
	s.signals.values[domain.SignalHTTPErrorRatio] = stepAt(s.deployedAt, 0.4, 0.7)

	impact, err := svc.ForRelease(s.ctx, s.release())
	s.Require().NoError(err)

	s.Equal(domain.ImpactWorse, s.signal(impact, domain.SignalHTTPErrorRatio).Verdict)
}

func (s *ImpactSuite) TestAnOpenAfterWindowIsPendingAndReadsWhatHasPassed() {
	s.now = s.deployedAt.Add(20 * time.Minute)
	s.signals.values[domain.SignalHTTPErrorRatio] = stepAt(s.deployedAt, 0.4, 3)

	impact, err := s.svc.ForRelease(s.ctx, s.release())
	s.Require().NoError(err)

	s.True(impact.Pending)
	s.Equal(domain.ImpactWorse, impact.Verdict)
	s.Equal(20, s.signal(impact, domain.SignalHTTPErrorRatio).AfterPoints)
	reads := s.signals.readsOf(domain.SignalHTTPErrorRatio)
	s.Require().Len(reads, 2)
	s.Equal(s.deployedAt.Add(-time.Hour), reads[0].from)
	s.Equal(s.now, reads[0].to)
	s.Equal(s.deployedAt.Add(-24*time.Hour), reads[1].from)
	s.Equal(s.now.Add(-24*time.Hour), reads[1].to, "the baseline covers as much of yesterday's hour as has passed today")
}

func (s *ImpactSuite) TestOnlyBoundSignalsAreListedAndAnUnreadableOneIsUnknown() {
	s.signals.unbound["http.requests"] = true
	s.signals.readErr[domain.SignalHTTPLatency] = true
	s.signals.values[domain.SignalHTTPErrorRatio] = stepAt(s.deployedAt, 0.4, 0.4)

	impact, err := s.svc.ForRelease(s.ctx, s.release())
	s.Require().NoError(err)

	s.Len(impact.Signals, 2)
	_, listed := impact.Signal("http.requests")
	s.False(listed)
	lat := s.signal(impact, domain.SignalHTTPLatency)
	s.Equal(domain.ImpactUnknown, lat.Verdict)
	s.Nil(lat.Before)
	s.Equal(domain.ImpactNeutral, impact.Verdict, "an unreadable signal does not make the whole release unknown")
}

func (s *ImpactSuite) TestNothingToJudgeIsUnknownWithNoSignals() {
	other := domain.Component{ID: uuid.New(), Role: domain.Fact[domain.ComponentRole]{Override: ptr(domain.ComponentRoleLibrary)}}
	tests := []struct {
		name    string
		release func() domain.Release
		prepare func()
	}{
		{"every golden signal unbound", s.release, func() {
			for id := range catalog {
				s.signals.unbound[id] = true
			}
		}},
		{"never deployed", func() domain.Release { r := s.release(); r.DeployedAt = nil; return r }, func() {}},
		{"no component", func() domain.Release { r := s.release(); r.ComponentID = nil; return r }, func() {}},
		{"component gone", func() domain.Release { r := s.release(); id := uuid.New(); r.ComponentID = &id; return r }, func() {}},
		{"role with no golden signals", func() domain.Release { r := s.release(); r.ComponentID = &other.ID; return r }, func() {}},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.SetupTest()
			s.svc = deployimpact.New(deployimpact.Deps{
				Signals:    s.signals,
				Components: fakeComponents{s.component.ID: s.component, other.ID: other},
				Clock:      func() time.Time { return s.now },
			})
			tt.prepare()
			impact, err := s.svc.ForRelease(s.ctx, tt.release())
			s.Require().NoError(err)
			s.Equal(domain.ImpactUnknown, impact.Verdict)
			s.NotNil(impact.Signals)
			s.Empty(impact.Signals)
		})
	}
}

func (s *ImpactSuite) TestAFinishedReleaseWithNoDeployIsAnchoredAtItsFinish() {
	r := s.release()
	r.DeployedAt = nil
	r.FinishedAt = &s.deployedAt

	impact, err := s.svc.ForRelease(s.ctx, r)
	s.Require().NoError(err)

	s.Require().NotNil(impact.DeployedAt)
	s.Equal(s.deployedAt, *impact.DeployedAt)
	s.NotEmpty(impact.Signals)
}

func (s *ImpactSuite) TestAnswersAreReusedWithinAMinute() {
	r := s.release()
	_, err := s.svc.ForRelease(s.ctx, r)
	s.Require().NoError(err)
	_, err = s.svc.ForRelease(s.ctx, r)
	s.Require().NoError(err)
	s.Equal(6, s.signals.readCount(), "one current and one baseline read per signal, then the cached answer")

	s.now = s.now.Add(time.Minute)
	_, err = s.svc.ForRelease(s.ctx, r)
	s.Require().NoError(err)
	s.Equal(12, s.signals.readCount())
}

func (s *ImpactSuite) TestAFailingReadIsAnError() {
	s.signals.err = errors.New("bindings store unavailable")
	_, err := s.svc.ForRelease(s.ctx, s.release())
	s.ErrorContains(err, "bindings store unavailable")
}

func (s *ImpactSuite) TestFromZeroAnyChangeCounts() {
	s.signals.values["http.requests"] = stepAt(s.deployedAt, 0, 5)
	impact, err := s.svc.ForRelease(s.ctx, s.release())
	s.Require().NoError(err)

	req := s.signal(impact, "http.requests")
	s.Nil(req.DeltaPct)
	s.Equal("5", strconv.FormatFloat(*req.Delta, 'f', -1, 64))
	s.Equal(domain.ImpactImproved, req.Verdict)
}

func (s *ImpactSuite) TestTheSampleAtTheDeployIsNotPartOfTheHourAfter() {
	s.signals.values[domain.SignalHTTPErrorRatio] = func(t time.Time) float64 {
		if t.Equal(s.deployedAt) {
			return 9
		}
		return 0.4
	}

	impact, err := s.svc.ForRelease(s.ctx, s.release())
	s.Require().NoError(err)

	errs := s.signal(impact, domain.SignalHTTPErrorRatio)
	s.InDelta(0.4, *errs.After, 1e-9)
	s.Equal(60, errs.AfterPoints)
	s.Equal(domain.ImpactNeutral, errs.Verdict)
}

func (s *ImpactSuite) TestOnlyTheP95OfAQuantileSignalIsRead() {
	_, err := s.svc.ForRelease(s.ctx, s.release())
	s.Require().NoError(err)

	for _, r := range s.signals.readsOf(domain.SignalHTTPLatency) {
		s.Equal(0.95, r.quantile)
	}
	for _, r := range s.signals.readsOf("http.requests") {
		s.Zero(r.quantile)
	}
}

func (s *ImpactSuite) TestTheWorstSeriesIsWhatASignalIsJudgedBy() {
	s.signals.extra[domain.SignalHTTPLatency] = []func(time.Time) float64{stepAt(s.deployedAt, 200, 400)}

	impact, err := s.svc.ForRelease(s.ctx, s.release())
	s.Require().NoError(err)

	lat := s.signal(impact, domain.SignalHTTPLatency)
	s.InDelta(400, *lat.After, 1e-9)
	s.Equal(domain.ImpactWorse, lat.Verdict)
}

func (s *ImpactSuite) TestMovesUnderTheAbsoluteFloorsAreNeutral() {
	worker := domain.Component{ID: uuid.New(), Role: domain.Fact[domain.ComponentRole]{Override: ptr(domain.ComponentRoleWorker)}}
	tests := []struct {
		name   string
		worker bool
		signal string
		before float64
		after  float64
		want   domain.ImpactVerdict
	}{
		{"half a dead letter from none", true, "queue.dead_letter", 0, 0.5, domain.ImpactNeutral},
		{"three dead letters from none", true, "queue.dead_letter", 0, 3, domain.ImpactWorse},
		{"a few errors a minute more", true, "process.errors", 0.01, 0.04, domain.ImpactNeutral},
		{"errors every second", true, "process.errors", 0.01, 1, domain.ImpactWorse},
		{"p95 up 40% but 40 ms", false, domain.SignalHTTPLatency, 100, 140, domain.ImpactNeutral},
		{"p95 up 60 ms", false, domain.SignalHTTPLatency, 100, 160, domain.ImpactWorse},
		{"throughput down 20%", false, "http.requests", 100, 80, domain.ImpactNeutral},
		{"throughput down 30%", false, "http.requests", 100, 70, domain.ImpactWorse},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.SetupTest()
			s.svc = deployimpact.New(deployimpact.Deps{
				Signals:    s.signals,
				Components: fakeComponents{s.component.ID: s.component, worker.ID: worker},
				Clock:      func() time.Time { return s.now },
			})
			s.signals.values[tt.signal] = stepAt(s.deployedAt, tt.before, tt.after)
			r := s.release()
			if tt.worker {
				r.ComponentID = &worker.ID
			}
			impact, err := s.svc.ForRelease(s.ctx, r)
			s.Require().NoError(err)
			s.Equal(tt.want, s.signal(impact, tt.signal).Verdict)
		})
	}
}

func (s *ImpactSuite) TestTheFloorsAreConfigurable() {
	svc := deployimpact.New(deployimpact.Deps{
		Signals:    s.signals,
		Components: fakeComponents{s.component.ID: s.component},
		Thresholds: domain.DeployImpactConfig{LatencyFloorMS: 10, ThroughputChangePct: 10},
		Clock:      func() time.Time { return s.now },
	})
	s.signals.values[domain.SignalHTTPLatency] = stepAt(s.deployedAt, 100, 140)
	s.signals.values["http.requests"] = stepAt(s.deployedAt, 100, 85)

	impact, err := svc.ForRelease(s.ctx, s.release())
	s.Require().NoError(err)

	s.Equal(domain.ImpactWorse, s.signal(impact, domain.SignalHTTPLatency).Verdict)
	s.Equal(domain.ImpactWorse, s.signal(impact, "http.requests").Verdict)
	s.Equal(10.0, svc.Thresholds().LatencyFloorMS)
	s.Equal(float64(domain.DefaultImpactCountFloor), svc.Thresholds().CountFloor)
}

func (s *ImpactSuite) TestAnAnswerWithAnUnreadableSignalIsNotReused() {
	s.signals.readErr[domain.SignalHTTPLatency] = true
	r := s.release()
	_, err := s.svc.ForRelease(s.ctx, r)
	s.Require().NoError(err)
	_, err = s.svc.ForRelease(s.ctx, r)
	s.Require().NoError(err)
	s.Equal(12, s.signals.readCount())
}

func (s *ImpactSuite) TestAFreshReadSkipsTheMinutesCachedAnswer() {
	r := s.release()
	_, err := s.svc.ForRelease(s.ctx, r)
	s.Require().NoError(err)
	_, err = s.svc.ForReleaseFresh(s.ctx, r)
	s.Require().NoError(err)
	s.Equal(12, s.signals.readCount(), "the soak's last read goes to the sources")
	_, err = s.svc.ForRelease(s.ctx, r)
	s.Require().NoError(err)
	s.Equal(12, s.signals.readCount(), "and leaves its answer cached for the minute")
}
