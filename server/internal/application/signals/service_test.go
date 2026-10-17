package signals_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"testing/fstest"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/datasource"
	"github.com/makifbaysal/tasktrooper/server/internal/application/signals"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/makifbaysal/tasktrooper/server/internal/port/mocks"
)

const (
	httpRecipe   = "svc-http"
	kafkaRecipe  = "svc-kafka"
	runRecipe    = "svc-cloud-run"
	depthRecipe  = "svc-depth"
	rabbitRecipe = "svc-rabbit"
	httpMetric   = "svc_http_requests_seconds_count"
	kafkaMetric  = "svc_kafka_consumergroup_lag"
	depthMetric  = "svc_queue_depth"
	rabbitMetric = "svc_rabbit_ready"
)

type promKind struct {
	*mocks.DataSourceKindAdapter
	*mocks.MetricsQuerier
	*mocks.Discoverer
	*mocks.TableQuerier
}

type brokerKind struct {
	*mocks.DataSourceKindAdapter
	*mocks.TableQuerier
	*mocks.QueueDiscoverer
}

type componentLinks struct {
	*mocks.LinkStore
	*mocks.ResourceStore
}

type gcpKind struct {
	*mocks.DataSourceKindAdapter
	*mocks.MetricsQuerier
}

// userRecipes stand in for the catalog's own so a test reads what it
// defines: detect metrics no catalog recipe shares, and $__quantile.
func userRecipes() []domain.Recipe {
	return []domain.Recipe{
		{
			ID: httpRecipe, SourceKind: domain.DataSourceKindPrometheus,
			Detect: domain.RecipeDetect{Metric: httpMetric}, Params: []string{"job"},
			Signals: map[string]string{
				"http.requests":    `sum(rate(` + httpMetric + `{job="$job"}[$__rate]))`,
				"http.error_ratio": `100 * sum(rate(` + httpMetric + `{job="$job", code=~"5.."}[$__rate])) / sum(rate(` + httpMetric + `{job="$job"}[$__rate]))`,
				"http.latency":     `1000 * histogram_quantile($__quantile, sum by (le) (rate(svc_http_bucket{job="$job"}[$__rate])))`,
			},
		},
		{
			ID: kafkaRecipe, SourceKind: domain.DataSourceKindPrometheus,
			AppliesTo: domain.RecipeAppliesTo{MessagingSystem: "kafka"},
			Detect:    domain.RecipeDetect{Metric: kafkaMetric}, Params: []string{"group", "topic"},
			Signals: map[string]string{
				"queue.backlog":      `sum(` + kafkaMetric + `{consumergroup="$group", topic="$topic"})`,
				"queue.publish_rate": `sum(rate(svc_kafka_offset{topic="$topic"}[$__rate]))`,
			},
			Tables: map[string]domain.RecipeTable{domain.RecipeTableQueues: {
				Columns: []domain.TableColumn{
					{Key: "topic", Label: "Topic"}, {Key: "partition", Label: "Partition"},
					{Key: "lag", Label: "Lag", Unit: "messages"}, {Key: "rate", Label: "Consumed", Unit: "msg/s"},
				},
				KeyLabels: []string{"topic", "partition"},
				Queries: map[string]string{
					"lag":  `sum by (topic, partition) (` + kafkaMetric + `{consumergroup="$group"})`,
					"rate": `sum by (topic, partition) (rate(svc_kafka_offset{consumergroup="$group"}[$__rate]))`,
				},
			}},
		},
		{
			ID: depthRecipe, SourceKind: domain.DataSourceKindPrometheus,
			Detect: domain.RecipeDetect{Metric: depthMetric}, Params: []string{"queue"},
			Signals: map[string]string{"queue.backlog": `sum(` + depthMetric + `{queue="$queue"})`},
		},
		{
			ID: rabbitRecipe, SourceKind: domain.DataSourceKindPrometheus,
			AppliesTo: domain.RecipeAppliesTo{MessagingSystem: "rabbitmq"},
			Detect:    domain.RecipeDetect{Metric: rabbitMetric}, Params: []string{"queue"},
			Signals: map[string]string{"queue.backlog": `sum(` + rabbitMetric + `{queue="$queue"})`},
		},
		{
			ID: runRecipe, SourceKind: domain.DataSourceKindGCPMonitoring,
			AppliesTo: domain.RecipeAppliesTo{
				ResourceKind:   domain.CloudResourceCloudRunService,
				ResourceParams: map[string]string{"service": "name", "region": "region", "project": "extra.project_id"},
			},
			Params: []string{"service", "region", "project"},
			Signals: map[string]string{
				"http.requests": `sum(rate(run_request_count{service_name="$service", location="$region", project_id="$project"}[$__rate]))`,
			},
		},
	}
}

type SignalsSuite struct {
	suite.Suite
	ctx        context.Context
	now        time.Time
	dsStore    *mocks.DataSourceStore
	recipes    *mocks.ObservabilityRecipeStore
	prom       *mocks.DataSourceKindAdapter
	querier    *mocks.MetricsQuerier
	discoverer *mocks.Discoverer
	gcp        *mocks.DataSourceKindAdapter
	gcpQuerier *mocks.MetricsQuerier
	cw         *mocks.DataSourceKindAdapter
	cwQuerier  *mocks.MetricsQuerier
	table      *mocks.TableQuerier
	broker     *mocks.DataSourceKindAdapter
	brokerRead *mocks.TableQuerier
	consumers  *mocks.QueueDiscoverer
	linkStore  *mocks.LinkStore
	resources  *mocks.ResourceStore
	links      []domain.ComponentLink
	linked     []domain.SystemResource
	bindings   *mocks.SignalBindingStore
	comps      *mocks.ComponentStore
	envs       *mocks.EnvironmentStore
	ds         *datasource.Service
	sources    []domain.DataSource
	promSource domain.DataSource
	svc        *signals.Service
}

func TestSignalsSuite(t *testing.T) { suite.Run(t, new(SignalsSuite)) }

func (s *SignalsSuite) SetupTest() {
	s.ctx = context.Background()
	s.now = time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	s.reset()
}

// reset builds fresh mocks bound to the current (sub)test.
func (s *SignalsSuite) reset(opts ...signals.Option) {
	t := s.T()
	s.dsStore = mocks.NewDataSourceStore(t)
	s.recipes = mocks.NewObservabilityRecipeStore(t)
	s.prom = mocks.NewDataSourceKindAdapter(t)
	s.querier = mocks.NewMetricsQuerier(t)
	s.discoverer = mocks.NewDiscoverer(t)
	s.gcp = mocks.NewDataSourceKindAdapter(t)
	s.gcpQuerier = mocks.NewMetricsQuerier(t)
	s.cw = mocks.NewDataSourceKindAdapter(t)
	s.cwQuerier = mocks.NewMetricsQuerier(t)
	s.table = mocks.NewTableQuerier(t)
	s.broker = mocks.NewDataSourceKindAdapter(t)
	s.brokerRead = mocks.NewTableQuerier(t)
	s.consumers = mocks.NewQueueDiscoverer(t)
	s.linkStore = mocks.NewLinkStore(t)
	s.resources = mocks.NewResourceStore(t)
	s.links, s.linked = nil, nil
	s.bindings = mocks.NewSignalBindingStore(t)
	s.comps = mocks.NewComponentStore(t)
	s.envs = mocks.NewEnvironmentStore(t)

	s.prom.On("Kind").Return(domain.DataSourceKindPrometheus).Maybe()
	s.prom.On("Schema").Return(domain.DataSourceSchema{Label: "Prometheus"}).Maybe()
	s.prom.On("Capabilities").Return([]domain.Capability{domain.CapabilityMetrics}).Maybe()
	s.gcp.On("Kind").Return(domain.DataSourceKindGCPMonitoring).Maybe()
	s.gcp.On("Schema").Return(domain.DataSourceSchema{Label: "Cloud Monitoring"}).Maybe()
	s.gcp.On("Capabilities").Return([]domain.Capability{}).Maybe()
	s.cw.On("Kind").Return(domain.DataSourceKindCloudWatch).Maybe()
	s.cw.On("Schema").Return(domain.DataSourceSchema{Label: "CloudWatch"}).Maybe()
	s.cw.On("Capabilities").Return([]domain.Capability{}).Maybe()
	s.broker.On("Kind").Return(domain.DataSourceKindKafka).Maybe()
	s.broker.On("Schema").Return(domain.DataSourceSchema{Label: "Kafka"}).Maybe()
	s.broker.On("Capabilities").Return([]domain.Capability{domain.CapabilityQueues}).Maybe()
	s.linkStore.On("ListLinks", mock.Anything, mock.Anything).Return(func(context.Context, uuid.UUID) ([]domain.ComponentLink, error) {
		return s.links, nil
	}).Maybe()
	s.resources.On("ListResources", mock.Anything, mock.Anything).Return(func(_ context.Context, ids []uuid.UUID) ([]domain.SystemResource, error) {
		var out []domain.SystemResource
		for _, r := range s.linked {
			if slices.Contains(ids, r.ID) {
				out = append(out, r)
			}
		}
		return out, nil
	}).Maybe()
	s.recipes.On("ListUserRecipes", mock.Anything).Return(userRecipes(), nil).Maybe()

	policy := urlguard.HumanConfigured()
	sources, err := datasource.NewService(datasource.Deps{
		Store:   s.dsStore,
		Recipes: s.recipes,
		Kinds: []port.DataSourceKindAdapter{
			promKind{s.prom, s.querier, s.discoverer, s.table}, gcpKind{s.gcp, s.gcpQuerier}, gcpKind{s.cw, s.cwQuerier},
			brokerKind{s.broker, s.brokerRead, s.consumers},
		},
		URLPolicy: &policy,
	})
	s.Require().NoError(err)
	s.ds = sources
	s.sources = nil
	s.promSource = s.addSource(domain.DataSource{Kind: domain.DataSourceKindPrometheus, Name: "local prom"})

	base := []signals.Option{signals.WithClock(func() time.Time { return s.now }), signals.WithQueryTimeout(2 * time.Second)}
	svc, err := signals.NewService(signals.Deps{
		Bindings: s.bindings, Sources: sources, Components: s.comps, Environments: s.envs,
		Links: componentLinks{s.linkStore, s.resources},
	}, append(base, opts...)...)
	s.Require().NoError(err)
	s.svc = svc
	s.dsStore.On("ListDataSources", mock.Anything).Return(func(context.Context) ([]domain.DataSource, error) {
		return s.sources, nil
	}).Maybe()
}

func (s *SignalsSuite) addSource(ds domain.DataSource) domain.DataSource {
	ds.ID = uuid.New()
	ds.UpdatedAt = s.now.Add(-time.Hour)
	s.sources = append(s.sources, ds)
	s.dsStore.On("GetDataSource", mock.Anything, ds.ID).Return(ds, nil).Maybe()
	s.dsStore.On("DataSourceSecrets", mock.Anything, ds.ID).Return(map[string]string{}, nil).Maybe()
	return ds
}

func (s *SignalsSuite) component(role domain.ComponentRole, name string) domain.Component {
	c := domain.Component{
		ID: uuid.New(), RepositoryID: uuid.New(), Path: name,
		Name: domain.Detected(name, domain.ConfidenceHigh), Role: domain.Detected(role, domain.ConfidenceHigh),
		Status: domain.ComponentStatusActive,
	}
	s.comps.On("GetComponent", mock.Anything, c.ID).Return(c, nil).Maybe()
	return c
}

func (s *SignalsSuite) stored(componentID uuid.UUID, env domain.DeployEnvironment, list ...domain.SignalBinding) {
	s.bindings.On("ListSignalBindings", mock.Anything, componentID, env).Return(list, nil)
}

func (s *SignalsSuite) binding(c domain.Component, signal string, status domain.SignalBindingStatus, params map[string]string) domain.SignalBinding {
	return domain.SignalBinding{
		ID: uuid.New(), ComponentID: c.ID, Environment: domain.EnvironmentProduction, Signal: signal,
		DataSourceID: s.promSource.ID, RecipeID: httpRecipe, Params: params, Status: status,
		Source: domain.SignalBindingDetected, CreatedAt: s.now, UpdatedAt: s.now,
	}
}

func signalByID(list domain.ComponentSignals, id string) *domain.ComponentSignal {
	for i := range list.Signals {
		if list.Signals[i].Signal == id {
			return &list.Signals[i]
		}
	}
	return nil
}

func (s *SignalsSuite) TestListGroupsBindingsByStatusOverTheRolesSignals() {
	api := s.component(domain.ComponentRoleBackend, "api")
	bound := s.binding(api, "http.requests", domain.SignalBindingConfirmed, map[string]string{"job": "api"})
	pending := s.binding(api, "http.latency", domain.SignalBindingSuggested, map[string]string{})
	dismissed := s.binding(api, "http.error_ratio", domain.SignalBindingDismissed, map[string]string{"job": "api"})
	offRole := s.binding(api, "web.lcp", domain.SignalBindingConfirmed, nil)
	offRole.RecipeID, offRole.Query = "", "histogram_quantile($__quantile, rate(lcp_bucket[$__rate]))"
	s.stored(api.ID, domain.EnvironmentProduction, bound, pending, dismissed, offRole)

	got, err := s.svc.List(s.ctx, api.ID, domain.EnvironmentProduction)
	s.Require().NoError(err)
	s.Equal(api.ID, got.ComponentID)
	s.Equal(domain.ComponentRoleBackend, got.Role)
	s.Equal(domain.EnvironmentProduction, got.Environment)

	var ids, want []string
	for _, sig := range got.Signals {
		ids = append(ids, sig.Signal)
	}
	for _, sig := range s.ds.Signals() {
		if slices.Contains(sig.Roles, domain.ComponentRoleBackend) {
			want = append(want, sig.ID)
		}
	}
	s.Equal(append(want, "web.lcp"), ids)
	s.Equal([]string{"http.requests", "http.error_ratio", "http.latency"}, ids[:3])

	requests := signalByID(got, "http.requests")
	s.Equal(domain.SignalStateBound, requests.Status)
	s.True(requests.Golden)
	s.Equal("req/s", requests.Unit)
	s.Require().NotNil(requests.Binding)
	s.Equal(bound.ID, requests.Binding.ID)
	s.Equal("local prom", requests.Binding.DataSourceName)
	s.Equal(domain.DataSourceKindPrometheus, requests.Binding.DataSourceKind)
	s.Equal(`sum(rate(`+httpMetric+`{job="api"}[120s]))`, requests.Binding.RenderedQuery)
	s.Equal([]string{}, requests.Binding.NeedsParams)
	s.Equal([]domain.SignalBindingView{}, requests.Suggestions)

	latency := signalByID(got, "http.latency")
	s.Equal(domain.SignalStateSuggested, latency.Status)
	s.Nil(latency.Binding)
	s.Require().Len(latency.Suggestions, 1)
	s.Equal([]string{"job"}, latency.Suggestions[0].NeedsParams)
	s.Empty(latency.Suggestions[0].RenderedQuery)

	errorRatio := signalByID(got, "http.error_ratio")
	s.Equal(domain.SignalStateUnbound, errorRatio.Status)
	s.Empty(errorRatio.Suggestions)

	cpu := signalByID(got, "infra.cpu")
	s.False(cpu.Golden)
	s.Equal(domain.SignalStateUnbound, cpu.Status)

	lcp := signalByID(got, "web.lcp")
	s.Equal(domain.SignalStateBound, lcp.Status)
	s.Equal("histogram_quantile(0.95, rate(lcp_bucket[120s]))", lcp.Binding.RenderedQuery)
}

func (s *SignalsSuite) TestListRefusesAnUnknownEnvironmentAndAMissingComponent() {
	_, err := s.svc.List(s.ctx, uuid.New(), "prod")
	s.ErrorIs(err, signals.ErrInvalidInput)

	missing := uuid.New()
	s.comps.On("GetComponent", mock.Anything, missing).Return(domain.Component{}, port.ErrNotFound).Once()
	_, err = s.svc.List(s.ctx, missing, domain.EnvironmentProduction)
	s.ErrorIs(err, port.ErrNotFound)
}

func (s *SignalsSuite) TestPutRefusesWhatDoesNotFitAsFieldErrors() {
	other := s.addSource(domain.DataSource{Kind: domain.DataSourceKindGCPMonitoring, Name: "gcp"})
	tests := []struct {
		name   string
		signal string
		req    signals.BindingRequest
		want   datasource.FieldErrors
	}{
		{"no source", "http.requests", signals.BindingRequest{RecipeID: httpRecipe, Params: map[string]string{"job": "api"}},
			datasource.FieldErrors{"data_source_id": "required"}},
		{"malformed source", "http.requests", signals.BindingRequest{DataSourceID: "nope", Query: "up"},
			datasource.FieldErrors{"data_source_id": "invalid"}},
		{"unknown source", "http.requests", signals.BindingRequest{DataSourceID: uuid.NewString(), Query: "up"},
			datasource.FieldErrors{"data_source_id": "invalid"}},
		{"neither recipe nor query", "http.requests", signals.BindingRequest{DataSourceID: s.promSource.ID.String()},
			datasource.FieldErrors{"recipe_id": "required"}},
		{"unknown recipe", "http.requests", signals.BindingRequest{DataSourceID: s.promSource.ID.String(), RecipeID: "nope"},
			datasource.FieldErrors{"recipe_id": "invalid"}},
		{"recipe of another kind", "http.requests", signals.BindingRequest{DataSourceID: other.ID.String(), RecipeID: httpRecipe, Params: map[string]string{"job": "api"}},
			datasource.FieldErrors{"recipe_id": "invalid_option"}},
		{"recipe without the signal", "queue.backlog", signals.BindingRequest{DataSourceID: s.promSource.ID.String(), RecipeID: httpRecipe, Params: map[string]string{"job": "api"}},
			datasource.FieldErrors{"recipe_id": "invalid_option"}},
		{"missing and unknown params", "http.requests", signals.BindingRequest{DataSourceID: s.promSource.ID.String(), RecipeID: httpRecipe, Params: map[string]string{"jobb": "api"}},
			datasource.FieldErrors{"params.job": "required", "params.jobb": "unknown_field"}},
		{"unknown signal", "http.nope", signals.BindingRequest{DataSourceID: s.promSource.ID.String(), Query: "up"},
			datasource.FieldErrors{"signal": "invalid"}},
		{"query with an unset param", "http.requests", signals.BindingRequest{DataSourceID: s.promSource.ID.String(), Query: `up{job="$job"}`},
			datasource.FieldErrors{"query": "invalid"}},
	}
	api := s.component(domain.ComponentRoleBackend, "api")
	for _, tt := range tests {
		s.Run(tt.name, func() {
			_, err := s.svc.Put(s.ctx, api.ID, domain.EnvironmentProduction, tt.signal, tt.req)
			var fe datasource.FieldErrors
			s.Require().ErrorAs(err, &fe)
			s.Equal(tt.want, fe)
		})
	}
}

func (s *SignalsSuite) TestPutSavesAHumanConfirmedBinding() {
	api := s.component(domain.ComponentRoleBackend, "api")
	s.bindings.On("PutConfirmedSignalBinding", mock.Anything, mock.MatchedBy(func(b domain.SignalBinding) bool {
		return b.ComponentID == api.ID && b.Environment == domain.EnvironmentStaging && b.Signal == "http.requests" &&
			b.DataSourceID == s.promSource.ID && b.RecipeID == httpRecipe && b.Params["job"] == "api" &&
			b.Query == "" && b.Source == domain.SignalBindingHuman
	})).Return(func(_ context.Context, b domain.SignalBinding) (domain.SignalBinding, error) {
		b.ID, b.Status, b.CreatedAt, b.UpdatedAt = uuid.New(), domain.SignalBindingConfirmed, s.now, s.now
		return b, nil
	}).Once()

	got, err := s.svc.Put(s.ctx, api.ID, domain.EnvironmentStaging, "http.requests", signals.BindingRequest{
		DataSourceID: " " + s.promSource.ID.String(), RecipeID: httpRecipe, Params: map[string]string{"job": " api "},
	})
	s.Require().NoError(err)
	s.Equal(domain.SignalBindingConfirmed, got.Status)
	s.Equal(domain.SignalBindingHuman, got.Source)
	s.Equal(`sum(rate(`+httpMetric+`{job="api"}[120s]))`, got.RenderedQuery)
}

func (s *SignalsSuite) TestConfirmRefusesASuggestionStillMissingItsParams() {
	api := s.component(domain.ComponentRoleBackend, "api")
	pending := s.binding(api, "http.requests", domain.SignalBindingSuggested, map[string]string{})
	s.bindings.On("GetSignalBinding", mock.Anything, pending.ID).Return(pending, nil).Once()

	_, err := s.svc.Confirm(s.ctx, pending.ID)
	var fe datasource.FieldErrors
	s.Require().ErrorAs(err, &fe)
	s.Equal(datasource.FieldErrors{"params.job": "required"}, fe)
}

func (s *SignalsSuite) TestConfirmPromotesTheSuggestion() {
	api := s.component(domain.ComponentRoleBackend, "api")
	pending := s.binding(api, "http.requests", domain.SignalBindingSuggested, map[string]string{"job": "api"})
	confirmed := pending
	confirmed.Status = domain.SignalBindingConfirmed
	s.bindings.On("GetSignalBinding", mock.Anything, pending.ID).Return(pending, nil).Once()
	s.bindings.On("ConfirmSignalBinding", mock.Anything, pending.ID).Return(confirmed, nil).Once()

	got, err := s.svc.Confirm(s.ctx, pending.ID)
	s.Require().NoError(err)
	s.Equal(domain.SignalBindingConfirmed, got.Status)
	s.Equal(domain.SignalBindingDetected, got.Source)
}

func (s *SignalsSuite) TestDismissMarksASuggestionAndRefusesTheConfirmedBinding() {
	api := s.component(domain.ComponentRoleBackend, "api")
	pending := s.binding(api, "http.requests", domain.SignalBindingSuggested, nil)
	confirmed := s.binding(api, "http.latency", domain.SignalBindingConfirmed, nil)
	dismissed := pending
	dismissed.Status = domain.SignalBindingDismissed
	s.bindings.On("DismissSignalBinding", mock.Anything, pending.ID).Return(dismissed, nil).Twice()
	s.bindings.On("DismissSignalBinding", mock.Anything, confirmed.ID).Return(confirmed, nil).Once()

	s.NoError(s.svc.Dismiss(s.ctx, pending.ID))
	s.NoError(s.svc.Dismiss(s.ctx, pending.ID))
	s.ErrorIs(s.svc.Dismiss(s.ctx, confirmed.ID), signals.ErrConfirmed)
}

func (s *SignalsSuite) TestDeletePassesNotFoundThrough() {
	id := uuid.New()
	s.bindings.On("DeleteSignalBinding", mock.Anything, id).Return(port.ErrNotFound).Once()
	s.True(errors.Is(s.svc.Delete(s.ctx, id), port.ErrNotFound))
}

func (s *SignalsSuite) TestNewServiceRefusesAGoldenListTheCatalogDoesNotBackUp() {
	tests := []struct {
		name string
		yaml string
		want string
	}{
		{"unknown signal", "golden:\n  backend: [http.nope]\n", "not in the signal catalog"},
		{"unknown role", "golden:\n  robot: [http.requests]\n", "unknown role"},
		{"listed twice", "golden:\n  backend: [http.requests, http.requests]\n", "twice"},
		{"no better direction", "golden:\n  backend: [infra.instances]\n", "no better direction"},
		{"unknown key", "golden: {}\nextra: 1\n", "extra"},
	}
	policy := urlguard.HumanConfigured()
	sources, err := datasource.NewService(datasource.Deps{Store: s.dsStore, URLPolicy: &policy})
	s.Require().NoError(err)
	for _, tt := range tests {
		s.Run(tt.name, func() {
			_, err := signals.NewService(signals.Deps{
				Bindings: s.bindings, Sources: sources, Components: s.comps, Environments: s.envs,
				Catalog: fstest.MapFS{"golden.yaml": {Data: []byte(tt.yaml)}},
			})
			s.Require().Error(err)
			s.Contains(err.Error(), tt.want)
		})
	}
}
