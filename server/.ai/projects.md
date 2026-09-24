# Repositories & Initiative Projects

Code repositories bind a filesystem directory to codebase indexing and the kanban board. Repository-scoped chat is removed; agents are triggered by board events instead. The board is global — there is no team or tenant grouping (`team_id` removed migration 038, `tenant_id` removed migration 133). See [Person columns](#person-columns-migrations-115-133) for the per-person columns migration 115 added and migration 133 then dropped.

An initiative project groups repositories; a repository groups **components** — the actual
buildable/deployable units a scan finds (migration 153, replacing the markdown profile and
`repofacts`/`repoprofile`). The hierarchy is `projects → repositories → components`, and
everything below this section is the component layer.

## Data Model

| Table | Purpose |
|-------|---------|
| `repositories` | `name`, `description`, `root_path`, `verify_command`/`build_command`/`test_command` (settings-editable via repo PATCH, each independently clearable) (no `team_id`) |
| `repository_projects` | Links repos to initiative projects |
| `projects` | Initiative projects: `name`, `description` (no `team_id`) |
| `workspace_indexes.repository_id` | Code index scoped to repository |
| `board_tasks` | Board tasks with `board_column` slug validated against `board_columns`, global `task_number`; `component_id` (migration 153) ties a task to the component it touches |
| `project_components` | One buildable/deployable unit per row: `path` (`.` for the whole repo), `name`/`role`/`stack` as Facts, `commands`, `docs`, `gates`, `status`, `needs_review` (migration 155) |
| `component_checks` | One CI job → one component, `purpose`/`gate`/`local_commands` as Facts, `needs_review` (migration 155) |
| `component_links` | One outgoing edge: to another component or to a `system_resources` row, `status`/`confidence`, `target_host`/`target_port` (migration 155) |
| `system_resources` | A workspace-wide node (database, queue, SaaS API…), deduped by `identity_key` |
| `project_notes` | Judgment an agent or a human wrote, evidence-gated |
| `project_scans` | One scan's progress/events/result, `review_count` |
| `cloud_accounts` / `component_environments` | Where a component runs — see [Cloud accounts & environments](#cloud-accounts--environments-phase-2) |

## Board Columns

Column slugs are global in `board_columns`. Default template: `backlog`, `todo`, `in_progress`, `ready_for_qa`, `in_qa`, `need_revision`, `pm_uat`, `human_uat`, `done`, `released`.

## API

| Method | Path |
|--------|------|
| GET/POST | `/v1/repositories` |
| POST | `/v1/repositories/open` |
| GET/PATCH/DELETE | `/v1/repositories/:id` |
| POST | `/v1/repositories/:id/restore` (re-clone the working copy onto this host) |
| PUT | `/v1/repositories/:id/projects` |
| GET | `/v1/repositories/:id/index/status` |
| POST | `/v1/repositories/:id/index` |
| GET/POST | `/v1/repositories/:id/tasks` |
| PATCH/DELETE | `/v1/repositories/:id/tasks/:taskId` |
| GET/POST | `/v1/repositories/:id/tasks/:taskId/comments` |
| GET | `/v1/repositories/:id/tasks/:taskId/runs` |
| GET/POST | `/v1/repositories/:id/tasks/:taskId/pipelines` |
| GET | `/v1/repositories/:id/tasks/:taskId/pipelines/:pipelineId` |
| GET | `/v1/tasks` (board tasks; released >7d are in the archive) · `/v1/tasks/released?q=` · `/v1/tasks/lookup?key=` |
| GET/POST | `/v1/projects` · GET/PATCH/DELETE `/v1/projects/:projectId` |
| GET | `/v1/projects/overview` · `/v1/projects/:projectId/overview` — cards for the projects page; `review_count`, `cross_projects`/`cross_links` (only when a link actually crosses), `shared_resources` |
| GET | `/v1/projects/map` — `domain.WorkspaceMap`: every project with its repositories/components, aggregated project-pair edges (only where projects actually link), resources shared by ≥2 projects. Registered before `/v1/projects/:projectId` so the literal path wins |
| GET | `/v1/projects/:projectId/map` — `domain.ProjectMap`: every active component of the project's own repositories, the resources they link to, and — only where a link crosses the boundary — the foreign component on the other end (`foreign: true`); node ids `c:<componentId>`/`r:<resourceId>` |
| GET | `/v1/repositories/:id/model` — `domain.RepositoryModel`: components, checks, links, incoming links, resources, notes, environments, review queue, latest scan |
| GET | `/v1/repositories/:id/brief?component_id=&area=` — the markdown injected into every board run and repo-bound chat |
| POST | `/v1/repositories/:id/scans` · GET `/v1/repositories/:id/scans/latest` · GET `/v1/scans/:scanId` |
| POST `/v1/repositories/:id/components` · PATCH `/v1/components/:componentId` |
| POST `/v1/components/:componentId/checks` · PATCH `/v1/checks/:checkId` · DELETE (manual only) |
| POST `/v1/links` · PATCH `/v1/links/:linkId` · DELETE (user-created only) |
| PUT `/v1/repositories/:id/notes` · PATCH `/v1/notes/:noteId` · DELETE `/v1/notes/:noteId` |

## Scans and the Fact rule

A scan (`internal/application/discovery`, a pure filesystem/git reader — no network, no
LLM) walks one working copy and returns a `domain.ScanResult`: components, their commands,
CI checks, links to other components/resources, deploy signals, and git metadata. Triggers
(`domain.ScanTrigger`): `import` (first add), `manual`, `push` (webhook/poll), `stale` (no
scan in 7d), `migrate` (a data migration replay). `StartScan` is single-flight per
repository; the result is stored verbatim on the `project_scans` row so a reconcile can be
replayed and audited.

Every detected value lands in a `domain.Fact[T]{Detected, Override, Confidence, Evidence}`:
a rescan only ever rewrites `Detected`, `Override` is the human's word and survives every
rescan until they revert it (`Fact.Value()` prefers `Override`). `Confidence` grades a
detection — `exact`/`high` apply without asking, `medium` queues for review
(`ReviewItem`), `low` never reaches the model (a brand-new low-confidence link isn't even
created). `internal/application/projectmodel/reconcile.go`'s `planReconcile` is the pure
function that diffs a scan's result against the stored model and decides what to
save/delete; it is exercised directly by table tests, no store required.

A component or a link a rescan no longer finds is deleted UNLESS the human touched it
(an override, `manually_added`, a non-default gate/docs) or it's already dismissed — those
survive as `missing: true` instead. A component the human dismissed and a check/link the
human dismissed are never resurrected by a later scan finding the same thing again.

**Review of later additions.** A scan whose trigger is not `import`/`migrate`, on a
repository that already had a model, flags every component it CREATES and every REQUIRED
check it creates with `needs_review` — a push added something the human hasn't seen, not
the first import. `PATCH /v1/components/:id {"reviewed": true}` /
`PATCH /v1/checks/:id {"reviewed": true}` clears it; dismissing (`status: "dismissed"`)
clears it too. `RepositoryModel.review` / `ProjectDetail.review` carry every open
`ReviewItem` (`role`, `link`, `environment`, `component`, `check` kinds).

After reconcile: the legacy fields project (below), a finished scan's deploy signals are
matched into `component_environments` (`cloud.Service.MatchScan`), `projectmodel.Relink`
re-resolves any link still waiting on a target, and — on the first scan, or when nothing has
documented the repository's purpose yet, or a stale note needs a rewrite — an agent pass
writes judgment notes (`internal/application/projectmodel/profiler.go`).

## Checks

`ComponentCheck` maps one CI job (or a manually added one) onto the component it verifies —
a workflow job that touches two components becomes one check per component, each holding
only that component's own local commands. `Gate` decides what an agent's hand-off does with
it: `required` runs locally before hand-off and is read back from CI by the pipeline gate,
`info` doesn't block, `off` is ignored. `domain.DefaultCheckGate` requires only cheap,
locally-reproducible purposes (lint/typecheck/test/build) that have local commands;
everything else defaults to `info`. `ComponentCheck.Required()` is what the brief and
`RequiredCommands` read.

## Links and resources

`ComponentLink` is one outgoing edge, resolved to exactly one of `ToComponentID` /
`ToResourceID`, or neither while the human is asked to pick (`hint`, `target_host`/
`target_port` carry what an unresolved URL pointed at). `status` is `suggested` (medium
confidence, or ambiguous), `confirmed` (exact/high, or a human's own pick —
`auto_confirmed` marks the former), or `dismissed`. `source` is `scan` or `user`; only a
user-created link can be deleted outright, a scan-detected one is dismissed instead.

Cross-repository resolution runs in two passes: at reconcile time, `matchByPackage`/
`matchByName` (`internal/application/projectmodel/match.go`) try to resolve an unresolved
signal against every OTHER repository's components. `projectmodel.Relink` runs a second,
wider pass — at the end of every successful scan and after a cloud environment is
newly confirmed (`BindEnvironment`/`PatchEnvironment`/`MatchScan`, run in a goroutine so the
request doesn't wait) — matching every still-open link's `target_host` against every
OTHER repository's confirmed environment URLs (case-insensitive, `www.` ignored): exactly
one match confirms the link outright; a `localhost`/`127.0.0.1` target whose port matches
another component's `Stack.DevPort` in the SAME project is a medium suggestion, never more,
since two repositories can share a dev port by coincidence. A link the human already
confirmed or dismissed is never touched again.

`SystemResource` is a workspace-wide node (database, cache, queue, SaaS API…) deduped by
`identity_key`, so a SaaS API is one node however many components call it. `shared_with` on
a map node lists the OTHER projects also linking the same resource.

## Notes

`ProjectNote` is judgment a parser cannot make: `purpose`, `entrypoints`, `conventions`,
`invariants`, `danger_zones`, `change_recipes`, `gotchas` — repository-level or
component-scoped. An agent may only write one through `record_project_note`, and only with
at least one evidence path that resolves in the working copy; a locked note, or one whose
evidence doesn't resolve, is refused. A push marks a note `stale` when a changed path
touches its evidence, without deleting it, so a stale note is still read (marked
`(may be outdated)`) until it's rewritten.

## Cloud accounts & environments (Phase 2; replaced Hosting Links / migration 112)

`repository_hosting_links`/the standalone Vercel settings/GCloud settings are gone. In their
place: `cloud_accounts` (one connected provider login — Vercel, GCP or AWS) and
`component_environments` (one `(component, environment)` → account + resource binding,
matched by a scan or confirmed by a human). This is the "where a component runs" half;
`repository_deploy_targets` stays the "how a task ships" half, and the two meet through the
legacy projection below.

| Piece | Where |
|-------|-------|
| Service | `internal/application/cloud` (`cloud.Service`) |
| Provider adapters | `internal/adapter/cloud/{vercel,gcloud,aws}` |
| Stores | `internal/adapter/storage/postgres/{cloud_accounts,component_environments}.go` |
| Routes | `internal/adapter/http/handler_cloud.go` — see api-spec's "Cloud accounts, environments & runtime" |
| Agent tools | `get_environment`, `query_runtime_logs`, `list_runtime_errors`, `list_deployments` — `.ai/tool-reference.md` |

An install's pre-cloud-accounts Vercel/GCloud connection and its per-repository links carry
forward once, in the background, as `cloud_accounts`/confirmed `component_environments` rows
(`cloud.Service.Boot`, reading `port.LegacyCloudSource`) — nothing for the operator to redo.
A scan match is never decisive on its own the way the old detector was either: an ambiguous
signal leaves `candidates` on the row for a human (or `PATCH .../environments/:envId`) to pick.

`EnvironmentRuntime.unavailable_code` (`"not_connected" | "cloud_auth" | "provider_error"`,
Phase 3) is why `GET .../environments/:envId/overview` couldn't reach a live picture — the UI
decides "Reconnect" from the code, not from the message text.

## Agent tools and the brief

The structured model's agent-facing surface (`internal/adapter/tools/projectmodel`,
`internal/adapter/tools/runtime`) is documented in full in
[Tool Reference](tool-reference.md): `get_project_brief`, `list_component_checks`,
`list_links`, `record_project_note` read/write the component layer; `get_environment`,
`query_runtime_logs`, `list_runtime_errors`, `list_deployments` read the cloud layer. The
brief (`projectmodel.Service.Brief`, `GET /v1/repositories/:id/brief`) is the markdown every
board run and repo-bound chat gets before it touches a repository: what the repository (or
one component/area) is, what it runs, what it talks to, the required checks to run before
hand-off, and the judgment notes on top — capped at 8000 characters, dropping the
lowest-priority component blocks first rather than truncating mid-sentence.

## Legacy projection (kind, sub_projects, pipeline slots, deploy targets)

Several older subsystems still read repository-level columns directly rather than the
component model: the pipeline auto-detect keys off `repositories.kind`, `storeops`/mobile
release reads `mobile_platform`/`detected_bundle_id`/`detected_xcode_scheme` etc.,
`repository_pipeline_jobs` is what the CI gate and deploy monitor resolve stages from, and
`repository_deploy_targets` is what `get_deploy_target`/the health probe/deploy watch read.
Rewriting all of those to read components directly is out of scope for this model; instead,
every reconcile and every environment change re-derives and overwrites the legacy columns
FROM the current components/checks/environments (`internal/application/projectmodel/projection.go`,
`internal/application/cloud/projection.go`), writing only what changed:

- `kind`/`sub_repo_kinds` from the components' roles (`ComponentRole.LegacyRepoKind()`);
  `monorepo` when more than one is active.
- `sub_projects` (one entry per active component, JSON) on a monorepo; mobile identity/build
  targets project straight onto `mobile_platform`/`detected_*` on a single-component mobile
  repo.
- `repository_pipeline_jobs` — one row per (sub-project, category) slot — from every
  REQUIRED lint/typecheck/build/test check and every `deploy` check with an environment.
- `repository_deploy_targets.base_url`/`health_url`/`provider` from every CONFIRMED
  environment; `provider` maps by resource kind, not by cloud provider, since GCP alone
  covers three legacy providers. Never deletes a target — an environment being dismissed or
  unbound is not evidence the deploy stopped existing.

These legacy tables/columns are kept **only** so the systems that already read them (board
CI gate, deploy monitor, prodops, mobile release, `storeops`) keep working unmodified; a
client of the project model itself should always read components/checks/environments, never
`kind`/`sub_projects`/`repository_pipeline_jobs`/`repository_deploy_targets` — those are
write-only from here down.

## Agent Workspace

Board-triggered runs use `repository.root_path` as the effective workspace for shell and code tools.

A recorded `root_path` may name a folder that no longer exists on this machine — the data directory moved, or the checkout was deleted by hand. The card says so (`domain.GitPresence`), and `POST /v1/repositories/:id/restore` is the way back: it clones from the recorded `remote_url` into **this** runtime's layout (`<workspace>/repos/<name>`, the same helper the GitHub import uses) and re-points `root_path` at where the code actually landed. It is explicit and one repository at a time — a clone is minutes and gigabytes, so it is never started by a read — and it refuses, without touching anything on disk, unless the folder is genuinely missing and a remote is recorded.

## Agent Board Tools

- `list_project_tasks`
- `list_ready_tasks` (the unblocked `backlog`/`todo` queue, priority-sorted)
- `create_project_task`
- `move_project_task`
- `update_project_task`
- `claim_project_task`
- `add_task_comment`

Repository resolution: tools read `repository_id` from context; if absent they fall back to the first repository (`DefaultRepositoryID`) since there is no team scope.

See [Workspace](workspace.md) for the board/dispatch layer.

## Person columns (migrations 115, 133)

Migration 115 added `tenants`, `tenant_members` and per-row `owner_user_id`/`assignee_user_id`
columns for a person layer no code ever used; migration 133 dropped all of them along with the
rest of the tenant schema.

The install has one person and no login: a task is assigned to an agent only
(`assignee_agent_id`), the dispatcher wakes every column subscriber, and memory reads have
no owner filter.
