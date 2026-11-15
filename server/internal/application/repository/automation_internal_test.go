package repository

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/makifbaysal/tasktrooper/server/internal/application/workflow/workflowtest"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestAutomationGateHoldsOnlyQAsOwnForwardMove(t *testing.T) {
	svc := &Service{workflows: workflowtest.Default().Reader()}
	on := domain.Repository{AutomationTests: true}
	cases := []struct {
		name   string
		repo   domain.Repository
		prev   domain.TaskColumn
		target domain.TaskColumn
		actor  domain.TaskActor
		held   bool
	}{
		{"QA passing a task on is held for the suite", on, domain.TaskColumnInQA, domain.TaskColumnHumanUAT, domain.TaskActorAgent, true},
		{"the board's own move after a green suite goes through", on, domain.TaskColumnInQA, domain.TaskColumnHumanUAT, domain.TaskActorSystem, false},
		{"a person is never held", on, domain.TaskColumnInQA, domain.TaskColumnHumanUAT, domain.TaskActorHuman, false},
		{"need_revision stays QA's", on, domain.TaskColumnInQA, domain.TaskColumnNeedRevision, domain.TaskActorAgent, false},
		{"off, nothing changes", domain.Repository{}, domain.TaskColumnInQA, domain.TaskColumnHumanUAT, domain.TaskActorAgent, false},
		{"only QA stages", on, domain.TaskColumnCodeReview, domain.TaskColumnReadyForQA, domain.TaskActorAgent, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := svc.automationGate(context.Background(), tc.repo, "task", tc.prev, tc.target, tc.actor)
			if tc.held {
				assert.ErrorContains(t, err, "automation tests are on")
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestAutomationBackfillCriteriaAreTheCatalogLines(t *testing.T) {
	got := automationBackfillCriteria()
	assert.Len(t, got, 4)
	for _, c := range got {
		assert.NotEmpty(t, c.Text)
		assert.NotContains(t, c.Text, "- ")
	}
}
