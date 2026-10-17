package signals_test

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"

	"github.com/makifbaysal/tasktrooper/server/internal/application/signals"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// listedSource is listed like any other but has no store expectation of
// its own: reading its row or its secrets fails the test.
func (s *SignalsSuite) listedSource() domain.DataSource {
	ds := domain.DataSource{ID: uuid.New(), Kind: domain.DataSourceKindPrometheus, Name: "counted", UpdatedAt: s.now.Add(-time.Hour)}
	s.sources = append(s.sources, ds)
	return ds
}

// countedSource is a listedSource whose secrets may be read once.
func (s *SignalsSuite) countedSource(secretsErr error) domain.DataSource {
	ds := s.listedSource()
	s.dsStore.On("DataSourceSecrets", mock.Anything, ds.ID).Return(map[string]string{}, secretsErr).Once()
	return ds
}

func (s *SignalsSuite) on(ds domain.DataSource, b domain.SignalBinding) domain.SignalBinding {
	b.DataSourceID = ds.ID
	return b
}

func (s *SignalsSuite) TestASeriesReadLoadsEachSourceOnce() {
	ds := s.countedSource(nil)
	api := s.component(domain.ComponentRoleBackend, "api")
	s.stored(api.ID, domain.EnvironmentProduction,
		s.on(ds, s.binding(api, "http.requests", domain.SignalBindingConfirmed, map[string]string{"job": "api"})),
		s.on(ds, s.binding(api, "http.error_ratio", domain.SignalBindingConfirmed, map[string]string{"job": "api"})),
		s.on(ds, s.binding(api, "http.latency", domain.SignalBindingConfirmed, map[string]string{"job": "api"})),
	)
	s.querier.On("Series", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(fresh(domain.Series{Name: "x"})).Times(12)

	got, err := s.svc.Series(s.ctx, api.ID, signals.SeriesQuery{
		Environment: domain.EnvironmentProduction, Signals: []string{"http.requests", "http.error_ratio", "http.latency"}, Compare: true,
	})

	s.Require().NoError(err)
	for _, sig := range got.Signals {
		s.Nil(sig.Error, sig.Signal)
		s.NotEmpty(sig.Series, sig.Signal)
	}
	s.dsStore.AssertNotCalled(s.T(), "GetDataSource", mock.Anything, ds.ID)
	s.recipes.AssertNumberOfCalls(s.T(), "ListUserRecipes", 1)
}

func (s *SignalsSuite) TestListingBindingsRendersThemWithoutReadingAnySource() {
	ds := s.listedSource()
	api := s.component(domain.ComponentRoleBackend, "api")
	s.stored(api.ID, domain.EnvironmentProduction,
		s.on(ds, s.binding(api, "http.requests", domain.SignalBindingConfirmed, map[string]string{"job": "api"})),
		s.on(ds, s.binding(api, "http.latency", domain.SignalBindingSuggested, map[string]string{"job": "api"})),
	)

	got, err := s.svc.List(s.ctx, api.ID, domain.EnvironmentProduction)

	s.Require().NoError(err)
	s.Equal(`sum(rate(`+httpMetric+`{job="api"}[120s]))`, signalByID(got, "http.requests").Binding.RenderedQuery)
	s.Contains(signalByID(got, "http.latency").Suggestions[0].RenderedQuery, "histogram_quantile(0.95,")
	s.dsStore.AssertNotCalled(s.T(), "GetDataSource", mock.Anything, mock.Anything)
	s.dsStore.AssertNotCalled(s.T(), "DataSourceSecrets", mock.Anything, mock.Anything)
	s.recipes.AssertNumberOfCalls(s.T(), "ListUserRecipes", 1)
}

func (s *SignalsSuite) TestAGoldenReadLoadsEachSourceOnce() {
	ds := s.countedSource(nil)
	var comps []domain.Component
	var confirmed []domain.SignalBinding
	for _, name := range []string{"api", "billing", "web"} {
		c := s.component(domain.ComponentRoleBackend, name)
		comps = append(comps, c)
		confirmed = append(confirmed, s.on(ds, s.binding(c, "http.requests", domain.SignalBindingConfirmed, map[string]string{"job": name})))
	}
	s.goldenWorld(comps, confirmed)
	s.querier.On("Series", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(fresh(domain.Series{Points: points(s.now, time.Minute, value(1))})).Times(3)

	got, err := s.svc.Golden(s.ctx, signals.GoldenQuery{Environment: domain.EnvironmentProduction})

	s.Require().NoError(err)
	for _, c := range got.Components {
		s.NotNil(c.Signals[0].Value)
	}
	s.dsStore.AssertNotCalled(s.T(), "GetDataSource", mock.Anything, ds.ID)
}

func (s *SignalsSuite) TestASignalThisServerFailedToReadAnswersNoDetail() {
	ds := s.countedSource(errors.New("decrypt data source secrets: cipher key 7f3a9 mismatch"))
	api := s.component(domain.ComponentRoleBackend, "api")
	s.stored(api.ID, domain.EnvironmentProduction,
		s.on(ds, s.binding(api, "http.requests", domain.SignalBindingConfirmed, map[string]string{"job": "api"})))

	got, err := s.svc.Series(s.ctx, api.ID, signals.SeriesQuery{Environment: domain.EnvironmentProduction, Signals: []string{"http.requests"}})

	s.Require().NoError(err)
	s.Require().NotNil(got.Signals[0].Error)
	s.Equal(domain.SignalErrInternal, got.Signals[0].Error.Code)
	s.Equal("reading the signal failed", got.Signals[0].Error.Message)
}

// fresh answers a new result per call, as an adapter does: the service
// fills in a series' nil fields in place, so one shared value would race.
func fresh(series ...domain.Series) func(context.Context, domain.DataSource, map[string]string, domain.SeriesQuery) (domain.SeriesResult, error) {
	return func(context.Context, domain.DataSource, map[string]string, domain.SeriesQuery) (domain.SeriesResult, error) {
		return domain.SeriesResult{Series: slices.Clone(series)}, nil
	}
}
