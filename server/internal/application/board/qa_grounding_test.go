package board

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func qaUsage(tools ...string) *registry.ToolUsage {
	usage := &registry.ToolUsage{}
	for _, name := range tools {
		usage.Record(name)
	}
	return usage
}

func TestUngroundedQARejectsARunThatExecutedNothing(t *testing.T) {
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnInQA}

	assert.True(t, isUngroundedQA(taskWF, task, domain.AgentResponse{}, qaUsage("move_board_task")))
}

func TestUngroundedQADoesNotAcceptVerdictsAsEvidence(t *testing.T) {
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnInQA}

	usage := qaUsage("review_criterion", "add_task_comment", "list_acceptance_criteria", "get_pipeline_status")

	assert.True(t, isUngroundedQA(taskWF, task, domain.AgentResponse{}, usage))
}

func TestUngroundedQAAcceptsAnExecutedRound(t *testing.T) {
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnInQA}

	for _, tool := range []string{"run_terminal", "browser_navigate", "browser_screenshot"} {
		assert.False(t, isUngroundedQA(taskWF, task, domain.AgentResponse{}, qaUsage(tool, "review_criterion")),
			"%s is an executed test", tool)
	}
}

func TestUngroundedQACoversTheQueueColumn(t *testing.T) {
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnReadyForQA}

	assert.True(t, isUngroundedQA(taskWF, task, domain.AgentResponse{}, qaUsage("move_board_task")))
}

func TestUngroundedQAIgnoresEveryOtherRun(t *testing.T) {
	for _, column := range []domain.TaskColumn{
		domain.TaskColumnInProgress,
		domain.TaskColumnCodeReview,
		domain.TaskColumnNeedRevision,
	} {
		task := domain.BoardTask{ID: uuid.New(), Column: column}
		assert.False(t, isUngroundedQA(taskWF, task, domain.AgentResponse{}, qaUsage("add_task_comment")),
			"column %s is not a QA round", column)
	}

	analiz := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnInQA, TaskType: "analiz"}
	assert.False(t, isUngroundedQA(analizWF, analiz, domain.AgentResponse{}, qaUsage("add_task_comment")))
}

func TestUngroundedQAExemptsAQuestion(t *testing.T) {
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnInQA}
	resp := domain.AgentResponse{Clarification: &domain.ClarificationRequest{Context: "which stage url?"}}

	assert.False(t, isUngroundedQA(taskWF, task, resp, qaUsage("move_board_task")))
}

func TestUngroundedQAIsNilSafe(t *testing.T) {
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnInQA}

	assert.False(t, isUngroundedQA(taskWF, task, domain.AgentResponse{}, nil))
}
