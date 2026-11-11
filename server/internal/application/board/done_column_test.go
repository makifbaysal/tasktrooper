package board

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// commit_on_finish is the production rule the runner reads; asserting on it
// directly keeps this honest about what actually decides whether a run pushes.
func TestDoneRunNeverCommitsTheWorkspace(t *testing.T) {
	assert.False(t, taskWF.Has(domain.TaskColumnDone, domain.BehaviourCommitOnFinish),
		"a run in done merges a finished change; committing its workspace would re-create the merged branch")

	for _, column := range []domain.TaskColumn{
		domain.TaskColumnInProgress,
		domain.TaskColumnNeedRevision,
		domain.TaskColumnTodo,
		domain.TaskColumnInQA,
	} {
		assert.True(t, taskWF.Has(column, domain.BehaviourCommitOnFinish), "%s must still commit and push", column)
	}

	for _, column := range []domain.TaskColumn{
		domain.TaskColumnCodeReview,
		domain.TaskColumnAnalizReview,
	} {
		assert.False(t, taskWF.Has(column, domain.BehaviourCommitOnFinish))
	}
}

// The merge-not-release workflow (merge_task_pull_request, batch releases,
// runtime-environment evidence, do-NOT-move-to-released) used to be spelled
// out in columnInstruction's own done case; it now lives entirely in
// release-engineer's own done.md column file — see catalogrepo's
// TestReleaseEngineerDoneAndReleasedForbidEditingCode and the release-engineer
// content asserted there. columnInstruction keeps only the engine fact that
// the board has signed the task off, and it must never fall back to the
// generic "move the task on to the next column" default.
func TestDoneInstructionIsAboutMergingAndNeverAboutReleasing(t *testing.T) {
	instruction := columnInstruction(taskWF, domain.BoardTask{
		Column:   domain.TaskColumnDone,
		TaskType: "task",
	})

	assert.Contains(t, instruction, "the board has signed it off")
	assert.NotContains(t, instruction, "move the task on to the next column")
}

// The analiz-vs-task split (decompose lives only in system-architect's own
// done.md, checked by catalogrepo's TestSystemArchitectCatalogCarriesTheAnalizWorkflow)
// still has to hold for the engine's own reduced fact line.
func TestDoneInstructionForAnalizIsUnchanged(t *testing.T) {
	instruction := columnInstruction(analizWF, domain.BoardTask{
		Column:   domain.TaskColumnDone,
		TaskType: "analiz",
	})

	assert.Contains(t, instruction, "the human's move here is the approval")
	assert.NotContains(t, instruction, "merge_task_pull_request")
}
