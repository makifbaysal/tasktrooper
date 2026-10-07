package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/workflow/workflowtest"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestCreateTaskRefusesTheProductManagerAsAssignee(t *testing.T) {
	f := newAssigneeFixture()
	pm := workflowtest.AgentID("product-manager")

	_, err := f.svc.CreateTask(context.Background(), f.repoID, domain.CreateBoardTaskRequest{
		Title:           "ship it",
		AssigneeAgentID: &pm,
	})

	require.ErrorIs(t, err, domain.ErrAssigneeNotAssignable)
	assert.Empty(t, f.tasks.tasks, "a refused assignee creates nothing")
}

func TestCreateTaskLeavesASystemTaskUnassignedWhenItsPurposeResolvesToThePM(t *testing.T) {
	f := newAssigneeFixture()
	pm := workflowtest.AgentID("product-manager")

	task := f.create(t, domain.CreateBoardTaskRequest{CreatedBy: "system", AssigneeAgentID: &pm})

	assert.Nil(t, task.AssigneeAgentID)
}

func TestCreateTaskFallsBackToTheRequestedAgentWhenTheTypeRoleIsThePM(t *testing.T) {
	f := newAssigneeFixture()
	pmRole := workflowtest.RoleID("product_manager")
	fx := workflowtest.Default()
	wf := fx.Workflows[domain.TaskTypeTask]
	wf.Type.AssigneeRoleID = &pmRole
	wf.Type.AssigneeMode = domain.AssigneeModeOverride
	fx.Workflows[domain.TaskTypeTask] = wf
	f.svc.workflows = fx.Reader()
	f.svc.roles = fx.Resolver()
	dev := workflowtest.AgentID("backend-developer")

	task := f.create(t, domain.CreateBoardTaskRequest{AssigneeAgentID: &dev})

	require.NotNil(t, task.AssigneeAgentID)
	assert.Equal(t, dev, *task.AssigneeAgentID)
}

func TestUpdateTaskRefusesTheProductManagerAsAssignee(t *testing.T) {
	f := newAssigneeFixture()
	task := f.create(t, domain.CreateBoardTaskRequest{})
	pm := workflowtest.AgentID("product-manager")

	_, err := f.svc.UpdateTask(context.Background(), f.repoID, task.ID, domain.UpdateBoardTaskRequest{
		AssigneeAgentID: domain.SetNullable(pm),
	})

	require.ErrorIs(t, err, domain.ErrAssigneeNotAssignable)
	assert.Nil(t, f.tasks.tasks[task.ID].AssigneeAgentID)
}

func TestUpdateTaskKeepsAnExistingPMAssigneeEditable(t *testing.T) {
	f := newAssigneeFixture()
	task := f.create(t, domain.CreateBoardTaskRequest{})
	pm := workflowtest.AgentID("product-manager")
	stored := f.tasks.tasks[task.ID]
	stored.AssigneeAgentID = &pm
	f.tasks.tasks[task.ID] = stored

	title := "renamed"
	_, err := f.svc.UpdateTask(context.Background(), f.repoID, task.ID, domain.UpdateBoardTaskRequest{
		Title:           &title,
		AssigneeAgentID: domain.SetNullable(pm),
	})

	require.NoError(t, err, "re-sending the assignee a task already has is not a new assignment")
}

func TestClaimTaskRefusesTheProductManager(t *testing.T) {
	f := newAssigneeFixture()
	task := f.create(t, domain.CreateBoardTaskRequest{})

	_, err := f.svc.ClaimTask(context.Background(), f.repoID, task.ID, workflowtest.AgentID("product-manager"))

	require.ErrorIs(t, err, domain.ErrAssigneeNotAssignable)
}

func TestOtherAgentsStayAssignable(t *testing.T) {
	f := newAssigneeFixture()
	agent := uuid.New()

	task := f.create(t, domain.CreateBoardTaskRequest{AssigneeAgentID: &agent})

	require.NotNil(t, task.AssigneeAgentID)
	assert.Equal(t, agent, *task.AssigneeAgentID)
}
