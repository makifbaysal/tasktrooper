package catalog

import (
	"slices"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

var (
	roleCodeTools = []string{
		"codebase_search",
		"grep_code",
		"get_repo_tree",
		"get_symbol_skeleton",
		"expand_symbol_context",
		"read_file",
	}
	roleWebTools = []string{
		"web_search",
		"fetch_url",
	}
	// Verbatim contract with the browser adapter.
	roleBrowserTools = []string{
		"browser_navigate",
		"browser_screenshot",
		"browser_click",
		"browser_fill",
		"browser_read_dom",
		"browser_wait_for",
		"browser_set_viewport",
	}
	// Verbatim contract with the mobile adapter; no tool is registered unless a device is attached.
	roleMobileTools = []string{
		"mobile_launch_app",
		"mobile_screenshot",
		"mobile_read_ui",
		"mobile_tap",
		"mobile_type_text",
		"mobile_swipe",
		"mobile_wait_for",
		"mobile_press_button",
		"mobile_rotate",
		"mobile_unlock_device",
		"mobile_release_device",
	}
	// An agent that may run `sed -i` may call edit_file, and one that may not, may not either.
	roleShellTools = append([]string{
		"run_terminal",
	}, domain.WorkspaceWriteTools...)
	// QA is black-box: read just enough to find the start command, the port and the route.
	// QA once held the full roleCodeTools set and used it to read the diff instead of exercising the product.
	roleQALookupTools = []string{
		"get_repo_tree",
		"grep_code",
		"read_file",
	}
	roleBoardReadTools = []string{
		"list_board_tasks",
		// Which backlog/todo rows are actually startable, so picking a task does not re-derive BlockedBy.
		"list_ready_tasks",
		"move_board_task",
		"update_board_task",
		"add_task_comment",
		"list_task_comments",
		// Since specs stopped being committed, the spec and plan live only as task documents.
		"list_task_documents",
		"list_acceptance_criteria",
		"set_criterion_completed",
		// A deliberately cancelled criterion leaves the list by saying why instead of parking the task.
		"cancel_criterion",
		"list_test_cases",
		"list_projects",
		"list_repositories",
		"get_board_summary",
		"list_team",
	}
	roleBoardClaimTools = []string{
		"claim_board_task",
	}
	roleBoardCreateTools = []string{
		"create_board_task",
		"add_task_document",
		// Paired with add_task_document: the role allowed to attach a spec is the one asked to change it.
		"update_task_document",
		"attach_task_file",
	}
	// PM owns the backlog, so PM owns what leaves it.
	roleBoardDeleteTools = []string{
		"delete_board_task",
	}
	roleWorkspaceManageTools = []string{
		"create_project",
		"update_project",
		"set_repository_projects",
	}
	roleMemoryTools = []string{
		"save_memory",
		"search_memory",
		"delete_memory",
	}
	roleSkillTools = []string{
		"load_skill",
		"create_skill",
	}
	// Read-only project model tools: the brief, CI checks and cross-component
	// links. Granted to every role that writes or reviews code.
	roleProjectModelReadTools = []string{
		"get_project_brief",
		"list_component_checks",
		"list_links",
	}
	// The runtime picture of a bound environment (Phase 2's cloud accounts):
	// where a component runs, its live logs, its grouped errors and its
	// deployments. Full set granted to developers and QA; the architect and
	// PM get a narrower slice (see their own policy functions) since neither
	// debugs a live request the way an implementer or a tester does.
	roleRuntimeReadTools = []string{
		"get_environment",
		"query_runtime_logs",
		"list_runtime_errors",
		"list_deployments",
	}
	// Read-only, so every role that reviews or reports on a task holds it.
	rolePRReadTools = []string{
		"get_task_pull_request",
	}
	// commit_task_changes is implementer-only: a reviewer that could commit would judge its own branch.
	rolePRReplyTools = []string{
		"comment_on_pull_request",
	}
	rolePRCommitTools = []string{
		"commit_task_changes",
	}
	// The release engineer merges, nobody else: the developer must not merge its own branch, and QA's work ends with its verdict.
	rolePRMergeTools = []string{
		domain.MergePullRequestToolName,
	}
	// Read-only, wider than roleBrowserTools: the release engineer never fills a form or clicks a button, only looks.
	roleReleaseBrowserReadTools = []string{
		"browser_navigate",
		"browser_screenshot",
		"browser_read_dom",
		"browser_wait_for",
		"browser_set_viewport",
	}
	roleReleaseTools = []string{
		domain.GetReleaseToolName,
		domain.DeployReleaseToolName,
		domain.WatchReleaseToolName,
		domain.RunSmokeChecksToolName,
		domain.FinishReleaseToolName,
		domain.ReleaseRollbackToolName,
	}
)

func developerToolPolicy() domain.ToolPolicy {
	tools := make([]string, 0, len(roleShellTools)+len(roleWebTools)+len(roleCodeTools)+len(roleBrowserTools)+len(roleBoardReadTools)+len(roleBoardClaimTools)+len(roleMemoryTools)+len(roleSkillTools))
	tools = append(tools, roleShellTools...)
	tools = append(tools, roleWebTools...)
	tools = append(tools, roleCodeTools...)
	// The developer can only claim "green build", not "the screen works": browser access lets it render the change before review, and the browser guard allows its own 127.0.0.1 dev server.
	tools = append(tools, roleBrowserTools...)
	tools = append(tools, roleBoardReadTools...)
	tools = append(tools, roleBoardClaimTools...)
	tools = append(tools, roleMemoryTools...)
	tools = append(tools, roleSkillTools...)
	tools = append(tools, roleProjectModelReadTools...)
	tools = append(tools, roleRuntimeReadTools...)
	tools = append(tools, rolePRReadTools...)
	tools = append(tools, rolePRReplyTools...)
	tools = append(tools, rolePRCommitTools...)
	return domain.ToolPolicy{AllowTools: tools}
}

func productManagerToolPolicy() domain.ToolPolicy {
	tools := make([]string, 0, len(roleWebTools)+len(roleCodeTools)+len(roleBrowserTools)+len(roleBoardReadTools)+len(roleBoardCreateTools)+len(roleBoardDeleteTools)+len(roleWorkspaceManageTools)+len(roleMemoryTools)+len(roleSkillTools)+2)
	tools = append(tools, roleWebTools...)
	// Read-only code tools: the PM verifies real file/endpoint names for technical_description — no shell.
	tools = append(tools, roleCodeTools...)
	// pm_uat: the PM walks the critical flows on stage; get_deploy_target resolves stage base_url.
	tools = append(tools, roleBrowserTools...)
	tools = append(tools, roleMobileTools...)
	// update_deploy_target only writes base_url/health_url when UAT finds the stage address unrecorded.
	tools = append(tools, "get_deploy_target", "update_deploy_target")
	tools = append(tools, roleBoardReadTools...)
	tools = append(tools, roleBoardCreateTools...)
	tools = append(tools, roleBoardDeleteTools...)
	tools = append(tools, roleWorkspaceManageTools...)
	// pm_uat: the PM records its own verdict per criterion.
	tools = append(tools, "review_criterion")
	// The PM answers "what shipped?" from the PR rather than the implementer's summary.
	tools = append(tools, rolePRReadTools...)
	tools = append(tools, "get_project_brief", "list_links")
	// pm_uat: where a stage/production environment actually runs, without the
	// live-debugging tools (query_runtime_logs, list_runtime_errors,
	// list_deployments) a PM never needs.
	tools = append(tools, "get_environment")
	tools = append(tools, roleMemoryTools...)
	tools = append(tools, roleSkillTools...)
	return domain.ToolPolicy{AllowTools: tools}
}

func architectToolPolicy() domain.ToolPolicy {
	tools := make([]string, 0)
	tools = append(tools, roleShellTools...)
	tools = append(tools, roleWebTools...)
	tools = append(tools, roleCodeTools...)
	tools = append(tools, roleBoardReadTools...)
	tools = append(tools, roleBoardCreateTools...)
	tools = append(tools, roleBoardClaimTools...)
	tools = append(tools, "get_pipeline_status")
	// The architect reads the PR it is judging and answers threads; no commit_task_changes.
	tools = append(tools, rolePRReadTools...)
	tools = append(tools, rolePRReplyTools...)
	tools = append(tools, roleMemoryTools...)
	tools = append(tools, roleSkillTools...)
	tools = append(tools, roleProjectModelReadTools...)
	// The two triage tools for judging whether a review's failure is live in
	// production: where it runs, and what it is erroring on. Not the raw log
	// tail (query_runtime_logs) or the deploy history (list_deployments) —
	// those are for the implementer and QA, not the reviewer.
	tools = append(tools, "get_environment", "list_runtime_errors")
	return domain.ToolPolicy{AllowTools: tools}
}

func qaToolPolicy() domain.ToolPolicy {
	tools := make([]string, 0, len(roleShellTools)+len(roleWebTools)+len(roleQALookupTools)+len(roleBrowserTools)+len(roleMobileTools)+len(roleMemoryTools)+len(roleSkillTools)+10)
	tools = append(tools, roleShellTools...)
	tools = append(tools, roleWebTools...)
	tools = append(tools, roleQALookupTools...)
	tools = append(tools, roleBrowserTools...)
	tools = append(tools, roleMobileTools...)
	tools = append(tools, roleProjectModelReadTools...)
	tools = append(tools, roleRuntimeReadTools...)
	tools = append(tools, "list_board_tasks", "list_ready_tasks", "move_board_task", "add_task_comment", "list_task_comments", "list_task_documents", "list_acceptance_criteria", "review_criterion", "list_repositories")
	// QA is the only role that WRITES test cases.
	tools = append(tools, "list_test_cases", "record_test_cases", "set_test_case_result")
	// Green CI and stage base_url for the suite; prod requests banned — the rule layer says so separately.
	tools = append(tools, "get_pipeline_status", "get_deploy_target", "update_deploy_target")
	tools = append(tools, rolePRReadTools...)
	tools = append(tools, roleMemoryTools...)
	tools = append(tools, roleSkillTools...)
	return domain.ToolPolicy{AllowTools: tools}
}

// The release engineer owns everything after sign-off: merge, deploy, watch,
// verify, finish or roll back. It holds no workspace writers and no commit
// tools — it ships and reverts through the release tools, never by editing.
func releaseEngineerToolPolicy() domain.ToolPolicy {
	tools := make([]string, 0, 48)
	tools = append(tools, "run_terminal")
	tools = append(tools, roleWebTools...)
	tools = append(tools, roleQALookupTools...)
	tools = append(tools, roleProjectModelReadTools...)
	tools = append(tools, roleRuntimeReadTools...)
	tools = append(tools, roleReleaseBrowserReadTools...)
	tools = append(tools, "list_board_tasks", "move_board_task", "add_task_comment", "list_task_comments", "list_task_documents", "list_acceptance_criteria", "list_repositories")
	tools = append(tools, "get_pipeline_status", "get_deploy_target", "update_deploy_target")
	tools = append(tools, rolePRReadTools...)
	tools = append(tools, rolePRMergeTools...)
	tools = append(tools, roleReleaseTools...)
	// get_deploy_logs stays for a CI job's own output; the release tools above cover the release's own checks.
	tools = append(tools, domain.DeployLogsToolName)
	tools = append(tools, "list_incidents", "get_incident")
	tools = append(tools, roleMemoryTools...)
	tools = append(tools, roleSkillTools...)
	return domain.ToolPolicy{AllowTools: tools}
}

func toolPolicyEqual(a, b domain.ToolPolicy) bool {
	return slices.Equal(sortedCopy(a.AllowTools), sortedCopy(b.AllowTools)) &&
		slices.Equal(sortedCopy(a.AllowMCPServers), sortedCopy(b.AllowMCPServers))
}

func sortedCopy(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	out := append([]string(nil), items...)
	slices.Sort(out)
	return out
}

// Developer policy plus the device tools; the device is one shared phone, so only the mobile developer handles it.
func mobileDeveloperToolPolicy() domain.ToolPolicy {
	base := developerToolPolicy()
	tools := make([]string, 0, len(base.AllowTools)+len(roleMobileTools))
	tools = append(tools, base.AllowTools...)
	tools = append(tools, roleMobileTools...)
	base.AllowTools = tools
	return base
}
