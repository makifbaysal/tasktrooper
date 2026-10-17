package signals_test

import (
	"context"
	"time"

	"github.com/stretchr/testify/mock"

	"github.com/makifbaysal/tasktrooper/server/internal/application/signals"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type tenantKey struct{}

func (s *SignalsSuite) scoped(tenant string) context.Context {
	return context.WithValue(s.ctx, tenantKey{}, tenant)
}

func (s *SignalsSuite) scopeByTenant() {
	s.svc.SetCacheScope(func(ctx context.Context) string {
		tenant, _ := ctx.Value(tenantKey{}).(string)
		return tenant
	})
}

func (s *SignalsSuite) TestGoldenNeverAnswersOneCacheScopeFromAnothersEntry() {
	s.scopeByTenant()
	api := s.component(domain.ComponentRoleBackend, "api")
	requests := s.binding(api, "http.requests", domain.SignalBindingConfirmed, map[string]string{"job": "api"})
	q := signals.GoldenQuery{Environment: domain.EnvironmentProduction}
	s.goldenWorld([]domain.Component{api}, []domain.SignalBinding{requests})
	s.goldenWorld([]domain.Component{api}, []domain.SignalBinding{requests})
	s.answers(mock.Anything, domain.Series{Points: points(s.now, time.Minute, value(1))}).Once()
	s.answers(mock.Anything, domain.Series{Points: points(s.now, time.Minute, value(2))}).Once()

	a, err := s.svc.Golden(s.scoped("tenant-a"), q)
	s.Require().NoError(err)
	b, err := s.svc.Golden(s.scoped("tenant-b"), q)
	s.Require().NoError(err)
	again, err := s.svc.Golden(s.scoped("tenant-a"), q)
	s.Require().NoError(err)

	s.Equal(1.0, *a.Components[0].Signals[0].Value)
	s.Equal(2.0, *b.Components[0].Signals[0].Value)
	s.Equal(a, again)
}

func (s *SignalsSuite) TestSuggestNeverReusesAnotherCacheScopesDiscovery() {
	s.scopeByTenant()
	api := s.component(domain.ComponentRoleBackend, "checkout")
	for _, tenant := range []string{"tenant-a", "tenant-b"} {
		s.inventory(httpMetric)
		s.noEnvironments(api)
		s.labelProbe(`count by (job) (`+httpMetric+`)`, map[string]string{"job": "web"})
		s.expectSuggestions(api, nil)
		_, err := s.svc.Suggest(s.scoped(tenant), api.ID, domain.EnvironmentProduction)
		s.Require().NoError(err)
	}
}

func (s *SignalsSuite) TestGoldenIsReadAgainOnceASourceOrARecipeChanges() {
	api := s.component(domain.ComponentRoleBackend, "api")
	requests := s.binding(api, "http.requests", domain.SignalBindingConfirmed, map[string]string{"job": "api"})
	q := signals.GoldenQuery{Environment: domain.EnvironmentProduction}
	read := func() {
		s.goldenWorld([]domain.Component{api}, []domain.SignalBinding{requests})
		s.answers(mock.Anything, domain.Series{Points: points(s.now, time.Minute, value(1))}).Once()
		_, err := s.svc.Golden(s.ctx, q)
		s.Require().NoError(err)
	}
	read()
	_, err := s.svc.Golden(s.ctx, q)
	s.Require().NoError(err)

	s.sources[0].UpdatedAt = s.now
	read()

	s.dsStore.On("DeleteDataSource", mock.Anything, s.promSource.ID).Return(nil).Once()
	s.Require().NoError(s.ds.Delete(s.ctx, s.promSource.ID))
	read()

	edited := userRecipes()
	edited[0].Signals["http.requests"] = `sum(rate(` + httpMetric + `{job="$job", le=""}[$__rate]))`
	s.recipes.ExpectedCalls = nil
	s.recipes.On("ListUserRecipes", mock.Anything).Return(edited, nil)
	read()
	_, err = s.svc.Golden(s.ctx, q)
	s.Require().NoError(err)
}

func (s *SignalsSuite) TestADeletedSourceIsDiscoveredAgain() {
	api := s.component(domain.ComponentRoleBackend, "checkout")
	suggest := func() {
		s.noEnvironments(api)
		s.labelProbe(`count by (job) (`+httpMetric+`)`, map[string]string{"job": "web"})
		s.expectSuggestions(api, nil)
		_, err := s.svc.Suggest(s.ctx, api.ID, domain.EnvironmentProduction)
		s.Require().NoError(err)
	}
	s.inventory(httpMetric)
	suggest()
	s.dsStore.On("DeleteDataSource", mock.Anything, s.promSource.ID).Return(nil).Once()
	s.Require().NoError(s.ds.Delete(s.ctx, s.promSource.ID))
	s.inventory(httpMetric)
	suggest()
}
