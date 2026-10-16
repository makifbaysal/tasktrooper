package signals_test

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func (s *SignalsSuite) inventory(metrics ...string) {
	s.discoverer.On("Discover", mock.Anything, mock.MatchedBy(func(ds domain.DataSource) bool { return ds.ID == s.promSource.ID }), mock.Anything).
		Return(domain.Inventory{Metrics: metrics}, nil).Once()
}

func (s *SignalsSuite) labelProbe(query string, labels ...map[string]string) {
	series := make([]domain.Series, 0, len(labels))
	for _, l := range labels {
		series = append(series, domain.Series{Name: "", Labels: l})
	}
	s.querier.On("Series", mock.Anything, mock.Anything, mock.Anything, mock.MatchedBy(func(q domain.SeriesQuery) bool {
		return q.Query == query && q.To.Equal(s.now) && q.From.Equal(s.now.Add(-time.Hour))
	})).Return(domain.SeriesResult{Series: series}, nil).Once()
}
func (s *SignalsSuite) noEnvironments(c domain.Component) {
	s.envs.On("ListEnvironments", mock.Anything, c.RepositoryID).Return([]domain.ComponentEnvironment{}, nil).Once()
}

// expectSuggestions captures what Suggest stores and answers it back on the
// re-read that follows.
func (s *SignalsSuite) expectSuggestions(c domain.Component, existing []domain.SignalBinding) *[]domain.SignalBinding {
	saved, _ := s.expectSuggestionsFrom(c, existing)
	return saved
}

// expectSuggestionsFrom also captures the sources Suggest heard back from.
func (s *SignalsSuite) expectSuggestionsFrom(c domain.Component, existing []domain.SignalBinding) (*[]domain.SignalBinding, *[]uuid.UUID) {
	var saved []domain.SignalBinding
	var answered []uuid.UUID
	s.bindings.On("ListSignalBindings", mock.Anything, c.ID, domain.EnvironmentProduction).Return(existing, nil).Once()
	s.bindings.On("ReplaceDetectedSuggestions", mock.Anything, c.ID, domain.EnvironmentProduction, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			answered = args.Get(3).([]uuid.UUID)
			saved = args.Get(4).([]domain.SignalBinding)
		}).
		Return(func(_ context.Context, _ uuid.UUID, _ domain.DeployEnvironment, _ []uuid.UUID, list []domain.SignalBinding) ([]domain.SignalBinding, error) {
			return list, nil
		}).Once()
	s.bindings.On("ListSignalBindings", mock.Anything, c.ID, domain.EnvironmentProduction).
		Return(func(context.Context, uuid.UUID, domain.DeployEnvironment) ([]domain.SignalBinding, error) {
			return append(append([]domain.SignalBinding{}, existing...), saved...), nil
		}).Once()
	return &saved, &answered
}

func summarizeBindings(list []domain.SignalBinding) []string {
	out := make([]string, 0, len(list))
	for _, b := range list {
		var params []string
		for k, v := range b.Params {
			params = append(params, k+"="+v)
		}
		sort.Strings(params)
		out = append(out, b.Signal+" "+b.RecipeID+" "+strings.Join(params, ","))
	}
	sort.Strings(out)
	return out
}

func (s *SignalsSuite) TestSuggestMatchesDiscoveredRecipesAndTakesParamsFromLabelValuesNamingTheComponent() {
	api := s.component(domain.ComponentRoleBackend, "api")
	s.noEnvironments(api)
	s.inventory("up", httpMetric)
	s.labelProbe(`count by (job) (`+httpMetric+`)`,
		map[string]string{"job": "web"}, map[string]string{"job": "api-gateway"}, map[string]string{"job": "api"}, map[string]string{})
	saved := s.expectSuggestions(api, nil)

	got, err := s.svc.Suggest(s.ctx, api.ID, domain.EnvironmentProduction)
	s.Require().NoError(err)
	s.Equal([]string{
		"http.error_ratio svc-http job=api",
		"http.latency svc-http job=api",
		"http.requests svc-http job=api",
	}, summarizeBindings(*saved))
	for _, b := range *saved {
		s.Equal(domain.SignalBindingSuggested, b.Status)
		s.Equal(domain.SignalBindingDetected, b.Source)
		s.Equal(s.promSource.ID, b.DataSourceID)
	}
	requests := signalByID(got, "http.requests")
	s.Equal(domain.SignalStateSuggested, requests.Status)
	s.Require().Len(requests.Suggestions, 1)
	s.Equal(`sum(rate(`+httpMetric+`{job="api"}[120s]))`, requests.Suggestions[0].RenderedQuery)
}

func (s *SignalsSuite) TestSuggestLeavesTheParamsToFillWhenNoLabelValueNamesTheComponent() {
	api := s.component(domain.ComponentRoleBackend, "checkout")
	s.noEnvironments(api)
	s.inventory(httpMetric)
	s.labelProbe(`count by (job) (`+httpMetric+`)`, map[string]string{"job": "web"})
	saved := s.expectSuggestions(api, nil)

	got, err := s.svc.Suggest(s.ctx, api.ID, domain.EnvironmentProduction)
	s.Require().NoError(err)
	s.Len(*saved, 3)
	requests := signalByID(got, "http.requests")
	s.Require().Len(requests.Suggestions, 1)
	s.Equal([]string{"job"}, requests.Suggestions[0].NeedsParams)
	s.Empty(requests.Suggestions[0].RenderedQuery)
}

func (s *SignalsSuite) TestSuggestTakesBrokerParamsFromTheGroupAndTopicLabelsTogether() {
	worker := s.component(domain.ComponentRoleWorker, "orders-consumer")
	s.noEnvironments(worker)
	s.inventory(kafkaMetric)
	s.labelProbe(`count by (consumergroup, topic) (`+kafkaMetric+`)`,
		map[string]string{"consumergroup": "billing", "topic": "invoices"},
		map[string]string{"consumergroup": "orders-consumer", "topic": "orders"},
	)
	saved := s.expectSuggestions(worker, nil)

	_, err := s.svc.Suggest(s.ctx, worker.ID, domain.EnvironmentProduction)
	s.Require().NoError(err)
	s.Equal([]string{
		"queue.backlog svc-kafka group=orders-consumer,topic=orders",
		"queue.publish_rate svc-kafka group=orders-consumer,topic=orders",
	}, summarizeBindings(*saved))
}

func (s *SignalsSuite) TestSuggestOffersTheResourceRecipesOfACloudBoundEnvironment() {
	api := s.component(domain.ComponentRoleBackend, "api")
	account := uuid.New()
	linked := s.addSource(domain.DataSource{Kind: domain.DataSourceKindGCPMonitoring, Name: "gcp", Config: map[string]string{"cloud_account_id": account.String()}})
	s.addSource(domain.DataSource{Kind: domain.DataSourceKindGCPMonitoring, Name: "other gcp", Config: map[string]string{"cloud_account_id": uuid.NewString()}})
	s.envs.On("ListEnvironments", mock.Anything, api.RepositoryID).Return([]domain.ComponentEnvironment{
		{
			ID: uuid.New(), ComponentID: api.ID, Environment: domain.EnvironmentProduction, Provider: domain.CloudGCP,
			AccountID: &account, Status: domain.LinkConfirmed,
			Resource: &domain.CloudResourceRef{
				Kind: domain.CloudResourceCloudRunService, ID: "projects/p1/locations/europe-west3/services/api-svc",
				Name: "api-svc", Region: "europe-west3", Extra: map[string]string{"project_id": "p1"},
			},
		},
		{ID: uuid.New(), ComponentID: uuid.New(), Environment: domain.EnvironmentProduction, AccountID: &account, Status: domain.LinkConfirmed,
			Resource: &domain.CloudResourceRef{Kind: domain.CloudResourceCloudRunService, Name: "someone-else"}},
	}, nil).Once()
	s.inventory()
	saved := s.expectSuggestions(api, nil)

	_, err := s.svc.Suggest(s.ctx, api.ID, domain.EnvironmentProduction)
	s.Require().NoError(err)
	var mine []domain.SignalBinding
	for _, b := range *saved {
		s.Equal(linked.ID, b.DataSourceID)
		if b.RecipeID == runRecipe {
			mine = append(mine, b)
		}
	}
	s.Equal([]string{"http.requests svc-cloud-run project=p1,region=europe-west3,service=api-svc"}, summarizeBindings(mine))
}

func (s *SignalsSuite) TestSuggestNeverTouchesABoundSignalOrOffersADismissedTargetAgain() {
	api := s.component(domain.ComponentRoleBackend, "api")
	s.noEnvironments(api)
	s.inventory(httpMetric)
	s.labelProbe(`count by (job) (`+httpMetric+`)`, map[string]string{"job": "api"})
	human := s.binding(api, "http.requests", domain.SignalBindingConfirmed, map[string]string{"job": "edge"})
	human.Source = domain.SignalBindingHuman
	dismissed := s.binding(api, "http.error_ratio", domain.SignalBindingDismissed, map[string]string{"job": "api"})
	stale := s.binding(api, "http.latency", domain.SignalBindingSuggested, map[string]string{"job": "old"})
	saved := s.expectSuggestions(api, []domain.SignalBinding{human, dismissed, stale})

	_, err := s.svc.Suggest(s.ctx, api.ID, domain.EnvironmentProduction)
	s.Require().NoError(err)
	s.Equal([]string{"http.latency svc-http job=api"}, summarizeBindings(*saved))
}

func (s *SignalsSuite) TestSuggestDiscoversEachSourceOnceWithinTheCacheWindow() {
	api := s.component(domain.ComponentRoleBackend, "checkout")
	s.inventory(httpMetric)
	for range 2 {
		s.noEnvironments(api)
		s.labelProbe(`count by (job) (`+httpMetric+`)`, map[string]string{"job": "web"})
		s.expectSuggestions(api, nil)
		_, err := s.svc.Suggest(s.ctx, api.ID, domain.EnvironmentProduction)
		s.Require().NoError(err)
	}
}

func (s *SignalsSuite) TestSuggestLeavesTheResourceParamsNoRefNamesToFill() {
	api := s.component(domain.ComponentRoleBackend, "api")
	account := uuid.New()
	s.addSource(domain.DataSource{Kind: domain.DataSourceKindGCPMonitoring, Name: "gcp", Config: map[string]string{"cloud_account_id": account.String()}})
	s.envs.On("ListEnvironments", mock.Anything, api.RepositoryID).Return([]domain.ComponentEnvironment{{
		ID: uuid.New(), ComponentID: api.ID, Environment: domain.EnvironmentProduction, Provider: domain.CloudGCP,
		AccountID: &account, Status: domain.LinkConfirmed,
		Resource: &domain.CloudResourceRef{Kind: domain.CloudResourceGKEWorkload, ID: "c1", Name: "prod-cluster", Region: "europe-west3"},
	}}, nil).Once()
	s.inventory()
	s.expectSuggestions(api, nil)

	got, err := s.svc.Suggest(s.ctx, api.ID, domain.EnvironmentProduction)
	s.Require().NoError(err)
	cpu := signalByID(got, "infra.cpu")
	s.Require().Len(cpu.Suggestions, 1)
	s.Equal("gcp-gke-workload", cpu.Suggestions[0].RecipeID)
	s.Equal(map[string]string{"cluster": "prod-cluster", "location": "europe-west3"}, cpu.Suggestions[0].Params)
	s.Equal([]string{"namespace", "workload"}, cpu.Suggestions[0].NeedsParams)
}

func (s *SignalsSuite) TestSuggestNamesTheComponentOnlyByTheResourcesOfThatEnvironment() {
	tests := []struct {
		name string
		env  domain.DeployEnvironment
		want []string
	}{
		{"another environment's resource", domain.EnvironmentStaging, []string{"http.error_ratio svc-http ", "http.latency svc-http ", "http.requests svc-http "}},
		{"this environment's resource", domain.EnvironmentProduction, []string{"http.error_ratio svc-http job=pay-gateway", "http.latency svc-http job=pay-gateway", "http.requests svc-http job=pay-gateway"}},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.reset()
			api := s.component(domain.ComponentRoleBackend, "checkout")
			s.envs.On("ListEnvironments", mock.Anything, api.RepositoryID).Return([]domain.ComponentEnvironment{{
				ID: uuid.New(), ComponentID: api.ID, Environment: tt.env, Status: domain.LinkConfirmed,
				Resource: &domain.CloudResourceRef{Kind: domain.CloudResourceCloudRunService, Name: "pay-gateway"},
			}}, nil).Once()
			s.inventory(httpMetric)
			s.labelProbe(`count by (job) (`+httpMetric+`)`, map[string]string{"job": "pay-gateway"})
			saved := s.expectSuggestions(api, nil)

			_, err := s.svc.Suggest(s.ctx, api.ID, domain.EnvironmentProduction)

			s.Require().NoError(err)
			s.Equal(tt.want, summarizeBindings(*saved))
		})
	}
}

func (s *SignalsSuite) TestSuggestReplacesOnlyTheSuggestionsOfSourcesThatAnswered() {
	api := s.component(domain.ComponentRoleBackend, "api")
	other := s.addSource(domain.DataSource{Kind: domain.DataSourceKindPrometheus, Name: "other prom"})
	s.noEnvironments(api)
	s.inventory(httpMetric)
	s.discoverer.On("Discover", mock.Anything, onSource(other), mock.Anything).
		Return(domain.Inventory{}, errors.New("label values: HTTP 503")).Once()
	s.labelProbe(`count by (job) (`+httpMetric+`)`, map[string]string{"job": "api"})
	saved, answered := s.expectSuggestionsFrom(api, nil)

	_, err := s.svc.Suggest(s.ctx, api.ID, domain.EnvironmentProduction)

	s.Require().NoError(err)
	s.Equal([]uuid.UUID{s.promSource.ID}, *answered)
	s.Len(*saved, 3)
}

func (s *SignalsSuite) TestSuggestCountsAFailedLabelProbeAsTheSourceNotAnswering() {
	api := s.component(domain.ComponentRoleBackend, "api")
	s.noEnvironments(api)
	s.inventory(httpMetric)
	s.querier.On("Series", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(domain.SeriesResult{}, errors.New("query: HTTP 502")).Once()
	saved, answered := s.expectSuggestionsFrom(api, nil)

	_, err := s.svc.Suggest(s.ctx, api.ID, domain.EnvironmentProduction)

	s.Require().NoError(err)
	s.Empty(*answered)
	s.Equal([]string{"http.error_ratio svc-http ", "http.latency svc-http ", "http.requests svc-http "}, summarizeBindings(*saved))
}
