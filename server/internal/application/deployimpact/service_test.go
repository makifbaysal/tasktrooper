package deployimpact_test

import (
	"context"
	"errors"
	"strconv"
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
}

// fakeSignals answers each signal from a function of time, so a window's mean
// is whatever the test says that stretch of time held. Unbound signals and
// read errors are per signal, the way signals.Service reports them.
type fakeSignals struct {
	values  map[string]func(time.Time) float64
	unbound map[string]bool
	readErr map[string]bool
	err     error
	queries []signals.SeriesQuery
}

func (f *fakeSignals) Series(_ context.Context, _ uuid.UUID, q signals.SeriesQuery) (domain.ComponentSeries, error) {
	f.queries = append(f.queries, q)
	if f.err != nil {
		return domain.ComponentSeries{}, f.err
	}
	out := domain.ComponentSeries{From: q.From, To: q.To}
	for _, id := range q.Signals {
		ss := domain.SignalSeries{Signal: id, Unit: catalog[id].Unit, Bound: !f.unbound[id]}
		switch {
		case !ss.Bound:
		case f.readErr[id]:
			ss.Error = &domain.SignalReadError{Code: domain.SignalErrTimeout, Message: "no answer"}
		default:
			var points []domain.SeriesPoint
			for t := q.From; !t.After(q.To); t = t.Add(q.Step) {
				points = append(points, domain.FinitePoint(t, f.values[id](t)))
			}
			if catalog[id].Aggregation == domain.AggregationQuantile {
				ss.Series = []domain.Series{
					{Name: "p50", Labels: map[string]string{"quantile": "0.5"}, Points: scaled(points, 0.5)},
					{Name: "p95", Labels: map[string]string{"quantile": "0.95"}, Points: points},
				}
			} else {
				ss.Series = []domain.Series{{Points: points}}
			}
		}
		out.Signals = append(out.Signals, ss)
	}
	return out, nil
}

func (f *fakeSignals) GoldenSignals(role domain.ComponentRole) []string {
	if role != domain.ComponentRoleBackend {
		return nil
	}
	return []string{"http.requests", domain.SignalHTTPErrorRatio, domain.SignalHTTPLatency}
}

func (f *fakeSignals) Signal(id string) (domain.Signal, bool) {
	sig, ok := catalog[id]
	return sig, ok
}

func scaled(points []domain.SeriesPoint, by float64) []domain.SeriesPoint {
	out := make([]domain.SeriesPoint, len(points))
	for i, p := range points {
		out[i] = domain.FinitePoint(p.Time, *p.Value*by)
	}
	return out
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
		},
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
	s.Require().Len(s.signals.queries, 2)
	s.Equal(s.deployedAt.Add(-time.Hour), s.signals.queries[0].From)
	s.Equal(s.now, s.signals.queries[0].To)
	s.Equal(s.deployedAt.Add(-24*time.Hour), s.signals.queries[1].From)
	s.Equal(s.now.Add(-24*time.Hour), s.signals.queries[1].To, "the baseline covers as much of yesterday's hour as has passed today")
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
	s.Len(s.signals.queries, 2, "one current and one baseline read, then the cached answer")

	s.now = s.now.Add(time.Minute)
	_, err = s.svc.ForRelease(s.ctx, r)
	s.Require().NoError(err)
	s.Len(s.signals.queries, 4)
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
