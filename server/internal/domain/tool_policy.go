package domain

import (
	"path"
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
// actually looked at the repository before writing about it.
//
// run_terminal is deliberately NOT on the list even though `grep`/`cat` can
// read code with it: the failure this list exists to catch was an analiz run
// whose only tool calls were `echo "<spec>" > docs/specs/...`, which would have
// passed a terminal-counts check. An agent that prefers the shell for reading
// still has to make one indexed call to prove grounding — the tools are in
// every code-facing agent's policy, and they are cheaper than shelling out.
var CodeExplorationTools = []string{
	"codebase_search",
	"grep_code",
	"get_repo_tree",
	"get_symbol_skeleton",
	"expand_symbol_context",
	"read_file",
}

// QAExecutionTools are the tools that actually exercise a running product, and
// therefore the evidence that a QA run tested anything at all.
//
// The failure they exist to catch: a QA run whose whole output was a scenario
// plan in the future tense — "I will boot the app, run these four scenarios and
// verify each acceptance criterion" — with one board move as its entire tool
// ledger. Nothing in that text distinguishes it from a finished test round, and
// the orchestration verifier passed it as a coherent plan; only the ledger says
// no product was ever started.
//
// Deliberately excluded:
//   - review_criterion / add_task_comment — recording a verdict is the CLAIM,
//     not the test. Counting them would let a run approve every criterion
//     without touching the product, which is the exact thing QA is the gate for.
//   - get_pipeline_status — reading someone else's build. QA's own manual pass
//     is what this list measures; the pipeline is checked on top of it.
//   - the code-reading tools — QA is black-box; reading source proves the code
//     says what it says, not that the product does it.
//
// UIObservationTools are the calls that put the running interface in front of
// the model — a picture of it, or its rendered DOM.
//
// They are separated from QAExecutionTools because run_terminal alone satisfies
// that list, and a QA round on a web change that only ran build and test
// commands approved every criterion without one image ever reaching the model.
// The agents are on vision-capable models; nothing was ever shown to them. A
// button rendering as a bare "?" passed QA that way — no screenshot exists in
// the run's ledger, so there was nothing for the vision model to miss.
var UIObservationTools = []string{
	"browser_screenshot",
	"browser_read_dom",
	"mobile_screenshot",
	"mobile_read_ui",
}

// RepoHasUI reports whether every task in a repository is a task about
// something a person looks at — so a round that produced no visual observation
// has not observed the deliverable.
//
// Only the single-kind frontend and mobile repos qualify, and the strictness is
// the point: a backend or worker repo has no screen, so demanding a screenshot
// there would fail correct API test rounds for missing evidence that cannot
// exist. Monorepos are excluded for the same reason from the other side — a
// monorepo holds backend work too, and the repository kind alone cannot tell
// which half a given task touched. Being wrong there would block backend tasks
// on an impossible requirement, which is worse than missing the gate on the
// frontend half of a monorepo.
func RepoHasUI(repo Repository) bool {
	switch repo.Kind {
	case RepoKindFrontend, RepoKindMobile:
		return true
	default:
		return false
	}
}

// nonUIPathExtensions are file extensions that never render a pixel:
// documentation and shell scripts.
var nonUIPathExtensions = map[string]bool{
	".md": true, ".mdx": true, ".rst": true, ".txt": true,
	".sh": true, ".bash": true, ".zsh": true,
}

// nonUIPathBasenames are exact, case-insensitive basenames that are
// documentation or dependency lockfiles regardless of extension — a lockfile
// declares versions, it never renders one.
var nonUIPathBasenames = map[string]bool{
	"license": true, "license.md": true, "changelog.md": true, "notice": true,
	"package-lock.json": true, "yarn.lock": true, "pnpm-lock.yaml": true,
	"go.sum": true, "go.mod": true,
}

// nonUIPathDirs are directories that hold repo tooling and docs, never a
// rendered surface.
var nonUIPathDirs = []string{".ai", ".github", "docs", "scripts"}

// DiffNeedsUIEvidence reports whether a changed-file set contains anything a
// screenshot or a DOM read could confirm or refute. RepoHasUI answers "is this
// repository the kind of thing a person looks at" (repo-level, static); this
// answers "did THIS diff touch the part of it a person would see" (diff-level)
// — both are required before the code_review hand-off can demand
// browser_screenshot/browser_read_dom/mobile_screenshot/mobile_read_ui, or a
// docs-only run on a frontend/mobile repo is held for evidence that cannot
// exist.
//
// The default is to REQUIRE evidence: an empty path list (unreadable diff) or
// any path that is not one of the narrow non-visual categories below counts as
// UI-relevant. Missing this gate on a real UI change is worse than an
// unnecessary screenshot demand on a borderline one — deliberately not
// exempting .json (locale strings render), .css/.ts/.tsx/.jsx/.vue/.html, or
// anything unrecognized.
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

	if nonUIPathBasenames[base] {
		return false
	}
	if nonUIPathExtensions[path.Ext(base)] {
		return false
	}
	dir := ""
	if idx := strings.LastIndex(lower, "/"); idx >= 0 {
		dir = lower[:idx]
	}
	for _, d := range nonUIPathDirs {
		if dir == d || strings.Contains("/"+dir+"/", "/"+d+"/") {
			return false
		}
	}
	return true
}

var QAExecutionTools = []string{
	"run_terminal",
	"browser_navigate",
	"browser_click",
	"browser_fill",
	"browser_wait_for",
	"browser_read_dom",
	"browser_screenshot",
	"browser_set_viewport",
	// The device half. A mobile QA round runs on the phone, and without these
	// a run that did exactly what it was asked — launched the app, tapped
	// through the flow, photographed each screen — would be judged as having
	// executed nothing and dispatched again.
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

// ImplementationVerificationTools are the evidence that an implementation run
// executed the code it wrote instead of only writing it.
//
// The failure they exist to catch: a run that edited files, ticked its criteria
// and closed with "the change should work", never having built or tested
// anything. The hand-off then opened a pull request whose first pipeline run was
// the first time the code was ever compiled, and the reviewer got a red build
// for a task reported as finished.
//
// run_terminal is the whole list on purpose. The build and test commands are
// per-project (go test, npm run build, pytest, gradle) and there is no tool that
// means "build" — the shell is where every one of them runs. This is a floor,
// not a proof: it catches the run that executed nothing at all, which is the
// failure actually observed. The reviewer and the pipeline judge the rest.
//
// Deliberately excluded: the file tools and the code-reading tools. Writing a
// file is the work being verified, and reading one back is not running it.
var ImplementationVerificationTools = []string{
	"run_terminal",
}

// AnalizDocumentTools are the evidence that an analiz run produced its
// deliverable: an analiz task's output is not a diff but the spec/plan
// documents attached with add_task_document — or, for a need_revision bounce,
// rewritten in place with update_task_document, exactly as the analiz system
// prompt instructs — so this is that column's equivalent of
// ImplementationVerificationTools for the same hand-off check.
var AnalizDocumentTools = []string{
	"add_task_document",
	"update_task_document",
}

// BoardProgressTools announce that work has STARTED. Claiming a task and moving
// it to in_progress tells the board someone picked it up; neither produces the
// thing the task asks for.
//
// A subtask whose whole successful tool ledger is these two was stamped
// "completed" purely because its agent loop returned without an error, so the UI
// showed a finished card for a run whose own output read "I claimed the task and
// moved it to in_progress. Now I will start by reviewing the project structure."
var BoardProgressTools = []string{
	"claim_board_task",
	"move_board_task",
}

// RequiredAnalizTools is the minimum tool set an agent needs to carry an
// analiz task through the board: read the code it is documenting
// (CodeExplorationTools), produce its deliverable (AnalizDocumentTools),
// announce it started and read back a need_revision bounce, and — the one
// that actually blocked the bug this setting exists for — open the
// per-project implementation tasks an approved analysis decomposes into
// (create_board_task). Built from the named groups already used for the same
// question elsewhere rather than a list invented from scratch.
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
	add(BoardProgressTools...)
	add("add_task_comment", "list_task_comments", "list_task_documents", "create_board_task")
	return out
}

// MissingAnalizTools reports which of RequiredAnalizTools a policy does not
// grant. An unrestricted policy (empty allowlist) is never missing anything,
// matching ToolAllowedByPolicy's own reading of a zero policy.
func MissingAnalizTools(p ToolPolicy) []string {
	if p.IsZero() {
		return nil
	}
	var missing []string
	for _, name := range RequiredAnalizTools {
		if !ToolAllowedByPolicy(name, p) {
			missing = append(missing, name)
		}
	}
	return missing
}

// ToolAllowedByPolicy reports whether a policy permits the named tool. An empty
// allowlist is unrestricted, matching how the registry resolves definitions.
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
// subtasks each holding one of these produce two records for one piece of work,
// which is the only reason a plan declares tool_names at all.
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
// policy is not role-scoped, because handing them out on the strength of a stale
// row would widen what an agent may DO, not just what it may see.
//
// The three PR tools ride with them rather than with the read-only uplift on
// purpose. commit_task_changes writes to origin, and comment_on_pull_request
// speaks to reviewers under the deployment's GitHub identity; both belong with the
// shell, not with grep. get_task_pull_request only reads, but a chat that can read
// the PR and not act on it is half a feature, and it is the same audience — so the
// three travel together and a role-scoped agent gets them from its role policy
// (see catalog/role_tools.go) and migration 088, never from an uplift.
// WorkspaceWriteTools change files in the workspace. They ride with the shell,
// not with the read-only uplift: whoever may run `sed -i` may call these, and
// whoever may not, may not. Granting them on the strength of a stale agent row
// would widen what an agent may DO, which is the line workspaceToolAllowPatterns
// draws.
//
// They exist because the shell was the only writer: every change went out as
// `sed -i` (one file, one pattern, per agent turn, silent about what it
// matched) and every new file as a heredoc, which the sandbox's allowlist mode
// rejects outright for containing a backtick or $( — so an agent holding the
// shell still reported that it could not create files.
var WorkspaceWriteTools = []string{
	"write_file",
	"edit_file",
	"edit_lines",
	"delete_file",
	"move_file",
	// download_file writes into the workspace the same way write_file does —
	// the bytes just come off the network instead of out of the model. It rides
	// with the writers so review/QA columns lose it exactly when they lose them.
	"download_file",
}

// merge_task_pull_request is deliberately NOT here. This list is what an agent
// gets for having an unscoped policy — a stale row, a custom agent someone
// wired up — and merging is the one board action that cannot be undone: it puts
// code on the default branch of a repository this system otherwise only ever
// proposes changes to. It is granted the other way round, by naming the role
// that may do it (catalog/role_tools.go: QA, and migration 104 for installs
// seeded earlier). An uplift can widen what an agent may SEE for free; widening
// what it may LAND is a decision someone has to have made.
//
// rollback_task_release is absent for exactly the same reason, one step further
// down the same pipe: it changes what is RUNNING in production. The two read-only
// deploy-watch tools (get_task_deploy_status, get_deploy_logs) are absent too,
// but only because they are useless without the role that acts on them — they
// would be harmless here, and an install that wants them for a custom agent
// should say so on that agent rather than inherit them from a stale row.
var workspaceToolAllowPatterns = append([]string{
	"run_terminal",
	"mcp_filesystem_*",
	"get_task_pull_request",
	"commit_task_changes",
	"comment_on_pull_request",
}, WorkspaceWriteTools...)

// workspaceReadAlwaysTools are read-only board/workspace tools that must be
// available to any tool-scoped agent regardless of a narrow or stale persisted
// allowlist, so a factual question ("how many projects do we have?") never fails
// because the tool was added to the code after the agent row was seeded.
var workspaceReadAlwaysTools = []string{
	"list_projects",
	"list_repositories",
	"get_board_summary",
	"list_team",
}

func UpliftWorkspaceTools(p ToolPolicy) ToolPolicy {
	if len(p.AllowTools) == 0 {
		return p
	}
	uplifted := p
	// Always ensure the read-only workspace tools, even for role-scoped agents
	// (whose board tools otherwise short-circuit uplift below).
	for _, pattern := range workspaceReadAlwaysTools {
		if !hasExactAllowPattern(uplifted.AllowTools, pattern) {
			uplifted.AllowTools = append(uplifted.AllowTools, pattern)
		}
	}
	// Reading the repository is on the same footing. A role agent's tool policy
	// is written when its row is created and never reconciled afterwards, so an
	// install seeded before a role gained the code tools keeps a policy without
	// them — and the board short-circuit below used to withhold them for good.
	// That is how a system-architect reached code_review with no way to read the
	// diff it was dispatched to review: it spent its run writing "I don't have
	// the necessary tools" onto the task. These tools only read, so granting
	// them cannot widen what an agent may change.
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

// RestrictToolsForTaskType narrows a run's policy by what the TASK asks for,
// after the role's policy has said what the AGENT may do. The two are different
// questions: the architect holds the workspace writers because it also runs in
// code_review and in chats, but on an analiz task there is nothing for them to
// write — the deliverable is a document attached with add_task_document, the run
// is never committed (Runner skips the post-run commit for analiz) and never
// handed to code_review (advanceToCodeReview returns early for it).
//
// Until this existed the gap was only ever stated in the prompt, and the prompt
// lost: dispatched on an analiz task whose description read like an instruction,
// the architect called edit_file three times and delete_file once, reported the
// feature as built, and every one of those edits died with the workspace.
//
// An empty allowlist means unrestricted and stays unrestricted: the policy has
// no deny list to express this in, and an agent with no allowlist is one the
// operator has deliberately left unscoped.
func RestrictToolsForTaskType(p ToolPolicy, t TaskType) ToolPolicy {
	if t != TaskTypeAnaliz || len(p.AllowTools) == 0 {
		return p
	}
	kept := make([]string, 0, len(p.AllowTools))
	for _, name := range p.AllowTools {
		if isWorkspaceWriteTool(name) {
			continue
		}
		kept = append(kept, name)
	}
	restricted := p
	restricted.AllowTools = kept
	return restricted
}

// verdictColumns are the columns whose deliverable is a judgement about someone
// else's work, not a change to it: review, QA and UAT — plus done, whose run
// makes no judgement at all.
//
// done is here because the run dispatched into it merges the task's pull request
// and deletes the branch. A writer or a commit in that run is worse than
// pointless: `commit_task_changes` would push the task branch back to origin
// moments after the merge deleted it, re-creating a branch whose content is
// already on the default branch. (The runner refuses the post-run commit there
// for the same reason — board.producesADiff — so this closes the tool-call half
// of the same hole.) The merge tool itself survives the narrowing; see below.
var verdictColumns = map[TaskColumn]bool{
	TaskColumnCodeReview:   true,
	TaskColumnAnalizReview: true,
	TaskColumnReadyForQA:   true,
	TaskColumnInQA:         true,
	TaskColumnPMUAT:        true,
	TaskColumnHumanUAT:     true,
	TaskColumnDone:         true,
}

// RestrictToolsForVerdictColumn takes the workspace writers away from a run
// whose job is to judge, not to change.
//
// The same lesson as RestrictToolsForTaskType, from the other end of the board.
// A QA agent that found a mobile layout bug went and fixed it: it edited the
// stylesheet and spent the rest of its run trying to boot the project to check
// its own repair. That is wrong twice over. The change escapes review — the
// post-run commit is skipped for these columns, so the edit either dies with the
// workspace or rides along unreviewed — and the bug it was there to REPORT never
// reaches the developer, because the card was never handed back. A reviewer that
// edits the diff is reviewing its own code.
//
// run_terminal stays: QA has to start the product and run its tests, and that is
// the evidence this column exists to produce. Only the writers, the commit and
// (outside `done`) the merge go.
//
// merge_task_pull_request is stripped everywhere except `done` for the same
// reason commit is: a run whose job is to judge a change must not be able to
// LAND it. In `done` the board has already signed the task off and merging is
// the only thing the run is dispatched for, so there the tool survives.
//
// An empty allowlist means unrestricted and stays unrestricted, exactly as in
// RestrictToolsForTaskType: the policy has no deny list, and an agent left
// unscoped was left unscoped deliberately.
func RestrictToolsForVerdictColumn(p ToolPolicy, col TaskColumn) ToolPolicy {
	if !verdictColumns[col] || len(p.AllowTools) == 0 {
		return p
	}
	kept := make([]string, 0, len(p.AllowTools))
	for _, name := range p.AllowTools {
		if isWorkspaceWriteTool(name) || name == "commit_task_changes" {
			continue
		}
		// The merge is the one write a run in `done` is there to make, and the
		// one write no other column may make: a reviewer or a QA round must not
		// be able to land the change it is judging.
		if name == MergePullRequestToolName && col != TaskColumnDone {
			continue
		}
		// The rollback changes what is running in production, so it is held to
		// the same rule from the other side: it survives only in the columns a
		// release can have happened in. A QA round in `in_qa` has nothing of its
		// own in production; giving it a rollback there would let a run judging
		// a change on stage undo somebody else's live release.
		//
		// `released` is not a verdict column and never reaches this function, so
		// naming `done` here is what keeps the tool available across the whole
		// merge → watch → rollback sequence.
		if IsReleaseControlTool(name) && col != TaskColumnDone {
			continue
		}
		kept = append(kept, name)
	}
	restricted := p
	restricted.AllowTools = kept
	return restricted
}

// pmUATColumns are the columns where the PM verifies rather than authors: the
// backlog-grooming use of CodeExplorationTools ("verify a real file/endpoint
// name before writing technical_description") does not apply here, so the
// whole set goes away.
var pmUATColumns = map[TaskColumn]bool{
	TaskColumnPMUAT:    true,
	TaskColumnHumanUAT: true,
}

// qaVerificationColumns are the columns where QA is expected to be black box.
var qaVerificationColumns = map[TaskColumn]bool{
	TaskColumnInQA:       true,
	TaskColumnReadyForQA: true,
}

// RestrictCodeToolsForVerification takes source-reading tools away from a run
// whose job is to exercise the running product, not to read the diff and
// decide it looks right.
//
// PM loses every CodeExplorationTools name in pm_uat/human_uat: those are the
// only columns PM signs a criterion off in, and its verdict must come from a
// browser/mobile session run against the product, never from read_file on the
// implementation. Outside those two columns PM keeps the set — it still needs
// to verify a real file or endpoint name before writing technical_description
// while grooming the backlog.
//
// QA loses only read_file in in_qa/ready_for_qa. get_repo_tree, grep_code and
// get_task_pull_request survive there because QA's three legitimate reasons to
// look at code before or during a round — debugging an observed failure from
// its own surfaced output, scoping a resubmission's re-test from the diff, and
// checking the case matrix is complete before execution — are tree-level and
// diff-level, never "read this file's full body and decide from it".
//
// An empty allowlist means unrestricted and stays unrestricted, exactly as in
// RestrictToolsForVerdictColumn: the policy has no deny list, and an agent
// left unscoped was left unscoped deliberately.
func RestrictCodeToolsForVerification(p ToolPolicy, col TaskColumn) ToolPolicy {
	if len(p.AllowTools) == 0 {
		return p
	}
	var strip []string
	switch {
	case pmUATColumns[col]:
		strip = CodeExplorationTools
	case qaVerificationColumns[col]:
		strip = []string{"read_file"}
	default:
		return p
	}
	kept := make([]string, 0, len(p.AllowTools))
	for _, name := range p.AllowTools {
		if containsToolName(strip, name) {
			continue
		}
		kept = append(kept, name)
	}
	restricted := p
	restricted.AllowTools = kept
	return restricted
}

func containsToolName(names []string, name string) bool {
	for _, n := range names {
		if n == name {
			return true
		}
	}
	return false
}

// MergePullRequestToolName is the tool that lands a task's change. Named here
// because two policy decisions turn on it — it is withheld from the verdict
// columns above, and withheld from the workspace uplift below — and a tool this
// consequential must not be identified by a string literal in either place.
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

// RestrictToPlannedTools resolves a subtask's policy from the agent's own
// policy and the tool_names its plan declared.
//
// The declaration exists for ONE purpose: to fence off board record writes so
// two concurrent subtasks cannot open the same task. It is not a capability
// budget. A plain intersection treated it as one, and a subtask that declared
// [claim_board_task, move_board_task, add_task_comment] ran with no way to read
// the repository at all — its agent asked the human where the files were, the
// question five seconds of grep would have answered, and the clarification gate
// let that through precisely because the run had no read tool to point it at.
//
// So the declaration only decides board-write tools. Everything else the agent
// is configured for stays: the operator configured it, not the planner.
func RestrictToPlannedTools(base ToolPolicy, declared []string) ToolPolicy {
	if len(declared) == 0 {
		return base
	}
	out := IntersectToolPolicy(base, declared)
	keep := base.AllowTools
	if len(keep) == 0 {
		// An unrestricted agent has no configured list to preserve, so keep the
		// read tools every run needs to answer its own questions.
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
// base does not already permit. An unrestricted base (empty allowlist) accepts
// the requested set as-is.
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
