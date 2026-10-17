package signals_test

import (
	"context"
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

type sentryKind struct {
	*mocks.DataSourceKindAdapter
	*mocks.MetricsQuerier
	*mocks.ErrorsQuerier
}

func storeKindMock(t *testing.T, kind domain.DataSourceKind, provider string) *mocks.DataSourceKindAdapter {
	k := mocks.NewDataSourceKindAdapter(t)
	k.On("Kind").Return(kind).Maybe()
	k.On("Capabilities").Return([]domain.Capability{domain.CapabilityMetrics}).Maybe()
	k.On("Schema").Return(domain.DataSourceSchema{Label: string(kind), Fields: []domain.DataSourceField{
		{Key: "store_credential", Type: domain.FieldStoreCredential, Provider: domain.CloudProviderKind(provider), Default: provider},
	}}).Maybe()
	return k
}

// StoreAppsSuite runs the detector and the errors bindings over the embedded
// catalog's store and Sentry recipes.
type StoreAppsSuite struct {
	suite.Suite
	ctx      context.Context
	now      time.Time
	dsStore  *mocks.DataSourceStore
	apps     *mocks.MobileStoreAppStore
	bindings *mocks.SignalBindingStore
	comps    *mocks.ComponentStore
	envs     *mocks.EnvironmentStore
	sentry   sentryKind
	sources  []domain.DataSource
	svc      *signals.Service
}

func TestStoreAppsSuite(t *testing.T) { suite.Run(t, new(StoreAppsSuite)) }

func (s *StoreAppsSuite) SetupTest() {
	t := s.T()
	s.ctx = context.Background()
	s.now = time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)
	s.dsStore = mocks.NewDataSourceStore(t)
	s.apps = mocks.NewMobileStoreAppStore(t)
	s.bindings = mocks.NewSignalBindingStore(t)
	s.comps = mocks.NewComponentStore(t)
	s.envs = mocks.NewEnvironmentStore(t)
	s.sentry = sentryKind{mocks.NewDataSourceKindAdapter(t), mocks.NewMetricsQuerier(t), mocks.NewErrorsQuerier(t)}
	s.sentry.DataSourceKindAdapter.On("Kind").Return(domain.DataSourceKindSentry).Maybe()
	s.sentry.DataSourceKindAdapter.On("Schema").Return(domain.DataSourceSchema{Label: "Sentry"}).Maybe()
	s.sentry.DataSourceKindAdapter.On("Capabilities").Return([]domain.Capability{domain.CapabilityErrors}).Maybe()
	policy := urlguard.HumanConfigured()
	sources, err := datasource.NewService(datasource.Deps{
		Store: s.dsStore,
		Kinds: []port.DataSourceKindAdapter{
			storeKindMock(t, domain.DataSourceKindGooglePlayVitals, domain.StoreCredentialGooglePlay),
			storeKindMock(t, domain.DataSourceKindAppStoreMetrics, domain.StoreCredentialASC),
			s.sentry,
		},
		URLPolicy: &policy,
	})
	s.Require().NoError(err)
	s.sources = nil
	s.dsStore.On("ListDataSources", mock.Anything).Return(func(context.Context) ([]domain.DataSource, error) { return s.sources, nil }).Maybe()
	svc, err := signals.NewService(signals.Deps{
		Bindings: s.bindings, Sources: sources, Components: s.comps, Environments: s.envs, StoreApps: s.apps,
	}, signals.WithClock(func() time.Time { return s.now }))
	s.Require().NoError(err)
	s.svc = svc
}

func (s *StoreAppsSuite) addSource(kind domain.DataSourceKind) domain.DataSource {
	ds := domain.DataSource{ID: uuid.New(), Kind: kind, Name: string(kind), UpdatedAt: s.now}
	s.sources = append(s.sources, ds)
	s.dsStore.On("GetDataSource", mock.Anything, ds.ID).Return(ds, nil).Maybe()
	s.dsStore.On("DataSourceSecrets", mock.Anything, ds.ID).Return(map[string]string{}, nil).Maybe()
	return ds
}

func (s *StoreAppsSuite) component(role domain.ComponentRole) domain.Component {
	c := domain.Component{
		ID: uuid.New(), RepositoryID: uuid.New(), Name: domain.Detected("app", domain.ConfidenceHigh),
		Role: domain.Detected(role, domain.ConfidenceHigh), Status: domain.ComponentStatusActive,
	}
	s.comps.On("GetComponent", mock.Anything, c.ID).Return(c, nil).Maybe()
	s.envs.On("ListEnvironments", mock.Anything, c.RepositoryID).Return([]domain.ComponentEnvironment{}, nil).Maybe()
	return c
}

func (s *StoreAppsSuite) expectReplace(c domain.Component) (*[]domain.SignalBinding, *[]uuid.UUID) {
	var saved []domain.SignalBinding
	var answered []uuid.UUID
	s.bindings.On("ListSignalBindings", mock.Anything, c.ID, domain.EnvironmentProduction).Return([]domain.SignalBinding{}, nil).Once()
	s.bindings.On("ReplaceDetectedSuggestions", mock.Anything, c.ID, domain.EnvironmentProduction, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			answered = args.Get(3).([]uuid.UUID)
			saved = args.Get(4).([]domain.SignalBinding)
		}).Return([]domain.SignalBinding{}, nil).Once()
	s.bindings.On("ListSignalBindings", mock.Anything, c.ID, domain.EnvironmentProduction).
		Return(func(context.Context, uuid.UUID, domain.DeployEnvironment) ([]domain.SignalBinding, error) {
			return saved, nil
		}).Once()
	return &saved, &answered
}

func (s *StoreAppsSuite) TestALinkedAndroidAppSuggestsPlayVitalsWithItsPackageAndAnIOSAppOffersTheMissingSource() {
	play := s.addSource(domain.DataSourceKindGooglePlayVitals)
	app := s.component(domain.ComponentRoleMobile)
	s.apps.On("ListByRepository", mock.Anything, app.RepositoryID).Return([]domain.MobileStoreApp{
		{Platform: domain.MobileStorePlatformIOS, Identifier: "com.acme.ios", StoreAppID: "1234567890"},
		{Platform: domain.MobileStorePlatformAndroid, Identifier: "com.acme.app"},
	}, nil).Once()
	saved, answered := s.expectReplace(app)

	got, err := s.svc.Suggest(s.ctx, app.ID, domain.EnvironmentProduction)
	s.Require().NoError(err)

	s.Equal([]string{
		"mobile.anr_rate play-vitals package=com.acme.app",
		"mobile.crash_free_users play-vitals package=com.acme.app",
		"mobile.crash_rate play-vitals package=com.acme.app",
	}, summarizeBindings(*saved))
	for _, b := range *saved {
		s.Equal(play.ID, b.DataSourceID)
		s.Equal(domain.SignalBindingDetected, b.Source)
	}
	s.Contains(*answered, play.ID, "a kind that lists nothing still answers, so its old suggestions are replaced")
	s.Equal([]domain.DataSourceOffer{{
		Kind: domain.DataSourceKindAppStoreMetrics, Config: map[string]string{"store_credential": "asc"},
		RecipeID: "app-store-metrics", Params: map[string]string{"app": "1234567890"},
		Signals: []string{"mobile.cold_start"}, StoreCredential: "asc",
	}}, got.SourceOffers)
	crash := signalByID(got, "mobile.crash_rate")
	s.Require().NotNil(crash)
	s.Equal(domain.SignalStateSuggested, crash.Status)
	s.Equal(`{"package": "com.acme.app", "metric_set": "crashRateMetricSet", "metric": "userPerceivedCrashRate", "as": "percent"}`,
		crash.Suggestions[0].RenderedQuery)
}

func (s *StoreAppsSuite) TestAnIOSAppNotChosenYetLeavesTheAppParamToFill() {
	s.addSource(domain.DataSourceKindAppStoreMetrics)
	app := s.component(domain.ComponentRoleMobile)
	s.apps.On("ListByRepository", mock.Anything, app.RepositoryID).Return([]domain.MobileStoreApp{
		{Platform: domain.MobileStorePlatformIOS, Identifier: "com.acme.ios"},
	}, nil).Once()
	saved, _ := s.expectReplace(app)

	got, err := s.svc.Suggest(s.ctx, app.ID, domain.EnvironmentProduction)
	s.Require().NoError(err)
	s.Equal([]string{"mobile.cold_start app-store-metrics "}, summarizeBindings(*saved))
	s.Equal([]domain.DataSourceOffer{}, got.SourceOffers)
	cold := signalByID(got, "mobile.cold_start")
	s.Equal([]string{"app"}, cold.Suggestions[0].NeedsParams)
}

func (s *StoreAppsSuite) TestAComponentThatIsNotMobileIsOfferedNoStoreRecipe() {
	s.addSource(domain.DataSourceKindGooglePlayVitals)
	api := s.component(domain.ComponentRoleBackend)
	saved, _ := s.expectReplace(api)

	got, err := s.svc.Suggest(s.ctx, api.ID, domain.EnvironmentProduction)
	s.Require().NoError(err)
	s.Empty(*saved)
	s.Equal([]domain.DataSourceOffer{}, got.SourceOffers)
	s.apps.AssertNotCalled(s.T(), "ListByRepository", mock.Anything, mock.Anything)
}

func (s *StoreAppsSuite) TestErrorBindingsAreTheConfirmedErrorsIssuesOnSourcesThatListErrors() {
	sentrySrc := s.addSource(domain.DataSourceKindSentry)
	play := s.addSource(domain.DataSourceKindGooglePlayVitals)
	app, gone := s.component(domain.ComponentRoleMobile), s.component(domain.ComponentRoleMobile)
	gone.Status = domain.ComponentStatusDismissed
	bind := func(c domain.Component, ds domain.DataSource, signal string) domain.SignalBinding {
		return domain.SignalBinding{
			ID: uuid.New(), ComponentID: c.ID, Environment: domain.EnvironmentProduction, Signal: signal,
			DataSourceID: ds.ID, RecipeID: "sentry-mobile", Params: map[string]string{"project": "ios-app"},
			Status: domain.SignalBindingConfirmed,
		}
	}
	read := bind(app, sentrySrc, domain.SignalErrorsIssues)
	onPlay := bind(app, play, domain.SignalErrorsIssues)
	onPlay.RecipeID, onPlay.Params, onPlay.Query = "", nil, `{"package":"com.acme.app","issues":"all"}`
	s.bindings.On("ListConfirmedSignalBindings", mock.Anything, domain.EnvironmentProduction).Return([]domain.SignalBinding{
		read, onPlay, bind(gone, sentrySrc, domain.SignalErrorsIssues), bind(app, sentrySrc, "mobile.crash_free_users"),
	}, nil).Once()
	s.comps.On("ListAllComponents", mock.Anything).Return([]domain.Component{app, gone}, nil).Once()

	got, err := s.svc.ErrorBindings(s.ctx, domain.EnvironmentProduction)
	s.Require().NoError(err)
	s.Require().Len(got, 1)
	s.Equal(read.ID, got[0].ID)
	s.Equal(domain.DataSourceKindSentry, got[0].DataSourceKind)

	since := s.now.Add(-24 * time.Hour)
	s.sentry.ErrorsQuerier.On("Errors", mock.Anything, mock.Anything, mock.Anything, domain.ErrorsQuery{
		Query: `{"project": "ios-app"}`, Since: since, Until: s.now, Limit: 10,
	}).Return([]domain.RuntimeErrorGroup{{Fingerprint: "101", Message: "boom"}}, nil).Once()
	groups, err := s.svc.ReadErrors(s.ctx, read, domain.ErrorsQuery{Since: since, Until: s.now, Limit: 10})
	s.Require().NoError(err)
	s.Equal([]domain.RuntimeErrorGroup{{Fingerprint: "101", Message: "boom"}}, groups)
}
