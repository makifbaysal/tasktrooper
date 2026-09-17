package catalog

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// A QA run is dispatched into a verdict column (in_qa / ready_for_qa), and the
// board runner narrows its policy twice on the way there. Both narrowings take
// WRITERS away — the file tools and commit_task_changes — because a round that
// judges someone else's work must not change it.
//
// The two tools a verdict is actually made of must survive that. A QA agent
// without list_acceptance_criteria has no criterion ids to rule on, and without
// review_criterion no way to record a ruling; it then writes its verdicts as
// prose in a comment, the criteria gate refuses the forward move because nothing
// was ruled on, and the board dispatches the same task again. That loop is
// silent and unbounded, so the invariant is worth a test of its own.
func TestQARunPolicyKeepsTheVerdictTools(t *testing.T) {
	for _, column := range []domain.TaskColumn{
		domain.TaskColumnReadyForQA,
		domain.TaskColumnInQA,
	} {
		t.Run(string(column), func(t *testing.T) {
			// Exactly what board.Runner computes (runner.go: upliftedPolicy).
			policy := domain.RestrictToolsForVerdictColumn(
				domain.RestrictToolsForTaskType(
					domain.UpliftWorkspaceTools(
						domain.MergeToolPolicy(domain.ToolPolicy{}, qaToolPolicy()),
					),
					domain.TaskTypeTask,
				),
				column,
			)
			require.NotEmpty(t, policy.AllowTools)

			allowed := func(name string) bool { return domain.ToolAllowedByPolicy(name, policy) }
			assert.True(t, allowed("list_acceptance_criteria"), "QA has no criterion ids without it")
			assert.True(t, allowed("review_criterion"), "QA cannot record a verdict without it")
			assert.True(t, allowed("move_board_task"), "QA cannot leave the column without it")
			assert.True(t, allowed("add_task_comment"))
			assert.True(t, allowed("run_terminal"), "QA still has to exercise the product")

			// The narrowing did happen: a verdict round holds no writers.
			assert.False(t, allowed("write_file"))
			assert.False(t, allowed("edit_file"))
			assert.False(t, allowed("commit_task_changes"))
			// And no way to land the change it is judging. QA merges in `done`,
			// after the board has signed the task off — never from the column
			// where it is still deciding whether the change is any good.
			assert.False(t, allowed(domain.MergePullRequestToolName),
				"a verdict round must not be able to merge the change under test")
		})
	}
}

// The other half of the same rule: in `done` QA keeps the merge tool, because
// merging is the only thing it is dispatched there to do — while everything that
// could write to the branch is still taken away, since a commit there would push
// the branch back to origin moments after the merge deleted it.
func TestQARunPolicyKeepsTheMergeToolInDone(t *testing.T) {
	policy := domain.RestrictToolsForVerdictColumn(
		domain.RestrictToolsForTaskType(
			domain.UpliftWorkspaceTools(
				domain.MergeToolPolicy(domain.ToolPolicy{}, qaToolPolicy()),
			),
			domain.TaskTypeTask,
		),
		domain.TaskColumnDone,
	)
	require.NotEmpty(t, policy.AllowTools)

	assert.True(t, domain.ToolAllowedByPolicy(domain.MergePullRequestToolName, policy))
	assert.True(t, domain.ToolAllowedByPolicy("get_task_pull_request", policy),
		"QA reads the PR before merging it")
	assert.True(t, domain.ToolAllowedByPolicy("get_pipeline_status", policy),
		"QA checks the build before merging it")
	assert.True(t, domain.ToolAllowedByPolicy("add_task_comment", policy),
		"QA reports the merge on the card")
	// Nothing that could put the branch back after the merge deleted it.
	assert.False(t, domain.ToolAllowedByPolicy("commit_task_changes", policy))
	assert.False(t, domain.ToolAllowedByPolicy("write_file", policy))
	assert.False(t, domain.ToolAllowedByPolicy("edit_file", policy))
}

// Nobody else gets it. The developer must not merge its own branch, the
// architect reviews it, the PM signs off on the product — and none of them is
// the role that ran the built thing last.
func TestOnlyQAHoldsTheMergeTool(t *testing.T) {
	for name, policy := range map[string]domain.ToolPolicy{
		"developer":        developerToolPolicy(),
		"mobile-developer": mobileDeveloperToolPolicy(),
		"system-architect": architectToolPolicy(),
		"product-manager":  productManagerToolPolicy(),
	} {
		t.Run(name, func(t *testing.T) {
			for _, tool := range policy.AllowTools {
				assert.NotEqual(t, domain.MergePullRequestToolName, tool)
			}
		})
	}
	assert.Contains(t, qaToolPolicy().AllowTools, domain.MergePullRequestToolName)
}

// An unscoped agent does not get the merge from the workspace uplift either.
// The uplift widens what an agent may see and change in ITS OWN workspace on the
// strength of a stale row; landing code on the default branch is not something
// a stale row should be able to authorise.
func TestWorkspaceUpliftDoesNotGrantTheMergeTool(t *testing.T) {
	uplifted := domain.UpliftWorkspaceTools(domain.ToolPolicy{AllowTools: []string{"run_terminal"}})

	assert.False(t, domain.ToolAllowedByPolicy(domain.MergePullRequestToolName, uplifted))
	// The uplift still did its job for the tools it does grant.
	assert.True(t, domain.ToolAllowedByPolicy("commit_task_changes", uplifted))
}

// ---------------------------------------------------------------------------
// The deploy watch and the rollback (migration 105).
//
// Three tools, one role, and one of them changes what is running in production.
// The tests below pin who holds them and where, because both halves of that
// have a cheap way to go wrong: a role list is a slice anyone can append to, and
// the column narrowing is a loop that has to name the tool explicitly.

func qaRunPolicyIn(column domain.TaskColumn) domain.ToolPolicy {
	// Exactly what board.Runner computes (runner.go: upliftedPolicy).
	return domain.RestrictToolsForVerdictColumn(
		domain.RestrictToolsForTaskType(
			domain.UpliftWorkspaceTools(
				domain.MergeToolPolicy(domain.ToolPolicy{}, qaToolPolicy()),
			),
			domain.TaskTypeTask,
		),
		column,
	)
}

func TestQAHoldsTheDeployWatchTools(t *testing.T) {
	policy := qaToolPolicy()
	for _, name := range []string{
		domain.DeployStatusToolName,
		domain.DeployLogsToolName,
		domain.RollbackReleaseToolName,
	} {
		assert.Contains(t, policy.AllowTools, name,
			"qa-agent must hold %s — it is the role dispatched into done/released", name)
	}
}

// Nobody else does. A developer that could roll production back could undo a
// release it was never asked about; a reviewer could undo the change it is
// judging.
func TestOnlyQAHoldsTheRollback(t *testing.T) {
	others := map[string]domain.ToolPolicy{
		"developer":        developerToolPolicy(),
		"mobile-developer": mobileDeveloperToolPolicy(),
		"architect":        architectToolPolicy(),
		"product-manager":  productManagerToolPolicy(),
	}
	for role, policy := range others {
		t.Run(role, func(t *testing.T) {
			assert.NotContains(t, policy.AllowTools, domain.RollbackReleaseToolName)
		})
	}
}

// The rollback survives in `done` — where the merge, the watch and the rollback
// are the whole reason the run exists — and is stripped from every other verdict
// column. `released` is not a verdict column, so the narrowing never runs there
// and the tool stays, which is what lets the health-window wake use it.
func TestRollbackSurvivesOnlyInDone(t *testing.T) {
	assert.Contains(t, qaRunPolicyIn(domain.TaskColumnDone).AllowTools, domain.RollbackReleaseToolName,
		"the rollback must survive in done: merge → watch → roll back is one sequence")

	for _, column := range []domain.TaskColumn{
		domain.TaskColumnReadyForQA,
		domain.TaskColumnInQA,
		domain.TaskColumnPMUAT,
		domain.TaskColumnCodeReview,
	} {
		t.Run(string(column), func(t *testing.T) {
			assert.NotContains(t, qaRunPolicyIn(column).AllowTools, domain.RollbackReleaseToolName,
				"a run judging a change on stage must not be able to undo a live release")
		})
	}
}

// The two READ tools are not release controls and must not be stripped: a QA
// round that can see a deploy status but not act on it is strictly better than
// one that can see nothing.
func TestDeployWatchReadToolsSurviveEveryColumn(t *testing.T) {
	for _, column := range []domain.TaskColumn{
		domain.TaskColumnReadyForQA,
		domain.TaskColumnInQA,
		domain.TaskColumnDone,
	} {
		t.Run(string(column), func(t *testing.T) {
			allow := qaRunPolicyIn(column).AllowTools
			assert.Contains(t, allow, domain.DeployStatusToolName)
			assert.Contains(t, allow, domain.DeployLogsToolName)
		})
	}
}

// The uplift is what an agent gets for having an unscoped or stale policy. It
// may widen what an agent can SEE for free; widening what it can change in
// production is a decision someone has to have made.
func TestWorkspaceUpliftNeverGrantsTheRollback(t *testing.T) {
	uplifted := domain.UpliftWorkspaceTools(domain.ToolPolicy{AllowTools: []string{"run_terminal"}})
	assert.NotContains(t, uplifted.AllowTools, domain.RollbackReleaseToolName)
	assert.NotContains(t, uplifted.AllowTools, domain.MergePullRequestToolName)
}

// ---------------------------------------------------------------------------
// RestrictCodeToolsForVerification, wired at the same call-site pattern
// board.Runner uses (RestrictToolsForVerdictColumn then
// RestrictCodeToolsForVerification, both keyed on job.Task.Column).

func pmRunPolicyIn(column domain.TaskColumn) domain.ToolPolicy {
	return domain.RestrictCodeToolsForVerification(
		domain.RestrictToolsForVerdictColumn(
			domain.RestrictToolsForTaskType(
				domain.UpliftWorkspaceTools(
					domain.MergeToolPolicy(domain.ToolPolicy{}, productManagerToolPolicy()),
				),
				domain.TaskTypeTask,
			),
			column,
		),
		column,
	)
}

// PM's whole reason to hold browser/mobile tools in pm_uat/human_uat is to
// walk the product itself instead of reading the diff — so those columns must
// take the code-exploration tools away, and only those two.
func TestPMUATRunPolicyLosesCodeExplorationTools(t *testing.T) {
	for _, column := range []domain.TaskColumn{domain.TaskColumnPMUAT, domain.TaskColumnHumanUAT} {
		t.Run(string(column), func(t *testing.T) {
			policy := pmRunPolicyIn(column)
			for _, name := range domain.CodeExplorationTools {
				assert.NotContains(t, policy.AllowTools, name)
			}
			assert.Contains(t, policy.AllowTools, "browser_navigate", "PM still has to walk the product")
			assert.Contains(t, policy.AllowTools, "review_criterion")
		})
	}
}

// Outside pm_uat/human_uat, PM keeps the full set — it still verifies a real
// file or endpoint name before writing technical_description while grooming
// the backlog.
func TestPMKeepsCodeExplorationToolsOutsideUATColumns(t *testing.T) {
	policy := pmRunPolicyIn(domain.TaskColumnTodo)
	for _, name := range domain.CodeExplorationTools {
		assert.Contains(t, policy.AllowTools, name)
	}
}

// QA loses only read_file in its two verdict columns; the tree/diff-level
// tools its three named exceptions actually need survive.
func TestQARunPolicyLosesOnlyReadFileInVerdictColumns(t *testing.T) {
	for _, column := range []domain.TaskColumn{domain.TaskColumnInQA, domain.TaskColumnReadyForQA} {
		t.Run(string(column), func(t *testing.T) {
			policy := domain.RestrictCodeToolsForVerification(qaRunPolicyIn(column), column)
			assert.NotContains(t, policy.AllowTools, "read_file")
			assert.Contains(t, policy.AllowTools, "get_repo_tree")
			assert.Contains(t, policy.AllowTools, "grep_code")
			assert.Contains(t, policy.AllowTools, "get_task_pull_request")
		})
	}
}
