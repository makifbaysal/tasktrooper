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
             {"id": "github", "connected": false, "last_error": "connect: ..."},
             {"id": "figma", "connected": false, "auth_required": true, "last_error": "connect: ... Unauthorized"}]}
```

`auth_required` appears when the server answered 401 (with no usable OAuth token). `?all=true`
on `/v1/tools` lists every registered tool, MCP access modes included (the admin picker).

## MCP servers (migration 179)

| Endpoint | Notes |
|---|---|
| `GET /v1/mcp/servers` · `GET /admin/mcp-servers` | `{servers, count, oauth_redirect_uri}`. Each server: config (secrets masked), `access`, `status` (`connected`\|`disabled`\|`needs_config`\|`error`), `auth` (`none`\|`oauth_connected`\|`oauth_needed`\|`oauth_expired`), tools. `oauth_redirect_uri` is `http://127.0.0.1:<port>/oauth/mcp/callback` (empty without OAuth) |
| `POST /admin/mcp-servers` · `PUT /admin/mcp-servers/{id}` · `DELETE …` | Body takes `access`: `all` or `listed`. Create defaults to `listed`; an update without `access` keeps the stored one; anything else is 400. Changing `url` or `transport` deletes the server's OAuth sign-in |
| `POST /v1/mcp/servers/{id}/oauth/start` | Optional `{client_id, client_secret}`. Discovers the authorization server, registers a client dynamically when none was given (reused while the issuer and redirect URI are unchanged), returns `{authorization_url, expires_at}` (PKCE S256, `resource` = the MCP server, single-use `state`, 10 min). 400 `invalid_request_error` for a stdio server; 400 `oauth_client_required` when the authorization server has no registration endpoint and no client id was given; 502 `upstream_error` when discovery or registration fails |
| `GET /oauth/mcp/callback` | **Public** (no bearer — see `isPublicPath`): the browser returning from the authorization server. Consumes `state` first (unknown/expired/replayed → 400 page), checks `iss` when present, exchanges `code` with the verifier, stores the tokens encrypted, reconnects MCP servers in the background. Answers a self-contained HTML page (`Content-Security-Policy: default-src 'none'`, `no-store`) |
| `POST /v1/mcp/servers/{id}/oauth/disconnect` | 204. Deletes the tokens; a dynamically registered client is dropped, a hand-entered one kept |

Access: an agent's tool policy with a non-empty `allow_mcp_servers` gets exactly those
servers; an empty one gets only `access = 'all'` servers. Applied in
`application/registry` to the tool list, to execution and to the `/mcp` endpoint alike.
Rows that existed before migration 179 are `all`; seeded templates are `all`.

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
| `GET /metrics` | `bridge_requests_total`, `bridge_tool_calls_total`, `bridge_agent_iterations`, `bridge_llm_latency_seconds`, `bridge_mcp_errors_total`; request series are labelled by route pattern (`/v1/tasks/:id`), not the raw path |
| `GET /docs` | OpenAPI 3.0 YAML |
| `POST /admin/reload` | Reloads config, reconnects MCP servers; no HTTP restart |
| `GET /health` | No auth; bridge + provider status. Provider probes are reused for 10 min (2 min after a failure) and dropped when a provider or endpoint changes; `?fresh=1` probes now |
| `GET /v1/tasks` | The board. Strong `ETag` + `Cache-Control: no-cache`; a matching `If-None-Match` gets `304`, answered without reading the board while this process has written nothing it shows, for at most 10 s (another host on the same database is seen within that). Each task carries `agent_running` |
| `GET /v1/releases` | Every repository's releases newest first (Operations → Changes). Filters `repository_id`, `component_id`, `status` (comma list), `before` (RFC3339 `created_at` cursor), `limit` ≤ 100 |
| `GET /v1/usage?days=30&tz=Europe/Istanbul` | Token usage aggregates from `llm_usage`, split into `generation` (kind api+cli) and `embedding` totals, plus `by_model` (grouped by kind/provider/model) and `daily` (generation only, bucketed by local day in `tz`). `days` 1-365, `tz` an IANA name (empty/invalid → UTC) |

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

## Code review verdicts (migration 180)

Every enabled agent subscribed to `code_review` is a required reviewer (by default the
system-architect and the security-agent). An agent's `move_board_task` out of
`code_review` is recorded as that reviewer's verdict in `task_review_verdicts` —
`ready_for_qa` (the stage's exit) is `approve`, `need_revision` is `reject` — and the
card stays put until every required reviewer has decided. The last verdict moves it:
to `need_revision` when anyone rejected, to the exit otherwise. A person's move is never
held. A park to `blocked` does not split a round.

- `GET /v1/repositories/{id}/tasks/{taskId}/reviews` →
  `{column: "code_review", rounds: [{round, entered_at, left_at?, outcome, reviewers: [{agent_id?, agent_name, verdict, decided_at?, required}]}]}`.
  `outcome` is `open` / `approved` / `rejected` / `closed`; `verdict` is `approve` /
  `reject`, or `pending` for a required reviewer of the open round that has not decided.
  `404 not_found` for an unknown task.

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
  own system-opened tasks to) and `repo_profiler` (defined for future use; currently has no
  runtime consumer) hooks. `role_id: null` clears the hook; nothing resolves for it until set
  again.
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
- `POST /v1/repositories` (create an empty repository) names the local folder with the
  same sanitized name GitHub gets (`domain.SanitizeRepoName`: lower-case, `[a-z0-9._-]`,
  spaces → `-`; nothing usable left or longer than 100 → 400), always a single path
  component under `<workspace>/repos`. An existing folder, or a registered repository
  whose folder is missing, → 400. When any step after the folder was made fails
  (git init, GitHub create/push, registering the row, linking projects), the folder and
  any half-registered row are removed so the same name can be retried; if the GitHub
  repository was already created the 400 message says so and names it
  (`domain.RemoteRepoCreatedError`, else the working copy's origin).
- `POST /v1/repositories/new` — create a brand-new repository **from the person's answers
  instead of a scan** (`application/newrepo`). Body:
  `{name, owner?, description?, project_ids?, role, stack?, notes?, scaffold, docs?}` —
  `role` is a component role (`frontend|backend|mobile|desktop|worker|library|infra|cli|other`),
  `docs` ⊆ `coding_standards|test_standards|architecture|local_run` (duplicates ignored).
  Everything is validated before anything touches disk or GitHub. Then: the repository is
  created exactly as `POST /v1/repositories` creates one (same naming and cleanup, `kind`
  = the role's legacy kind) but **no scan starts** — there is no code yet; the first push
  to the default branch scans it through the existing webhook (`HandleGitHubPush` →
  `RefreshAfterPush`) or, on a desktop install, the freshness poll (`SweepIndexFreshness`
  → `RefreshAfterPush`), neither of which needs an earlier scan; a repository's first-ever
  scan runs with trigger `import` even when a push woke it, so what it finds is not queued
  for review as a later addition. The root component `.`
  is added with the chosen role, name = the repository name, and its docs set to
  `DefaultRepoDocPath(kind)` for each chosen kind. When `scaffold` is true or `docs` is
  non-empty, one board task "Set up <name>" is opened (todo, medium, created by system,
  assignee = `system_task_assignee` for the role's area, linked to the root component)
  whose description carries the answers verbatim and asks for, on one branch in one PR:
  the runnable skeleton (preferring a `search_boilerplate_catalog` starter), each doc
  written at its default path PRESCRIBING conventions for the stack
  (`repodocs.NewRepoDocInstructions`; `local_run` is the bootstrap script), and
  `CLAUDE.md` + `AGENTS.md` with a docs index.
  → 201 `{"repository": Repository, "component_id": uuid, "task": {"id","key","title"} | null}`
  (`task` null only when nothing was asked for). 400 with the reason for validation and
  git/GitHub failures (nothing left on disk); 500 when the repository was created but
  adding the component or opening the task failed — the message says the repository exists.
- `POST /v1/repositories/open` on a folder that is **already registered** starts a scan
  only when that repository has never been scanned (`RefreshIfNeverScanned`); one that
  has a scan is left alone, as before.
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

## Releases (migrations 159–161, 190)

A component's delivery profile decides whether a merge deploys on its own (`on_merge`),
waits to be dispatched (`dispatch`), collects into a release a human cuts (`batch`), or
ships nothing (`none`) — see [projects.md](projects.md) → "Component delivery". Every
route below sits behind the same bearer auth as everything else; every WRITE additionally
requires `confirm` to equal the repository's name, the same guardrail
`handler_deployops.go`'s dispatch/rollback routes use (`errReleaseConfirmMismatch` → 400).

| Endpoint | Notes |
|---|---|
| `GET /v1/repositories/{id}/releases?component_id=&task_id=&limit=` | `{"releases": domain.Release[]}`, newest first; default limit 20, max 100 |
| `GET /v1/releases/{releaseId}` | One release in full: status, mode/executor, commit/tag, `deploy` (incl. a batch release's `local_run`/`store_builds`), `checks` (health/smoke/new error groups), `verdict`, `rollback`, `tasks`, and the rollback state below |
| `POST /v1/releases/{releaseId}/rollback-proposal/dismiss` | No body, **no confirm** (nothing in production changes) → `Release`: clears `rollback_proposal` and records `rollback_dismissal`. Idempotent — a release without one (or whose proposal was replaced meanwhile) answers as it is. 404 unknown release |
| `GET /v1/releases/{releaseId}/cut-preview` | `domain.ReleaseCutPreview` for a `draft` release, or a `pending` one `deploy_release` was never attempted on (a re-cut, e.g. after `ErrReleaseTagExists`): suggested/previous version (the HIGHEST semver among the component's last released version, its newest matching git tag, and any tag any of its releases ever carried), the tag it would carry, the commit it would cut at, generated notes, its tasks. 409 `ErrReleaseWrongStatus` off draft/re-cuttable-pending, 409 `ErrReleaseEmpty` with no tasks |
| `GET /v1/releases/{releaseId}/impact` | `domain.DeployImpact` `{release_id, component_id, environment: "production", deployed_at, window_minutes: 60, pending, verdict, computed_at, signals: [{signal, unit, before, after, after_points, baseline, delta, delta_pct, higher_is_better, verdict}]}` (`application/deployimpact`). T is `deployed_at` (`finished_at` for a release that never deployed on its own); for each golden signal of the component's role (`catalog/observability/golden.yaml`) with a confirmed production binding, read through `signals.ReadSignal` — a quantile signal at p95 alone — and judged by its worst series at each instant where it answers several (the highest where lower is better, the lowest where higher is, as the alert evaluator does): the mean over [T−60m, T) (`before`), (T, T+60m] (`after`; the sample at T itself still straddles the deploy, so it is left out; `after_points` is how many were averaged) and that after-window 24h earlier (`baseline`). A signal is `worse`/`improved` when it moved past `deploy_ops.impact` both from `before` and, when read, from `baseline` — else `neutral`: `change_pct` of `before` (`throughput_change_pct` for a rate where higher is better), or `ratio_points` percentage points for a ratio signal; a count where lower is better must also move by `count_floor` (gauge) or `rate_floor` (rate), each at least `floor_pct` of the larger value, and a quantile in ms by `latency_floor_ms`. `unknown` when unreadable. `higher_is_better` is the catalog's `better` (`signals.yaml`). `verdict` is the worst judged signal, `unknown` when none was; every bound signal failing to read is the answer's error. `pending` while T+60m is in the future: `after`/`baseline` cover what has passed. A fully read answer is kept per (release, minute). 404 unknown release; 200 `verdict: "unknown"`, `signals: []` when nothing is bound, the release has no component or never deployed |
| `POST /v1/releases/{releaseId}/cut` | `{confirm, version, notes}` → `Release`, `draft → pending` (or re-cutting a `pending` release that never deployed); re-reads and freezes the component's CURRENT confirmed delivery profile, stamps every carried task's before-deploy confirmation, wakes the release engineer. 400 invalid version (`domain.ValidReleaseVersion` — also enforces git ref rules: no `..`, no trailing `.`, no `.lock` suffix, no `@{`, no `//`); 409 `ErrDeployDependencyPending` while any carried task ships after an unreleased dependency |
| `POST /v1/releases/{releaseId}/deploy` | `{confirm}` → `Release`. `pending` only; a `dispatch` release dispatches its workflow, a cut `batch` release runs its executor (tag+watch / local command / store build). Claims the release before the side effect; a side effect that started nothing at all (a transient dispatch/start error) returns it to `pending` with the error rather than `failed`, so the call may simply be retried — only a definitive refusal (CI unavailable, or the workflow/tag trigger itself invalid) becomes `failed` |
| `POST /v1/releases/{releaseId}/finish` | `{confirm, note}` → `Release`, verdict recorded, every task moved to `released`. Same states `finish_release` accepts (`awaiting_verdict`, or `failed` — a human overriding it) |
| `POST /v1/releases/{releaseId}/rollback` | `{confirm, note}` → `Release`, always `domain.RollbackManual` (an agent's own reasons — `deploy_failed`/`verify_failed`/`health_incident` — only come from the `rollback_release` tool). Claims the release first; refused when the release is not `failed`/`awaiting_verdict`/`released`-within-24h-and-still-newest, or when a newer release of the component is already open or has since shipped |
| `POST /v1/repositories/{id}/tasks/{taskId}/before-deploy/confirm` | No body. Stamps `board_tasks.before_deploy_confirmed_at` (`COALESCE`, idempotent) and, when the task sits in `done`, wakes the release engineer on it immediately (`release.Service.WakeTask`) instead of waiting for the next sweep |
| `PATCH /v1/components/{componentId}` | Existing `ComponentPatch` plus `"delivery": domain.ComponentDelivery \| null` — `null` clears the human override back to the detected profile. A save that newly CONFIRMS the profile (crossing into an override, or a detected exact/high) opens releases for every task of that component already sitting in `done`, merged, with no release yet (`OpenPending`), and wakes any of the component's `done` tasks that never merged at all A save that switches `gates.coverage_enabled` or `gates.mutation_enabled` from off to on on an active component reads its CI checks (`discovery/ci.SplitByGate`, per language of the component's stack) and, when a language has no step for that gate, opens one `todo` task per gate (`projectmodel.gate_ci_task`, system task assignee for the component's role) and records it in `gates.coverage_ci_task_id` / `gates.mutation_ci_task_id` (server-owned; a patch never sets them). While that task is not done/released/deleted, a later switch reports it instead of opening another. The response is the component plus `gate_ci_tasks: domain.GateCITask[]` (omitted when empty) |
| `POST /v1/components/{componentId}/smoke-checks/test` | `{"checks": domain.SmokeCheck[]}` (1..20, same validation `ComponentDelivery.Validate` runs) → 200 `{"base_url": string, "results": domain.SmokeResult[]}`. Runs a DRAFT set of checks right now against the component's resolved production base URL (confirmed environment, else the legacy prod deploy target, else `""` — same resolution a release's verify window uses) with the same runner/policy/timeout `run_smoke_checks` uses. Nothing is persisted — the UI's "test before you save" action while editing a delivery profile. 400 `ErrInvalidDelivery` (including an empty list); 404 unknown component |
| `POST /v1/components/{componentId}/smoke-checks/generate` | `{"existing": domain.SmokeCheck[]}` → 202 `Job`. Starts an AI draft of smoke checks: the `release-engineer` agent reads the component's code read-only (read_file/grep_code/glob/codebase_search, in the repository checkout) and proposes GET/HEAD checks; the server validates each, drops duplicates of `existing`, caps at 8, then test-runs the survivors like `/smoke-checks/test`. Returns the already-running job for the component instead of starting a second. 404 unknown component; 409 no enabled release-engineer agent with a runtime |
| `GET /v1/smoke-check-generations/{jobId}` | → 200 `Job` `{job_id, component_id, status: running\|done\|failed\|cancelled, agent_name, started_at, finished_at?, checks: SmokeCheck[], results: SmokeResult[], base_url, dropped, error}` — `results[i]` belongs to `checks[i]`, arrays never null; a `done` job can have zero checks with `error` saying why. Jobs live in memory, 5-minute run timeout, evicted 30 minutes after finishing (then 404). Nothing is saved to the delivery profile |
| `DELETE /v1/smoke-check-generations/{jobId}` | → 204. Cancels a running generation; idempotent |

`domain.SmokeCheck`: `method` (`GET`\|`HEAD`), `path` (relative to the env URL, or an
absolute http(s) URL), `expect_status` (0 = any 2xx/3xx), `contains` (substring the body
must hold), `name` (optional label, ≤80 chars), `max_latency_ms` (0 = no limit, else
1..10000 — a response slower than this fails the check with `error: "slower than <N> ms
(<latency> ms)"`, checked after status/contains so the first real failure reason wins).

**Rollback state on every release JSON** (the list routes, `GET`, and every action's
answer — cut, deploy, finish, rollback, dismiss):

- `rollback_proposal`: `null` or `{proposed_at, by: "agent", reason, note?, incident_id?}` —
  the `rollback_release` tool called while the release's frozen profile has `auto_rollback`
  off (`reason` is the tool's `deploy_failed|verify_failed|health_incident`, `note` what it
  observed, `incident_id` the incident the release was attributed when the reason is
  `health_incident` and this process attributed one). Stored (`releases.rollback_proposal`,
  migration 190) BEFORE the `rollback_suggested` notification goes out, so its approve
  action (`/operations/changes?release=<id>`) always finds it. Written only by
  `ReleaseStore.SetRollbackProposal` (only on a `failed`/`awaiting_verdict`/`released`
  release; it never moves `updated_at`), never by the general update; cleared when the release
  moves to `rolling_back`/`rolled_back`/`superseded`, or is finished into `released` (in the
  update itself), when a newer release of the same component is finished (`Finish` clears the
  older ones), and by the dismiss route. A proposal stored since the last hand-back also stops
  the watchdog re-waking the release engineer: the release waits on a human.
- `rollback_dismissal`: `null` or `{dismissed_at, incident_id?}` (migration 192) — a person
  dismissed the proposal; `incident_id` is the one it answered. Written only by
  `ReleaseStore.DismissRollbackProposal`, which clears the proposal in the same statement and
  only if it is still the one read (compare on `proposed_at`). Until an incident other than that
  one is attributed to the release, the agent's `rollback_release` with `auto_rollback` off is
  refused (`domain.ErrRollbackProposalDismissed`, `guard.release_rollback_proposal_dismissed`)
  and nothing is stored or told. A dismissal since the last hand-back counts as its answer for the
  watchdog, like a proposal.
- `rollback_allowed`: what `POST …/rollback` would do right now, by the server's own rule
  (computed on read, never stored; one store read per component per list).
- `rollback_refusal`: `""` when allowed, else `newer_shipped` (a newer release of the
  component is live), `newer_open` (a newer one is pending/deploying/verifying/awaiting a
  verdict), `not_deployed` (`draft`/`pending`/`deploying`), `too_old` (released more than
  24h ago), `no_released`/`unresolved` (the component's newest released release is missing
  or unreadable), `wrong_status` (`verifying`, `rolling_back`, `rolled_back`, `superseded`),
  `not_configured` (this server has no git reverter; every rollback starts with the revert).

An automatic rollback (`auto_rollback` on, agent) whose later step fails after the revert
landed on the default branch (a redeploy refused, the record not saved) still sends
`release_rolled_back`, its body naming what did not finish.

`domain.DeliveryVerify` also takes `max_error_rate_delta` (percentage points, 0 = not
checked, else up to 100) and `max_p95_delta_pct` (percent, 0 = not checked, else up to 1000);
anything else is `ErrInvalidDelivery`. With either set, the soak reads the release's deploy
impact (above) at most every 2 minutes and once more at the window's end, and records it in
`checks.impact` `{checked_at, pending, verdict, signals: [{signal, before, after, delta,
delta_pct, verdict, max_delta?, max_delta_pct?, breached?, settling?}], unchecked?}` — `unchecked`
names a threshold whose signal (`http.error_ratio`, `http.latency`'s p95) is unbound or has no
delta. The read runs under a 15s timeout. A threshold breaches only once the after-window is
at least 10 minutes long AND averaged at least 5 samples (`settling: true` until then, never
breached), and only when the signal moved past it from yesterday's same hour too when that
was read (the masking `get_deploy_impact`'s verdict applies, so the soak and the tool agree):
`http.error_ratio`'s `delta` (and `after − baseline`) over `max_error_rate_delta`, or
`http.latency`'s `delta_pct` (and its percent over `baseline`) over `max_p95_delta_pct` with
`delta` at least `deploy_ops.impact.latency_floor_ms`. A breach sets `checks.early_stop` (when
nothing set it first) and the release goes `awaiting_verdict` like any failed check; the
release engineer decides, `auto_rollback` unchanged. The thresholds are frozen with the rest of
the profile when a release opens. With either set, `verify_until` is at least 10 minutes after
the deploy. When the window ends with a threshold's signal still `settling`, the soak keeps
verifying (impact read at most every 2 minutes) until it settles or 15 more minutes pass; the
read that ends the soak skips deploy impact's one-minute cache (`deployimpact.Service.ForReleaseFresh`),
and a threshold whose signal is still settling then is added to `unchecked`. The sweeper's
in-process pacing is keyed by `release.Service.SetScope` as well, so a sweep of one tenant never
forgets another's.

Errors: `ErrReleaseNotFound` → 404; `ErrReleaseWrongStatus`, `ErrReleaseNoDeploy`,
`ErrDeliveryUnconfirmed`, `ErrReleaseEmpty`, `ErrReleaseTagExists`,
`ErrDeployDependencyPending`, `ErrBeforeDeployPending` → 409; `ErrInvalidDelivery`,
`ErrInvalidVersion`, a confirm mismatch → 400; an unknown repository → 404.

**Removed.** The old "release train" — `GET/POST /v1/repositories/{id}/deploy-packages`,
`PATCH/DELETE .../deploy-packages/{pkgId}`, `PUT .../deploy-packages/{pkgId}/tasks`,
`POST .../deploy-packages/{pkgId}/release` — is gone; batch releases (above) replace
shipping several tasks together. `trigger_release`/`TriggerRelease` (the old per-task
release dispatch) is gone too — a release now opens automatically at merge time (or joins
a draft), never through a separate trigger call.

## Cloud accounts, environments & runtime (migration 155, Phase 2)

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
  Every environment row carries the derived `per_branch: boolean` — true for a
  `preview` environment on Vercel, which is one deployment per branch/PR rather than one
  address. A non-production environment bound to a Vercel project never defaults its `url`
  to the project's (production) URL, by bind, candidate pick or scan; the health sweep
  never probes a `per_branch` row's URL — its `health.status` is the newest preview
  build's (`ready`→`healthy`, building→`deploying`, error→`failed`, none→`unknown`).
- `GET /v1/environments/{envId}/overview` — `{environment, detail?, deployments[],
  errors[], unavailable?, errors_supported, preview_access?}`; never errors for "not
  connected" or a provider auth refusal, both go through `unavailable` instead.
  `errors_supported` is false when the environment has no error surface (a `per_branch` row — a
  Vercel preview; other Vercel rows group their error-level logs) — `errors` is then `[]` and not a verdict.
  `preview_access` (`per_branch` rows only) is `{protected, mode:
  "none"|"vercel_authentication"|"password"|"vercel_authentication_and_password",
  bypass_configured}` — whether a "Protection Bypass for Automation" exists, never the
  secret. For a `per_branch` row `detail` describes the newest preview build, not the
  project's production.
- `GET /v1/environments/{envId}/logs?since=&until=&min_severity=&q=&limit=&cursor=` —
  `since`/`until` RFC3339, `min_severity` ∈ `debug|info|warning|error|critical`, defaults
  last hour / 200 entries.
  `GET …/errors?since=` (default 24h) — `{errors: RuntimeErrorGroup[]}`, provider-native
  grouping where there is one (GCP Error Reporting), a message-fingerprint grouping over
  error-level logs otherwise.
  A `per_branch` row's logs are the newest READY preview deployment's (an empty page when
  there is none), and its `errors` are always `[]`.
  `GET …/deployments?limit=` — `{deployments: CloudDeployment[]}`, only the environment's
  own target: production → production builds, preview → preview builds, staging/development
  → everything but production. `CloudDeployment` has `pr_number?` (Vercel
  `meta.githubPrId`) and `branch_url?` (the stable `-git-` alias, when known).
  `POST …/errors/task` body one `RuntimeErrorGroup` (as returned) — opens a bug task with
  the error, a sample and recent log lines; component set from the environment.
- `POST /v1/components/{id}/errors/task` body one Errors inbox group read through a binding
  (`RuntimeErrorGroup` plus `data_source_id`, and `environment`, `production` when absent) —
  opens the same bug task (`cloud.Service.CreateBoundFixTask`) in the component's repository,
  naming the data source (`Data source: <name> (<kind>)`) instead of a provider resource, with
  the group's `external_url` as its link when that is an http(s) page; no log lines. 201 the
  task; 400 for a malformed `data_source_id` or an unknown `environment`; 404 for an unknown
  component or data source.
- `GET /v1/repositories/{id}/tasks/{taskId}/previews` — `{previews: TaskPreview[]}`
  (`domain.TaskPreview`), one per active component whose confirmed `preview` environment
  is `per_branch`; `[]` when there is none. `TaskPreview` = `{component_id,
  component_name, environment_id, provider, status:
  "building"|"ready"|"error"|"canceled"|"none", url, branch_url, pr_number, commit_sha,
  created_at, ready_at?, inspect_url, protected, bypass_configured}`. The deployment is the
  newest non-production build of the task's branch (the PR's head branch when GitHub is
  connected, else the task branch), preferring the one built from the PR's head commit;
  `status: "none"` (other fields empty) when Vercel has not built the branch yet. Cached
  30s per environment. 404 unknown task.
- `RepositoryModel.environments` (`GET /v1/repositories/{id}/model`) and
  `RepositorySummary.environments` (`GET /v1/projects/overview`, from stored health only)
  carry the same rows for the UI; agents read the equivalent through `get_environment` /
  `query_runtime_logs` / `list_runtime_errors` / `list_deployments`
  (`.ai/tool-reference.md`).

## Operations: Errors inbox and Logs explorer

`internal/application/opsquery` fans one query out over every component environment of one
deploy environment kind that is `confirmed` and bound (account + resource), leaving out those of a
dismissed component, through the same `cloud.Service.Errors`/`Logs` the per-environment routes
above use (and their 30 s cache). At most 4 environments are read at once and each is cut off after
20 s; one that fails is listed in `unavailable` and never fails the answer. Mounted with the cloud
routes. JSON shapes are `domain.OpsErrors` / `OpsLogs` (`internal/domain/opsquery.go`); errors are
the flat `{"error"}` shape. Common parameters: `env` ∈ `production|staging|preview|development`
(absent → `production`, anything else → 400); `component_id`, repeated or comma-separated, limits
the read to those components (an unknown one matches nothing, a malformed one is 400).

- `GET /v1/operations/errors?env=&since=&component_id=…` → `{groups: [{component_id,
  repository_id, environment_id, provider, fingerprint, message, count, first_seen, last_seen,
  sample?, source?, new, external_url?}], unavailable, truncated}`. `since` RFC3339, default 24 h
  ago, clamped to 30 days back. An environment with no error surface (`errors_supported: false` —
  a `per_branch` row, i.e. a Vercel preview) is skipped silently, not reported; every other bound
  one is read (GCP Error Reporting, else its error-level logs grouped by message — Vercel production
  and AWS included), and an unbound one (a custom URL, no account) is never selected. At most 50 environments are
  read; `truncated: true` says more matched. Sorted `new` first, then `last_seen` newest first, then
  `count`.
  **Errors bindings join.** Every component's confirmed `errors.issues` binding in `env` on a
  source whose kind lists error groups (`port.ErrorsQuerier`; today `sentry`) is read as well,
  beside its cloud environments — not instead of them, unlike logs: a Sentry issue and a cloud
  error group are different records, and a person bound the project to add it
  (`signals.Service.ErrorBindings` / `ReadErrors`, `port.BoundErrorReader`). At most 50 bindings
  (more → `truncated`), read 4 at a time beside the cloud fan-out, each cut off after 20 s, at
  most 100 groups each (a binding answering all 100 sets `truncated`: it may hold more), over
  the same `since`…now; a component with such a binding and no connected environment is read all
  the same. Their groups carry `provider` = the source's
  kind (`sentry`), `data_source_id`, and the zero `environment_id` (`00000000-…`); for Sentry
  `fingerprint` is the issue id, `message` its title, `source` its culprit, `sample` its
  metadata value, `count` its events in the window, `external_url` its permalink, and `new` is
  set when its first event falls in the window. A binding that fails is an `unavailable` row with
  `data_source_id` (codes as for logs bindings). Bindings of dismissed components are left out;
  failing to list the bindings at all leaves them out of the answer, never failing it. Such a
  group becomes a task through `POST /v1/components/{id}/errors/task` (see "Components and
  where they run"), there being no environment to post it to.
  **Error definitions (migration 191).** Every group also carries `definition` — `{id, name,
  category, severity, action}` of the active error definition it matches, or `null` (see "Error
  definitions and AI suggestions"). Groups whose definition's action is `ignore` are left out
  unless `include_ignored=1` (or `true`); `ignored_count` (present only when non-zero) says how
  many were. A failure reading the definitions leaves every group unannotated, never failing the
  answer.
- `GET /v1/operations/logs?env=&since=&until=&component_id=…&min_severity=&text=&limit=` →
  `{entries: [{timestamp, severity, message, source?, method?, path?, status_code?, trace_id?,
  fields?, component_id, environment_id}], unavailable, truncated}`. `until` defaults to now,
  `since` to an hour before `until`; `since` not before `until` → 400. `min_severity` as for one
  environment; `text` is passed to each provider's search. `limit` default 200, max 500, bounds
  every environment's page and the merge. At most 10 environments: more with no `component_id` →
  400 `{error: "too_many_environments", code: "too_many_environments", matched,
  max_environments}` (the UI asks the user to pick components); more with one → the first 10 and
  `truncated: true`. Entries merge newest first; `truncated` is also set when an environment had
  more (a cursor, a provider cap, or a full page) or the merge was cut. There is no cursor across
  providers: the next older page is the same query with `until` = the oldest `timestamp`
  returned. `until` is inclusive and kept at full precision, so the entries at exactly that
  instant come back again — de-duplicate them on the client.
  **Logs bindings win.** A component with a confirmed `logs.lines` binding in `env` (see
  "Logs and traces data sources") is read through that binding — its data source and rendered
  query, the same `since`/`until`/`min_severity`/`text`/`limit` — **instead of** its cloud
  environments, not merged with them: the binding is a person's choice of where that component's
  lines are read, and the same lines often reach both stores (a shipper forwarding the
  platform's logs to Loki), with no reliable key to drop the copies (each store stamps and
  formats a line its own way). A binding read counts toward the 10 like an environment, and
  bindings come first, so cutting at 10 (with `component_id`) drops environments before any
  binding; a component with a binding and no connected environment is read all the same. Its
  entries carry `data_source_id` and the zero `environment_id` (`00000000-…`), and a failed one
  is an `unavailable` row with `data_source_id` (`not_connected` for a deleted source, `timeout`,
  else `provider_error` with the source's redacted message). Bindings of dismissed components are
  left out (`signals.Service.LogBindings`).
- `unavailable[]` = `{component_id, environment_id, data_source_id?, code, message}`; `code` ∈ `not_connected` (no
  provider adapter or account for it any more), `cloud_auth` (reconnect the account, which is
  marked `error`), `provider_error`, `timeout` (no answer within 20 s). A timeout or cancellation
  is never cached, so the next read tries the environment again.

## Observability data sources, signals and recipes (migration 186)

`internal/application/datasource`. Three concepts: a **data source kind** is a connection type
in a registry (`port.DataSourceKindAdapter`: a config schema and capabilities `metrics`, `logs`,
`traces`, `errors`, `crashes`, `rum`, `queues`; what it can do beyond `Verify` is the optional
`port.MetricsQuerier` / `port.Discoverer` / `port.TableQuerier` (an instant per-entity table, see "Recipe tables") / `port.QueueDiscoverer` (a broker's consumers) / `port.LogsQuerier`, `port.TracesQuerier`, `port.LabelValuesLister` (see "Logs and traces data sources"), detected by type assertion). A **signal** is a fixed,
vendor-neutral measure (`catalog/observability/signals.yaml`). A **recipe** is data mapping
signals to one kind's native queries (`catalog/observability/recipes/<id>.yaml`): supporting a new
exporter is one YAML file, no Go. Routes are mounted only when Postgres is connected. JSON shapes
are the Go tags in `internal/domain/datasource.go` and `observability.go`. Errors are flat
`{"error"}`, plus `fields` (400, the request does not fit the kind's schema) or `code`.

Kinds: `prometheus` (`internal/adapter/datasource/prometheus`) — Prometheus, Mimir, Cortex,
Thanos, VictoriaMetrics, Grafana Cloud, AMP behind a SigV4 proxy. Config `url` (the API base,
without `/api/v1`), `auth` `none|bearer|basic`; secrets `token` (bearer), `password` (basic, with
config `username`), `headers` (one `Name: value` per line, e.g. `X-Scope-OrgID`; `Host`,
`Content-Length` and other framing headers, and the metadata-service ones `Metadata-Flavor` and
`X-aws-ec2-metadata-token(-ttl-seconds)`, refused in any case). Verify reads `/api/v1/status/buildinfo`,
falling back to `/api/v1/query?query=1` on anything but success or 401/403. Query is
`POST /api/v1/query_range` (form); discover is `/api/v1/label/__name__/values` over the last hour,
at most 20 000 names. Its `prometheus.API` (an `Endpoint`: base URL, headers, masks) is what the
other Prometheus-family kind shares.

`gcp_monitoring` (`internal/adapter/datasource/gcpmonitoring`) — Google Cloud Monitoring through
its Prometheus-compatible API, `https://monitoring.googleapis.com/v1/projects/<project>/location/global/prometheus`
plus the same `/api/v1/...` paths (verify is `/api/v1/query?query=1` alone). Config
`cloud_account_id` (a connected `gcp` account) and `project_id` (empty → the account's
`meta.project_id`, then the key file's `project_id`). No secrets of its own: an OAuth2 token
for scope `monitoring.read` is minted from the account's service account key
(`gcloud.MintAccessToken`, the key's `token_uri` dialled through the URL policy) and cached per
key until a minute before it expires. The fixed API address still goes through the URL policy.
PromQL names: `run.googleapis.com/request_count` → `run_googleapis_com:request_count`; metric
and resource labels are labels (`service_name`, `location`, `response_code_class`; a metric
label colliding with a resource label is `metric_<name>`); `monitored_resource` is the resource
type; a distribution is a histogram (`_bucket`, `_count`, `_sum`).

`aws_cloudwatch` (`internal/adapter/datasource/cloudwatch`) — CloudWatch `GetMetricData`
(series) and `ListMetrics` (discover, metrics active in the last 3 h, listed as
`<namespace>/<metric>`, at most 5 000), over the query protocol at
`https://monitoring.<region>.amazonaws.com`, SigV4-signed with the account's keys
(`aws.SigningCredentials`, the SDK's v4 signer). Config `cloud_account_id` (a connected `aws`
account) and `region` (empty → the account's). Verify is one `ListMetrics` page. **Query
format** — JSON, `domain.CloudWatchQuery`: one metric `{"namespace", "metric", "stat",
"dimensions"?, "period"?}`, or `{"expression", "metrics"?: {"<id>": <one metric>},
"period"?}` where the expression is metric math over the ids (`100 * FILL(e, 0) / i`,
`r / PERIOD(r)`), a `SEARCH(...)` or a Metrics Insights `SELECT` (no metrics needed). `stat` is
any statistic (`Sum`, `Average`, `Maximum`, `p95`, `tm99`, …); ids start lowercase. `period`
(1, 5, 10, 30 or a multiple of 60) defaults to the step rounded up to a minute, raised to a
multiple of 300 s for a range starting over 15 days ago and of 3600 s over 63 days (what
CloudWatch still keeps). Pages follow `NextToken` (at most 20), a series stops at 11 000 points
(a warning says so). A query that does not parse is 400 `bad_query`, as are CloudWatch's
`InvalidParameterValue`/`ValidationError`; a rejected key reads "authentication failed".

**Direct broker kinds** — `kafka`, `rabbitmq`, `nats` read a broker's own state, no exporter, and
have capabilities `metrics` and `queues`: each implements `port.MetricsQuerier`,
`port.TableQuerier`, `port.Discoverer` and `port.QueueDiscoverer`. They read **live state only**:
`Series` answers one point per series, at the query's `to` (now when `to` is later), and a window
that ended more than 5 minutes ago (the previous window of a comparison) answers no series and a
warning rather than the live value (`adapter/datasource/live.Instant`). So alerts (which judge the
latest value), golden values and the queues table work directly; charts show one point, and chart
history needs an exporter-backed source (`kafka-exporter`, `kminion`, `rabbitmq-prometheus`,
`nats-exporter`). `Table` takes the same query JSON and ignores its `metric`; a past `At` answers
no rows. Each query is one JSON object, unknown keys refused (`domain.Parse{Kafka,RabbitMQ,NATS}Query`;
a query that does not parse is 400 `bad_query`); an absent `metric` is the backlog measure.

- `kafka` (`internal/adapter/datasource/kafka`, franz-go `kgo` + `kadm`). Config
  `bootstrap_servers` (comma-separated `host[:port]`, default port 9092; a **destination** field:
  each host is checked against the URL policy's literal rules at save — `invalid` when malformed,
  `not_permitted` when refused — and moving it to another host set needs the stored secrets
  again, like a `url` changing origin), `sasl_mechanism` `none|plain|scram-sha-256|scram-sha-512`
  (with `username`; refused before connecting when either credential is missing), `tls`
  `off|on`; secrets `password`, `ca_cert` (PEM of a private CA, shown with `tls: on`; a PEM pasted
  on one line is accepted). No insecure-skip-verify. Every broker connection — the bootstrap
  servers and every address the cluster's metadata advertises — goes through
  `urlguard.Policy.DialContext` (resolve once, refuse the whole host if any answer is blocked,
  dial the vetted IP), then TLS with the broker's host as server name. Verify reads the metadata:
  `info.detail` `Kafka · <n> broker(s) · cluster <id>`, `info.version` the ApiVersions guess;
  with `sasl_mechanism: plain` and `tls: off` and any bootstrap or advertised broker off this
  computer (not `localhost` or a loopback address), `info.detail` ends `· warning: SASL PLAIN
  without TLS sends the password unencrypted; turn TLS on or use SCRAM` (the source still saves).
  Discover lists `group/<name>` and `topic/<name>` (not internal topics), at most 2 000. Query
  `{"group", "topic"?, "metric"?: lag|lag_by_partition|consumers|members}`: `lag` the group's
  total (committed offset against end offset per partition, kadm's `Lag`; a partition never
  committed to counts from its start offset), `lag_by_partition` one series per partition (labels
  `group`, `topic`, `partition`), `consumers` members assigned a partition (of `topic` when given),
  `members` every member. A KIP-848 group (Kafka 4's consumer protocol), which the classic describe
  does not show, is read through `ConsumerGroupDescribe`. Table: one row per partition —
  `topic`, `partition`, `committed_offset`, `end_offset`, `lag`, `member_id`, `client_id` (an
  unknown value is left out), at most 5 000; key columns `topic`, `partition`. A missing group
  answers no series and a warning.
  Broker errors name the broker as given, never a resolved address; a broker closing the
  connection reads "check the SASL mechanism and credentials, and whether the listener expects
  TLS".
- `rabbitmq` (`internal/adapter/datasource/rabbitmq`, management HTTP API). Config `url` (the
  management base, e.g. `http://localhost:15672`, with any path prefix), `username`, `vhost`
  (empty → `/`); secret `password` (basic auth; the encoded value is masked too). Verify reads
  `/api/overview` (`info.version` the RabbitMQ version, `info.detail` `RabbitMQ · <cluster> ·
  vhost <v>`) and checks `/api/vhosts/<vhost>`. Discover lists `queue/<name>` (paged, 500 a page,
  at most 2 000). Query `{"queue", "metric"?: messages|ready|unacked|consumers|publish_rate|deliver_rate|ack_rate}`:
  a plain name reads `/api/queues/<vhost>/<name>`; a `*` matches any run of characters and pages
  through `/api/queues/<vhost>?name=<anchored regex>&use_regex=true`, re-checking each answer, one
  series (labels `queue`, `vhost`) or row per queue. Rates are `message_stats.<x>_details.rate`
  (`deliver_rate` is `deliver_get`), 0 while the queue has not seen that event. Table columns
  `queue`, `state`, `ready`, `unacked`, `consumers`, `publish_rate`, `deliver_rate`, `ack_rate`
  (key column `queue`). A listing reads at most 40 pages (more is `truncated`), and a vhost or
  queue named `.` or `..` is refused rather than sent as a path segment.
- `nats` (`internal/adapter/datasource/nats`, the server's HTTP monitoring endpoint). Config
  `url` (e.g. `http://localhost:8222`); optional secret `token` (sent as `Authorization: Bearer`,
  for an endpoint behind a proxy). Verify reads `/varz` (`info.version`, `info.detail` `NATS ·
  <server>`, `· JetStream off` without JetStream). Reads `/jsz?accounts=true&streams=true&consumers=true`;
  in a cluster, point it at a server holding the streams. Discover lists `stream/<name>` and
  `consumer/<stream>/<name>`, at most 2 000. Query `{"stream", "consumer"?, "metric"?:
  pending|ack_pending|redelivered|waiting}`: one series per consumer of the stream (labels
  `account`, `stream`, `consumer`), or only the named one. Table columns `consumer`, `pending`,
  `ack_pending`, `redelivered`, `waiting` (key column `consumer`).

`QueueDiscoverer` offers each consumer by name with the params of its kind's direct recipe
(`domain.QueueTarget.Params`): Kafka every consumer group (`{group}`, `kafka-direct`), RabbitMQ
every queue of the vhost (`{queue}`, `rabbitmq-direct`; a queue whose name holds `*` is left
out, since a query reads `*` as a pattern), NATS every consumer (`{stream, consumer}`,
`nats-direct`). The detector suggests that recipe with those params, so a discovered name only
ever reaches a query through the recipe's rendering, which escapes it for a JSON string.

The log and trace kinds `loki`, `elasticsearch` and `tempo` are described in "Logs and traces
data sources" below; the store and crash reporting kinds `google_play_vitals`,
`app_store_metrics` and `sentry` in "Mobile store and Sentry data sources"; the web analytics
and APM kinds `posthog`, `ga4`, `plausible`, `datadog` and `newrelic` in "Web analytics and APM
data sources".

**Cloud account fields.** A field of type `cloud_account` (with `provider`) holds a cloud account
id, never a credential: save checks the account exists (`not_found`) and is that provider's
(`wrong_provider`); without `datasource.Deps.CloudAccounts` such a field takes no value
(`not_permitted`). The kind reads the credential through `port.CloudAccountReader` (handed over
by `CloudAccountsSetter`) on every call, and errors are masked with that credential's values
too (not the ones the account reports in `meta`, like an AWS region).

- `GET /v1/data-source-kinds` → `{kinds: [{kind, label, description, capabilities, fields}]}`.
  A field is `{key, label, help, type: text|url|secret|select|headers|cloud_account|store_credential,
  required, options, placeholder, show_when, secret, provider?, default?, destination?,
  multiline?}` (`destination: true` on a text field holding the `host[:port]` list a kind connects
  to, Kafka's `bootstrap_servers`; `multiline: true` on a field whose value spans lines, Kafka's
  `ca_cert` and GA4's `service_account_json`, for a text area; both absent when false; `provider` `gcp|aws`
  on a `cloud_account` field: pick from that provider's connected accounts and send the account
  id; `google_play|asc` on a `store_credential` field: the store credential it reads, see "Store
  credential fields"; `default` is the value a shown field takes when none is sent — a
  `store_credential` field's provider, Sentry's `https://sentry.io` — so the UI may prefill it);
  `show_when` `{auth: "bearer"}` shows the field only while
  `auth` is `bearer`, and `required` applies only while it is shown. `secret: true` fields go in
  `secrets`.
- `GET /v1/data-sources` → `{data_sources: [DataSource]}`; `GET /v1/data-sources/{id}` → one.
  `DataSource` = `{id, kind, name, config, secrets_set, status: ok|error|unverified,
  status_detail, info: {version, detail}, verified_at (last success, null before), created_at,
  updated_at, capabilities}`. A secret value is never returned — only its key in `secrets_set`.
- `POST /v1/data-sources {kind, name?, config, secrets}` → 201 `DataSource`. Saved, THEN
  verified: a source that cannot be reached is kept with `status: "error"` and `status_detail`.
  Keys are routed by the schema, so a secret field sent in `config` is still stored encrypted.
  Empty `name` → the URL's host (a cloud kind: `<kind label> · <account label>`; a store kind its
  label). 400 `{error,
  fields: {key: code}}` with code `required`, `invalid`, `not_permitted`, `unknown_field`,
  `invalid_option`, `too_long`, `secrets_required`, `not_found` or `wrong_provider` (`kind` →
  `unknown_field` for an unregistered kind).
- `PATCH /v1/data-sources/{id} {name?, config?, secrets?}` → `DataSource`. A key given replaces
  its value, `""` removes it, keys left out are kept; a field hidden by its `show_when` loses its
  value (switching `auth` to `none` drops the token). Saved as `unverified`, then verified again.
  **Moving a `url` to another origin** (scheme, host or port; the path may change freely) must
  not carry stored secrets there: every key of `secrets_set` the result would keep has to come in
  `secrets`, with a new value or `""` to remove it — otherwise 400 `{error, fields: {secrets:
  "secrets_required"}}` and nothing is saved. A secret the same PATCH hides (via `show_when`) is
  dropped, so it needs no value. `DELETE /v1/data-sources/{id}` → 204.
  `POST …/{id}/verify` → `DataSource`, 200 either way. A verify writes only the outcome, and only
  if the source was not saved again meanwhile; one that lost that race answers the stored source
  unchanged.
- `GET /v1/data-sources/{id}/discover?match=&limit=` → `{metrics, truncated, recipes:
  [{recipe_id, matched_metric}]}`. `match` is a case-insensitive substring filter, `limit`
  defaults to 1000 (max 5000); `recipes` are the kind's recipes whose `detect.metric` the source
  holds, judged over the whole inventory, not the filtered page. Where the source's own listing
  stopped short (`truncated`; CloudWatch stops after 20 ListMetrics pages) and its kind can look one
  metric up (`port.MetricDiscoverer`; CloudWatch: ListMetrics by `Namespace` + `MetricName`), each
  detect metric the listing lacks is looked up, at most 32; a lookup that fails only leaves that
  recipe out.
- `POST /v1/data-sources/{id}/query {query, from, to, step_seconds}` (RFC3339; `step_seconds`
  0 → about 600 points, at least 15 s) → `{series: [{name, labels, points: [[unix_ms,
  value|null]]}], warnings}`. `NaN`/`±Inf` are `null`. Capped at 90 days, 11 000 points per
  series and 500 series (a warning says so). 400 `fields` for a bad range/step, 400
  `code: "bad_query"` for a query the source refused, 422 `code: "unsupported"` for a kind
  without metrics, 502 `code: "upstream"` when the source fails.
- `GET /v1/signals` → `{signals: [{id, kind: metric|log|trace|error, unit, roles,
  aggregation: rate|ratio|quantile|gauge, better?: higher|lower}]}`. `better` is which way the
  signal moves when things improve; a signal without it is not judged across a deploy, and
  every golden signal has one (the signals service refuses a golden list that names one without).
- `GET /v1/recipes` → `{recipes: [{id, source_kind, applies_to: {technology,
  messaging_system, resource_kind, resource_params, store_platform, store_params}, detect: {metric}, params, defaults?: {param: value}, signals:
  {signal_id: query}, tables?: {<queues|crashes|signatures|pages|referrers|countries|devices|endpoints>:
  {columns: [{key, label, unit}], key_labels, queries: {column: query}}}, origin: catalog|user}]}`. Rows of `observability_recipes` (no write API yet) join the catalog; one with
  a catalog recipe's id replaces it, one that no longer validates is left out.

**Recipe file.** `id` (must equal the file name), `source_kind` (a registered kind, or one this
build ships: `domain.BuiltinDataSourceKinds`), `applies_to`, `detect.metric`, `params`,
`defaults` (optional: the value a declared param takes where a binding leaves it empty —
`domain.Recipe.WithDefaults`, applied when a binding is rendered and when a table's params are
gathered, so `needs_params` never names it; each default must be a declared param and every query
must render with it), `signals` (every key a catalog signal). A query references `$param` for a declared param and the built-ins
`$__interval` (the step), `$__rate` (`max(2 × step, 2m)`: two samples at a 60 s scrape) and
`$__quantile` (the quantile a quantile signal is read at, `0 < q < 1`, default 0.95;
`RenderOptions.Quantile`). Where each may sit is the kind's language's rule
(`datasource.RenderKindQuery`; `RenderQuery` is the PromQL one):

- **PromQL** (`prometheus`, `gcp_monitoring`): values are escaped for a double-quoted string
  literal, so a param may sit only inside a double-quoted label value matched with `=` or `!=`
  (not `=~`/`!~`, not bare in `by (...)` or a range); `$__rate`/`$__interval` only inside
  `[...]`; `$__quantile` only where a number goes, outside strings, `{...}` and `[...]`
  (`histogram_quantile($__quantile, …)`), rendered as `0.95`.
- **CloudWatch JSON** (`aws_cloudwatch`): values are escaped for a JSON string, so a param may
  sit only inside a dimension value (`"dimensions": {"QueueName": "$queue"}`, also under
  `metrics.<id>`), never in a key, a namespace, a stat or the expression; `$__quantile` only as
  a whole `stat` value, rendered as the percentile statistic (`p95`, `p99.9`); `$__rate` and
  `$__interval` are refused (the period follows the step). The rendered query must parse as a
  `domain.CloudWatchQuery`.
- **Broker JSON** (`kafka`, `rabbitmq`, `nats`): values are escaped for a JSON string, so a param
  may sit only inside the value naming the entity read — kafka `group`/`topic`, rabbitmq `queue`,
  nats `stream`/`consumer` — never in `metric` or a key; every built-in is refused (a live read has
  no step and no quantile). The rendered query must parse as the kind's `domain.*Query`. A detect
  metric, where a recipe has one, is an inventory entry (`group/<name>`, `queue/<name>`,
  `stream/<name>`, `consumer/<stream>/<name>`); the shipped direct recipes have none, since these
  kinds are offered through `port.QueueDiscoverer`.
- **LogQL** (`loki`): lexically PromQL's rules — a param only inside a double-quoted stream
  selector value matched with `=`/`!=` (never in a line filter `|= "…"`, a label filter stage or
  a regex matcher), `$__rate`/`$__interval` inside `[...]`, `$__quantile` where a number goes. A
  detect metric is a label name (`service_name`).
- **TraceQL** (`tempo`): a param only as the double-quoted value an attribute is compared to
  with `=`/`!=` inside `{...}`; every built-in is refused. A detect metric is a scoped tag
  (`resource.service.name`; `.name` unscoped).
- **Elasticsearch JSON** (`elasticsearch`): `domain.ElasticsearchQuery`, `{"filters": {"<field>":
  "<value>", …}}` (at most 32, unknown keys refused), a param only inside a filter's value,
  escaped for a JSON string, never a key; every built-in is refused. A detect metric is a field
  name (`service.name`).
- **Web JSON** (`posthog`, `ga4`, `plausible`): `domain.WebQuery`, held to the broker rules — a
  param only inside the value naming what is read (`host`, `event`; GA4's `property`, Plausible's
  `site`), escaped for a JSON string, never in `metric`, `table` or a key; every built-in refused.
  A detect metric is an event name as Discover lists it (`$pageview`, `page_view`, `pageview`).
- **Datadog** (`datadog`): a Datadog metric query. Datadog has no quoting to escape for, so a
  param's value is checked, not escaped: as a whole tag value inside `{...}` (`service:$service`)
  it must be `[A-Za-z0-9_.:/-]+`; outside `{...}` only as part of a metric name
  (`$metric_prefix.hits`, `trace.$span.errors`), where it must be dotted `[A-Za-z0-9_]` segments —
  anything else is `ErrInvalidInput` at render. `$__quantile` only as the aggregator before a `:`
  (rendered `p95`); `$__interval` the step in whole seconds where a number goes
  (`.rollup(sum, $__interval)`); `$__rate` refused (use `.as_rate()`). A detect metric is a metric
  name.
- **NRQL** (`newrelic`): a param only inside a single-quoted string compared with `=` or `!=`
  (`appName = '$app'`); NRQL's in-string escaping is undocumented, so a value with `'`, `\` or a
  control character is refused at render rather than escaped. `$__quantile` is a percent where a
  number goes, outside strings (`percentile(duration, $__quantile)` → `95`); `$__rate` and
  `$__interval` are refused (the kind appends SINCE, UNTIL and TIMESERIES). A detect metric is an
  event type (`Transaction`).

`applies_to.resource_kind` (a `domain.CloudResourceKind`) says which bound cloud resource the
recipe fits, and `applies_to.resource_params` which field of that resource's ref fills which
param (`id`, `name`, `region` or `extra.<key>`; `Recipe.ParamsFromResource(ref)`); a param it
leaves out is set by hand. A recipe that breaks any of this does not validate. A test loads and
renders every embedded recipe; the server refuses to mount the routes if the catalog does not
validate.

| recipe | kind | resource_kind → params from the ref | set by hand |
|---|---|---|---|
| `gcp-cloud-run` | gcp_monitoring | `cloud_run_service`: `service` ← name, `location` ← region | — |
| `gcp-gke-workload` | gcp_monitoring | `gke_workload` (a cluster ref): `cluster` ← name, `location` ← region | `namespace`, `workload` (top-level controller); infra only |
| `gcp-https-loadbalancer` | gcp_monitoring | — | `backend` (`backend_target_name`) |
| `gcp-pubsub` | gcp_monitoring | — | `subscription` (id) |
| `aws-alb` | aws_cloudwatch | — | `load_balancer` (`app/<name>/<id>`) |
| `aws-ecs` | aws_cloudwatch | `ecs_service`: `cluster` ← extra.cluster_name, `service` ← name | — (needs Container Insights) |
| `aws-lambda` | aws_cloudwatch | `lambda_function`: `function` ← name | — |
| `aws-sqs` | aws_cloudwatch | — | `queue` (name) |
| `aws-msk` | aws_cloudwatch | — | `cluster`, `group`, `topic` |
| `kafka-direct` | kafka | — | `group` (backlog = lag, consumers) |
| `rabbitmq-direct` | rabbitmq | — | `queue` (`*` allowed; backlog, consumers, consume/publish rate) |
| `nats-direct` | nats | — | `stream`, `consumer` (backlog = pending) |
| `loki-service` | loki | — (detect `service_name`) | `service` (`logs.lines`; `process.errors` from Loki 3's `detected_level`) — the primary Loki recipe |
| `loki-app` | loki | — (detect `app`) | `app` (`logs.lines`; Promtail's Kubernetes label, Loki 2) |
| `elasticsearch-service` | elasticsearch | — (detect `service.name`) | `service` (`logs.lines`, ECS) |
| `tempo-service` | tempo | — (detect `resource.service.name`) | `service` (`traces.spans`) |
| `play-vitals` | google_play_vitals | store app `android`: `package` ← identifier | — (crash_free_users, crash_rate, anr_rate; `crashes` table) |
| `app-store-metrics` | app_store_metrics | store app `ios`: `app` ← store_app_id | — (cold_start; `signatures` table) |
| `sentry-mobile` | sentry | — | `project` (crash_free_users, crash_rate, errors.issues; `crashes` table) |
| `sentry-web` | sentry | — | `project` (web.js_errors, errors.issues) |
| `sentry-backend` | sentry | — | `project` (process.errors, errors.issues) |
| `posthog-web` | posthog | — (detect `$pageview`) | — (page_views, sessions, visitors, js_errors; pages, referrers, countries, devices tables) |
| `posthog-web-vitals` | posthog | — (detect `$web_vitals`) | — (lcp, inp, cls, fcp at p75) |
| `ga4-web` | ga4 | — (detect `page_view`) | — (page_views, sessions, visitors; the four web tables) |
| `plausible-web` | plausible | — (detect `pageview`) | — (page_views, sessions, visitors; the four web tables) |
| `vercel-web` | vercel | — (detect `pageview`; resource `vercel_project`, `project`=id) | `project` (page_views, visitors; the four web tables) |
| `vercel-web-vitals` | vercel | — (detect `speed_insights`; resource `vercel_project`, `project`=id) | `project` (lcp, inp, cls, fcp at p75) |
| `datadog-apm` | datadog | — (detect `trace.http.request.hits`) | `service`, `env`; `metric_prefix` defaults to `trace.http.request` (http.requests, error_ratio, latency; `endpoints` table) |
| `datadog-system` | datadog | — (detect `system.cpu.user`) | `host` (infra.cpu, infra.memory) |
| `datadog-container` | datadog | — (detect `container.cpu.usage`) | `container` (infra.cpu, infra.memory) |
| `newrelic-apm` | newrelic | — (detect `Transaction`) | `app` (http.requests, error_ratio, latency; `endpoints` table) |
| `newrelic-browser` | newrelic | — (detect `PageView`) | `app` (page_views, sessions, js_errors, lcp, inp, cls; `pages` table) |
| `newrelic-mobile` | newrelic | — (detect `MobileSession`) | `app` (crash_rate, crash_free_users) |

**Recipe tables.** A recipe may also declare `tables`, by name: `queues` (the queues view reads
it), `crashes` (one row per crash or error issue — columns `issue`, `type`, `cause`, `location`,
`users`, `reports`, `last_version`, `last_seen`, `link`; the mobile Crashes view's) and
`signatures` (one row per diagnostic signature — `issue`, `type`, `cause`, `weight`, `build`),
`pages`, `referrers`, `countries`, `devices` (the web view's: one row per path, source, country or
device/browser/OS, with `views`, `visitors`, …) and `endpoints` (the backend endpoints view's: one
row per route or transaction — its key column, then `rpm`, `error_pct`, `p95_ms`). A
table is `columns: [{key, label, unit?}]` (shown in order),
`key_labels` (at least one, each a column key: the label whose value fills that column and
identifies the row) and one query per other column: `queries: {<column>: <query>}`, or `query:`
when exactly one column is not a key label — or, for a `crashes`, `signatures`, web or `endpoints`
table, `query:` alone for every value column: their kinds answer every column by name from one
read, so a reader runs each distinct rendered query once. The queues view reads `queues`; the
component tables routes ("Component tables" under signal bindings) read the others. Each query is an instant read
(`port.TableQuerier`) and is held to the signal queries' placeholder rules (messages name
`table "queues" column "<key>"`); `$__rate` renders at a 60 s step. The answers are joined on the
key labels' values: a row is one distinct key tuple, a value column a result's
`domain.TableValueKey` (`value`), or its own key where the kind answers named columns; a row one
query lacks has `null` there. Column keys are `[A-Za-z_][A-Za-z0-9_]*`. Instant reads per kind:
`prometheus`/`gcp_monitoring` `POST /api/v1/query` (form `query`, `time`): each vector sample a
row of its labels but `__name__` plus `value` (`null` for NaN/±Inf/a native histogram), a
scalar one row, a matrix or string `bad_query`; `aws_cloudwatch` the query's series over the 20
minutes before the instant at a 60 s period, each series' latest datapoint a row of its labels (a
single metric's dimensions and `stat`) plus `value` — an expression's series carries no
dimensions, so a CloudWatch table reads single metrics keyed by dimension names; the web and APM
kinds as "Web analytics and APM data sources" says. At most 500 rows per table (`truncated`):
where each row carries one `value`, those with the largest values (a sort before the cut, not the
first by label), so a query need not rank itself; a ranked table query may still `topk(500, …)`.

| recipe | queues table key labels → value columns | needs |
|---|---|---|
| `kafka-exporter` | `topic`, `partition` → `lag`, `consume_rate` (the group's, every topic it reads) | — |
| `kminion` | `topic_name`, `partition_id` → `lag` | partition granularity (kminion's default) |
| `rabbitmq-prometheus` | `vhost`, `queue` → `ready`, `unacked`, `consumers` | per-object metrics |
| `gcp-pubsub` | `subscription_id` → `undelivered`, `oldest_age` | — |
| `aws-sqs` | `QueueName` → `visible`, `in_flight`, `oldest_age` | — |
| `aws-msk` | `Topic` → `sum_lag`, `max_lag`, `time_lag` (per topic: a CloudWatch read names one dimension set, so it cannot list partitions) | — |
| `nats-exporter` | `stream_name`, `consumer_name` → `pending`, `ack_pending`, `redelivered`, `waiting` | `prometheus-nats-exporter -jsz=consumers` (or `all`) |
| `redis-exporter` | `stream`, `group` → `lag`, `pending`, `consumers` | `redis_exporter --check-streams`; lag needs Redis 7 |
| `redis-exporter-lists` | `key` → `length` (LLEN of BullMQ's `bull:<q>:wait`, Sidekiq's `queue:<name>`) | `redis_exporter --check-keys` |

`nats-exporter` (params `stream`, `consumer`; the highest report across replicas),
`redis-exporter` (params `stream`, `group`) and `redis-exporter-lists` (param `key`) are the
exporter recipes for NATS JetStream and Redis-backed queues.

**Where a source URL may point.** `datasource.Deps.URLPolicy` is required — no default. Local
mode passes `urlguard.HumanConfigured()` (`platform/runtime/datasources.go`): a person typed the
URL, and a local Prometheus is on this machine or its network, so loopback and private ranges
dial. Never, under any policy: link-local (169.254.0.0/16, fe80::/10), the cloud metadata
addresses 169.254.169.254, 100.100.100.200, `fd00:ec2::254` and `fd20:ce::254` (refused before
`AllowPrivate` is consulted); `HumanConfigured` also refuses the names `metadata.google.internal`,
`metadata.goog` and `metadata` before any lookup. A literal is refused at save (`url: not_permitted`); every request re-checks the address it dials
and pins it (no second lookup), and a redirect may change neither host nor scheme. The service
hands its policy to every kind adapter (`URLPolicySetter`); an adapter has none of its own and
refuses everything until it gets one. A shared host passes `urlguard.PublicOnly()`. Errors in
`status_detail` and 502 bodies name the host as typed, never the address it resolved to, and mask
every secret (the encoded basic-auth value included): the longest value first, so one inside
another (a key's `private_key_id` inside the key file) leaves nothing showing; for a secret that
is a service account key file (a pasted GA4 key, a cloud account's or store credential's), its
`private_key` (as parsed and as JSON writes it), each line of it and its `private_key_id` too;
and where an adapter cut the message short (ending `…`), a tail that begins a secret. A kind that dials TCP rather than a URL
(`kafka`) uses `urlguard.Policy.DialContext` for every connection and
`urlguard.Policy.PrecheckHost` at save; the zero policy dials nothing there either.

**Google Cloud sign-in.** The `gcp_monitoring`, `ga4` and `google_play_vitals` kinds mint their
token with a service account key file (`gcloud.MintAccessToken`). The key's `token_uri` must be
Google's token endpoint `https://oauth2.googleapis.com/token`, or one of the legacy URLs older
keys carry (`https://accounts.google.com/o/oauth2/token`, `https://www.googleapis.com/oauth2/v4/token`),
which are asked at that endpoint; default port, no userinfo, query or fragment; anything else
is refused before any request (`token_uri is not Google's token endpoint`). The token request
follows no redirect (a 3xx is a refusal naming its status), and a minted token is cached at
most an hour whatever `expires_in` says. A
refusal names the HTTP status and, when it is one of RFC 6749's codes, the OAuth error
(`invalid_grant`); the token endpoint's own text is only logged, never put in `status_detail`.
Verify's probe (no build info endpoint there) is `POST …/api/v1/query` with a form body, which
Prometheus also takes.

**Tables.** `data_sources` — `config` JSONB (non-secret fields only), `secret_enc` (the secret
fields as one AES-GCM JSON blob, the `cloud_accounts` cipher; NULL when there are none),
`secrets_set` (their names), `status`, `status_detail`, `info`, `verified_at`.
`observability_recipes` — `id` and `body` (a `domain.Recipe` as JSON).

## Mobile store and Sentry data sources

Three kinds read what the app stores and Sentry already measure, with nothing to deploy. All three
dial through `internal/adapter/datasource/httpapi` (the URL policy — the stores' fixed hosts
included —, pinned addresses, same-host redirects, size caps, masked errors) and answer 400 from
the API as `bad_query`, 429 as an upstream error saying "rate limited (HTTP 429); try again in a
minute", 401/403 as "authentication failed" with what the credential needs. Their queries are
JSON objects held to the broker kinds' placeholder rules (`brokerDialect`): unknown keys refused,
a param only inside the value naming what is read — `package` (Play), `app`, `version`, `build`
(App Store Connect), `project`, `environment`, `release` (Sentry) — never in a metric, a filter or
a key, and every built-in refused (each kind picks the interval its API answers at). The rendered
query must parse as `domain.ParsePlayVitalsQuery` / `ParseAppStoreMetricsQuery` /
`ParseSentryQuery`; a package name or app id is checked where it is read (a query naming none
falls back to the source's config), so a recipe validates with any sample value.

**Store credential fields.** A field of type `store_credential` (with `provider` `google_play` or
`asc`, and that provider as its `default`) names the workspace's store credential — the one per
provider saved under Settings → Integrations (`/v1/store/credentials`), which the release flow
uses —, never a copy of it: its value is the provider (`app_store_connect` is accepted for
`asc`). Save checks the credential is saved (`not_found`) and is the field's provider
(`wrong_provider`; anything else is `invalid`); a field naming no provider takes no value
(`not_permitted`), nor does any without `datasource.Deps.StoreCredentials` (or
`Service.SetStoreCredentials`, how local mode hands over `storeops.Service` once it exists) such a
field takes no value (`not_permitted`). The kind reads the credential through
`port.StoreCredentialReader` (`StoreCredentialsSetter`) on every call, and errors are masked with
its values too (a service account key's `private_key` included).

- `google_play_vitals` (`internal/adapter/datasource/playvitals`; capabilities `metrics`,
  `crashes`). Google Play Developer Reporting API v1beta1 at
  `https://playdeveloperreporting.googleapis.com`. Config `store_credential` (`google_play`),
  `package_name` (optional default). An OAuth2 token for scope
  `https://www.googleapis.com/auth/playdeveloperreporting` is minted from the credential's
  service account key (`gcloud.MintAccessToken`: the key's `token_uri` must be Google's) and
  cached per key until a minute before it expires (`tokencache`, an LRU of 512 tokens). The service account needs the
  Reporting API enabled in its Google Cloud project and permission to view app information in
  Play Console. Verify: with a package, `GET /v1beta1/apps/<package>/crashRateMetricSet`
  (detail `Google Play vitals · <package> · data through <day>`), else `GET
  /v1beta1/apps:search` (`· <n> app(s)`). **Query** `{"package"?, "metric_set", "metric",
  "dimension"?, "filter"?, "period"?: daily|hourly, "as"?: value|percent|free_percent}`:
  `metric_set` ∈ `crashRateMetricSet`, `anrRateMetricSet`, `slowStartRateMetricSet`,
  `errorCountMetricSet`, `metric` one of its metrics (`crashRate`, `userPerceivedCrashRate`,
  `…7dUserWeighted`, `…28dUserWeighted`, `distinctUsers`; `anrRate`, `userPerceivedAnrRate`, …;
  `slowStartRate`, …; `errorReportCount`, `distinctUsers` — the API refuses an unknown one, 400
  `bad_query`), `dimension` one breakdown (`versionCode`, `deviceModel`, `countryCode`,
  `apiLevel`, …: a series per value, labelled by it), `filter` the API's AIP-160 filter. `daily`
  (default) is calendar days in America/Los_Angeles, Play's only daily zone; `hourly` (UTC) only
  for the crash and ANR sets and not their rolling averages. `as` scales the decimal Play answers
  (a rate is a fraction of distinct users): `percent` ×100, `free_percent` 100 − ×100. **Series**:
  `GET` the metric set first for its freshness, then `POST
  /v1beta1/apps/<package>/<metric_set>:query {timelineSpec: {aggregationPeriod, startTime,
  endTime}, metrics, dimensions?, filter?, pageSize: 10000}` (pages followed, at most 20) over
  the window's days (hours), the end cut at the latest data Play holds — daily vitals lag a day
  or two, and a window reaching past it carries the warning "Google Play publishes vitals late:
  …"; one point per day (hour) at its start, `Step` 24 h (1 h). **Table** `{"package"?,
  "issues": all|crash|anr|non_fatal, "days"?: 1–30 (7)}`: `GET
  /v1beta1/apps/<package>/errorIssues:search` over the last `days` hours-aligned UTC before the
  instant, `orderBy=distinctUsers desc`, `filter=errorIssueType = CRASH|APPLICATION_NOT_RESPONDING|NON_FATAL`,
  pages of 100, at most 20 pages and 500 rows (`truncated` past either): the `crashes` columns
  (`issue` the issue id, `type` crash|anr|non_fatal, `cause`, `location`, `users`, `reports`,
  `last_version` its last versionCode, `last_seen` RFC3339, `link` Play Console's `issueUri`,
  kept only when it is an https page on `play.google.com`, else `""`).
- `app_store_metrics` (`internal/adapter/datasource/appstoremetrics`; capabilities `metrics`,
  `crashes` — the `signatures` table is its crashes-type table). App
  Store Connect API at `https://api.appstoreconnect.apple.com`. Config `store_credential`
  (`asc`), `app_id` (optional default: the app's App Store Connect id). A JWT (ES256, the key's
  `key_id`/`issuer_id`/`.p8`, `appstore.MintToken`, 15 min) is reused until a minute before it
  expires. Verify reads `/v1/apps/<id>` (detail `App Store Connect · <name>`) or `/v1/apps`.
  **Query** `{"app"?, "metric", "percentile"?: p50|p90, "device"?, "version"?}`: `metric` an
  Xcode Organizer metric identifier (`launchTime`, `hangRate`, …) or a whole category (`launch`,
  `hang`, `memory`, `disk`, `battery`, `termination(s)`, `animation`, `storage`: every metric of
  it). `GET /v1/apps/<id>/perfPowerMetrics?filter[platform]=IOS&filter[deviceType]=<device,
  default all_iphones>[&filter[metricType]=<category>]` (Accept
  `application/vnd.apple.xcode-metrics+json`) answers values **per app version**, not over time,
  at p50 and p90 only; the version dates come from `GET /v1/apps/<id>/appStoreVersions`
  (`createdDate`, paged via `links.next`). So a **series** is a staircase: at every step of the
  window, the value of the newest version created by then (none before the first), one series per
  metric and percentile (named `p50`/`p90` for one metric, labels `metric`, `device`, `quantile`,
  `unit`), always with the warning that these are per version; a version with metrics but no
  version record is left out (warned); an app without metrics yet (404) answers no series and a
  warning. Apple reports **no crash-free rate** — bind `mobile.crash_free_users` to Sentry (or
  Crashlytics). **Table** `{"app"?, "diagnostics": all|hangs|launches|disk_writes, "build"? |
  "version"?}`: the build's `GET /v1/builds/<id>/diagnosticSignatures` (`filter[diagnosticType]`
  HANGS|LAUNCHES|DISK_WRITES; App Store Connect has no crash signatures there), `build` an App
  Store Connect build id, else the newest upload of `version`, else the newest upload (`GET
  /v1/builds?filter[app]=…&sort=-uploadedDate&limit=1`); `links.next` is followed only on the API's
  own host and scheme, at most 5 pages / 500 rows: the `signatures` columns (`issue` the
  signature id, `type` hangs|launches|disk_writes, `cause` the signature, `weight` its share of
  the build's events in percent, `build` the build number).
- `sentry` (`internal/adapter/datasource/sentry`; capabilities `metrics`, `errors`, `crashes`).
  Sentry's Web API at config `url` (default `https://sentry.io`; `https://de.sentry.io` for an EU
  organization, or a self-hosted Sentry — a destination the URL policy judges like any URL, and
  moving it to another origin re-asks for the token), `org` (slug, `[A-Za-z0-9][A-Za-z0-9_-]*`);
  secret `token` (`Authorization: Bearer`; scopes `org:read`, `project:read`, `event:read`).
  Verify reads `GET /api/0/organizations/<org>/` (detail `Sentry · <name>`). Discover lists
  `GET /api/0/organizations/<org>/projects/` as `project/<slug>` (at most 2 000). Pages follow
  the `Link` header's `rel="next"` while `results="true"`, taking only its `cursor`, never its
  URL. A slug is resolved to the project id first (`GET /api/0/projects/<org>/<slug>/`; a
  numeric project is used as given). **Query** `{"project", "metric"?, "environment"?,
  "release"?, "search"?, "days"?}`: `metric` (default `events`) `crash_free_users`,
  `crash_free_sessions`, `crash_rate` (crashed sessions), `crash_rate_users` — percent (Sentry
  answers a fraction) —, `sessions`, `users` (per interval) from release health `GET
  /api/0/organizations/<org>/sessions/?field=crash_free_rate(user)|…&project=<id>&interval=<1–24
  h, dividing a day>&start&end&includeTotals=0` (`Step` that interval; release health needs the
  SDK's session tracking), or `events` (error events per interval) and `event_rate` (per second)
  from `GET /api/0/organizations/<org>/events-stats/?yAxis=count()&dataset=errors&interval=<n>m`;
  `environment` narrows every read, `release` adds `release:"<v>"` to the search, `search` (Sentry
  search syntax) to an events or issues read. **Errors** (`port.ErrorsQuerier`, the errors inbox):
  `GET /api/0/organizations/<org>/issues/?project=<id>&query=<search, default
  is:unresolved>&start&end&sort=freq&limit` (paged up to the limit) as error groups (see
  "Errors bindings join"); when more issues match than the limit, a fifth of it goes to the
  newest ones first seen in the window that the most frequent leave out (a second read,
  `sort=new`), so a new rare issue still reaches the inbox. An issue's `count` and `userCount`
  are Sentry's over the window asked (`start`/`end`, `environment`; `lifetime` is all time), and
  its `filtered` counts, when Sentry answers them (a search with conditions, a release), are
  used instead. **Table** (`crashes`): the same issues over the last `days` (1–90, 14) before the
  instant, the most frequent first, at most 500: `issue` the short id, `type` `crash` for an
  unhandled issue else its level, `cause` the title, `location` the culprit, `users`, `reports`
  (events), `last_seen`, `link`; no `last_version`. A permalink (`external_url`, `link`) is kept
  only when it points at this Sentry — the configured host over https (or the scheme the URL is
  configured with), or for Sentry's cloud an https page on `sentry.io` or a subdomain of it —,
  else it is `""`.

**Signal.** `errors.issues` (kind `error`, unit `issues`, roles backend, worker, frontend,
mobile, desktop) is bound like any signal (recipes `sentry-mobile`, `sentry-web`,
`sentry-backend`, or a raw Sentry query) but never read as a series (`unsupported`, as for
`logs.lines`): the errors inbox reads it.

## Web analytics and APM data sources

Five kinds read hosted analytics and APM products. All dial through
`internal/adapter/datasource/httpapi` (the URL policy — fixed hosts included —, pinned addresses,
same-host redirects, base-path containment, size caps, masked errors) and name a refusal with
`httpapi.Conn.Refusal`: 400/422 `bad_query`, 401/403 "authentication failed" plus what the
credential needs, 404 not found, 429 "rate limited (HTTP 429); try again in a minute" (an upstream
error, never `bad_query`). Each implements `MetricsQuerier`, `TableQuerier` and `Discoverer`.

**Web analytics** — `posthog`, `ga4`, `plausible` (and `vercel`, below) (capabilities `metrics`, `rum`). **Query**
(`domain.WebQuery`, unknown keys refused): a series `{"metric"?, "event"?, "quantile"?, "host"?,
"property"? (GA4), "site"? (Plausible)}`, a table `{"table": pages|referrers|countries|devices,
"days"?: 1–90 (7), "limit"?: 1–500 (100), "host"?, …}`. `metric` (default `page_views`):
`page_views` and `js_errors` per second (the signals' views/s and errors/s), `sessions`,
`visitors` and `events` (needs `event`) counts per interval, and PostHog's vitals `lcp`, `inp`,
`fcp` (ms) and `cls` (score) at `quantile` (default 0.75); GA4 has no vitals or errors, Plausible
only page views, sessions and visitors. `host` keeps one hostname. A bucket with no data is 0 for
a count and a gap for a vital. Tables answer their key columns (`path`; `referrer`; `country`;
`device`, `browser`, `os`) then `views`, `visitors` (and PostHog's `lcp_p75` on pages, GA4's
`sessions` on referrers), the most viewed first.

- `posthog` (`adapter/datasource/posthog`). Config `url` (default `https://us.posthog.com`;
  `https://eu.posthog.com`, or self-hosted with any path prefix), `project_id` (digits); secret
  `personal_api_key` (`Authorization: Bearer`, scope Query Read). Every call is `POST
  /api/projects/<id>/query/ {query: {kind: "HogQLQuery", query}}` with HogQL the kind writes
  itself: a value lands only in a single-quoted literal and one holding `'`, `\` or a control
  character is refused (`bad_query`) rather than escaped. Series bucket `intDiv(toUnixTimestamp(
  timestamp), <step ≥ 60 s>)` over `fromUnixTimestamp(from..to)` (at most 10 000 buckets):
  `$pageview` `count()` / `uniq(properties.$session_id)` / `uniq(person_id)`, `$exception`
  `count()`, a vital `quantile(q)(toFloat(properties.$web_vitals_<LCP|INP|CLS|FCP>_value))` of
  `$web_vitals` events; `host` adds `properties.$host = '<host>'`. Tables group `$pageview` (and,
  for pages, `$web_vitals`) events by `$pathname`, `$referring_domain`, `$geoip_country_code`,
  `$device_type`/`$browser`/`$os`. Verify counts the last day's events (`PostHog · project <id> ·
  <n> events in the last day`); Discover lists the last 7 days' event names, most frequent first
  (at most 2 000).
- `ga4` (`adapter/datasource/ga4`). Google Analytics Data API v1beta at
  `https://analyticsdata.googleapis.com`, `POST /v1beta/properties/<id>:runReport`. Config
  `property_id` (digits, not a `G-…` measurement id), `auth` `cloud_account` (default; field
  `cloud_account_id`, a connected `gcp` account) or `service_account` (secret
  `service_account_json`). A token for `https://www.googleapis.com/auth/analytics.readonly` is
  minted from the service account key (`gcloud.MintAccessToken`) and cached per key
  (`tokencache`); the account must be a Viewer of the property. Series: dimension `dateHour`
  below a one-day step, else `date`, metric `screenPageViews`/`sessions`/`activeUsers`/
  `eventCount`, `dimensionFilter` on `hostName`/`eventName` (EXACT); the date range is widened a
  day on each side, read in the answer's `metadata.timeZone` (the property's), then cut back;
  `Step` 1 h or 24 h. A window ending under 48 h ago warns "Google Analytics processes data for up
  to 48 hours; the newest points may still rise"; `subjectToThresholding` and
  `dataLossFromOtherRow` warn too. Tables: `pagePath`, `sessionSourceMedium`, `countryId`,
  `deviceCategory`/`browser`/`operatingSystem` over `<days-1>daysAgo`..`today`, ordered by
  `screenPageViews` desc, `truncated` when `rowCount` exceeds the rows. Verify reads 7 days'
  `activeUsers`; Discover lists the last 28 days' `eventName`s (at most 1 000).
- `plausible` (`adapter/datasource/plausible`). Stats API v2 `POST /api/v2/query` at config `url`
  (default `https://plausible.io`); `site_id`; secret `api_key` (Bearer). Series: metrics
  `pageviews`/`visits`/`visitors`, dimension `time:hour` below a one-day step else `time:day`,
  `date_range` the window as RFC3339, `filters [["is", "event:hostname", [host]]]`. Time labels are
  in the site's reporting zone, read at the offset Plausible echoes in `query.date_range` (UTC,
  with a warning, when it echoes none). Tables: `event:page`, `visit:source`, `visit:country`,
  `visit:device`/`visit:browser`/`visit:os`, metrics `pageviews`, `visitors`, `order_by visitors
  desc`. Verify reads 7 days' visitors; Discover answers `pageview` once the site recorded one in
  28 days.

- `vercel` (`adapter/datasource/vercel`). Reads a connected Vercel account (cloud_account field, its
  `token` and `team_id`), per project: query `{"metric"?, "project" (prj_…), "quantile"?}`, a table
  `{"table", "project", …}`; `host` is refused and there is no `sessions`. page_views/visitors:
  `GET /v1/query/web-analytics/visits/aggregate` (`projectId`, `teamId`, `since`/`until` RFC3339,
  `by=hour` below a one-day step else `day`; rows `{timestamp, pageviews, visitors}`; page_views per
  second). Vitals `lcp`/`inp`/`cls`/`fcp`: `POST /v2/observability/query` (`?teamId`; body `metric`
  `vercel.speed_insights.<lcp_ms|inp_ms|cls|fcp_ms>`, `aggregation` the nearest of p50/p75/p90/p95/p99
  to `quantile`, `scope {type: project, ownerId: team id or, for a personal account, `GET /v2/user`'s
  id, projectIds}`, `granularity {hours|days: 1}`; rows `{timestamp, <metric with _>_<aggregation>}`).
  Tables: `GET …/visits/aggregate?by=requestPath|referrerHostname|country|deviceType`, the `Others`
  group dropped. Web Analytics and Speed Insights are per-project switches (one project each on the
  free plan): before a read the adapter fetches `GET /v9/projects/<id>` and, when `webAnalytics` (for
  page views, visitors, tables) or `speedInsights` (for the vitals) has no `enabledAt` later than its
  `disabledAt`/`canceledAt`, answers `port.ErrSignalNotEnabled` (series error code `not_enabled`,
  with a hint to turn it on); a project Vercel will not describe is read anyway, and a 400 saying
  "not enabled" is the same state. Nothing is ever switched on. Verify reads `GET /v2/user`; Discover answers `pageview` and `speed_insights` once
  Verify passes (what a project has switched on is learnt when its signals are read). Capabilities
  `metrics`, `rum`.

**Analytics state** (`port.CloudAnalyticsReader`, Vercel's provider; `cloud.Service.Analytics`): the
readiness read (`GET …/observability`) carries `analytics {web_analytics, speed_insights}`
(`enabled|not_enabled|unknown`) for a Vercel environment, from the same project JSON, and the suggestion
`analytics_not_enabled` (goal `rum`) when either is off; the entity page words it on the gaps card and
on the tiles of the web signals that have no source.

**Sources made for cloud accounts** (migration 193, `cloud_account_data_sources`; `application/datasource/cloud_sources.go`): a
connected Vercel, Google Cloud or AWS account gets one `vercel`, `gcp_monitoring` or `aws_cloudwatch`
source with `cloud_account_id` as its only config, verified when made. `cloud.Service` announces
`CloudAccountConnected` (after create and after the legacy backfill) and `CloudAccountRemoved` (before
the account row goes) to an `AccountObserver`; `Service.SyncCloudAccounts` makes up for accounts
that predate it, at boot. A link row per account and kind makes it once: a source already there for
the pair is adopted, and a link whose `data_source_id` went NULL (the source was deleted by hand) is
never made again. Removal deletes only the sources the links name.

**APM** — `datadog` (capability `metrics`) and `newrelic` (`metrics`, `rum`, `crashes`); neither
searches traces (no `port.TracesQuerier`), so neither declares `traces`. A kind declares only
what it implements (`platform/runtime` tests it).

- `datadog` (`adapter/datasource/datadog`). Config `site` (select: `datadoghq.com`,
  `us3.datadoghq.com`, `us5.datadoghq.com`, `datadoghq.eu`, `ap1.datadoghq.com`, `ddog-gov.com`; no
  free-form host — the API is `https://api.<site>`); secrets `api_key`, `app_key` (`DD-API-KEY`,
  `DD-APPLICATION-KEY`). Query is a Datadog metric query (see the Datadog dialect above). Series:
  `GET /api/v1/query?from&to&query` (seconds), one series per answer series named by
  `display_name`, labelled by its `tag_set` (`k:v` → `k=v`; the scope when the set is empty);
  Datadog picks the rollup from the window, so `Step` is the largest `interval` it answered at. A
  `status: "error"` answer is `bad_query`. Table: the query over the 10 minutes before the instant,
  each series' latest non-null point a row of its tags plus `value` (so `… by {resource_name}` is a
  row per resource). Verify: `GET /api/v1/validate` and `GET /api/v1/metrics?from=<1 h ago>`
  (`Datadog · <site> · <n> metrics active in the last hour`); Discover: the metrics active in the
  last day, sorted, at most 10 000.
- `newrelic` (`adapter/datasource/newrelic`). NerdGraph `POST /graphql` at
  `https://api.newrelic.com` (`region` `us`) or `https://api.eu.newrelic.com` (`eu`); config
  `account_id` (digits); secret `api_key` (a user key, header `API-Key`). The GraphQL text is fixed
  — `actor.account(id: $id).nrql(query: $nrql, timeout: 30) { results }` — and the NRQL travels as a
  variable. Query is NRQL without SINCE, UNTIL, TIMESERIES or COMPARE WITH, and without a comment
  (`--`, `//`, `/* */`, which would swallow what the kind appends), each refused `bad_query`
  outside strings: a series appends `SINCE <from ms> UNTIL <to ms> TIMESERIES <n> minutes` (the
  step in whole minutes, raised to fit NRQL's 366 buckets) and answers every numeric value of a
  bucket as a series named by its key (an alias, `percentile.duration.95`), labelled `facet`; a
  table appends `SINCE`/`UNTIL` the hour before the instant unless it has its own SINCE (no
  TIMESERIES) and answers each result row: `facet` (a multi-attribute facet joined with ", "),
  string attributes, values by key. GraphQL errors are told apart by message: an API key or
  authorization one is an authentication failure, a syntax/invalid/NRQL one `bad_query`. Verify
  reads the account's name (`New Relic · <name>`); Discover is `SHOW EVENT TYPES SINCE 1 day ago`.

**Signals.** `web.visitors` (count, gauge, higher) and `web.fcp` (ms, quantile, lower), roles
frontend, join the catalog for these kinds.

## Signal bindings and series (migration 188)

`internal/application/signals`. A **signal binding** says where one signal of one component
environment is read: a data source plus a recipe and its params, or a raw `query` in the source's
own language, which wins when both are set. At most one binding per (component, environment,
signal) is `confirmed`; `suggested` ones may be several; a `dismissed` one is kept so the detector
never offers it again. `source` ∈ `detected|human|ai`. Deleting the component or the data source
deletes its bindings. Mounted only when the data source routes are. `env` ∈
`production|staging|preview|development` (absent → `production`, anything else → 400). Errors are
the data source routes' shapes: flat `{"error"}`, plus `fields` (400) or `code`; 404 for an unknown
component or binding id.

- **Binding** = `{id, component_id, environment, signal, data_source_id, data_source_name,
  data_source_kind, recipe_id, params, query, status, source, rendered_query, needs_params,
  created_at, updated_at}`. `needs_params` lists the recipe's params without a value (a raw query
  needs none); `rendered_query` is the query at a 60 s step (`$__quantile` at 0.95), empty while
  `needs_params` is not.
- `GET /v1/components/{id}/signals?env=` → `{component_id, role, environment, signals: [{signal,
  kind, unit, golden, status: bound|suggested|unbound, binding: Binding|null, suggestions:
  [Binding]}]}`. The signals are the role's (those listing it in `signals.yaml`, plus its golden
  ones) in catalog order, then any other signal that has a binding or a suggestion.
- `POST /v1/components/{id}/signals/suggest?env=` runs the detector, then answers as GET does.
  **(a)** Every source whose kind lists what it holds (`port.Discoverer`) is discovered (cached 5 min per source and
  `updated_at`); each recipe whose `detect.metric` it holds (and that names no `resource_kind`) is
  offered for the role's signals it maps. Params come from a label probe, `count by (<labels>)
  (<detect.metric>)` over the last hour, where the labels are those the recipe matches its params
  with (`label="$param"`): the label sets whose values best name the component win (its name,
  directory, package name or the name of a cloud resource it runs as in that same `env` —
  equal, then equal ignoring case and punctuation, then containing a name of at least 3
  characters), at most 3. Nothing naming it, or a language without such matchers → one suggestion
  with empty params and `needs_params` set. A log or trace source (`loki`, `elasticsearch`,
  `tempo`, which list labels, fields or tags as their inventory) is discovered the same way, but
  its probe lists each param's label values directly (`port.LabelValuesLister`: Loki
  `/loki/api/v1/label/<name>/values`, an Elasticsearch terms aggregation, Tempo
  `/api/v2/search/tag/<tag>/values`, over the last hour) and keeps those that best name the
  component by the same score — up to 3 for a one-param recipe, else each param's best; the
  params a query compares come from its language (`datasource.ParamFields`: `label="$p"`,
  `attr = "$p"`, a filter value that is exactly `"$p"`).
  **(b)** When the environment is `confirmed` and bound to a cloud resource, every recipe whose
  `applies_to.resource_kind` is that resource's kind, on every source whose
  `config.cloud_account_id` is the environment's account, params from
  `applies_to.resource_params`.
  **(c)** On a source whose kind lists its broker's consumers (`port.QueueDiscoverer`: Kafka
  consumer groups, RabbitMQ queues, NATS durable consumers, each a `domain.QueueTarget{Name,
  Params}`), the consumers whose `Name` best names the component (the same score as the label
  probe, at most 3) are offered through every recipe of the kind whose params are exactly the
  target's (`kafka-direct` `{group}`, `rabbitmq-direct` `{queue}`, `nats-direct` `{stream,
  consumer}`), one suggestion per role signal of that recipe, with the target's params — never a
  raw query holding a discovered name; the recipe's rendering escapes it.
  **(d)** For a **mobile** component whose repository has store apps linked
  (`signals.Deps.StoreApps`, the `mobile_store_apps` rows: one per platform), every recipe whose
  `applies_to.store_platform` is an app's platform, on every source of the recipe's kind, with
  params from the app (`applies_to.store_params`: `identifier` — bundle id or package name — or
  `store_app_id`, App Store Connect's id, empty until an app is chosen, leaving the param to
  fill). These come before the other suggestions. Where no source of the recipe's kind exists
  but the kind reads a store credential, the answer's `source_offers` lists it instead: `{kind,
  config: {<store_credential field>: <provider>}, recipe_id, params, signals, store_credential}`
  — `POST /v1/data-sources {kind, config}` creates it (the credential must be saved under
  Settings → Integrations first, else 400 `fields.store_credential: not_found`), and the next
  suggest offers the recipe. `source_offers` is `[]` on every other answer (GET, PUT, confirm).
  A kind that lists nothing (no `port.Discoverer`: the store kinds) is not asked during (a) and
  counts as having answered, so its old suggestions are replaced.
  **Brokers.** A component's messaging systems are the vendors of the `queue` resources its links
  reach (`signals.Deps.Links`; not dismissed, not missing) — the discovery catalog
  (`discovery/links/catalog.yaml`) links a messaging library to its broker: kafka (kafkajs,
  confluent, node-rdkafka, segmentio/kafka-go, sarama, franz-go, confluent-kafka-go, aiokafka,
  spring-kafka, kafka-clients, kafka-streams, the spring-cloud-stream Kafka binders, Quarkus and
  SmallRye reactive messaging Kafka, faust/faust-streaming, ruby-kafka/karafka, NuGet
  Confluent.Kafka), rabbitmq (amqplib, amqp091-go, streadway/amqp, pika, aio-pika, lapin, bunny,
  spring-amqp/spring-rabbit, amqp-client, the spring-cloud-stream Rabbit binder, Quarkus reactive
  messaging RabbitMQ, NuGet RabbitMQ.Client), nats (nats, nats.go, @nats-io/*, nats-py,
  async-nats, jnats, NuGet NATS.Client/NATS.Net/NATS.Client.Core), pubsub
  (`@google-cloud/pubsub`, `cloud.google.com/go/pubsub`, google-cloud-pubsub, NuGet
  Google.Cloud.PubSub.V1), sqs (`@aws-sdk/client-sqs`, sqs-consumer, aws-sdk-go-v2 sqs,
  aws-sdk-sqs, shoryuken, AWS Java SDK sqs, NuGet AWSSDK.SQS), servicebus (`@azure/service-bus`,
  azservicebus, azure-servicebus, azure-messaging-servicebus, NuGet Azure.Messaging.ServiceBus),
  redis (bullmq, bull, bee-queue, asynq, rq, sidekiq, resque, laravel/horizon). NuGet references
  are read from a `.csproj`/`.fsproj`'s `<PackageReference Include=…>`, ignoring case. Those names
  are what `applies_to.messaging_system` and the broker kinds (`kafka`, `rabbitmq`, `nats`, each
  its own system) use. Once they are known, suggestions reading one of the component's systems
  come first (before the per-signal cap); the others, and broker sources of another system, stay
  on offer after them, since the links know only the libraries the catalog names. Unknown (no such link, or
  the read fails) → as before. A signal with a confirmed binding gets no suggestion, a target
  dismissed (or offered by a person) for a signal is never offered again, at most 6 per signal, and
  only the detector's own pending suggestions are replaced — and only those of sources that
  answered: a source whose discovery, label probe or consumer listing failed keeps its pending
  suggestions (a broker source skipped as another system's answered). A
  suggestion offered again (same signal, source, recipe, params and query) keeps its row and `id`,
  so a confirm or dismiss sent from an older listing still finds it.
- `PUT /v1/components/{id}/signals/{signal}?env= {data_source_id, recipe_id?, params?, query?}` →
  Binding (`confirmed`, `source: human`), replacing the slot's confirmed binding in place (same
  `id`) and dropping its pending suggestions. 400 `fields`: `signal: invalid` (not a catalog
  signal); `data_source_id: required|invalid` (malformed or unknown); `recipe_id: required` (neither
  recipe nor query), `invalid` (unknown) or `invalid_option` (another kind's, or it does not map the
  signal); `params.<name>: required|unknown_field|too_long` (while the recipe drives the query; a
  param the recipe has a `defaults` value for may be left out, as `needs_params` and every read
  treat it);
  `query: too_long|invalid` (it references a param without a value).
- `POST /v1/signal-bindings/{bindingId}/confirm` → Binding; the slot's other confirmed binding and
  its pending suggestions are deleted. 400 `fields: {"params.<name>": "required"}` for a suggestion
  still missing a param; 404 for one another confirm of its slot already replaced. `POST …/dismiss`
  → 204, and dismisses only a `suggested` row (one already dismissed is no change); 409 `code:
  "confirmed"` for the confirmed binding, even one confirmed since it was listed (delete it
  instead). `DELETE /v1/signal-bindings/{bindingId}` → 204.
- **Concurrent writes.** Every write to one (component, environment, signal) — PUT, confirm, and
  the detector's replace — holds that slot's transaction-scoped advisory lock
  (`pg_advisory_xact_lock` on an FNV-1a hash of the slot), so two confirms or puts of a slot run one
  after the other and never deadlock. A write that still trips the confirmed-row unique index
  (23505) answers 409 `code: "conflict"` (PUT and confirm): read the slot again and retry.
- `POST /v1/components/{id}/signals/preview {signal, data_source_id, recipe_id?, params?, query?,
  from?, to?, step_seconds?}` → `{rendered_query, series, warnings}`: validated as PUT, read as the
  series route (`rendered_query` is the p95 one for a per-quantile read); errors as
  `POST /v1/data-sources/{id}/query` (400 `bad_query`, 422 `unsupported`, 502 `upstream`). A log
  or trace signal is 422 `unsupported`: it is read in the Logs explorer or the traces view.
- `GET /v1/components/{id}/series?env=&signals=a,b&from=&to=&step_seconds=&compare=1` → `{from, to,
  step_seconds, signals: [{signal, unit, bound, live?, series, previous, error: {code,
  message}|null, previous_error: {code, message}|null}]}`. `live: true` (absent otherwise) when
  the bound source reads only the value now (a direct broker kind, `domain.LiveOnlyKind`): its
  series hold one point however long the window, and a previous window has none.
  `to` defaults to now and `from` to an hour before it; `step_seconds` absent or 0 → about 120
  points, at least 15 s; at most 90 days and 11 000 points (400 `fields`). No `signals` → every
  metric signal of the role (`logs.lines` and `traces.spans` are no series). A quantile signal whose query takes `$__quantile` answers one series per
  quantile, named `p50`, `p75`, `p95`, `p99` (label `quantile`; p75 is the Core Web Vitals one). `compare=1` (or `true`) fills `previous`:
  the window before, shifted onto this one's timestamps (else `[]`); when only the previous window
  fails, `previous` is `[]`, `previous_error` says why and `series` stands. Each signal stands
  alone: `error.code` ∈ `unknown_signal`, `needs_params`, `unsupported` (not a metric, or a kind
  without metrics), `bad_query`, `upstream`, `timeout`, `not_found`, `error`; an unbound one is
  `bound: false` with no error. `error` (code `error`) is this server's own failure (a store or
  decryption error): its message is a fixed "reading the signal failed", the detail only logged.
  At most 4 queries at once, each cut off after 20 s, and 30 s for the whole read: a signal still
  unread then answers `timeout`. Once a source times out, its remaining queries in the same read
  are not made (`timeout`, "skipped: …"). `step_seconds` is the coarsest spacing any source
  answered at, never finer than asked: CloudWatch reads at whole-minute periods (5 min past 15
  days, 1 h past 63), so a 30 s step over CloudWatch answers 60. One read loads each data source's
  row and secrets once, however many signals it answers.
- `GET /v1/operations/golden?env=&from=&to=&component_id=…` → `{generated_at, components:
  [{component_id, signals: [{signal, unit, bound, live?, value, previous, spark: [[unix_ms,
  value|null]]}]}]}`. `bound` is whether the component has a confirmed binding for the signal, so
  an unbound signal reads apart from a bound one with no data; `live: true` as for the series
  route (the spark is one point).
  Every active component with a confirmed binding in `env` (at most 200, by name; `component_id`
  as for the errors inbox), with its role's golden signals from
  `catalog/observability/golden.yaml` (backend http.requests, http.error_ratio, http.latency;
  worker queue.backlog, queue.consume_rate, process.errors; frontend web.page_views, web.lcp,
  web.inp; mobile mobile.crash_free_users, mobile.cold_start, mobile.anr_rate; desktop
  mobile.crash_free_users, mobile.crash_rate, mobile.cold_start). `from`/`to` default to the last
  hour and are rounded down to the minute; at most 30 days. One query per bound signal covers [from
  − span, to] at span/30: `spark` is the window's ~30 points, `value` its last non-null one (the p95
  for a quantile signal), `previous` the last non-null one at or before that instant minus the
  span. An unbound or failing signal answers `null`s and an empty spark. At most 8 queries at
  once, each cut off after 20 s, and 25 s for the whole read: a signal still unread then answers
  `null`s; a source that timed out is not asked again within it. Answers are kept 60 s per (cache
  scope, env, window, component set, version of the sources and recipes read through — each
  source's `updated_at`, each recipe's content), dropped for the cache scope whose binding
  changes and for every scope when a source is saved or deleted; polls asking the same at once
  share one read.
- <a id="queue-tables"></a>`GET /v1/components/{id}/queues?env=` → `{component_id, environment,
  read_at, queues: [{signal_binding_id, signal, data_source_id, data_source_name, recipe_id,
  messaging_system, params, columns: [{key, label, unit}], key_columns?, rows: [{<column key>:
  string|number|null}], truncated, error: {code, message}|null}]}`. `read_at` (RFC3339) is when
  the tables were read — a cached answer is older than the request. `signal` is the naming
  binding's signal (to open its binding editor on an `error`). `key_columns` are the columns that
  name a row — a recipe table's `key_labels`, a broker kind's own (Kafka `topic`, `partition`;
  RabbitMQ `queue`; NATS `consumer`) — and are absent when unknown (a failed broker table). One table per distinct thing the component's **confirmed**
  `queue.*` bindings in `env` read, at the instant of the request, in signal catalog order:
  a binding whose recipe (of the source's kind) declares `tables.queues` reads that table with the
  params its queries use — two bindings of one source and recipe agreeing on those share one table
  (`params` lists only them; `signal_binding_id` is the first binding in catalog order); a binding
  on a source whose kind has the `queues` capability (a broker kind) hands its own rendered query
  (recipe or raw) to the kind's `Table` and answers the kind's columns and rows — grouped by what
  the query reads with its `metric` left out (`domain.BrokerTarget`; the kind's table ignores the
  metric), so a group's lag and consumers bindings share one table; any other binding
  (a raw PromQL query, a recipe without a table) has no table. `messaging_system` is the recipe's
  `applies_to.messaging_system`, else the broker kind. Each table stands alone: `error.code` ∈
  `needs_params` (a param the table reads has no value), `bad_query`, `unsupported`, `upstream`,
  `timeout`, `not_found` (the source is gone), `error`, with no rows (a recipe table with any
  column's query failing is wholly failed). At most 4 queries at once, each cut off after 20 s, 20 s
  for the whole read (a table still unread then answers `timeout`); one read loads each source's
  row and secrets once; requests asking the same at once share one read. Answers are kept 30 s
  (at most 128) per (cache scope, component, `env`, sources/recipes version, the tables' bindings
  and rendered queries), dropped as golden answers are. More than 500 rows keep the 500 with the
  largest first value column (a source answering one value per row is cut the same way before
  the join), in key order; `truncated` says so. 400 for a bad `env`, 404 for an unknown component.
- <a id="component-tables"></a>**Component tables** — `GET /v1/components/{id}/endpoints?env=`,
  `GET /v1/components/{id}/web?env=&table=pages|referrers|countries|devices` (default `pages`; any
  other is 400) and `GET /v1/components/{id}/crashes?env=` → `{component_id, environment,
  read_at, tables: [{signal_binding_id, signal, data_source_id, data_source_name, recipe_id,
  table, params, columns: [{key, label, unit}], key_columns?, rows, truncated, error: {code,
  message}|null}]}` (`domain.ComponentTables`; `read_at`, `signal` and `key_columns` — the
  recipe table's `key_labels` — as for queues). Each
  reads the recipe tables of the component's **confirmed** bindings in `env` of its signals — the
  endpoints view `http.*` bindings' `tables.endpoints`, the web view `web.*` bindings' table named
  by `table`, the crashes view `mobile.*` and `errors.issues` bindings' `tables.crashes` then
  `tables.signatures` — in signal catalog order, each with the params its queries use (a recipe
  `defaults` value fills one the binding leaves empty); bindings of one source and recipe agreeing
  on those share one table (`signal_binding_id` the first). A binding with a raw query, or whose
  recipe declares none of the view's tables, has none. Same machinery as the queues read
  (`signals.readTargets`): each distinct rendered query runs once, at most 4 at once, 20 s each,
  20 s for the whole read (`signals.WithTablesBudget`), the same per-table `error` codes, one
  read shared by requests asking at once, answers kept 30 s per (cache scope, view, component,
  `env`, version), dropped with the queue tables. Row order: a table whose value columns share one
  query (a named-column table: every web table, the crashes and signatures tables, New Relic's
  endpoints) keeps the source's order (the most viewed page, the most frequent issue first),
  unless the source answers one value per row in no order of its own (an instant PromQL vector),
  which is ranked by it; one joined from several queries (otel-http's and Datadog's endpoints) is
  ordered by its first value column, largest first (the busiest endpoint first). A cut at 500
  rows keeps the largest (otel-http's `rpm` query is a `topk(500, …)` for the same reason).
  **Endpoints convention**: the key column is the source's own label for the route (`http_route`,
  `resource_name`, New Relic's `facet`), labelled Endpoint, then `rpm` (req/min), `error_pct` (%),
  `p95_ms` (ms). 400 for a bad `env`, 404 for an unknown component.
- **Process-wide caches** (golden answers; queue and component tables; discovered recipe matches, 5 min, at most
  256 entries) are keyed by `signals.Service.SetCacheScope(func(ctx) string)` as well; local mode
  leaves it unset (scope `""`). A process serving several tenants must set it to the tenant on the
  context; a binding written in one scope then drops only that scope's answers (a source change,
  which comes with no request, still drops every scope's).

**Table.** `signal_bindings` — `component_id` (→ `project_components`, cascade), `environment`,
`signal`, `data_source_id` (→ `data_sources`, cascade), `recipe_id` (`''` for a raw query),
`params` JSONB, `query` (`''` for a recipe), `status`, `source`; a partial unique index keeps one
`confirmed` row per (component, environment, signal).

**Company edition port.** The shared cloud runs one process for every tenant, so it must set
`SetCacheScope` to the tenant, and re-key both the partial unique index
`uq_signal_bindings_confirmed` and the `ON CONFLICT (…) WHERE status = 'confirmed'` target of
`PutConfirmedSignalBinding` (`adapter/storage/postgres/signal_bindings.go`) to lead with
`tenant_id` — `(tenant_id, component_id, environment, signal)`.

## Logs and traces data sources

Three kinds read a component's log lines and traces from a store of their own rather than from
the cloud it runs on: `loki`, `elasticsearch`, `tempo`. What they can do is `port.LogsQuerier`
(`Logs`, newest first), `port.TracesQuerier` (`Trace`, `SearchTraces`; a trace the source does
not hold is `port.ErrNotFound`) and `port.LabelValuesLister` (one label's values, for the
detector); each is also a `port.Discoverer` whose inventory is its label, field or tag names, so
a recipe's `detect.metric` names one of those. They share `internal/adapter/datasource/httpapi`:
every request goes through the URL policy and is pinned to the address it vetted, a redirect may
change neither host nor scheme, a request path may not climb out of the base URL (`..`), an answer
is size-capped, and errors name the host as typed and mask every secret (the encoded basic-auth
value too). `loki` and `tempo` take config `url`, `auth` `none|bearer|basic`, secrets `token`,
`password` (with config `username`) and `headers` (`X-Scope-OrgID` for a multi-tenant
install), as the Prometheus kind does.

- `loki` (`internal/adapter/datasource/loki`; capabilities `logs`, `metrics`). Verify reads
  `/loki/api/v1/status/buildinfo` (`info.version`, detail `Loki`), falling back on anything but
  success or 401/403 to `/ready` (`ready`). **Logs**: `GET /loki/api/v1/query_range`, `direction=
  backward`, `limit` (≤ 5000, Loki's default `max_entries_limit_per_query`; it counts across
  streams), `start`/`end` in ns (`end` = until + 1 ns: Loki's end is exclusive). The query must be
  a log query (`{…} | …`, else 400 `bad_query`). The console's filters go on a line of their own
  after the query, so a `#` comment closing it cannot swallow them: `text` becomes a line filter
  `|= "<text quoted as a Go string>"`; `min_severity` one label filter per level label, each
  dropping lines whose level is known to be lower: `| detected_level!~"(?i)^(trace|debug|…)$" |
  level!~"…" | severity!~"…"`. A line with none of those labels passes them and is judged by its
  text, so a page can come back shorter than `limit` (`truncated` still says the window had a
  full page). Each line's level is the first of the labels or structured metadata
  `detected_level`, `level`, `severity`, `lvl`, `log_level`, `loglevel`, `severity_text`, else
  the line's own (`level=error`, `"level":"warn"`, a leading `ERROR`/`[warn]`), else `info`;
  `trace_id` from `trace_id`/`traceID`/`traceId`/`traceid`; `source` from `service_name`, `app`,
  `application`, `job`, `container`, `component`; `fields` every stream label and structured
  metadata (and a categorize-labels answer's `parsed`), at most 64. **Series** run a LogQL metric
  query through the same endpoint with `step` (a log query → 400 `bad_query`), so an error rate
  can be counted from lines (`loki-service`'s `process.errors`). Discover lists
  `/loki/api/v1/labels` over the last hour (at most 5000); label values
  `/loki/api/v1/label/<name>/values`.
- `elasticsearch` (`internal/adapter/datasource/elasticsearch`; capability `logs`; Elasticsearch,
  OpenSearch, Elastic Cloud). Config `url`, `index` (an index, data stream, alias or comma list
  of patterns, `logs-*,-logs-debug`; never one starting `_` or holding `/`, `?`, `#`),
  `time_field` (`@timestamp`), `message_field` (`message`), `level_field` (`log.level`), `auth`
  `none|basic|api_key`; secrets `password`, `api_key` (the encoded key, sent `Authorization:
  ApiKey <key>`), `headers`. Verify reads `GET /` (`info.version` the version number, detail
  `Elasticsearch|OpenSearch · <cluster> · <n> indices match <index>`) and refuses an index pattern
  matching no index or none with the time field (`_field_caps`). **Logs**: `POST
  /<index>/_search?ignore_unavailable=true&allow_no_indices=true`, `size` = limit (≤ 1000),
  `track_total_hits: false`, sorted by the time field descending (`unmapped_type: date`) then
  `_doc` descending (the tie-breaker `search_after` needs between lines of one timestamp; no
  point in time, which `_shard_doc` would need), a bool
  query whose `filter` is the window (`range`, epoch milliseconds, both ends inclusive), each
  query filter as a `match` with `operator: and` (exact on a keyword field, every word on a text
  one), and `text` as a `match_phrase` on the message field; `min_severity` adds a `must_not`
  `terms` on the level field of the lower level words (lowercase, UPPER and Title), and a line
  with no level field is judged by its message. A full page answers `truncated` and
  `next_cursor`: the last hit's sort values, base64url JSON, sent back as `search_after`
  (`domain.LogQuery.Cursor`; numbers kept exact, so a `date_nanos` sort value round-trips).
  Hits: `_source` flattened to dotted keys (so `log.level` reads the same nested or dotted),
  `timestamp` from the time field (RFC3339, epoch ms/ns) else the sort value, `message` from the
  message field else `event.original`/`log`/`msg`, `source` `service.name`, `trace_id`
  `trace.id`, ECS `http.request.method`, `url.path`, `http.response.status_code`, `fields` the
  rest (at most 64, values cut at 1024). A 400 is `bad_query` with the cluster's reason. Discover
  lists the pattern's field names (`_field_caps?fields=*`, at most 5000); label values are a
  `terms` aggregation (500) over the last hour, retried on a text field's `.keyword`.
- `tempo` (`internal/adapter/datasource/tempo`; capability `traces`; Grafana Tempo and Grafana
  Cloud Traces, whose URL ends in `/tempo`). Verify reads `/api/status/buildinfo`, falling back
  to `/ready`. **Trace**: `GET /api/traces/<id>` (OTLP JSON: `batches` or `resourceSpans`,
  `scopeSpans` or `instrumentationLibrarySpans`; ids in base64 or hex, answered as lowercase hex;
  404 → not found). Spans by start: `{id, parent_id?, service (resource service.name), name, kind
  (server|client|internal|producer|consumer), start, duration_ms, status (unset|ok|error),
  status_message?, attributes}` with the resource's attributes as `resource.<key>` (at most 32 of
  them, `service.name` always kept) and at most 128 of the span's own, values cut at 1024; the
  earliest 20 000 spans (`truncated` past them; only those are turned into spans), from an
  answer of at most 32 MiB. **Search**: `GET /api/search?q=<TraceQL>&start=&end=`
  (unix seconds, `end` rounded up) `&limit=&minDuration=<n>ms`, and the answer is held to
  `min_duration_ms` too (not every version applies it beside a TraceQL query). Summaries `{id
  (32 hex digits, the leading zeros Tempo drops put back), root_service, root_name, start,
  duration_ms, span_count (spans matched, across span sets), error}`, newest first; `error` is
  set on a trace that also matches `(<query>) && { status = error }`, a second search whose
  failure only leaves every `error` false. Discover lists `/api/v2/search/tags` as
  `<scope>.<tag>` (not intrinsics), or a Tempo before 2.2's `/api/search/tags` unscoped
  (`.service.name`); label values `/api/v2/search/tag/<tag>/values` (v1 on a 404).

**Signals.** `logs.lines` (kind `log`) and `traces.spans` (kind `trace`), roles backend, worker
and frontend, are bound like any signal (PUT, suggest, confirm; recipes `loki-service`,
`loki-app`, `elasticsearch-service`, `tempo-service`, or a raw query in the kind's language) but
are never a series: `series` leaves them out when no `signals` is named and answers
`unsupported` when one is, and preview is 422. The Logs explorer reads `logs.lines` bindings (see
"Operations: Errors inbox and Logs explorer"); the routes below read `traces.spans`. Mounted with
the signal routes.

- `GET /v1/components/{id}/traces?env=&since=&until=&min_duration_ms=&limit=` → `{component_id,
  environment, bound, data_source_id|null, traces: [TraceSummary], error: {code, message}|null}`.
  `until` defaults to now, `since` to an hour before; at most 7 days (Tempo's own search
  ceiling); `limit` 1–100 (default 20); `min_duration_ms` 0–86 400 000 (a day); 400 `fields`
  (`since: invalid|too_long`, `until`, `limit`, `min_duration_ms: invalid`), 400 for a bad `env`, 404 for an unknown component. An
  unbound component answers `bound: false` and `traces: []`; a binding that cannot be read
  answers `error` (`needs_params`, `bad_query`, `unsupported`, `upstream`, `timeout` — 20 s —,
  `not_found`, `error`), never a failed request.
- `GET /v1/components/{id}/traces/{traceId}?env=` → `{trace: {id, spans, truncated},
  component_id, data_source_id}`. `traceId` is 1–32 hex digits, any case (else 400, never sent);
  404 for an unknown component or a trace the source does not hold; 422 `code: "unbound"` without
  a confirmed `traces.spans` binding; 502 `upstream`, 504 `timeout` (20 s).
- `GET /v1/operations/traces/{traceId}?component_id=&env=` — the Logs explorer's `trace_id`
  link. With `component_id`, as above through that component's binding. Without, the id is
  looked up in each data source the environment's confirmed `traces.spans` bindings read (a
  dismissed component's left out; each source once, at most 8, 4 at a time, all within one 20 s
  query timeout): the first in binding order that holds it answers, with the `component_id` of
  the binding it was found through; 422 `unbound` when no component has such a binding, 404 when
  none holds it and none failed, else the last failure.

## Alert rules (migrations 189, 190)

`internal/application/alerting`. An **alert rule** is a condition on one catalog `signal` of one
component environment, read through that environment's confirmed binding, or on a raw `query`
against `data_source_id` (exactly one of the two). Mounted only when the signal routes are. Errors
are the data source routes' shapes: 400 `{"error", "fields"}` (codes `required`, `invalid`,
`invalid_option`, `too_long`, `unavailable`), 404 for an unknown rule id, plus 422 `code: "unbound"` where a rule
must be read and its signal has no confirmed binding.

- **Rule** = `{id, name, component_id, environment, signal, data_source_id, query, condition: {op,
  threshold, for_seconds, window_seconds}, severity, channels, open_incident, enabled, state,
  state_since, last_value, last_evaluated_at, error_count, last_error, created_by, created_at,
  updated_at}`. `error_count`/`last_error` (migration 192) are the evaluations in a row whose read
  failed for a reason that says nothing about the value and the last such failure (≤ 300 bytes);
  a read that works zeroes both, and so does an edit that restarts the rule. `signal` is `''` for a raw
  rule, `data_source_id` `null` and `query` `''` for a signal rule. `created_by` ∈ `human|ai`
  (migration 191): set on create and never changed — `ai` is a disabled draft the release
  engineer's `propose_alert_rule` stored, and it stays `ai` after a person enables it. `environment` ∈
  `production|staging|preview|development` (default `production`); `severity` ∈
  `critical|high|medium|low` (default `medium`); `channels` ⊆ `in_app, desktop, ios_push, slack,
  email, webhook` (default `[]`); `enabled` defaults to `true`, `open_incident` to `false`;
  `window_seconds` 60–86 400 (default 300), `for_seconds` 0–604 800 (default 0). `state` ∈
  `ok|pending|firing|no_data` since `state_since`; `last_value` is the last judged value (`null`
  with no data); `updated_at` moves only on an edit, never on an evaluation.
- **Ops.** Every value is the latest sample of a read over `[now − max(2·window, 15m), now]` at a
  step of one window, at most 1.5 *effective* steps old (a source labelling a period by its start
  still counts) — the effective step is the coarsest of the window, the spacing the source said it
  answered at (CloudWatch's period, `SeriesResult.Step`) and the closest two samples, so a source
  keeping one point every five minutes is judged every minute; the value a window earlier is the
  sample before it. A query answering several series is judged by
  its worst at each instant (the highest; the lowest for `lt` and `change_pct_lt`). A quantile
  signal is read at p95, a `web.*` one (Core Web Vitals) at p75. `gt`/`lt` compare the value with
  `threshold`; `rising` holds while the value is above `threshold` **and** higher than a window
  earlier (`threshold: 0` for a bare "is rising"; no sample a window earlier is no data); `change_pct_gt`/`change_pct_lt` compare the
  percent change from a window earlier (`last_value` is then that percent; a zero or missing
  earlier value is no data); `no_data` holds while there is no value (threshold unused). Only a
  read that cannot work is no data: `signals.ErrUnbound`, `signals`/`datasource.ErrInvalidInput`
  (a binding missing a param, a query naming one), `port.ErrDataSourceQuery` (the source refused
  the query), `datasource.ErrUnsupported`, `port.ErrNotFound` (the source is gone). Every other
  failure — the source down or slow (`upstream`, timeout), a store or decryption error, a panic,
  anything unexpected — leaves the rule where it is (its evaluation still recorded, so it waits
  its interval, with `error_count` and `last_error`), so one failed read never resolves a firing
  rule; past 10 such evaluations in a row the read counts as no data (a firing rule then moves to
  `no_data` after its grace, resolving). A `pending` rule whose run of failed reads ends starts its
  `for_seconds` over at that evaluation.
- **State machine**, one step per evaluation: holding → `pending` until it has held `for_seconds`
  (0: `firing` at once), then `firing`; not holding → `ok`; no value → `no_data`, except for a
  `no_data` rule, where it is the condition. Leaving `firing` takes longer (hysteresis, migration
  190 `clear_count`/`clear_since`/`no_data_count`, not in the JSON): a firing rule goes `ok` only
  after its condition read false for at least 2 evaluations in a row **and** for `for_seconds`, or
  5 minutes when that is longer, since the first of them (an evaluation reading nothing in between neither counts nor breaks the
  run; one holding again starts it over), and goes `no_data` only on the 4th evaluation in a row
  that read nothing (it stays `firing` through 3). Backtests replay the same rule, a step being
  an evaluation. Leaving `firing` (for `ok` or `no_data`) resolves.
  Every state change is an event. **Firing with `open_incident`** ingests an incident through
  prodops — source `alert_rule`, fingerprint `alert:<rule id>`, the component's repository, env
  `prod`/`stage`/`local`/`preview` for production/staging/development/preview, title `"<name>:
  <value>"`, the rule's severity, payload `{rule_id, rule_name, component_id, environment, signal,
  data_source_id?, query, condition, value}` — so its dedupe, remedy, policy and notifications
  apply; resolving ingests the recovery of the same fingerprint. A firing or recovery prodops
  refuses is tried again at the next evaluation (the rule stays pending or firing). **Without
  `open_incident`** firing and resolving notify on the rule's `channels` (none: no notification)
  with kind `alert_firing`/`alert_resolved`, source `alert`, source id the rule id, link
  `/operations/entity/<component id>/alerts?rule=<rule id>`. A repeat of the kind last told for
  the rule is held back inside 30 minutes; a firing after a told recovery (or a recovery after a
  told firing) goes out — unless the rule has changed state 4 times inside those 30 minutes: then
  every change is held until the rule has stayed in one state for 15 minutes, and the evaluator
  sends the one it ended on (nothing if that is what was last told). `notify.Cooldown`
  `WithFlapLimit`/`AllowTransitionHolding`/`Settled`; prodops applies the same rule to an alert's
  opened/recovered notifications, delivering what settled from a one-minute loop
  (`prodops.Service.Start`/`FlushSettled`). The hysteresis above keeps a condition near its
  threshold from flapping that fast.
- **Orphans.** Deleting a component or a data source deletes its rules in the database (cascade),
  with no evaluation left to resolve what a firing one opened; at most every 5 minutes a sweep
  resolves (prodops `Resolve`, note "the alert rule that opened it no longer exists") every live
  `alert_rule` incident whose `alert:<id>` rule no longer exists (`IncidentFilter.Source`, up to
  500).
- **Channels.** A rule may name only the channel kinds registered with the notify service
  (`notify.Service.Kinds`; the desktop app registers `desktop`); another known kind is
  `channels: unavailable`, an unknown one `invalid_option`. Only a request naming `channels` is
  checked, so a stored rule keeps saving. `GET /v1/notification-channels` →
  `{"channels": [{kind, available}]}`, every kind, for the rule form. Suggestions name `desktop`
  only where it is available.
- **Attribution.** An `alert_rule` incident's payload names its `component_id`; release
  attribution (`release.Service.AttributeIncident`, through `prodops.IncidentAttributor`) then
  considers only that component's releases, so a monorepo part's alert is never pinned on
  another part's deploy. An incident naming no component is attributed repository-wide as
  before; the deploy-run ledger stays the fallback either way.
- **Evaluator.** `Sweep` takes the enabled rules last evaluated at least `alerting.interval` ago
  (never-evaluated first, at most `alerting.max_rules_per_sweep`) and evaluates four at a time, each
  read cut off after 20 s; it runs every quarter interval. An evaluation's write is dropped when
  the rule was edited since it was read. Each sweep drops events older than 30 days. `Start` after
  `Stop` starts the loop again.
- **For the company port** (not done here): several replicas each running the evaluator will
  double-fire a notify-only rule (an `open_incident` one folds into one incident by fingerprint)
  until one is elected to run it; raw-query rules and `/backtest` read a data source directly, so
  they should require the same role as `POST /v1/data-sources/{id}/query`; and the global
  `alerting.enabled` loop (`Start`) must be off there, the control plane calling `Sweep` per
  tenant instead, with `alerting.Service.SetScope` (the orphan sweep's pacing and the flap
  cooldown are keyed by it) and `prodops.Service.SetScope` returning the tenant. The orphan
  resolver lists incidents with no repository filter, so it must only ever run inside one
  tenant's scope.
- `GET /v1/alert-rules?component_id=&env=` → `{"rules": [Rule]}` by name; both filters optional
  (absent `env` = every environment).
- `GET /v1/alert-rules/{id}` → Rule.
- `POST /v1/alert-rules` → 201 Rule, `state: ok`. Fields: `name: required|too_long` (≤ 200);
  `component_id: required|invalid` (malformed or unknown); `environment: invalid`; `signal:
  required` (neither a signal nor a query), `invalid` (not a catalog signal), `invalid_option` (not
  a metric); `query: required|too_long|invalid` (it names a param, which a raw rule has no value
  for) or `invalid_option` (beside a signal); `data_source_id: required|invalid|invalid_option`
  (beside a signal, or a source without metrics); `condition.op: required|invalid`;
  `condition.window_seconds: invalid|too_long`; `condition.for_seconds: invalid|too_long`;
  `severity: invalid`; `channels: invalid_option|unavailable`.
- `PATCH /v1/alert-rules/{id}` → Rule. Absent fields keep their value; naming a `signal` and neither
  `query` nor `data_source_id` turns a raw rule into a signal rule (and a `query` or source without
  `signal` the other way). Changing what the rule reads or tests (component, environment, signal,
  source, query, condition), `open_incident` or `enabled` restarts it from `ok` — a firing rule is
  resolved first; a new `name`, `severity` or `channels` keeps its state.
- `DELETE /v1/alert-rules/{id}` → 204; a firing rule is resolved first.
- `GET /v1/alert-rules/{id}/events?limit=` → `{"events": [{at, from_state, to_state, value,
  incident_id}]}`, newest first; `limit` 1–500 (default 50; else 400 `fields.limit`).
  `incident_id` is the incident a firing opened or a recovery resolved.
- `POST /v1/alert-rules/backtest` a rule's fields (no `name` needed) + `"days"` (1–14, default 7)
  → `{"fires": [{start, end, peak}], "series": [{name, points}], "step_seconds"}`. Replays the
  state machine over the last `days` at a step of one window (wider, in whole minutes, past 2 000
  steps), from `ok`: `end` is `null` for a firing still on at the end, `peak` the worst value while
  firing. `series[0]` is what was read (named after the signal, or `query`), `[[unix_ms, value]]`;
  a change rule adds `change_pct`, the percent it judged at each step. The read failing is the
  answer (422 `unbound`, 400 `bad_query`, 502 `upstream`, …).
- `POST /v1/alert-rules/suggest {component_id, environment?}` → `{"rules": [Rule without id and
  state]}`, nothing saved: the role's templates for signals bound in the environment that no rule
  of the component environment already tests with the same op. Thresholds from the last 14 days
  of the signal at a 10-minute step (nearest-rank percentiles; under 12 samples the fallback):
  backend `http.error_ratio` `gt` max(2, p99 × 1.3) for 5 m, high, incident; backend
  `http.latency` `gt` 2 × the 14-day median of its p95 (fallback 1000 ms) for 10 m, medium;
  worker `queue.backlog` `rising` above max(1000, p99 × 1.3) for 10 m over a 10-minute window,
  high, incident (the "above and rising" pair is one rule, since `rising` has a floor); mobile and
  desktop `mobile.crash_free_users` `lt` 99.5 for 30 m, high, incident; frontend `web.lcp` (p75)
  `gt` 2500 ms for 15 m, medium. Thresholds keep 3 significant digits; `channels: ["desktop"]`
  (`[]` where `desktop` is not available).
  400 `fields`: `component_id: invalid` (malformed or unknown), `environment: invalid`.

**Tables.** `alert_rules` — `component_id` (→ `project_components`, cascade), `environment`,
`signal` (`''` for a raw rule), `data_source_id` (→ `data_sources`, cascade; `NULL` for a signal
rule), `query`, `condition` JSONB, `severity`, `channels` TEXT[], `open_incident`, `enabled`,
`state`, `state_since`, `last_value`, `last_evaluated_at`, and (190) `clear_count`, `clear_since`,
`no_data_count`, and (191) `created_by`; a CHECK keeps signal and raw query exclusive. `alert_events` — `rule_id` (cascade), `at`, `from_state`, `to_state`, `value`,
`incident_id` (no foreign key).

## Error definitions and AI suggestions (migration 191)

Migration 192 adds the job, claim and suggestion columns and indexes noted below.
`internal/application/errordef` (definitions, matching, the action sweep) and
`internal/application/opsai` (the release engineer's suggestion jobs, the "enable observability"
task, readiness). Mounted when Postgres and the workflow service exist; the job and component
routes also need the AI service. Errors are the data source routes' shapes: 400 `{"error",
"fields"}` (codes `required`, `invalid`, `invalid_option`, `too_long`, `not_permitted`), 404 for an
unknown id, 409 `code: "not_suggested"` (accept/dismiss of a definition that is not a suggestion),
409 `code: "too_many_definitions"` (the repository already holds 200 active and suggested
definitions), 409 `code: "no_release_engineer"` (no enabled agent holds role `release` for the component's area,
or its fallback), 503 `code: "unavailable"` (no chat sessions or no board on this server).

**Definition** = `{id, repository_id, component_id|null, name, match: {fingerprint?, message_regex?,
exception_type?, endpoint?, status?, attributes?}, category, severity, action, alert_threshold:
{count, window_seconds}|null, source, status, rationale, evidence: [string], task_id|null,
firing_since|null, created_at, updated_at}`. `component_id` `null` covers every component of the
repository. `category` ∈ `bug|expected|dependency|infrastructure|user_error`; `severity` ∈
`critical|high|medium|low` (default `medium`); `action` ∈ `none|ignore|alert|incident|task`
(default `none`); `source` ∈ `human|ai|code`; `status` ∈ `active|suggested|dismissed`. Limits: name
≤ 200, rationale ≤ 4000, ≤ 20 evidence entries of ≤ 500, fingerprint ≤ 500, exception_type ≤ 200,
endpoint ≤ 500, `status` 100–599, ≤ 20 attributes (key ≤ 100, value ≤ 500), `alert_threshold`
only on `alert`/`incident` (else `invalid_option`), `count` 1–1 000 000, `window_seconds`
60–604 800.

**Matching** happens on read; nothing per group is stored. A definition matches a group when every
`match` field that is set holds (AND; an empty match is refused). `fingerprint` equals the group's;
`message_regex` (RE2, ≤ 512 characters, compiled program ≤ 1000 instructions — `too_long` past
either, `invalid` when it does not compile) is tested against the group's message, then its sample,
each cut to 4 KiB; `exception_type` equals the identifier before the first `:` or `(` of the
message's first line, else of the sample's last or first line (a bare `NullPointerException`
also matches `java.lang.NullPointerException`, likewise after `::` and `\`); `endpoint` is the
`METHOD /path` printed in the message or sample — the definition's may name the method
(`POST /v1/charges`), a trailing `*` matches any rest of the path, a `*`, `:id` or `{id}` segment
any one segment; `status` is a status printed as `status 502`, `status_code=502`, `HTTP 502` or
after a request line (`GET /x 503`); `attributes` are exact pairs among the message's `key=value`
tokens plus `source` and `provider`. Only `active` definitions match. Of several that match, a
component's own beats a repository-wide one, then the more specific (a fingerprint counts 10, every
other field 1, each attribute 1), then the most recently edited; that one is the group's
`definition` in the inbox. Active definitions are cached per repository for 30 s; any write through
the service clears that repository's entry. A repository holds at most 200 active and suggested
definitions (`POST` past that is 409 `too_many_definitions`; a proposal past it is rejected with
`fields: {definitions: too_long}`).

**Actions.** `ignore` hides the group from the Errors inbox (above). The others are carried out by
a sweep (every 2 min, started when `alerting.enabled`) over **production** only — definitions have
no environment. Each sweep lists the active definitions once and judges by that list. It reads
`GET /v1/operations/errors`'s fan-out per repository and distinct threshold window, at most 50
components per read; a window is read at most once per twelfth of it (and at most every 2 min), and
one sweep makes at most 8 reads, the due windows read longest ago first. A group counts toward a
definition only when that definition acts on it: per action class — `alert`/`incident` (both open
an incident) and `task` — the best matching definition of that class acts, unless a better
matching `ignore` definition silences it; a `none` definition silences nothing, and an `alert` and
a `task` definition may both act on one group. `alert`: while the matched groups' summed `count`
over the window is ≥ `count` (default 10 in 3600 s) one incident is opened through prodops
(`source: error_definition`, fingerprint `errdef:<id>`, the definition's severity, env `prod`;
title `<name>: <count> in <window>`; detail the five largest groups as one sanitized line each;
payload with the fingerprints) and `firing_since` is set; once below, it is resolved and cleared —
but never on a read that missed one of the definition's own components (unavailable, failed or
truncated). A repository-wide definition counts an error tracker's issue once however many
components bind the same source (one per `data_source_id` and fingerprint). `incident`: the same
crossing (default 1 in 3600 s) opens it, and dropping below only clears `firing_since`; a person
resolves the incident. Deleting a firing `alert`/`incident` definition, or changing its action to
one that opens none, resolves its incident at once, and each sweep resolves the live
`error_definition` incidents whose definition no longer exists or no longer opens one (deleted with
its component, for example). `task`: one board task per (definition, fingerprint) —
`error_group_links` is claimed before the task is opened (`claimed_at`, migration 192) and released
if opening fails; a claim that opened no task for 10 min is claimed again — at most 1 per
definition and 5 in all per sweep, groups by count; looked for over the last 24 h. The task is the workspace's defect type, in `todo`, priority from severity, `component_id` the
group's, assigned to the agent holding role `developer` for the component's area
(`domain.ComponentArea`: frontend and desktop → frontend, mobile, data, game, everything else
backend; with `AreaFallbacks`), unassigned when none; its brief is
`catalog/system/prompts/observability/error_task.md`. The definition's `task_id` is its first task.

- `GET /v1/error-definitions?repository_id=&component_id=&status=` → `{definitions}`, newest first.
  `component_id` answers that component's definitions and the repository-wide ones. A malformed id
  is 400 `{"error"}`; an unknown `status` is 400 `fields`.
- `POST /v1/error-definitions` `{repository_id, component_id?, name, match, category, severity?,
  action?, alert_threshold?, rationale?, evidence?}` → 201 definition, `source: human`, `status:
  active`. `component_id` must be a component of `repository_id` (`invalid_option`).
- `PATCH /v1/error-definitions/:id` — any of the same fields; absent keeps, `component_id: ""`
  makes it repository-wide, `alert_threshold: null` clears it, `match` replaces the whole match;
  a different `repository_id` is `not_permitted`. Changing `action` clears `firing_since`; status
  and source never change here.
- `DELETE /v1/error-definitions/:id` → 204; resolves the incident a firing `alert`/`incident`
  definition holds open.
- `POST /v1/error-definitions/:id/accept` → the definition, `status: active` (its `source` stays
  `ai`/`code`). `POST /v1/error-definitions/:id/dismiss` → `status: dismissed`; a dismissed match is
  never suggested again. Either on a definition already in that status answers it unchanged; on any
  other non-suggestion → 409 `not_suggested`.

**AI jobs.** A job is one chat turn with the release engineer (`session.Service.SendMessage`, no
orchestration, the server's default tool policy merged with the agent's own), titled from
`observability.job_title`, in a new chat on the repository. The turn carries a tool ceiling
(`SessionMessageRequest.ToolCeiling`, `domain.CeilToolPolicy`): it gets only the tools both the
agent's policy and its kind's list allow, and no user MCP server — `define_errors`:
`list_error_groups`, `propose_error_definitions`; `error_catalog`: `propose_error_definitions`;
`map_signals`: `list_data_source_metrics`, `propose_signal_binding`, `propose_alert_rule`; every
kind also `codebase_search`, `grep_code`, `read_file`, `get_repo_tree`, `get_project_brief`,
`load_skill` (`opsai.JobTools`). The turn is never parked on a usage limit (`NoQuotaPark`): the
job fails with the limit's notice instead of resuming after it ended. Its tools find the job
through the chat session on the tool call's context and append what they stored to its `created`,
once per item and only while it runs. Inside a job they refuse a `repository_id` or component
other than the job's (`guard.observability_outside_job`), and `list_data_source_metrics` lists,
and the propose tools accept, only data sources a component of the job's repository reads through
(a confirmed binding in the job's environment) or no component reads through yet; outside a job a
run bound to a repository may name only that one (`guard.observability_outside_run_repository`).
At most 2 jobs run at once (`opsai.Service.SetScope` makes that per scope); a job that
waits 30 min for a slot fails, and one that runs 30 min is stopped (`session.CancelSession`) and
fails. A job of the same kind, repository, component and environment that is still running is
answered instead of starting another (`uq_ai_jobs_running`, migration 192); a running one that
started more than 35 min ago, or has waited more than 35 min, is failed as stale first. At boot the
jobs another process created (`owner`) are failed with `error: "interrupted by a restart"`.

- **Job** = `{id, kind, repository_id, component_id|null, environment, status, session_id|null,
  result_summary, error, created: [{kind, id, name, rationale?}], created_at, started_at|null,
  updated_at, finished_at|null}`. `started_at` is when it took a slot (null while it waits). `kind` ∈ `define_errors|error_catalog|map_signals`; `status` ∈
  `running|done|failed`; `created[].kind` ∈ `error_definition|signal_binding|alert_rule` (only what
  this job newly stored: a duplicate proposal, or one matching a suggestion from before the job, is
  not listed). Two equal suggestions (repository, component, match) cannot both be stored
  (`uq_error_definitions_suggested`); the second is answered as a duplicate. `result_summary` is the agent's final reply
  (≤ 2000 bytes); `failed` carries `error` — the turn's error, or "the release engineer stopped on a
  question; answer it in the chat" when it asked one (`session_id` is that chat).
- `POST /v1/error-definitions/suggest` `{repository_id, component_id?, environment? (default
  production), mode?: "errors" (default) | "code"}` → 202 `{job_id, job}`. `errors` reads the
  recent error groups and proposes definitions (`source: ai`); `code` reads the code's own error
  types and codes and proposes `expected`/`user_error` ones (`source: code`). Prompt
  `catalog/system/prompts/observability/define_errors.md`.
- `POST /v1/components/:id/signals/ai-suggest?env=` → 202 `{job_id, job}`: the agent reads the data
  sources' inventories and proposes bindings (`source: ai`, `status: suggested`, through
  `signals.Service.Propose` — the same checks as `PUT …/signals/:signal`; a target already offered
  or dismissed for that signal is answered as it stands) for the component's unbound signals,
  golden first, and disabled alert rules (`created_by: ai`) for the golden ones. Prompt
  `observability/map_signals.md`.
- `GET /v1/ai-jobs/:id` → job.

**Enable observability.**

- `GET /v1/components/:id/observability?env=` → `{component_id, environment, role, provider,
  signals: [{signal, status, golden}], instrumentation: {otel, sentry, datadog, prometheus,
  posthog, crashlytics, newrelic, web_vitals: bool}, suggestions: [{goal, code}]}`. `signals` is
  `GET …/signals`'s list; `provider` the cloud of the environment's confirmed, bound binding (`""`
  without one); `instrumentation` is read from the component's libraries and frameworks (a
  lowercased name holding `opentelemetry`/`otel`, `sentry`, `dd-trace`/`ddtrace`/`datadog`,
  `prometheus`/`prom-client`, `posthog`, `crashlytics`, `newrelic`, `web-vitals`) and from the
  vendors of the observability resources its links reach. `suggestions[].code` ∈
  `golden_unbound`, `golden_partly_unbound` (goal `metrics`), `no_tracing_sdk` (backend/worker with
  no OTel, Datadog or New Relic), `no_log_source` (backend/worker, `logs.lines` unbound and no cloud
  environment bound), `no_rum` (frontend with no web-vitals, Sentry, PostHog or Datadog),
  `no_crash_sdk` (mobile/desktop with no Sentry, Crashlytics or Datadog), `queues_unbound` (a
  `queue.*` signal unbound); the UI words them.
- `POST /v1/components/:id/observability/enable` `{environment? (default production), goals:
  [metrics|traces|logs|rum|crash|queues], target: own_cloud|self_hosted|otlp_endpoint,
  otlp_endpoint?}` → 201 `{task_id, task_key}`. `otlp_endpoint` (http/https, ≤ 2048) is required
  with `otlp_endpoint` and refused otherwise (`invalid_option`); an unknown goal is
  `invalid_option`, none `required`. Opens a `technical` task in `todo` on the component, assigned
  to the release engineer for the component's area, titled `Enable observability: <component>
  (<env>)` — while that task is still open (short of `done`/`released`), asking again answers it
  instead of opening another — whose description is `observability/enable_brief.md` rendered with the component, its
  stack, the environment's provider, bound and unbound golden signals, detected instrumentation,
  goals and target. The brief has the release engineer define the work and open the developer's
  task with `create_board_task` (`assignee_role: developer`), park its own card on it with
  `update_board_task` `blocked_by`, and, woken again when it lands, verify the telemetry and
  propose bindings and disabled rules; it never edits code.

**Tables.** `error_definitions` — `repository_id` (cascade), `component_id` (→
`project_components`, cascade, nullable), `name`, `match` JSONB, `category`, `severity`, `action`,
`alert_threshold` JSONB (nullable), `source`, `status`, `rationale`, `evidence` JSONB, `task_id` (→
`board_tasks`, set null), `firing_since`; CHECKs on every enum. `error_group_links` — PK
`(definition_id, fingerprint)` (definition cascade), `component_id`, `environment_id`,
`data_source_id`, `task_id` (set null), `first_matched_at`. `ai_jobs` — `kind`, `repository_id`
(cascade), `component_id` (cascade, nullable), `environment`, `status`, `session_id` (unique, no
foreign key), `result_summary`, `error`, `created` JSONB, `finished_at`. `alert_rules.created_by`
(`human` default).

## Task documents and analysis review (migration 164)

**Documents.** `domain.TaskDocument` carries `format: "markdown" | "html"` (column
`task_documents.format`, CHECK-constrained, existing rows `markdown`).

| Endpoint | Notes |
|---|---|
| `GET /v1/repositories/{id}/tasks/{taskId}/documents` | `{documents, count}`; single-task detail also carries `documents` |
| `POST .../documents` | `{title, content, format?, position?}` → `201` document. `format` defaults to `markdown` |
| `PATCH .../documents/{docId}` | `{title?, content?, format?, position?}`; omitted `format` keeps the document's own |
| `DELETE .../documents/{docId}` | `204`; its annotations cascade |

An `html` document is sanitized on every write, from the API and the agent tools alike
(`repository.prepareDocumentContent` → `application/htmldoc.Sanitize`): `script`, `iframe`,
`frame(set)`, `object`, `embed`, `applet`, `link`, `base`, `meta http-equiv`, `form`,
`input`/`button`/`textarea`/`select`, `noscript`, `template` and SVG `set`/`animate*` are
removed with their contents; every `on*` attribute, `srcdoc`, and any
`javascript:`/`vbscript:`/`data:text/html` URL in `href`/`src`/`xlink:href`/`action`/… is
stripped; an `img` survives only with an `https:` or `data:image/` source. `style` elements
and attributes and inline `svg` are kept, and the result is always a full document
(`<!DOCTYPE html><html><head>…</head><body>…</body></html>`). Over 1 MB
(`domain.MaxHTMLDocumentBytes`, measured before sanitizing) or an unknown `format` → `400`.

**Annotations.** A reviewer's comment anchored to a passage of a document by a text-quote
selector (the passage's rendered text plus a little context either side), stored in
`task_document_annotations` (FK to the document and the task, both `ON DELETE CASCADE`):

```json
{"id","task_id","document_id","quote","prefix","suffix","body",
 "status":"open"|"submitted"|"resolved","reply","created_by_type":"user"|"agent",
 "created_at","updated_at","submitted_at"?,"resolved_at"?}
```

`open` = a draft the human can still edit or delete; `submitted` = sent with a review and
waiting for the agent; `resolved` = answered by the agent (`reply`).

| Endpoint | Notes |
|---|---|
| `GET /v1/repositories/{id}/tasks/{taskId}/annotations[?document_id=]` | `{annotations: [...]}` ordered by `created_at`; `[]`, never `null` |
| `POST .../documents/{docId}/annotations` | `{quote, prefix, suffix, body}` → `201`, `status: open`, `created_by_type: user`. `quote` 1–2000 characters, `body` 1–4000 (trimmed), `prefix`/`suffix` ≤ 200 each (characters, not bytes) → `400` otherwise; a document not on the task → `404` |
| `PATCH .../annotations/{annId}` | `{body?, status?: "open"}`. `body` only while `open`; `status: "open"` reopens a `resolved` one (clears `reply`, `resolved_at`, `submitted_at`) and is a no-op on an `open` one. Anything else on a `submitted` one → `409`; any other `status` value → `400` |
| `DELETE .../annotations/{annId}` | `204` while `open`, `409` otherwise |
| `POST .../annotations/submit` | Optional `{note}` → `200 {submitted: n, task: <BoardTask>}`. `409` unless the task is in `analiz_review`; `400` with no `open` annotation |

Submit (`repository.Service.SubmitAnnotations`) marks every `open` annotation `submitted`
(one conditional UPDATE), adds ONE `user` comment — `Analysis review: n comments`, a
numbered `"quote" → comment` list (each clipped) and the note — and then moves the task to
`need_revision` through `UpdateTask` with `Actor: human`, exactly the path a drag onto the
column takes, so `ReviewGate.OnHumanRejection` and `evolution.NotifyRevision` fire. The
statuses and the comment are written before the move because the move dispatches the
revision run, which reads both; if the move is refused the annotations go back to `open`.

The revision run (`board.Runner.reviewAnnotationsMessage`) gets a system message
`## Review comments on your analysis document` listing every `submitted` annotation — id,
document, the quoted passage (one line, ≤ 600 bytes) and the full comment — capped at
20000 bytes; when capped it says how many were left out and to call
`list_document_annotations`. `derived_from` context (`Runner.analysisContext`) injects an
html document as its text rendition (`htmldoc.Text`) with a 1 MiB budget (`domain.MaxHTMLDocumentBytes`) instead of the
markdown 12000.

## Deploy metadata, ordering relations, packages

**Task fields** `before_deploy`, `after_deploy`, `rollback_plan` (nullable markdown) ride
on `domain.BoardTask` and are accepted by `POST /v1/repositories/{id}/tasks` and
`PATCH /v1/repositories/{id}/tasks/{taskId}` (the usual optional-pointer contract: omitted leaves the value, `""`
clears it). `before_deploy` is now a gate, not just a checklist comment (migration 161):
an `on_merge` component refuses the merge, and a `dispatch` component refuses `deploy_release`,
while any carried task's steps are unconfirmed; editing the field's text clears a prior
confirmation. `rollback_plan` is read back into `rollback_release`'s `manual_steps` at
rollback time (`domain.TaskRollbackRunbook`); `after_deploy` is posted as one system comment
per task once `finish_release` moves it to `released`. See [architecture.md](architecture.md)
→ "Releases (migrations 159–161)" → "Before/after-deploy runbook".

**Relations.** Four types, all reachable from the task API:

| Field | Direction | Write semantics | Enforcement |
|---|---|---|---|
| `deploy_depends_on: [{target_task_id \| target_key}]` | source = this task | PATCH **replaces** (`[]` clears, omitting changes nothing); POST via `relations` | Renders the generated ordering block in `before_deploy` (below) and is guarded against cycles at write time. Enforced by the release engine (`release/deploy_order.go`): an `on_merge` task is not merged, a `dispatch` release not deployed and a `batch` release not cut (`ErrDeployDependencyPending`) until every target is in `released`; finishing a release wakes the tasks that were waiting on it |
| `blocked_by: [{target_task_id \| target_key}]` | stored as `blocks` with the BLOCKER as `source_task_id` (migration 022) | **ADDS** — a blocker one planner learned must not be silently dropped by another | Work-order park; a move into `todo`/`in_progress` is refused |
| `derived_from` | source = implementation task, target = the analiz task | via `relations` or `create_board_task` | None — provenance, not order |
| `discovered_from` | source = the new task, target = the task the run was working on | written automatically by `create_board_task` inside a task run (migration 136); no tool argument | None — provenance, not order |

- A task may not depend on itself (400). A **cycle is refused where the edge is
  written**, not at release, naming the chain that closes it (`deploy-order cycle
  refused: T-2 already ships after T-1 (T-2 → T-1)`).
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
- Ships after: T-1 (task export endpoint).
- Built after: T-1 (task export endpoint). Work on this task does not start until those are done.
<!-- /tt:order -->

Confirm the export feature flag is off in prod.
```

Regenerated by `Service.syncOrderNote` wherever the order can move: after `POST .../tasks`
writes relations, and after `PATCH` replaces `deploy_depends_on` or adds `blocked_by`. Only
the fenced region is replaced, so an agent-written runbook survives. Generated rather than
agent-written because a typed-in ordering drifts the moment the relation is edited and
would then assert an order the release gate does not enforce. The checklist comment
carries the same text with the markers stripped.

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
| `POST /v1/repositories/{id}/alert-tokens` | `{label, default_env?}` (label ≤100 chars; `default_env` one of `local`/`stage`/`preprod`/`prod`, `prod` when omitted) → 201 `{id, label, default_env, created_at, token, ingest_path}`. The plaintext `token` is in this response only; the server keeps its sha256. 400 bad label/env, 404 no such repository, 503 when the server has no token store |
| `GET /v1/repositories/{id}/alert-tokens` | `{tokens: [{id, label, default_env, created_at, last_used_at}]}`, newest first — never the token. 503 as above |
| `DELETE /v1/repositories/{id}/alert-tokens/{tokenId}` | 204; 404 when the token is unknown OR belongs to another repository (the delete matches both ids); 503 as above |
| `POST /v1/alerts/ingest/{token}` | **No API key** — the token is the credential (`isPublicPath` lets exactly one segment under `/v1/alerts/ingest/` through). Same normalize + ingest as the incident webhook above, for the token's repository; environment is the payload's, else `?env=`, else the token's `default_env`. 413 when `Content-Length` (or, for a chunked body, the read length) is over 256 KiB, before the lookup; bare 404 for an unknown or malformed token; 400 non-JSON body; 201 `{incident_id}`; 202 for an unmatched recovery. **Limits per token** (`prod_ops.alert_ingest`, in memory): a request bucket keyed by the token hash (60/min, burst 10) and at most 20 NEW incidents per rolling hour (keyed by token id); over either, 429 `{"error":"rate_limited"}`. The hourly cap never refuses an alert that folds into an incident already open (its occurrences still count) or a recovery. `last_used_at` is stamped only when an alert is accepted. Logged paths go through `RedactPath`, so the token never reaches a log line. Migration 187 (`alert_ingest_tokens`) |

Notifications: `prodops` decides when (a new or critical-recurring incident at `high` or above, and its recovery, keyed by (repository, env, fingerprint) since a flapping alert re-fires as a new incident each cycle: only a repeat of the kind last told for the key is held back inside 30 minutes — a critical incident recurring — while an opening after a told recovery, or a recovery after a told opening, always goes out (`notify.Cooldown.AllowTransition`), so the last word told is never "recovered" while it is down again; a fix proposed by anyone but a human) and hands a `domain.Notification` (`kind`, title, body, severity, app link `/operations/issues?id=…`, source `incident`, source id) to `application/notify`, which fans it out to every registered and enabled `port.NotificationChannel` with a 5-second bound and logs, never returns, a channel's failure. `kind` is one of `incident_opened`, `incident_recovered`, `remedy_proposed`, `alert_firing`, `alert_resolved`, `rollback_suggested`, `release_rolled_back`. `application/release` notifies too, source `release`, link `/operations/changes?release=…`, at most once per 30 minutes per (kind, release) (`notify.Cooldown`, the same rule): `rollback_suggested` when the release engineer's `rollback_release` only writes a proposal (`auto_rollback` off), with `actions: [{id: "approve_rollback", label: "notifications.actions.approve_rollback"}]` — the label is a translation key — and `release_rolled_back` when a rollback nobody typed (an agent's, `auto_rollback` on) is under way; a human's own rollback is not told back. Local mode registers `desktop` (the macOS banner); `in_app`, `ios_push`, `slack`, `email` and `webhook` are reserved kinds with no adapter yet.

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
| `POST /v1/repositories/{id}/store/apps/{platform}/promote` | Body `{from, to, confirm, user_fraction?}` — one channel forward at a time (`domain.NextChannel`: internal→external→production); `to=production` requires `confirm`; Android into production starts a staged rollout at `user_fraction` ∈ (0, 1] (left out: 1, everyone; 400 `ErrInvalidRolloutFraction` outside it), ignored for iOS and the testing channels; 409 `ErrAppNotReady`. An iOS promote into production (App Review) sets `review_state` to waiting, as `ios/submit` does, so the store monitor follows the review |
| `POST /v1/repositories/{id}/store/apps/ios/submit` · `…/ios/release` | Body `{confirm}` (the repository's name, trimmed). Submit sends the newest App Store version to App Review and sets `review_state` to `waiting_for_review` (`MarkSubmitted`), so the monitor follows it and turns a rejection into an incident; release releases the version in `PENDING_DEVELOPER_RELEASE` — the production track's `approved` status. 409 before the app is live, 424 without the App Store Connect credential |
| `POST /v1/repositories/{id}/store/apps/android/rollout` | Body `{user_fraction, confirm?}`: production's staged share, (0, 1] — Play reads 0 as completed, so 0 is a 400 like anything outside the range; 1 completes the rollout and requires `confirm` (400 `ErrConfirmMismatch`); 409 before the app is live |
| `POST /v1/repositories/{id}/store/apps/{platform}/build` | Body `{engine?}` (`auto`\|`github_actions`\|`local`, default the repo's `release_engine`); 409 `domain.ErrNoReleaseEngine` when neither engine can run — the card is parked on `human_decision`, not retried; 409 `storeops.ErrBuildTargetUnknown` when the working copy named no Xcode scheme / Gradle module (migration 127) |

Linking (`.../link`) reads the console back: an unregistered row takes the
observed state (`live` when a version is on sale / production has a published
release, else `test_ready`, or `onboarding` for a Play app with no first upload),
and the monitor does the same for linked rows it finds unregistered. Go-live is
"a version is on sale", never "the newest version is" — a newer draft does not
hide the live one. `tracks.production` shows the version customers have and
queues a newer drafted/in-review/approved one in `pending_version` /
`pending_status`. Track statuses: `none`, `draft`, `in_review`, `approved`
(App Store's `PENDING_DEVELOPER_RELEASE` — cleared review, waiting for
`ios/release`; never from Play), `rolling_out`, `halted`, `live`, `unknown`.

### Store test builds

A task entering a stage that carries `store_test_build_on_enter` (Human UAT on
`task`/`bug`/`technical`, migration 177) is built for every linked app that is
`test_ready`/`live`, uploaded to TestFlight (iOS) or Play internal app sharing
(Android), and opened to the repository's automatic groups. Build numbers come
from one counter per app — the higher of this table's and the store's — iOS
`<sequence>.<taskNo>.<attempt>` (CFBundleVersion), Android `versionCode =
<sequence>`. The engine is this machine when it can build the platform, else
GitHub Actions (a pinned `release_engine` is honoured). A task coming back to
UAT with no new commit is not rebuilt. The release channels never pick a test
build: the iOS internal/external channel and its promote skip every build
number this server uploaded for testing, Play test builds cannot go to the
internal track, and the iOS `prod` script channel submits only the build named
by its `build_number` input.

| Endpoint | Notes |
|---|---|
| `GET /v1/repositories/{id}/store/test-builds?platform=&task_id=&limit=` | `[]domain.StoreTestBuild`, newest first |
| `POST /v1/repositories/{id}/store/test-builds` | Body `{task_id?, platforms?}` — no `task_id` builds the default branch, no `platforms` every linked app. 202 `{builds, error?}` (partial success keeps the started ones); 409 when an app cannot take builds (`ErrTestBuildNoStoreApp`, `ErrTestBuildAppNotTestable`) or no engine can run |
| `GET /v1/repositories/{id}/store/test-builds/{buildId}` | One build (status `queued`→`building`→`processing` (iOS)→`ready`, or `failed`, or `action_required` with `failure: "export_compliance"`) |
| `POST .../test-builds/{buildId}/open` | Body `{groups}` — TestFlight group ids (an external group also submits the build to Beta App Review) or Play track names (`internal` and `production` refused: the release flow promotes what they hold); Android re-uploads the kept AAB when Play does not hold the versionCode yet |
| `POST .../test-builds/{buildId}/close` | Body `{groups}` — iOS only (Play tracks are replaced, not emptied) |
| `POST .../test-builds/{buildId}/export-compliance` | Body `{uses_non_exempt_encryption}` — required, no default: it is the developer's legal answer; the build then opens to its automatic groups |
| `GET /v1/repositories/{id}/store/apps/{platform}/test-groups` | `[]domain.StoreTestGroup`: TestFlight groups or Play closed/open testing tracks (never internal or production), with `auto_distribute` |
| `POST /v1/repositories/{id}/store/apps/{platform}/test-groups` | Body `{name, internal}` — iOS only |
| `PUT /v1/repositories/{id}/store/apps/{platform}/test-groups/auto` | Body `{groups}` — which groups a ready build opens to. Unset: every TestFlight internal group; no Play track (the internal app sharing link only) |
| `GET/POST /v1/repositories/{id}/store/test-groups/{groupId}/testers`, `DELETE .../testers/{testerId}` | TestFlight testers of a group; POST `{email, first_name, last_name}` invites |

### Simulator runs

| Endpoint | Notes |
|---|---|
| `GET /v1/simulator/devices` | `[]domain.SimulatorDevice` of this machine: iOS simulators (UDID), Android AVDs (`avd:<name>`) and USB devices (`adb:<serial>`); 409 when there are none |
| `POST /v1/repositories/{id}/tasks/{taskId}/simulator-run` | Body `{platform, device_id}` — debug-builds a detached worktree of the task's checkout, installs and launches it; 202 `domain.SimulatorRun`; 409 when a run is already going on this machine |
| `GET /v1/repositories/{id}/tasks/{taskId}/simulator-run` | The task's latest run (in memory only); 404 when none |

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
- There is no more `PUT /v1/repositories/{id}/lifecycle-gates` endpoint (migration 158
  dropped `repositories.require_review_chain`, `require_release_deploy` and
  `require_pipeline_for_review`, and the route/handler with them). The gates it used to
  arm are now unconditional or gone:
  - The **pipeline gate** (code_review waits for the build/test pipeline before the
    reviewing architect is dispatched) is always on wherever a stage carries
    `wait_for_ci`; there is no per-repository off switch. The wait is still bounded
    (`board.pipeline_gate_timeout`), so it cannot deadlock a repository whose CI cannot
    answer.
  - The **review chain gate** is always enforced: moving into `done` (or `released`
    when that skips `done`) 400s unless the column-span history shows every review
    stage the type's workflow requires — `code_review`, `in_qa`, `pm_uat` for
    `task`/`bug`, `analiz_review` for `analiz` — with none of those stages' latest
    visit rejected; the message names the missing/rejected stage and the move that
    earns it (`done means the task passed its review chain, and this one has not …
    Missing: QA (in_qa) — move it to ready_for_qa`). It still fails closed (400, not
    200) if the span ledger cannot be read.
  - The **release-deploy gate** is gone entirely, replaced by the release state machine
    (migrations 159–161, see "Releases" above): a task reaches `released` only through
    a release's verdict (`POST /v1/releases/{id}/finish`, or the `finish_release` tool),
    never through a bare column move.

## GitHub webhook

- `POST /v1/github/webhook` — **public path, no bearer auth.** The per-repository HMAC
  over the raw body (`X-Hub-Signature-256`) is the entire authentication story:
  `verifiedWebhookRepo` resolves the delivery's repository by `repository.full_name` and
  verifies before anything that costs money (a pull, an embedding call, a GitHub
  round-trip). An unknown repository → `204`, so GitHub stops retrying; a bad or missing
  signature → `401`; otherwise `202 {accepted, reason}`, where `reason` is diagnostic and
  shows up in GitHub's delivery log.

  Four `X-GitHub-Event` values are handled; anything else (`ping`, `installation`, …) is
  acknowledged with `204` and ignored.

  - `push` to the default branch debounces a reindex + a project-model rescan (trigger `push`).
  - `workflow_run`, `check_suite`: a **completed** run resolves the task pipelines waiting
    on its `head_sha` (`HandleGitHubWorkflowEvent` → `PipelineRunner.ResolveByHeadSHA`) —
    the `task_pipelines` row is written and the board acts on it: success/skipped hands
    the card to its reviewer, failure moves it to `need_revision`. A run that is not yet
    completed, a payload with no head SHA, or a redelivered `X-GitHub-Delivery` is
    acknowledged and does nothing. Most deliveries legitimately match no waiting pipeline
    (`reason: "no pipeline is waiting on this commit"`), which is not an error.
  - `issues` (`opened`, `labeled`): an issue carrying the issue-sync label is imported when
    GitHub auto-import is on (`issuesync.Service.HandleGitHubIssuesEvent`). Pull requests
    delivered through the issues event are ignored. The poller imports the same issues
    without the webhook, which is the usual case on a local install.

- `POST /v1/repositories/{id}/webhook` — one-click install/rotate: mints a fresh secret
  and `PATCH`es or `POST`s the hook so GitHub agrees with both the stored secret and
  `githubapi.WebhookEvents` (`push`, `workflow_run`, `check_suite`, `issues`). Returns the updated
  `domain.Repository` (`webhook_installed`); 400 with the reason when the repository has
  no GitHub remote, GitHub is not connected, or `server.public_base_url` is unset.
  Existing hooks are also repaired at boot, without this call and without rotating any
  secret.

## This computer as a GitHub Actions runner

`internal/adapter/local/ghrunner`; wired only for the install the desktop app supervises (a
`DATA_DIR`) with Postgres connected. Config is `app_settings.github_runner`, plain JSON
`{enabled, repos}` — no credential.

- `GET /v1/settings/github/runner` → `domain.GitHubRunnerStatus` `{supported, enabled, labels,
  version, repos[], candidates[]}`. Without the manager it answers `supported: false`.
- `PUT /v1/settings/github/runner {"enabled", "repos": ["owner/name"]}` → the new status. At most
  `domain.GitHubRunnerMaxRepos` (5). A repository not already saved is checked through GitHub
  first (`GET /repos/{o}/{r}`): it must exist, be private, and the connection must have admin on
  it. A refusal is 400 with `type: "github_runner_<code>"` (`domain.GitHubRunnerErr*`); 503
  `github_runner_unsupported_platform` without a manager.

Per repository: one install under `$DATA_DIR/github-runner/repos/<hash>` (a just-in-time runner
writes `.runner`/`.credentials` into its root, so two cannot share one; ~450 MB each), unpacked from
the pinned `actions/runner` package in `github-runner/dist`, verified against the digest in GitHub's
release notes. A loop asks `POST /repos/{o}/{r}/actions/runners/generate-jitconfig` for a one-job
runner labelled `self-hosted, <OS>, <arch>, tasktrooper`, starts `run.sh` (`run.cmd` on Windows)
with the config in `ACTIONS_RUNNER_INPUT_JITCONFIG` (the runner deletes it from its own
environment; never argv) and the `childenv` allowlist environment, and registers again once a job
ran. A registration that ran no job is deleted (`DELETE …/actions/runners/{id}`); the job's
`_work` folder is removed after every run. A failure backs off from 15 s to 5 min. Turning a
repository off stops its runner and deletes its install; server shutdown stops every runner, a job
in progress included.

## Issue sync (migration 183)

GitHub and Jira issues imported as board tasks (`internal/application/issuesync`). Routes are
registered only when Postgres is connected.

- `GET /v1/issues/search?provider=github|jira` — GitHub needs `repository_id`, Jira `project`;
  optional `q`. `{issues: [domain.ExternalIssue]}`, each with `imported_task` when already linked.
- `POST /v1/issues/import {provider, key, repository_id}` — 201 `issuesync.Imported`
  `{task, link, import}`. Errors: 400 `invalid_issue` / `issue_source_not_configured`, 409
  `issue_already_imported` (`task_id`, `task_key`, `repository_id` beside `error`), 502
  `issue_source_error`.
- `POST /v1/issues/imports/{id}/convert` — re-queues the product manager's conversion of a
  `failed`, `skipped` or never-converted import whose imported task still exists. 202
  `{import}`; 409 `issue_conversion_unavailable` otherwise.
- `GET /v1/repositories/{id}/tasks/{taskId}/issue-link` — `{link, import}`, both `null` for a
  task with no issue.
- `GET|PUT|DELETE /v1/settings/jira`, `GET /v1/settings/jira/projects` — Jira Cloud only
  (`https://<name>.atlassian.net`); PUT verifies with `/myself` before storing; the API token is
  encrypted like `github_token` and never returned (400 `invalid_jira_site`, `jira_auth_failed`).
- `GET|PUT /v1/settings/issue-sync` — `domain.IssueSyncSettings`; defaults: label `tasktrooper`,
  both auto-imports off, `write_back` and `convert_with_pm` on.

**Tables.** `issue_imports` is one row per issue (unique `(provider, external_key)`): the
idempotency key, the intake task, the conversion's status, chat session and error, and
`closed_at`. `issue_links` is one row per task (unique `task_id`, cascades with the task);
an issue the product manager split has several.

**Conversion.** An import opens the issue as an intake task in backlog and, when
`convert_with_pm` is on and the session service is wired, queues the import (`pending`). One
worker (`StartConversions`) opens a chat with the agent holding `product_manager` for the
repository's area, project-bound and not task-bound, and sends
`catalog/system/prompts/issuesync/convert_request.md` through `session.Service.SendMessage`, so
an agent CLI provider runs it on this machine. Tasks `create_board_task` opens in that chat
reach `repository.Service`'s `TaskCreatedObserver`, which links them to the issue by the session
on the tool call's context — also for turns after the person answers a question in the chat.
Once at least one task is linked the intake task is deleted and the issue gets one "Tracked in
TaskTrooper as …" comment. No product manager → `skipped`; a failed turn or no task opened →
`failed`; a question or a parked quota → `needs_input`. In each of those the intake task stays.
A restart puts `converting` back to `pending`; a conversion that had already opened tasks is
finished without running the agent again.

**Write-back** (when `write_back` is on) goes through the board dispatcher's `TaskNotifier`: a
comment per column change of any linked task, and once every linked task is done or released,
the GitHub issue is closed (`state_reason=completed`) or the Jira issue transitioned to a done
status, once (`closed_at`).

## Project model

Components, checks, links, resources and scans — the full route list and
payloads are in [projects.md](projects.md) and the domain JSON tags
(`internal/domain/project_model*.go`, `project_map.go`). The brief is not a route: it is
fetched on demand through the `get_project_brief` tool.

- `GET /v1/projects/overview`, `GET /v1/projects/{id}/overview`, `GET /v1/projects/map`,
  `GET /v1/projects/{id}/map`
- `GET /v1/repositories/{id}/model`
- `POST /v1/repositories/{id}/scans` (202; 409 while one runs),
  `GET /v1/repositories/{id}/scans/latest`, `GET /v1/scans/{id}`
- `POST /v1/repositories/{id}/components`, `PATCH /v1/components/{id}`,
  `POST /v1/components/{id}/checks`, `PATCH|DELETE /v1/checks/{id}`,
  `POST /v1/links`, `PATCH|DELETE /v1/links/{id}`
- `GET /v1/resources` — `{"resources": [domain.WorkspaceResource, ...]}`, never `null`
- `PATCH /v1/resources/{id}` (`{"name": "…"}`) — 200 `domain.SystemResource`, locks the name
  against a later rescan
- `POST /v1/resources/{id}/merge` (`{"into_resource_id": "uuid"}`) — 200 the surviving
  (target) `domain.SystemResource`; the resource in the URL is deleted and its identity key
  becomes an alias of the target, so a rescan that still emits it resolves to the target
  instead of recreating it
- `POST /v1/resources/{id}/split` (`{"link_ids": ["uuid", ...]}`) — 201 the freshly minted
  `domain.SystemResource` the given links now point at

PATCH bodies distinguish an absent field (leave), `null` (revert to detected) and a
value (set the override). Errors are flat `{"error": "…"}`.

### Code-health findings (migration 185)

`GET /v1/repositories/{id}/model` carries `findings`: every `domain.ComponentFinding`
of the repository, any status, never `null`. Details in
[projects.md](projects.md#code-health-findings-migration-185).

| Route | Body / response |
|---|---|
| `PATCH /v1/findings/{id}` | `{status?, reason?}` — `status` is `open`, `accepted` or `false_positive`; `reason` (≤500 chars) is kept only on the two closed statuses, and reopening clears it. 400 on `resolved` (only a scan resolves or reopens) and on a finding that is already resolved. 200 the finding |
| `POST /v1/repositories/{id}/findings/task` | `{finding_ids, title, task_type?, priority?, notes?}` — one board task for up to 50 **open** findings of the repository. `task_type` defaults to `technical`, `priority` to the worst finding's severity (`high`/`medium`/`low`); the task is tied to the findings' component when they share one. The description is `catalog/system/prompts/findings/task.md` with each rule's `partials/findings/<rule>.md`; `notes` goes above it. 201 `{task, findings}` with every finding now carrying `task_id` and `task` |

## Design systems (migration 178)

One base design system per project (`scope: "project"`) and an optional layer per
repository (`scope: "repository"`) that adds or overrides tokens on top of it. Every
version is proposed by a `design` task through `propose_design_system`; moving that
task to `done` (the human's approval in `analiz_review`) approves the versions it
proposed and supersedes the ones they replace (`approve_design_system_on_enter`).
Nothing here approves directly.

| Route | Body / response |
|---|---|
| `GET /v1/projects/{projectId}/design-system` | `designsystem.ProjectView`: `current` (approved base), `pending`, `versions` (newest first), `repositories` (each with its approved `layer`, `pending_layer`, `builds_on_this_project`, `ambiguous`) and `request` (the last design task opened from here) |
| `POST /v1/projects/{projectId}/design-system/generate` | Body `{notes?}`. Opens a design task, in one of the project's UI repositories, that derives (or updates) the base and the repository layers from the code the repositories already have. 201 `{task, created: true}`; 200 `{task, created: false}` with the task already open — handed to the designer now if it was opened before one existed; `waiting_for_designer: true` while no agent holds the designer role (the task waits unassigned; no column watcher picks it up); 409 when the project has no repository |
| `GET /v1/repositories/{id}/design-system` | `designsystem.RepositoryView`: `effective` (`base`, `layer`, merged DTCG `tokens`, `ambiguous`), `base_project_id`, `project_choices`, `pending_layers`, `layer_versions`, `overrides` (token paths the layer changes), `request` |
| `POST /v1/repositories/{id}/design-system/generate` | Body `{notes?}`. Opens a design task for the repository's layer — or its whole design system when it has no project base. Same responses as the project route |
| `PUT /v1/repositories/{id}/design-system/base-project` | Body `{project_id}` (or `null` for automatic). A repository in several projects that each have a base is `ambiguous` until one is chosen; the project must be linked to the repository (400 otherwise) |
| `GET /v1/design-systems/{designSystemId}` | One `domain.DesignSystem` version |
| `GET /v1/repositories/{id}/design-system/files` | `{files: [{path, content}]}` — `DESIGN.md`, `design/tokens.json`, `design/tokens.css`, `design/INVENTORY.md` rendered from the effective design system (empty when it has none) |
| `GET /v1/repositories/{id}/tasks/{taskId}/design` | `{references, design_system?}` — the approved design tasks the task derives from or is blocked by (each a `domain.AnalysisReference` with `task_type: "design"` and its documents) and the version summary of the repository's design system |

Every version, and the repository view's merged tokens, carries `lint`
(`domain.DesignLintFinding`: `code` `unresolved_alias | invalid_color |
contrast_below_aa | empty_group | missing_section`, `severity`, `path`,
`related_path`, `value`, `ratio`), computed on read. A `design` task blocks the
work it `blocks` until it is `released`, not `done`
(`postgres.unfinishedBlockerSQL`).

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

- `POST /v1/embedding-map/locate` — where search hits sit on the map. A hit may not be in
  the map's sample, so each hit chunk is mapped to the most similar sampled ("anchor")
  chunk, whose position the UI draws it at.

  ```json
  {"repository_id": "uuid", "anchor_ids": ["chunk-uuid"], "chunk_ids": ["chunk-uuid"]}
  ```

  ```json
  {"locations": [{"chunk_id": "uuid", "anchor_id": "uuid", "similarity": 0.93}]}
  ```

  One entry per requested chunk that exists in the repository's current index, in request
  order; unknown ids and chunks with an unusable embedding are omitted. A chunk that is
  itself an anchor maps to itself with similarity `1`. Similarity is cosine over
  L2-normalized embeddings; ties go to the first anchor in request order. `400` for a
  missing or malformed `repository_id`, an empty `anchor_ids` or `chunk_ids`, more than
  5000 anchors or more than 50 chunk ids; malformed uuids inside the arrays are skipped.
  A repository without an index is `200` with `{"locations": []}`.

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
