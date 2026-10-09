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
| `GET /v1/releases` | Every repository's releases newest first (Operations → Deployments). Filters `repository_id`, `component_id`, `status` (comma list), `before` (RFC3339 `created_at` cursor), `limit` ≤ 100 |
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

## Releases (migrations 159–161)

A component's delivery profile decides whether a merge deploys on its own (`on_merge`),
waits to be dispatched (`dispatch`), collects into a release a human cuts (`batch`), or
ships nothing (`none`) — see [projects.md](projects.md) → "Component delivery". Every
route below sits behind the same bearer auth as everything else; every WRITE additionally
requires `confirm` to equal the repository's name, the same guardrail
`handler_deployops.go`'s dispatch/rollback routes use (`errReleaseConfirmMismatch` → 400).

| Endpoint | Notes |
|---|---|
| `GET /v1/repositories/{id}/releases?component_id=&task_id=&limit=` | `{"releases": domain.Release[]}`, newest first; default limit 20, max 100 |
| `GET /v1/releases/{releaseId}` | One release in full: status, mode/executor, commit/tag, `deploy` (incl. a batch release's `local_run`/`store_builds`), `checks` (health/smoke/new error groups), `verdict`, `rollback`, `tasks` |
| `GET /v1/releases/{releaseId}/cut-preview` | `domain.ReleaseCutPreview` for a `draft` release, or a `pending` one `deploy_release` was never attempted on (a re-cut, e.g. after `ErrReleaseTagExists`): suggested/previous version (the HIGHEST semver among the component's last released version, its newest matching git tag, and any tag any of its releases ever carried), the tag it would carry, the commit it would cut at, generated notes, its tasks. 409 `ErrReleaseWrongStatus` off draft/re-cuttable-pending, 409 `ErrReleaseEmpty` with no tasks |
| `POST /v1/releases/{releaseId}/cut` | `{confirm, version, notes}` → `Release`, `draft → pending` (or re-cutting a `pending` release that never deployed); re-reads and freezes the component's CURRENT confirmed delivery profile, stamps every carried task's before-deploy confirmation, wakes the release engineer. 400 invalid version (`domain.ValidReleaseVersion` — also enforces git ref rules: no `..`, no trailing `.`, no `.lock` suffix, no `@{`, no `//`); 409 `ErrDeployDependencyPending` while any carried task ships after an unreleased dependency |
| `POST /v1/releases/{releaseId}/deploy` | `{confirm}` → `Release`. `pending` only; a `dispatch` release dispatches its workflow, a cut `batch` release runs its executor (tag+watch / local command / store build). Claims the release before the side effect; a side effect that started nothing at all (a transient dispatch/start error) returns it to `pending` with the error rather than `failed`, so the call may simply be retried — only a definitive refusal (CI unavailable, or the workflow/tag trigger itself invalid) becomes `failed` |
| `POST /v1/releases/{releaseId}/finish` | `{confirm, note}` → `Release`, verdict recorded, every task moved to `released`. Same states `finish_release` accepts (`awaiting_verdict`, or `failed` — a human overriding it) |
| `POST /v1/releases/{releaseId}/rollback` | `{confirm, note}` → `Release`, always `domain.RollbackManual` (an agent's own reasons — `deploy_failed`/`verify_failed`/`health_incident` — only come from the `rollback_release` tool). Claims the release first; refused when the release is not `failed`/`awaiting_verdict`/`released`-within-24h-and-still-newest, or when a newer release of the component is already open or has since shipped |
| `POST /v1/repositories/{id}/tasks/{taskId}/before-deploy/confirm` | No body. Stamps `board_tasks.before_deploy_confirmed_at` (`COALESCE`, idempotent) and, when the task sits in `done`, wakes the release engineer on it immediately (`release.Service.WakeTask`) instead of waiting for the next sweep |
| `PATCH /v1/components/{componentId}` | Existing `ComponentPatch` plus `"delivery": domain.ComponentDelivery \| null` — `null` clears the human override back to the detected profile. A save that newly CONFIRMS the profile (crossing into an override, or a detected exact/high) opens releases for every task of that component already sitting in `done`, merged, with no release yet (`OpenPending`), and wakes any of the component's `done` tasks that never merged at all |
| `POST /v1/components/{componentId}/smoke-checks/test` | `{"checks": domain.SmokeCheck[]}` (1..20, same validation `ComponentDelivery.Validate` runs) → 200 `{"base_url": string, "results": domain.SmokeResult[]}`. Runs a DRAFT set of checks right now against the component's resolved production base URL (confirmed environment, else the legacy prod deploy target, else `""` — same resolution a release's verify window uses) with the same runner/policy/timeout `run_smoke_checks` uses. Nothing is persisted — the UI's "test before you save" action while editing a delivery profile. 400 `ErrInvalidDelivery` (including an empty list); 404 unknown component |
| `POST /v1/components/{componentId}/smoke-checks/generate` | `{"existing": domain.SmokeCheck[]}` → 202 `Job`. Starts an AI draft of smoke checks: the `release-engineer` agent reads the component's code read-only (read_file/grep_code/glob/codebase_search, in the repository checkout) and proposes GET/HEAD checks; the server validates each, drops duplicates of `existing`, caps at 8, then test-runs the survivors like `/smoke-checks/test`. Returns the already-running job for the component instead of starting a second. 404 unknown component; 409 no enabled release-engineer agent with a runtime |
| `GET /v1/smoke-check-generations/{jobId}` | → 200 `Job` `{job_id, component_id, status: running\|done\|failed\|cancelled, agent_name, started_at, finished_at?, checks: SmokeCheck[], results: SmokeResult[], base_url, dropped, error}` — `results[i]` belongs to `checks[i]`, arrays never null; a `done` job can have zero checks with `error` saying why. Jobs live in memory, 5-minute run timeout, evicted 30 minutes after finishing (then 404). Nothing is saved to the delivery profile |
| `DELETE /v1/smoke-check-generations/{jobId}` | → 204. Cancels a running generation; idempotent |

`domain.SmokeCheck`: `method` (`GET`\|`HEAD`), `path` (relative to the env URL, or an
absolute http(s) URL), `expect_status` (0 = any 2xx/3xx), `contains` (substring the body
must hold), `name` (optional label, ≤80 chars), `max_latency_ms` (0 = no limit, else
1..10000 — a response slower than this fails the check with `error: "slower than <N> ms
(<latency> ms)"`, checked after status/contains so the first real failure reason wins).

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
  `errors_supported` is false when the provider has no error surface for the environment
  (Vercel, any `per_branch` row) — `errors` is then `[]` and not a verdict.
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
html document as its text rendition (`htmldoc.Text`) with a 24000-byte budget instead of the
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

Linking (`.../link`) reads the console back: an unregistered row takes the
observed state (`live` when a version is on sale / production has a published
release, else `test_ready`, or `onboarding` for a Play app with no first upload),
and the monitor does the same for linked rows it finds unregistered. Go-live is
"a version is on sale", never "the newest version is" — a newer draft does not
hide the live one. `tracks.production` shows the version customers have and
queues a newer drafted/in-review/approved one in `pending_version` /
`pending_status`.

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
