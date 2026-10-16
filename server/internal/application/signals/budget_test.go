package signals_test

import (
	"context"
	"sync"
	"time"

	"github.com/stretchr/testify/mock"

	"github.com/makifbaysal/tasktrooper/server/internal/application/signals"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func onSource(id domain.DataSource) interface{} {
	return mock.MatchedBy(func(ds domain.DataSource) bool { return ds.ID == id.ID })
}

func untilCancelled(args mock.Arguments) { <-args.Get(0).(context.Context).Done() }

func (s *SignalsSuite) TestGoldenAnswersNullsForWhatIsStillUnreadWhenItsBudgetRunsOut() {
	s.reset(signals.WithGoldenBudget(100*time.Millisecond), signals.WithQueryTimeout(5*time.Second))
	slow := s.addSource(domain.DataSource{Kind: domain.DataSourceKindPrometheus, Name: "slow prom"})
	api := s.component(domain.ComponentRoleBackend, "api")
	web := s.component(domain.ComponentRoleBackend, "web")
	fast := s.binding(api, "http.requests", domain.SignalBindingConfirmed, map[string]string{"job": "api"})
	stuck := s.binding(web, "http.requests", domain.SignalBindingConfirmed, map[string]string{"job": "web"})
	stuck.DataSourceID = slow.ID
	s.goldenWorld([]domain.Component{api, web}, []domain.SignalBinding{fast, stuck})
	s.querier.On("Series", mock.Anything, onSource(s.promSource), mock.Anything, mock.Anything).
		Return(domain.SeriesResult{Series: []domain.Series{{Points: points(s.now, time.Minute, value(7))}}}, nil).Once()
	s.querier.On("Series", mock.Anything, onSource(slow), mock.Anything, mock.Anything).
		Run(untilCancelled).Return(domain.SeriesResult{}, context.Canceled).Once()

	started := time.Now()
	got, err := s.svc.Golden(s.ctx, signals.GoldenQuery{Environment: domain.EnvironmentProduction})

	s.Require().NoError(err)
	s.Less(time.Since(started), 2*time.Second)
	s.Require().Len(got.Components, 2)
	s.Equal(7.0, *got.Components[0].Signals[0].Value)
	s.True(got.Components[1].Signals[0].Bound)
	s.Nil(got.Components[1].Signals[0].Value)
}

func (s *SignalsSuite) TestSeriesAnswersATimeoutForWhatIsStillUnreadWhenItsBudgetRunsOut() {
	s.reset(signals.WithSeriesBudget(100*time.Millisecond), signals.WithQueryTimeout(5*time.Second), signals.WithConcurrency(1))
	api := s.component(domain.ComponentRoleBackend, "api")
	s.stored(api.ID, domain.EnvironmentProduction,
		s.binding(api, "http.requests", domain.SignalBindingConfirmed, map[string]string{"job": "api"}),
		s.binding(api, "http.error_ratio", domain.SignalBindingConfirmed, map[string]string{"job": "api"}),
	)
	s.querier.On("Series", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Run(untilCancelled).Return(domain.SeriesResult{}, context.Canceled).Once()

	started := time.Now()
	got, err := s.svc.Series(s.ctx, api.ID, signals.SeriesQuery{
		Environment: domain.EnvironmentProduction, Signals: []string{"http.requests", "http.error_ratio"},
	})

	s.Require().NoError(err)
	s.Less(time.Since(started), 2*time.Second)
	for _, sig := range got.Signals {
		s.Require().NotNil(sig.Error, sig.Signal)
		s.Equal(domain.SignalErrTimeout, sig.Error.Code)
		s.Contains(sig.Error.Message, "100ms")
	}
}

func (s *SignalsSuite) TestSeriesStopsAskingASourceThatTimedOut() {
	s.reset(signals.WithQueryTimeout(50*time.Millisecond), signals.WithConcurrency(1))
	other := s.addSource(domain.DataSource{Kind: domain.DataSourceKindPrometheus, Name: "other prom"})
	api := s.component(domain.ComponentRoleBackend, "api")
	elsewhere := s.binding(api, "infra.cpu", domain.SignalBindingConfirmed, nil)
	elsewhere.DataSourceID, elsewhere.RecipeID, elsewhere.Query = other.ID, "", "sum(up)"
	s.stored(api.ID, domain.EnvironmentProduction,
		s.binding(api, "http.requests", domain.SignalBindingConfirmed, map[string]string{"job": "api"}),
		s.binding(api, "http.error_ratio", domain.SignalBindingConfirmed, map[string]string{"job": "api"}),
		s.binding(api, "http.latency", domain.SignalBindingConfirmed, map[string]string{"job": "api"}),
		elsewhere,
	)
	s.querier.On("Series", mock.Anything, onSource(s.promSource), mock.Anything, mock.Anything).
		Run(untilCancelled).Return(domain.SeriesResult{}, context.Canceled).Once()
	s.querier.On("Series", mock.Anything, onSource(other), mock.Anything, mock.Anything).
		Return(domain.SeriesResult{Series: []domain.Series{{Name: "cpu"}}}, nil).Once()

	got, err := s.svc.Series(s.ctx, api.ID, signals.SeriesQuery{
		Environment: domain.EnvironmentProduction, Signals: []string{"http.requests", "http.error_ratio", "http.latency", "infra.cpu"},
	})

	s.Require().NoError(err)
	s.Require().NotNil(got.Signals[0].Error)
	s.Equal(domain.SignalErrTimeout, got.Signals[0].Error.Code)
	s.Contains(got.Signals[0].Error.Message, "50ms")
	for _, skipped := range got.Signals[1:3] {
		s.Require().NotNil(skipped.Error, skipped.Signal)
		s.Equal(domain.SignalErrTimeout, skipped.Error.Code)
		s.Contains(skipped.Error.Message, "skipped")
	}
	s.Nil(got.Signals[3].Error)
	s.Len(got.Signals[3].Series, 1)
}

func (s *SignalsSuite) TestPollsAskingTheSameGoldenAtOnceShareOneRead() {
	api := s.component(domain.ComponentRoleBackend, "api")
	requests := s.binding(api, "http.requests", domain.SignalBindingConfirmed, map[string]string{"job": "api"})
	s.goldenWorld([]domain.Component{api}, []domain.SignalBinding{requests})
	release := make(chan struct{})
	s.querier.On("Series", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Run(func(mock.Arguments) { <-release }).
		Return(domain.SeriesResult{Series: []domain.Series{{Points: points(s.now, time.Minute, value(3))}}}, nil).Once()

	const polls = 5
	boards := make([]domain.GoldenBoard, polls)
	errs := make([]error, polls)
	var wg sync.WaitGroup
	for i := range polls {
		wg.Add(1)
		go func() {
			defer wg.Done()
			boards[i], errs[i] = s.svc.Golden(s.ctx, signals.GoldenQuery{Environment: domain.EnvironmentProduction})
		}()
	}
	time.Sleep(100 * time.Millisecond)
	close(release)
	wg.Wait()

	for i := range polls {
		s.Require().NoError(errs[i])
		s.Equal(3.0, *boards[i].Components[0].Signals[0].Value)
	}
}
