# API Specification

All endpoints require `Authorization: Bearer <api_key>` unless noted; auth accepts
`server.api_key` (legacy) or any key in `server.api_keys[]`. `X-Request-ID` is accepted
and echoed (generated when omitted).

## Chat, models, tools

### POST /v1/chat/completions

Runs the agent loop with optional tool policy, session continuity and RAG context.

```json
{
  "model": "local",
  "messages": [{"role": "user", "content": "List files in /tmp"}],
  "stream": false,
  "session_id": "optional-uuid",
  "tool_policy": {"allow_tools": ["web_search"], "deny_tools": ["run_terminal"]},
  "file_ids": ["uuid-of-uploaded-file"]
}
```

OpenAI-compatible `chat.completion` response; `stream: true` returns SSE chunks for the
final assistant message.

### POST /v1/llm/providers/{type}/connect | /test

Both resolve `base_url`, `default_model` and `timeout_seconds` on the same ladder as
`resolveTimeoutSeconds`, with one deliberate difference:

| Field | `/test` | `/connect` |
|---|---|---|
| `base_url` | request → **stored** → definition default | request → definition default (an empty field on the form means "reset me") |
| `default_model` | request → stored → definition default | same |

`/test` skipping the stored row is what made it dial `127.0.0.1:1234` — the "Custom
(OpenAI-compatible)" default — for a provider connected on `127.0.0.1:11234`, and report the
refusal there as the user's. `/connect` accepts `default_model` although the form never sends
one: the custom provider's definition default is empty, so an API caller had no way to set the
fallback model and no sign that the value it sent had been dropped.


### GET /v1/llm/embedding-models | /v1/llm/embedding-status

`?provider=` on the first takes a catalog type or an `llm_endpoints` uuid. Anything
else is refused as "this provider cannot produce embeddings" (`endpointRef` checks the
ref parses as a uuid before it reaches Postgres). The same guard
covers `PUT`/`DELETE /v1/llm/endpoints/{id}`.

`/embedding-status` returns `{provider, model, dimensions}` (`domain.EmbeddingStatus`):
the resolved values, never the raw stored `""`. `dimensions` is set only when `model`
is the pinned local embedder's `nomic-embed-text-v1.5` (768) — the embedder child
process the desktop app spawns (see `docs/architecture.md`); otherwise it is 0 and the
client does not know the vector length until an index reports it. A separate call from
`GET /v1/llm/providers` because it's a cheap, independent read, not because of any
network round trip.

### GET /v1/models

Proxies the active provider's model list. `?provider=<type|endpoint uuid>` lists that
provider instead — this fills the web's model picker.

```json
{"object": "list", "data": [{"id": "opus", "object": "model", "label": "Opus (latest)"}]}
```

`label` is optional, present only where ids are not self-explanatory.

**Host-executed providers are answered from a constant.** `claude_code` is a binary on
the runner host with no endpoint to query, so the handler short-circuits on
`domain.ModelsForHostExecutedProvider` and serves `domain.ClaudeCodeModels()`:

| id | meaning |
|---|---|
| `""` | send no `--model`; the operator's CLI default decides |
| `fable` / `opus` / `sonnet` / `haiku` | latest of that family |
| `opus[1m]` / `sonnet[1m]` | 1M-token long-context variants |

Curated and static, because no CLI subcommand enumerates models and the CLI does not
validate `--model` — a bogus alias reaches the API verbatim and costs a run.
`haiku[1m]`, `fable[1m]` and `opusplan` work but are deliberately not offered (see the
`ClaudeCodeModels` doc comment).

### GET /v1/tools · GET /v1/tools/health

Definitions filtered by the merged policy (config default + API key + request), and
per-server connectivity:

```json
{"servers": [{"id": "browser", "connected": true, "tool_count": 12},
             {"id": "github", "connected": false, "last_error": "connect: ..."}]}
```

## Sessions and jobs

| Endpoint | Notes |
|---|---|
| `POST /v1/sessions` | `{title, model}`. Requires PostgreSQL |
| `GET /v1/sessions/{id}` | `session` + `messages` (each with its `attachments`) |
| `POST /v1/sessions/{id}/messages` | `{role, content, model, tool_policy, file_ids, attachment_ids}` |
| `POST /v1/sessions/{id}/cancel` | Optional `{reason}` → `{"cancelled": true}` |
| `DELETE /v1/sessions/{id}` | 204, messages included |
| `POST /v1/jobs` | Async agent run; 202 with `status: pending`. Requires PostgreSQL |
| `GET /v1/jobs/{id}` · `GET /v1/jobs/{id}/result` | `pending`/`running`/`completed`/`failed`/`cancelled`; result JSON once completed |
| `DELETE /v1/jobs/{id}` | Cancels pending/running, deletes completed |
| `GET /v1/audit?limit=50` | Recent tool-call audit entries. Requires PostgreSQL |

Cancel: `cancelled: false` means there was nothing left to stop (the answer landed
first) — normal, not an error. 404 unknown session, 400 malformed id. The stopped run
ends as `cancelled` (a later terminal write cannot overwrite it), whatever was streamed
is persisted as the assistant message, and the SSE stream ends with a normal
`finish_reason: "stop"` frame plus `[DONE]`.

## Files and attachments

| Endpoint | Notes |
|---|---|
| `POST /v1/files` | Multipart `file`; chunked and embedded for RAG, stored under `DATA_DIR/files` |
| `GET /v1/files` · `DELETE /v1/files/{id}` | Delete removes chunks and disk storage |
| `POST /v1/attachments` | Multipart `file` + optional `repository_id`; binary attachments stored as BYTEA |
| `GET /v1/attachments/{id}` | Raw bytes, stored `Content-Type`, `Content-Disposition: inline`, `Cache-Control: private, max-age=31536000, immutable` |
| `DELETE /v1/attachments/{id}` | Task/message links cascade away |
| `GET /v1/repositories/{id}/tasks/{taskId}/attachments` · `POST` (same path) | `{attachments, count}`; POST `{attachment_id}` links an uploaded one (task existence verified first) |
| `DELETE .../attachments/{attachmentId}` | Unlinks without deleting the attachment row |

Attachments: max 10 MB (413 over), content-type allowlist (png/jpeg/webp/gif, pdf,
plain/markdown/csv, json, zip, docx, xlsx) validated after server-side sniffing (415
otherwise). The POST returns metadata (`{id, filename, content_type, size_bytes,
sha256, …}`), never bytes, and GET needs the normal Authorization header — the web UI
fetches blobs and uses object URLs rather than pointing `<img src>` at it. Attachments
are not injected into the LLM context. Single-task detail responses carry `attachments`
alongside `documents`.

## Operational endpoints

| Endpoint | Notes |
|---|---|
| `GET /metrics` | `bridge_requests_total`, `bridge_tool_calls_total`, `bridge_agent_iterations`, `bridge_llm_latency_seconds`, `bridge_mcp_errors_total` |
| `GET /docs` | OpenAPI 3.0 YAML |
| `POST /admin/reload` | Reloads config, reconnects MCP servers; no HTTP restart |
| `GET /health` | No auth; bridge + LM Studio status |
| `GET /v1/usage?days=30` | Token usage aggregates (totals, by-model, daily) from `llm_usage`. `days` 1-365 |

## Board run control

Both return the run under a `run` key (`{"run": {...domain.TaskAgentRun...}}`), answer
`503 service_unavailable` with no board runner wired, and `404 not_found` when the run
does not exist or belongs to a different task than the URL names.

- `POST /v1/repositories/{id}/tasks/{taskId}/runs/{runId}/cancel` — stops a
  `pending`/`running` run and parks the task **blocked** with the reason; recovery is a
  human dragging the task onto a column, which releases the block and lets that column's
  agent take it through normal dispatch. Optional `{reason}`. Returns the cancelled run;
  `409 conflict` when it already stopped.
- `POST /v1/repositories/{id}/tasks/{taskId}/runs/{runId}/rerun` — queues a new run of the **same agent** on the task's
  **current** state. No column move; the run named in the URL stays untouched history.
  Returns the new (`pending`) run; `409` while the run is still going or the task is
  blocked — a blocked task is recovered by moving it, not by re-running in place.

Neither goes through the board dispatcher: cancel would immediately fan fresh agents
onto the task it just stopped, and re-run would start every agent configured for the
column rather than the one asked for. Each records its own board event
(`task.run_cancelled` / `task.rerun_requested`).

## Roles, task types and workflows (migration 143)

Roles, task types and their per-column workflow stages are data now, not hardcoded agent
names / `TaskType*` constants / column literals — see `internal/domain/{role,workflow,
workflow_behaviour}.go` and `internal/application/workflow`. Dispatch reads an in-memory
snapshot (`application/workflow.Service`, both `port.WorkflowReader` and
`port.RoleResolver`), reloaded after every write; an empty/never-loaded snapshot fails
every read CLOSED rather than answering as if a type or stage were simply absent.

- `GET /v1/roles` → `{roles:[{id,key,name,description,required_tools:[],
  assignments:[{agent_id,agent_name,areas:[]|null,priority}],purposes:[]}]}`. `areas: null`
  means "any area"; a specific list narrows the assignment to those repo areas
  (`backend`/`frontend`/`mobile`).
- `POST /v1/roles` `{key,name,description,required_tools}` → `201` role. `key` is
  immutable once created (task-type assignee and system-purpose references are keyed off
  the id, but the key itself never changes underneath a stored reference).
- `PUT /v1/roles/{id}` `{name,description,required_tools}` → role.
- `DELETE /v1/roles/{id}` → `204`.
- `PUT /v1/roles/{id}/assignments` `{assignments:[{agent_id,areas,priority}],
  confirm_grant_tools}` → `200 {saved:true,role,granted_tools?:{<agent_id>:[..]}}` when
  every assigned agent already carries (or was just granted) the role's `required_tools`,
  else `422 {saved:false,missing_tools:{<agent_id>:[..]}}` — the same
  confirm-then-grant shape `PUT /v1/settings/analiz-assignment` already used, generalized
  off a role's own tool list instead of the fixed analiz one.
- `GET /v1/agents/{agentId}/roles` → `{roles:[{role_id,key,name,areas}]}`.
- `PUT /v1/agents/{agentId}/roles` `{roles:[{role_id,areas}],confirm_grant_tools}` — the
  agent-centric write of the same relationship, replacing every role membership this
  agent holds in one call; same `200`/`422` tool-grant shape, checked against the union of
  every named role's `required_tools`.
- `GET /v1/role-purposes` / `PUT /v1/role-purposes/{purpose}` `{role_id|null}` — the
  `system_task_assignee` (who `CreateWorkflowSetupTask`/deploy/repodocs/prodops hand their
  own system-opened tasks to) and `repo_profiler` (who a repository notes pass goes
  to) hooks. `role_id: null` clears the hook; nothing resolves for it until set again.
- `GET /v1/task-types` → `{task_types:[{key,label,key_prefix,position,is_default,
  is_defect,assignee_role_id,assignee_mode,behaviours:[{key,params}],built_in,
  task_count}]}`. `assignee_mode` is `none` (task/bug/technical today — the requested
  assignee, or none, stands), `default` (fills from the role only when nothing was
  requested) or `override` (the role's agent for the task's area always wins when the role
  has one there — analiz's behaviour today).
- `POST /v1/task-types` `{key,label,key_prefix,clone_from?}` → `201`. `clone_from` copies
  another type's whole workflow-stage set onto the new type.
- `PUT /v1/task-types/{key}` `{label,key_prefix,position,is_default,is_defect,
  assignee_role_id,assignee_mode,behaviours}` → type; `409` when `key_prefix` changes on a
  type that already has tasks (every existing task's key was rendered with the old
  prefix — see `postgres/repository.go`'s `taskKeySQL`, which now reads the prefix from
  `task_types` instead of a hardcoded switch).
- `DELETE /v1/task-types/{key}` → `204` | `409` (has tasks, or is the board's default
  type).
- `GET /v1/task-types/{key}/workflow` → `{task_type,stages:[{id,column_slug,position,
  on_path,kind,behaviours:[{key,params}],instructions,
  participants:[{role_id,mode,instructions,position}]}]}`. A board column with no stage
  row for a type carries no behaviours and routes to the task's assignee — the same
  fallback a custom column gets today.
- `PUT /v1/task-types/{key}/workflow` `{stages:[…]}` → `200` (the saved set) |
  `422 {error,problems:[{column_slug,field,message}]}` on an unknown behaviour key, a
  behaviour attached at the wrong scope (a `(type)`-scoped one on a stage, or vice versa),
  an invalid/missing param, a `column` param naming a slug the board does not have, or more
  than one `worker`/`approver` participant in a stage.
- `GET /v1/workflows` → `{workflows:[{task_type,stages}]}` — every type's full workflow in
  one call.
- `GET /v1/workflow/behaviours` → `{behaviours:[{key,scope,label,description,
  params:[{name,type,options?,required}]}],kinds:[..],areas:["backend","frontend",
  "mobile"]}` — the registry (`domain.BehaviourRegistry`) driving both the stage editor's
  behaviour picker and its own validation; `scope` is `stage` or `type`.
- `GET /v1/agents/{agentId}/subscriptions` now also returns
  `subscriptions:[{column_slug,task_types:[]|null}]` alongside the legacy `column_slugs`
  array (`task_types: null` = every type). `PUT` accepts either body: `subscriptions` with
  the per-column filter, or the legacy `column_slugs` (each column saved with no filter,
  unchanged from before). This is also the fix for a standing bug: saving through the
  agent-scoped endpoint used to silently drop `task_type_filter` on every write.
- `PUT /v1/board/columns` (`UpdateBoardColumns`) now refuses (`409`) removing a column
  slug that a workflow stage with behaviours still references — `workflow_stages` carries
  no FK to `board_columns` on purpose (`ReplaceColumns` deletes every row and reinserts on
  every save), so this refusal is the only thing standing between a rename and an orphaned
  stage.

Task keys: `T-n` / `A-n` / `B-n` / `TC-n` render from `task_types.key_prefix` now, not a
hardcoded Go switch — a custom type's own prefix works the moment the type exists, with no
further migration.

## POST /v1/repositories/{id}/tasks/{taskId}/chat

Opens — or reopens — the thread a human discusses ONE board task in, returning the
session to send messages to plus the agent answering there:

```json
{"session_id": "0f9b…", "agent_id": "3c4d…"}
```

Idempotent: one thread per task for its whole life, so pressing "discuss" again returns
the same `session_id`. An existing clarification thread becomes the task thread rather
than a second one being opened. `agent_id` is `""` when no agent is assigned and nobody
is subscribed to the column — the chat still works, it just has no persona. `404` for an
unknown repository or a task the repository does not own (repository scope is the
ownership check), `400` for a malformed id, `503` without a board.

It differs from an ordinary repository chat in two ways:

- Its workspace is the task's own branch checkout (`<workspace_root>/task-<taskId>` on
  `feature/<task-key>`), not the shared mirror clone, so changes can be committed and
  pushed to the task branch and reach the pull request. A checkout that cannot be
  prepared fails the turn instead of silently falling back to the mirror.
- Every turn carries compact task + PR context (key, title, column, description,
  technical notes, acceptance criteria, branch, PR number/url) and the three PR tools
  (`get_task_pull_request`, `commit_task_changes`, `comment_on_pull_request`). The diff
  is deliberately not in the prompt; it is fetched per request through the tool.

**PR fields live on the task** (migration 088): `pr_url` / `pr_number` are written the
moment a PR is opened (`ensurePullRequestAsync`, the pipeline's pre-gate ensure, the
code-review ensure, `commit_task_changes`) — before this the URL survived only as the
text of a system comment. Task PRs are opened **ready for review**, not drafts (migration
104): GitHub refuses to merge a draft and nothing ever un-drafted them. Nothing gates on
draft state; `get_task_pull_request` still reports the `draft` field and the merge tool
un-drafts a legacy draft as a repair step. `merge_commit_sha` (migration 104) is the
squash commit the PR produced, written only by `merge_task_pull_request`: it tells the
dispatcher a task in `done` needs no QA wake, and is the commit the follow-up deploy
watch keys off rather than the default branch.

## Repository registration

- `POST /v1/repositories/open`, `POST /v1/repositories/import` and
  `POST /v1/repositories` accept an optional `kind` (`backend|frontend|mobile|worker|monorepo`; invalid → 400). Omitted, it is
  detected from the working copy (Flutter/Xcode markers → mobile; several projects under
  `apps/`/`packages/` → monorepo; a react/vue/svelte/vite/next `package.json` →
  frontend; otherwise backend) and persisted with the row.
- `PATCH /v1/repositories/{id}` accepts `kind` too. `name` and `description` are applied
  unconditionally on this route: an omitted description clears the stored one.
- `PATCH /v1/repositories/{id}` also accepts `release_engine` (`auto|github_actions|local`):
  which machine builds/uploads a mobile store release. `auto` (default) means GitHub Actions,
  falling back to the paired Mac; see `domain.ErrNoReleaseEngine` under Store operations.
- `POST /v1/repositories/{id}/restore` — re-clones a registered repository from its
  recorded `remote_url` into **this** runtime's layout (`<workspace>/repos/<name>`) and
  re-points `root_path`. 202 with the repository; the clone runs in the background and is
  reported by `git_restore` (`{status: running|completed|failed, root_path, error}`) on
  any later read, so a client polls `GET /v1/repositories/{id}`.

  Refused with 400 (the message is the reason, nothing on disk is touched) unless the
  folder is *genuinely missing* — the four-state `GitPresence`, so "exists but is not a
  repository" and "unreadable" both refuse — and a remote is recorded. `git_restorable`
  on every repository read is that rule pre-computed, and is what the UI's button keys
  off. The clone is `port.GitClient.CloneRepo`, the same call the GitHub import makes, so
  private repositories work through the same token injection.

## Deploy targets

- `GET /v1/deploy/templates?kind=backend` — recipe catalog (bodies stripped);
  `GET /v1/deploy/templates/{templateId}` — one recipe in full.
- `GET /v1/repositories/{id}/deploy/config` — saved targets + fitting templates + missing
  vars per env, plus `detected_app_identity: {bundle_id, package_name}` (migration 125):
  the store identifiers read off the working copy at import, for prefilling a store
  target's `bundle_id`/`package_name`. Both keys always present, `""` = not readable.
  Persisted, not read on demand — detection runs where the code is, this endpoint is
  served by a host that may hold no copy. Scoped like `kind`: with `sub_project_path`
  it is that sub-project's own (from the `sub_projects` JSON), never the repository's.
- `PUT /v1/repositories/{id}/deploy/targets` — upsert one env: `{env, provider,
  template_id, vars, health_url, logs_url, base_url, app_package, app_url,
  auto_rollback}`. `health_url` and `logs_url` are destination-guarded (urlguard) before
  storage; `logs_url` is an endpoint the application itself serves, read by the deploy
  watch after a deploy (migration 105).
- `DELETE /v1/repositories/{id}/deploy/targets/{env}`.
- `GET /v1/repositories/{id}/deploy/targets/{env}/instructions` — recipe rendered with the target's vars.
- `POST /v1/repositories/{id}/deploy/targets/{env}/setup-task` — opens the board task that authors the
  deploy workflow.

## Cloud accounts, environments & runtime (migration 154, Phase 2)

Replaced the Vercel connection/hosting-links/GCloud settings surface this
section used to document — `/v1/settings/vercel*`, `/v1/vercel/*`,
`/v1/gcloud/*`, `/v1/repositories/{id}/{hosting,vercel,gcloud}/*` are gone. An
existing
connection and its per-repository bindings carry forward once, in the
background, as `cloud_accounts`/`component_environments` rows
(`cloud.Service.Boot`) — nothing for the operator to redo. JSON shapes are the
Go tags on `domain.CloudAccount`, `CloudResource(Ref/Detail)`,
`ComponentEnvironment`, `EnvironmentRuntime`, `RuntimeLog(Query/Entry/Page)`,
`RuntimeErrorGroup`, `CloudDeployment` (`internal/domain/cloud.go`). Errors are
flat `{"error": "message"}`, never the nested shape the rest of this file
uses: 400 bad input, 400 `{error, code:"cloud_auth"}` for a provider refusing
the stored credential, 404 unknown id, 409 `{error, code:"not_connected"}` for
an environment with no bound account/resource, 502 the provider/network is
unreachable.

- `GET/POST /v1/cloud-accounts` — list (never carries secret fields back); create
  `{provider: "vercel"|"gcp"|"aws", label?, fields}` (vercel `{token, team_id?}` · gcp
  `{service_account_json}` · aws `{access_key_id, secret_access_key, session_token?,
  region}`), verified against the provider BEFORE anything is stored.
- `PATCH /v1/cloud-accounts/{id}` `{label?, fields?}` — re-verifies only when `fields` is
  given. `POST …/{id}/verify` re-checks the stored credential (200 either way; a refusal
  is reported through `status`/`status_detail`, not an HTTP error).
  `DELETE /v1/cloud-accounts/{id}` — environments bound to it keep their rows, lose the
  account.
- `GET /v1/cloud-accounts/{id}/resources?refresh=1` — the account's resource listing,
  cached 60s.
- `PUT /v1/components/{componentId}/environments/{env}` (`env` ∈
  `production|staging|preview|development`) `{account_id?, resource?, url?, health_url?}` —
  with `account_id`, `resource` is required (picked from the account's resources); without,
  `url` is required (a custom, log-less environment).
  `PATCH /v1/environments/{envId}` `{status?: "confirmed"|"dismissed"|"suggested",
  account_id?, resource?}` — confirming one of a suggested row's `candidates` is sending
  its `account_id` + `resource.ref` with `status: "confirmed"`.
  `DELETE /v1/environments/{envId}` — a user-made binding is deleted outright; a
  scan-sourced one is only dismissed, so the next scan does not resurrect it.
- `GET /v1/environments/{envId}/overview` — `{environment, detail?, deployments[],
  errors[], unavailable?}`; never errors for "not connected" or a provider auth
  refusal, both go through `unavailable` instead.
- `GET /v1/environments/{envId}/logs?since=&until=&min_severity=&q=&limit=&cursor=` —
  `since`/`until` RFC3339, `min_severity` ∈ `debug|info|warning|error|critical`, defaults
  last hour / 200 entries.
  `GET …/errors?since=` (default 24h) — `{errors: RuntimeErrorGroup[]}`, provider-native
  grouping where there is one (GCP Error Reporting), a message-fingerprint grouping over
  error-level logs otherwise.
  `GET …/deployments?limit=` — `{deployments: CloudDeployment[]}`.
  `POST …/errors/task` body one `RuntimeErrorGroup` (as returned) — opens a bug task with
  the error, a sample and recent log lines; component set from the environment.
- `RepositoryModel.environments` (`GET /v1/repositories/{id}/model`) and
  `RepositorySummary.environments` (`GET /v1/projects/overview`, from stored health only)
  carry the same rows for the UI; agents read the equivalent through `get_environment` /
  `query_runtime_logs` / `list_runtime_errors` / `list_deployments`
  (`.ai/tool-reference.md`).

## Deploy metadata, ordering relations, packages

**Task fields** `before_deploy`, `after_deploy`, `rollback_plan` (nullable markdown) ride
on `domain.BoardTask` and are accepted by `POST /v1/repositories/{id}/tasks` and
`PATCH /v1/repositories/{id}/tasks/{taskId}` (the usual optional-pointer contract: omitted leaves the value, `""`
clears it). `before_deploy` + `rollback_plan` are posted as ONE system comment when
`TriggerRelease` dispatches the deploy; `after_deploy` when the prod deploy finalizes
successfully. Empty fields post nothing.

**Relations.** Four types, all reachable from the task API:

| Field | Direction | Write semantics | Enforcement |
|---|---|---|---|
| `deploy_depends_on: [{target_task_id \| target_key}]` | source = this task | PATCH **replaces** (`[]` clears, omitting changes nothing); POST via `relations` | `TriggerRelease` 400s with `domain.ErrDeployDependencyNotReleased` and comments the blocking keys while any target lacks production evidence |
| `blocked_by: [{target_task_id \| target_key}]` | stored as `blocks` with the BLOCKER as `source_task_id` (migration 022) | **ADDS** — a blocker one planner learned must not be silently dropped by another | Work-order park; a move into `todo`/`in_progress` is refused |
| `derived_from` | source = implementation task, target = the analiz task | via `relations` or `create_board_task` | None — provenance, not order |
| `discovered_from` | source = the new task, target = the task the run was working on | written automatically by `create_board_task` inside a task run (migration 136); no tool argument | None — provenance, not order |

- A task may not depend on itself (400). A **cycle is refused where the edge is
  written**, not at release, naming the chain that closes it (`deploy-order cycle
  refused: T-2 already ships after T-1 (T-2 → T-1)`).
- Production evidence = a successful `prod_deploy`, a successful `preprod_deploy` on a
  repo with no prod workflow mapped, or the `released` column. The gate runs after the
  migration gate and before the mobile-store and release-target gates.
- `GET /v1/repositories/{id}/tasks/{taskId}` returns both directions: `relations` (edges
  this task is the source of) and `blocked_by` (the `blocks` edges pointing at it, with
  `source_key` / `source_title`). Single-task detail only; bulk lists carry neither.
- `derived_from` is a route, not an order: an analiz task's deliverable is
  `task_documents` on that task and nothing else — no `docs/` commit, no file on any
  branch — so without it an implementation task's specification is unreachable from the
  work it specifies. `Service.AnalysisReferences` resolves it to
  `[]domain.AnalysisReference{Key, Title, Documents}` and `board.Runner.analysisContext`
  renders it into every run of the task.

### The generated ordering block in `before_deploy` (migration 106)

`before_deploy` is part agent-authored, part generated, fenced by `domain.OrderNoteOpen`
/ `OrderNoteClose` (literal `<!-- tt:order -->`):

```
<!-- tt:order -->
**Release order (generated from this task's relations — do not edit by hand):**
- Ships after: T-1 (task export endpoint). Each one must be live in production before this
  task is released; the release is refused otherwise.
- Built after: T-1 (task export endpoint). Work on this task does not start until those are done.
<!-- /tt:order -->

Confirm the export feature flag is off in prod.
```

Regenerated by `Service.syncOrderNote` wherever the order can move: after `POST .../tasks`
writes relations, after `PATCH` replaces `deploy_depends_on` or adds `blocked_by`, and
inside `TriggerRelease` immediately before the pre-deploy checklist is posted. Only the
fenced region is replaced, so an agent-written runbook survives. Generated rather than
agent-written because a typed-in ordering drifts the moment the relation is edited and
would then assert an order the release gate does not enforce. The checklist comment
carries the same text with the markers stripped.

### Deploy packages

The release path for a repository with `auto_release_on_done` off, which otherwise has
none (`trigger_release` refuses it as "batched release"). A package release skips only
that flag; every other release gate still applies per task.

| Endpoint | Notes |
|---|---|
| `GET /v1/repositories/{id}/deploy-packages` | `{packages, count}`. **Not a pure read**: each package is advanced first — members with production evidence marked, an all-live package becomes `released`, members whose dependencies just became satisfied are dispatched. Members carry `{task_id, position, key, title, column, released}` |
| `POST /v1/repositories/{id}/deploy-packages` | `{name, description?}` → 201, status `draft` |
| `PATCH /v1/repositories/{id}/deploy-packages/{pkgId}` | `{name?, description?, status?}`; `status` accepts only `"cancelled"` (every other transition is evidence-driven → 400), cancelling an already-`released` package 400s, unknown package 404 |
| `DELETE /v1/repositories/{id}/deploy-packages/{pkgId}` | 204; membership cascades, tasks untouched |
| `PUT /v1/repositories/{id}/deploy-packages/{pkgId}/tasks` | `{task_ids: []}` replaces membership, array index becomes `position`. 400 on a task from another repository or a `releasing`/`released` package |
| `POST /v1/repositories/{id}/deploy-packages/{pkgId}/release` | 202 with the package. Allowed from `draft` or `failed` (a retry). Members are topologically sorted by their in-package `deploy_depends_on` edges, position breaking ties; a cycle fails the package with `domain.ErrDeployPackageCycle` in `note` and dispatches nothing. Only the FIRST wave is dispatched and the package stays `releasing` — deploys are async, so later waves ride the advancement on each GET. A member with production evidence is skipped, not an error; the first hard refusal moves the package to `failed` with `note = "<task key>: <error>"` |

## Production incidents

| Endpoint | Notes |
|---|---|
| `POST /v1/repositories/{id}/incidents` | Alert webhook: Alertmanager, Sentry, GCP Cloud Monitoring, or generic `{title, severity, env, detail, fingerprint, resolved}`. `?env=` / `?source=` fill what the payload omits; a recovery payload resolves the matching incident (202 when nothing matched) |
| `GET /v1/incidents?repository_id=&env=&status=open,triaging` | List |
| `GET /v1/incidents/{incidentId}` | Incident with its timeline |
| `POST /v1/incidents/{incidentId}/triage` | Re-run the remedy engine |
| `POST /v1/incidents/{incidentId}/remedy` | `{kind, summary, steps, evidence, confidence, rollback}` |
| `POST /v1/incidents/{incidentId}/resolve` · `POST /v1/incidents/{incidentId}/ignore` | — |
| `PUT /v1/repositories/{id}/incident-policy` | `{incident_policy: off\|suggest\|auto_fix}` |

## Store operations

| Endpoint | Notes |
|---|---|
| `PUT /v1/store/credentials/{provider}` | `provider`: `asc` (`{key_id, issuer_id, p8}`) or `google_play` (`{service_account_json}`), wrapped in `{"data": …}`. Validated against the console (`ValidateAuth`) before anything is persisted: 204 ok, 400 invalid input or rejected credential, 500 backend/config failure (e.g. no secrets cipher) |
| `GET /v1/store/credentials` | `[{provider, configured, updated_at}]` — one row per known provider even when never saved (`configured:false`, zero `updated_at`); never the payload |
| `DELETE /v1/store/credentials/{provider}` | 204; 400 on an unknown provider |
| `GET /v1/repositories/{id}/store/apps` | `[]domain.MobileStoreApp` — the repo's per-platform store lifecycle rows |
| `POST /v1/repositories/{id}/store/apps/{platform}/verify` | Re-runs `VerifyOnboarding` against the store API now instead of waiting for the next monitor sweep; returns the resulting row |
| `GET /v1/store/credentials/{provider}/apps` | Picker source: `{listing_available, apps: []port.StoreAppRef}`. 200 with `listing_available:false` (never 5xx) when the store cannot enumerate — the Play Developer API has no listing endpoint at all and a service account may not reach the separate Reporting API that does |
| `PUT /v1/repositories/{id}/store/apps/{platform}/link` | Body `{identifier, store_app_id, name}` — binds the repo/platform to one app from the picker; returns the updated `MobileStoreApp` |
| `GET /v1/repositories/{id}/store/apps/{platform}/tracks` | Reads the three channels (internal/external/production) live from the console and refreshes the row's `tracks` cache; returns `domain.StoreTracks` |
| `POST /v1/repositories/{id}/store/apps/{platform}/promote` | Body `{from, to, confirm}` — one channel forward at a time (`domain.NextChannel`: internal→external→production); `to=production` requires `confirm`; 409 `ErrAppNotReady` |
| `POST /v1/repositories/{id}/store/apps/{platform}/build` | Body `{engine?}` (`auto`\|`github_actions`\|`local`, default the repo's `release_engine`); 409 `domain.ErrNoReleaseEngine` when neither engine can run — the card is parked on `human_decision`, not retried; 409 `storeops.ErrBuildTargetUnknown` when the working copy named no Xcode scheme / Gradle module (migration 127) |

Mobile release now runs through `application/storeops/pipeline`-generated
`scripts/mobile-release.sh` (one script, called by a thin workflow wrapper) —
the four platform deploy templates (`ios-app-store`, `android-google-play`,
`flutter-app-store`, `flutter-google-play`) and `mobile-qa-build` are deleted.
Publishing is store-link + app-pick (above); mobile QA verification is now the
repository's own `local_run` doc (`scripts/dev.sh`).

## Verification settings & environment inventory

- `PUT /v1/repositories/{id}/test-strategy` — `{test_strategy: local|stage|per_step}`:
  `local` runs workspace tests only, `stage` (default) deploys to staging when a task
  enters ready_for_qa, `per_step` also deploys at code_review.
- `GET /v1/repositories/{id}/env-inventory` — variable **names** declared by the repo's
  `.env.example`-style files (root + two levels for monorepos; real `.env` files are
  never read) plus the deploy targets, so "what does this need and where does it answer"
  is one call. Targets carry `base_url` next to `health_url`.
- `PUT /v1/repositories/{id}/lifecycle-gates` — `{require_review_chain?,
  require_release_deploy?, require_pipeline_for_review?}`, all optional; an omitted field
  is left as it was. Returns the updated `domain.Repository` (every flag round-trips).
  400 on an invalid id or a failed update.

  The first two default `false`; **`require_pipeline_for_review` defaults `true`**
  (migration 107), deliberately: the others are new requirements a repository opts INTO,
  while this is behaviour that has always been on and is now opt-OUT-able. It is also the
  only one gating a **dispatch** rather than a **move** — on, a task entering
  `code_review` waits for the build/test pipeline before the reviewing architect is
  dispatched; off, dispatch is immediate and the board event carries `pipeline_gate:
  gate_disabled` so a card that skipped the gate is never mistaken for one that passed
  it. Turn it off for a repository whose CI cannot answer (no Actions minutes, checks this
  server cannot read). Even on, the wait is bounded
  (`board.pipeline_gate_timeout`).

  Arming the other two changes what a later move accepts, not this call.
  `require_review_chain`: moving into `done` (or `released` when that skips `done`) 400s
  unless the column-span history shows every review stage the type requires —
  `code_review`, `in_qa`, `pm_uat` for `task`/`bug`, `analiz_review` for `analiz` — with
  none of those stages' latest visit rejected; the message names the missing/rejected
  stage and the move that earns it (`done means the task passed its review chain, and
  this one has not … Missing: QA (in_qa) — move it to ready_for_qa`).
  `require_release_deploy`: moving into `released` 400s unless `task_pipelines` holds a
  successful `prod_deploy` (or `preprod_deploy` on a repo with no prod workflow mapped);
  a `skipped` pipeline is never accepted and `analiz` tasks are exempt. Both fail closed
  (400, not 200) if their evidence store cannot be read, so only enable a gate the board
  can satisfy — see [orchestration-agents.md](orchestration-agents.md) and
  [architecture.md](architecture.md) for the deadlock each can cause.

## GitHub webhook

- `POST /v1/github/webhook` — **public path, no bearer auth.** The per-repository HMAC
  over the raw body (`X-Hub-Signature-256`) is the entire authentication story:
  `verifiedWebhookRepo` resolves the delivery's repository by `repository.full_name` and
  verifies before anything that costs money (a pull, an embedding call, a GitHub
  round-trip). An unknown repository → `204`, so GitHub stops retrying; a bad or missing
  signature → `401`; otherwise `202 {accepted, reason}`, where `reason` is diagnostic and
  shows up in GitHub's delivery log.

  Three `X-GitHub-Event` values are handled; anything else (`ping`, `installation`, …) is
  acknowledged with `204` and ignored.

  - `push` to the default branch debounces a reindex + a project-model rescan (trigger `push`).
  - `workflow_run`, `check_suite`: a **completed** run resolves the task pipelines waiting
    on its `head_sha` (`HandleGitHubWorkflowEvent` → `PipelineRunner.ResolveByHeadSHA`) —
    the `task_pipelines` row is written and the board acts on it: success/skipped hands
    the card to its reviewer, failure moves it to `need_revision`. A run that is not yet
    completed, a payload with no head SHA, or a redelivered `X-GitHub-Delivery` is
    acknowledged and does nothing. Most deliveries legitimately match no waiting pipeline
    (`reason: "no pipeline is waiting on this commit"`), which is not an error.

- `POST /v1/repositories/{id}/webhook` — one-click install/rotate: mints a fresh secret
  and `PATCH`es or `POST`s the hook so GitHub agrees with both the stored secret and
  `githubapi.WebhookEvents` (`push`, `workflow_run`, `check_suite`). Returns the updated
  `domain.Repository` (`webhook_installed`); 400 with the reason when the repository has
  no GitHub remote, GitHub is not connected, or `server.public_base_url` is unset.
  Existing hooks are also repaired at boot, without this call and without rotating any
  secret.

## Project model

Components, checks, links, resources, notes and scans — the full route list and
payloads are in [projects.md](projects.md) and the domain JSON tags
(`internal/domain/project_model*.go`, `project_map.go`):

- `GET /v1/projects/overview`, `GET /v1/projects/{id}/overview`, `GET /v1/projects/map`,
  `GET /v1/projects/{id}/map`
- `GET /v1/repositories/{id}/model`, `GET /v1/repositories/{id}/brief`
- `POST /v1/repositories/{id}/scans` (202; 409 while one runs),
  `GET /v1/repositories/{id}/scans/latest`, `GET /v1/scans/{id}`
- `POST /v1/repositories/{id}/components`, `PATCH /v1/components/{id}`,
  `POST /v1/components/{id}/checks`, `PATCH|DELETE /v1/checks/{id}`,
  `POST /v1/links`, `PATCH|DELETE /v1/links/{id}`,
  `PUT /v1/repositories/{id}/notes`, `PATCH|DELETE /v1/notes/{id}`

PATCH bodies distinguish an absent field (leave), `null` (revert to detected) and a
value (set the override). Errors are flat `{"error": "…"}`.

## Embedding map (UMAP source data)

The browser runs UMAP (umap-js); the backend picks the right chunk embeddings,
L2-normalizes them and reduces the dimensions before they cross the wire — a raw
3072-float embedding × 2000 chunks is ~25 MB of JSON, the same points at 50 dimensions
~400 KB.

- `GET /v1/embedding-map/sources` — what can be visualized, so the UI can build its
  selectors:

  ```json
  {
    "files": {"available": true, "chunk_count": 412, "document_count": 7},
    "repositories": [
      {"id": "uuid", "name": "local-llm", "branch": "main", "index_id": "uuid",
       "chunk_count": 8321, "file_count": 640, "indexed_at": "2026-08-05T10:00:00Z"}
    ]
  }
  ```

  Only repositories with a workspace index holding at least one chunk appear. A
  repository indexed on several branches contributes **one** row, picked deterministically
  (most chunks, ties to the most recently indexed, then index id). `indexed_at` is `null`
  for an index that never completed.

- `GET /v1/embedding-map?source=files|code&repository_id=&limit=&dims=` — the projection.

  - `source` is required; anything other than `files`/`code` is `400`. `repository_id` is
    required (and must be a uuid) when `source=code`, ignored for `files`.
  - `limit` defaults to 2000, `dims` to 50. Out-of-range values are **clamped** (limit
    100…5000, dims 2…128), never rejected — they are viewer controls.
  - `dims` is additionally capped by the source's own embedding width (a 64-wide
    embedding answers `dims=128` with `"dimensions": 64`), so the response's `dimensions`
    is authoritative; never assume it equals what was asked.

  ```json
  {
    "source": "code", "repository_id": "uuid-or-empty-string", "branch": "main",
    "dimensions": 50, "total": 8321, "sampled": 2000, "truncated": true,
    "points": [
      {"id": "uuid", "group_id": "uuid-or-path", "group_label": "apps/web/src/api.ts",
       "chunk_index": 0, "snippet": "first ~200 chars, whitespace-collapsed",
       "language": "typescript", "symbol": "fetchTasks", "vector": [0.12, -0.4]}
    ]
  }
  ```

  `vector` is exactly `dimensions` long on every point. For `source=files`, `group_id` is
  the `files.id` and `group_label` the filename; for `source=code`, both are the
  repository-relative file path, `chunk_index` is a 0-based ordinal within that file
  (start line, then id), and `language` / `symbol` carry the chunk's tree-sitter metadata
  (empty string when unknown).

  `total` is what is stored, `sampled` what came back. When `total > limit` the rows are
  sampled **evenly** over a stable ordering — a step of `floor(rn·limit/total)` over
  `(path, start_line, id)` — not the first N, which would show only the
  alphabetically-first files, and the same request returns the same points. `sampled` can
  fall below the sampled row count when a stored embedding is unusable (zero, NaN/Inf, or
  a different width after an embedding-provider switch); such rows are dropped rather
  than plotted at the origin. An empty source is `200` with `"points": []` and
  `"total": 0`, not an error — including `source=code` for a repository that was never
  indexed.

Reduction is PCA computed in-process (`application/embedmap`): L2-normalize, mean-center,
then block power iteration with Gram-Schmidt deflation against the covariance action
`Xᵀ(Xv)` — the d×d covariance matrix is never materialized. It is deterministic down to
the last bit, including the parallel decomposition, so a client may cache a projection
and diff it against a later one.

## Assignee field: omitted vs `null` vs a value

`PATCH /v1/repositories/{id}/tasks/{taskId}` reads `assignee_agent_id` as `domain.Nullable`:

| Spelling | Effect |
|---|---|
| key omitted | leave whoever is on the card alone |
| `null` | unassign |
| a value | assign that agent |

`null` used to decode to the same nil pointer as an omitted key, so it was a clear that
silently did nothing — including the board's own "unassign this agent" control.

## Machine-readable refusals

Every code below appears in **both** `error.type` and a top-level `code`, so a client reads
one place for the refusal reason.

| Code | Status | Meaning |
|---|---|---|
| `provider_unavailable` | 409 | a declared-but-not-built provider was named (`cursor_agent`, `antigravity`) |
| `host_executed_provider` | 409 | `claude_code` was asked to behave like an endpoint — connect/test/activate. It's a CLI process this server execs directly, not a network endpoint, so there is nothing to dial |
| `invalid_catalog_input` | 400 | agent-catalog validation (`POST\|PUT /admin/agents`, `.../skills`, `.../rules`): `name is required`, `content is required`, `effort must be one of …`, `max_turns cannot be negative`. The sentence is the whole explanation and is rendered verbatim |
| `unknown_agent_cli_flavor` | 400 | `POST /v1/agent-cli/{flavor}/connect` or `DELETE /v1/agent-cli/{flavor}` named no CLI at all. A flavor that IS known but not built answers 409 `provider_unavailable` instead |

The first two were previously `500` — a permanent refusal that told every client and every
monitor to retry. The last two already answered 400 and were the stragglers: right status,
no code. Written by `adapter/http.permanentRefusal` / `typedBadRequest` / `codedBadRequest`,
the first two reached from `internalError` and `badRequestErr`, so any route that propagates
the error gets the same answer.
