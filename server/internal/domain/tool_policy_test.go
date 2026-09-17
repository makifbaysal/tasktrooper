package domain_test

import (
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/stretchr/testify/assert"
)

func TestMissingAnalizTools_UnrestrictedPolicyHasNoneMissing(t *testing.T) {
	got := domain.MissingAnalizTools(domain.ToolPolicy{})
	assert.Empty(t, got)
}

// Shaped like the developer role policy (roleShellTools + roleWebTools +
// roleCodeTools + roleBrowserTools + roleBoardReadTools + roleBoardClaimTools
// + roleMemoryTools + roleSkillTools + roleProfileTools + rolePRReadTools +
// rolePRReplyTools + rolePRCommitTools), minus roleBoardCreateTools — the
// gap that let a PM assign an analiz task to backend-developer and leave it
// stuck with no way to open the implementation tasks the approval decomposes
// into.
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
		"update_project_profile",
		"get_task_pull_request", "comment_on_pull_request", "commit_task_changes",
	}}
	got := domain.MissingAnalizTools(p)
	assert.ElementsMatch(t, []string{"add_task_document", "update_task_document", "create_board_task"}, got)
}

// Shaped like the architect role policy (roleShellTools + roleWebTools +
// roleCodeTools + roleBoardReadTools + roleBoardCreateTools +
// roleBoardClaimTools + get_pipeline_status + rolePRReadTools +
// rolePRReplyTools + roleMemoryTools + roleSkillTools + roleProfileTools):
// the architect already holds every required tool, so routing an analiz task
// to it (the default) never asks for a tool-grant confirmation.
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
		"update_project_profile",
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

// The failure this function exists for: a subtask told to change a website
// declared only board tools, the executor intersected its agent down to them,
// and the run asked the human where the files were because it had no way to
// look. A short declaration must never cost an agent its eyes.
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

// The one thing the declaration still decides: an undeclared board write is
// withheld, so two concurrent subtasks cannot open the same record.
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

// A role agent's persisted allowlist is written once, when the agent row is
// created, and never reconciled afterwards — so an install seeded before the
// code tools were added to a role keeps a policy without them. The uplift is
// the runtime net for exactly that, but it skipped every code tool as soon as
// the agent held any board tool: the system-architect dispatched into
// code_review had no way to read the diff it was there to review, and said so
// in a task comment instead ("I don't have the necessary tools").
func TestUpliftWorkspaceTools_BoardScopedAgentKeepsCodeExplorationTools(t *testing.T) {
	stale := domain.ToolPolicy{AllowTools: []string{
		"list_board_tasks", "move_board_task", "add_task_comment",
	}}

	got := domain.UpliftWorkspaceTools(stale)

	for _, tool := range domain.CodeExplorationTools {
		assert.Contains(t, got.AllowTools, tool)
	}
}

// Read-only is the whole justification for granting these without the operator
// asking. A board-scoped agent still does not get the shell or filesystem
// writes its policy withholds.
func TestUpliftWorkspaceTools_BoardScopedAgentGetsNoWriteTools(t *testing.T) {
	stale := domain.ToolPolicy{AllowTools: []string{"list_board_tasks", "add_task_comment"}}

	got := domain.UpliftWorkspaceTools(stale)

	assert.NotContains(t, got.AllowTools, "run_terminal")
	assert.NotContains(t, got.AllowTools, "mcp_filesystem_*")
	// The file writers ride with the shell, not with the read-only uplift: an
	// agent kept away from `sed -i` must not get edit_file through the back door.
	for _, tool := range domain.WorkspaceWriteTools {
		assert.NotContains(t, got.AllowTools, tool)
	}
}

// An agent that holds the shell already writes files; the file tools write the
// same things, confined to the workspace, and reading a result that says what
// changed is what keeps a run from grepping to check.
func TestUpliftWorkspaceTools_ShellAgentGetsTheFileWriters(t *testing.T) {
	policy := domain.ToolPolicy{AllowTools: []string{"run_terminal", "grep_code"}}

	got := domain.UpliftWorkspaceTools(policy)

	assert.Contains(t, got.AllowTools, "read_file")
	for _, tool := range domain.WorkspaceWriteTools {
		assert.Contains(t, got.AllowTools, tool)
	}
}

// An agent with no board tools keeps the full workspace uplift, shell included.
func TestUpliftWorkspaceTools_NonBoardAgentStillGetsShell(t *testing.T) {
	got := domain.UpliftWorkspaceTools(domain.ToolPolicy{AllowTools: []string{"web_search"}})

	assert.Contains(t, got.AllowTools, "run_terminal")
	assert.Contains(t, got.AllowTools, "codebase_search")
}

// An unrestricted policy (no allowlist at all) already permits everything;
// materialising a list would narrow it.
func TestUpliftWorkspaceTools_UnrestrictedPolicyUntouched(t *testing.T) {
	assert.Empty(t, domain.UpliftWorkspaceTools(domain.ToolPolicy{}).AllowTools)
}

// The architect holds the file writers for its other columns, and on an analiz
// task it used them: three edit_file calls and a delete_file on a task whose
// deliverable was a spec. The run is never committed, so the edits went nowhere
// and the analysis was never written.
func TestRestrictToolsForTaskType_AnalizLosesTheFileWriters(t *testing.T) {
	policy := domain.ToolPolicy{AllowTools: append([]string{
		"run_terminal", "grep_code", "add_task_document", "move_board_task",
	}, domain.WorkspaceWriteTools...)}

	got := domain.RestrictToolsForTaskType(policy, domain.TaskTypeAnaliz)

	for _, tool := range domain.WorkspaceWriteTools {
		assert.NotContains(t, got.AllowTools, tool)
	}
	// Reading the repo, cloning it and attaching the documents is the whole job.
	assert.Contains(t, got.AllowTools, "run_terminal")
	assert.Contains(t, got.AllowTools, "grep_code")
	assert.Contains(t, got.AllowTools, "add_task_document")
}

func TestRestrictToolsForTaskType_ImplementationTasksKeepEverything(t *testing.T) {
	policy := domain.ToolPolicy{AllowTools: append([]string{"run_terminal"}, domain.WorkspaceWriteTools...)}

	for _, typ := range []domain.TaskType{domain.TaskTypeTask, domain.TaskTypeBug, ""} {
		got := domain.RestrictToolsForTaskType(policy, typ)
		for _, tool := range domain.WorkspaceWriteTools {
			assert.Contains(t, got.AllowTools, tool, "task type %q lost %s", typ, tool)
		}
	}
}

// No allowlist means the operator left the agent unscoped; there is no deny list
// to express a narrowing in, and materialising one here would widen nothing but
// silently narrow everything else.
func TestRestrictToolsForTaskType_UnrestrictedPolicyUntouched(t *testing.T) {
	assert.Empty(t, domain.RestrictToolsForTaskType(domain.ToolPolicy{}, domain.TaskTypeAnaliz).AllowTools)
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

func TestRestrictCodeToolsForVerification_PMUATStripsAllCodeTools(t *testing.T) {
	for _, column := range []domain.TaskColumn{domain.TaskColumnPMUAT, domain.TaskColumnHumanUAT} {
		t.Run(string(column), func(t *testing.T) {
			got := domain.RestrictCodeToolsForVerification(fullCodeToolsPolicy(), column)
			for _, name := range domain.CodeExplorationTools {
				assert.NotContains(t, got.AllowTools, name)
			}
			assert.Contains(t, got.AllowTools, "browser_navigate")
			assert.Contains(t, got.AllowTools, "run_terminal")
		})
	}
}

func TestRestrictCodeToolsForVerification_OtherColumnsKeepAllCodeTools(t *testing.T) {
	for _, column := range []domain.TaskColumn{domain.TaskColumnTodo, domain.TaskColumnInProgress, domain.TaskColumnCodeReview} {
		t.Run(string(column), func(t *testing.T) {
			got := domain.RestrictCodeToolsForVerification(fullCodeToolsPolicy(), column)
			for _, name := range domain.CodeExplorationTools {
				assert.Contains(t, got.AllowTools, name)
			}
		})
	}
}

func TestRestrictCodeToolsForVerification_QAColumnsStripOnlyReadFile(t *testing.T) {
	for _, column := range []domain.TaskColumn{domain.TaskColumnInQA, domain.TaskColumnReadyForQA} {
		t.Run(string(column), func(t *testing.T) {
			got := domain.RestrictCodeToolsForVerification(fullCodeToolsPolicy(), column)
			assert.NotContains(t, got.AllowTools, "read_file")
			assert.Contains(t, got.AllowTools, "get_repo_tree")
			assert.Contains(t, got.AllowTools, "grep_code")
			assert.Contains(t, got.AllowTools, "get_task_pull_request")
		})
	}
}

func TestRestrictCodeToolsForVerification_UnrestrictedPolicyUntouched(t *testing.T) {
	got := domain.RestrictCodeToolsForVerification(domain.ToolPolicy{}, domain.TaskColumnPMUAT)
	assert.Empty(t, got.AllowTools)
}
