package board

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// The report this gate exists for: a PM run in pm_uat that approves every
// criterion from board-read tools alone, never touching the running product.
func TestUngroundedPMUATRejectsAReviewWithNoExecution(t *testing.T) {
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnPMUAT}

	usage := qaUsage("list_test_cases", "read_file", "review_criterion")

	assert.True(t, isUngroundedPMUAT(task, domain.AgentResponse{}, usage))
}

func TestUngroundedPMUATAcceptsAnExecutedRound(t *testing.T) {
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnPMUAT}

	for _, tool := range []string{"browser_navigate", "browser_screenshot", "mobile_tap"} {
		assert.False(t, isUngroundedPMUAT(task, domain.AgentResponse{}, qaUsage(tool, "review_criterion")),
			"%s is an executed pm_uat check", tool)
	}
}

// Everything that is not a pm_uat run passes untouched.
func TestUngroundedPMUATIgnoresNonPMUATColumns(t *testing.T) {
	for _, column := range []domain.TaskColumn{
		domain.TaskColumnInProgress,
		domain.TaskColumnCodeReview,
		domain.TaskColumnInQA,
		domain.TaskColumnReadyForQA,
		domain.TaskColumnNeedRevision,
		domain.TaskColumnHumanUAT,
	} {
		task := domain.BoardTask{ID: uuid.New(), Column: column}
		assert.False(t, isUngroundedPMUAT(task, domain.AgentResponse{}, qaUsage("add_task_comment")),
			"column %s is not a pm_uat round", column)
	}
}

// Asking IS the answer, same rule as isUngroundedQA.
func TestUngroundedPMUATExemptsAQuestion(t *testing.T) {
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnPMUAT}
	resp := domain.AgentResponse{Clarification: &domain.ClarificationRequest{Context: "which stage url?"}}

	assert.False(t, isUngroundedPMUAT(task, resp, qaUsage("review_criterion")))
}

// An unmeasured run has no ledger to judge.
func TestUngroundedPMUATIsNilSafe(t *testing.T) {
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnPMUAT}

	assert.False(t, isUngroundedPMUAT(task, domain.AgentResponse{}, nil))
}

func pmApprovedCriterion(id uuid.UUID) domain.AcceptanceCriterion {
	return domain.AcceptanceCriterion{
		ID: id,
		Checks: []domain.CriterionCheck{
			{CriterionID: id, Role: domain.CriterionReviewRolePM, Approved: true},
		},
	}
}

func TestPMApprovedUncoveredCriterionRequiresOwnEvidence(t *testing.T) {
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnPMUAT}
	criterionID := uuid.New()
	criteria := []domain.AcceptanceCriterion{pmApprovedCriterion(criterionID)}

	usage := qaUsage("list_test_cases", "review_criterion")

	assert.True(t, pmApprovedUncoveredCriterion(task, criteria, nil, usage))
}

func TestPMApprovedUncoveredCriterionPassesWhenQACaseIsPassed(t *testing.T) {
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnPMUAT}
	criterionID := uuid.New()
	criteria := []domain.AcceptanceCriterion{pmApprovedCriterion(criterionID)}
	testCases := []domain.TaskTestCase{
		{ID: uuid.New(), CriterionID: &criterionID, Status: domain.TestCaseStatusPassed},
	}

	usage := qaUsage("list_test_cases", "review_criterion")

	assert.False(t, pmApprovedUncoveredCriterion(task, criteria, testCases, usage))
}

func TestPMApprovedUncoveredCriterionPassesWhenPMExecutedItself(t *testing.T) {
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnPMUAT}
	criterionID := uuid.New()
	criteria := []domain.AcceptanceCriterion{pmApprovedCriterion(criterionID)}

	usage := qaUsage("browser_navigate", "review_criterion")

	assert.False(t, pmApprovedUncoveredCriterion(task, criteria, nil, usage))
}

func TestPMApprovedUncoveredCriterionIgnoresNonPMUATColumns(t *testing.T) {
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnInQA}
	criterionID := uuid.New()
	criteria := []domain.AcceptanceCriterion{pmApprovedCriterion(criterionID)}

	assert.False(t, pmApprovedUncoveredCriterion(task, criteria, nil, qaUsage("read_file")))
}

func TestPMApprovedUncoveredCriterionIsNilSafe(t *testing.T) {
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnPMUAT}
	criterionID := uuid.New()
	criteria := []domain.AcceptanceCriterion{pmApprovedCriterion(criterionID)}

	assert.False(t, pmApprovedUncoveredCriterion(task, criteria, nil, nil))
}
