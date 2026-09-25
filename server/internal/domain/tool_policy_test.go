package domain_test

import (
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/application/workflow/workflowtest"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/stretchr/testify/assert"
)

func TestMissingAnalizTools_UnrestrictedPolicyHasNoneMissing(t *testing.T) {
	got := domain.MissingAnalizTools(domain.ToolPolicy{})
	assert.Empty(t, got)
}

// Developer-shaped policy, minus roleBoardCreateTools — the gap that left an
// analiz task stuck with no way to open the implementation tasks the approval
// decomposes into.
func TestMissingAnalizTools_DeveloperShapedPolicyIsMissingBoardCreateTools(t *testing.T) {
	p := domain.ToolPolicy{AllowTools: []string{
		"run_terminal", "write_file", "edit_file", "edit_lines", "delete_file", "move_file", "download_file",
		"web_search", "fetch_url",
		"codebase_search", "grep_code", "get_repo_tree", "get_symbol_skeleton", "expand_symbol_context", "read_file",
		"browser_navigate", "browser_screenshot", "browser_click", "browser_fill", "browser_read_dom", "browser_wait_for", "browser_set_viewport",
		"list_board_tasks", "move_board_task", "update_board_task", "add_task_comment", "list_task_comments",
		"list_task_documents", "list_acceptance_criteria", "set_criterion_completed", "cancel_criterion",
		"list_test_cases", "list_projects", "list_repositories", "get_board_summary", "list_team",
		"claim_board_task",
		"save_memory", "search_memory", "delete_memory",
		"load_skill", "create_skill",
		"get_task_pull_request", "comment_on_pull_request", "commit_task_changes",
	}}
	got := domain.MissingAnalizTools(p)
	assert.ElementsMatch(t, []string{"add_task_document", "update_task_document", "create_board_task"}, got)
}

// Architect-shaped policy already holds every required tool, so routing an
// analiz task to it (the default) never asks for a tool-grant confirmation.
func TestMissingAnalizTools_ArchitectShapedPolicyHasNoneMissing(t *testing.T) {
	p := domain.ToolPolicy{AllowTools: []string{
		"run_terminal", "write_file", "edit_file", "edit_lines", "delete_file", "move_file", "download_file",
		"web_search", "fetch_url",
		"codebase_search", "grep_code", "get_repo_tree", "get_symbol_skeleton", "expand_symbol_context", "read_file",
		"list_board_tasks", "move_board_task", "update_board_task", "add_task_comment", "list_task_comments",
		"list_task_documents", "list_acceptance_criteria", "set_criterion_completed", "cancel_criterion",
		"list_test_cases", "list_projects", "list_repositories", "get_board_summary", "list_team",
		"create_board_task", "add_task_document", "update_task_document", "attach_task_file",
		"claim_board_task",
		"get_pipeline_status",
		"get_task_pull_request", "comment_on_pull_request",
		"save_memory", "search_memory", "delete_memory",
		"load_skill", "create_skill",
	}}
	got := domain.MissingAnalizTools(p)
	assert.Empty(t, got)
}

func TestIntersectToolPolicy_EmptyToolNames(t *testing.T) {
	base := domain.ToolPolicy{AllowTools: []string{"web_search", "run_terminal"}}
	got := domain.IntersectToolPolicy(base, nil)
	assert.Equal(t, base, got)
}

func TestIntersectToolPolicy_IntersectsWithBase(t *testing.T) {
	base := domain.ToolPolicy{AllowTools: []string{"web_search", "run_terminal"}}
	got := domain.IntersectToolPolicy(base, []string{"web_search", "fetch_url"})
	assert.Equal(t, []string{"web_search"}, got.AllowTools)
}

func TestIntersectToolPolicy_ZeroBase(t *testing.T) {
	got := domain.IntersectToolPolicy(domain.ToolPolicy{}, []string{"run_terminal"})
	assert.Equal(t, []string{"run_terminal"}, got.AllowTools)
}

// The request body must never be able to widen a server-side allowlist: an API
// key scoped to read-only tools asking for run_terminal gets nothing back.
func TestIntersectToolPolicy_CannotWidenBase(t *testing.T) {
	base := domain.ToolPolicy{AllowTools: []string{"web_search", "codebase_search"}}
	got := domain.IntersectToolPolicy(base, []string{"run_terminal"})
	assert.Empty(t, got.AllowTools)
}

// A short declaration must never cost an agent its eyes.
func TestRestrictToPlannedTools_BoardOnlyDeclarationKeepsCodeTools(t *testing.T) {
	base := domain.ToolPolicy{AllowTools: []string{
		"run_terminal", "codebase_search", "grep_code", "get_repo_tree",
		"claim_board_task", "move_board_task", "create_board_task", "add_task_comment", "load_skill",
	}}

	got := domain.RestrictToPlannedTools(base, []string{"claim_board_task", "move_board_task", "add_task_comment"})

	assert.Contains(t, got.AllowTools, "codebase_search")
	assert.Contains(t, got.AllowTools, "grep_code")
	assert.Contains(t, got.AllowTools, "run_terminal")
	assert.Contains(t, got.AllowTools, "load_skill")
	assert.Contains(t, got.AllowTools, "move_board_task", "a declared board write stays granted")
	assert.Contains(t, got.AllowTools, domain.AskUserToolName)
}

// Undeclared board writes stay withheld, so two concurrent subtasks cannot open
// the same record.
func TestRestrictToPlannedTools_WithholdsUndeclaredBoardWrites(t *testing.T) {
	base := domain.ToolPolicy{AllowTools: []string{
		"grep_code", "create_board_task", "move_board_task", "update_board_task", "add_task_comment",
	}}

	got := domain.RestrictToPlannedTools(base, []string{"add_task_comment"})

	assert.NotContains(t, got.AllowTools, "create_board_task")
	assert.NotContains(t, got.AllowTools, "move_board_task")
	assert.NotContains(t, got.AllowTools, "update_board_task")
	assert.Contains(t, got.AllowTools, "grep_code")
}

func TestRestrictToPlannedTools_CannotWidenBase(t *testing.T) {
	base := domain.ToolPolicy{AllowTools: []string{"grep_code"}}

	got := domain.RestrictToPlannedTools(base, []string{"run_terminal", "create_board_task"})

	assert.NotContains(t, got.AllowTools, "run_terminal")
	assert.NotContains(t, got.AllowTools, "create_board_task")
}

func TestRestrictToPlannedTools_EmptyDeclarationInheritsAgent(t *testing.T) {
	base := domain.ToolPolicy{AllowTools: []string{"grep_code", "create_board_task"}}

	assert.Equal(t, base, domain.RestrictToPlannedTools(base, nil))
}

// An unrestricted agent has no configured list to preserve, so the declaration
// stands — plus the read tools, so it can still answer its own questions.
func TestRestrictToPlannedTools_UnrestrictedBaseKeepsReadTools(t *testing.T) {
	got := domain.RestrictToPlannedTools(domain.ToolPolicy{}, []string{"move_board_task"})

	assert.Contains(t, got.AllowTools, "move_board_task")
	for _, name := range domain.CodeExplorationTools {
		assert.Contains(t, got.AllowTools, name)
	}
}

func TestIntersectMCPServers_NarrowsOnly(t *testing.T) {
	base := domain.ToolPolicy{AllowMCPServers: []string{"filesystem", "github"}}

	assert.Equal(t, []string{"github"},
		domain.IntersectMCPServers(base, []string{"github", "postgres"}).AllowMCPServers,
		"a server the base does not allow must be dropped")

	assert.Equal(t, base, domain.IntersectMCPServers(base, nil),
		"no requested servers leaves the base untouched")

	assert.Equal(t, []string{"postgres"},
		domain.IntersectMCPServers(domain.ToolPolicy{}, []string{"postgres"}).AllowMCPServers,
		"an unrestricted base accepts the requested set")
}

// A role agent's persisted allowlist is written once at creation and never
// reconciled, so an install seeded before the code tools were added to a role
// keeps a policy without them. The uplift is the runtime net for exactly that.
func TestUpliftWorkspaceTools_BoardScopedAgentKeepsCodeExplorationTools(t *testing.T) {
	stale := domain.ToolPolicy{AllowTools: []string{
		"list_board_tasks", "move_board_task", "add_task_comment",
	}}

	got := domain.UpliftWorkspaceTools(stale)

	for _, tool := range domain.CodeExplorationTools {
		assert.Contains(t, got.AllowTools, tool)
	}
}

// Read-only is the whole justification for granting these without asking: a
// board-scoped agent still does not get the shell or filesystem writes its
// policy withholds.
func TestUpliftWorkspaceTools_BoardScopedAgentGetsNoWriteTools(t *testing.T) {
	stale := domain.ToolPolicy{AllowTools: []string{"list_board_tasks", "add_task_comment"}}

	got := domain.UpliftWorkspaceTools(stale)

	assert.NotContains(t, got.AllowTools, "run_terminal")
	assert.NotContains(t, got.AllowTools, "mcp_filesystem_*")
	// The file writers ride with the shell, not with the read-only uplift, so
	// an agent kept away from `sed -i` must not get edit_file through the back
	// door.
	for _, tool := range domain.WorkspaceWriteTools {
		assert.NotContains(t, got.AllowTools, tool)
	}
}

// An agent that holds the shell already writes files; reading a result that
// says what changed is what keeps a run from grepping to check.
func TestUpliftWorkspaceTools_ShellAgentGetsTheFileWriters(t *testing.T) {
	policy := domain.ToolPolicy{AllowTools: []string{"run_terminal", "grep_code"}}

	got := domain.UpliftWorkspaceTools(policy)

	assert.Contains(t, got.AllowTools, "read_file")
	for _, tool := range domain.WorkspaceWriteTools {
		assert.Contains(t, got.AllowTools, tool)
	}
}

func TestUpliftWorkspaceTools_NonBoardAgentStillGetsShell(t *testing.T) {
	got := domain.UpliftWorkspaceTools(domain.ToolPolicy{AllowTools: []string{"web_search"}})

	assert.Contains(t, got.AllowTools, "run_terminal")
	assert.Contains(t, got.AllowTools, "codebase_search")
}

// An unrestricted policy already permits everything; materialising a list would
// narrow it.
func TestUpliftWorkspaceTools_UnrestrictedPolicyUntouched(t *testing.T) {
	assert.Empty(t, domain.UpliftWorkspaceTools(domain.ToolPolicy{}).AllowTools)
}

func TestDiffNeedsUIEvidence(t *testing.T) {
	cases := []struct {
		name  string
		paths []string
		want  bool
	}{
		{"nil paths require evidence", nil, true},
		{"empty paths require evidence", []string{}, true},
		{
			"docs and scripts tooling dirs are exempt",
			[]string{".ai/coding-standards.md", ".ai/test-standards.md", ".ai/architecture.md", "scripts/dev.sh"},
			false,
		},
		{"top-level readme is exempt", []string{"README.md"}, false},
		{"go module files are exempt", []string{"go.mod", "go.sum"}, false},
		{"a tsx component requires evidence", []string{"src/components/Button.tsx"}, true},
		{
			"one ui file among exempt ones still requires evidence",
			[]string{"docs/setup.md", "src/App.tsx"},
			true,
		},
		{"unrecognized extension defaults to requiring evidence", []string{"locales/en.json"}, true},
		{"github workflow dir is exempt", []string{".github/workflows/ci.yml"}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, domain.DiffNeedsUIEvidence(tc.paths))
		})
	}
}

func fullCodeToolsPolicy() domain.ToolPolicy {
	return domain.ToolPolicy{AllowTools: []string{
		"codebase_search", "grep_code", "get_repo_tree", "get_symbol_skeleton", "expand_symbol_context", "read_file",
		"browser_navigate", "run_terminal", "get_task_pull_request", "move_board_task",
	}}
}

// wfStage looks up one stage of the named workflowtest task type — a real
// migration-143 fixture rather than a hand-built WorkflowStage.
func wfStage(t *testing.T, taskType domain.TaskType, col domain.TaskColumn) (domain.WorkflowStage, domain.TaskTypeDef) {
	t.Helper()
	wf, ok := workflowtest.Default().Workflows[taskType]
	if !ok {
		t.Fatalf("workflowtest.Default() has no workflow for type %q", taskType)
	}
	stage, ok := wf.Stage(col)
	if !ok {
		t.Fatalf("workflow %q has no stage for column %s", taskType, col)
	}
	return stage, wf.Type
}

// The architect held the file writers for its other columns, and on an analiz
// task (no_workspace_writes, a TYPE behaviour) it used them on a run whose
// deliverable was never committed.
func TestRestrictToolsForStage_TypeNoWorkspaceWritesStripsFileWriters(t *testing.T) {
	stage, typeDef := wfStage(t, "analiz", domain.TaskColumnInProgress)
	policy := domain.ToolPolicy{AllowTools: append([]string{
		"run_terminal", "grep_code", "add_task_document", "move_board_task",
	}, domain.WorkspaceWriteTools...)}

	got := domain.RestrictToolsForStage(policy, stage, typeDef)

	for _, tool := range domain.WorkspaceWriteTools {
		assert.NotContains(t, got.AllowTools, tool)
	}
	assert.Contains(t, got.AllowTools, "run_terminal")
	assert.Contains(t, got.AllowTools, "grep_code")
	assert.Contains(t, got.AllowTools, "add_task_document")
}

func TestRestrictToolsForStage_ImplementationStagesKeepEverything(t *testing.T) {
	policy := domain.ToolPolicy{AllowTools: append([]string{"run_terminal"}, domain.WorkspaceWriteTools...)}

	for _, typeKey := range []domain.TaskType{"task", "bug", "technical"} {
		stage, typeDef := wfStage(t, typeKey, domain.TaskColumnInProgress)
		got := domain.RestrictToolsForStage(policy, stage, typeDef)
		for _, tool := range domain.WorkspaceWriteTools {
			assert.Contains(t, got.AllowTools, tool, "task type %q lost %s", typeKey, tool)
		}
	}
}

// code_review carries strip_writers with no "allow" — every writer, the commit
// and the merge tool must go.
func TestRestrictToolsForStage_StripWritersOnCodeReview(t *testing.T) {
	stage, typeDef := wfStage(t, "task", domain.TaskColumnCodeReview)
	policy := domain.ToolPolicy{AllowTools: append([]string{
		"run_terminal", "commit_task_changes", domain.MergePullRequestToolName, domain.ReleaseRollbackToolName, "get_task_pull_request",
	}, domain.WorkspaceWriteTools...)}

	got := domain.RestrictToolsForStage(policy, stage, typeDef)

	for _, tool := range domain.WorkspaceWriteTools {
		assert.NotContains(t, got.AllowTools, tool)
	}
	assert.NotContains(t, got.AllowTools, "commit_task_changes")
	assert.NotContains(t, got.AllowTools, domain.MergePullRequestToolName, "a reviewer must not be able to land the change it is judging")
	assert.NotContains(t, got.AllowTools, domain.ReleaseRollbackToolName)
	assert.Contains(t, got.AllowTools, "run_terminal")
	assert.Contains(t, got.AllowTools, "get_task_pull_request")
}

// done's strip_writers carries allow=merge_task_pull_request,release_control —
// the one column where landing the change and acting on its release are the
// point of the run.
func TestRestrictToolsForStage_DoneAllowsMergeAndReleaseControl(t *testing.T) {
	stage, typeDef := wfStage(t, "task", domain.TaskColumnDone)
	policy := domain.ToolPolicy{AllowTools: append([]string{
		"run_terminal", "commit_task_changes", domain.MergePullRequestToolName, domain.ReleaseRollbackToolName,
	}, domain.WorkspaceWriteTools...)}

	got := domain.RestrictToolsForStage(policy, stage, typeDef)

	for _, tool := range domain.WorkspaceWriteTools {
		assert.NotContains(t, got.AllowTools, tool, "done never writes to the workspace")
	}
	assert.NotContains(t, got.AllowTools, "commit_task_changes")
	assert.Contains(t, got.AllowTools, domain.MergePullRequestToolName, "done is the one column dispatched to land the change")
	assert.Contains(t, got.AllowTools, domain.ReleaseRollbackToolName, "done is on the merge -> watch -> rollback sequence")
}

// pm_uat/human_uat carry no_code_reading: PM's verdict must come from the
// running product, never from reading the implementation.
func TestRestrictToolsForStage_NoCodeReadingOnPMUATAndHumanUAT(t *testing.T) {
	for _, col := range []domain.TaskColumn{domain.TaskColumnPMUAT, domain.TaskColumnHumanUAT} {
		t.Run(string(col), func(t *testing.T) {
			stage, typeDef := wfStage(t, "task", col)
			got := domain.RestrictToolsForStage(fullCodeToolsPolicy(), stage, typeDef)
			for _, name := range domain.CodeExplorationTools {
				assert.NotContains(t, got.AllowTools, name)
			}
			assert.Contains(t, got.AllowTools, "browser_navigate")
			assert.Contains(t, got.AllowTools, "run_terminal")
		})
	}
}

// ready_for_qa/in_qa carry no_read_file only: QA keeps the tree/diff-level code
// tools, it just loses read_file.
// QA keeps its code tools. read_file used to be stripped here, which enforced
// nothing — grep_code and expand_symbol_context return the same source — so
// the restriction is a line in the QA stage's instructions now, not a flag.
func TestRestrictToolsForStage_QAKeepsItsCodeTools(t *testing.T) {
	for _, col := range []domain.TaskColumn{domain.TaskColumnReadyForQA, domain.TaskColumnInQA} {
		t.Run(string(col), func(t *testing.T) {
			stage, typeDef := wfStage(t, "task", col)
			got := domain.RestrictToolsForStage(fullCodeToolsPolicy(), stage, typeDef)
			assert.Contains(t, got.AllowTools, "read_file")
			assert.Contains(t, got.AllowTools, "get_repo_tree")
			assert.Contains(t, got.AllowTools, "grep_code")
			assert.Contains(t, got.AllowTools, "get_task_pull_request")
		})
	}
}

// Columns with no restricting behaviour at all (todo, in_progress on a coding
// type) keep every code tool.
func TestRestrictToolsForStage_OtherStagesKeepAllCodeTools(t *testing.T) {
	for _, col := range []domain.TaskColumn{domain.TaskColumnTodo, domain.TaskColumnInProgress} {
		t.Run(string(col), func(t *testing.T) {
			stage, typeDef := wfStage(t, "task", col)
			got := domain.RestrictToolsForStage(fullCodeToolsPolicy(), stage, typeDef)
			for _, name := range domain.CodeExplorationTools {
				assert.Contains(t, got.AllowTools, name)
			}
		})
	}
}

func TestRestrictToolsForStage_UnrestrictedPolicyUntouched(t *testing.T) {
	stage, typeDef := wfStage(t, "analiz", domain.TaskColumnTodo)
	got := domain.RestrictToolsForStage(domain.ToolPolicy{}, stage, typeDef)
	assert.Empty(t, got.AllowTools)
}
