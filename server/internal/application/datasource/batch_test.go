package datasource_test

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"

	"github.com/makifbaysal/tasktrooper/server/internal/application/datasource"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/makifbaysal/tasktrooper/server/internal/port/mocks"
)

type lookingUpKind struct {
	*mocks.DataSourceKindAdapter
	*mocks.Discoverer
	*mocks.MetricDiscoverer
}

func (s *ServiceSuite) TestABatchReadsEachSourceOnceForAllItsQueries() {
	known := domain.DataSource{ID: uuid.New(), Kind: domain.DataSourceKindPrometheus, Config: map[string]string{"url": "http://prom:9090"}}
	other := uuid.New()
	s.store.On("DataSourceSecrets", mock.Anything, known.ID).Return(map[string]string{"token": "t0ken"}, nil).Once()
	s.stored(other, domain.DataSource{Kind: domain.DataSourceKindPrometheus, Config: map[string]string{"url": "http://other:9090"}}, map[string]string{})
	s.querier.On("Series", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(domain.SeriesResult{Warnings: []string{"slow t0ken"}}, nil).Times(4)
	q := domain.SeriesQuery{Query: "up", From: s.now.Add(-time.Hour), To: s.now}

	batch := s.svc.Batch(known)
	for range 3 {
		res, err := batch.Query(s.ctx, known.ID, q)
		s.Require().NoError(err)
		s.Equal([]string{"slow [redacted]"}, res.Warnings)
	}
	_, err := batch.Query(s.ctx, other, q)
	s.Require().NoError(err)

	s.store.AssertNumberOfCalls(s.T(), "DataSourceSecrets", 2)
	s.store.AssertNumberOfCalls(s.T(), "GetDataSource", 1)
}

func (s *ServiceSuite) TestOnChangeHearsOfAnUpdateAndADelete() {
	id := uuid.New()
	s.stored(id, domain.DataSource{Kind: domain.DataSourceKindPrometheus, Name: "prod", Config: map[string]string{"url": "http://prom:9090"}}, map[string]string{})
	s.store.On("UpdateDataSource", mock.Anything, mock.Anything, mock.Anything).
		Return(func(_ context.Context, ds domain.DataSource, _ map[string]string) (domain.DataSource, error) {
			return ds, nil
		}).Once()
	s.kind.On("Verify", mock.Anything, mock.Anything, mock.Anything).Return(domain.DataSourceInfo{}, nil).Once()
	s.expectRecordEchoes()
	s.store.On("DeleteDataSource", mock.Anything, id).Return(nil).Once()
	var heard []uuid.UUID
	s.svc.OnChange(func(changed uuid.UUID) { heard = append(heard, changed) })

	name := "renamed"
	_, err := s.svc.Update(s.ctx, id, domain.PatchDataSourceRequest{Name: &name})
	s.Require().NoError(err)
	s.Require().NoError(s.svc.Delete(s.ctx, id))

	s.Equal([]uuid.UUID{id, id}, heard)
}

func (s *ServiceSuite) TestATruncatedInventoryLooksUpTheDetectMetricsItLacks() {
	kind := mocks.NewDataSourceKindAdapter(s.T())
	discoverer := mocks.NewDiscoverer(s.T())
	finder := mocks.NewMetricDiscoverer(s.T())
	kind.On("Kind").Return(domain.DataSourceKindPrometheus).Maybe()
	kind.On("Schema").Return(testSchema()).Maybe()
	kind.On("Capabilities").Return([]domain.Capability{domain.CapabilityMetrics}).Maybe()
	policy := urlguard.HumanConfigured()
	svc, err := datasource.NewService(datasource.Deps{
		Store: s.store, Recipes: s.recipes, URLPolicy: &policy,
		Kinds: []port.DataSourceKindAdapter{lookingUpKind{kind, discoverer, finder}},
	})
	s.Require().NoError(err)
	id := uuid.New()
	s.stored(id, domain.DataSource{Kind: domain.DataSourceKindPrometheus, Config: map[string]string{"url": "http://prom:9090"}}, map[string]string{})
	s.recipes.On("ListUserRecipes", mock.Anything).Return(nil, nil)
	discoverer.On("Discover", mock.Anything, mock.Anything, mock.Anything).
		Return(domain.Inventory{Metrics: []string{"rabbitmq_queue_messages_ready"}, Truncated: true}, nil).Once()
	finder.On("DiscoverMetric", mock.Anything, mock.Anything, mock.Anything, "kafka_consumergroup_lag").Return(true, nil).Once()
	finder.On("DiscoverMetric", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(false, nil)

	got, err := svc.Discover(s.ctx, id, "", 0)

	s.Require().NoError(err)
	s.Equal([]string{"rabbitmq_queue_messages_ready"}, got.Metrics)
	s.True(got.Truncated)
	s.Contains(got.Recipes, domain.RecipeMatch{RecipeID: "kafka-exporter", MatchedMetric: "kafka_consumergroup_lag"})
	s.Contains(got.Recipes, domain.RecipeMatch{RecipeID: "rabbitmq-prometheus", MatchedMetric: "rabbitmq_queue_messages_ready"})
	for _, c := range finder.Calls {
		s.NotEqual("rabbitmq_queue_messages_ready", c.Arguments.Get(3))
	}

	discoverer.On("Discover", mock.Anything, mock.Anything, mock.Anything).
		Return(domain.Inventory{Metrics: []string{"up"}}, nil).Once()
	asked := len(finder.Calls)
	_, err = svc.Discover(s.ctx, id, "", 0)
	s.Require().NoError(err)
	s.Len(finder.Calls, asked)
}
