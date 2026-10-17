package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	goccyjson "github.com/goccy/go-json"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/datasource/cloudwatch"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/datasource/gcpmonitoring"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/datasource/prometheus"
	"github.com/makifbaysal/tasktrooper/server/internal/application/datasource"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/makifbaysal/tasktrooper/server/internal/port/mocks"
)

type DataSourceHandlerSuite struct {
	suite.Suite
	store   *mocks.DataSourceStore
	recipes *mocks.ObservabilityRecipeStore
	plain   *mocks.DataSourceKindAdapter
	prom    *httptest.Server
	svc     *datasource.Service
	app     *fiber.App
}

func TestDataSourceHandlerSuite(t *testing.T) { suite.Run(t, new(DataSourceHandlerSuite)) }

func (s *DataSourceHandlerSuite) SetupTest() {
	s.store = &mocks.DataSourceStore{}
	s.recipes = &mocks.ObservabilityRecipeStore{}
	s.plain = &mocks.DataSourceKindAdapter{}
	s.plain.On("Kind").Return(domain.DataSourceKind("errors_only")).Maybe()
	s.plain.On("Schema").Return(domain.DataSourceSchema{Label: "Errors"}).Maybe()
	s.plain.On("Capabilities").Return([]domain.Capability{domain.CapabilityErrors}).Maybe()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/status/buildinfo", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{"status":"success","data":{"version":"2.53.0"}}`)
	})
	mux.HandleFunc("/api/v1/query_range", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{"__name__":"up","job":"api"},"values":[[1700000000,"1"],[1700000060,"NaN"]]}]}}`)
	})
	mux.HandleFunc("/api/v1/label/__name__/values", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{"status":"success","data":["up","kminion_kafka_consumer_group_topic_lag"]}`)
	})
	s.prom = httptest.NewServer(mux)

	policy := urlguard.HumanConfigured()
	svc, err := datasource.NewService(datasource.Deps{
		Store:     s.store,
		Recipes:   s.recipes,
		Kinds:     []port.DataSourceKindAdapter{prometheus.New(), s.plain},
		URLPolicy: &policy,
	})
	s.Require().NoError(err)
	s.svc = svc
	s.app = fiber.New(fiber.Config{JSONEncoder: goccyjson.Marshal, JSONDecoder: goccyjson.Unmarshal})
	(&Handler{dataSourceSvc: svc}).registerDataSourceRoutes(s.app)
}

func (s *DataSourceHandlerSuite) TearDownTest() {
	s.prom.Close()
	s.store.AssertExpectations(s.T())
	s.recipes.AssertExpectations(s.T())
	s.plain.AssertExpectations(s.T())
}

func (s *DataSourceHandlerSuite) do(method, path, body string) (int, map[string]any) {
	var r io.Reader
	if body != "" {
		r = bytes.NewBufferString(body)
	}
	req := httptest.NewRequest(method, path, r)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.app.Test(req, 10000)
	s.Require().NoError(err)
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	if len(raw) > 0 {
		s.Require().NoError(json.Unmarshal(raw, &out), string(raw))
	}
	return resp.StatusCode, out
}

func (s *DataSourceHandlerSuite) storedSource(id uuid.UUID, kind domain.DataSourceKind, url string) {
	ds := domain.DataSource{ID: id, Kind: kind, Name: "local", Config: map[string]string{"url": url}, Status: domain.DataSourceUnverified}
	s.store.On("GetDataSource", mock.Anything, id).Return(ds, nil)
	s.store.On("DataSourceSecrets", mock.Anything, id).Return(map[string]string{}, nil)
}

func (s *DataSourceHandlerSuite) echoVerifications(base domain.DataSource) {
	s.store.On("RecordVerification", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(func(_ context.Context, _ uuid.UUID, v domain.DataSourceVerification, _ time.Time) (domain.DataSource, error) {
			base.Status, base.StatusDetail, base.Info, base.VerifiedAt = v.Status, v.StatusDetail, v.Info, v.VerifiedAt
			return base, nil
		})
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func (s *DataSourceHandlerSuite) TestKindsCarryTheFormSchema() {
	status, out := s.do("GET", "/v1/data-source-kinds", "")
	s.Equal(200, status)
	kinds := out["kinds"].([]any)
	s.Require().Len(kinds, 2)
	prom := kinds[0].(map[string]any)
	s.Equal("prometheus", prom["kind"])
	s.Equal("Prometheus compatible", prom["label"])
	s.NotEmpty(prom["description"])
	s.Equal([]any{"metrics"}, prom["capabilities"])
	url := prom["fields"].([]any)[0].(map[string]any)
	s.ElementsMatch([]string{"key", "label", "type", "required", "placeholder", "help", "options", "show_when", "secret"}, keysOf(url))
	s.Equal("url", url["key"])
	s.Equal(true, url["required"])
	s.Equal("http://localhost:9090", url["placeholder"])
	s.Equal([]any{}, url["options"])
	s.Equal(map[string]any{}, url["show_when"])
}

func (s *DataSourceHandlerSuite) TestCreateAnswersTheVerifiedSource() {
	id := uuid.New()
	created := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	row := domain.DataSource{ID: id, Kind: domain.DataSourceKindPrometheus, Name: "local",
		Config: map[string]string{"url": s.prom.URL}, SecretsSet: []string{"headers"}, CreatedAt: created, UpdatedAt: created}
	s.store.On("CreateDataSource", mock.Anything, mock.Anything, map[string]string{"headers": "X-Scope-OrgID: t1"}).Return(row, nil)
	s.echoVerifications(row)

	status, out := s.do("POST", "/v1/data-sources",
		fmt.Sprintf(`{"kind":"prometheus","name":"local","config":{"url":%q},"secrets":{"headers":"X-Scope-OrgID: t1"}}`, s.prom.URL))

	s.Equal(201, status, out)
	s.ElementsMatch([]string{"id", "kind", "name", "config", "secrets_set", "status", "status_detail", "info",
		"verified_at", "created_at", "updated_at", "capabilities"}, keysOf(out))
	s.Equal(id.String(), out["id"])
	s.Equal("ok", out["status"])
	s.Equal(map[string]any{"version": "2.53.0", "detail": "Prometheus API"}, out["info"])
	s.Equal([]any{"headers"}, out["secrets_set"])
	s.Equal(map[string]any{"url": s.prom.URL}, out["config"])
	s.Equal([]any{"metrics"}, out["capabilities"])
	s.NotNil(out["verified_at"])
}

func (s *DataSourceHandlerSuite) TestCreateNamesTheFieldsTheSchemaRefuses() {
	status, out := s.do("POST", "/v1/data-sources", `{"kind":"prometheus","config":{"url":"http://169.254.169.254","auth":"bearer"}}`)

	s.Equal(400, status)
	s.NotEmpty(out["error"])
	s.Equal(map[string]any{"url": "not_permitted", "token": "required"}, out["fields"])
}

func (s *DataSourceHandlerSuite) TestQueryAnswersSeriesWithNullForNaN() {
	id := uuid.New()
	s.storedSource(id, domain.DataSourceKindPrometheus, s.prom.URL)

	status, out := s.do("POST", "/v1/data-sources/"+id.String()+"/query",
		`{"query":"up","from":"2023-11-14T22:00:00Z","to":"2023-11-14T23:00:00Z","step_seconds":60}`)

	s.Equal(200, status, out)
	s.Equal([]any{}, out["warnings"])
	series := out["series"].([]any)
	s.Require().Len(series, 1)
	s.Equal(map[string]any{
		"name":   "up",
		"labels": map[string]any{"job": "api"},
		"points": []any{[]any{float64(1700000000000), float64(1)}, []any{float64(1700000060000), nil}},
	}, series[0])
}

func (s *DataSourceHandlerSuite) TestQueryOnAKindWithoutMetricsIs422() {
	id := uuid.New()
	s.storedSource(id, "errors_only", "http://localhost:1")

	status, out := s.do("POST", "/v1/data-sources/"+id.String()+"/query",
		`{"query":"up","from":"2023-11-14T22:00:00Z","to":"2023-11-14T23:00:00Z","step_seconds":60}`)

	s.Equal(422, status)
	s.Equal("unsupported", out["code"])
}

func (s *DataSourceHandlerSuite) TestQueryRefusesAnUnparseableTime() {
	status, out := s.do("POST", "/v1/data-sources/"+uuid.NewString()+"/query", `{"query":"up","from":"yesterday","to":"2023-11-14T23:00:00Z"}`)

	s.Equal(400, status)
	s.Equal(map[string]any{"from": "invalid"}, out["fields"])
}

func (s *DataSourceHandlerSuite) TestDiscoverListsMetricsAndDetectedRecipes() {
	id := uuid.New()
	s.storedSource(id, domain.DataSourceKindPrometheus, s.prom.URL)
	s.recipes.On("ListUserRecipes", mock.Anything).Return(nil, nil)

	status, out := s.do("GET", "/v1/data-sources/"+id.String()+"/discover?limit=10", "")

	s.Equal(200, status, out)
	s.Equal(map[string]any{
		"metrics":   []any{"kminion_kafka_consumer_group_topic_lag", "up"},
		"truncated": false,
		"recipes":   []any{map[string]any{"recipe_id": "kminion", "matched_metric": "kminion_kafka_consumer_group_topic_lag"}},
	}, out)
}

func (s *DataSourceHandlerSuite) TestAPublicOnlyPolicyReachesTheAdapter() {
	s.svc.SetURLPolicy(urlguard.PublicOnly())
	id := uuid.New()
	s.storedSource(id, domain.DataSourceKindPrometheus, s.prom.URL)
	s.echoVerifications(domain.DataSource{ID: id, Kind: domain.DataSourceKindPrometheus})

	status, out := s.do("POST", "/v1/data-sources/"+id.String()+"/verify", "")

	s.Equal(200, status)
	s.Equal("error", out["status"])
	s.Contains(out["status_detail"], "not an allowed destination")
}

func (s *DataSourceHandlerSuite) TestUnknownSourcesAre404() {
	id := uuid.New()
	s.store.On("GetDataSource", mock.Anything, id).Return(domain.DataSource{}, fmt.Errorf("get data source: %w", port.ErrNotFound))
	s.store.On("DeleteDataSource", mock.Anything, id).Return(fmt.Errorf("delete data source: %w", port.ErrNotFound))

	status, _ := s.do("POST", "/v1/data-sources/"+id.String()+"/verify", "")
	s.Equal(404, status)
	status, _ = s.do("DELETE", "/v1/data-sources/"+id.String(), "")
	s.Equal(404, status)
}

func (s *DataSourceHandlerSuite) TestDeleteIs204() {
	id := uuid.New()
	s.store.On("DeleteDataSource", mock.Anything, id).Return(nil)

	status, _ := s.do("DELETE", "/v1/data-sources/"+id.String(), "")

	s.Equal(204, status)
}

func (s *DataSourceHandlerSuite) TestListNeverAnswersNull() {
	s.store.On("ListDataSources", mock.Anything).Return([]domain.DataSource{}, nil)

	status, out := s.do("GET", "/v1/data-sources", "")

	s.Equal(200, status)
	s.Equal([]any{}, out["data_sources"])
}

func (s *DataSourceHandlerSuite) TestSignalsAndRecipesListTheCatalog() {
	status, out := s.do("GET", "/v1/signals", "")
	s.Equal(200, status)
	first := out["signals"].([]any)[0].(map[string]any)
	s.ElementsMatch([]string{"id", "kind", "unit", "roles", "aggregation", "better"}, keysOf(first))

	s.recipes.On("ListUserRecipes", mock.Anything).Return(nil, nil)
	status, out = s.do("GET", "/v1/recipes", "")
	s.Equal(200, status)
	var otel map[string]any
	for _, r := range out["recipes"].([]any) {
		if r.(map[string]any)["id"] == "otel-http" {
			otel = r.(map[string]any)
		}
	}
	s.Require().NotNil(otel)
	s.ElementsMatch([]string{"id", "source_kind", "applies_to", "detect", "params", "signals", "tables", "origin"}, keysOf(otel))
	endpoints := otel["tables"].(map[string]any)["endpoints"].(map[string]any)
	s.ElementsMatch([]string{"columns", "key_labels", "queries"}, keysOf(endpoints))
	s.Equal([]any{"http_route"}, endpoints["key_labels"])
	s.Equal(map[string]any{
		"technology": "opentelemetry", "messaging_system": "", "resource_kind": "", "resource_params": map[string]any{},
		"store_platform": "", "store_params": map[string]any{},
	}, otel["applies_to"])
	s.Equal(map[string]any{"metric": "http_server_request_duration_seconds_count"}, otel["detect"])
	s.Equal([]any{"job"}, otel["params"])
	s.Equal("catalog", otel["origin"])
}

func (s *DataSourceHandlerSuite) TestMovingTheURLWithoutTheSecretsIs400() {
	id := uuid.New()
	s.store.On("GetDataSource", mock.Anything, id).Return(domain.DataSource{ID: id, Kind: domain.DataSourceKindPrometheus, Name: "prod",
		Config: map[string]string{"url": "http://prom:9090", "auth": "bearer"}}, nil)
	s.store.On("DataSourceSecrets", mock.Anything, id).Return(map[string]string{"token": "tok"}, nil)

	status, out := s.do("PATCH", "/v1/data-sources/"+id.String(), `{"config":{"url":"http://elsewhere:9090"}}`)

	s.Equal(400, status)
	s.NotEmpty(out["error"])
	s.Equal(map[string]any{"secrets": "secrets_required"}, out["fields"])
}

func (s *DataSourceHandlerSuite) TestCloudKindsListAnAccountFieldOfTheirProvider() {
	policy := urlguard.HumanConfigured()
	svc, err := datasource.NewService(datasource.Deps{
		Store:         s.store,
		CloudAccounts: &mocks.CloudAccountReader{},
		Kinds:         []port.DataSourceKindAdapter{gcpmonitoring.New(), cloudwatch.New()},
		URLPolicy:     &policy,
	})
	s.Require().NoError(err)
	s.app = fiber.New(fiber.Config{JSONEncoder: goccyjson.Marshal, JSONDecoder: goccyjson.Unmarshal})
	(&Handler{dataSourceSvc: svc}).registerDataSourceRoutes(s.app)

	status, out := s.do("GET", "/v1/data-source-kinds", "")

	s.Equal(200, status)
	kinds := out["kinds"].([]any)
	s.Require().Len(kinds, 2)
	for i, want := range []struct{ kind, provider, second string }{
		{"gcp_monitoring", "gcp", "project_id"},
		{"aws_cloudwatch", "aws", "region"},
	} {
		k := kinds[i].(map[string]any)
		s.Equal(want.kind, k["kind"])
		s.Equal([]any{"metrics"}, k["capabilities"])
		fields := k["fields"].([]any)
		s.Require().Len(fields, 2)
		account := fields[0].(map[string]any)
		s.Equal("cloud_account_id", account["key"])
		s.Equal("cloud_account", account["type"])
		s.Equal(want.provider, account["provider"])
		s.Equal(true, account["required"])
		s.Equal(false, account["secret"])
		second := fields[1].(map[string]any)
		s.Equal(want.second, second["key"])
		s.NotContains(second, "provider")
	}
}
