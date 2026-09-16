package settings_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/settings"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type fakeSettingsStore struct {
	got                 domain.AppSettings
	updateAnalizCalls   int
	updateAnalizArgs    [3]string
	updateAnalizReturns domain.AppSettings
}

func (f *fakeSettingsStore) Get(context.Context) (domain.AppSettings, error) { return f.got, nil }

func (f *fakeSettingsStore) Update(context.Context, domain.UpdateSettingsRequest) (domain.AppSettings, error) {
	return f.got, nil
}

func (f *fakeSettingsStore) UpdateAnalizAssignment(_ context.Context, backend, frontend, mobile string) (domain.AppSettings, error) {
	f.updateAnalizCalls++
	f.updateAnalizArgs = [3]string{backend, frontend, mobile}
	return f.updateAnalizReturns, nil
}

type fakeAgentCatalog struct {
	byName  map[string]domain.Agent
	updated []domain.Agent
}

func (f *fakeAgentCatalog) ListAgents(context.Context) ([]domain.Agent, error) {
	out := make([]domain.Agent, 0, len(f.byName))
	for _, a := range f.byName {
		out = append(out, a)
	}
	return out, nil
}

func (f *fakeAgentCatalog) UpdateAgent(_ context.Context, id uuid.UUID, req domain.UpdateAgentRequest) (domain.Agent, error) {
	updated := domain.Agent{ID: id, Name: req.Name, ToolPolicy: req.ToolPolicy, Enabled: req.Enabled}
	f.updated = append(f.updated, updated)
	for name, a := range f.byName {
		if a.ID == id {
			f.byName[name] = updated
		}
	}
	return updated, nil
}

func newBackendDeveloperShapedAgent() domain.Agent {
	return domain.Agent{
		ID:   uuid.New(),
		Name: domain.AgentBackendDeveloper,
		ToolPolicy: domain.ToolPolicy{AllowTools: []string{
			"run_terminal", "codebase_search", "read_file",
			"claim_board_task", "move_board_task", "add_task_comment",
			"list_task_comments", "list_task_documents",
		}},
	}
}

func TestUpdateAnalizAssignment_SystemArchitectSkipsToolCheckAndSaves(t *testing.T) {
	store := &fakeSettingsStore{updateAnalizReturns: domain.AppSettings{AnalizAssigneeBackend: domain.AgentSystemArchitect}}
	svc := settings.NewService(store)

	result, err := svc.UpdateAnalizAssignment(context.Background(), domain.UpdateAnalizAssignmentRequest{Backend: domain.AgentSystemArchitect})

	require.NoError(t, err)
	assert.True(t, result.Saved)
	assert.Equal(t, 1, store.updateAnalizCalls)
	assert.Equal(t, [3]string{domain.AgentSystemArchitect, "", ""}, store.updateAnalizArgs)
}

func TestUpdateAnalizAssignment_MissingToolsWithoutConfirmRefusesAndSavesNothing(t *testing.T) {
	agent := newBackendDeveloperShapedAgent()
	store := &fakeSettingsStore{}
	catalog := &fakeAgentCatalog{byName: map[string]domain.Agent{agent.Name: agent}}
	svc := settings.NewService(store)
	svc.SetAgentCatalog(catalog)

	result, err := svc.UpdateAnalizAssignment(context.Background(), domain.UpdateAnalizAssignmentRequest{Backend: domain.AgentBackendDeveloper})

	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrMissingAnalizTools))
	var missingErr *domain.MissingAnalizToolsError
	require.ErrorAs(t, err, &missingErr)
	assert.Contains(t, missingErr.Missing["backend"], "create_board_task")
	assert.False(t, result.Saved)
	assert.Equal(t, 0, store.updateAnalizCalls, "the setting must not be written when a tool check fails")
	assert.Empty(t, catalog.updated, "the agent's tool policy must not be widened without confirmation")
}

func TestUpdateAnalizAssignment_ConfirmGrantToolsWidensPolicyAndSaves(t *testing.T) {
	agent := newBackendDeveloperShapedAgent()
	store := &fakeSettingsStore{updateAnalizReturns: domain.AppSettings{AnalizAssigneeBackend: domain.AgentBackendDeveloper}}
	catalog := &fakeAgentCatalog{byName: map[string]domain.Agent{agent.Name: agent}}
	svc := settings.NewService(store)
	svc.SetAgentCatalog(catalog)

	result, err := svc.UpdateAnalizAssignment(context.Background(), domain.UpdateAnalizAssignmentRequest{
		Backend:           domain.AgentBackendDeveloper,
		ConfirmGrantTools: true,
	})

	require.NoError(t, err)
	assert.True(t, result.Saved)
	assert.Equal(t, 1, store.updateAnalizCalls)
	assert.Equal(t, [3]string{domain.AgentBackendDeveloper, "", ""}, store.updateAnalizArgs)
	require.Len(t, catalog.updated, 1)
	assert.Contains(t, catalog.updated[0].ToolPolicy.AllowTools, "create_board_task")
	assert.Contains(t, result.GrantedTools["backend"], "create_board_task")
}
