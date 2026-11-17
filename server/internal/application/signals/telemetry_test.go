package signals_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/datasource"
	"github.com/makifbaysal/tasktrooper/server/internal/application/signals"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/makifbaysal/tasktrooper/server/internal/port/mocks"
)

type telemetrySourceKind struct {
	*mocks.DataSourceKindAdapter
	*mocks.MetricsQuerier
	*mocks.Discoverer
	*mocks.LabelValuesLister
	*mocks.ErrorsQuerier
}

type telemetryFixture struct {
	ds       domain.DataSource
	comp     domain.Component
	bindings *mocks.SignalBindingStore
	envs     *mocks.EnvironmentStore
	metrics  *mocks.MetricsQuerier
	finder   *mocks.Discoverer
	values   *mocks.LabelValuesLister
	errs     *mocks.ErrorsQuerier
	svc      *signals.Service
}

func newTelemetryFixture(t *testing.T) *telemetryFixture {
	f := &telemetryFixture{
		ds: domain.DataSource{ID: uuid.New(), Kind: domain.DataSourceKindTaskTrooperTelemetry, Name: "TaskTrooper Telemetry", UpdatedAt: time.Now()},
		comp: domain.Component{
			ID: uuid.New(), RepositoryID: uuid.New(), Path: "services/orders",
			Name: domain.Detected("orders", domain.ConfidenceHigh), Role: domain.Detected(domain.ComponentRoleBackend, domain.ConfidenceHigh),
			Status: domain.ComponentStatusActive,
		},
		bindings: mocks.NewSignalBindingStore(t), envs: mocks.NewEnvironmentStore(t),
		metrics: mocks.NewMetricsQuerier(t), finder: mocks.NewDiscoverer(t), values: mocks.NewLabelValuesLister(t), errs: mocks.NewErrorsQuerier(t),
	}
	store := mocks.NewDataSourceStore(t)
	store.On("ListDataSources", mock.Anything).Return([]domain.DataSource{f.ds}, nil).Maybe()
	store.On("DataSourceSecrets", mock.Anything, f.ds.ID).Return(map[string]string{}, nil).Maybe()
	recipes := mocks.NewObservabilityRecipeStore(t)
	recipes.On("ListUserRecipes", mock.Anything).Return(nil, nil).Maybe()
	comps := mocks.NewComponentStore(t)
	comps.On("GetComponent", mock.Anything, f.comp.ID).Return(f.comp, nil).Maybe()
	comps.On("ListAllComponents", mock.Anything).Return([]domain.Component{f.comp}, nil).Maybe()

	k := mocks.NewDataSourceKindAdapter(t)
	k.On("Kind").Return(domain.DataSourceKindTaskTrooperTelemetry).Maybe()
	k.On("Schema").Return(domain.DataSourceSchema{Label: "TaskTrooper Telemetry"}).Maybe()
	k.On("Capabilities").Return([]domain.Capability{domain.CapabilityMetrics, domain.CapabilityErrors}).Maybe()
	policy := urlguard.HumanConfigured()
	sources, err := datasource.NewService(datasource.Deps{
		Store: store, Recipes: recipes, URLPolicy: &policy,
		Kinds: []port.DataSourceKindAdapter{telemetrySourceKind{k, f.metrics, f.finder, f.values, f.errs}},
	})
	require.NoError(t, err)
	f.svc, err = signals.NewService(signals.Deps{Bindings: f.bindings, Sources: sources, Components: comps, Environments: f.envs},
		signals.WithQueryTimeout(2*time.Second))
	require.NoError(t, err)
	return f
}

func TestSuggestBindsAComponentToItsTelemetryServiceAndEnvironment(t *testing.T) {
	f := newTelemetryFixture(t)
	ctx := context.Background()
	f.bindings.On("ListSignalBindings", mock.Anything, f.comp.ID, domain.EnvironmentStaging).Return(nil, nil).Twice()
	f.envs.On("ListEnvironments", mock.Anything, f.comp.RepositoryID).Return(nil, nil).Once()
	f.finder.On("Discover", mock.Anything, sameSource(f.ds), mock.Anything).
		Return(domain.Inventory{Metrics: []string{domain.TelemetryLabelEnvironment, domain.TelemetryLabelService}}, nil).Once()
	f.values.On("LabelValues", mock.Anything, sameSource(f.ds), mock.Anything, domain.TelemetryLabelService).
		Return([]string{"billing", "orders", "orders-worker"}, nil).Once()
	f.values.On("LabelValues", mock.Anything, sameSource(f.ds), mock.Anything, domain.TelemetryLabelEnvironment).
		Return([]string{"prod", "stg"}, nil).Once()
	var offered []domain.SignalBinding
	f.bindings.On("ReplaceDetectedSuggestions", mock.Anything, f.comp.ID, domain.EnvironmentStaging, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) { offered = args.Get(4).([]domain.SignalBinding) }).Return(nil, nil).Once()

	_, err := f.svc.Suggest(ctx, f.comp.ID, domain.EnvironmentStaging)

	require.NoError(t, err)
	got := map[string]map[string]string{}
	for _, b := range offered {
		assert.Equal(t, "tasktrooper-telemetry", b.RecipeID)
		got[b.Signal] = b.Params
	}
	want := map[string]string{"service": "orders", "env": "stg"}
	for _, sig := range []string{"http.requests", "http.error_ratio", "http.latency", "logs.lines", "traces.spans", "errors.issues"} {
		assert.Equal(t, want, got[sig], sig)
	}
	assert.NotContains(t, got, "process.errors", "a worker signal is not offered to a backend")
}

func TestATelemetryBindingReadsSeriesAndErrorsThroughTheKind(t *testing.T) {
	f := newTelemetryFixture(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Minute)
	bound := func(signal string) domain.SignalBinding {
		return domain.SignalBinding{
			ID: uuid.New(), ComponentID: f.comp.ID, Environment: domain.EnvironmentProduction, Signal: signal,
			DataSourceID: f.ds.ID, RecipeID: "tasktrooper-telemetry", Params: map[string]string{"service": "orders"},
			Status: domain.SignalBindingConfirmed, Source: domain.SignalBindingHuman,
		}
	}
	f.bindings.On("ListSignalBindings", mock.Anything, f.comp.ID, domain.EnvironmentProduction).
		Return([]domain.SignalBinding{bound("http.latency")}, nil).Once()
	f.metrics.On("Series", mock.Anything, sameSource(f.ds), map[string]string{}, mock.MatchedBy(func(q domain.SeriesQuery) bool {
		return q.Query == `{"service": "orders", "env": "*", "metric": "latency", "quantile": "0.99"}`
	})).Return(domain.SeriesResult{Series: []domain.Series{{Name: "latency", Points: []domain.SeriesPoint{domain.FinitePoint(now, 42)}}}}, nil).Once()

	series, err := f.svc.ReadSignal(ctx, f.comp.ID, domain.EnvironmentProduction, "http.latency", 0.99, now.Add(-time.Hour), now, time.Minute)
	require.NoError(t, err)
	require.Len(t, series, 1)

	f.errs.On("Errors", mock.Anything, sameSource(f.ds), map[string]string{}, mock.MatchedBy(func(q domain.ErrorsQuery) bool {
		return q.Query == `{"service": "orders", "env": "*"}`
	})).Return([]domain.RuntimeErrorGroup{{Fingerprint: "1", Message: "boom", Count: 3}}, nil).Once()
	groups, err := f.svc.ReadErrors(ctx, bound(domain.SignalErrorsIssues), domain.ErrorsQuery{Since: now.Add(-time.Hour), Until: now})
	require.NoError(t, err)
	require.Len(t, groups, 1)
	assert.Equal(t, "boom", groups[0].Message)
}
