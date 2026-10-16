package http

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/projectmodel"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type fakeHTTPFindings struct {
	rows map[uuid.UUID]domain.ComponentFinding
}

func (f *fakeHTTPFindings) ListFindings(_ context.Context, repositoryID uuid.UUID) ([]domain.ComponentFinding, error) {
	var out []domain.ComponentFinding
	for _, r := range f.rows {
		if r.RepositoryID == repositoryID {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeHTTPFindings) GetFinding(_ context.Context, id uuid.UUID) (domain.ComponentFinding, error) {
	r, ok := f.rows[id]
	if !ok {
		return domain.ComponentFinding{}, port.ErrNotFound
	}
	return r, nil
}

func (f *fakeHTTPFindings) SaveFinding(_ context.Context, r domain.ComponentFinding) (domain.ComponentFinding, error) {
	f.rows[r.ID] = r
	return r, nil
}

func (f *fakeHTTPFindings) ApplyFindings(context.Context, uuid.UUID, []domain.ComponentFinding, []uuid.UUID) error {
	return nil
}

func (f *fakeHTTPFindings) LinkFindingsToTask(_ context.Context, ids []uuid.UUID, taskID uuid.UUID) error {
	for _, id := range ids {
		r := f.rows[id]
		r.TaskID = &taskID
		f.rows[id] = r
	}
	return nil
}

type fakeHTTPTasks struct{}

func (fakeHTTPTasks) CreateTask(_ context.Context, repositoryID uuid.UUID, req domain.CreateBoardTaskRequest) (domain.BoardTask, error) {
	return domain.BoardTask{ID: uuid.New(), RepositoryID: repositoryID, Title: req.Title, TaskType: req.TaskType, Priority: req.Priority}, nil
}

func (fakeHTTPTasks) GetTask(context.Context, uuid.UUID, uuid.UUID) (domain.BoardTask, error) {
	return domain.BoardTask{}, domain.ErrBoardTaskNotFound
}

func newFindingsTestApp(t *testing.T) (*fiber.App, domain.Component, domain.ComponentFinding) {
	t.Helper()
	comp := domain.Component{ID: uuid.New(), RepositoryID: uuid.New(), Path: "api", Status: domain.ComponentStatusActive}
	finding := domain.ComponentFinding{
		ID: uuid.New(), RepositoryID: comp.RepositoryID, ComponentID: &comp.ID, Rule: "empty_catch_block",
		Category: domain.FindingErrorHandling, Severity: domain.FindingSeverityMedium, Status: domain.FindingOpen, Occurrences: 2,
	}
	repos := &fakePMRepos{repo: domain.Repository{ID: comp.RepositoryID, Name: "tasktrooper"}}
	svc := projectmodel.NewService(projectmodel.Deps{Store: newFakePMStore(comp), Repos: repos, Projector: repos, Pipelines: &fakePMPipelines{}})
	svc.SetFindingStore(&fakeHTTPFindings{rows: map[uuid.UUID]domain.ComponentFinding{finding.ID: finding}})
	svc.SetTaskCreator(fakeHTTPTasks{})
	h := &Handler{projectModelSvc: svc}
	app := fiber.New()
	h.registerProjectModelRoutes(app)
	return app, comp, finding
}

func send(t *testing.T, app *fiber.App, method, path, body string) (int, []byte) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	var raw json.RawMessage
	_ = json.NewDecoder(resp.Body).Decode(&raw)
	return resp.StatusCode, raw
}

func TestUpdateFindingRoute(t *testing.T) {
	app, _, finding := newFindingsTestApp(t)

	status, body := send(t, app, "PATCH", "/v1/findings/"+finding.ID.String(), `{"status":"false_positive","reason":"intended"}`)
	require.Equal(t, fiber.StatusOK, status)
	var got domain.ComponentFinding
	require.NoError(t, json.Unmarshal(body, &got))
	require.Equal(t, domain.FindingFalsePositive, got.Status)
	require.Equal(t, "intended", got.StatusReason)

	status, _ = send(t, app, "PATCH", "/v1/findings/"+finding.ID.String(), `{"status":"resolved"}`)
	require.Equal(t, fiber.StatusBadRequest, status)
	status, _ = send(t, app, "PATCH", "/v1/findings/"+uuid.NewString(), `{"status":"accepted"}`)
	require.Equal(t, fiber.StatusNotFound, status)
	status, _ = send(t, app, "PATCH", "/v1/findings/nope", `{}`)
	require.Equal(t, fiber.StatusBadRequest, status)
}

func TestCreateFindingTaskRoute(t *testing.T) {
	app, comp, finding := newFindingsTestApp(t)
	path := "/v1/repositories/" + comp.RepositoryID.String() + "/findings/task"

	status, _ := send(t, app, "POST", path, `{"finding_ids":["`+finding.ID.String()+`"],"title":" "}`)
	require.Equal(t, fiber.StatusBadRequest, status)

	status, body := send(t, app, "POST", path, `{"finding_ids":["`+finding.ID.String()+`"],"title":"Handle swallowed errors"}`)
	require.Equal(t, fiber.StatusCreated, status)
	var got domain.FindingTaskResult
	require.NoError(t, json.Unmarshal(body, &got))
	require.Equal(t, "Handle swallowed errors", got.Task.Title)
	require.Equal(t, domain.TaskTypeTechnical, got.Task.TaskType)
	require.Equal(t, domain.TaskPriorityMedium, got.Task.Priority)
	require.Len(t, got.Findings, 1)
	require.Equal(t, got.Task.ID, *got.Findings[0].TaskID)
}
