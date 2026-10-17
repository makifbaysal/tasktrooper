package signals_test

import (
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// linkTo links c to a queue resource of vendor, as the discovery scan does
// for a messaging library in its manifests.
func (s *SignalsSuite) linkTo(c domain.Component, vendor string, status domain.LinkStatus) {
	res := domain.SystemResource{ID: uuid.New(), Kind: domain.ResourceQueue, Vendor: vendor}
	s.linked = append(s.linked, res)
	s.links = append(s.links, domain.ComponentLink{
		ID: uuid.New(), RepositoryID: c.RepositoryID, FromComponentID: c.ID, ToResourceID: &res.ID,
		Protocol: domain.LinkQueue, Status: status, SignalKey: "dep:" + vendor,
	})
}

func (s *SignalsSuite) TestSuggestRanksTheBrokersAWorkerIsLinkedToFirstAndKeepsTheOthersOnOffer() {
	worker := s.component(domain.ComponentRoleWorker, "billing")
	s.linkTo(worker, "kafka", domain.LinkConfirmed)
	s.linkTo(worker, "rabbitmq", domain.LinkDismissed)
	s.noEnvironments(worker)
	s.inventory(rabbitMetric, kafkaMetric)
	s.labelProbe(`count by (consumergroup, topic) (`+kafkaMetric+`)`, map[string]string{"consumergroup": "billing", "topic": "orders"})
	s.labelProbe(`count by (queue) (`+rabbitMetric+`)`, map[string]string{"queue": "billing"})
	saved := s.expectSuggestions(worker, nil)

	_, err := s.svc.Suggest(s.ctx, worker.ID, domain.EnvironmentProduction)
	s.Require().NoError(err)
	var order []string
	for _, b := range *saved {
		order = append(order, b.Signal+" "+b.RecipeID)
	}
	s.Equal([]string{"queue.backlog svc-kafka", "queue.publish_rate svc-kafka", "queue.backlog svc-rabbit"}, order,
		"a dismissed link names no broker, and a library the discovery catalog misses must not hide the broker it uses")
}

func (s *SignalsSuite) TestSuggestOffersEveryBrokersRecipeWhileTheComponentsBrokersAreUnknown() {
	worker := s.component(domain.ComponentRoleWorker, "billing")
	s.noEnvironments(worker)
	s.inventory(kafkaMetric, rabbitMetric)
	s.labelProbe(`count by (consumergroup, topic) (`+kafkaMetric+`)`, map[string]string{"consumergroup": "billing", "topic": "orders"})
	s.labelProbe(`count by (queue) (`+rabbitMetric+`)`, map[string]string{"queue": "billing"})
	saved := s.expectSuggestions(worker, nil)

	_, err := s.svc.Suggest(s.ctx, worker.ID, domain.EnvironmentProduction)
	s.Require().NoError(err)
	s.Equal([]string{
		"queue.backlog svc-kafka group=billing,topic=orders",
		"queue.backlog svc-rabbit queue=billing",
		"queue.publish_rate svc-kafka group=billing,topic=orders",
	}, summarizeBindings(*saved))
}

func (s *SignalsSuite) TestSuggestRanksTheRecipesOfTheComponentsBrokersFirst() {
	worker := s.component(domain.ComponentRoleWorker, "billing")
	s.linkTo(worker, "kafka", domain.LinkConfirmed)
	s.noEnvironments(worker)
	s.inventory(depthMetric, kafkaMetric)
	s.labelProbe(`count by (queue) (`+depthMetric+`)`, map[string]string{"queue": "billing"})
	s.labelProbe(`count by (consumergroup, topic) (`+kafkaMetric+`)`, map[string]string{"consumergroup": "billing", "topic": "orders"})
	saved := s.expectSuggestions(worker, nil)

	_, err := s.svc.Suggest(s.ctx, worker.ID, domain.EnvironmentProduction)
	s.Require().NoError(err)
	var order []string
	for _, b := range *saved {
		order = append(order, b.Signal+" "+b.RecipeID)
	}
	s.Equal([]string{"queue.backlog svc-kafka", "queue.publish_rate svc-kafka", "queue.backlog svc-depth"}, order,
		"a recipe naming no messaging system stays on offer, after the broker's own")
}

func (s *SignalsSuite) TestSuggestOffersTheBrokersConsumersThatNameTheComponent() {
	worker := s.component(domain.ComponentRoleWorker, "billing")
	kafka := s.addSource(domain.DataSource{Kind: domain.DataSourceKindKafka, Name: "kafka prod"})
	s.noEnvironments(worker)
	s.inventory()
	s.consumers.On("DiscoverQueues", mock.Anything, mock.MatchedBy(func(ds domain.DataSource) bool { return ds.ID == kafka.ID }), mock.Anything).
		Return([]domain.QueueTarget{
			{Name: "shipping", Params: map[string]string{"group": "shipping"}},
			{Name: "billing-v2", Params: map[string]string{"group": "billing-v2"}},
			{Name: `billing"}`, Params: map[string]string{"group": `billing"}`}},
			{Name: "billing.audit", Params: map[string]string{"group": "billing.audit", "topic": "audit"}},
		}, nil).Once()
	saved, answered := s.expectSuggestionsFrom(worker, nil)

	got, err := s.svc.Suggest(s.ctx, worker.ID, domain.EnvironmentProduction)
	s.Require().NoError(err)
	for _, b := range *saved {
		s.Equal(kafka.ID, b.DataSourceID)
		s.Empty(b.Query, "a discovered name never goes into a raw query")
	}
	s.Equal([]string{
		`queue.backlog kafka-direct group=billing"}`,
		`queue.consumers kafka-direct group=billing"}`,
	}, summarizeBindings(*saved), "only the best-named consumer, through the recipe taking exactly its params")
	s.ElementsMatch([]uuid.UUID{s.promSource.ID, kafka.ID}, *answered)
	backlog := signalByID(got, "queue.backlog")
	s.Require().Len(backlog.Suggestions, 1)
	s.Equal(`{"group": "billing\"}", "metric": "lag"}`, backlog.Suggestions[0].RenderedQuery)
}

func (s *SignalsSuite) TestSuggestStillOffersABrokerSourceOfAnotherMessagingSystem() {
	worker := s.component(domain.ComponentRoleWorker, "billing")
	s.linkTo(worker, "rabbitmq", domain.LinkConfirmed)
	kafka := s.addSource(domain.DataSource{Kind: domain.DataSourceKindKafka, Name: "kafka prod"})
	s.noEnvironments(worker)
	s.inventory()
	s.consumers.On("DiscoverQueues", mock.Anything, mock.Anything, mock.Anything).
		Return([]domain.QueueTarget{{Name: "billing", Params: map[string]string{"group": "billing"}}}, nil).Once()
	saved, answered := s.expectSuggestionsFrom(worker, nil)

	_, err := s.svc.Suggest(s.ctx, worker.ID, domain.EnvironmentProduction)
	s.Require().NoError(err)
	s.Equal([]string{"queue.backlog kafka-direct group=billing", "queue.consumers kafka-direct group=billing"}, summarizeBindings(*saved))
	s.Contains(*answered, kafka.ID)
}
