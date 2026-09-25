package catalog

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/workflow/workflowtest"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// Real migration-143 fixture rather than a hand-built stage, so these tests pin the exact behaviour combinations it seeds.
func stageFor(t *testing.T, taskType domain.TaskType, column domain.TaskColumn) (domain.WorkflowStage, domain.TaskTypeDef) {
	t.Helper()
	wf, ok := workflowtest.Default().Workflows[taskType]
	if !ok {
		t.Fatalf("workflowtest.Default() has no workflow for type %q", taskType)
	}
	stage, ok := wf.Stage(column)
	if !ok {
		t.Fatalf("workflow %q has no stage for column %s", taskType, column)
	}
	return stage, wf.Type
}

// Mirrors board.Runner's upliftedPolicy (runner.go) so the tests exercise the real pipeline.
func qaRunPolicyIn(t *testing.T, column domain.TaskColumn) domain.ToolPolicy {
	t.Helper()
	stage, typeDef := stageFor(t, "task", column)
	return domain.RestrictToolsForStage(
		domain.UpliftWorkspaceTools(
			domain.MergeToolPolicy(domain.ToolPolicy{}, qaToolPolicy()),
		),
		stage, typeDef,
	)
}

func TestQARunPolicyKeepsTheVerdictTools(t *testing.T) {
	for _, column := range []domain.TaskColumn{
		domain.TaskColumnReadyForQA,
		domain.TaskColumnInQA,
	} {
		t.Run(string(column), func(t *testing.T) {
			policy := qaRunPolicyIn(t, column)
			require.NotEmpty(t, policy.AllowTools)

			allowed := func(name string) bool { return domain.ToolAllowedByPolicy(name, policy) }
			assert.True(t, allowed("list_acceptance_criteria"))
			assert.True(t, allowed("review_criterion"))
			assert.True(t, allowed("move_board_task"))
			assert.True(t, allowed("add_task_comment"))
			assert.True(t, allowed("run_terminal"))

			assert.False(t, allowed("write_file"))
			assert.False(t, allowed("edit_file"))
			assert.False(t, allowed("commit_task_changes"))
			assert.False(t, allowed(domain.MergePullRequestToolName))
		})
	}
}

// QA's work ends with its verdict in in_qa: it never merges, deploys or rolls back again.
func TestQAPolicyDropsMergeAndReleaseTools(t *testing.T) {
	policy := qaToolPolicy()
	for _, name := range []string{
		domain.MergePullRequestToolName,
		domain.DeployLogsToolName,
		domain.GetReleaseToolName,
		domain.DeployReleaseToolName,
		domain.WatchReleaseToolName,
		domain.FinishReleaseToolName,
		domain.ReleaseRollbackToolName,
	} {
		assert.NotContains(t, policy.AllowTools, name)
	}
}

func TestOnlyReleaseEngineerHoldsTheMergeTool(t *testing.T) {
	for name, policy := range map[string]domain.ToolPolicy{
		"developer":        developerToolPolicy(),
		"mobile-developer": mobileDeveloperToolPolicy(),
		"system-architect": architectToolPolicy(),
		"product-manager":  productManagerToolPolicy(),
		"qa-agent":         qaToolPolicy(),
	} {
		t.Run(name, func(t *testing.T) {
			assert.NotContains(t, policy.AllowTools, domain.MergePullRequestToolName)
		})
	}
	assert.Contains(t, releaseEngineerToolPolicy().AllowTools, domain.MergePullRequestToolName)
}

func TestWorkspaceUpliftDoesNotGrantTheMergeTool(t *testing.T) {
	uplifted := domain.UpliftWorkspaceTools(domain.ToolPolicy{AllowTools: []string{"run_terminal"}})

	assert.False(t, domain.ToolAllowedByPolicy(domain.MergePullRequestToolName, uplifted))
	assert.True(t, domain.ToolAllowedByPolicy("commit_task_changes", uplifted))
}

func TestReleaseEngineerHoldsTheReleaseTools(t *testing.T) {
	policy := releaseEngineerToolPolicy()
	for _, name := range []string{
		domain.GetReleaseToolName,
		domain.DeployReleaseToolName,
		domain.WatchReleaseToolName,
		domain.RunSmokeChecksToolName,
		domain.FinishReleaseToolName,
		domain.ReleaseRollbackToolName,
		domain.DeployLogsToolName,
		"list_incidents",
		"get_incident",
	} {
		assert.Contains(t, policy.AllowTools, name)
	}
	// No workspace writers, no commit tool: it ships and reverts through the release tools, never by editing.
	for _, name := range append([]string{"commit_task_changes"}, domain.WorkspaceWriteTools...) {
		assert.NotContains(t, policy.AllowTools, name)
	}
}

func TestOnlyReleaseEngineerHoldsTheRollback(t *testing.T) {
	others := map[string]domain.ToolPolicy{
		"developer":        developerToolPolicy(),
		"mobile-developer": mobileDeveloperToolPolicy(),
		"architect":        architectToolPolicy(),
		"product-manager":  productManagerToolPolicy(),
		"qa-agent":         qaToolPolicy(),
	}
	for role, policy := range others {
		t.Run(role, func(t *testing.T) {
			assert.NotContains(t, policy.AllowTools, domain.ReleaseRollbackToolName)
		})
	}
	assert.Contains(t, releaseEngineerToolPolicy().AllowTools, domain.ReleaseRollbackToolName)
}

func TestWorkspaceUpliftNeverGrantsTheRollback(t *testing.T) {
	uplifted := domain.UpliftWorkspaceTools(domain.ToolPolicy{AllowTools: []string{"run_terminal"}})
	assert.NotContains(t, uplifted.AllowTools, domain.ReleaseRollbackToolName)
	assert.NotContains(t, uplifted.AllowTools, domain.MergePullRequestToolName)
}

func pmRunPolicyIn(t *testing.T, column domain.TaskColumn) domain.ToolPolicy {
	t.Helper()
	stage, typeDef := stageFor(t, "task", column)
	return domain.RestrictToolsForStage(
		domain.UpliftWorkspaceTools(
			domain.MergeToolPolicy(domain.ToolPolicy{}, productManagerToolPolicy()),
		),
		stage, typeDef,
	)
}

func TestPMUATRunPolicyLosesCodeExplorationTools(t *testing.T) {
	for _, column := range []domain.TaskColumn{domain.TaskColumnPMUAT, domain.TaskColumnHumanUAT} {
		t.Run(string(column), func(t *testing.T) {
			policy := pmRunPolicyIn(t, column)
			for _, name := range domain.CodeExplorationTools {
				assert.NotContains(t, policy.AllowTools, name)
			}
			assert.Contains(t, policy.AllowTools, "browser_navigate")
			assert.Contains(t, policy.AllowTools, "review_criterion")
		})
	}
}

func TestPMKeepsCodeExplorationToolsOutsideUATColumns(t *testing.T) {
	policy := pmRunPolicyIn(t, domain.TaskColumnTodo)
	for _, name := range domain.CodeExplorationTools {
		assert.Contains(t, policy.AllowTools, name)
	}
}

func TestQARunPolicyKeepsItsCodeToolsInVerdictColumns(t *testing.T) {
	for _, column := range []domain.TaskColumn{domain.TaskColumnInQA, domain.TaskColumnReadyForQA} {
		t.Run(string(column), func(t *testing.T) {
			policy := qaRunPolicyIn(t, column)
			assert.Contains(t, policy.AllowTools, "read_file")
			assert.Contains(t, policy.AllowTools, "get_repo_tree")
			assert.Contains(t, policy.AllowTools, "grep_code")
			assert.Contains(t, policy.AllowTools, "get_task_pull_request")
		})
	}
}
