package http

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	cloudapp "github.com/makifbaysal/tasktrooper/server/internal/application/cloud"
	"github.com/makifbaysal/tasktrooper/server/internal/application/datasource"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/makifbaysal/tasktrooper/server/internal/port/mocks"
)

func post(t *testing.T, app *fiber.App, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	rec.Code = resp.StatusCode
	_, err = rec.Body.ReadFrom(resp.Body)
	require.NoError(t, err)
	return rec
}

type recordedTasks struct {
	repo uuid.UUID
	req  domain.CreateBoardTaskRequest
}

func (r *recordedTasks) CreateTask(_ context.Context, repositoryID uuid.UUID, req domain.CreateBoardTaskRequest) (domain.BoardTask, error) {
	r.repo, r.req = repositoryID, req
	return domain.BoardTask{ID: uuid.New(), Title: req.Title, TaskType: req.TaskType}, nil
}

func TestCreateComponentErrorTaskOpensTheBugTaskForABindingsGroup(t *testing.T) {
	comps := &fakeComponents{components: map[uuid.UUID]domain.Component{}}
	comp := domain.Component{ID: uuid.New(), RepositoryID: uuid.New(), Status: domain.ComponentStatusActive}
	comps.components[comp.ID] = comp
	tasks := &recordedTasks{}
	svc := cloudapp.NewService(cloudapp.Deps{
		Accounts: newFakeCloudAccounts(), Environments: newFakeEnvironments(), Components: comps, Repos: fakeCloudRepos{}, Tasks: tasks,
	})
	store := mocks.NewDataSourceStore(t)
	sentry := domain.DataSource{ID: uuid.New(), Kind: domain.DataSourceKindSentry, Name: "Sentry prod"}
	store.On("GetDataSource", mock.Anything, sentry.ID).Return(sentry, nil).Maybe()
	store.On("GetDataSource", mock.Anything, mock.Anything).Return(domain.DataSource{}, port.ErrNotFound).Maybe()
	kind := mocks.NewDataSourceKindAdapter(t)
	kind.On("Kind").Return(domain.DataSourceKindSentry).Maybe()
	kind.On("Schema").Return(domain.DataSourceSchema{Label: "Sentry"}).Maybe()
	kind.On("Capabilities").Return([]domain.Capability{domain.CapabilityErrors}).Maybe()
	policy := urlguard.HumanConfigured()
	sources, err := datasource.NewService(datasource.Deps{Store: store, Kinds: []port.DataSourceKindAdapter{kind}, URLPolicy: &policy})
	require.NoError(t, err)
	h := &Handler{cloudSvc: svc, dataSourceSvc: sources}
	app := fiber.New()
	h.registerCloudRoutes(app)
	path := "/v1/components/" + comp.ID.String() + "/errors/task"

	rec := post(t, app, path, `{"data_source_id":"`+sentry.ID.String()+`","fingerprint":"101","message":"NSInvalidArgumentException","count":250,"external_url":"https://acme.sentry.io/issues/101/"}`)

	require.Equal(t, fiber.StatusCreated, rec.Code, rec.Body.String())
	var task domain.BoardTask
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &task))
	assert.Equal(t, "Fix: NSInvalidArgumentException", task.Title)
	assert.Equal(t, comp.RepositoryID, tasks.repo)
	require.NotNil(t, tasks.req.ComponentID)
	assert.Equal(t, comp.ID, *tasks.req.ComponentID)
	assert.Contains(t, tasks.req.Description, "Environment: production", "production when the body names none")
	assert.Contains(t, tasks.req.Description, "Data source: Sentry prod (sentry)")

	for body, want := range map[string]int{
		`{"message":"x"}`: fiber.StatusBadRequest,
		`{"data_source_id":"` + uuid.NewString() + `","message":"x"}`:                      fiber.StatusNotFound,
		`{"data_source_id":"` + sentry.ID.String() + `","environment":"qa","message":"x"}`: fiber.StatusBadRequest,
	} {
		assert.Equal(t, want, post(t, app, path, body).Code, body)
	}
	assert.Equal(t, fiber.StatusNotFound, post(t, app, "/v1/components/"+uuid.NewString()+"/errors/task", `{"data_source_id":"`+sentry.ID.String()+`","message":"x"}`).Code)
}
