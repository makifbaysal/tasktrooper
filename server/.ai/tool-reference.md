# Tool Reference

## Built-in Tools

### `run_terminal`

Executes a shell command and returns combined stdout/stderr.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `command` | string | yes | Shell command to run via `sh -c` |
| `working_dir` | string | no | Override working directory for this call |

A non-zero exit produces `isError: true` (with the exit status and the output as content), as do timeouts, sandbox rejections and invalid arguments. It used to be reported as a success, which hid every failing command from the agent loop's error-streak guard and from the run's tool stats.

A `sed -n '<from>,<to>p'` read of fewer than 200 lines gets a line appended to its output pointing at `read_file`: paging a file through a small shell window costs one agent turn per window.

### `read_file`

Reads a workspace file with line numbers. Registered unconditionally (no semantic index required), confined to the workspace root, and part of `CodeExplorationTools` — so every tool-scoped agent has it, including on unindexed repositories. `domain.RestrictCodeToolsForVerification` takes it back out of QA's policy in `in_qa`/`ready_for_qa`, and out of PM's whole `CodeExplorationTools` set in `pm_uat`/`human_uat` — those two roles verify a running product in those columns, never a file body.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `path` | string | yes | File path relative to the workspace root |
| `offset` | integer | no | First line to return, 1-based (default 1) |
| `limit` | integer | no | Lines to return (default and maximum 800) |

Output is `path — lines A-B of N`, the numbered lines, then either `[end of file]` or the offset to continue from. One call is capped at 12,000 characters — below `tools.max_tool_output_chars` (16,000), so the loop never cuts the middle out of a listing. Directories, binary content and files over 20 MB are refused with the alternative named.

Exists because the deployment had no read tool: agents read source with `run_terminal: sed -n '630,640p'` and slid the window ten lines at a time, spending sixty of eighty iterations scrolling.

### Workspace write tools

`write_file`, `edit_file`, `edit_lines`, `delete_file`, `move_file` — everything an agent does to a file, without the shell. Registered unconditionally, confined to the workspace root, and `.git` plus the root itself are refused outright. They are **not** in `CodeExplorationTools`; they ride with `run_terminal` in `workspaceToolAllowPatterns` and `roleShellTools`, so an agent kept away from the shell does not get them (migration 090 grants them to every agent whose policy already carries `run_terminal`).

Every result states what changed — the count, the line numbers, the region as it now reads — so a run never has to spend a second turn grepping to confirm an edit landed.

| Tool | Parameters | Notes |
|---|---|---|
| `edit_file` | `path`, `old_string`, `new_string`, `replace_all` | Refuses an ambiguous match unless `replace_all`; refuses a no-op; preserves the file mode. Returns `N occurrences replaced at line a, b, c`. |
| `edit_lines` | `path`, `mode` (`replace`/`insert_after`/`delete`), `start_line`, `end_line`, `text` | Takes the line numbers `read_file` prints. `insert_after: 0` inserts at the top. Returns the changed region with its new numbering. |
| `write_file` | `path`, `content` | Creates parent directories, adds a trailing newline, preserves an existing file's mode. For a new file or a whole rewrite. |
| `delete_file` | `path`, `recursive` | A non-empty directory needs `recursive: true`. |
| `move_file` | `from`, `to`, `overwrite` | Rename or move; creates missing parents; refuses to clobber without `overwrite`. Says that imports of the old path are not updated. |

They exist because `run_terminal` was the only writer: a change meant `sed -i` (one file, one pattern, per turn, silent about what it matched) and a new file meant a heredoc, which allowlist mode rejects for containing a backtick or `$(`.

### `fetch_url`

Fetches a URL over HTTP GET.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `url` | string | yes | Must start with `http://` or `https://` |

For `text/html` responses, visible text is extracted (tags stripped). Response body is truncated to `tools.web.max_response_bytes`.

### `download_file`

Downloads a binary asset (image, font, icon) from a URL and saves it inside the workspace. The binary sibling of `fetch_url`, which decodes bodies as text and corrupts binaries — before this tool an agent could only reference assets it had no way to materialize, committing `<img>` tags that rendered as broken-image placeholders.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `url` | string | yes | Direct URL of the asset itself, not a page that shows it |
| `path` | string | yes | Destination path relative to the workspace root |

Same URL guard as `fetch_url`; path confined to the workspace, `.git` protected. Refuses `text/html` responses (a landing page saved as `.png` is the bug this tool prevents), non-2xx statuses, empty bodies, and anything over 10 MB. Rides with the workspace write tools in `domain.WorkspaceWriteTools`, so verdict columns (review/QA) lose it with the other writers. Registered under `tools.web.enabled`.

### `web_search`

Keyless. Queries DuckDuckGo (form POST to `html.duckduckgo.com`, browser headers, `kl=wt-wt`) and falls back to Bing when DuckDuckGo answers 202 or a challenge page; the result block ends with `source: duckduckgo|bing`.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `query` | string | yes | The search query |
| `max_results` | int | no | Default `tools.search.max_results`; clamped to 10, `<= 0` means the default |

One retry per backend under a second user agent, spent only on a challenge page, 429 or 5xx — never on a blocked destination, refused connection or oversized body. 1s minimum gap between outbound requests; 64-entry/10-minute cache of non-empty answers only; 512 KB response cap, and a result list past the cut is `response too large` with the next backend tried; same-host redirects only; URL-policy rejections reach the model as `blocked by the outbound URL policy`, detail in the log. Registered whenever `tools.search.enabled` — there is no key to be missing.

### `search_boilerplate_catalog`

Searches a shared boilerplate repository's `.ai/catalog.yaml` for an existing starter stack (backend/frontend/mobile/worker) before an agent writes new project code from scratch — copy the matched entry's `path` instead of generating files by hand.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `query` | string | no | Free-text filter over id/type/language/framework/tags/description. Omit or leave empty to list the full catalog. |

Which repo it searches is the admin-editable `boilerplate_catalog_repo` setting (`GET/PUT /v1/settings`; defaults to `github.com/makifbaysal/boilerplates`), read fresh on every call — no restart needed after changing it. Response JSON: `{repo, branch, query, count, entries: [{id, path, type, language, framework, tags, description, status}], hint}`.

## Board Tools

Board tools (`internal/adapter/tools/board`) are registered when the board/repository services are wired; they operate on tasks, criteria, projects, and pipelines rather than the shell/web.

### `list_ready_tasks`

The unblocked queue — the `bd ready` of Beads: `backlog` and `todo` tasks with no unfinished `blocks` blocker, sorted critical → high → medium → low, then by `task_number` (oldest first). Optional `column` (`backlog`|`todo`) and `assigned_to_me` (the run's agent from `registry.AgentIDFromContext`; an error outside a run). Repository-scoped like `list_board_tasks`; no `repository_id` argument. Returns `{tasks: [...], count}` with `tasks` never null.

It exists because `list_board_tasks` shows the whole board and leaves the model to work out for itself which cards are actually startable — a job it does by reading every card's `blocked_by` and guessing at columns. The service (`repository.Service.ListReadyTasks`) reads every unfinished `blocks` edge in ONE query (`TaskRelationStore.ListUnfinishedBlockers`) and filters in memory, so the answer costs two queries regardless of board size. Tasks parked in `blocked` never appear: the column filter excludes them before the blocker check runs.

### `get_pipeline_status`

Returns the most recent QA-gate pipeline run for a task (jobs attached). Agents call this after landing in `need_revision` to see exactly what failed — the same report is also proactively injected into the run's system prompt in that case (see `internal/application/board/runner.go`).

| Parameter | Type | Required | Description |
|---|---|---|---|
| `task_id` | string | yes | Board task UUID |

Response JSON: `{status, trigger, created_at, note, jobs: [{name, status, exit_code, output?}]}`. `output` is present only for jobs with `status: "failed"`, tail-truncated to 4,000 characters. If the task has no pipeline runs yet, the tool returns `{"message": "no pipeline for this task"}` rather than an error.

Repository-scoped like the other task tools: the task is resolved against the agent's context-bound repository (registry context), so a `task_id` from another repository returns not-found instead of that repository's build logs.

### `record_test_cases` / `set_test_case_result` / `list_test_cases`

The task's test round, stored next to its acceptance criteria (`task_test_cases`, migration 130). QA writes it; every other role reads it.

| Tool | Args | Notes |
|---|---|---|
| `record_test_cases` | `task_id`, `cases[]` | Upsert by `title`. Case: `title`, `category` (happy_path/boundary/negative/auth/empty_state/regression/visual/async/other), `status` (planned/passed/failed/skipped/invalid), `expected`, `actual`, `evidence`, `notes`, optional `criterion_id`. |
| `set_test_case_result` | `test_case_id`, `status`, `actual?`, `evidence?`, `notes?` | Empty fields keep their stored value. |
| `list_test_cases` | `task_id` | Returns the cases plus a status summary. |

Refused: `failed` without `actual`, `invalid`/`skipped` without `notes`, duplicate titles in one batch, a `criterion_id` from another task. `testCaseGate` refuses a forward move out of `ready_for_qa`/`in_qa` when the task has no cases or any case is still `planned`.

### `cancel_criterion`

Drops one acceptance criterion from scope with a reason (`task_acceptance_criteria.canceled/cancel_reason`, migration 131). Args: `criterion_id`, `reason`, optional `canceled` (default true). The reason is stored and posted as a task comment; the criteria gates treat a cancelled criterion as settled, never as met. Granted wherever `set_criterion_completed` is.

### `get_project_brief`, `list_component_checks`, `list_links`

The structured project model's agent-facing surface (`internal/adapter/tools/projectmodel`), replacing the old markdown profile and `update_project_profile`. All three resolve their repository from the run context the way the board tools do, with an optional `repository_id` argument to override it; a call with neither gets an error naming both ways out. None of these is injected into a run's context automatically — an agent calls them on demand.

- **`get_project_brief`** `{component?, area?}` — the repository overview: what the repository (or the named component) is, its stack, its components and their commands, git conventions, reference docs, what runs where, what it talks to, and the CI checks to run before handing off. `component` is a component's own path; `area` is a role like `backend`, ignored when `component` is set.
- **`list_component_checks`** `{component?}` — JSON per component: each CI check's workflow, job, purpose, gate, local command in display form (`cd <dir> && <argv>`), and whether it is required/missing. This is how a run finds the exact command CI runs.
- **`list_links`** `{component?, direction?}` — JSON list of what a component talks to and what talks to it: `direction` is `out`/`in`/`both` (default `both`). Each entry names the other end (`repo/path` for a component, `name (kind)` for a system resource), protocol, env vars, status, confidence and reason.

Granted: all three to `system-architect`, `backend-developer`, `frontend-developer`, `mobile-developer`, `qa-agent` (every role that writes or reviews code) and, narrower, `get_project_brief` + `list_links` to `product-manager`.

### `get_environment`, `query_runtime_logs`, `list_runtime_errors`, `list_deployments`

The runtime picture of a component's environment (`internal/adapter/tools/runtime`), read through `cloud.Service` — the Phase 2 cloud accounts/environments API that replaced the standalone Vercel/GCloud settings. All four resolve their repository the way the project-model tools do (`repository_id` optional, run context otherwise) and their component the same way: `component` is a component's own path, or `.`/omitted resolves to the repository's only component — with more than one and no path given, the error lists every path to retry with.

- **`get_environment`** `{component?, environment?}` — every environment of the resolved component (or just the one named), each with provider, resource, URL, health URL, status and whether it is actually bound (an account + a resource, not just a suggestion or a custom URL). This is the lookup the other three need before they can read anything live.
- **`query_runtime_logs`** `{component?, environment? (default production), since? (default "1h"; a duration like "30m", "2h", "1d"), min_severity?, text?, limit? (default 100, max 500)}` — the environment's own application logs, newest first: timestamp, severity, message, and path/status_code when the entry carries them. A `note` field says when the provider capped the window.
- **`list_runtime_errors`** `{component?, environment? (default production), since? (default "24h")}` — runtime errors grouped and deduplicated (native grouping where the provider has it — GCP Error Reporting — a message fingerprint otherwise): message, count, first/last seen, a `new` flag (first occurrence falls inside this window — the "started with this deploy" signal), and up to the first 10 lines of a sample.
- **`list_deployments`** `{component?, environment? (default production), limit? (default 10)}` — the environment's recent deployments as the provider reports them (status, commit, branch, URL, timestamps), newest first.

The three runtime reads all resolve one *bound* environment first and refuse with the same clear sentence when there is none: `"no <environment> environment is bound for <component> — the human connects it on the repository's Deploy tab"` — never `cloud.ErrNotConnected` verbatim. `get_deploy_logs` (Prod Ops & Deploy Tools, below) is unrelated and stays: it reads a CI/Actions job's output, not the running application's own logs.

Granted: all four to `backend-developer`, `frontend-developer`, `mobile-developer`, `qa-agent`; `get_environment` + `list_runtime_errors` to `system-architect`; `get_environment` alone to `product-manager` (`internal/application/catalog/role_tools.go`'s `roleRuntimeReadTools`).

### `list_task_documents`

Reads the documents attached to a board task, full content, in position order. The read half of `add_task_document`, added by migration 106 to `roleBoardReadTools` and backfilled to every existing agent that already holds `list_task_comments`.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `task_id` | string | no | Board task UUID or its board key. Defaults to the run's own task, like the other task-ref arguments. |

Response JSON: `{task_id, count, documents: [{id, title, content, position, created_by_type, created_by_id, created_at, updated_at}]}`.

It exists for the reason `list_task_comments` does — a kit that ships only the writer gets a model that "checks the documents" by writing one — and it became load-bearing when an analysis stopped committing its spec to the repository. An analysis task's spec and plan are `task_documents` on that task and nothing else: no `docs/` commit, no file on any branch. An implementation task names its analysis with a `derived_from` relation, `board.Runner` reads that analysis's documents into the run's context automatically, and this tool is how a run re-reads a long plan halfway through or looks at an analysis it was not handed.

### Task ordering arguments (`create_board_task` / `update_board_task`)

Three arguments on the task-writing tools, all pointing the same way — **this task comes after the ones you list** — so a model never has to reason about which end of a relation it is holding:

| Argument | Relation written | Enforced by |
|---|---|---|
| `blocked_by: ["T-1"]` | `blocks`, with the BLOCKER as `source_task_id` | `board.WorkOrder` in the dispatcher parks the card on `domain.ResourceWorkOrder`; `board.WorkOrderSweeper` (1 min) releases it when every blocker reaches done/released or disappears. `repository.Service.validateMoveAllowed` additionally refuses a move into `todo`/`in_progress`. |
| `deploy_depends_on: ["T-1"]` | `deploy_depends_on`, source = this task | `repository.Service.deployDependencyGate` refuses the release and comments why. |
| `derived_from: ["A-12"]` | `derived_from`, source = this task | Nothing — it is provenance, not order. It is what feeds the analysis's documents into the run. |
| *(no argument)* | `discovered_from`, source = this task, target = the task the run was working on | Nothing — provenance only. Written automatically by `create_board_task` inside a task run (migration 136) so an agent cannot forget where a task came from; skipped when `derived_from` already names that same task. It never feeds documents: only `derived_from` is read by `AnalysisReferences`. |

`blocked_by` ADDS (a blocker one planner learned must not be dropped by another); `deploy_depends_on` REPLACES (the release path reads the set as a whole). Both refuse a cycle at the point the edge is written, naming the chain that closes it. The ordering is also regenerated into the task's `before_deploy` runbook inside a fenced block — see `.ai/api-spec.md` → "The generated ordering block in `before_deploy`".

### `attach_task_file`

Attach an already-uploaded file (image/document) to a board task. Use when the conversation/task context references an uploaded file that belongs on the task. Part of `roleBoardCreateTools` (product-manager, system-architect); backfilled to existing installs by migration 081.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `task_id` | string | yes | Board task UUID or its board key (e.g. `"T-1"` work, `"B-1"` bug, `"A-1"` analysis) |
| `attachment_id` | string | yes | UUID of the already-uploaded attachment (`POST /v1/attachments`) |

Response JSON is the linked attachment's metadata (`{id, filename, content_type, size_bytes, sha256, ...}`). The task must exist in the resolved repository and the attachment must exist, otherwise the tool errors instead of writing an orphan link.

### `delete_board_task`

Deletes a board task permanently, with its comments, criteria, documents and relations. Product-manager only (`roleBoardDeleteTools`); backfilled to existing installs by migration 092. It exists because the PM previously had no board write meaning "this should not be here": asked to merge three tasks into one, it opened a fourth and left the three standing.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `task_id` | string | yes | Board task UUID or its board key (e.g. `"T-1"` work, `"B-1"` bug, `"A-1"` analysis) |
| `reason` | string | no | Why it is being deleted (e.g. `"merged into DE-4"`). Only trace left once the row is gone. |
| `force` | boolean | no | Delete a task that is no longer in `backlog`/`todo`. |

A task outside `backlog`/`todo` is refused without `force`: the result is `{"deleted": false, "reason", "task", "hint"}`, not an error, and the hint points at `move_board_task`. A successful delete returns `{"deleted": true, id, key, title, column, priority, repository_id, delete_reason}` — the flat `id`/`key`/`title` is what the session action ledger builds the chat's "Task deleted" card from, since the board row no longer exists to read.

### `get_task_pull_request`

Reads the pull request opened for a board task: state (`open`/`closed`, plus
`draft`/`merged`/`mergeable_state`), head and base ref, head SHA, the changed-file
list, the review comments (path/line/author/id/`in_reply_to`), the PR conversation
comments, and a size-capped unified diff. Granted to the developer roles,
`system-architect`, `qa-agent` and `product-manager`; backfilled to existing
installs by migration 088.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `task_id` | string | no | Board task UUID or its board key (e.g. `"T-1"` work, `"B-1"` bug, `"A-1"` analysis). Omit in a task-scoped chat — the task in the run context is used. |
| `include_diff` | boolean | no | Include the unified diff (default `true`). Set `false` when only the state, files or comments are needed. |

Response JSON is `domain.TaskPullRequest`: `{known, number, url, title, state,
draft, merged, mergeable_state, head_ref, base_ref, head_sha, additions,
deletions, changed_files, files: [{path, status, additions, deletions,
previous_path}], review_comments: [{id, author, body, path, line, in_reply_to,
url, created_at}], comments: [...], diff, diff_truncated, note}`.

A task with no PR returns `{"known": false, "note": "No pull request has been
opened for this task yet…"}` rather than an error — an error result reads to a
model as a broken system and it then reports the PR as unreachable instead of
absent. The same holds when GitHub is not connected or the recorded URL carries no
readable number: the link comes back with `note` explaining what is missing. The
diff is capped at 40,000 characters and `diff_truncated` says when it was cut; the
file list is capped at 500 entries and `note` says so when `changed_files` exceeds
it.

### `commit_task_changes`

Commits the task's working copy, pushes it to the task branch, ensures the PR
exists, records `pr_url`/`pr_number` on the task, and returns what landed.
**This is the only way an edit an agent made reaches the pull request** — writing
the file is not enough, because the board runner's post-run commit only happens on
a board run, not in a chat. Granted to the developer roles only: a reviewer that
could commit would put its own name on the branch it is judging.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `task_id` | string | no | Board task UUID or board key. Omit in a task-scoped chat. |
| `message` | string | yes | Commit message describing what changed and why, in the imperative. |

Response JSON is `domain.TaskCommitResult`: `{committed, branch, sha,
changed_files, pr_url, pr_number, message}`.

`committed: false` is a normal result, not an error: either the workspace matches
the branch already (detected by comparing HEAD before and after the commit, so a
change made entirely of new files is not mistaken for no change) or the task has no
working copy on this machine. Returning an error there would make the model retry a
no-op until its iteration budget was gone. Opening the PR is best-effort — GitHub
refuses a PR whose head has no commits beyond base, which is exactly the
nothing-to-commit case — and the failure is reported in `message`.

### `comment_on_pull_request`

Posts a comment on the task's PR, or answers one review comment inside its own
thread. Granted to the developer roles and `system-architect`.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `task_id` | string | no | Board task UUID or board key. Omit in a task-scoped chat. |
| `body` | string | yes | Comment text (markdown). |
| `reply_to_comment_id` | integer | no | Id of the review comment to answer (from `get_task_pull_request`'s `review_comments`). Omit for a new top-level PR comment. |

Replying in-thread matters: a top-level comment leaves the reviewer's thread
unanswered and GitHub keeps showing it unresolved. Errors (rather than a
"nothing happened" result) when the task has no PR, when the recorded URL has no
readable number, or when GitHub is not connected — all three mean the requested
action genuinely could not be performed.

### `merge_task_pull_request`

Squash-merges the task's pull request into its base branch and deletes the task
branch. This is how a finished task's code actually lands: task PRs are opened
ready for review (they used to be drafts, which GitHub refuses to merge at all),
and nothing else in the system merges anything. **Granted to `qa-agent` alone**,
backfilled to existing installs by migration 104 — the developer must not merge
its own branch, the architect reviews it, and the PM signs off on the product
rather than on the git history. QA is dispatched into `done` for exactly this
(see orchestration-agents.md).

| Parameter | Type | Required | Description |
|---|---|---|---|
| `task_id` | string | no | Board task UUID or board key. Omit in a task-scoped chat or a board run — the task in the run context is used. |

Response JSON is `domain.TaskPRMergeResult`: `{merged, pr_number, pr_url,
merge_commit_sha, branch, base_branch, branch_deleted, undrafted, release, message}`.
The merge commit is recorded in `board_tasks.merge_commit_sha` and the board renders
it next to the PR link. A clean merge writes **no** comment (it used to write
`message` every time — the same fact for the third time); only a merge that left
something undone does: an undeleted branch, or a merge commit the task could not
record.

`release` (`domain.ReleaseOpening`) is what the merge set in motion for the
task's component — `{mode, release_id?, status?, released?, unconfirmed?, next}`
— read it, do not guess: `unconfirmed: true` means the component's delivery
profile was never confirmed and the system already commented saying so; mode
`none` means the merge already was the release; `batch` means the merge joined
(or opened) the component's draft release and nothing deploys until a human
cuts it; `on_merge` means the deploy already started, call `watch_release`;
`dispatch` means call `deploy_release`, then `watch_release`. See "Release
tools" below and `.ai/architecture.md` → "Releases (migrations 159–161)".
Before this touches GitHub, an `on_merge` component with a task whose
`before_deploy` steps are not confirmed refuses the merge outright
(`ErrBeforeDeployPending`) — a human must confirm them on the task first.

The refusal matrix — every one of these is an error result carrying "Nothing was
merged. Do not retry", because merging is irreversible and no retry can change
any of them:

| Refusal | Sentinel | Condition |
|---|---|---|
| not signed off | `ErrMergeTaskNotDone` | the task is not in `done` |
| nothing to merge | `ErrMergeNoPullRequest` | no `pr_url` recorded, or the URL carries no readable number |
| already merged | `ErrMergeAlreadyMerged` | `merge_commit_sha` is set, or GitHub reports the PR merged (then the SHA is recorded so `done` stops asking) |
| closed unmerged | `ErrMergeClosed` | the PR was closed without merging |
| checks not green | `ErrMergeChecksNotGreen` | GitHub's `mergeable_state` is anything but `clean`/`has_hooks` (`blocked`, `unstable`, `dirty`, `behind`, `unknown`), or the task's last pipeline `failed`. `dirty`/`behind` are the CONFLICT case: the remedy text tells the release engineer to move the task to `need_revision`, because rebasing a branch is the developer's work |
| review chain incomplete | `ErrReviewChainIncomplete` / `ErrReviewStageRejected` | a required review stage is missing or was rejected — the review chain is always enforced (no repository opt-out), the same `reviewChainGate` the move into `done` runs, re-asked through `repository.Service.CheckReviewChain` |
| head drifted | `ErrReleaseTargetMoved` / `ErrReleaseTargetUnverified` | the PR head SHA is not `board_tasks.verified_sha` — the same `domain.VerifiedCommitMatches` comparison the release gate makes, asked of the PR head rather than of the workspace |
| before-deploy steps pending | `ErrBeforeDeployPending` | the component is `on_merge` and the task's `before_deploy` steps are not confirmed (migration 161) — this one is NOT in the tool's own "do not retry" sentinel list (`isMergeRefusal`), so it surfaces as a plain error without that suffix; the release engineer is woken again once a human confirms, so retrying manually still cannot help |

A missing or `skipped` pipeline does **not** block: that is a repository with no
CI wired up, where refusing would make the merge unreachable, and GitHub's own
mergeable state is then the check that remains. The merge request always carries
the verified head SHA as GitHub's `sha` precondition, so a push that lands
between the gate and the merge produces a 409 instead of merging unreviewed code.

## Release tools

The release engineer's tool surface from `merge_task_pull_request` onward: read
the release covering a task, move it through deploy/watch/verify, and close it
with a verdict. Every tool takes an optional `task_id` (resolved like the other
task tools) and discovers the release covering that task via `ForTask` — a
release is never addressed by its own id from the outside. Registered on the
same condition as the merge tool (`kit.PullRequests != nil`); when
`kit.Releases` itself is nil, every one of them refuses with "the release
service is not configured on this deployment".

### `get_release`

Reads the release covering this task: status, mode/executor, commit and tag,
deploy result (including a batch release's `local_run` or `store_builds`), the
health/smoke/runtime-error evidence gathered so far (`checks`), verdict and
rollback (if any), and a `next` sentence for the current status. Changes
nothing and never parks — call it any time, including right after a merge and
again whenever the current state is unclear. A `batch` component may return a
`draft` release still collecting merged tasks: nothing to do until a human
cuts it. If nothing has opened a release for this task, the error says why
(never merged, the component's mode never opens one, or its delivery profile
is unconfirmed) instead of a bare not-found.

### `deploy_release`

| Parameter | Type | Required | Description |
|---|---|---|---|
| `task_id` | string | no | Board task UUID or board key. |

Dispatches the deploy for this task's release. Meaningful for a `dispatch`
release (the merge did not deploy — this is what tells the workflow to run at
the release tag) and for a `pending` cut `batch` release, where the executor
decides what happens: `github_actions` tags the cut commit (the repository's
own tag-triggered workflow builds and publishes — an "already exists" tag is a
REAL failure here, unlike `dispatch`, since a batch version is never re-used);
`local` runs the profile's command in a detached worktree of the cut commit
and logs it; `store` starts a store build for every platform with a linked
app. Refused, dispatching nothing, when the release is not `pending` or its
mode is neither `dispatch` nor a cut `batch` (`ErrReleaseWrongStatus` /
`ErrReleaseNoDeploy`) — an `on_merge` release already started on its own; call
`watch_release` for it instead. On success call `watch_release` next; never
poll `get_release` waiting for the deploy to finish.

### `watch_release`

| Parameter | Type | Required | Description |
|---|---|---|---|
| `task_id` | string | no | Board task UUID or board key. |

Watches this task's release through its deploy and soak window. While the
release is `Watched()` (deploying, verifying, rolling back) this call PARKS
the task and ends the run — that is correct and expected, never poll, wait or
sleep. The release sweeper watches it in the background and re-dispatches the
task the moment a verdict is needed (`awaiting_verdict`) or something failed;
you (or the next run) are woken with the answer. Call it right after
`merge_task_pull_request` for an `on_merge` release, and right after
`deploy_release` for a `dispatch` or cut `batch` one. Once it returns a result
instead of parking, read `next` and follow it (the same sentence
`get_release` gives for that status).

The park is surfaced through a `domain.ResourceBlock{Resource: "release_watch"}`
exactly the way the old `get_task_deploy_status` surfaced `deploy_watch` — the
runner parks the card and `Dispatcher.deployWatchWake` accepts
`resumed_resource == "release_watch"` alongside the legacy `"deploy_watch"`.

### `run_smoke_checks`

| Parameter | Type | Required | Description |
|---|---|---|---|
| `task_id` | string | no | Board task UUID or board key. |

Runs this release's frozen smoke checks (GET/HEAD only, read-only) against its
verify target right now and reports each result plus a one-line pass/fail
summary — a way to double-check before `finish_release`, or see what the
automatic soak window already found. It does not change the release's status
and does not replace reading `query_runtime_logs`/`list_runtime_errors`. No
smoke checks configured returns an empty result, not a failure.

### `finish_release`

| Parameter | Type | Required | Description |
|---|---|---|---|
| `task_id` | string | no | Board task UUID or board key. |
| `note` | string | yes | What you checked: the log window, the error groups (or their absence), the smoke results. Becomes the release's verdict. |

Confirms this release as shipped: moves every task it carries to `released`.
This is the ONLY way a task may reach `released` — never move the card there
yourself. Callable only while the release is `awaiting_verdict`, and only
after `query_runtime_logs`/`list_runtime_errors` since `deployed_at` have
actually been read in THIS run wherever the component has a bound runtime
environment. A `failed` release can only be finished by a human overriding it
("ship it anyway") — an agent call is refused (`ErrReleaseWrongStatus`).

### `rollback_release`

| Parameter | Type | Required | Description |
|---|---|---|---|
| `task_id` | string | no | Board task UUID or board key. |
| `reason` | string | yes | `deploy_failed`, `verify_failed`, or `health_incident`. |
| `note` | string | yes | What was actually observed — the failing step, the error, the health check that went red. |

Rolls this release back off production: reverts its merge commits on the
default branch, then redeploys the previous good release (`dispatch`) or lets
the revert push itself redeploy (`on_merge`) — a bound production environment
whose provider supports it (Vercel, Cloud Run) is rolled back FIRST, in
seconds, before the revert lands. For a `batch` release it only reverts the
default branch — nothing is redeployed, because a published desktop or store
build cannot be unpublished by a revert; `rollback.manual_steps` then leads
with unpublishing or halting the artifact itself. Call it only on EVIDENCE,
never a hunch, and never for a noisy but PRE-EXISTING error. If the
component's `auto_rollback` is off, nothing executes: the proposal is written
on the task for a human to confirm and the result carries `{proposed: true}` —
stop there, it is a successful call, not a refusal
(`ErrRollbackNeedsHuman`). On an actual rollback, perform or explicitly report
EVERY entry in `rollback.manual_steps`, then call `watch_release` to follow
the rollback deploy. Refused (nothing reverted, nothing redeployed) when the
release is not `failed`, `awaiting_verdict`, or `released` within 24h and
still the component's newest released release (`ErrReleaseWrongStatus`).

See `.ai/architecture.md` → "Releases (migrations 159–161)" for the full state
machine, the provider-rollback mechanism and the Vercel promote trap, and the
before/after-deploy runbook.

### `get_deploy_logs`

Reads the log behind a deploy. **Granted to `release-engineer` alone.** This is
a CI/Actions job's output — a bound environment's own live logs and grouped
errors come from `query_runtime_logs`/`list_runtime_errors` instead (above).

| Parameter | Type | Required | Description |
|---|---|---|---|
| `task_id` | string | no | Board task UUID or board key. |
| `source` | string | no | `actions_job` (default) or `logs_url`. |
| `job_id` | integer | no | Actions job id, from `get_release`'s `release.deploy.failed_job`. Omit to default to this task's RELEASE's failing job (`Releases.ForTask(...).Deploy.FailedJob`), falling back to the legacy per-task deploy watch only when the task has no release. |
| `env` | string | no | Which environment's `logs_url` to read (default `prod`). `source: logs_url` only. |

The result is a **summary**, not a dump: the error-looking lines are lifted out
first (deduped, capped at 40) and the tail follows them, within ~6,000
characters. A blind tail loses the stack trace whenever the job prints a cleanup
summary after failing; a raw dump costs the run's whole context budget to deliver
the same four lines.

`source: logs_url` reads `repository_deploy_targets.logs_url` — an endpoint the
**application itself** serves. `health_url` only ever answers "it is up"; a
deploy that came up and is logging a failed migration on boot is invisible to it.
The destination is re-validated through `urlguard` on **every** fetch, not only
when it was written, because the field is agent-writable and a name that resolved
to a public address at save time is free to answer `127.0.0.1` later.

There is no cluster and no cloud log API anywhere in this — no `kubectl`, no
`gcloud`, no provider SDK ([CLAUDE.md](../CLAUDE.md)). An HTTP endpoint the
application serves is a fact about the application, not about where it is hosted.
An agent running on a **local runner** may additionally use its own shell to
inspect whatever tooling that host happens to provide; that capability comes from
the machine, not from this codebase, and nothing here depends on it.

The release tools resolve the task from the `task_id` argument **or** from the
run context (`registry.ContextWithTaskID`, set for every turn of a task-scoped
chat), and then resolve the repository the same way the other task tools do —
so a `task_id` from another repository is not-found instead of another
repository's PR. `merge_task_pull_request` rides with the workspace tools in
`domain.workspaceToolAllowPatterns` rather than the read-only uplift, and is
deliberately **not** in the uplift's own list — an uplift may widen what a
stale agent row can see or change in its own workspace, never what it can land
on the default branch. `domain.RestrictToolsForStage` (the `strip_writers`
stage behaviour, not the old `RestrictToolsForVerdictColumn`) strips it and the
release-control tools on every stage except the ones whose `allow` param names
them — `done` keeps both, `released` keeps only the release-control tools; see
`.ai/orchestration-agents.md` → "`done` / `released` = the columns where the
release is watched".

## MCP Tools

MCP tools are named `mcp_<server_id>_<original_tool_name>`. Their parameters and descriptions are sourced directly from the MCP server's tool definitions.

To see which MCP tools are active: `GET /v1/tools`.

To add a new MCP server, add an entry to `tools.mcp_servers` in `resources/config.yml` and restart the bridge.

### Serving OUR tools to a Claude Code session (`/mcp`)

The other direction: `adapter/mcpserver` hands a CLI session the registry's tools as
`mcp__tasktrooper__<name>`. The session is always a local `claude` child process, so the URL
given to it is always `http://127.0.0.1:<bound port>/mcp`, passed as a `--mcp-config` file
written here, loopback-only, with a token that dies with the process and is bound at mint
to the run's own context.

A repository's checked-in `.mcp.json` is never loaded (`--strict-mcp-config` on every run) and
nothing here reads or records one — a profile naming those servers would promise tools the run
excludes on purpose.

## Prod Ops & Deploy Tools

Registered when the board and Postgres are wired (`internal/adapter/tools/ops`).

| Tool | Purpose |
| --- | --- |
| `list_incidents` | List live production incidents (filter by repository / env / status). `repository_id` optional — omitted lists every repository. |
| `get_incident` | Full incident: raw alert payload, timeline, occurrences, current remedy. |
| `propose_incident_remedy` | Record the diagnosis: kind, summary, executable steps, evidence, confidence. |
| `resolve_incident` | Close an incident after verifying the environment recovered. |
| `list_deploy_templates` | The deploy recipe catalog (GCP Cloud Run/GKE, AWS ECS/Lambda, Vercel, Fly). |
| `load_deploy_template` | Full recipe: workflow YAML, required secrets, smoke check, rollback. |
| `get_deploy_target` | How a repo ships to an env (provider, vars, health URL) + rendered recipe. `repository_id` optional. |
| `update_deploy_target` | Record where an env actually answers after a deploy: `base_url` / `health_url` / `logs_url` / `app_url` only. Creates a minimal hand-rolled target when the env has none; never touches provider, template or vars. `repository_id` optional. |
| `record_local_deploy` | Report a break-glass deploy run from a machine (`scripts/release-local.sh`, `scripts/deploy-local.sh`) so Operations → Deployments still shows what is live. Call twice: `status=in_progress` before, `status=completed` + `conclusion` + the returned `run_id` after. Records only — deploys nothing. `repository_id` optional but strict (writer). |

`repository_id` is **optional** on those three, and an omission means something different on each: `list_incidents` lists every repository, while `get_deploy_target` and `update_deploy_target` use the repository of the task this run is working on. A value that is not a UUID (agents pass the repository *name*) resolves to that same run repository on the two readers — but `update_deploy_target` **writes**, so there it is refused rather than guessed at: a guess would record one repository's URL against another's target with nothing in the transcript to show it.

`record_local_deploy` exists because the deploy matrix mirrors GitHub Actions
runs, and a deploy driven from somebody's machine while Actions cannot run
(billing, outage — see each repo's `.ai/local-deploy.md`) leaves that mirror
showing a stale run as if it were current. Its rows are ordinary
`deployment_runs` with `trigger_source=local` and a **negative** `run_id`
(`domain.LocalRunID`), which is what keeps them out of GitHub's id space; the
finish call refuses any positive id, so it cannot rewrite a real Actions run.
The UI renders such a run's SHA as plain text — there is no Actions page to link
to — and tags it `local`.

`logs_url` (migration 105) is the newest of those address fields and the only one
that is *read* rather than polled: `get_deploy_logs` fetches it after a deploy.
It gets the same treatment as `health_url` in every respect that matters —
agent-writable, offline `urlguard` check on write, and re-validated on **every**
fetch. It must point at an endpoint the application itself serves; there is no
cluster or cloud-provider log source anywhere in this repository and there must
never be one.

## `mobile_*` — this machine's devices

Eleven tools, all driving `mobile.Pool`: a run's first `mobile_*` call walks the registered
devices and takes the first free one; every later call in that run goes to the same phone. Only
when every registered device is taken does a call report busy.

| Tool sees | Cause | Result |
|---|---|---|
| the device is busy | Appium's own 4xx on a second session | `ResourceBlock{mobile_device}`, released by `board.DeviceSweeper` |
| no Appium / no Android SDK | the catalog's `capabilities.*.detail` | a tool error carrying that sentence — no park; nothing frees itself |

`mobile_release_device` deletes the Appium session and deliberately LEAVES the simulator running —
the next task wants the device this one warmed.

`codebase_search`, `expand_symbol_context` and `get_symbol_skeleton` read the index (Postgres), then
layer an **unindexed-edit overlay** on top: `buildOverlay` shells `git -C <session workspace> diff`
and returns the changed files' bytes as `Snippet`, so uncommitted edits show up without a re-index.
A stale index (migration 116) refuses with `domain.ErrIndexEmbeddingStale` rather than returning
results a different embedding model produced.
