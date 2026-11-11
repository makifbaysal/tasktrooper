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

	"github.com/makifbaysal/tasktrooper/server/internal/application/readiness"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type readinessRepos struct {
	repo    domain.Repository
	created []domain.CreateBoardTaskRequest
	tasks   map[uuid.UUID]domain.BoardTask
}

func (r *readinessRepos) Get(context.Context, uuid.UUID) (domain.Repository, error) {
	return r.repo, nil
}
func (r *readinessRepos) IndexStatus(context.Context, uuid.UUID) (domain.WorkspaceIndex, error) {
	return domain.WorkspaceIndex{Status: domain.IndexStatusCompleted}, nil
}
func (r *readinessRepos) Reindex(context.Context, uuid.UUID) error { return nil }
func (r *readinessRepos) ResolveRootPath(context.Context, uuid.UUID) (string, error) {
	return "/work", nil
}
func (r *readinessRepos) CreateTask(_ context.Context, _ uuid.UUID, req domain.CreateBoardTaskRequest) (domain.BoardTask, error) {
	r.created = append(r.created, req)
	t := domain.BoardTask{ID: uuid.New(), Key: "T-1", Title: req.Title, Column: req.Column}
	r.tasks[t.ID] = t
	return t, nil
}
func (r *readinessRepos) GetTask(_ context.Context, _, id uuid.UUID) (domain.BoardTask, error) {
	return r.tasks[id], nil
}
func (r *readinessRepos) WorkflowSetupTask(context.Context, uuid.UUID) (domain.BoardTask, bool, error) {
	return domain.BoardTask{}, false, nil
}
func (r *readinessRepos) CreateWorkflowSetupTaskAfter(context.Context, uuid.UUID, []domain.TaskRelationInput) (domain.BoardTask, bool, error) {
	return domain.BoardTask{}, false, nil
}

type readinessModel struct{ model domain.RepositoryModel }

func (m readinessModel) RepositoryModel(context.Context, uuid.UUID) (domain.RepositoryModel, error) {
	return m.model, nil
}
func (m readinessModel) StartScan(context.Context, uuid.UUID, domain.ScanTrigger) (domain.ProjectScan, bool, error) {
	return domain.ProjectScan{}, true, nil
}

type readinessStore struct {
	states map[string]domain.ReadinessStepState
}

func (s *readinessStore) List(context.Context, uuid.UUID) ([]domain.ReadinessStepState, error) {
	out := []domain.ReadinessStepState{}
	for _, st := range s.states {
		out = append(out, st)
	}
	return out, nil
}
func (s *readinessStore) SetSkipped(_ context.Context, _ uuid.UUID, step string, skipped bool) error {
	s.states[step] = domain.ReadinessStepState{StepID: step, Skipped: skipped}
	return nil
}
func (s *readinessStore) SetTask(_ context.Context, _ uuid.UUID, step string, id uuid.UUID) error {
	s.states[step] = domain.ReadinessStepState{StepID: step, TaskID: &id}
	return nil
}

func newReadinessTestApp(t *testing.T) (*fiber.App, uuid.UUID, *readinessRepos) {
	t.Helper()
	repoID := uuid.New()
	repos := &readinessRepos{repo: domain.Repository{ID: repoID, Name: "app", Kind: domain.RepoKindBackend}, tasks: map[uuid.UUID]domain.BoardTask{}}
	comp := domain.Component{
		ID: uuid.New(), Path: ".", Status: domain.ComponentStatusActive,
		Role: domain.Detected(domain.ComponentRoleBackend, domain.ConfidenceExact),
	}
	svc := readiness.New(readiness.Deps{
		Store: &readinessStore{states: map[string]domain.ReadinessStepState{}},
		Repos: repos,
		Model: readinessModel{model: domain.RepositoryModel{
			Components: []domain.Component{comp},
			LatestScan: &domain.ProjectScan{Status: domain.ScanSucceeded},
		}},
		ListRoot: func(string) ([]string, error) { return []string{"README.md", "CLAUDE.md"}, nil },
	})
	h := &Handler{readinessSvc: svc}
	app := fiber.New()
	h.registerReadinessRoutes(app)
	return app, repoID, repos
}

func readinessCall(t *testing.T, app *fiber.App, method, path, body string) (int, []byte) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	_, err = rec.Body.ReadFrom(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, rec.Body.Bytes()
}

func TestTheChecklistIsServedWithItsRunAllPlan(t *testing.T) {
	app, repoID, _ := newReadinessTestApp(t)
	code, body := readinessCall(t, app, "GET", "/v1/repositories/"+repoID.String()+"/readiness", "")
	require.Equal(t, fiber.StatusOK, code)
	var got domain.RepositoryReadiness
	require.NoError(t, json.Unmarshal(body, &got))
	require.Equal(t, repoID, got.RepositoryID)
	require.Equal(t, 1, got.RunAll.Tasks, "only the tests gap is a task here")
	require.Contains(t, string(body), `"id":"scan"`)
}

func TestAStepThatOpensATaskIsRefusedWithoutApproval(t *testing.T) {
	app, repoID, repos := newReadinessTestApp(t)
	path := "/v1/repositories/" + repoID.String() + "/readiness/steps/tests/run"

	code, _ := readinessCall(t, app, "POST", path, "")
	require.Equal(t, fiber.StatusBadRequest, code)
	code, _ = readinessCall(t, app, "POST", path, `{"approve":false}`)
	require.Equal(t, fiber.StatusBadRequest, code)
	require.Empty(t, repos.created)

	code, body := readinessCall(t, app, "POST", path, `{"approve":true}`)
	require.Equal(t, fiber.StatusAccepted, code)
	require.Contains(t, string(body), `"task"`)
	require.Len(t, repos.created, 1)
}

func TestRunAllNeedsTheCountItShowed(t *testing.T) {
	app, repoID, repos := newReadinessTestApp(t)
	path := "/v1/repositories/" + repoID.String() + "/readiness/run-all"

	code, _ := readinessCall(t, app, "POST", path, "")
	require.Equal(t, fiber.StatusBadRequest, code)
	code, _ = readinessCall(t, app, "POST", path, `{"approve_tasks":5}`)
	require.Equal(t, fiber.StatusConflict, code)
	require.Empty(t, repos.created)

	code, _ = readinessCall(t, app, "POST", path, `{"approve_tasks":1}`)
	require.Equal(t, fiber.StatusAccepted, code)
	require.Len(t, repos.created, 1)
}

func TestSkippingAndHidingComeBackAsTheFreshChecklist(t *testing.T) {
	app, repoID, _ := newReadinessTestApp(t)
	base := "/v1/repositories/" + repoID.String() + "/readiness"

	code, body := readinessCall(t, app, "PUT", base+"/steps/tests", `{"skipped":true}`)
	require.Equal(t, fiber.StatusOK, code)
	var got domain.RepositoryReadiness
	require.NoError(t, json.Unmarshal(body, &got))
	require.Equal(t, 1, got.Skipped)

	code, body = readinessCall(t, app, "PUT", base, `{"dismissed":true}`)
	require.Equal(t, fiber.StatusOK, code)
	require.NoError(t, json.Unmarshal(body, &got))
	require.True(t, got.Dismissed)

	code, _ = readinessCall(t, app, "PUT", base+"/steps/nonsense", `{"skipped":true}`)
	require.Equal(t, fiber.StatusNotFound, code)
}

func TestAStepTheAppDoesItselfCannotBeStartedHere(t *testing.T) {
	app, repoID, _ := newReadinessTestApp(t)
	code, _ := readinessCall(t, app, "POST", "/v1/repositories/"+repoID.String()+"/readiness/steps/delivery/run", `{"approve":true}`)
	require.Equal(t, fiber.StatusConflict, code)
}
