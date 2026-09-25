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
		domain.TaskColumnPMUAT,
	} {
		assert.False(t, taskWF.Has(column, domain.BehaviourCommitOnFinish))
	}
}

func TestDoneInstructionIsAboutMergingAndNeverAboutReleasing(t *testing.T) {
	instruction := columnInstruction(taskWF, domain.BoardTask{
		Column:   domain.TaskColumnDone,
		TaskType: "task",
	})

	assert.Contains(t, instruction, "merge_task_pull_request")
	assert.NotContains(t, instruction, "move the task on to the next column")
	assert.Contains(t, instruction, "do NOT move this task to `released`")
}

func TestDoneInstructionCoversBatchReleases(t *testing.T) {
	instruction := columnInstruction(taskWF, domain.BoardTask{
		Column:   domain.TaskColumnDone,
		TaskType: "task",
	})

	for _, want := range []string{
		"joined the component's draft release",
		"a human just cut this release",
		"no bound runtime environment",
		"only reverts the default branch",
		"local_run.tail",
	} {
		assert.Contains(t, instruction, want)
	}
}

func TestDoneInstructionForAnalizIsUnchanged(t *testing.T) {
	instruction := columnInstruction(analizWF, domain.BoardTask{
		Column:   domain.TaskColumnDone,
		TaskType: "analiz",
	})

	assert.Contains(t, instruction, "decompose")
	assert.NotContains(t, instruction, "merge_task_pull_request")
}
