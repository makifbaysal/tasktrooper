package board

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/makifbaysal/tasktrooper/server/internal/application/workflow/workflowtest"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

var designWF = workflowtest.Default().Workflows[domain.TaskTypeDesign]

func TestDesignTaskColumnsReadTheDesignInstructions(t *testing.T) {
	for col, want := range map[domain.TaskColumn]string{
		domain.TaskColumnTodo:         "This is a design task in `todo`",
		domain.TaskColumnInProgress:   "This is a design task ALREADY claimed",
		domain.TaskColumnNeedRevision: "This is a design task in `need_revision`",
		domain.TaskColumnDone:         "every design system version this task proposed is now approved",
	} {
		got := columnInstruction(designWF, domain.BoardTask{TaskType: domain.TaskTypeDesign, Column: col})
		assert.Contains(t, got, want, "column %s", col)
		assert.NotContains(t, got, "analiz task", "column %s", col)
	}
}

func TestDesignWorkflowAdvancesOnTheDocumentAndApprovesOnDone(t *testing.T) {
	to, ok := designWF.Param(domain.TaskColumnInProgress, domain.BehaviourAdvanceOnDocument, "to")
	assert.True(t, ok)
	assert.Equal(t, "analiz_review", to)
	assert.True(t, designWF.Has(domain.TaskColumnDone, domain.BehaviourApproveDesignSystem))
	assert.True(t, designWF.Type.Has(domain.BehaviourNoWorkspaceWrites))
}

func TestDesignTaskAsksThroughOpenQuestions(t *testing.T) {
	policy := domain.ToolPolicy{AllowTools: []string{"read_file"}}
	assert.NotContains(t, cliAskPolicy(policy, domain.TaskTypeDesign).AllowTools, "ask_user")
	assert.Contains(t, cliAskPolicy(policy, domain.TaskTypeTask).AllowTools, "ask_user")
}

type fakeDesignNotes struct {
	gotRepo     uuid.UUID
	gotShowTool bool
}

func (f *fakeDesignNotes) ContextNote(_ context.Context, repositoryID uuid.UUID, showTool bool) string {
	f.gotRepo, f.gotShowTool = repositoryID, showTool
	return "## Design system (TaskTrooper)"
}

func TestDesignSystemNoteFollowsThePolicy(t *testing.T) {
	r := &Runner{}
	assert.Empty(t, r.designSystemNote(context.Background(), uuid.New(), domain.ToolPolicy{}))

	notes := &fakeDesignNotes{}
	r.SetDesignSystems(notes)
	repo := uuid.New()
	got := r.designSystemNote(context.Background(), repo, domain.UpliftWorkspaceTools(domain.ToolPolicy{AllowTools: []string{"read_file"}}))
	assert.Equal(t, "## Design system (TaskTrooper)", got)
	assert.Equal(t, repo, notes.gotRepo)
	assert.True(t, notes.gotShowTool, "get_design_system rides with the always-available read tools")

	assert.Empty(t, r.designSystemNote(context.Background(), uuid.Nil, domain.ToolPolicy{}))
}
