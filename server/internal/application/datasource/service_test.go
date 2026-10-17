package datasource_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/datasource"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/makifbaysal/tasktrooper/server/internal/port/mocks"
)

const plainKind domain.DataSourceKind = "errors_only"

type queryingKind struct {
	*mocks.DataSourceKindAdapter
	*mocks.MetricsQuerier
	*mocks.Discoverer
}

func testSchema() domain.DataSourceSchema {
	return domain.DataSourceSchema{
		Label: "Prometheus compatible",
		Fields: []domain.DataSourceField{
			{Key: "url", Label: "URL", Type: domain.FieldURL, Required: true},
			{Key: "auth", Label: "Authentication", Type: domain.FieldSelect, Options: []string{"none", "bearer", "basic"}},
			{Key: "token", Type: domain.FieldSecret, Required: true, ShowWhen: map[string]string{"auth": "bearer"}},
			{Key: "username", Type: domain.FieldText, Required: true, ShowWhen: map[string]string{"auth": "basic"}},
			{Key: "password", Type: domain.FieldSecret, Required: true, ShowWhen: map[string]string{"auth": "basic"}},
			{Key: "headers", Type: domain.FieldHeaders},
		},
	}
}

type ServiceSuite struct {
	suite.Suite
	ctx        context.Context
	store      *mocks.DataSourceStore
	recipes    *mocks.ObservabilityRecipeStore
	kind       *mocks.DataSourceKindAdapter
	querier    *mocks.MetricsQuerier
	discoverer *mocks.Discoverer
	plain      *mocks.DataSourceKindAdapter
	svc        *datasource.Service
	now        time.Time
}

func TestServiceSuite(t *testing.T) { suite.Run(t, new(ServiceSuite)) }

func (s *ServiceSuite) SetupTest() {
	s.ctx = context.Background()
	s.store = &mocks.DataSourceStore{}
	s.recipes = &mocks.ObservabilityRecipeStore{}
	s.kind = &mocks.DataSourceKindAdapter{}
	s.querier = &mocks.MetricsQuerier{}
	s.discoverer = &mocks.Discoverer{}
	s.plain = &mocks.DataSourceKindAdapter{}

	s.kind.On("Kind").Return(domain.DataSourceKindPrometheus).Maybe()
	s.kind.On("Schema").Return(testSchema()).Maybe()
	s.kind.On("Capabilities").Return([]domain.Capability{domain.CapabilityMetrics}).Maybe()
	s.plain.On("Kind").Return(plainKind).Maybe()
	s.plain.On("Schema").Return(domain.DataSourceSchema{Label: "Errors", Fields: []domain.DataSourceField{
		{Key: "url", Type: domain.FieldURL, Required: true},
	}}).Maybe()
	s.plain.On("Capabilities").Return([]domain.Capability{domain.CapabilityErrors}).Maybe()

	policy := urlguard.HumanConfigured()
	svc, err := datasource.NewService(datasource.Deps{
		Store:     s.store,
		Recipes:   s.recipes,
		Kinds:     []port.DataSourceKindAdapter{queryingKind{s.kind, s.querier, s.discoverer}, s.plain},
		URLPolicy: &policy,
	})
	s.Require().NoError(err)
	s.now = time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	svc.SetClock(func() time.Time { return s.now })
	s.svc = svc
}

func (s *ServiceSuite) TearDownTest() {
	s.store.AssertExpectations(s.T())
	s.recipes.AssertExpectations(s.T())
	s.kind.AssertExpectations(s.T())
	s.querier.AssertExpectations(s.T())
	s.discoverer.AssertExpectations(s.T())
	s.plain.AssertExpectations(s.T())
}

func (s *ServiceSuite) expectRecordEchoes() {
	s.store.On("RecordVerification", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(recorded(nil))
}

func recorded(secretsSet []string) func(context.Context, uuid.UUID, domain.DataSourceVerification, time.Time) (domain.DataSource, error) {
	return func(_ context.Context, id uuid.UUID, v domain.DataSourceVerification, updatedAt time.Time) (domain.DataSource, error) {
		return domain.DataSource{
			ID: id, Kind: domain.DataSourceKindPrometheus, Status: v.Status, StatusDetail: v.StatusDetail,
			Info: v.Info, VerifiedAt: v.VerifiedAt, UpdatedAt: updatedAt, SecretsSet: secretsSet,
		}, nil
	}
}

func TestNewServiceRequiresAURLPolicy(t *testing.T) {
	_, err := datasource.NewService(datasource.Deps{Store: &mocks.DataSourceStore{}})
	if err == nil {
		t.Fatal("a service without a URL policy must not be built")
	}
}

func (s *ServiceSuite) stored(id uuid.UUID, ds domain.DataSource, secrets map[string]string) {
	ds.ID = id
	s.store.On("GetDataSource", mock.Anything, id).Return(ds, nil)
	s.store.On("DataSourceSecrets", mock.Anything, id).Return(secrets, nil)
}

func (s *ServiceSuite) TestKindsListTheSchemaWithNoNullsAndEverySecretMarked() {
	kinds := s.svc.Kinds()
	s.Require().Len(kinds, 2)
	s.Equal(domain.DataSourceKindPrometheus, kinds[0].Kind)
	s.Equal("Prometheus compatible", kinds[0].Label)
	s.Equal([]domain.Capability{domain.CapabilityMetrics}, kinds[0].Capabilities)
	for _, f := range kinds[0].Fields {
		s.NotNil(f.Options, f.Key)
		s.NotNil(f.ShowWhen, f.Key)
		s.Equal(f.Key == "token" || f.Key == "password" || f.Key == "headers", f.Secret, f.Key)
	}
}

func (s *ServiceSuite) TestCreateStoresTheSourceThenRecordsTheVerifyOutcome() {
	id := uuid.New()
	createdAt := s.now.Add(-time.Second)
	s.store.On("CreateDataSource", mock.Anything, domain.DataSource{
		Kind:   domain.DataSourceKindPrometheus,
		Name:   "localhost:9090",
		Config: map[string]string{"url": "http://localhost:9090", "auth": "bearer"},
		Status: domain.DataSourceUnverified,
	}, map[string]string{"token": "tok-123"}).
		Return(func(_ context.Context, ds domain.DataSource, secrets map[string]string) (domain.DataSource, error) {
			ds.ID = id
			ds.SecretsSet = []string{"token"}
			ds.UpdatedAt = createdAt
			return ds, nil
		})
	s.kind.On("Verify", mock.Anything, mock.MatchedBy(func(ds domain.DataSource) bool { return ds.ID == id }), map[string]string{"token": "tok-123"}).
		Return(domain.DataSourceInfo{Version: "2.53.0", Detail: "Prometheus API"}, nil)
	s.store.On("RecordVerification", mock.Anything, id, mock.Anything, createdAt).Return(recorded([]string{"token"}))

	got, err := s.svc.Create(s.ctx, domain.SaveDataSourceRequest{
		Kind:    domain.DataSourceKindPrometheus,
		Config:  map[string]string{"url": "http://localhost:9090/", "auth": "bearer"},
		Secrets: map[string]string{"token": " tok-123 "},
	})

	s.Require().NoError(err)
	s.Equal(id, got.ID)
	s.Equal(domain.DataSourceOK, got.Status)
	s.Equal("2.53.0", got.Info.Version)
	s.Require().NotNil(got.VerifiedAt)
	s.Equal(s.now, *got.VerifiedAt)
	s.Equal([]domain.Capability{domain.CapabilityMetrics}, got.Capabilities)
	s.Equal([]string{"token"}, got.SecretsSet)
}

func (s *ServiceSuite) TestCreateKeepsAnUnreachableSourceAndRedactsItsError() {
	s.store.On("CreateDataSource", mock.Anything, mock.Anything, mock.Anything).
		Return(func(_ context.Context, ds domain.DataSource, _ map[string]string) (domain.DataSource, error) {
			ds.ID = uuid.New()
			return ds, nil
		})
	s.kind.On("Verify", mock.Anything, mock.Anything, mock.Anything).
		Return(domain.DataSourceInfo{}, errors.New("build info: HTTP 500: token tok-123 rejected"))
	s.expectRecordEchoes()

	got, err := s.svc.Create(s.ctx, domain.SaveDataSourceRequest{
		Kind:    domain.DataSourceKindPrometheus,
		Name:    "prod",
		Config:  map[string]string{"url": "http://10.0.0.5:9090", "auth": "bearer"},
		Secrets: map[string]string{"token": "tok-123"},
	})

	s.Require().NoError(err)
	s.Equal(domain.DataSourceError, got.Status)
	s.Contains(got.StatusDetail, "HTTP 500")
	s.NotContains(got.StatusDetail, "tok-123")
	s.Nil(got.VerifiedAt)
}

func (s *ServiceSuite) TestCreateRefusesWhatTheSchemaDoesNotAllow() {
	cases := []struct {
		name string
		req  domain.SaveDataSourceRequest
		want datasource.FieldErrors
	}{
		{"unknown kind", domain.SaveDataSourceRequest{Kind: "datadog"}, datasource.FieldErrors{"kind": datasource.FieldUnknown}},
		{"no url", domain.SaveDataSourceRequest{Kind: "prometheus"}, datasource.FieldErrors{"url": datasource.FieldRequired}},
		{"not http", req(map[string]string{"url": "file:///etc/passwd"}, nil), datasource.FieldErrors{"url": datasource.FieldInvalid}},
		{"credentials in the url", req(map[string]string{"url": "http://u:p@localhost:9090"}, nil), datasource.FieldErrors{"url": datasource.FieldInvalid}},
		{"cloud metadata", req(map[string]string{"url": "http://169.254.169.254"}, nil), datasource.FieldErrors{"url": datasource.FieldNotPermitted}},
		{"gcp metadata name", req(map[string]string{"url": "http://metadata.google.internal"}, nil), datasource.FieldErrors{"url": datasource.FieldNotPermitted}},
		{"unknown auth", req(map[string]string{"url": "http://localhost:9090", "auth": "oauth"}, nil), datasource.FieldErrors{"auth": datasource.FieldInvalidOption}},
		{"bearer without a token", req(map[string]string{"url": "http://localhost:9090", "auth": "bearer"}, nil), datasource.FieldErrors{"token": datasource.FieldRequired}},
		{"basic without credentials", req(map[string]string{"url": "http://localhost:9090", "auth": "basic"}, nil), datasource.FieldErrors{"username": datasource.FieldRequired, "password": datasource.FieldRequired}},
		{"unknown field", req(map[string]string{"url": "http://localhost:9090", "region": "eu"}, nil), datasource.FieldErrors{"region": datasource.FieldUnknown}},
		{"malformed headers", req(map[string]string{"url": "http://localhost:9090"}, map[string]string{"headers": "no colon here"}), datasource.FieldErrors{"headers": datasource.FieldInvalid}},
		{"overriding Host", req(map[string]string{"url": "http://localhost:9090"}, map[string]string{"headers": "Host: evil"}), datasource.FieldErrors{"headers": datasource.FieldInvalid}},
	}
	for _, tc := range cases {
		s.Run(tc.name, func() {
			_, err := s.svc.Create(s.ctx, tc.req)
			var fe datasource.FieldErrors
			s.Require().ErrorAs(err, &fe)
			s.Equal(tc.want, fe)
			s.ErrorIs(err, datasource.ErrInvalidInput)
		})
	}
}

func req(config, secrets map[string]string) domain.SaveDataSourceRequest {
	return domain.SaveDataSourceRequest{Kind: domain.DataSourceKindPrometheus, Config: config, Secrets: secrets}
}

func (s *ServiceSuite) TestASecretSentAsConfigIsStoredAsASecret() {
	s.store.On("CreateDataSource", mock.Anything,
		mock.MatchedBy(func(ds domain.DataSource) bool {
			_, leaked := ds.Config["token"]
			return !leaked && ds.Config["auth"] == "bearer"
		}),
		map[string]string{"token": "tok"}).
		Return(domain.DataSource{ID: uuid.New(), Kind: domain.DataSourceKindPrometheus}, nil)
	s.kind.On("Verify", mock.Anything, mock.Anything, mock.Anything).Return(domain.DataSourceInfo{}, nil)
	s.expectRecordEchoes()

	_, err := s.svc.Create(s.ctx, req(map[string]string{"url": "http://localhost:9090", "auth": "bearer", "token": "tok"}, nil))
	s.Require().NoError(err)
}

func (s *ServiceSuite) TestTheURLPolicyDecidesWhatASourceMayPointAt() {
	s.svc.SetURLPolicy(urlguard.PublicOnly())

	_, err := s.svc.Create(s.ctx, req(map[string]string{"url": "http://127.0.0.1:9090"}, nil))

	var fe datasource.FieldErrors
	s.Require().ErrorAs(err, &fe)
	s.Equal(datasource.FieldErrors{"url": datasource.FieldNotPermitted}, fe)
}

func (s *ServiceSuite) TestUpdateMergesThePatchAndDropsFieldsItHides() {
	id := uuid.New()
	s.stored(id, domain.DataSource{
		Kind: domain.DataSourceKindPrometheus, Name: "prod",
		Config: map[string]string{"url": "http://prom:9090", "auth": "bearer"},
	}, map[string]string{"token": "old", "headers": "X-Scope-OrgID: t1"})
	savedAt := s.now.Add(-time.Second)
	s.store.On("UpdateDataSource", mock.Anything,
		mock.MatchedBy(func(ds domain.DataSource) bool { return ds.Status == domain.DataSourceUnverified }),
		map[string]string{"password": "pw"}).
		Return(func(_ context.Context, ds domain.DataSource, _ map[string]string) (domain.DataSource, error) {
			s.Equal(map[string]string{"url": "http://prom:9090", "auth": "basic", "username": "me"}, ds.Config)
			s.Equal("renamed", ds.Name)
			ds.UpdatedAt = savedAt
			return ds, nil
		}).Once()
	s.kind.On("Verify", mock.Anything, mock.Anything, map[string]string{"password": "pw"}).Return(domain.DataSourceInfo{}, nil)
	s.store.On("RecordVerification", mock.Anything, id, mock.Anything, savedAt).Return(recorded(nil)).Once()

	name := "renamed"
	got, err := s.svc.Update(s.ctx, id, domain.PatchDataSourceRequest{
		Name:    &name,
		Config:  map[string]string{"auth": "basic", "username": "me"},
		Secrets: map[string]string{"password": "pw", "headers": ""},
	})

	s.Require().NoError(err)
	s.Equal(domain.DataSourceOK, got.Status)
}

func (s *ServiceSuite) TestUpdateRefusesAnEmptyName() {
	id := uuid.New()
	s.stored(id, domain.DataSource{Kind: domain.DataSourceKindPrometheus, Name: "prod", Config: map[string]string{"url": "http://prom:9090"}}, map[string]string{})

	empty := " "
	_, err := s.svc.Update(s.ctx, id, domain.PatchDataSourceRequest{Name: &empty})

	var fe datasource.FieldErrors
	s.Require().ErrorAs(err, &fe)
	s.Equal(datasource.FieldErrors{"name": datasource.FieldRequired}, fe)
}

func (s *ServiceSuite) TestVerifyReportsAFailureThroughStatus() {
	id := uuid.New()
	earlier := s.now.Add(-time.Hour)
	s.stored(id, domain.DataSource{
		Kind: domain.DataSourceKindPrometheus, Config: map[string]string{"url": "http://prom:9090"},
		Status: domain.DataSourceOK, VerifiedAt: &earlier, Info: domain.DataSourceInfo{Version: "2.53.0"},
	}, map[string]string{})
	s.kind.On("Verify", mock.Anything, mock.Anything, mock.Anything).Return(domain.DataSourceInfo{}, errors.New("build info: timed out"))
	s.store.On("RecordVerification", mock.Anything, id, domain.DataSourceVerification{
		Status: domain.DataSourceError, StatusDetail: "build info: timed out",
	}, mock.Anything).Return(recorded(nil))

	got, err := s.svc.Verify(s.ctx, id)

	s.Require().NoError(err)
	s.Equal(domain.DataSourceError, got.Status)
	s.Equal("build info: timed out", got.StatusDetail)
	s.Equal(domain.DataSourceInfo{}, got.Info)
}

func (s *ServiceSuite) TestQueryNeedsAKindThatQueriesMetrics() {
	id := uuid.New()
	s.stored(id, domain.DataSource{Kind: plainKind, Config: map[string]string{"url": "http://x"}}, map[string]string{})

	_, err := s.svc.Query(s.ctx, id, domain.SeriesQuery{Query: "up", From: s.now.Add(-time.Hour), To: s.now, Step: time.Minute})

	s.ErrorIs(err, datasource.ErrUnsupported)
}

func (s *ServiceSuite) TestQueryValidatesTheRangeBeforeCallingTheSource() {
	from := s.now.Add(-time.Hour)
	cases := []struct {
		name string
		q    domain.SeriesQuery
		want datasource.FieldErrors
	}{
		{"no query", domain.SeriesQuery{From: from, To: s.now}, datasource.FieldErrors{"query": datasource.FieldRequired}},
		{"backwards", domain.SeriesQuery{Query: "up", From: s.now, To: from}, datasource.FieldErrors{"to": datasource.FieldInvalid}},
		{"too long a range", domain.SeriesQuery{Query: "up", From: s.now.Add(-100 * 24 * time.Hour), To: s.now}, datasource.FieldErrors{"from": datasource.FieldTooLong}},
		{"too many points", domain.SeriesQuery{Query: "up", From: s.now.Add(-24 * time.Hour), To: s.now, Step: time.Second}, datasource.FieldErrors{"step_seconds": datasource.FieldTooLong}},
		{"sub-second step", domain.SeriesQuery{Query: "up", From: from, To: s.now, Step: time.Millisecond}, datasource.FieldErrors{"step_seconds": datasource.FieldInvalid}},
	}
	for _, tc := range cases {
		s.Run(tc.name, func() {
			_, err := s.svc.Query(s.ctx, uuid.New(), tc.q)
			var fe datasource.FieldErrors
			s.Require().ErrorAs(err, &fe)
			s.Equal(tc.want, fe)
		})
	}
}

func (s *ServiceSuite) TestQueryPicksAStepCapsSeriesAndNeverAnswersNull() {
	id := uuid.New()
	s.stored(id, domain.DataSource{Kind: domain.DataSourceKindPrometheus, Config: map[string]string{"url": "http://prom:9090"}}, map[string]string{})
	many := make([]domain.Series, 501)
	s.querier.On("Series", mock.Anything, mock.Anything, mock.Anything,
		mock.MatchedBy(func(q domain.SeriesQuery) bool { return q.Step == 15*time.Second && q.Query == "up" })).
		Return(domain.SeriesResult{Series: many}, nil)

	res, err := s.svc.Query(s.ctx, id, domain.SeriesQuery{Query: " up ", From: s.now.Add(-time.Hour), To: s.now})

	s.Require().NoError(err)
	s.Len(res.Series, 500)
	s.Equal([]string{"showing the first 500 of 501 series"}, res.Warnings)
	s.NotNil(res.Series[0].Labels)
	s.NotNil(res.Series[0].Points)
}

func (s *ServiceSuite) TestQueryTellsARejectedQueryFromAFailingSource() {
	id := uuid.New()
	s.stored(id, domain.DataSource{Kind: domain.DataSourceKindPrometheus, Config: map[string]string{"url": "http://prom:9090"}}, map[string]string{"token": "tok-123"})
	s.querier.On("Series", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(domain.SeriesResult{}, fmt.Errorf("%w: parse error near tok-123", port.ErrDataSourceQuery)).Once()
	s.querier.On("Series", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(domain.SeriesResult{}, errors.New("query: HTTP 503")).Once()
	q := domain.SeriesQuery{Query: "sum(", From: s.now.Add(-time.Hour), To: s.now, Step: time.Minute}

	_, err := s.svc.Query(s.ctx, id, q)
	s.ErrorIs(err, port.ErrDataSourceQuery)
	s.NotErrorIs(err, datasource.ErrUpstream)
	s.NotContains(err.Error(), "tok-123")

	_, err = s.svc.Query(s.ctx, id, q)
	s.ErrorIs(err, datasource.ErrUpstream)
	s.Equal("query: HTTP 503", err.Error())
}

func (s *ServiceSuite) TestDiscoverFiltersTheListButDetectsOverAllOfIt() {
	id := uuid.New()
	s.stored(id, domain.DataSource{Kind: domain.DataSourceKindPrometheus, Config: map[string]string{"url": "http://prom:9090"}}, map[string]string{})
	s.discoverer.On("Discover", mock.Anything, mock.Anything, mock.Anything).Return(domain.Inventory{Metrics: []string{
		"go_goroutines", "kafka_consumergroup_lag", "rabbitmq_queue_messages_ready", "up",
	}}, nil)
	s.recipes.On("ListUserRecipes", mock.Anything).Return(nil, nil)

	got, err := s.svc.Discover(s.ctx, id, "RABBIT", 0)
	s.Require().NoError(err)
	s.Equal([]string{"rabbitmq_queue_messages_ready"}, got.Metrics)
	s.False(got.Truncated)
	s.Equal([]domain.RecipeMatch{
		{RecipeID: "kafka-exporter", MatchedMetric: "kafka_consumergroup_lag"},
		{RecipeID: "rabbitmq-prometheus", MatchedMetric: "rabbitmq_queue_messages_ready"},
	}, got.Recipes)

	got, err = s.svc.Discover(s.ctx, id, "", 2)
	s.Require().NoError(err)
	s.Equal([]string{"go_goroutines", "kafka_consumergroup_lag"}, got.Metrics)
	s.True(got.Truncated)
}

func (s *ServiceSuite) TestRecipesMergeUserRowsOverTheCatalog() {
	s.recipes.On("ListUserRecipes", mock.Anything).Return([]domain.Recipe{
		{ID: "cadvisor", SourceKind: domain.DataSourceKindPrometheus, Signals: map[string]string{"infra.cpu": "sum(rate(my_cpu[$__rate]))"}},
		{ID: "broken", SourceKind: domain.DataSourceKindPrometheus, Signals: map[string]string{"no.such": "x"}},
		{ID: "my-queue", SourceKind: domain.DataSourceKindPrometheus, Params: []string{"q"}, Signals: map[string]string{"queue.backlog": `depth{q="$q"}`}},
	}, nil)

	recipes, err := s.svc.Recipes(s.ctx)

	s.Require().NoError(err)
	byID := map[string]domain.Recipe{}
	for _, r := range recipes {
		byID[r.ID] = r
	}
	s.Equal(domain.RecipeOriginUser, byID["cadvisor"].Origin)
	s.Equal(map[string]string{"infra.cpu": "sum(rate(my_cpu[$__rate]))"}, byID["cadvisor"].Signals)
	s.Equal(domain.RecipeOriginUser, byID["my-queue"].Origin)
	s.Equal(domain.RecipeOriginCatalog, byID["otel-http"].Origin)
	s.NotContains(byID, "broken")
	s.NotNil(byID["cadvisor"].Params)
}

func (s *ServiceSuite) TestNotFoundPassesThrough() {
	id := uuid.New()
	s.store.On("GetDataSource", mock.Anything, id).Return(domain.DataSource{}, fmt.Errorf("get: %w", port.ErrNotFound))

	_, err := s.svc.Verify(s.ctx, id)

	s.ErrorIs(err, port.ErrNotFound)
}

func (s *ServiceSuite) TestAVerifyThatLostToANewerWriteAnswersWhatIsStoredNow() {
	id := uuid.New()
	snapshotAt := s.now.Add(-time.Minute)
	newer := domain.DataSource{ID: id, Kind: domain.DataSourceKindPrometheus, Name: "patched",
		Config: map[string]string{"url": "http://new:9090"}, Status: domain.DataSourceUnverified}
	s.store.On("GetDataSource", mock.Anything, id).Return(domain.DataSource{
		ID: id, Kind: domain.DataSourceKindPrometheus, Config: map[string]string{"url": "http://old:9090"}, UpdatedAt: snapshotAt,
	}, nil).Once()
	s.store.On("DataSourceSecrets", mock.Anything, id).Return(map[string]string{}, nil)
	s.kind.On("Verify", mock.Anything, mock.Anything, mock.Anything).Return(domain.DataSourceInfo{Version: "old"}, nil)
	s.store.On("RecordVerification", mock.Anything, id, mock.Anything, snapshotAt).
		Return(domain.DataSource{}, fmt.Errorf("record: %w", port.ErrStaleDataSource))
	s.store.On("GetDataSource", mock.Anything, id).Return(newer, nil).Once()

	got, err := s.svc.Verify(s.ctx, id)

	s.Require().NoError(err)
	s.Equal("patched", got.Name)
	s.Equal(domain.DataSourceUnverified, got.Status)
	s.Equal(map[string]string{"url": "http://new:9090"}, got.Config)
}

func (s *ServiceSuite) TestMovingTheURLToAnotherOriginNeedsTheStoredSecretsAgain() {
	stored := domain.DataSource{Kind: domain.DataSourceKindPrometheus, Name: "prod",
		Config: map[string]string{"url": "http://prom:9090", "auth": "bearer"}}
	secrets := map[string]string{"token": "tok", "headers": "X-Scope-OrgID: t1"}
	cases := []struct {
		name    string
		patch   domain.PatchDataSourceRequest
		refused bool
	}{
		{"another host", patch(map[string]string{"url": "http://evil.example:9090"}, nil), true},
		{"another port", patch(map[string]string{"url": "http://prom:9091"}, nil), true},
		{"https instead of http", patch(map[string]string{"url": "https://prom:9090"}, nil), true},
		{"only one secret re-sent", patch(map[string]string{"url": "http://evil.example:9090"}, map[string]string{"token": "tok"}), true},
		{"every kept secret re-sent or removed", patch(map[string]string{"url": "http://evil.example:9090"}, map[string]string{"token": "new", "headers": ""}), false},
		{"a secret the patch hides is not carried", patch(map[string]string{"url": "http://evil.example:9090", "auth": "none"}, map[string]string{"headers": ""}), false},
		{"another path on the same origin", patch(map[string]string{"url": "http://prom:9090/prometheus"}, nil), false},
		{"the same host spelled differently", patch(map[string]string{"url": "http://PROM.:9090"}, nil), false},
		{"no url change", patch(map[string]string{"auth": "bearer"}, nil), false},
	}
	for _, tc := range cases {
		s.Run(tc.name, func() {
			s.SetupTest()
			id := uuid.New()
			s.stored(id, stored, secrets)
			if !tc.refused {
				s.store.On("UpdateDataSource", mock.Anything, mock.Anything, mock.Anything).
					Return(func(_ context.Context, ds domain.DataSource, _ map[string]string) (domain.DataSource, error) {
						return ds, nil
					})
				s.kind.On("Verify", mock.Anything, mock.Anything, mock.Anything).Return(domain.DataSourceInfo{}, nil)
				s.expectRecordEchoes()
			}

			_, err := s.svc.Update(s.ctx, id, tc.patch)

			if tc.refused {
				var fe datasource.FieldErrors
				s.Require().ErrorAs(err, &fe)
				s.Equal(datasource.FieldErrors{"secrets": datasource.FieldSecretsRequired}, fe)
			} else {
				s.Require().NoError(err)
			}
			s.TearDownTest()
		})
	}
}

func patch(config, secrets map[string]string) domain.PatchDataSourceRequest {
	return domain.PatchDataSourceRequest{Config: config, Secrets: secrets}
}
