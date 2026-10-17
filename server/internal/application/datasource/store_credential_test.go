package datasource_test

import (
	"context"
	"errors"
	"testing"
	"testing/fstest"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/datasource"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/makifbaysal/tasktrooper/server/internal/port/mocks"
)

// storeKind records the reader the service hands it; that hand-over is what
// SetStoreCredentials is for, so it is the behavior under test.
type storeKind struct {
	*mocks.DataSourceKindAdapter
	*mocks.MetricsQuerier
	*mocks.ErrorsQuerier
	creds port.StoreCredentialReader
}

func (k *storeKind) SetStoreCredentials(r port.StoreCredentialReader) { k.creds = r }

func storeSchema() domain.DataSourceSchema {
	return domain.DataSourceSchema{
		Label: "Google Play vitals",
		Fields: []domain.DataSourceField{
			{
				Key: "store_credential", Type: domain.FieldStoreCredential, Required: true,
				Provider: domain.CloudProviderKind(domain.StoreCredentialGooglePlay), Default: domain.StoreCredentialGooglePlay,
			},
			{Key: "package_name", Type: domain.FieldText},
		},
	}
}

type StoreCredentialFieldSuite struct {
	suite.Suite
	ctx   context.Context
	store *mocks.DataSourceStore
	creds *mocks.StoreCredentialReader
	kind  *storeKind
	svc   *datasource.Service
}

func TestStoreCredentialFieldSuite(t *testing.T) { suite.Run(t, new(StoreCredentialFieldSuite)) }

func (s *StoreCredentialFieldSuite) SetupTest() {
	s.ctx = context.Background()
	s.store = &mocks.DataSourceStore{}
	s.creds = &mocks.StoreCredentialReader{}
	s.kind = &storeKind{DataSourceKindAdapter: &mocks.DataSourceKindAdapter{}, MetricsQuerier: &mocks.MetricsQuerier{}, ErrorsQuerier: &mocks.ErrorsQuerier{}}
	s.kind.DataSourceKindAdapter.On("Kind").Return(domain.DataSourceKindGooglePlayVitals).Maybe()
	s.kind.DataSourceKindAdapter.On("Schema").Return(storeSchema()).Maybe()
	s.kind.DataSourceKindAdapter.On("Capabilities").Return([]domain.Capability{domain.CapabilityMetrics}).Maybe()
	policy := urlguard.HumanConfigured()
	svc, err := datasource.NewService(datasource.Deps{
		Store:            s.store,
		StoreCredentials: s.creds,
		Kinds:            []port.DataSourceKindAdapter{s.kind},
		URLPolicy:        &policy,
	})
	s.Require().NoError(err)
	s.svc = svc
}

func (s *StoreCredentialFieldSuite) TearDownTest() {
	s.store.AssertExpectations(s.T())
	s.creds.AssertExpectations(s.T())
	s.kind.MetricsQuerier.AssertExpectations(s.T())
	s.kind.ErrorsQuerier.AssertExpectations(s.T())
}

func (s *StoreCredentialFieldSuite) TestTheServiceHandsKindsItsCredentialReader() {
	s.Same(s.creds, s.kind.creds)
}

func (s *StoreCredentialFieldSuite) TestCreateTakesTheDefaultProviderAndStoresNoCopyOfTheCredential() {
	id := uuid.New()
	s.creds.On("StoreCredential", mock.Anything, domain.StoreCredentialGooglePlay).
		Return(domain.StoreCredential{Provider: domain.StoreCredentialGooglePlay, Data: map[string]string{"service_account_json": "{}"}}, nil)
	s.store.On("CreateDataSource", mock.Anything, domain.DataSource{
		Kind: domain.DataSourceKindGooglePlayVitals, Name: "Google Play vitals",
		Config: map[string]string{"store_credential": "google_play", "package_name": "com.acme.app"},
		Status: domain.DataSourceUnverified,
	}, map[string]string{}).Return(func(_ context.Context, ds domain.DataSource, _ map[string]string) (domain.DataSource, error) {
		ds.ID = id
		return ds, nil
	})
	s.kind.DataSourceKindAdapter.On("Verify", mock.Anything, mock.Anything, mock.Anything).Return(domain.DataSourceInfo{}, nil)
	s.store.On("RecordVerification", mock.Anything, id, mock.Anything, mock.Anything).Return(recorded(nil))

	_, err := s.svc.Create(s.ctx, domain.SaveDataSourceRequest{
		Kind: domain.DataSourceKindGooglePlayVitals, Config: map[string]string{"package_name": "com.acme.app"},
	})
	s.Require().NoError(err)
}

func (s *StoreCredentialFieldSuite) TestCreateRefusesACredentialThatDoesNotFit() {
	storeDown := errors.New("connection reset")
	cases := []struct {
		name   string
		value  string
		setup  func()
		want   datasource.FieldErrors
		failed error
	}{
		{"none saved", "google_play", func() {
			s.creds.On("StoreCredential", mock.Anything, domain.StoreCredentialGooglePlay).Return(domain.StoreCredential{}, port.ErrNotFound).Once()
		}, datasource.FieldErrors{"store_credential": datasource.FieldNotFound}, nil},
		{"the other store's", "asc", func() {}, datasource.FieldErrors{"store_credential": datasource.FieldWrongProvider}, nil},
		{"the other store's, named in full", "app_store_connect", func() {}, datasource.FieldErrors{"store_credential": datasource.FieldWrongProvider}, nil},
		{"not a store", "dropbox", func() {}, datasource.FieldErrors{"store_credential": datasource.FieldInvalid}, nil},
		{"the store failing", "google_play", func() {
			s.creds.On("StoreCredential", mock.Anything, domain.StoreCredentialGooglePlay).Return(domain.StoreCredential{}, storeDown).Once()
		}, nil, storeDown},
	}
	for _, tc := range cases {
		s.Run(tc.name, func() {
			tc.setup()
			_, err := s.svc.Create(s.ctx, domain.SaveDataSourceRequest{
				Kind: domain.DataSourceKindGooglePlayVitals, Config: map[string]string{"store_credential": tc.value},
			})
			if tc.failed != nil {
				s.ErrorIs(err, tc.failed)
				return
			}
			var fe datasource.FieldErrors
			s.Require().ErrorAs(err, &fe)
			s.Equal(tc.want, fe)
		})
	}
}

func (s *StoreCredentialFieldSuite) TestAnErrorQuotingTheStoreCredentialIsMasked() {
	id := uuid.New()
	ds := domain.DataSource{ID: id, Kind: domain.DataSourceKindGooglePlayVitals, Config: map[string]string{"store_credential": "google_play"}}
	s.store.On("GetDataSource", mock.Anything, id).Return(ds, nil)
	s.store.On("DataSourceSecrets", mock.Anything, id).Return(map[string]string{}, nil)
	s.creds.On("StoreCredential", mock.Anything, domain.StoreCredentialGooglePlay).Return(domain.StoreCredential{
		Provider: domain.StoreCredentialGooglePlay,
		Data:     map[string]string{"service_account_json": `{"client_email":"x@y","private_key":"-----PRIVATE-KEY-MATERIAL-----"}`},
	}, nil)
	s.kind.MetricsQuerier.On("Series", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(domain.SeriesResult{}, errors.New("upstream echoed -----PRIVATE-KEY-MATERIAL-----"))

	from := time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC)
	_, err := s.svc.Query(s.ctx, id, domain.SeriesQuery{Query: `{"metric_set":"crashRateMetricSet","metric":"crashRate"}`, From: from, To: from.Add(time.Hour)})

	s.Require().Error(err)
	s.ErrorIs(err, datasource.ErrUpstream)
	s.NotContains(err.Error(), "PRIVATE-KEY-MATERIAL")
}

func (s *StoreCredentialFieldSuite) TestBatchErrorsCapsAndRedactsTheGroups() {
	id := uuid.New()
	ds := domain.DataSource{ID: id, Kind: domain.DataSourceKindGooglePlayVitals, Config: map[string]string{"package_name": "com.acme.app"}}
	s.store.On("GetDataSource", mock.Anything, id).Return(ds, nil)
	s.store.On("DataSourceSecrets", mock.Anything, id).Return(map[string]string{"token": "tok-secret-1"}, nil)
	since := time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC)
	s.kind.ErrorsQuerier.On("Errors", mock.Anything, mock.Anything, mock.Anything, domain.ErrorsQuery{
		Query: `{"project":"p"}`, Since: since, Until: since.Add(time.Hour), Limit: 1,
	}).Return([]domain.RuntimeErrorGroup{{Fingerprint: "1", Message: "auth tok-secret-1 rejected"}, {Fingerprint: "2"}}, nil)

	groups, err := s.svc.Batch().Errors(s.ctx, id, domain.ErrorsQuery{Query: ` {"project":"p"} `, Since: since, Until: since.Add(time.Hour), Limit: 1})
	s.Require().NoError(err)
	s.Require().Len(groups, 1)
	s.Equal("auth [redacted] rejected", groups[0].Message)

	_, err = s.svc.Batch().Errors(s.ctx, id, domain.ErrorsQuery{Query: "q", Since: since, Until: since})
	var fe datasource.FieldErrors
	s.Require().ErrorAs(err, &fe)
	s.Equal(datasource.FieldErrors{"since": datasource.FieldInvalid}, fe)
	s.True(s.svc.CanReadErrors(domain.DataSourceKindGooglePlayVitals))
	s.False(s.svc.CanReadErrors(domain.DataSourceKindPrometheus))
}

func (s *StoreCredentialFieldSuite) TestStoreCredentialConfigNamesTheFieldAndItsProvider() {
	config, provider, ok := s.svc.StoreCredentialConfig(domain.DataSourceKindGooglePlayVitals)
	s.True(ok)
	s.Equal(map[string]string{"store_credential": "google_play"}, config)
	s.Equal(domain.StoreCredentialGooglePlay, provider)
	_, _, ok = s.svc.StoreCredentialConfig(domain.DataSourceKindSentry)
	s.False(ok)
}

func TestWithoutACredentialReaderAStoreCredentialFieldTakesNoValue(t *testing.T) {
	kind := &mocks.DataSourceKindAdapter{}
	kind.On("Kind").Return(domain.DataSourceKindGooglePlayVitals)
	kind.On("Schema").Return(storeSchema())
	policy := urlguard.HumanConfigured()
	svc, err := datasource.NewService(datasource.Deps{Store: &mocks.DataSourceStore{}, Kinds: []port.DataSourceKindAdapter{kind}, URLPolicy: &policy})
	require.NoError(t, err)

	_, err = svc.Create(context.Background(), domain.SaveDataSourceRequest{Kind: domain.DataSourceKindGooglePlayVitals})

	var fe datasource.FieldErrors
	require.ErrorAs(t, err, &fe)
	require.Equal(t, datasource.FieldErrors{"store_credential": datasource.FieldNotPermitted}, fe)
}

func TestAStoreCredentialFieldNamingNoProviderTakesNoValue(t *testing.T) {
	schema := storeSchema()
	schema.Fields[0].Provider = ""
	kind := &mocks.DataSourceKindAdapter{}
	kind.On("Kind").Return(domain.DataSourceKindGooglePlayVitals)
	kind.On("Schema").Return(schema)
	creds := mocks.NewStoreCredentialReader(t)
	policy := urlguard.HumanConfigured()
	svc, err := datasource.NewService(datasource.Deps{
		Store: &mocks.DataSourceStore{}, StoreCredentials: creds, Kinds: []port.DataSourceKindAdapter{kind}, URLPolicy: &policy,
	})
	require.NoError(t, err)

	_, err = svc.Create(context.Background(), domain.SaveDataSourceRequest{
		Kind: domain.DataSourceKindGooglePlayVitals, Config: map[string]string{"store_credential": "asc"},
	})

	var fe datasource.FieldErrors
	require.ErrorAs(t, err, &fe)
	require.Equal(t, datasource.FieldErrors{"store_credential": datasource.FieldNotPermitted}, fe, "a field that says no provider reads none, rather than any")
}

func TestValidateRecipeHoldsTheStoreAndSentryQueriesToTheirNameKeys(t *testing.T) {
	c, err := datasource.LoadCatalog(fstest.MapFS{"signals.yaml": {Data: []byte(mobileSignals)}}, domain.BuiltinDataSourceKinds())
	require.NoError(t, err)
	recipe := func(kind domain.DataSourceKind, query string) domain.Recipe {
		return domain.Recipe{ID: "r", SourceKind: kind, Params: []string{"package", "app", "project", "env"},
			Signals: map[string]string{"mobile.crash_rate": query}}
	}
	for _, ok := range []struct {
		kind  domain.DataSourceKind
		query string
	}{
		{domain.DataSourceKindGooglePlayVitals, `{"package": "$package", "metric_set": "crashRateMetricSet", "metric": "crashRate", "as": "percent"}`},
		{domain.DataSourceKindAppStoreMetrics, `{"app": "$app", "metric": "launchTime", "percentile": "p90"}`},
		{domain.DataSourceKindSentry, `{"project": "$project", "environment": "$env", "metric": "crash_rate"}`},
	} {
		assert.NoError(t, c.ValidateRecipe(recipe(ok.kind, ok.query)), ok.query)
	}
	cases := []struct {
		name  string
		kind  domain.DataSourceKind
		query string
		want  string
	}{
		{"a param in the filter", domain.DataSourceKindGooglePlayVitals, `{"package": "p.q", "metric_set": "crashRateMetricSet", "metric": "crashRate", "filter": "versionCode = $app"}`, "may appear only in the package value"},
		{"a built-in", domain.DataSourceKindSentry, `{"project": "p", "release": "$__interval"}`, "Sentry picks the interval"},
		{"the metric chosen by a param", domain.DataSourceKindAppStoreMetrics, `{"app": "1", "metric": "$app"}`, "may appear only in the app or version or build value"},
		{"an unknown metric set", domain.DataSourceKindGooglePlayVitals, `{"package": "$package", "metric_set": "batteryMetricSet", "metric": "x"}`, "is not one of"},
		{"an unknown Sentry metric", domain.DataSourceKindSentry, `{"project": "$project", "metric": "apdex"}`, "not one of"},
		{"no project", domain.DataSourceKindSentry, `{"metric": "events"}`, "project is required"},
		{"a series and a table at once", domain.DataSourceKindAppStoreMetrics, `{"app": "$app", "metric": "launchTime", "diagnostics": "hangs"}`, "not both"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := c.ValidateRecipe(recipe(tc.kind, tc.query))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

const mobileSignals = `signals:
  - {id: mobile.crash_rate, kind: metric, unit: "%", roles: [mobile], aggregation: ratio, better: lower}
`

func TestLoadCatalogReadsStoreAppliesToAndOneQueryForANamedColumnTable(t *testing.T) {
	c, err := datasource.LoadCatalog(fstest.MapFS{
		"signals.yaml": {Data: []byte(mobileSignals)},
		"recipes/r.yaml": {Data: []byte(`id: r
source_kind: google_play_vitals
applies_to: {store_platform: android, store_params: {package: identifier}}
params: [package]
signals: {mobile.crash_rate: '{"package": "$package", "metric_set": "crashRateMetricSet", "metric": "crashRate"}'}
tables:
  crashes:
    key_labels: [issue]
    columns: [{key: issue, label: Issue}, {key: cause, label: Cause}, {key: users, label: Users}]
    query: '{"package": "$package", "issues": "all"}'
`)},
	}, domain.BuiltinDataSourceKinds())
	require.NoError(t, err)
	r := c.Recipes()[0]
	assert.Equal(t, "android", r.AppliesTo.StorePlatform)
	assert.Equal(t, map[string]string{"package": "identifier"}, r.AppliesTo.StoreParams)
	q := `{"package": "$package", "issues": "all"}`
	assert.Equal(t, map[string]string{"cause": q, "users": q}, r.Tables[domain.RecipeTableCrashes].Queries)
	assert.Equal(t, map[string]string{"package": "com.acme.app"}, r.ParamsFromStoreApp(domain.MobileStoreApp{Platform: "android", Identifier: "com.acme.app"}))
}

func TestLoadCatalogRefusesAStoreAppliesToThatDoesNotFit(t *testing.T) {
	cases := []struct{ name, appliesTo, want string }{
		{"an unknown platform", `{store_platform: windows}`, `store_platform "windows" is not ios or android`},
		{"params without a platform", `{store_params: {package: identifier}}`, "store_params needs a store_platform"},
		{"an unknown field", `{store_platform: android, store_params: {package: bundle}}`, `"bundle" is not identifier or store_app_id`},
		{"an undeclared param", `{store_platform: android, store_params: {pkg: identifier}}`, `names "pkg", which is not a declared param`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := datasource.LoadCatalog(fstest.MapFS{
				"signals.yaml": {Data: []byte(mobileSignals)},
				"recipes/r.yaml": {Data: []byte(`id: r
source_kind: google_play_vitals
applies_to: ` + tc.appliesTo + `
params: [package]
signals: {mobile.crash_rate: '{"package": "$package", "metric_set": "crashRateMetricSet", "metric": "crashRate"}'}
`)},
			}, domain.BuiltinDataSourceKinds())
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}
