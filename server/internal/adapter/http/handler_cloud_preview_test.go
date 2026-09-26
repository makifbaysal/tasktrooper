package http

import (
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	cloudapp "github.com/makifbaysal/tasktrooper/server/internal/application/cloud"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

func getJSONBody(t *testing.T, app *fiber.App, path string) (int, map[string]any) {
	t.Helper()
	resp, err := app.Test(httptest.NewRequest("GET", path, nil))
	require.NoError(t, err)
	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	var body map[string]any
	require.NoError(t, json.Unmarshal(raw, &body), string(raw))
	return resp.StatusCode, body
}

func TestOverviewCarriesPerBranchAndErrorsSupported(t *testing.T) {
	fx := newCloudTestApp(t)
	acct, err := fx.accounts.CreateCloudAccount(context.Background(), domain.CloudAccount{Provider: domain.CloudVercel, Status: domain.CloudAccountOK}, map[string]string{"token": "t"})
	require.NoError(t, err)
	ref := domain.CloudResourceRef{Kind: domain.CloudResourceVercelProject, ID: "prj_1"}
	seed := func(env domain.DeployEnvironment) domain.ComponentEnvironment {
		saved, err := fx.envs.SaveEnvironment(context.Background(), domain.ComponentEnvironment{
			ComponentID: uuid.New(), Environment: env, Provider: domain.CloudVercel,
			AccountID: &acct.ID, Resource: &ref, Status: domain.LinkConfirmed,
		})
		require.NoError(t, err)
		return saved
	}

	status, body := getJSONBody(t, fx.app, "/v1/environments/"+seed(domain.EnvironmentPreview).ID.String()+"/overview")
	require.Equal(t, fiber.StatusOK, status)
	require.Equal(t, false, body["errors_supported"])
	require.Equal(t, true, body["environment"].(map[string]any)["per_branch"])

	status, body = getJSONBody(t, fx.app, "/v1/environments/"+seed(domain.EnvironmentProduction).ID.String()+"/overview")
	require.Equal(t, fiber.StatusOK, status)
	require.Equal(t, true, body["errors_supported"])
	require.Equal(t, false, body["environment"].(map[string]any)["per_branch"])
	require.NotContains(t, body, "preview_access")
}

type fakePreviewTasks struct{ task domain.BoardTask }

func (f fakePreviewTasks) GetTask(_ context.Context, _, taskID uuid.UUID) (domain.BoardTask, error) {
	if taskID != f.task.ID {
		return domain.BoardTask{}, domain.ErrBoardTaskNotFound
	}
	return f.task, nil
}

func TestListTaskPreviews(t *testing.T) {
	task := domain.BoardTask{ID: uuid.New(), Key: "T-1"}
	svc := cloudapp.NewService(cloudapp.Deps{
		Accounts:     newFakeCloudAccounts(),
		Environments: newFakeEnvironments(),
		Providers:    []port.CloudProvider{&fakeCloudProvider{kind: domain.CloudVercel}},
		Components:   &fakeComponents{components: map[uuid.UUID]domain.Component{}},
		Repos:        fakeCloudRepos{},
	})
	svc.SetTaskPreviewSources(fakePreviewTasks{task: task}, nil)
	app := fiber.New()
	(&Handler{cloudSvc: svc}).registerCloudRoutes(app)
	repoID := uuid.New()

	status, body := getJSONBody(t, app, "/v1/repositories/"+repoID.String()+"/tasks/"+task.ID.String()+"/previews")
	require.Equal(t, fiber.StatusOK, status)
	require.Equal(t, []any{}, body["previews"])

	status, _ = getJSONBody(t, app, "/v1/repositories/"+repoID.String()+"/tasks/"+uuid.New().String()+"/previews")
	require.Equal(t, fiber.StatusNotFound, status)

	status, _ = getJSONBody(t, app, "/v1/repositories/"+repoID.String()+"/tasks/not-a-uuid/previews")
	require.Equal(t, fiber.StatusBadRequest, status)
}
