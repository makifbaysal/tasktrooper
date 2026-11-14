package board

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestAReviewThatRecordedNoVerdictIsAFailedRunTheReconcilerRetries(t *testing.T) {
	run := domain.TaskAgentRun{Status: domain.TaskAgentRunStatusFailed, Summary: reviewVerdictMissingSummary(domain.TaskColumnInQA)}

	assert.True(t, isReviewVerdictMissingRun(run))
	assert.Equal(t, "review verdict not recorded: the review ended with the task still in in_qa and no verdict; retried with a fresh run", run.Summary)
	assert.False(t, isUnsettledCriteriaRun(run), "the criteria loop guard must not read it as its own streak")
}

func TestOnlyTheLastRetriedReviewLeavesTheCardToAPerson(t *testing.T) {
	current := domain.TaskAgentRun{ID: uuid.New()}
	failed := func() domain.TaskAgentRun {
		return domain.TaskAgentRun{ID: uuid.New(), Status: domain.TaskAgentRunStatusFailed}
	}
	completed := domain.TaskAgentRun{ID: uuid.New(), Status: domain.TaskAgentRunStatusCompleted}

	assert.False(t, lastReviewVerdictAttempt(nil, current), "first attempt: the reconciler retries it")
	assert.False(t, lastReviewVerdictAttempt([]domain.TaskAgentRun{current, failed()}, current))
	assert.False(t, lastReviewVerdictAttempt([]domain.TaskAgentRun{failed(), completed, failed()}, current),
		"a run that finished in between starts the streak again")
	assert.True(t, lastReviewVerdictAttempt([]domain.TaskAgentRun{current, failed(), failed()}, current),
		"the reconciler stops at three failed runs in a row, so this one says so")
}
