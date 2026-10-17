package signals_test

import (
	"context"
	"fmt"
	"testing"
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

type lokiKind struct {
	*mocks.DataSourceKindAdapter
	*mocks.LogsQuerier
	*mocks.Discoverer
	*mocks.LabelValuesLister
}

type tempoKind struct {
	*mocks.DataSourceKindAdapter
	*mocks.TracesQuerier
	*mocks.LabelValuesLister
}

// LogsTracesSuite reads through the embedded catalog's loki-service,
// loki-app and tempo-service recipes.
type LogsTracesSuite struct {
	suite.Suite
	ctx        context.Context
	now        time.Time
	dsStore    *mocks.DataSourceStore
	logs       *mocks.LogsQuerier
	lokiFinder *mocks.Discoverer
	lokiValues *mocks.LabelValuesLister
	traces     *mocks.TracesQuerier
	bindings   *mocks.SignalBindingStore
	comps      *mocks.ComponentStore
	envs       *mocks.EnvironmentStore
	loki       domain.DataSource
	tempo      domain.DataSource
	api        domain.Component
	sources    *datasource.Service
	svc        *signals.Service
}

func TestLogsTracesSuite(t *testing.T) { suite.Run(t, new(LogsTracesSuite)) }

func (s *LogsTracesSuite) SetupTest() {
	t := s.T()
	s.ctx = context.Background()
	s.now = time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	s.dsStore = mocks.NewDataSourceStore(t)
	recipes := mocks.NewObservabilityRecipeStore(t)
	recipes.On("ListUserRecipes", mock.Anything).Return(nil, nil).Maybe()
	s.logs, s.lokiFinder, s.lokiValues = mocks.NewLogsQuerier(t), mocks.NewDiscoverer(t), mocks.NewLabelValuesLister(t)
	s.traces = mocks.NewTracesQuerier(t)
	s.bindings, s.comps, s.envs = mocks.NewSignalBindingStore(t), mocks.NewComponentStore(t), mocks.NewEnvironmentStore(t)

	lk := mocks.NewDataSourceKindAdapter(t)
	lk.On("Kind").Return(domain.DataSourceKindLoki).Maybe()
	lk.On("Schema").Return(domain.DataSourceSchema{Label: "Loki"}).Maybe()
	lk.On("Capabilities").Return([]domain.Capability{domain.CapabilityLogs, domain.CapabilityMetrics}).Maybe()
	tk := mocks.NewDataSourceKindAdapter(t)
	tk.On("Kind").Return(domain.DataSourceKindTempo).Maybe()
	tk.On("Schema").Return(domain.DataSourceSchema{Label: "Tempo"}).Maybe()
	tk.On("Capabilities").Return([]domain.Capability{domain.CapabilityTraces}).Maybe()

	s.loki = domain.DataSource{ID: uuid.New(), Kind: domain.DataSourceKindLoki, Name: "loki", UpdatedAt: s.now}
	s.tempo = domain.DataSource{ID: uuid.New(), Kind: domain.DataSourceKindTempo, Name: "tempo", UpdatedAt: s.now}
	s.dsStore.On("ListDataSources", mock.Anything).Return([]domain.DataSource{s.loki, s.tempo}, nil).Maybe()
	for _, ds := range []domain.DataSource{s.loki, s.tempo} {
		s.dsStore.On("DataSourceSecrets", mock.Anything, ds.ID).Return(map[string]string{}, nil).Maybe()
	}
	s.api = domain.Component{
		ID: uuid.New(), RepositoryID: uuid.New(), Path: "services/api",
		Name: domain.Detected("api", domain.ConfidenceHigh), Role: domain.Detected(domain.ComponentRoleBackend, domain.ConfidenceHigh),
		Status: domain.ComponentStatusActive,
	}
	s.comps.On("GetComponent", mock.Anything, s.api.ID).Return(s.api, nil).Maybe()

	policy := urlguard.HumanConfigured()
	sources, err := datasource.NewService(datasource.Deps{
		Store: s.dsStore, Recipes: recipes, URLPolicy: &policy,
		Kinds: []port.DataSourceKindAdapter{lokiKind{lk, s.logs, s.lokiFinder, s.lokiValues}, tempoKind{tk, s.traces, mocks.NewLabelValuesLister(t)}},
	})
	s.Require().NoError(err)
	s.sources = sources
	s.svc, err = signals.NewService(signals.Deps{Bindings: s.bindings, Sources: sources, Components: s.comps, Environments: s.envs},
		signals.WithClock(func() time.Time { return s.now }), signals.WithQueryTimeout(2*time.Second))
	s.Require().NoError(err)
}

func (s *LogsTracesSuite) binding(comp uuid.UUID, signal string, ds domain.DataSource, recipe string, params map[string]string) domain.SignalBinding {
	return domain.SignalBinding{
		ID: uuid.New(), ComponentID: comp, Environment: domain.EnvironmentProduction, Signal: signal,
		DataSourceID: ds.ID, RecipeID: recipe, Params: params, Status: domain.SignalBindingConfirmed, Source: domain.SignalBindingHuman,
	}
}

func (s *LogsTracesSuite) TestReadLogsRendersTheBindingAndHandsTheFiltersToTheKind() {
	b := s.binding(s.api.ID, domain.SignalLogsLines, s.loki, "loki-service", map[string]string{"service": `api"`})
	since, until := s.now.Add(-time.Hour), s.now
	want := domain.LogQuery{Query: `{service_name="api\""}`, Since: since, Until: until, MinSeverity: domain.LogError, Text: "boom", Limit: 50}
	s.logs.On("Logs", mock.Anything, sameSource(s.loki), map[string]string{}, want).
		Return(domain.RuntimeLogPage{Entries: []domain.RuntimeLogEntry{{Message: "boom"}}}, nil).Once()

	page, err := s.svc.ReadLogs(s.ctx, b, domain.LogQuery{Query: "ignored", Since: since, Until: until, MinSeverity: domain.LogError, Text: "boom", Limit: 50})

	s.Require().NoError(err)
	s.Len(page.Entries, 1)
}

func (s *LogsTracesSuite) TestReadLogsRefusesABindingStillMissingAParam() {
	b := s.binding(s.api.ID, domain.SignalLogsLines, s.loki, "loki-service", map[string]string{})

	_, err := s.svc.ReadLogs(s.ctx, b, domain.LogQuery{Since: s.now.Add(-time.Hour), Until: s.now})

	s.ErrorIs(err, datasource.ErrInvalidInput)
	s.ErrorContains(err, "no value for service")
}

func (s *LogsTracesSuite) TestLogBindingsKeepsTheConfirmedLogsBindingsOfLiveComponents() {
	gone := uuid.New()
	mine := s.binding(s.api.ID, domain.SignalLogsLines, s.loki, "loki-service", map[string]string{"service": "api"})
	dismissed := s.binding(gone, domain.SignalLogsLines, s.loki, "loki-service", map[string]string{"service": "old"})
	metric := s.binding(s.api.ID, "process.errors", s.loki, "loki-service", map[string]string{"service": "api"})
	s.bindings.On("ListConfirmedSignalBindings", mock.Anything, domain.EnvironmentProduction).Return([]domain.SignalBinding{mine, dismissed, metric}, nil).Once()
	s.comps.On("ListAllComponents", mock.Anything).Return([]domain.Component{s.api, {ID: gone, Status: domain.ComponentStatusDismissed}}, nil).Once()

	got, err := s.svc.LogBindings(s.ctx, domain.EnvironmentProduction)

	s.Require().NoError(err)
	s.Equal([]domain.SignalBinding{mine}, got)
}

func (s *LogsTracesSuite) TestTracesSearchesTheComponentsBindingOverTheDefaultWindow() {
	b := s.binding(s.api.ID, domain.SignalTracesSpans, s.tempo, "tempo-service", map[string]string{"service": "api"})
	s.bindings.On("ListSignalBindings", mock.Anything, s.api.ID, domain.EnvironmentProduction).Return([]domain.SignalBinding{b}, nil).Once()
	found := []domain.TraceSummary{{ID: "abc", RootService: "api", DurationMS: 12}}
	s.traces.On("SearchTraces", mock.Anything, sameSource(s.tempo), map[string]string{}, domain.TraceQuery{
		Query: `{ resource.service.name = "api" }`, Since: s.now.Add(-signals.DefaultTraceWindow), Until: s.now,
		Limit: datasource.DefaultTraceLimit, MinDuration: 250 * time.Millisecond,
	}).Return(found, nil).Once()

	got, err := s.svc.Traces(s.ctx, s.api.ID, signals.TracesQuery{Environment: domain.EnvironmentProduction, MinDuration: 250 * time.Millisecond})

	s.Require().NoError(err)
	s.True(got.Bound)
	s.Equal(s.tempo.ID, *got.DataSourceID)
	s.Equal(found, got.Traces)
	s.Nil(got.Error)
}

func (s *LogsTracesSuite) TestTracesOfAnUnboundComponentAnswerBoundFalse() {
	s.bindings.On("ListSignalBindings", mock.Anything, s.api.ID, domain.EnvironmentStaging).Return(nil, nil).Once()

	got, err := s.svc.Traces(s.ctx, s.api.ID, signals.TracesQuery{Environment: domain.EnvironmentStaging})

	s.Require().NoError(err)
	s.False(got.Bound)
	s.Equal([]domain.TraceSummary{}, got.Traces)
	s.Nil(got.DataSourceID)
}

func (s *LogsTracesSuite) TestTracesAnswerAFailedReadInTheBody() {
	tests := []struct {
		name   string
		params map[string]string
		fail   error
		want   domain.SignalReadErrorCode
	}{
		{name: "missing param", params: map[string]string{}, want: domain.SignalErrNeedsParams},
		{name: "refused query", params: map[string]string{"service": "api"}, fail: fmt.Errorf("%w: parse error", port.ErrDataSourceQuery), want: domain.SignalErrBadQuery},
		{name: "source down", params: map[string]string{"service": "api"}, fail: fmt.Errorf("Tempo: HTTP 503"), want: domain.SignalErrUpstream},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			b := s.binding(s.api.ID, domain.SignalTracesSpans, s.tempo, "tempo-service", tt.params)
			s.bindings.On("ListSignalBindings", mock.Anything, s.api.ID, domain.EnvironmentProduction).Return([]domain.SignalBinding{b}, nil).Once()
			if tt.fail != nil {
				s.traces.On("SearchTraces", mock.Anything, sameSource(s.tempo), mock.Anything, mock.Anything).Return(nil, tt.fail).Once()
			}

			got, err := s.svc.Traces(s.ctx, s.api.ID, signals.TracesQuery{Environment: domain.EnvironmentProduction})

			s.Require().NoError(err)
			s.Require().NotNil(got.Error)
			s.Equal(tt.want, got.Error.Code)
			s.Equal([]domain.TraceSummary{}, got.Traces)
		})
	}
}

func (s *LogsTracesSuite) TestTracesRefusesABadWindow() {
	_, err := s.svc.Traces(s.ctx, s.api.ID, signals.TracesQuery{Environment: domain.EnvironmentProduction, Since: s.now.Add(-8 * 24 * time.Hour), Until: s.now})
	s.Equal(datasource.FieldErrors{"since": datasource.FieldTooLong}, err)

	_, err = s.svc.Traces(s.ctx, s.api.ID, signals.TracesQuery{Environment: domain.EnvironmentProduction, Limit: 101})
	s.Equal(datasource.FieldErrors{"limit": datasource.FieldInvalid}, err)
}

func (s *LogsTracesSuite) TestTraceReadsThroughTheBinding() {
	b := s.binding(s.api.ID, domain.SignalTracesSpans, s.tempo, "tempo-service", map[string]string{"service": "api"})
	s.bindings.On("ListSignalBindings", mock.Anything, s.api.ID, domain.EnvironmentProduction).Return([]domain.SignalBinding{b}, nil).Once()
	s.traces.On("Trace", mock.Anything, sameSource(s.tempo), map[string]string{}, "abc").Return(domain.Trace{ID: "abc", Spans: []domain.Span{{ID: "1"}}}, nil).Once()

	got, err := s.svc.Trace(s.ctx, s.api.ID, domain.EnvironmentProduction, "ABC")

	s.Require().NoError(err)
	s.Equal("abc", got.Trace.ID)
	s.Equal(s.api.ID, got.ComponentID)
	s.Equal(s.tempo.ID, got.DataSourceID)
}

func (s *LogsTracesSuite) TestTraceWithoutABindingIsUnboundAndAMalformedIDInvalid() {
	s.bindings.On("ListSignalBindings", mock.Anything, s.api.ID, domain.EnvironmentProduction).Return(nil, nil).Once()

	_, err := s.svc.Trace(s.ctx, s.api.ID, domain.EnvironmentProduction, "abc")
	s.ErrorIs(err, signals.ErrUnbound)

	_, err = s.svc.Trace(s.ctx, s.api.ID, domain.EnvironmentProduction, "not-hex")
	s.ErrorIs(err, signals.ErrInvalidInput)
}

func (s *LogsTracesSuite) TestFindTraceTriesEachSourceUntilOneHoldsIt() {
	other := domain.DataSource{ID: uuid.New(), Kind: domain.DataSourceKindTempo, Name: "tempo-eu"}
	gone := domain.DataSource{ID: uuid.New(), Kind: domain.DataSourceKindTempo, Name: "tempo-old"}
	s.dsStore.ExpectedCalls = nil
	s.dsStore.On("ListDataSources", mock.Anything).Return([]domain.DataSource{s.loki, s.tempo, other, gone}, nil).Maybe()
	s.dsStore.On("DataSourceSecrets", mock.Anything, mock.Anything).Return(map[string]string{}, nil).Maybe()
	web, dismissed := uuid.New(), domain.Component{ID: uuid.New(), Status: domain.ComponentStatusDismissed}
	s.comps.On("ListAllComponents", mock.Anything).Return([]domain.Component{s.api, dismissed}, nil).Once()
	first := s.binding(s.api.ID, domain.SignalTracesSpans, s.tempo, "tempo-service", map[string]string{"service": "api"})
	shared := s.binding(uuid.New(), domain.SignalTracesSpans, s.tempo, "tempo-service", map[string]string{"service": "jobs"})
	ofDismissed := s.binding(dismissed.ID, domain.SignalTracesSpans, gone, "tempo-service", map[string]string{"service": "old"})
	second := s.binding(web, domain.SignalTracesSpans, other, "tempo-service", map[string]string{"service": "web"})
	s.bindings.On("ListConfirmedSignalBindings", mock.Anything, domain.EnvironmentProduction).Return([]domain.SignalBinding{first, shared, ofDismissed, second}, nil).Once()
	s.traces.On("Trace", mock.Anything, sameSource(s.tempo), mock.Anything, "abc").Return(domain.Trace{}, fmt.Errorf("gone: %w", port.ErrNotFound)).Once()
	s.traces.On("Trace", mock.Anything, sameSource(other), mock.Anything, "abc").Return(domain.Trace{ID: "abc"}, nil).Once()
	s.traces.AssertNotCalled(s.T(), "Trace", mock.Anything, sameSource(gone), mock.Anything, mock.Anything)

	got, err := s.svc.FindTrace(s.ctx, domain.EnvironmentProduction, "abc")

	s.Require().NoError(err)
	s.Equal(web, got.ComponentID)
	s.Equal(other.ID, got.DataSourceID)
}

func (s *LogsTracesSuite) TestFindTraceWithNoTracesBindingIsUnbound() {
	s.bindings.On("ListConfirmedSignalBindings", mock.Anything, domain.EnvironmentProduction).Return(nil, nil).Once()

	_, err := s.svc.FindTrace(s.ctx, domain.EnvironmentProduction, "abc")

	s.ErrorIs(err, signals.ErrUnbound)
}

func (s *LogsTracesSuite) TestSuggestOffersLogRecipesWithParamsFromLabelValues() {
	s.bindings.On("ListSignalBindings", mock.Anything, s.api.ID, domain.EnvironmentProduction).Return(nil, nil).Twice()
	s.envs.On("ListEnvironments", mock.Anything, s.api.RepositoryID).Return(nil, nil).Once()
	s.lokiFinder.On("Discover", mock.Anything, sameSource(s.loki), mock.Anything).Return(domain.Inventory{Metrics: []string{"app", "service_name"}}, nil).Once()
	s.lokiValues.On("LabelValues", mock.Anything, sameSource(s.loki), mock.Anything, "service_name").Return([]string{"billing", "api", "api-gateway"}, nil).Once()
	s.lokiValues.On("LabelValues", mock.Anything, sameSource(s.loki), mock.Anything, "app").Return([]string{"web"}, nil).Once()
	var offered []domain.SignalBinding
	s.bindings.On("ReplaceDetectedSuggestions", mock.Anything, s.api.ID, domain.EnvironmentProduction, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) { offered = args.Get(4).([]domain.SignalBinding) }).Return(nil, nil).Once()

	_, err := s.svc.Suggest(s.ctx, s.api.ID, domain.EnvironmentProduction)

	s.Require().NoError(err)
	byRecipe := map[string]map[string]string{}
	for _, b := range offered {
		s.Equal(domain.SignalLogsLines, b.Signal, "process.errors is a worker signal")
		byRecipe[b.RecipeID] = b.Params
	}
	s.Equal(map[string]string{"service": "api"}, byRecipe["loki-service"], "the value equal to the component's name wins")
	s.Equal(map[string]string{}, byRecipe["loki-app"], "no app value names it: the suggestion needs its param")
}

// sameSource: the kinds are handed the source as listed, capabilities filled.
func sameSource(ds domain.DataSource) any {
	return mock.MatchedBy(func(got domain.DataSource) bool { return got.ID == ds.ID })
}

func (s *LogsTracesSuite) TestFindTraceAsksTheSourcesAtOnceWithinOneQueryTimeout() {
	slow := make([]domain.DataSource, 4)
	var bound []domain.SignalBinding
	for i := range slow {
		slow[i] = domain.DataSource{ID: uuid.New(), Kind: domain.DataSourceKindTempo, Name: fmt.Sprint("tempo-", i)}
		bound = append(bound, s.binding(uuid.New(), domain.SignalTracesSpans, slow[i], "tempo-service", map[string]string{"service": "x"}))
	}
	s.dsStore.ExpectedCalls = nil
	s.dsStore.On("ListDataSources", mock.Anything).Return(append([]domain.DataSource{s.loki, s.tempo}, slow...), nil).Maybe()
	s.dsStore.On("DataSourceSecrets", mock.Anything, mock.Anything).Return(map[string]string{}, nil).Maybe()
	s.comps.On("ListAllComponents", mock.Anything).Return([]domain.Component{s.api}, nil).Once()
	s.bindings.On("ListConfirmedSignalBindings", mock.Anything, domain.EnvironmentProduction).Return(bound, nil).Once()
	s.traces.On("Trace", mock.Anything, mock.Anything, mock.Anything, "abc").
		Run(func(args mock.Arguments) { <-args.Get(0).(context.Context).Done() }).
		Return(domain.Trace{}, context.DeadlineExceeded)

	const timeout = 200 * time.Millisecond
	svc, err := signals.NewService(signals.Deps{Bindings: s.bindings, Sources: s.sources, Components: s.comps, Environments: s.envs},
		signals.WithClock(func() time.Time { return s.now }), signals.WithQueryTimeout(timeout))
	s.Require().NoError(err)

	start := time.Now()
	_, err = svc.FindTrace(s.ctx, domain.EnvironmentProduction, "abc")

	s.Require().ErrorIs(err, context.DeadlineExceeded)
	s.Less(time.Since(start), 3*timeout, "four slow sources cost one timeout, not four")
}
