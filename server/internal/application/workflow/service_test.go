package workflow_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/workflow"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// memRoleStore/memWorkflowStore are minimal in-memory port.RoleStore/
// port.WorkflowStore fakes — just enough for workflow.Service's Reload and
// the resolver methods built on top of it.
type memRoleStore struct {
	roles    map[uuid.UUID]domain.AgentRole
	purposes map[domain.RolePurposeKey]*uuid.UUID
}

func newMemRoleStore() *memRoleStore {
	return &memRoleStore{roles: map[uuid.UUID]domain.AgentRole{}, purposes: map[domain.RolePurposeKey]*uuid.UUID{}}
}

func (m *memRoleStore) List(context.Context) ([]domain.AgentRole, error) {
	out := make([]domain.AgentRole, 0, len(m.roles))
	for _, r := range m.roles {
		out = append(out, r)
	}
	return out, nil
}
func (m *memRoleStore) Get(_ context.Context, id uuid.UUID) (domain.AgentRole, error) {
	return m.roles[id], nil
}
func (m *memRoleStore) Create(_ context.Context, r domain.AgentRole) (domain.AgentRole, error) {
	r.ID = uuid.New()
	m.roles[r.ID] = r
	return r, nil
}
func (m *memRoleStore) Update(_ context.Context, r domain.AgentRole) (domain.AgentRole, error) {
	m.roles[r.ID] = r
	return r, nil
}
func (m *memRoleStore) Delete(_ context.Context, id uuid.UUID) error {
	delete(m.roles, id)
	return nil
}
func (m *memRoleStore) SetAssignments(_ context.Context, roleID uuid.UUID, assignments []domain.RoleAssignment) error {
	r := m.roles[roleID]
	r.Assignments = assignments
	m.roles[roleID] = r
	return nil
}
func (m *memRoleStore) ListAssignmentsByAgent(_ context.Context, agentID uuid.UUID) ([]domain.AgentRole, error) {
	var out []domain.AgentRole
	for _, r := range m.roles {
		for _, a := range r.Assignments {
			if a.AgentID == agentID {
				out = append(out, r)
			}
		}
	}
	return out, nil
}
func (m *memRoleStore) SetAgentRoles(_ context.Context, agentID uuid.UUID, roles []domain.AgentRoleMembership) error {
	for id, r := range m.roles {
		filtered := r.Assignments[:0]
		for _, a := range r.Assignments {
			if a.AgentID != agentID {
				filtered = append(filtered, a)
			}
		}
		r.Assignments = filtered
		m.roles[id] = r
	}
	for _, mem := range roles {
		r := m.roles[mem.RoleID]
		r.Assignments = append(r.Assignments, domain.RoleAssignment{AgentID: agentID, Areas: mem.Areas})
		m.roles[mem.RoleID] = r
	}
	return nil
}
func (m *memRoleStore) ListPurposes(context.Context) ([]domain.RolePurpose, error) {
	out := make([]domain.RolePurpose, 0, len(m.purposes))
	for p, id := range m.purposes {
		out = append(out, domain.RolePurpose{Purpose: p, RoleID: id})
	}
	return out, nil
}
func (m *memRoleStore) SetPurpose(_ context.Context, p domain.RolePurposeKey, roleID *uuid.UUID) error {
	m.purposes[p] = roleID
	return nil
}

type memWorkflowStore struct {
	types  map[domain.TaskType]domain.TaskTypeDef
	stages map[domain.TaskType][]domain.WorkflowStage
}

func newMemWorkflowStore() *memWorkflowStore {
	return &memWorkflowStore{types: map[domain.TaskType]domain.TaskTypeDef{}, stages: map[domain.TaskType][]domain.WorkflowStage{}}
}

func (m *memWorkflowStore) ListTaskTypes(context.Context) ([]domain.TaskTypeDef, error) {
	out := make([]domain.TaskTypeDef, 0, len(m.types))
	for _, t := range m.types {
		out = append(out, t)
	}
	return out, nil
}
func (m *memWorkflowStore) GetTaskType(_ context.Context, key domain.TaskType) (domain.TaskTypeDef, error) {
	return m.types[key], nil
}
func (m *memWorkflowStore) CreateTaskType(_ context.Context, def domain.TaskTypeDef, _ domain.TaskType) (domain.TaskTypeDef, error) {
	m.types[def.Key] = def
	return def, nil
}
func (m *memWorkflowStore) UpdateTaskType(_ context.Context, def domain.TaskTypeDef) (domain.TaskTypeDef, error) {
	m.types[def.Key] = def
	return def, nil
}
func (m *memWorkflowStore) DeleteTaskType(_ context.Context, key domain.TaskType) error {
	delete(m.types, key)
	return nil
}
func (m *memWorkflowStore) ListStages(_ context.Context, taskType domain.TaskType) ([]domain.WorkflowStage, error) {
	return m.stages[taskType], nil
}
func (m *memWorkflowStore) ReplaceStages(_ context.Context, taskType domain.TaskType, stages []domain.WorkflowStage) error {
	m.stages[taskType] = stages
	return nil
}
func (m *memWorkflowStore) LoadAll(ctx context.Context) ([]domain.Workflow, error) {
	types, _ := m.ListTaskTypes(ctx)
	out := make([]domain.Workflow, 0, len(types))
	for _, t := range types {
		out = append(out, domain.Workflow{Type: t, Stages: m.stages[t.Key]})
	}
	return out, nil
}
func (m *memWorkflowStore) ColumnHasBehaviourStages(context.Context, string) (bool, error) {
	return false, nil
}

func TestWorkflowSnapshotEmptyFailsClosed(t *testing.T) {
	svc := workflow.NewService(newMemRoleStore(), newMemWorkflowStore())
	_, err := svc.Workflow(context.Background(), "task")
	require.ErrorIs(t, err, workflow.ErrSnapshotEmpty)

	_, err = svc.TaskTypeExists(context.Background(), "task")
	require.ErrorIs(t, err, workflow.ErrSnapshotEmpty)
}

func TestAgentForRole_ExactAreaBeatsAnyArea(t *testing.T) {
	roles := newMemRoleStore()
	anyAgent := uuid.New()
	backendAgent := uuid.New()
	roleID := uuid.New()
	roles.roles[roleID] = domain.AgentRole{
		ID: roleID, Key: "developer",
		Assignments: []domain.RoleAssignment{
			// "any area" listed FIRST on purpose — the resolver must still
			// prefer the area-specific assignment regardless of list order.
			{AgentID: anyAgent, Areas: nil, Priority: 0},
			{AgentID: backendAgent, Areas: []string{"backend"}, Priority: 5},
		},
	}
	wfStore := newMemWorkflowStore()
	wfStore.types["task"] = domain.TaskTypeDef{Key: "task", IsDefault: true}

	svc := workflow.NewService(roles, wfStore)
	require.NoError(t, svc.Reload(context.Background()))

	got, err := svc.AgentForRole(context.Background(), roleID, "backend")
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, backendAgent, *got, "an exact-area assignment must win over an any-area one")

	got, err = svc.AgentForRole(context.Background(), roleID, "frontend")
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, anyAgent, *got, "no exact-area match falls back to the any-area assignment")

	got, err = svc.AgentForRole(context.Background(), uuid.New(), "backend")
	require.NoError(t, err)
	require.Nil(t, got, "an unknown role resolves nobody")
}

func TestAgentForRole_DataAndGameFallBackToBackend(t *testing.T) {
	roles := newMemRoleStore()
	backendAgent := uuid.New()
	dataAgent := uuid.New()
	roleID := uuid.New()
	roles.roles[roleID] = domain.AgentRole{
		ID: roleID, Key: "developer",
		Assignments: []domain.RoleAssignment{
			{AgentID: backendAgent, Areas: []string{"backend"}},
			{AgentID: dataAgent, Areas: []string{"data"}},
		},
	}
	wfStore := newMemWorkflowStore()
	wfStore.types["task"] = domain.TaskTypeDef{Key: "task", IsDefault: true}
	svc := workflow.NewService(roles, wfStore)
	require.NoError(t, svc.Reload(context.Background()))

	got, err := svc.AgentForRole(context.Background(), roleID, "data")
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, dataAgent, *got, "the data area's own agent wins")

	got, err = svc.AgentForRole(context.Background(), roleID, "game")
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, backendAgent, *got, "a game repo with no game developer falls back to the backend developer")

	got, err = svc.AgentForRole(context.Background(), roleID, "mobile")
	require.NoError(t, err)
	require.Nil(t, got, "only data and game have a fallback area")
}

func TestAgentForRole_AnyAreaBeatsFallbackArea(t *testing.T) {
	roles := newMemRoleStore()
	backendAgent := uuid.New()
	anyAgent := uuid.New()
	roleID := uuid.New()
	roles.roles[roleID] = domain.AgentRole{
		ID: roleID, Key: "developer",
		Assignments: []domain.RoleAssignment{
			{AgentID: backendAgent, Areas: []string{"backend"}},
			{AgentID: anyAgent, Areas: nil},
		},
	}
	wfStore := newMemWorkflowStore()
	wfStore.types["task"] = domain.TaskTypeDef{Key: "task", IsDefault: true}
	svc := workflow.NewService(roles, wfStore)
	require.NoError(t, svc.Reload(context.Background()))

	got, err := svc.AgentForRole(context.Background(), roleID, "game")
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, anyAgent, *got)
}

func TestAssigneeForNewTask_Modes(t *testing.T) {
	roles := newMemRoleStore()
	roleID := uuid.New()
	roleAgent := uuid.New()
	roles.roles[roleID] = domain.AgentRole{
		ID: roleID, Key: "analyst",
		Assignments: []domain.RoleAssignment{{AgentID: roleAgent, Areas: nil}},
	}
	wfStore := newMemWorkflowStore()
	wfStore.types["task"] = domain.TaskTypeDef{Key: "task", IsDefault: true, AssigneeMode: domain.AssigneeModeNone}
	wfStore.types["analiz"] = domain.TaskTypeDef{Key: "analiz", AssigneeMode: domain.AssigneeModeOverride, AssigneeRoleID: &roleID}
	wfStore.types["custom"] = domain.TaskTypeDef{Key: "custom", AssigneeMode: domain.AssigneeModeDefault, AssigneeRoleID: &roleID}

	svc := workflow.NewService(roles, wfStore)
	require.NoError(t, svc.Reload(context.Background()))

	requested := uuid.New()

	got, err := svc.AssigneeForNewTask(context.Background(), "task", "backend", &requested)
	require.NoError(t, err)
	require.Equal(t, &requested, got, "mode none never touches the requested assignee")

	got, err = svc.AssigneeForNewTask(context.Background(), "analiz", "backend", &requested)
	require.NoError(t, err)
	require.Equal(t, roleAgent, *got, "mode override always prefers the role's agent")

	got, err = svc.AssigneeForNewTask(context.Background(), "custom", "backend", nil)
	require.NoError(t, err)
	require.Equal(t, roleAgent, *got, "mode default fills the role's agent only when nothing was requested")

	got, err = svc.AssigneeForNewTask(context.Background(), "custom", "backend", &requested)
	require.NoError(t, err)
	require.Equal(t, &requested, got, "mode default leaves a requested assignee alone")
}
