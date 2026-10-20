package signals_test

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"github.com/stretchr/testify/mock"

	"github.com/makifbaysal/tasktrooper/server/internal/application/signals"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

func value(v float64) *float64 { return &v }

func points(from time.Time, step time.Duration, values ...*float64) []domain.SeriesPoint {
	out := make([]domain.SeriesPoint, len(values))
	for i, v := range values {
		out[i] = domain.SeriesPoint{Time: from.Add(time.Duration(i) * step), Value: v}
	}
	return out
}

func queryContaining(fragment string) interface{} {
	return mock.MatchedBy(func(q domain.SeriesQuery) bool { return strings.Contains(q.Query, fragment) })
}

func (s *SignalsSuite) answers(match interface{}, series ...domain.Series) *mock.Call {
	return s.querier.On("Series", mock.Anything, mock.Anything, mock.Anything, match).
		Return(domain.SeriesResult{Series: series}, nil)
}

func (s *SignalsSuite) TestSeriesReadsAQuantileSignalOncePerQuantile() {
	api := s.component(domain.ComponentRoleBackend, "api")
	s.stored(api.ID, domain.EnvironmentProduction, s.binding(api, "http.latency", domain.SignalBindingConfirmed, map[string]string{"job": "api"}))
	from := s.now.Add(-time.Hour)
	for i, q := range []string{"0.5", "0.75", "0.95", "0.99"} {
		s.answers(mock.MatchedBy(func(sq domain.SeriesQuery) bool {
			return strings.Contains(sq.Query, "histogram_quantile("+q+",") && strings.Contains(sq.Query, `{job="api"}[120s]`) &&
				sq.Step == 30*time.Second && sq.From.Equal(from) && sq.To.Equal(s.now)
		}), domain.Series{Labels: map[string]string{}, Points: points(from, time.Minute, value(float64(i)))}).Once()
	}

	got, err := s.svc.Series(s.ctx, api.ID, signals.SeriesQuery{Environment: domain.EnvironmentProduction, Signals: []string{"http.latency"}})
	s.Require().NoError(err)
	s.Equal(int64(30), got.StepSeconds)
	s.True(got.From.Equal(from))
	s.True(got.To.Equal(s.now))
	s.Require().Len(got.Signals, 1)
	lat := got.Signals[0]
	s.Nil(lat.Error)
	s.True(lat.Bound)
	s.Equal("ms", lat.Unit)
	s.Require().Len(lat.Series, 4)
	for i, want := range []struct{ name, q string }{{"p50", "0.5"}, {"p75", "0.75"}, {"p95", "0.95"}, {"p99", "0.99"}} {
		s.Equal(want.name, lat.Series[i].Name)
		s.Equal(want.q, lat.Series[i].Labels["quantile"])
		s.Equal(float64(i), *lat.Series[i].Points[0].Value)
	}
	s.Equal([]domain.Series{}, lat.Previous)
}

func (s *SignalsSuite) TestSeriesComparesWithThePreviousWindowShiftedOntoThisOne() {
	api := s.component(domain.ComponentRoleBackend, "api")
	s.stored(api.ID, domain.EnvironmentProduction, s.binding(api, "http.requests", domain.SignalBindingConfirmed, map[string]string{"job": "api"}))
	from := s.now.Add(-time.Hour)
	s.answers(mock.MatchedBy(func(q domain.SeriesQuery) bool { return q.From.Equal(from) }),
		domain.Series{Name: "now", Points: points(from, time.Minute, value(5))}).Once()
	s.answers(mock.MatchedBy(func(q domain.SeriesQuery) bool { return q.From.Equal(from.Add(-time.Hour)) && q.To.Equal(from) }),
		domain.Series{Name: "before", Points: points(from.Add(-time.Hour), time.Minute, value(3), nil)}).Once()

	got, err := s.svc.Series(s.ctx, api.ID, signals.SeriesQuery{
		Environment: domain.EnvironmentProduction, Signals: []string{"http.requests"}, Compare: true,
	})
	s.Require().NoError(err)
	req := got.Signals[0]
	s.Require().Len(req.Series, 1)
	s.Require().Len(req.Previous, 1)
	s.Equal("before", req.Previous[0].Name)
	s.True(req.Previous[0].Points[0].Time.Equal(from))
	s.True(req.Previous[0].Points[1].Time.Equal(from.Add(time.Minute)))
	s.Equal(3.0, *req.Previous[0].Points[0].Value)
	s.Nil(req.Previous[0].Points[1].Value)
}

func (s *SignalsSuite) TestSeriesIsolatesEachSignalsError() {
	api := s.component(domain.ComponentRoleBackend, "api")
	s.stored(api.ID, domain.EnvironmentProduction,
		s.binding(api, "http.requests", domain.SignalBindingConfirmed, map[string]string{"job": "api"}),
		s.binding(api, "http.error_ratio", domain.SignalBindingConfirmed, map[string]string{"job": "api"}),
		s.binding(api, "http.latency", domain.SignalBindingConfirmed, map[string]string{}),
	)
	s.querier.On("Series", mock.Anything, mock.Anything, mock.Anything, mock.MatchedBy(func(q domain.SeriesQuery) bool {
		return strings.HasPrefix(q.Query, "sum(rate("+httpMetric)
	})).
		Return(domain.SeriesResult{}, fmt.Errorf("parse error: %w", port.ErrDataSourceQuery)).Once()
	s.answers(queryContaining("100 * sum"), domain.Series{Name: "ratio", Points: points(s.now, time.Minute, value(1))}).Once()

	got, err := s.svc.Series(s.ctx, api.ID, signals.SeriesQuery{
		Environment: domain.EnvironmentProduction,
		Signals:     []string{"http.requests", "http.error_ratio", "http.latency", "infra.cpu", "http.nope"},
	})
	s.Require().NoError(err)
	byID := map[string]domain.SignalSeries{}
	for _, sig := range got.Signals {
		byID[sig.Signal] = sig
	}
	s.Require().NotNil(byID["http.requests"].Error)
	s.Equal(domain.SignalErrBadQuery, byID["http.requests"].Error.Code)
	s.Equal([]domain.Series{}, byID["http.requests"].Series)
	s.Nil(byID["http.error_ratio"].Error)
	s.Len(byID["http.error_ratio"].Series, 1)
	s.Equal(domain.SignalErrNeedsParams, byID["http.latency"].Error.Code)
	s.False(byID["infra.cpu"].Bound)
	s.Nil(byID["infra.cpu"].Error)
	s.Equal(domain.SignalErrUnknown, byID["http.nope"].Error.Code)
}

func (s *SignalsSuite) TestASourceThatSaysNotEnabledIsAStateNotAFailure() {
	api := s.component(domain.ComponentRoleBackend, "api")
	s.stored(api.ID, domain.EnvironmentProduction,
		s.binding(api, "http.requests", domain.SignalBindingConfirmed, map[string]string{"job": "api"}),
	)
	s.querier.On("Series", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(domain.SeriesResult{}, fmt.Errorf("Web Analytics is not enabled for this project in Vercel: %w", port.ErrSignalNotEnabled)).Once()

	got, err := s.svc.Series(s.ctx, api.ID, signals.SeriesQuery{
		Environment: domain.EnvironmentProduction,
		Signals:     []string{"http.requests"},
	})
	s.Require().NoError(err)
	s.Require().NotNil(got.Signals[0].Error)
	s.Equal(domain.SignalErrNotEnabled, got.Signals[0].Error.Code)
	s.Contains(got.Signals[0].Error.Message, "not enabled for this project")
	s.True(got.Signals[0].Bound)
}

func (s *SignalsSuite) TestSeriesReadsAtMostFourQueriesAtOnce() {
	api := s.component(domain.ComponentRoleBackend, "api")
	var list []domain.SignalBinding
	for _, id := range []string{"http.requests", "http.error_ratio", "http.latency"} {
		list = append(list, s.binding(api, id, domain.SignalBindingConfirmed, map[string]string{"job": "api"}))
	}
	for _, id := range []string{"infra.cpu", "infra.memory", "infra.instances"} {
		b := s.binding(api, id, domain.SignalBindingConfirmed, nil)
		b.RecipeID, b.Query = "", "sum(up)"
		list = append(list, b)
	}
	s.stored(api.ID, domain.EnvironmentProduction, list...)
	var inFlight, peak atomic.Int32
	s.querier.On("Series", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Run(func(mock.Arguments) {
			n := inFlight.Add(1)
			for {
				p := peak.Load()
				if n <= p || peak.CompareAndSwap(p, n) {
					break
				}
			}
			time.Sleep(20 * time.Millisecond)
			inFlight.Add(-1)
		}).
		Return(domain.SeriesResult{}, nil).Times(18)

	got, err := s.svc.Series(s.ctx, api.ID, signals.SeriesQuery{Environment: domain.EnvironmentProduction, Compare: true})
	s.Require().NoError(err)
	bound := 0
	for _, sig := range got.Signals {
		s.Nil(sig.Error, sig.Signal)
		if sig.Bound {
			bound++
		}
	}
	s.Equal(6, bound)
	s.LessOrEqual(peak.Load(), int32(4))
	s.GreaterOrEqual(peak.Load(), int32(2))
}

func (s *SignalsSuite) TestSeriesCutsASlowSignalOffAtItsTimeout() {
	s.reset(signals.WithQueryTimeout(50 * time.Millisecond))
	api := s.component(domain.ComponentRoleBackend, "api")
	s.stored(api.ID, domain.EnvironmentProduction, s.binding(api, "http.requests", domain.SignalBindingConfirmed, map[string]string{"job": "api"}))
	s.querier.On("Series", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Run(func(mock.Arguments) { time.Sleep(300 * time.Millisecond) }).
		Return(domain.SeriesResult{}, nil).Once()

	got, err := s.svc.Series(s.ctx, api.ID, signals.SeriesQuery{Environment: domain.EnvironmentProduction, Signals: []string{"http.requests"}})
	s.Require().NoError(err)
	s.Require().NotNil(got.Signals[0].Error)
	s.Equal(domain.SignalErrTimeout, got.Signals[0].Error.Code)
	time.Sleep(300 * time.Millisecond)
}

func (s *SignalsSuite) TestSeriesRefusesABadWindow() {
	api := s.component(domain.ComponentRoleBackend, "api")
	tests := []struct {
		name string
		q    signals.SeriesQuery
		key  string
	}{
		{"to before from", signals.SeriesQuery{From: s.now, To: s.now.Add(-time.Minute)}, "to"},
		{"too long", signals.SeriesQuery{From: s.now.Add(-100 * 24 * time.Hour), To: s.now}, "from"},
		{"too many points", signals.SeriesQuery{From: s.now.Add(-24 * time.Hour), To: s.now, Step: time.Second}, "step_seconds"},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			tt.q.Environment = domain.EnvironmentProduction
			_, err := s.svc.Series(s.ctx, api.ID, tt.q)
			s.Require().Error(err)
			s.Contains(err.Error(), tt.key)
		})
	}
}

func (s *SignalsSuite) TestPreviewRendersAndRunsAQueryBeforeItIsBound() {
	api := s.component(domain.ComponentRoleBackend, "api")
	from := s.now.Add(-time.Hour)
	for _, q := range []string{"0.5", "0.75", "0.95", "0.99"} {
		s.answers(queryContaining("histogram_quantile("+q+","), domain.Series{Points: points(from, time.Minute, value(1))}).Once()
	}

	got, err := s.svc.Preview(s.ctx, api.ID, signals.PreviewRequest{
		BindingRequest: signals.BindingRequest{DataSourceID: s.promSource.ID.String(), RecipeID: httpRecipe, Params: map[string]string{"job": "api"}},
		Signal:         "http.latency",
	})
	s.Require().NoError(err)
	s.Equal(`1000 * histogram_quantile(0.95, sum by (le) (rate(svc_http_bucket{job="api"}[120s])))`, got.RenderedQuery)
	s.Len(got.Series, 4)
	s.Equal("p75", got.Series[1].Name, "a web vital's p75 is read too")
	s.Equal("p99", got.Series[3].Name)
	s.Equal([]string{}, got.Warnings)
}

func (s *SignalsSuite) TestSeriesReadsACloudWatchQuantileSignalOncePerPercentileStat() {
	api := s.component(domain.ComponentRoleBackend, "api")
	cw := s.addSource(domain.DataSource{Kind: domain.DataSourceKindCloudWatch, Name: "aws"})
	lat := s.binding(api, "http.latency", domain.SignalBindingConfirmed, map[string]string{"load_balancer": "app/web/1"})
	lat.DataSourceID, lat.RecipeID = cw.ID, "aws-alb"
	s.stored(api.ID, domain.EnvironmentProduction, lat)
	for _, stat := range []string{"p50", "p75", "p95", "p99"} {
		s.cwQuerier.On("Series", mock.Anything, mock.Anything, mock.Anything, mock.MatchedBy(func(q domain.SeriesQuery) bool {
			return regexp.MustCompile(`"stat":\s*"`+stat+`"`).MatchString(q.Query) && strings.Contains(q.Query, `"LoadBalancer": "app/web/1"`)
		})).Return(func(context.Context, domain.DataSource, map[string]string, domain.SeriesQuery) (domain.SeriesResult, error) {
			return domain.SeriesResult{Series: []domain.Series{{Name: "t", Points: points(s.now, time.Minute, value(1))}}}, nil
		}).Once()
	}

	got, err := s.svc.Series(s.ctx, api.ID, signals.SeriesQuery{Environment: domain.EnvironmentProduction, Signals: []string{"http.latency"}})
	s.Require().NoError(err)
	s.Require().Nil(got.Signals[0].Error)
	var names []string
	for _, sr := range got.Signals[0].Series {
		names = append(names, sr.Name)
	}
	s.Equal([]string{"p50", "p75", "p95", "p99"}, names)
}

func (s *SignalsSuite) TestSeriesKeepsThisWindowWhenThePreviousOneFails() {
	api := s.component(domain.ComponentRoleBackend, "api")
	s.stored(api.ID, domain.EnvironmentProduction, s.binding(api, "http.requests", domain.SignalBindingConfirmed, map[string]string{"job": "api"}))
	from := s.now.Add(-time.Hour)
	s.answers(mock.MatchedBy(func(q domain.SeriesQuery) bool { return q.From.Equal(from) }),
		domain.Series{Name: "now", Points: points(from, time.Minute, value(5))}).Once()
	s.querier.On("Series", mock.Anything, mock.Anything, mock.Anything, mock.MatchedBy(func(q domain.SeriesQuery) bool { return q.To.Equal(from) })).
		Return(domain.SeriesResult{}, errors.New("query: HTTP 503")).Once()

	got, err := s.svc.Series(s.ctx, api.ID, signals.SeriesQuery{
		Environment: domain.EnvironmentProduction, Signals: []string{"http.requests"}, Compare: true,
	})

	s.Require().NoError(err)
	req := got.Signals[0]
	s.Nil(req.Error)
	s.Require().Len(req.Series, 1)
	s.Equal("now", req.Series[0].Name)
	s.Equal([]domain.Series{}, req.Previous)
	s.Require().NotNil(req.PreviousError)
	s.Equal(domain.SignalErrUpstream, req.PreviousError.Code)
	s.Equal("query: HTTP 503", req.PreviousError.Message)
}

func (s *SignalsSuite) TestSeriesStepIsTheCoarsestSpacingASourceAnsweredAt() {
	api := s.component(domain.ComponentRoleBackend, "api")
	cw := s.addSource(domain.DataSource{Kind: domain.DataSourceKindCloudWatch, Name: "aws"})
	backlog := s.binding(api, "queue.backlog", domain.SignalBindingConfirmed, nil)
	backlog.DataSourceID, backlog.RecipeID = cw.ID, ""
	backlog.Query = `{"namespace": "AWS/SQS", "metric": "ApproximateNumberOfMessagesVisible", "stat": "Maximum", "dimensions": {"QueueName": "orders"}}`
	s.stored(api.ID, domain.EnvironmentProduction,
		s.binding(api, "http.requests", domain.SignalBindingConfirmed, map[string]string{"job": "api"}), backlog)
	s.answers(mock.Anything, domain.Series{Name: "requests"}).Once()
	s.cwQuerier.On("Series", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(domain.SeriesResult{Series: []domain.Series{{Name: "backlog"}}, Step: time.Minute}, nil).Once()

	got, err := s.svc.Series(s.ctx, api.ID, signals.SeriesQuery{
		Environment: domain.EnvironmentProduction, Signals: []string{"http.requests", "queue.backlog"},
	})

	s.Require().NoError(err)
	s.Equal(int64(60), got.StepSeconds)
	for _, sig := range got.Signals {
		s.Nil(sig.Error, sig.Signal)
	}
}
