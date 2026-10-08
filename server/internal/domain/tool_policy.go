package domain

import (
	"path"
	"regexp"
	"strings"
)

type ToolPolicy struct {
	AllowMCPServers []string `json:"allow_mcp_servers,omitempty" koanf:"allow_mcp_servers"`
	AllowTools      []string `json:"allow_tools,omitempty" koanf:"allow_tools"`
}

func (p ToolPolicy) IsZero() bool {
	return len(p.AllowMCPServers) == 0 && len(p.AllowTools) == 0
}

func MergeToolPolicy(base, override ToolPolicy) ToolPolicy {
	merged := base
	if len(override.AllowMCPServers) > 0 {
		merged.AllowMCPServers = override.AllowMCPServers
	}
	if len(override.AllowTools) > 0 {
		merged.AllowTools = override.AllowTools
	}
	return merged
}

// CodeExplorationTools are the read-only tools that count as evidence an agent
// actually looked at the repository before writing about it. run_terminal is
// deliberately excluded so a run whose only calls were `echo "<spec>" > …`
// still must make one indexed call to prove grounding.
var CodeExplorationTools = []string{
	"codebase_search",
	"grep_code",
	"get_repo_tree",
	"get_symbol_skeleton",
	"expand_symbol_context",
	"read_file",
}

// QAExecutionTools actually exercise a running product — the evidence a QA run
// tested anything. A verdict-only run (review_criterion, add_task_comment) or a
// shell-only round never touches these and cannot pass.
var QAExecutionTools = []string{
	"run_terminal",
	"browser_navigate",
	"browser_click",
	"browser_fill",
	"browser_wait_for",
	"browser_read_dom",
	"browser_screenshot",
	"browser_set_viewport",
	// The device half: mobile QA runs on the phone, and a run that launched the
	// app and tapped through the flow must count as executing something.
	"mobile_launch_app",
	"mobile_tap",
	"mobile_type_text",
	"mobile_swipe",
	"mobile_wait_for",
	"mobile_read_ui",
	"mobile_screenshot",
	"mobile_press_button",
	"mobile_rotate",
}

// UIObservationTools put the running interface in front of the (vision-capable)
// model — a screenshot or its rendered DOM — so a QA round that only ran build
// and test commands cannot approve visual criteria unobserved.
var UIObservationTools = []string{
	"browser_screenshot",
	"browser_read_dom",
	"mobile_screenshot",
	"mobile_read_ui",
}

// RepoHasUI reports whether every task in a repository is about something a
// person looks at, so a no-visual-evidence round has not observed the
// deliverable. Only single-kind frontend and mobile repos qualify; demanding a
// screenshot on a backend or monorepo task would ask for evidence that cannot
// exist.
func RepoHasUI(repo Repository) bool {
	switch repo.Kind {
	case RepoKindFrontend, RepoKindMobile:
		return true
	default:
		return false
	}
}

// nonUIPathExtensions are file extensions that never render a pixel.
var nonUIPathExtensions = map[string]bool{
	".md": true, ".mdx": true, ".rst": true, ".txt": true,
	".sh": true, ".bash": true, ".zsh": true,
	".sql": true, ".prisma": true, ".toml": true,
}

// nonUIPathBasenames are exact, case-insensitive basenames that are docs,
// dependency lockfiles or build/deploy configuration regardless of extension.
var nonUIPathBasenames = map[string]bool{
	"license": true, "license.md": true, "changelog.md": true, "notice": true,
	"package-lock.json": true, "yarn.lock": true, "pnpm-lock.yaml": true,
	"go.sum": true, "go.mod": true,
	"package.json": true, "tsconfig.json": true, "vercel.json": true, "netlify.toml": true,
	".gitignore": true, ".npmrc": true, ".nvmrc": true,
	"dockerfile": true, "docker-compose.yml": true, "docker-compose.yaml": true,
}

var nonUIPathBasenamePrefixes = []string{".env", ".eslintrc", ".prettierrc"}

// nonUIPathDirs are directory segments, at any depth, that hold repo tooling,
// docs, server-side code, schema or tests. "api" covers Vercel api/, Next
// pages/api and app/api, and frontend API clients.
var nonUIPathDirs = map[string]bool{
	".ai": true, ".github": true, "docs": true, "scripts": true,
	"api": true, "server": true, "functions": true,
	"supabase": true, "prisma": true, "migrations": true,
	"__tests__": true, "test": true, "tests": true,
	"e2e": true, "cypress": true, "playwright": true,
	"__mocks__": true, "mocks": true,
}

var testFileName = regexp.MustCompile(`\.(test|spec)\.[a-z0-9]+$`)

// DiffNeedsUIEvidence reports whether a changed-file set could be confirmed or
// refuted by a screenshot/DOM read. The default is to REQUIRE evidence: an
// empty path list or any path outside the non-visual categories (docs, tooling,
// lockfiles and build config, server-side and API code, schema and
// migrations, tests and mocks) counts as UI-relevant. Style, Tailwind and
// framework config, index.html and public assets change pixels and stay UI.
func DiffNeedsUIEvidence(paths []string) bool {
	if len(paths) == 0 {
		return true
	}
	for _, p := range paths {
		if isUIRelevantPath(p) {
			return true
		}
	}
	return false
}

func isUIRelevantPath(p string) bool {
	p = strings.TrimSpace(strings.ReplaceAll(p, "\\", "/"))
	if p == "" {
		return false
	}
	lower := strings.ToLower(p)
	base := path.Base(lower)

	if nonUIPathBasenames[base] || nonUIPathExtensions[path.Ext(base)] {
		return false
	}
	for _, prefix := range nonUIPathBasenamePrefixes {
		if strings.HasPrefix(base, prefix) {
			return false
		}
	}
	// tsconfig.api.json and friends only steer the type checker, and a
	// .vercelignore/.dockerignore only decides what is uploaded — neither
	// renders a pixel.
	if (strings.HasPrefix(base, "tsconfig") && strings.HasSuffix(base, ".json")) ||
		(strings.HasPrefix(base, ".") && strings.HasSuffix(base, "ignore")) {
		return false
	}
	if testFileName.MatchString(base) || strings.HasSuffix(base, "_test.go") {
		return false
	}
	if idx := strings.LastIndex(lower, "/"); idx >= 0 {
		for _, seg := range strings.Split(lower[:idx], "/") {
			if nonUIPathDirs[seg] {
				return false
			}
		}
	}
	return true
}

// PMUATExecutionTools demand the same evidence QAExecutionTools does, minus
// run_terminal (PM has no shell); get_deploy_target is excluded too — resolving
// the stage URL is a lookup, not exercising the product.
var PMUATExecutionTools = []string{
	"browser_navigate",
	"browser_click",
	"browser_fill",
	"browser_wait_for",
	"browser_read_dom",
	"browser_screenshot",
	"browser_set_viewport",
	"mobile_launch_app",
	"mobile_tap",
	"mobile_type_text",
	"mobile_swipe",
	"mobile_wait_for",
	"mobile_read_ui",
	"mobile_screenshot",
	"mobile_press_button",
	"mobile_rotate",
}

// ImplementationVerificationTools are the evidence an implementation run
// executed the code it wrote instead of only writing it. run_terminal is the
// whole list because build/test commands are per-project and only the shell can
// run them; it is a floor, not a proof — the reviewer and pipeline judge the
// rest.
var ImplementationVerificationTools = []string{
	"run_terminal",
}

// AnalizDocumentTools are the evidence an analiz run produced its deliverable:
// analiz output is the spec/plan documents attached (or rewritten) with
// add/update_task_document, not a diff.
var AnalizDocumentTools = []string{
	"add_task_document",
	"update_task_document",
}

// OpenQuestionsTools let an analiz run record, edit and withdraw open
// questions for the human instead of ask_user or prose in the report's risks
// section. Granted alongside AnalizDocumentTools — anyone trusted to attach
// the report is trusted to ask about it.
var OpenQuestionsTools = []string{
	"record_open_questions",
	"list_open_questions",
}

// BoardProgressTools announce that work STARTED; neither produces the task's
// deliverable. A subtask whose whole ledger is these two must not pass as
// completed.
var BoardProgressTools = []string{
	"claim_board_task",
	"move_board_task",
}

// RequiredAnalizTools is the minimum set to carry an analiz task through the
// board: read the code (CodeExplorationTools), produce the deliverable
// (AnalizDocumentTools), announce the start (BoardProgressTools), and open the
// implementation tasks an approved analysis decomposes into (create_board_task).
var RequiredAnalizTools = buildRequiredAnalizTools()

func buildRequiredAnalizTools() []string {
	seen := make(map[string]bool)
	out := make([]string, 0, len(CodeExplorationTools)+len(AnalizDocumentTools)+len(BoardProgressTools)+4)
	add := func(names ...string) {
		for _, name := range names {
			if seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, name)
		}
	}
	add(CodeExplorationTools...)
	add(AnalizDocumentTools...)
	add(OpenQuestionsTools...)
	add(BoardProgressTools...)
	add("add_task_comment", "list_task_comments", "list_task_documents", "create_board_task")
	return out
}

// MissingAnalizTools reports which of RequiredAnalizTools a policy does not
// grant.
func MissingAnalizTools(p ToolPolicy) []string {
	return MissingTools(p, RequiredAnalizTools)
}

// MissingTools reports which of required a policy does not grant; an
// unrestricted policy (empty allowlist) is never missing anything.
func MissingTools(p ToolPolicy, required []string) []string {
	if p.IsZero() {
		return nil
	}
	var missing []string
	for _, name := range required {
		if !ToolAllowedByPolicy(name, p) {
			missing = append(missing, name)
		}
	}
	return missing
}

// ToolAllowedByPolicy reports whether a policy permits the named tool; an empty
// allowlist is unrestricted.
func ToolAllowedByPolicy(name string, p ToolPolicy) bool {
	if len(p.AllowTools) == 0 {
		return true
	}
	return matchesAnyToolPattern(name, p.AllowTools)
}

// IsBoardProgressTool reports whether a tool only signals progress.
func IsBoardProgressTool(name string) bool {
	for _, t := range BoardProgressTools {
		if t == name {
			return true
		}
	}
	return false
}

// BoardWriteTools are the tools whose effect is a board record. Two concurrent
// subtasks each holding one produce two records for one piece of work — the
// only reason a plan declares tool_names at all.
var BoardWriteTools = []string{
	"create_board_task",
	"move_board_task",
	"update_board_task",
}

// IsBoardWriteTool reports whether a tool writes a board record.
func IsBoardWriteTool(name string) bool {
	for _, t := range BoardWriteTools {
		if t == name {
			return true
		}
	}
	return false
}

// workspaceToolAllowPatterns can change the machine: the shell, filesystem
// writes, and pushing the task branch. They are granted only to an agent whose
// policy is not role-scoped — handing them out on the strength of a stale row
// would widen what an agent may DO. The three PR tools ride with them rather
// than with the read-only uplift for the same reason.
var WorkspaceWriteTools = []string{
	"write_file",
	"edit_file",
	"edit_lines",
	"delete_file",
	"move_file",
	// download_file writes into the workspace like write_file; the bytes just
	// come off the network, so it rides with the writers.
	"download_file",
}

// merge_task_pull_request and rollback_task_release are deliberately NOT here:
// merging lands code on a default branch, rollback changes production — both
// are granted the other way round, by naming the role that may do it. An uplift
// may widen what an agent may SEE; what it may LAND is a decision someone must
// have made.
var workspaceToolAllowPatterns = append([]string{
	"run_terminal",
	"mcp_filesystem_*",
	"get_task_pull_request",
	"commit_task_changes",
	"comment_on_pull_request",
}, WorkspaceWriteTools...)

// workspaceReadAlwaysTools are read-only board/workspace tools always available
// to a tool-scoped agent, so a factual question never fails because the tool
// postdates the agent's seeded row.
var workspaceReadAlwaysTools = []string{
	"list_projects",
	"list_repositories",
	"get_board_summary",
	"list_team",
	// Every agent that touches UI has to build with the design system, and an
	// agent row seeded before the tool existed would otherwise never see it.
	"get_design_system",
}

func UpliftWorkspaceTools(p ToolPolicy) ToolPolicy {
	if len(p.AllowTools) == 0 {
		return p
	}
	uplifted := p
	// Always ensure the read-only workspace tools, even for role-scoped agents.
	for _, pattern := range workspaceReadAlwaysTools {
		if !hasExactAllowPattern(uplifted.AllowTools, pattern) {
			uplifted.AllowTools = append(uplifted.AllowTools, pattern)
		}
	}
	// Role policies are written at row creation and never reconciled, so an
	// install seeded before a role gained the code tools keeps a policy without
	// them — which stranded a system-architect in code_review unable to read the
	// diff. These tools only read, so granting them widens nothing.
	for _, pattern := range CodeExplorationTools {
		if !hasExactAllowPattern(uplifted.AllowTools, pattern) {
			uplifted.AllowTools = append(uplifted.AllowTools, pattern)
		}
	}
	if hasRoleScopedBoardTools(p.AllowTools) {
		return uplifted
	}
	for _, pattern := range workspaceToolAllowPatterns {
		if !hasExactAllowPattern(uplifted.AllowTools, pattern) {
			uplifted.AllowTools = append(uplifted.AllowTools, pattern)
		}
	}
	return uplifted
}

// RestrictToolsForStage narrows a run's policy by what its STAGE (task type +
// column) asks for, the workflow-driven replacement for the old literal
// type/column switches. The narrowings are cumulative and apply type-first,
// then the stage's own. The architect holds workspace writers only on stages
// where they are not stripped (analiz: the deliverable is a document, the run
// is never committed, so writes would die with the workspace).
func RestrictToolsForStage(p ToolPolicy, stage WorkflowStage, typeDef TaskTypeDef) ToolPolicy {
	if len(p.AllowTools) == 0 {
		return p
	}
	out := p
	if typeDef.Has(BehaviourNoWorkspaceWrites) {
		out = dropTools(out, isWorkspaceWriteTool)
	}
	if stage.Has(BehaviourStripWriters) {
		allow, _ := stage.Param(BehaviourStripWriters, "allow")
		keepMerge := allowListNames(allow)["merge_task_pull_request"]
		keepRelease := allowListNames(allow)["release_control"]
		out = dropTools(out, func(name string) bool {
			if isWorkspaceWriteTool(name) || name == "commit_task_changes" {
				return true
			}
			if name == MergePullRequestToolName && !keepMerge {
				return true
			}
			if IsReleaseControlTool(name) && !keepRelease {
				return true
			}
			return false
		})
	}
	if stage.Has(BehaviourNoCodeReading) {
		out = dropTools(out, func(name string) bool { return containsToolName(CodeExplorationTools, name) })
	}
	return out
}

// dropTools filters a policy's allow list by a predicate.
func dropTools(p ToolPolicy, drop func(name string) bool) ToolPolicy {
	kept := make([]string, 0, len(p.AllowTools))
	for _, name := range p.AllowTools {
		if drop(name) {
			continue
		}
		kept = append(kept, name)
	}
	out := p
	out.AllowTools = kept
	return out
}

// allowListNames parses a strip_writers "allow" param (a csv such as
// "merge_task_pull_request,release_control") into a set.
func allowListNames(csv string) map[string]bool {
	out := map[string]bool{}
	for _, part := range strings.Split(csv, ",") {
		if name := strings.TrimSpace(part); name != "" {
			out[name] = true
		}
	}
	return out
}

func containsToolName(names []string, name string) bool {
	for _, n := range names {
		if n == name {
			return true
		}
	}
	return false
}

// MergePullRequestToolName is the tool that lands a task's change; named so two
// policy decisions never identify it by a string literal.
const MergePullRequestToolName = "merge_task_pull_request"

func isWorkspaceWriteTool(name string) bool {
	for _, w := range WorkspaceWriteTools {
		if w == name {
			return true
		}
	}
	return false
}

func hasRoleScopedBoardTools(allow []string) bool {
	for _, name := range allow {
		if strings.Contains(name, "_board_") {
			return true
		}
	}
	return false
}

func (p ToolPolicy) Equal(other ToolPolicy) bool {
	if len(p.AllowTools) != len(other.AllowTools) || len(p.AllowMCPServers) != len(other.AllowMCPServers) {
		return false
	}
	for i, t := range p.AllowTools {
		if t != other.AllowTools[i] {
			return false
		}
	}
	for i, s := range p.AllowMCPServers {
		if s != other.AllowMCPServers[i] {
			return false
		}
	}
	return true
}

func hasExactAllowPattern(allow []string, pattern string) bool {
	for _, a := range allow {
		if a == pattern {
			return true
		}
	}
	return false
}

func matchesToolPattern(name, pattern string) bool {
	if pattern == name {
		return true
	}
	if strings.Contains(pattern, "*") {
		ok, err := path.Match(pattern, name)
		return err == nil && ok
	}
	return false
}

func matchesAnyToolPattern(name string, patterns []string) bool {
	for _, p := range patterns {
		if matchesToolPattern(name, p) {
			return true
		}
	}
	return false
}

// RestrictToPlannedTools resolves a subtask's policy from the agent's own policy
// and the tool_names its plan declared. The declaration exists for ONE purpose:
// to fence off board-record writes so two concurrent subtasks cannot open the
// same task — it is not a capability budget, so a plain intersection that would
// starve the run of read tools is widened back to everything else the agent is
// configured for.
func RestrictToPlannedTools(base ToolPolicy, declared []string) ToolPolicy {
	if len(declared) == 0 {
		return base
	}
	out := IntersectToolPolicy(base, declared)
	keep := base.AllowTools
	if len(keep) == 0 {
		// An unrestricted agent has no configured list, so keep the read tools
		// every run needs to answer its own questions.
		keep = CodeExplorationTools
	}
	for _, pattern := range keep {
		if IsBoardWriteTool(pattern) || hasExactAllowPattern(out.AllowTools, pattern) {
			continue
		}
		out.AllowTools = append(out.AllowTools, pattern)
	}
	return EnsureAskUserTool(out)
}

func EnsureAskUserTool(p ToolPolicy) ToolPolicy {
	if p.IsZero() {
		return ToolPolicy{AllowTools: []string{AskUserToolName}}
	}
	if matchesAnyToolPattern(AskUserToolName, p.AllowTools) {
		return p
	}
	out := p
	out.AllowTools = append(append([]string(nil), p.AllowTools...), AskUserToolName)
	return out
}

// IntersectMCPServers narrows base's MCP allowlist to `names`, dropping any the
// base does not already permit; an unrestricted base accepts the set as-is.
func IntersectMCPServers(base ToolPolicy, names []string) ToolPolicy {
	if len(names) == 0 {
		return base
	}
	out := base
	if len(base.AllowMCPServers) == 0 {
		out.AllowMCPServers = append([]string(nil), names...)
		return out
	}
	allowed := make([]string, 0, len(names))
	for _, name := range names {
		if matchesAnyToolPattern(name, base.AllowMCPServers) {
			allowed = append(allowed, name)
		}
	}
	out.AllowMCPServers = allowed
	return out
}

func IntersectToolPolicy(base ToolPolicy, toolNames []string) ToolPolicy {
	if len(toolNames) == 0 {
		return base
	}
	if base.IsZero() {
		return ToolPolicy{AllowTools: append([]string(nil), toolNames...)}
	}
	allowed := make([]string, 0, len(toolNames))
	for _, name := range toolNames {
		if len(base.AllowTools) == 0 || matchesAnyToolPattern(name, base.AllowTools) {
			allowed = append(allowed, name)
		}
	}
	merged := base
	merged.AllowTools = allowed
	return merged
}
