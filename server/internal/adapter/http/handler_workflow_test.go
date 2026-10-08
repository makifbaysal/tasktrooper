package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/workflow"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// fakeRoleStoreHTTP / fakeWorkflowStoreHTTP are minimal port.RoleStore /
// port.WorkflowStore fakes for exercising the HTTP layer's error-shape
// translation (422 validation problems, 409 conflicts) without a database.
type fakeRoleStoreHTTP struct {
	roles map[uuid.UUID]domain.AgentRole
}

func newFakeRoleStoreHTTP() *fakeRoleStoreHTTP {
	return &fakeRoleStoreHTTP{roles: map[uuid.UUID]domain.AgentRole{}}
}
func (f *fakeRoleStoreHTTP) List(context.Context) ([]domain.AgentRole, error) {
	out := make([]domain.AgentRole, 0, len(f.roles))
	for _, r := range f.roles {
		out = append(out, r)
	}
	return out, nil
}
func (f *fakeRoleStoreHTTP) Get(_ context.Context, id uuid.UUID) (domain.AgentRole, error) {
	return f.roles[id], nil
}
func (f *fakeRoleStoreHTTP) Create(_ context.Context, r domain.AgentRole) (domain.AgentRole, error) {
	r.ID = uuid.New()
	f.roles[r.ID] = r
	return r, nil
}
func (f *fakeRoleStoreHTTP) Update(_ context.Context, r domain.AgentRole) (domain.AgentRole, error) {
	f.roles[r.ID] = r
	return r, nil
}
func (f *fakeRoleStoreHTTP) Delete(_ context.Context, id uuid.UUID) error {
	delete(f.roles, id)
	return nil
}
func (f *fakeRoleStoreHTTP) SetAssignments(_ context.Context, roleID uuid.UUID, assignments []domain.RoleAssignment) error {
	r := f.roles[roleID]
	r.Assignments = assignments
	f.roles[roleID] = r
	return nil
}
func (f *fakeRoleStoreHTTP) ListAssignmentsByAgent(context.Context, uuid.UUID) ([]domain.AgentRole, error) {
	return nil, nil
}
func (f *fakeRoleStoreHTTP) SetAgentRoles(context.Context, uuid.UUID, []domain.AgentRoleMembership) error {
	return nil
}
func (f *fakeRoleStoreHTTP) ListPurposes(context.Context) ([]domain.RolePurpose, error) {
	return nil, nil
}
func (f *fakeRoleStoreHTTP) SetPurpose(context.Context, domain.RolePurposeKey, *uuid.UUID) error {
	return nil
}

type fakeWorkflowStoreHTTP struct {
	types  map[domain.TaskType]domain.TaskTypeDef
	stages map[domain.TaskType][]domain.WorkflowStage
}

func newFakeWorkflowStoreHTTP() *fakeWorkflowStoreHTTP {
	return &fakeWorkflowStoreHTTP{types: map[domain.TaskType]domain.TaskTypeDef{}, stages: map[domain.TaskType][]domain.WorkflowStage{}}
}
func (f *fakeWorkflowStoreHTTP) ListTaskTypes(context.Context) ([]domain.TaskTypeDef, error) {
	out := make([]domain.TaskTypeDef, 0, len(f.types))
	for _, t := range f.types {
		out = append(out, t)
	}
	return out, nil
}
func (f *fakeWorkflowStoreHTTP) GetTaskType(_ context.Context, key domain.TaskType) (domain.TaskTypeDef, error) {
	t, ok := f.types[key]
	if !ok {
		return domain.TaskTypeDef{}, errNotFoundHTTP
	}
	return t, nil
}
func (f *fakeWorkflowStoreHTTP) CreateTaskType(_ context.Context, def domain.TaskTypeDef, _ domain.TaskType) (domain.TaskTypeDef, error) {
	f.types[def.Key] = def
	return def, nil
}
func (f *fakeWorkflowStoreHTTP) UpdateTaskType(_ context.Context, def domain.TaskTypeDef) (domain.TaskTypeDef, error) {
	f.types[def.Key] = def
	return def, nil
}
func (f *fakeWorkflowStoreHTTP) DeleteTaskType(_ context.Context, key domain.TaskType) error {
	delete(f.types, key)
	return nil
}
func (f *fakeWorkflowStoreHTTP) ListStages(_ context.Context, taskType domain.TaskType) ([]domain.WorkflowStage, error) {
	return f.stages[taskType], nil
}
func (f *fakeWorkflowStoreHTTP) ReplaceStages(_ context.Context, taskType domain.TaskType, stages []domain.WorkflowStage) error {
	f.stages[taskType] = stages
	return nil
}
func (f *fakeWorkflowStoreHTTP) LoadAll(ctx context.Context) ([]domain.Workflow, error) {
	types, _ := f.ListTaskTypes(ctx)
	out := make([]domain.Workflow, 0, len(types))
	for _, t := range types {
		out = append(out, domain.Workflow{Type: t, Stages: f.stages[t.Key]})
	}
	return out, nil
}
func (f *fakeWorkflowStoreHTTP) ColumnHasBehaviourStages(context.Context, string) (bool, error) {
	return false, nil
}

type notFoundErrHTTP struct{}

func (notFoundErrHTTP) Error() string { return "not found" }

var errNotFoundHTTP error = notFoundErrHTTP{}

func newWorkflowTestApp(t *testing.T, roles *fakeRoleStoreHTTP, wfStore *fakeWorkflowStoreHTTP) (*fiber.App, *workflow.Service) {
	t.Helper()
	svc := workflow.NewService(roles, wfStore)
	require.NoError(t, svc.Reload(context.Background()))
	h := &Handler{workflowSvc: svc}
	app := fiber.New()
	h.registerWorkflowRoutes(app)
	return app, svc
}

func doJSON(t *testing.T, app *fiber.App, method, path string, body any) (int, map[string]any) {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

// TestPutWorkflowStages_UnknownBehaviour422 covers PUT
// /v1/task-types/:key/workflow refusing an invalid stage with 422 and a
// problems array, per the frozen API contract.
func TestPutWorkflowStages_UnknownBehaviour422(t *testing.T) {
	roles := newFakeRoleStoreHTTP()
	wfStore := newFakeWorkflowStoreHTTP()
	wfStore.types["task"] = domain.TaskTypeDef{Key: "task", IsDefault: true}
	app, _ := newWorkflowTestApp(t, roles, wfStore)

	status, out := doJSON(t, app, "PUT", "/v1/task-types/task/workflow", map[string]any{
		"stages": []map[string]any{
			{"column_slug": "todo", "kind": "queue", "behaviours": []map[string]any{{"key": "not_a_real_behaviour"}}},
		},
	})

	require.Equal(t, fiber.StatusUnprocessableEntity, status)
	problems, ok := out["problems"].([]any)
	require.True(t, ok, "expected a problems array, got %v", out)
	require.NotEmpty(t, problems)
}

// TestDeleteDefaultTaskType409 covers DELETE /v1/task-types/:key refusing
// the default type.
func TestDeleteDefaultTaskType409(t *testing.T) {
	roles := newFakeRoleStoreHTTP()
	wfStore := newFakeWorkflowStoreHTTP()
	wfStore.types["task"] = domain.TaskTypeDef{Key: "task", IsDefault: true}
	app, _ := newWorkflowTestApp(t, roles, wfStore)

	status, _ := doJSON(t, app, "DELETE", "/v1/task-types/task", nil)
	require.Equal(t, fiber.StatusConflict, status)
}

// TestUpdateTaskTypePrefixChangeWithTasks409 covers PUT /v1/task-types/:key
// refusing a key_prefix change once the type has tasks.
func TestUpdateTaskTypePrefixChangeWithTasks409(t *testing.T) {
	roles := newFakeRoleStoreHTTP()
	wfStore := newFakeWorkflowStoreHTTP()
	wfStore.types["bug"] = domain.TaskTypeDef{Key: "bug", KeyPrefix: "B", IsDefect: true, TaskCount: 3}
	app, _ := newWorkflowTestApp(t, roles, wfStore)

	status, out := doJSON(t, app, "PUT", "/v1/task-types/bug", map[string]any{
		"label": "Bug", "key_prefix": "BG",
	})
	require.Equal(t, fiber.StatusConflict, status, "%v", out)
}

// TestSetRoleAssignments_MissingTools422 covers PUT
// /v1/roles/:id/assignments refusing an assignment to an agent missing the
// role's required tools, without confirm_grant_tools.
func TestSetRoleAssignments_MissingTools422(t *testing.T) {
	roles := newFakeRoleStoreHTTP()
	roleID := uuid.New()
	roles.roles[roleID] = domain.AgentRole{ID: roleID, Key: "analyst", RequiredTools: []string{"create_board_task"}}
	wfStore := newFakeWorkflowStoreHTTP()
	wfStore.types["task"] = domain.TaskTypeDef{Key: "task", IsDefault: true}

	svc := workflow.NewService(roles, wfStore)
	require.NoError(t, svc.Reload(context.Background()))
	agentID := uuid.New()
	catalog := &fakeAgentCatalogHTTP{agents: map[string]domain.Agent{
		"some-agent": {ID: agentID, Name: "some-agent", ToolPolicy: domain.ToolPolicy{AllowTools: []string{"read_file"}}},
	}}
	svc.SetAgentCatalog(catalog)
	h := &Handler{workflowSvc: svc}
	app := fiber.New()
	h.registerWorkflowRoutes(app)

	status, out := doJSON(t, app, "PUT", "/v1/roles/"+roleID.String()+"/assignments", map[string]any{
		"assignments": []map[string]any{{"agent_id": agentID.String(), "areas": nil, "priority": 0}},
	})

	require.Equal(t, fiber.StatusUnprocessableEntity, status, "%v", out)
	saved, _ := out["saved"].(bool)
	require.False(t, saved)
	require.NotNil(t, out["missing_tools"])
}

func TestListWorkflowBehavioursOffersEveryRoleArea(t *testing.T) {
	app, _ := newWorkflowTestApp(t, newFakeRoleStoreHTTP(), newFakeWorkflowStoreHTTP())

	status, out := doJSON(t, app, "GET", "/v1/workflow/behaviours", nil)

	require.Equal(t, fiber.StatusOK, status, "%v", out)
	require.Equal(t, []any{"backend", "frontend", "mobile", "data", "game"}, out["areas"])
}
