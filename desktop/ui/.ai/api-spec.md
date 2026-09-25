> `../server` is the source of truth for these endpoints; this copy exists so
> UI work does not need to read Go.
>
> **Reading the paths from this app:** every `/v1/...`, `/admin/...` and
> `/health` path below is called through `apiUrl()` (`src/lib/apiBase.ts`),
> which prefixes `VITE_API_BASE` — the desktop shell states it at runtime
> (`http://127.0.0.1:<port>`), and a browser dev build proxies it.

# API Specification

Every endpoint requires `Authorization: Bearer <SERVER_API_KEY>`.

`X-Request-ID` is accepted on requests; if omitted, the bridge generates one and returns it in the response header.

## POST /v1/chat/completions

Runs the agent loop with optional tool policy, session continuity, and RAG context.

**Request**

```json
{
  "model": "local",
  "messages": [{"role": "user", "content": "List files in /tmp"}],
  "stream": false,
  "session_id": "optional-uuid",
  "tool_policy": {
    "allow_tools": ["web_search"],
    "deny_tools": ["run_terminal"]
  },
  "file_ids": ["uuid-of-uploaded-file"]
}
```

**Response** — OpenAI-compatible `chat.completion` object.

Streaming (`stream: true`) returns SSE chunks for the final assistant message.

## GET /v1/models

Proxies LM Studio model list.

## GET /v1/tools

Returns tool definitions filtered by merged policy (config default + API key policy + request policy).

## GET /v1/tools/health

```json
{
  "servers": [
    {"id": "browser", "connected": true, "tool_count": 12},
    {"id": "github", "connected": false, "last_error": "connect: ..."}
  ]
}
```

## POST /v1/sessions

Creates a session. Requires PostgreSQL.

```json
{"title": "My chat", "model": "local"}
```

## GET /v1/sessions/{id}

Returns `session` and `messages` arrays.

## POST /v1/sessions/{id}/messages

```json
{
  "role": "user",
  "content": "Hello",
  "model": "local",
  "tool_policy": {},
  "file_ids": []
}
```

## POST /v1/sessions/{id}/cancel

Stops the agent turn the session has in flight. Optional body `{"reason": "..."}`.

```json
{"cancelled": true}
```

`cancelled: false` means there was nothing left to stop (the answer landed
before the request did) — a normal outcome, not an error. 404 for an unknown
session, 400 for a malformed id.

The stopped run's row ends as `cancelled` (and a later terminal write cannot
overwrite it), whatever the agent had already streamed is persisted as the
assistant message, and the SSE stream of the turn being stopped ends with a
normal `finish_reason: "stop"` frame plus `[DONE]` instead of an error frame.

## DELETE /v1/sessions/{id}

Deletes session and messages. Returns 204.

## POST /v1/jobs

Enqueues an async agent run. Requires PostgreSQL.

```json
{
  "model": "local",
  "messages": [{"role": "user", "content": "Long task"}],
  "tool_policy": {},
  "callback_url": "https://example.com/webhook",
  "file_ids": []
}
```

Returns `202` with job object (`status: pending`).

## GET /v1/jobs/{id}

Returns job status: `pending`, `running`, `completed`, `failed`, `cancelled`.

## GET /v1/jobs/{id}/result

Returns job result JSON when `status` is `completed`.

## DELETE /v1/jobs/{id}

Cancels pending/running jobs or deletes completed jobs.

## GET /v1/audit?limit=50

Returns recent tool call audit entries. Requires PostgreSQL.

## POST /v1/files

Multipart upload with `file` field. Chunks and embeds via LM Studio `/v1/embeddings`.

## GET /v1/files

Lists uploaded files.

## DELETE /v1/files/{id}

Deletes file, chunks, and disk storage.

## POST /v1/attachments

Multipart upload with `file` field (+ optional `repository_id` form value).
Binary attachments (images/documents) stored as BYTEA in Postgres — a parallel
concept to `/v1/files`, which feeds the RAG text pipeline and writes to local
disk under `DATA_DIR/files`. Max 10 MB (413 over the cap); content-type allowlist
(png/jpeg/webp/gif, pdf, plain/markdown/csv, json, zip, docx, xlsx) validated
after server-side sniffing (415 otherwise). Returns attachment metadata
(`{id, filename, content_type, size_bytes, sha256, ...}`, never the bytes).

## GET /v1/attachments/{id}

Raw bytes with the stored `Content-Type`, `Content-Disposition: inline` and
`Cache-Control: private, max-age=31536000, immutable` (rows are immutable).
Requires the normal Authorization header — the web UI fetches blobs and uses
object URLs instead of pointing `<img src>` at this endpoint.

## DELETE /v1/attachments/{id}

Deletes the attachment; task/message links cascade away.

## GET /v1/repositories/{id}/tasks/{taskId}/attachments

`{attachments: [AttachmentMeta], count}` for the task. `POST` to the same path
with `{"attachment_id": "..."}` links an already-uploaded attachment (task
existence is verified first); `DELETE .../attachments/{attachmentId}` unlinks
without deleting the attachment row. Single-task detail responses also carry
`attachments` alongside `documents`.

Chat: `POST /v1/sessions/{id}/messages` accepts `attachment_ids` (linked to
the persisted user message) and `GET /v1/sessions/{id}` returns each message's
`attachments`. Attachments are not injected into the LLM context.

## GET /metrics

Prometheus metrics: `bridge_requests_total`, `bridge_tool_calls_total`, `bridge_agent_iterations`, `bridge_llm_latency_seconds`, `bridge_mcp_errors_total`.

## GET /docs

OpenAPI 3.0 YAML spec.

## POST /admin/reload

Reloads config file and reconnects MCP servers. Does not restart the HTTP server.

## GET /health

No auth. Returns bridge and LM Studio status.

## GET /v1/usage?days=30

LLM token usage aggregates for the cost dashboard: totals, by-model, and
daily breakdowns (`llm_usage` table, recorded by the `application/usage`
LLM-client decorator). `days` 1-365, default 30.

## Embedding provider & model

Settings has no embedding provider/model picker: embedding runs on a bundled,
self-downloading engine with no user-facing settings and no provider/model
choice. `LLMProvidersResponse` carries no `embedding_provider` /
`embedding_model` / `embedding_on_member_mac`, and this SPA calls none of
`PUT /v1/llm/embedding-provider`, `GET /v1/llm/embedding-models`,
`GET /v1/llm/embedding-status`, `POST /v1/llm/reindex-all` — those routes
still exist server-side (admin API) but nothing here should call them.

## Board run control

The task drawer's stop / re-run buttons. Both return the run under a `run` key
(`{"run": {...domain.TaskAgentRun...}}`) and answer `503 service_unavailable`
when the build has no board runner wired.

- `POST /v1/repositories/{id}/tasks/{taskId}/runs/{runId}/cancel` — stops a
  `pending`/`running` run and parks the task as **blocked** with the reason;
  recovery is a human dragging the task onto a column, which releases the block
  and lets that column's agent take it through normal dispatch. Optional body
  `{"reason": "..."}` (absent/empty is fine). Returns the **cancelled** run.
  `409 conflict` when the run already stopped.
- `POST /v1/repositories/{id}/tasks/{taskId}/runs/{runId}/rerun` — queues a new
  run of the **same agent** on the task's **current** state. The task does not
  move columns and the run named in the URL stays untouched history. Returns the
  **new** (`pending`) run. `409 conflict` when the run is still going, or when
  the task is blocked — a blocked task is recovered by moving it, not by
  re-running in place.

Both answer `404 not_found` when the run does not exist or belongs to a
different task than the one in the URL. Neither goes through the board
dispatcher: cancel would immediately fan fresh agents onto the task it just
stopped, and re-run would start every agent configured for the column rather
than the one asked for. Each records its own board event
(`task.run_cancelled` / `task.rerun_requested`).

## POST /v1/repositories/{id}/tasks/{taskId}/chat

Opens — or reopens — the chat thread a human talks about ONE board task in, and
returns the session to send messages to plus the agent answering there:

```json
{"session_id": "0f9b…", "agent_id": "3c4d…"}
```

Idempotent: a task has exactly one thread for its whole life, so pressing the
drawer's "discuss" button again returns the same `session_id` rather than
splitting the conversation across chats. If the task already has a clarification
thread (the chat an agent asked its questions in), that thread becomes the task
thread instead of a second one being opened. `agent_id` is `""` when no agent is
assigned to the task and nobody is subscribed to its column — the chat still
works, it just has no agent persona.

The thread differs from an ordinary repository chat in two ways:

- Its workspace is the task's own branch checkout
  (`<workspace_root>/task-<taskId>` on `feature/<task-key>`), not the
  shared mirror clone — so what the agent changes can be committed and pushed to
  the task branch and reach the pull request. A branch checkout that cannot be
  prepared fails the turn instead of silently falling back to the mirror.
- Every turn carries a compact task + PR context message (key, title, column,
  description, technical notes, acceptance criteria, branch, PR number/url) and
  the three PR tools (`get_task_pull_request`, `commit_task_changes`,
  `comment_on_pull_request`). The diff is deliberately NOT in the prompt; it is
  fetched per request through the tool.

`404 not_found` for an unknown repository or a task the repository does not own
(the repository scope is the ownership check), `400` for a malformed id, `503
service_unavailable` on a build with no board.

The pull request itself is now a task field — `pr_url` / `pr_number` on
`BoardTask` (migration 088) — written the moment a PR is opened
(`createDraftPRAsync`, the pipeline's pre-gate PR ensure, the code-review PR
ensure, and `commit_task_changes`). Before this it survived only as the text of a
`"Draft PR: <url>"` system comment.

## Repository registration

- `POST /v1/repositories/open`, `POST /v1/repositories/import` and
  `POST /v1/repositories` all accept an optional `kind`
  (`backend|frontend|mobile|worker|monorepo`); an invalid value is a 400. Left
  out, the kind is detected from the working copy (Flutter/Xcode markers →
  mobile, several projects under `apps/`/`packages/` → monorepo, a
  react/vue/svelte/vite/next `package.json` → frontend, otherwise backend) and
  persisted with the row, instead of every repo landing on the column default.
  When detection lands on `monorepo`, `sub_projects: [{path, kind}]` is
  detected and persisted the same way — one row per sub-directory that looked
  like a project of its own, each independently classified. Distinct from
  `sub_repo_kinds` (a deduplicated set of kind values used only for GitHub
  Actions pipeline job routing): `sub_projects` is the addressable,
  human-curated list shown in the initial-setup dialog.
- `PATCH /v1/repositories/{id}` also accepts `release_engine`
  (`auto|github_actions|local`) — which machine builds/uploads a mobile store
  release. `auto` (default) is GitHub Actions, falling back to this machine;
  see Store operations below for `domain.ErrNoReleaseEngine`.
- `PATCH /v1/repositories/{id}` also accepts `kind` and now actually applies it
  (the field existed on the request but was never forwarded to the store).
  Note that `name` and `description` are applied unconditionally on this route:
  an omitted description clears the stored one. It also accepts `sub_projects`
  (replace-the-whole-list; applied only when the field is present, so omitting
  it leaves the stored list untouched) — send `[]` to clear it, e.g. when the
  kind is changed away from `monorepo` in the same request.
- `GET /v1/repositories/{id}/directories?path=` — lists the immediate
  subdirectories under `path` (repo-relative, root when omitted) inside the
  repository's working copy, skipping the same noise directories detection
  itself skips (`.git`, `node_modules`, `vendor`, `dist`, `build`, `.venv`,
  `target`, any dotdir). Response: `{path, parent, kind, entries: [{name, path}]}`
  — `parent` is `null` at the root, `kind` is `path`'s own detected kind
  (backend/frontend/mobile). Backs the folder picker for manually
  adding a `sub_projects` row when auto-detection found nothing or missed
  one; `path` is invalid (escapes the root, doesn't exist) → 400.
- `POST /v1/repositories/{id}/restore` — fetch a registered repository's code
  onto this machine, from the `remote_url` already on its record. 202 with
  the repository; the clone runs server-side and is
  watched through `git_restore` (`{status: running|completed|failed, root_path,
  error}`) by polling `GET /v1/repositories/{id}`. Offer it only when the
  repository read says `git_restorable` — the server refuses (400, message is
  the reason) whenever the folder exists, holds something that is not a
  repository, cannot be read, or no remote is recorded.

## Deploy targets

Four environments: `local`, `stage`, `preprod`, `prod` (`local` has no
pipeline category — nothing in CI ships to a developer's own machine, it is
just a recorded address + health check like any other).

- `GET /v1/deploy/templates?kind=backend` — recipe catalog (bodies stripped).
- `GET /v1/deploy/templates/{templateId}` — one recipe in full.
- `GET /v1/repositories/{id}/deploy/config?sub_project_path=` — saved targets + fitting templates + missing vars per env, scoped to one monorepo sub-project (`""`/omitted = the repository itself).
- `PUT /v1/repositories/{id}/deploy/targets` — upsert one (sub_project_path, env): `{sub_project_path, env, provider, template_id, vars, health_url, auto_rollback}`.
- `DELETE /v1/repositories/{id}/deploy/targets/{env}?sub_project_path=`.
- `GET /v1/repositories/{id}/deploy/targets/{env}/instructions` — recipe rendered with the target's vars (repository-scoped only, no sub_project_path).
- `POST /v1/repositories/{id}/deploy/targets/{env}/setup-task` — open the board task that authors the deploy workflow (repository-scoped only).

## Releases (migrations 159–161)

A component's delivery profile decides whether a merge deploys on its own (`on_merge`),
waits to be dispatched (`dispatch`), collects into a release a human cuts (`batch`), or
ships nothing (`none`) — the `DeliveryCard`/`DeliveryEditDialog` pair on the Deploy &
Runtime tab edit it. Every WRITE below additionally requires `confirm` to equal the
repository's name (same guardrail as the deploy-target rollback endpoints).

- `GET /v1/repositories/{id}/releases?component_id=&task_id=&limit=` — `{releases: Release[]}`,
  newest first; default limit 20, max 100 (`api.listReleases`).
- `GET /v1/releases/{releaseId}` — one release in full: status, mode/executor, commit/tag,
  `deploy` (incl. a batch release's `local_run`/`store_builds`), `checks` (health/smoke/new
  error groups), `verdict`, `rollback`, `tasks` (`api.getRelease`).
- `GET /v1/releases/{releaseId}/cut-preview` — `ReleaseCutPreview` for a `draft` batch
  release: suggested/previous version, the tag it would carry, the commit, generated notes,
  its tasks. 409 off-draft or with no tasks.
- `POST /v1/releases/{releaseId}/cut` — `{confirm, version, notes}` → `Release`,
  `draft → pending`; freezes the component's current confirmed delivery profile, stamps
  every carried task's before-deploy confirmation, wakes the release engineer. Driven by
  `CutReleaseDialog`.
- `POST /v1/releases/{releaseId}/deploy` — `{confirm}` → `Release`. `pending` only.
- `POST /v1/releases/{releaseId}/finish` — `{confirm, note}` → `Release`, every task moved
  to `released`.
- `POST /v1/releases/{releaseId}/rollback` — `{confirm, note}` → `Release`, always a manual
  (human) reason.
- `POST /v1/repositories/{id}/tasks/{taskId}/before-deploy/confirm` — no body. Stamps
  `before_deploy_confirmed_at` and, when the task sits in `done`, wakes the release engineer
  immediately. Driven by `TaskDetailDrawer`'s before-deploy Confirm button
  (`api.confirmBeforeDeploy`).
- `PATCH /v1/components/{componentId}` — existing body plus
  `"delivery": ComponentDelivery | null` (`api.updateComponentDelivery`); `null` clears the
  human override back to the detected profile. A save that newly CONFIRMS the profile opens
  releases for every task of that component already sitting in `done`, merged, with none yet.

**Removed.** `GET/POST /v1/repositories/{id}/deploy-packages` and its
`PATCH`/`DELETE`/`PUT .../tasks`/`POST .../release` siblings are gone — batch releases
(above) replace the deploy-package "release train". `trigger_release` (the old per-task
release dispatch) is gone too; a release now opens automatically at merge time, or joins a
draft.

## Pipeline setup task

`GET`/`PUT /v1/repositories/{id}/pipeline/config` are gone (Project Model
Phase 1 — see "Project model" below); only the setup task survives:

- `POST /v1/repositories/{id}/pipeline/setup-task` — open the board task that
  writes this repository's CI workflows (`api.createWorkflowSetupTask`).

## GitHub connection

Settings → Integrations (`admin/GitHubCard`). `GET /v1/settings/github` → `{connected, login?, detail?}`;
`PUT /v1/settings/github {"token"}` verifies the personal access token against GitHub before storing
it encrypted (scopes: `repo`, `admin:repo_hook`, `read:org`); `DELETE` removes it. There is no OAuth
hop — nothing here can hold an OAuth app's client secret or receive GitHub's callback.

## Cloud accounts & environments (Phase 2)

Replaced the old one-account-each `/v1/settings/vercel*` and `/v1/gcloud/*` (plus
the hosting-link endpoints, `/v1/repositories/{id}/hosting/*`): a repository's
components now bind to a provider-neutral environment, read through one or more
connected accounts. JSON shapes mirror `server/internal/domain/cloud.go`'s tags
exactly. Errors: `{error, code}`; a rejected credential is 400 with
`code:"cloud_auth"` (`api.isCloudAuthError`), 404 unknown id, 502 provider
unreachable.

Accounts (Settings → Integrations, `admin/CloudAccountsCard` + `CloudAccountDialog`):

- `GET /v1/cloud-accounts` → `{accounts: CloudAccount[]}` (never carries secrets).
- `POST /v1/cloud-accounts` `SaveCloudAccountRequest {provider: "vercel"|"gcp"|"aws", label?, fields}`
  → 201 `CloudAccount`. Verified with the provider before storing.
  `fields`: vercel `{token, team_id?}` · gcp `{service_account_json}` · aws
  `{access_key_id, secret_access_key, session_token?, region}`.
- `PATCH /v1/cloud-accounts/{id}` `{label?, fields?}` → `CloudAccount` (re-verified
  when `fields` is given — this is how a credential is replaced).
- `POST /v1/cloud-accounts/{id}/verify` → `CloudAccount` (`status` ok|error, `status_detail`).
- `DELETE /v1/cloud-accounts/{id}` → 204 (environments bound through it keep their
  rows but lose live data).
- `GET /v1/cloud-accounts/{id}/resources?refresh=1` → `{resources: CloudResource[]}` (cached 60s).

Environments (a repository's Deploy & Runtime tab, one per `ComponentEnvironment`;
listed in `GET /v1/repositories/{id}/model` → `environments`, suggestions surface
in `review` with `kind:"environment"`, rendered by `projects/model/ReviewList`):

- `PUT /v1/components/{componentId}/environments/{env}`
  `SaveEnvironmentRequest {account_id?, resource?, url?, health_url?}` →
  `ComponentEnvironment` (source user, confirmed). `{env}` ∈
  production|staging|preview|development. With `account_id` a `resource` is
  required (picked from that account's resources); without one it is a custom
  environment and `url` is required.
- `PATCH /v1/environments/{envId}` `{status?: "confirmed"|"dismissed"|"suggested", account_id?, resource?}`
  → `ComponentEnvironment` — choosing one of the row's `candidates` means sending
  its `account_id` + `resource.ref` with `status:"confirmed"` (`api.patchEnvironment`).
- `DELETE /v1/environments/{envId}` → 204.
- `GET /v1/environments/{envId}/overview` → `EnvironmentRuntime {environment, detail?, deployments[], errors[], unavailable?}`.
- `GET /v1/environments/{envId}/logs?since=&until=&min_severity=&q=&limit=&cursor=` → `RuntimeLogPage` (default: last hour, 200).
- `GET /v1/environments/{envId}/errors?since=` → `{errors: RuntimeErrorGroup[]}` (default last 24h).
- `GET /v1/environments/{envId}/deployments?limit=` → `{deployments: CloudDeployment[]}`.
- `POST /v1/environments/{envId}/errors/task` body `RuntimeErrorGroup` → 201 `BoardTask`.

`RepositorySummary.environments[]` (`EnvironmentSummary`) comes from stored health
only — cheap, no provider call — and is what `projects/hub/EnvironmentChips`
renders per component on the projects hub.

## Roles, task types & workflows

Task types (`task`/`analiz`/`bug`/`technical` plus anything custom) and their
per-column workflow stages are data now, not a closed enum — edited under
Settings → Roles / Settings → Workflows (`pages/RolesSettingsPage.tsx`,
`pages/WorkflowSettingsPage.tsx`). `TaskType` (`api.ts`) is a plain `string`;
`hooks/useTaskTypes.ts` loads the list and `lib/project-board.ts`'s
`taskTypeOptions`/`taskTypeLabel` read it, falling back to the four built-ins
(with their locale strings) until the list has loaded.

- `GET /v1/roles` → `{roles:[{id,key,name,description,required_tools:[],assignments:[{agent_id,agent_name,areas:[]|null,priority}],purposes:[]}]}`.
  `areas: null` means "any area"; `purposes` lists which system duties (below)
  currently point at this role.
- `POST /v1/roles` `{key,name,description,required_tools}` → 201. `PUT
  /v1/roles/{id}` `{name,description,required_tools}` (key is immutable after
  creation). `DELETE /v1/roles/{id}` → 204.
- `PUT /v1/roles/{id}/assignments` `{assignments:[{agent_id,areas,priority}],confirm_grant_tools}`
  → `200 {saved:true,role,granted_tools?}` or `422
  {saved:false,missing_tools:{<agent_id>:[..]}}` when an assigned agent's tool
  policy is short of the role's `required_tools` — the UI opens
  `components/workflow/MissingToolsDialog.tsx` and resubmits with
  `confirm_grant_tools:true`. The 422 is a normal outcome, not a thrown error.
- `GET /v1/agents/{agentId}/roles` → `{roles:[{role_id,key,name,areas}]}`. `PUT
  /v1/agents/{agentId}/roles` `{roles:[{role_id,areas}],confirm_grant_tools}` —
  same 200/422 shape as the role assignments PUT, mirrored the other
  direction. Edited from `components/agent/AgentRolesSection.tsx`, composed
  into `pages/AgentColumnsPage.tsx`.
- `GET /v1/role-purposes` → `{purposes:[{purpose,role_id|null}]}`. `PUT
  /v1/role-purposes/{purpose}` `{role_id|null}`. Purposes are hook names, not
  roles: `system_task_assignee` (who answers a system-created task) and
  `repo_profiler` (who runs repository profiling) — edited as "System duties"
  on the Roles page.
- `GET /v1/task-types` → `{task_types:[{key,label,key_prefix,position,is_default,is_defect,assignee_role_id,assignee_mode,behaviours:[{key,params}],built_in,task_count}]}`.
  `assignee_mode` is `none` (leave whatever was requested), `default` (fill
  only when nothing requested) or `override` (always fill from the role,
  replacing anything requested).
- `POST /v1/task-types` `{key,label,key_prefix,clone_from?}` → 201.
  `clone_from` copies another type's behaviours/stages as a starting point.
  `PUT /v1/task-types/{key}` `{label,key_prefix,position,is_default,is_defect,assignee_role_id,assignee_mode,behaviours}`
  — `409` when changing `key_prefix` on a type that already has tasks. `DELETE
  /v1/task-types/{key}` → `204` or `409` (has tasks, or is the default type).
- `GET /v1/task-types/{key}/workflow` → `{task_type,stages:[{id,column_slug,position,on_path,kind,behaviours:[{key,params}],instructions,participants:[{role_id,mode,instructions,position}]}]}`.
  `kind` is one of `intake|queue|work|review|approval|rework|parked|terminal`.
  A board column with no stage row for a type has no behaviours and routes to
  the assignee (today's custom-column behaviour).
- `PUT /v1/task-types/{key}/workflow` `{stages:[…]}` → `200` or `422
  {error,problems:[{column_slug,field,message}]}` — the UI
  (`components/workflow/StageEditor.tsx`) shows each problem inline under the
  stage/field it names, expanding that stage automatically is not required
  since problems are visible as a per-row badge before expansion too.
- `GET /v1/workflows` → `{workflows:[{task_type,stages}]}` (every type at
  once).
- `GET /v1/workflow/behaviours` → `{behaviours:[{key,scope,label,description,params:[{name,type,options?,required}]}],kinds:[..],areas:[...]}`.
  `scope` is `stage` or `type`; `params[].type` is `column|string|enum|bool`.
  Entirely drives `components/workflow/BehaviourPicker.tsx` — no behaviour key
  is hardcoded client-side.
- `GET /v1/agents/{agentId}/subscriptions` → `{column_slugs:[...],
  subscriptions:[{column_slug,task_types:[]|null}]}`. `PUT` accepts
  `{subscriptions:[...]}` (superset of the legacy `{column_slugs:[...]}` body,
  which is still accepted server-side); `task_types: null` wakes the agent for
  every type on that column, a non-null array filters to those types.
  `pages/AgentColumnsPage.tsx` edits this per column, alongside the agent's
  roles (`AgentRolesSection`).

The old `/v1/settings/analiz-assignment` route and `analiz_assignee_*`
`AppSettings` fields are gone — any role can now hold the
`system_task_assignee`/analysis-assignment duty, not just a hardcoded
analyst agent. `/settings/analiz-assignment` redirects to `/settings/roles`.

## Task assignee

A board task is assigned to an agent only (`assignee_agent_id`); there is no person assignee.

| Call | Contract |
|---|---|
| `PATCH .../tasks/{taskId}` | `assignee_agent_id` is `domain.Nullable`: omitted leaves it, `null` unassigns, a value assigns. Do not narrow the type to one spelling. |

## `sync_warning` on the index status

Set to `err.Error()` when a reindex pass's git pull from origin fails, so the index
still runs (indexing slightly old code beats refusing) but a caller can say the code
it describes may be behind. Cleared by the next pass that pulls cleanly.

## Deploy metadata

Three optional task fields carry the release runbook, and one relation type carries
shipping order.

- Task fields (`before_deploy`, `after_deploy`, `rollback_plan`, all nullable markdown)
  ride on `domain.BoardTask` and are accepted by `POST /v1/repositories/{id}/tasks` and
  `PATCH /v1/repositories/{id}/tasks/{taskId}` — the PATCH follows the usual optional-pointer
  contract (omitted leaves the value, `""` clears it). `before_deploy` is now a gate, not
  just a checklist comment: an `on_merge` component refuses the merge, and a `dispatch`
  component refuses `deploy_release`, while any carried task's steps are unconfirmed
  (`before_deploy_confirmed_at`, confirmed on the task via `api.confirmBeforeDeploy` — see
  "Releases" above); editing the field's text clears a prior confirmation. `rollback_plan`
  is read back into a rollback's `manual_steps`; `after_deploy` is posted as one system
  comment per task once a release finishes.
- `PATCH /v1/repositories/{id}/tasks/{taskId}` also accepts `deploy_depends_on:
  [{target_task_id | target_key}]`, which REPLACES the task's `deploy_depends_on` relations
  and leaves every other relation type alone (`[]` clears them, omitting the field changes
  nothing). `POST .../tasks` accepts the same edges through the existing `relations` array
  with `relation_type: "deploy_depends_on"`. A task may not depend on itself (400); a cycle
  is refused where the edge is written. The relation renders the generated ordering block in
  `before_deploy` and no longer gates a release directly — the old `TriggerRelease`-time
  check was removed along with `trigger_release` itself.

## Production incidents

- `POST /v1/repositories/{id}/incidents` — the alert webhook. Accepts Alertmanager, Sentry,
  GCP Cloud Monitoring and generic `{title, severity, env, detail, fingerprint, resolved}`;
  `?env=` and `?source=` fill what the payload omits. A recovery payload resolves the
  matching incident (202 when nothing matched).
- `GET /v1/incidents?repository_id=&env=&status=open,triaging` — list.
- `GET /v1/incidents/{incidentId}` — incident with its timeline.
- `POST /v1/incidents/{incidentId}/triage` — re-run the remedy engine.
- `POST /v1/incidents/{incidentId}/remedy` — record a proposal `{kind, summary, steps, evidence, confidence, rollback}`.
- `POST /v1/incidents/{incidentId}/resolve` · `POST /v1/incidents/{incidentId}/ignore`.
- `PUT /v1/repositories/{id}/incident-policy` — `{incident_policy: off|suggest|auto_fix}`.

## Store operations

- `PUT /v1/store/credentials/{provider}` — save+validate a store console credential (`provider`:
  `asc` or `google_play`). Body `{"data": {"key_id", "issuer_id", "p8"}}` for `asc`, or
  `{"data": {"service_account_json"}}` for `google_play`. Validated against the console
  (`ValidateAuth`) before anything is persisted; 204 on success, 400 when the input itself is
  invalid (unknown provider, or the console rejects the credential), 500 on a backend/config
  failure (e.g. the secrets cipher isn't configured).
- `GET /v1/store/credentials` — `[{provider, configured, updated_at}]`, one row per known
  provider (`asc`, `google_play`) even when never saved (`configured:false`, zero
  `updated_at`) — never the payload.
- `DELETE /v1/store/credentials/{provider}` — 204; 400 on an unknown provider, same
  validation as the PUT.
- `GET /v1/repositories/{id}/store/apps` — `[]domain.MobileStoreApp`, the repo's per-platform
  store lifecycle rows.
- `POST /v1/repositories/{id}/store/apps/{platform}/verify` — re-run `VerifyOnboarding` against
  the store API right now (the "I did the console step, check it" button) instead of waiting
  for the next monitor sweep; returns the resulting row.
- `GET /v1/store/credentials/{provider}/apps` (`api.listStoreCredentialApps`) — the picker's
  source: `{listing_available, apps: StoreAppRef[]}`. `listing_available:false` is a normal
  200, never a 5xx — Play has no listing endpoint at all, only the separate Reporting API,
  which a service account may not reach; the UI falls back to a manual identifier field.
- `PUT /v1/repositories/{id}/store/apps/{platform}/link` (`api.linkStoreApp`) — body
  `{identifier, store_app_id, name}`, binds the repo/platform to one picked app; returns the
  updated `MobileStoreApp`.
- `GET /v1/repositories/{id}/store/apps/{platform}/tracks` (`api.storeAppTracks`) — reads the
  three channels live and refreshes the row's cache; returns `StoreTracks`.
- `POST /v1/repositories/{id}/store/apps/{platform}/promote` (`api.promoteStoreChannel`) — body
  `{from, to, confirm}`, one channel forward at a time (internal→external→production);
  `to=production` needs `confirm`; 409 when the app isn't ready for the move.
- `POST /v1/repositories/{id}/store/apps/{platform}/build` (`api.startStoreBuild`) — body
  `{engine?}` (`auto|github_actions|local`, defaults to the repo's `release_engine`); 409 when
  neither engine can run it (`domain.ErrNoReleaseEngine`) — the task card is parked on
  `human_decision`, not retried.

Mobile release now runs through a pipeline-generated `scripts/mobile-release.sh` (one script,
a thin workflow calls it) instead of the four deleted platform deploy templates
(`ios-app-store`/`android-google-play`/`flutter-app-store`/`flutter-google-play`) and
`mobile-qa-build`; publishing is store-link + app-pick above, mobile QA is the repo's own
`local_run` doc (`scripts/dev.sh`).

## Verification settings & environment inventory

- `PUT /v1/repositories/{id}/test-strategy` — `{test_strategy: local|stage|per_step}`.
  `local` runs workspace tests only, `stage` (default) deploys to staging when a task
  enters ready_for_qa, `per_step` also deploys at code_review.
- `GET /v1/repositories/{id}/env-inventory` — variable **names** declared by the repo's
  `.env.example`-style files (root + two levels for monorepos; real `.env` files are never
  read) plus the deploy targets, so "what does this need and where does it answer" is one
  call. Deploy targets carry `base_url` (the environment's DNS entry) next to `health_url`.

## Project model

Replaced the old repository profile, in Phase 1: instead of one agent-written
markdown brief, a repository now has a structured model — components, checks,
links, resources — built by a deterministic scan. Agents fetch project facts
through tools themselves rather than reading a server-maintained note.
JSON shapes are the Go types' JSON tags exactly: `server/internal/domain/project_model.go`
(Component, ComponentCheck, ComponentLink, SystemResource,
ProjectScan, `Fact<T>`, enums), `scan_result.go` (ScanResult, inside
`ProjectScan.result` only), `project_model_requests.go` (patch/request bodies,
RepositoryModel, ProjectsOverview, ProjectDetail, ReviewItem, summaries),
`repository.go` (Repository, RepositoryDocs).

`Fact<T>`: `{ detected?: T, override?: T, confidence?: "exact"|"high"|"medium"|"low", evidence?: [{path, line?, note?}] }`.
Effective value = `override ?? detected`; "edited by you" = override present;
reverting to detected means sending `null` for that field in a PATCH.
`Patch<T>` in request bodies: field absent = leave, `null` = clear the
override, a value = set it.

Errors: `{ "error": "message" }` with 400 (bad input), 404 (unknown id), 409
(a scan is already running — body still carries `{scan}`), 500.

- `GET/POST /v1/projects`, `GET/PATCH/DELETE /v1/projects/{id}`,
  `PUT /v1/repositories/{id}/projects` (`{project_ids: []}`) — unchanged, basic
  project CRUD and repo↔project membership.
- `GET /v1/projects/overview` → `{ projects: ProjectOverview[], unassigned: RepositorySummary[] }`
  — the projects hub's one call. `GET /v1/projects/{id}/overview` → the same
  `ProjectOverview` plus `review: ReviewItem[]` across its repositories.
  `ProjectOverview.type` (`"empty"|"single_repo"|"monorepo"|"multi_repo"`) is
  computed; `cross_projects`/`cross_links`/`shared_resources` are empty for an
  independent project.
- `GET /v1/repositories/{id}/model` → `RepositoryModel`: `{ repository,
  shape: "single"|"monorepo", components, checks, links (outgoing),
  incoming_links, resources, linked_components (display info for other
  repositories' components a link points at), review, latest_scan? }`.
- `POST /v1/repositories/{id}/scans` → 202 `{ scan }` (409 while one is
  already running). `GET /v1/repositories/{id}/scans/latest` → `{ scan:
  ProjectScan | null }` and `GET /v1/scans/{scanId}` → `{ scan }` (neither
  carries `result`). `ProjectScan.events[]`: `{stage, done, summary?, at}`;
  stages in order: clone, inventory, shape, components, stack, checks, links,
  deploy, match. Import (`POST /v1/repositories/import|open`, `POST
  /v1/repositories`) now starts a scan instead of the old profile refresh —
  poll `GET /v1/repositories/{id}/scans/latest` every 1s until `status` is
  `succeeded`/`failed`.
- Edits: `POST /v1/repositories/{id}/components` (`NewComponentRequest
  {path, name?, role}`) → 201; `PATCH /v1/components/{id}` (`ComponentPatch
  {name?, role?, commands?: {<purpose>: string|null}, gates?, docs?, status?,
  reviewed?}` — `reviewed: true` acknowledges a component a later scan found
  on its own, clearing `Component.needs_review`).
  `POST /v1/components/{id}/checks` (`NewCheckRequest {name, purpose,
  local_commands:[{dir, argv}], gate}`) → 201; `PATCH`/`DELETE
  /v1/checks/{id}` (manual checks only — a CI-sourced check 400s on DELETE;
  dismiss it instead via the PATCH `status`; `CheckPatch` also takes
  `reviewed?`, clearing `ComponentCheck.needs_review` the same way). `POST
  /v1/links`
  (`NewLinkRequest {from_component_id, to_component_id? | to_resource?:
  {kind, vendor?, name}, protocol, detail?}`) → 201; `PATCH`/`DELETE
  /v1/links/{id}` (same manual-only rule as checks; retargeting a suggested
  link with `PATCH` confirms it; `LinkPatch` also takes `to_resource_id`, to
  point a link at an existing `SystemResource` instead of typing a new one).
  The "add component" folder picker reuses
  the existing `GET /v1/repositories/{id}/directories?path=`.
- **Resource unification** (merging duplicate `SystemResource`s a scan split,
  e.g. "Database" + "PostgreSQL" from the same connection string):
  `SystemResource` gains `name_locked: boolean` (true once a person renames
  it; a later scan no longer overwrites the name). `GET /v1/resources` →
  `{resources: WorkspaceResource[]}` — every resource in the workspace with
  ≥1 non-dismissed link, sorted by kind then name:
  `WorkspaceResource {resource: SystemResource, users: ResourceUser[],
  projects: ProjectRef[], link_count: number}`,
  `ResourceUser {repository_id, repository_name, project_ids: string[],
  component_id, component_path}`. `PATCH /v1/resources/{id}`
  (`{name: string}`) → `SystemResource`, renames it and sets `name_locked`.
  `POST /v1/resources/{id}/merge` (`{into_resource_id: string}`) →
  `SystemResource` — moves every link of `id` onto `into_resource_id` and
  deletes `id`; future scans keep resolving `id`'s signals to the target.
  `POST /v1/resources/{id}/split` (`{link_ids: string[]}`) → 201
  `SystemResource` — moves only those links (which must currently point at
  `id`) onto a fresh copy of it, confirmed.
- **Removed, the UI must not call them:** `GET /v1/repositories/{id}/profile`,
  `POST /v1/repositories/{id}/profile/refresh`,
  `/v1/repositories/{id}/dependencies*`, `GET /v1/projects/{id}/dependencies`,
  `GET`/`PUT /v1/repositories/{id}/pipeline/config`. Phase 2 additionally removed
  `/v1/settings/vercel*`, `/v1/vercel/*`, `/v1/repositories/{id}/vercel/project`,
  `/v1/gcloud/*`, `/v1/repositories/{id}/gcloud/resource` and
  `/v1/repositories/{id}/hosting/*` — see "Cloud accounts & environments (Phase 2)".
  Deploy targets, store, incidents, deploy ops,
  `POST /v1/repositories/{id}/pipeline/setup-task`, docs tasks, index, local
  preview, lifecycle gates and test strategy are unchanged.

## Maps & review (Phase 3)

JSON shapes are `server/internal/domain/project_map.go`'s Go tags exactly
(ProjectMap, MapNode, MapEdge, WorkspaceMap, WorkspaceMapProject,
WorkspaceMapRepository, WorkspaceMapEdge, WorkspaceSharedResource, MapTier),
plus the Phase 1/2 types above.

- `GET /v1/projects/map` → `WorkspaceMap` — registered **before**
  `/v1/projects/{projectId}`, so `"map"` is never read back as a project id.
  Every project with its repositories and component summaries; `edges` exist
  only between projects that actually link (aggregated per ordered project
  pair); `shared_resources` only for a resource linked from ≥2 projects.
  Independent projects appear with no edges — `projects/map/WorkspaceMapView`.
- `GET /v1/projects/{projectId}/map` → `ProjectMap` — `nodes` are every active
  component of the project's repositories (tier by role; libraries get tier
  `"library"`), every resource they link to (tier `"data"`/`"external"`,
  `shared_with` = the other projects linking it), plus FOREIGN component
  nodes (`foreign: true`) for components of other projects on either end of a
  cross-project link; `edges` are links (confirmed + suggested; dismissed
  excluded) with `cross_project` set when one end is foreign. Node ids:
  `c:<componentId>` / `r:<resourceId>`. Health/provider come from the
  component's production environment. Drives `projects/map/ProjectArchitectureMap`,
  the project page's Architecture tab (default the moment the project has a
  repository).
- Review kinds added to `ReviewItem.kind`: `"component"` (a component a later
  scan found — `Component.needs_review`) and `"check"` (a REQUIRED check a
  later scan added — `ComponentCheck.needs_review`). Acknowledge with `PATCH
  /v1/components/{id} {"reviewed": true}` / `PATCH /v1/checks/{id}
  {"reviewed": true}`; dismissing still uses the existing `status:
  "dismissed"` — see `projects/model/ReviewList`.
- `ComponentLink.target_host` / `target_port`: what an unresolved URL pointed
  at. When an environment is later bound whose URL host/domain equals it, the
  link is re-matched to that component automatically (exact match →
  confirmed, `auto_confirmed`). Display-only on the UI side; nothing here
  writes these fields.
- `EnvironmentRuntime.unavailable_code`: `"not_connected" | "cloud_auth" |
  "provider_error"` — `RuntimePanel` reads this to decide Reconnect
  (`cloud_auth`) vs Connect (`not_connected`), never the `unavailable` message
  text.

## Embedding map (UMAP source data)

The browser runs UMAP (umap-js); the backend's job is to pick the right chunk
embeddings, L2-normalize them, and reduce them to a manageable number of
dimensions before they cross the wire. A raw 3072-float embedding × 2000 chunks
is ~25 MB of JSON; the same points at 50 dimensions are ~400 KB.

- `GET /v1/embedding-map/sources` — what can be visualized, so the UI can build
  its selectors.

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
  repository indexed on several branches contributes **one** row, picked
  deterministically: most chunks, ties to the most recently indexed, then index
  id. `indexed_at` is `null` for an index that never completed.

- `GET /v1/embedding-map?source=files|code&repository_id=&limit=&dims=` — the
  projection itself.

  - `source` is required; anything other than `files`/`code` is `400`.
  - `repository_id` is required (and must be a uuid) when `source=code`, else
    `400`. Ignored for `files`.
  - `limit` defaults to 2000, `dims` to 50. Out-of-range values are **clamped**
    (limit 100…5000, dims 2…128), never rejected — they are viewer controls.
  - `dims` is additionally capped by the source's own embedding width — a
    64-wide embedding answers `dims=128` with `"dimensions": 64`. The response's
    `dimensions` field is authoritative; never assume it equals what was asked.

  ```json
  {
    "source": "code",
    "repository_id": "uuid-or-empty-string",
    "branch": "main",
    "dimensions": 50,
    "total": 8321,
    "sampled": 2000,
    "truncated": true,
    "points": [
      {"id": "uuid", "group_id": "uuid-or-path", "group_label": "src/api.ts",
       "chunk_index": 0, "snippet": "first ~200 chars, whitespace-collapsed",
       "language": "typescript", "symbol": "fetchTasks", "vector": [0.12, -0.4]}
    ]
  }
  ```

  `vector` is exactly `dimensions` long on every point. For `source=files`,
  `group_id` is the `files.id` and `group_label` the filename; for
  `source=code`, both are the repository-relative file path, `chunk_index` is a
  0-based ordinal within that file (start line, then id), and `language` /
  `symbol` carry the chunk's tree-sitter metadata (empty string when unknown).

  `total` is what is stored, `sampled` is what came back. When `total > limit`
  the rows are sampled **evenly** over a stable ordering — every point is a
  step of `floor(rn·limit/total)` over `(path, start_line, id)` — not the first
  N, which would show only the alphabetically-first files. The sample is stable:
  the same request returns the same points. `sampled` can also fall below the
  sampled row count when a stored embedding is unusable (zero, NaN/Inf, or of a
  different width than its neighbours after an embedding-provider switch); such
  rows are dropped rather than plotted at the origin.

  An empty source is `200` with `"points": []` and `"total": 0`, not an error —
  including `source=code` for a repository that has never been indexed.

Reduction is PCA computed in-process (`application/embedmap`): L2-normalize,
mean-center, then block power iteration with Gram-Schmidt deflation against the
covariance action `Xᵀ(Xv)` — the d×d covariance matrix is never materialized.
It is deterministic down to the last bit, including the parallel decomposition,
so a client may cache a projection and diff it against a later one.
