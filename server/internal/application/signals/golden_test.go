package signals_test

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"

	"github.com/makifbaysal/tasktrooper/server/internal/application/signals"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func (s *SignalsSuite) goldenWorld(components []domain.Component, confirmed []domain.SignalBinding) {
	s.bindings.On("ListConfirmedSignalBindings", mock.Anything, domain.EnvironmentProduction).Return(confirmed, nil).Once()
	s.comps.On("ListAllComponents", mock.Anything).Return(components, nil).Once()
}

func (s *SignalsSuite) TestGoldenAnswersValuePreviousAndSparkPerGoldenSignal() {
	api := s.component(domain.ComponentRoleBackend, "api")
	idle := s.component(domain.ComponentRoleBackend, "idle")
	gone := s.component(domain.ComponentRoleBackend, "gone")
	gone.Status = domain.ComponentStatusDismissed
	requests := s.binding(api, "http.requests", domain.SignalBindingConfirmed, map[string]string{"job": "api"})
	latency := s.binding(api, "http.latency", domain.SignalBindingConfirmed, map[string]string{"job": "api"})
	s.goldenWorld([]domain.Component{api, idle, gone}, []domain.SignalBinding{requests, latency, s.binding(gone, "http.requests", domain.SignalBindingConfirmed, map[string]string{"job": "gone"})})

	from, span, step := s.now.Add(-time.Hour), time.Hour, 2*time.Minute
	var values []*float64
	for i := 0; i <= 60; i++ {
		values = append(values, value(float64(i)))
	}
	values[60] = nil
	s.answers(mock.MatchedBy(func(q domain.SeriesQuery) bool {
		return strings.HasPrefix(q.Query, "sum(rate(") && q.From.Equal(from.Add(-span)) && q.To.Equal(s.now) && q.Step == step
	}), domain.Series{Points: points(from.Add(-span), step, values...)}).Once()
	s.answers(queryContaining("histogram_quantile(0.95,"), domain.Series{Points: points(from, step, value(250))}).Once()

	got, err := s.svc.Golden(s.ctx, signals.GoldenQuery{Environment: domain.EnvironmentProduction, From: from.Add(20 * time.Second), To: s.now.Add(30 * time.Second)})
	s.Require().NoError(err)
	s.True(got.GeneratedAt.Equal(s.now))
	s.Require().Len(got.Components, 1)
	gc := got.Components[0]
	s.Equal(api.ID, gc.ComponentID)
	s.Require().Len(gc.Signals, 3)
	s.Equal([]string{"http.requests", "http.error_ratio", "http.latency"}, []string{gc.Signals[0].Signal, gc.Signals[1].Signal, gc.Signals[2].Signal})

	req := gc.Signals[0]
	s.Equal("req/s", req.Unit)
	s.Len(req.Spark, 31)
	s.True(req.Spark[0].Time.Equal(from))
	s.Require().NotNil(req.Value)
	s.Equal(59.0, *req.Value)
	s.Require().NotNil(req.Previous)
	s.Equal(29.0, *req.Previous)

	ratio := gc.Signals[1]
	s.Nil(ratio.Value)
	s.Nil(ratio.Previous)
	s.Equal([]domain.SeriesPoint{}, ratio.Spark)

	s.Equal(250.0, *gc.Signals[2].Value)
	s.Nil(gc.Signals[2].Previous)
}

func (s *SignalsSuite) TestGoldenKeepsAnAnswerForAMinuteUntilABindingChanges() {
	api := s.component(domain.ComponentRoleBackend, "api")
	requests := s.binding(api, "http.requests", domain.SignalBindingConfirmed, map[string]string{"job": "api"})
	q := signals.GoldenQuery{Environment: domain.EnvironmentProduction, ComponentIDs: []uuid.UUID{api.ID}}

	s.goldenWorld([]domain.Component{api}, []domain.SignalBinding{requests})
	s.answers(mock.Anything, domain.Series{Points: points(s.now, time.Minute, value(1))}).Once()
	first, err := s.svc.Golden(s.ctx, q)
	s.Require().NoError(err)

	s.now = s.now.Add(30 * time.Second)
	again, err := s.svc.Golden(s.ctx, q)
	s.Require().NoError(err)
	s.Equal(first, again)

	s.now = s.now.Add(31 * time.Second)
	s.goldenWorld([]domain.Component{api}, []domain.SignalBinding{requests})
	s.answers(mock.Anything, domain.Series{}).Once()
	_, err = s.svc.Golden(s.ctx, q)
	s.Require().NoError(err)

	s.bindings.On("DeleteSignalBinding", mock.Anything, requests.ID).Return(nil).Once()
	s.Require().NoError(s.svc.Delete(s.ctx, requests.ID))
	s.goldenWorld([]domain.Component{api}, nil)
	after, err := s.svc.Golden(s.ctx, q)
	s.Require().NoError(err)
	s.Empty(after.Components)
}

func (s *SignalsSuite) TestGoldenAnswersAtMostTwoHundredComponentsByName() {
	var comps []domain.Component
	var confirmed []domain.SignalBinding
	for i := 0; i < signals.MaxGoldenComponents+1; i++ {
		c := s.component(domain.ComponentRoleBackend, fmt.Sprintf("svc-%03d", i))
		comps = append(comps, c)
		confirmed = append(confirmed, s.binding(c, "http.requests", domain.SignalBindingConfirmed, map[string]string{"job": c.Path}))
	}
	s.goldenWorld(comps, confirmed)
	s.answers(mock.Anything).Times(signals.MaxGoldenComponents)

	got, err := s.svc.Golden(s.ctx, signals.GoldenQuery{Environment: domain.EnvironmentProduction})
	s.Require().NoError(err)
	s.Require().Len(got.Components, signals.MaxGoldenComponents)
	s.Equal(comps[0].ID, got.Components[0].ComponentID)
	s.Equal(comps[signals.MaxGoldenComponents-1].ID, got.Components[signals.MaxGoldenComponents-1].ComponentID)
}

func (s *SignalsSuite) TestGoldenRefusesAnUnknownEnvironment() {
	_, err := s.svc.Golden(s.ctx, signals.GoldenQuery{Environment: "prod"})
	s.ErrorIs(err, signals.ErrInvalidInput)
}
