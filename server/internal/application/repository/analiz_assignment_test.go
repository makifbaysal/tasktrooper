package repository

// CreateTask's analiz-assignment override: an analiz task's assignee is
// resolved from the backend/frontend/mobile settings, not from whatever the
// caller (typically the PM agent) requested — see AnalizAssignmentSource.

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type fakeAnalizAssignmentSource struct {
	settings domain.AppSettings
}

func (f *fakeAnalizAssignmentSource) Get(context.Context) (domain.AppSettings, error) {
	return f.settings, nil
}

func newAnalizAssignmentFixture(settings domain.AppSettings, agents []domain.Agent) *assigneeFixture {
	f := newAssigneeFixture()
	f.svc.analizAssignment = &fakeAnalizAssignmentSource{settings: settings}
	f.svc.agentLister = func(context.Context) ([]domain.Agent, error) { return agents, nil }
	f.svc.repos = &fakeReleaseRepoStore{repo: domain.Repository{ID: f.repoID, Kind: domain.RepoKindBackend}}
	return f
}

func TestCreateTaskOverridesAnalizAssigneeFromSettings(t *testing.T) {
	backendDevID := uuid.New()
	architectID := uuid.New()
	f := newAnalizAssignmentFixture(
		domain.AppSettings{AnalizAssigneeBackend: domain.AgentBackendDeveloper},
		[]domain.Agent{
			{ID: architectID, Name: domain.AgentSystemArchitect},
			{ID: backendDevID, Name: domain.AgentBackendDeveloper},
		},
	)
	pmChoice := architectID

	task := f.create(t, domain.CreateBoardTaskRequest{
		TaskType:        domain.TaskTypeAnaliz,
		AssigneeAgentID: &pmChoice,
	})

	require.NotNil(t, task.AssigneeAgentID)
	assert.Equal(t, backendDevID, *task.AssigneeAgentID, "the setting's agent replaces the PM's requested assignee")
}

func TestCreateTaskDoesNotOverrideAssigneeForNonAnalizTaskType(t *testing.T) {
	backendDevID := uuid.New()
	pmChoiceID := uuid.New()
	f := newAnalizAssignmentFixture(
		domain.AppSettings{AnalizAssigneeBackend: domain.AgentBackendDeveloper},
		[]domain.Agent{{ID: backendDevID, Name: domain.AgentBackendDeveloper}},
	)

	task := f.create(t, domain.CreateBoardTaskRequest{
		TaskType:        domain.TaskTypeTask,
		AssigneeAgentID: &pmChoiceID,
	})

	require.NotNil(t, task.AssigneeAgentID)
	assert.Equal(t, pmChoiceID, *task.AssigneeAgentID, "only analiz tasks are overridden")
}

func TestCreateTaskLeavesAssigneeAloneWhenAnalizAssignmentSourceIsUnwired(t *testing.T) {
	f := newAssigneeFixture()
	pmChoiceID := uuid.New()

	task := f.create(t, domain.CreateBoardTaskRequest{
		TaskType:        domain.TaskTypeAnaliz,
		AssigneeAgentID: &pmChoiceID,
	})

	require.NotNil(t, task.AssigneeAgentID)
	assert.Equal(t, pmChoiceID, *task.AssigneeAgentID)
}
