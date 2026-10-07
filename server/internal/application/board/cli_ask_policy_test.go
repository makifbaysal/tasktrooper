package board

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestCLIAskPolicyGrantsAskUserOutsideAnaliz(t *testing.T) {
	scoped := domain.ToolPolicy{AllowTools: []string{"move_board_task"}}

	assert.True(t, domain.ToolAllowedByPolicy(domain.AskUserToolName, cliAskPolicy(scoped, domain.TaskTypeTask)))
	assert.False(t, domain.ToolAllowedByPolicy(domain.AskUserToolName, cliAskPolicy(scoped, domain.TaskTypeAnaliz)),
		"analiz asks through record_open_questions")
	assert.Equal(t, []string{"move_board_task"}, scoped.AllowTools, "the caller's policy is not mutated")
}

func TestCLIAskPolicyLeavesAnUnrestrictedPolicyUnrestricted(t *testing.T) {
	open := domain.ToolPolicy{AllowMCPServers: []string{"github"}}

	got := cliAskPolicy(open, domain.TaskTypeTask)

	assert.Empty(t, got.AllowTools, "adding one name to an empty allow list would take every other tool away")
}
