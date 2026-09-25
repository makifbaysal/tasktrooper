# Architecture

Hexagonal (ports and adapters): all business logic lives in `application` and `domain`;
frameworks and external systems are adapters.

## Layers

```
cmd/agent-server/main.go    ← wiring: read config, build adapters, start server
internal/
  domain/                   ← Message, ToolCall, Config, AgentRequest/Response
  port/                     ← LLMClient, ToolExecutor, ToolRegistry (interfaces)
  application/
    agent/                  ← agent loop: LLM ↔ tool execution cycle
    config/                 ← config loader (koanf + YAML)
    registry/               ← tool registry implementation
  adapter/
    http/                   ← Fiber handlers, OpenAI request/response schema
    llm/                    ← LM Studio HTTP client (OpenAI-compatible)
    tools/shell/            ← run_terminal tool
    tools/web/              ← fetch_url tool
    mcp/                    ← MCP client adapter (go-sdk)
    mcpserver/              ← the /mcp endpoint CLI sessions call back on
```

## Request flow

1. `POST /v1/chat/completions` with a Bearer token.
2. `Handler.ChatCompletions` converts the OpenAI request to `[]domain.Message`.
3. `agent.Loop.Run` fetches all tool definitions from the registry (built-in + MCP).
4. `llm.Chat` is called with messages + tools.
5. Returned `tool_calls` are dispatched to the registry, results appended, loop iterates.
6. The loop exits on a plain message or max iterations; the final assistant message is
   returned in an OpenAI-compatible response.

**Retries.** An LLM error is retried twice (3 attempts total); persistent failure is a
hard error — no fallback parsing or patching of LLM output.

## MCP integration (client side)

`mcp.Manager.LoadAndRegister` iterates `tools.mcp_servers`: `stdio` spawns the server via
`mcp.CommandTransport`, `http` connects with `mcp.StreamableClientTransport`. Each
server's tools are discovered with `session.ListTools` and registered as
`mcp_<server_id>_<tool_name>` (no collisions with built-ins); calls dispatch back through
`session.CallTool`.

## Orchestration flow

With `orchestration.enabled`, a session message can trigger multi-agent orchestration:

1. `orchestrator.Router` applies a fast-path heuristic — short/simple messages skip
   orchestration unless `orchestrate: true`.
2. `orchestrator.Planner` decomposes the request with a strict JSON schema (agents,
   skills, dependencies), retrying twice on parse failure — no key patching.
3. Plan and tasks persist in `orchestration_plans` / `plan_tasks`; activity step
   `orchestration_plan_created` is recorded.
4. `orchestrator.Executor` topologically sorts tasks into waves and runs independent ones
   in parallel (`max_parallel_tasks`, errgroup).
5. Each subtask builds its system prompt from agent prompt + skills + subtask rules
   (`prompt.Builder`) and runs `agent.Loop`; failures retry twice with repair context.
6. Steps `subtask_started` / `subtask_completed` / `subtask_failed` /
   `orchestration_complete` are recorded; the final response is appended to the session
   (LLM synthesis when `orchestration.synthesis_enabled`).

Skills and orchestrator rules are agent-scoped (`/admin/agents/:id/skills`, `…/rules`);
skill create/update embeds via `llm.Embed`. Plan status: `GET /v1/runs/{runId}/plan`. See
[Orchestration Agents](orchestration-agents.md).

## QA pipeline (board)

On a move to `ready_for_qa`, `board.PipelineRunner` runs an async build/test pipeline
against the task workspace (migration 040: `task_pipelines`, `task_pipeline_jobs`). QA
dispatch is gated on green (`DispatchQA`); a failure moves the task to `need_revision`
with a system comment carrying the failing stage's log. A repository with no
job/workflow mapping records `skipped` (migration 067), which opens the gate
(`domain.PipelineStatusOpensGate`) but is never painted green — nothing was built.

Stages resolve to the repository's configured verify/build/test commands, or a
multi-language auto-detect per ecosystem marker (Go, Node, Rust, Python, Maven, Gradle);
a `Dockerfile`/`Containerfile` triggers a container build via the configured runtime
(`podman`/`docker`/auto).

Agents read the latest run with `get_pipeline_status`, and a failed report is
auto-injected into the next `need_revision` run's system prompt. API:
`GET/POST /v1/repositories/:id/tasks/:taskId/pipelines` (+ `/:pipelineId`).
`BoardTask.latest_pipeline_status` is bulk-enriched (no N+1) on list endpoints. Every FK
reachable from `repositories`/`board_tasks` cascades on delete, pipelines included.

Full breakdown, with the other board quality gates: [Orchestration
Agents](orchestration-agents.md) → "QA pipeline (migration 040)".

### The code-review gate has a bounded life (migration 107)

The gate defers the reviewing architect on a move into `code_review` until the pipeline
reports. Originally only `PipelineRunner.finalize`, **inside the process that started the
pipeline**, could open it — so a process restart mid-poll, a repository out of Actions
minutes (402), or a hook registered for `push` only left the card wedged with a spinner
and no agent, permanently and silently.

The gate is unconditional — there is no per-repository off switch; it cannot deadlock
because the other three mechanisms below all bound the wait. Three things bound it,
fastest first:

1. **The webhook, actually wired** — the hook subscribes to `push`, `workflow_run` and
   `check_suite` (`githubapi.WebhookEvents`). A completed delivery goes
   `handler_github_webhook.go` → `repository.Service.HandleGitHubWorkflowEvent` →
   `PipelineRunner.ResolveByHeadSHA`, the join from "GitHub finished a run" to "which card
   was waiting" (`task_pipelines.head_sha`). `pull_request` is deliberately absent — the
   board opens and merges its own PRs and the gate reads run conclusions, not PR state.
2. **Hook repair** — `ReconcileWebhooksAsync` at boot installs a missing hook and
   `PATCH`es an existing one's **events list only**
   (`githubapi.ReconcileRepoWebhookEvents`): no `config` block, so no secret rotation and
   no window of rejected deliveries. It sends the union (an operator's extra event
   survives) and leaves a `*` wildcard hook alone. A converged hook costs one GET.
3. **A reconciling poll** — `board.PipelineGateSweeper` every `board.pipeline_gate_interval`
   (2m) asks GitHub about each unfinished pipeline and settles it. `PipelineRunner.inflight`
   keeps it off pipelines this process already polls, so the two never double-write jobs.

Three possible outcomes, and the difference is recorded:

- **A real result** → `finalize`: success/skipped hands the task to its reviewer, failure
  moves it to `need_revision` with the failing job's log. A red build never opens the gate,
  and at the deadline a check that already reported red still beats the clock.
- **An answer that can never come** → the gate OPENS with `task_pipelines.gate_reason`:
  `no_ci_configured`, `ci_unavailable` (`githubapi.IsCIUnavailable` — 402, or a 403 naming
  billing/quota/disabled; a plain rate limit does not count — or no run for the head SHA
  after `pipelineNoRunGrace`, 5m), or `timeout` (nothing within `board.pipeline_gate_timeout`,
  45m). The pipeline is `skipped`, never `success`; a comment says why; the event reads
  `pipeline_gate_opened`.
- **The card already moved on** → the row is settled quietly, no dispatch, no move.

`BoardTask.latest_pipeline_gate_reason` rides beside the status in the same bulk query, so
the card shows a warning glyph with the reason instead of a spinner.

The 45-minute window is derived, not chosen: it must exceed `pipelineMaxWait` (30m, the
in-process poll's budget) so a live pipeline always produces the real verdict first —
otherwise the sweeper would convert a build about to go red into an opened review gate —
plus 15 minutes of slack for a process restart and Actions queue time. Every other path
pre-empts it.

## Where work may start, and who is recorded as starting it

`board.Dispatcher.Dispatch` records the board event for every task change, but two
columns never produce an agent run (`isDispatchSuspendedColumn`): `blocked` (the
clarification resume path owns it) and `backlog` (not yet taken onto the board — a task
created there with an assignee used to be dispatched immediately by its own `task.created`
event). `board.Reconciler` skips the same columns. Work starts when the task is moved onto
the board.

Every `task.moved` payload carries an explicit `actor` (`agent`/`human`/`system`), and
system moves also carry a `system_reason` (`domain.MoveReason*`), so a system-originated
move never renders as "by User". The pipeline hand-off changes no column, so
its history row shows the reason sentence instead of an empty `▭ → ▭`.

## Column span ledger (migration 057)

`task_column_spans` records one uninterrupted stay of a task in one column: entry, exit,
duration, and the agent that worked it. It is the source of truth for time KPIs and for
deciding which agent a defect escape belongs to.

Spans are written from `board.Dispatcher.Dispatch` — the single place a board event row is
created — so no move path can bypass the ledger and the destination column never has to be
recovered from an event payload. The owning agent is claimed when the span's first
`task_agent_runs` row is created.

`board_tasks.clean_completion` / `completed_at` are stamped by `board.CompletionStamper` on
`done`/`released`, recording whether the task got there without entering `need_revision`;
time KPIs read only clean tasks. Review verdicts (`review_verdict` on the open span) drive
the "human after agent" review mode.

## Context optimization (large codebases)

Three layers reduce tokens: **repository mapping** (ASCII tree + skeleton signatures,
[repository-mapping.md](repository-mapping.md)), **workspace RAG** (AST chunks embedded in
Postgres, top-K retrieval, [codebase-indexing.md](codebase-indexing.md)) and the
**dependency graph** (call/import edges, BFS around a symbol,
[dependency-graph.md](dependency-graph.md)).

Session message flow: history from Postgres → optional upload RAG (`file_ids`) →
workspace/index inject (tree + chunks) → token budget (rolling summary, then trim) → agent
loop or orchestrator. The orchestrator adds planner/explorer context via
`orchestrator.ContextBuilder`; code tools (`codebase_search`, `grep_code`, `get_repo_tree`,
`get_symbol_skeleton`, `expand_symbol_context`) allow on-demand retrieval. See
[context-management.md](context-management.md).

## Deploy targets & production incidents (migration 058)

`repository_deploy_targets` holds one row per (repository, env): provider, template id,
substitution vars, health URL, rollback policy. Recipes are embedded markdown
(`internal/application/deploy/templates/*.md`, YAML frontmatter + workflow YAML) rendered
against a target — `{{var}}` placeholders are filled, `${{ … }}` Actions expressions are
left alone, an unfilled required var stays visible as `{{key — SET THIS}}`.

Production signals converge on `prod_incidents`: the alert webhook (Alertmanager / Sentry /
Cloud Monitoring / generic JSON, normalized in `prodops.Normalize`), the health monitor
(`prodops.Monitor` — two consecutive failures open an incident, a success closes it), and
failed stage/preprod/prod deploys reported by the pipeline runner.

Dedupe is a partial unique index on `(repository_id, env, fingerprint) WHERE status NOT IN
('resolved','ignored')`: an alert storm folds into one incident (occurrences++), while the
same alert months after a resolve opens a new one. Severity escalates on recurrence, never
downgrades.

`prodops.Suggest` is a pure rules engine over (incident, recent deploys, prior resolved
incidents with the same fingerprint, deploy target): a deploy finishing within 45 min
before onset yields a rollback proposal; a previously resolved fingerprint replays the fix
that worked; otherwise the error signature classifies it as config / dependency / capacity
/ code defect. An unmatched incident still returns a diagnostic checklist — never silence.

`incident_policy` decides what follows: `off` records only, `suggest` (default) opens a
diagnosis task that must stop at a written proposal, `auto_fix` lets the task carry the fix
through the board.

## Run lifetime vs process lifetime (drain, heartbeat, stale sweep)

A board run is minutes of work on a checked-out branch; the server process can restart
mid-run (a rebuild, a crash, a machine sleep/wake). Liveness for a run is **never** inferred
from any in-memory map — it comes from the database:

- **The server drains on shutdown.** `Runner.Drain(ctx)` stops taking new jobs and waits for
  in-flight ones. `server.Shutdown()` runs first; terminal run writes go through `persistCtx`
  (`context.WithoutCancel` + 15s) so even a run whose request context was just cancelled
  still records its final status instead of leaving the row stuck `running`.
- **`updated_at` is a heartbeat, not a start timestamp.** The runner `Touch`es the row every
  `runHeartbeat` (10s), and the heartbeat is the ONLY liveness authority — nothing infers
  "abandoned" from a process's own in-memory state. `reconcile_stale_after` (30m) is clamped
  to `maxRunStale` (18 missed beats), and `FailIfStale` re-asserts the cutoff inside the
  write, so a run that heartbeats mid-sweep is never falsely failed.

The UI had the mirror-image bug: liveness inferred from the step stream alone showed a killed
run as "Live" forever. `useRunActivity` takes the run's own status, and an unfinished subtask
in a run that is over renders as interrupted.

## Migration gate & test strategy (migration 059)

A schema change is the one class of change build+test cannot judge — both stay green while
production breaks on rollout. The board detects it from the branch diff
(`domain.DetectMigrationChange` over `git diff --name-only` against the merge base,
covering golang-migrate, Flyway/Liquibase, Prisma, Alembic and Rails layouts, ignoring
vendor and testdata) and stamps `board_tasks.has_migration`. An agent cannot opt out by not
mentioning it.

`board_tasks.stage_verified_at` is written only by a **successful stage deploy** of that
task (`PipelineRunner.finalize`). `TriggerRelease` refuses `has_migration &&
stage_verified_at IS NULL` (`ErrMigrationNotStaged`) and comments why, so the block is
visible on the board rather than only in a failed API call.

`repositories.test_strategy` decides when staging is used: `local` (workspace tests only),
`stage` (default, deploy at ready_for_qa) or `per_step` (also at code_review). The
migration gate is independent — a `local` repo still cannot release an unstaged schema
change, it just dispatches the stage deploy deliberately.

## Mobile store deploy (migration 061)

Two providers (`app_store`, `google_play`), **no templates** — the five mobile ones were
deleted (2026-09-02). A repository is bound to one app picked from the connected console
(ASC `GET /v1/apps`; Play only through the Reporting API's `apps:search`, which falls back
to a typed identifier when the service account cannot reach it), and `storeops/pipeline`
generates one `scripts/mobile-release.sh` plus a thin workflow that calls it — same script,
both engines.

Channels are `internal` / `external` / `production` (`domain.StoreTracks`), promoted one
step forward only. iOS: TestFlight internal groups → external groups + Beta App Review →
App Store version. Play: `internal` → `alpha`/`beta` → `production`.

`repositories.release_engine` (`auto` | `github_actions` | `local`, migration 126) picks
where a release runs. `auto` tries Actions, then falls back to running the release script on
this machine. Only a definite "Actions cannot run" (no workflow, 402, billing, quota) falls
through; a plain 5xx propagates. When neither can run, `domain.ErrNoReleaseEngine` parks the
card on `human_decision` — there is no third path.

The script's build targets come from the working copy, not from a convention
(`repositories.detected_xcode_scheme` / `detected_gradle_module`, migration 127; read by
`repository.DetectBuildTargets` at import, sub-projects carry their own inside
`sub_projects`). iOS: the shared `.xcscheme` file names first (test/extension names
dropped), else the `.xcodeproj`/`.xcworkspace` name; Android: the `settings.gradle` module
applying `com.android.application`. Two candidates or none ⇒ `""`, and `""` fails
`StartBuild` with `storeops.ErrBuildTargetUnknown` (409) naming the fix, instead of
archiving a scheme that does not exist.

`mobile_store_apps` (one row per repository+platform) is the source of truth for
first-publish-vs-update: `unregistered → onboarding → test_ready → live`, with one backward
edge `test_ready → onboarding`. `mobileStoreGate` reads it on every stage/prod dispatch:
stage needs `test_ready` or `live` (`ErrMobileAppNotTestReady`), prod needs `live`
(`ErrMobileAppNotLive`); a lookup failure propagates rather than failing open.

A successful prod deploy for a store target *is* the submit for review, so
`board.PipelineRunner.markStoreSubmitted` calls `storeops.MarkSubmitted`
(`review_state = waiting_for_review`) — that write is what puts a row in the monitor's poll
gate, and a "no workflow configured" skip never does.

`storeops.Monitor` sweeps every registered app on `storeops.poll_interval` (5m):
re-verifies an `onboarding` checklist against the store APIs (advancing to `test_ready`),
detects go-live, and polls a `live` row's pending review — a rejection or halted Play
rollout ingests into `prod_incidents`. The dedupes differ: an iOS rejection is a terminal
`review_state`, so the row leaves the poll gate for good; a halted Play rollout has no such
field and is deduped by an in-memory fingerprint claim, so one still halted across a restart
is reported once more. Signing assets due within 30 days are renewed in the same sweep; a
renewal whose GitHub secret push fails is retried next sweep (the fresh expiry would
otherwise carry it out of the window forever).

Signing is system-owned: an iOS distribution cert + provisioning profile minted through App
Store Connect; an Android upload keystore generated locally (self-signed RSA-2048,
25-year), its alias read back out of the produced PKCS#12 (go-pkcs12 writes no
friendlyName), falling back to Java's default `"1"`, never a hardcoded `"upload"`. Both are
encrypted at rest in `signing_assets` with `secrets.Cipher` and pushed to the repository's
Actions secrets on every mint or renewal.

Manual first publish is forced by the APIs: ASC cannot create an app record, Play can
neither create an app nor accept its first AAB, and iOS builds need macOS runners. First
publish stops at TestFlight / Play internal testing with a guided onboarding checklist
task; once `live`, a prod deploy is a store submit with no human step — the prod workflows
never rebuild (`--skip_binary_upload`, `track_promote_to`).

Migration 061 tables: `store_credentials`, `mobile_store_apps`, `signing_assets`.

## The review chain gate (migration 080, unconditional since migration 158)

`done` claims "this passed its review chain"; nothing checked that until this gate
(`internal/domain/lifecycle_gate.go`, `internal/application/repository/lifecyclegate.go`).

It blocks a move into `done` (and into `released` when it skips `done`, but not the
ordinary `done → released` promotion) unless the task visited every stage
`Workflow.ReviewChain()` requires (built from each stage's `review_chain_stage`
behaviour) — `code_review`/`in_qa`/`pm_uat` for `task`/`bug`, `analiz_review` for
`analiz` — with none of those stages' latest visit rejected. Evidence is the
`task_column_spans` ledger, not the current column, so rework through `need_revision` is
not punished; a stage whose column is absent from the board is skipped, since a
customized board cannot route through a column it does not have.

The gate is always on — there is no per-repository opt-out any more
(`repositories.require_review_chain` was dropped in migration 158). It fails closed on an
unreadable ledger: a check that passes when its evidence cannot be read is not a check.

Release itself is no longer a column-transition gate at all: `repositories.require_release_deploy`
and the `require_release_deploy` stage behaviour were dropped by migration 158, and migration 159
replaced what used to fill that gap. A task now reaches `released` only through a release's own
verdict (`finish_release`), or through a `none`-mode merge that IS the release — never through a
bare, ungated column move. See "Releases (migrations 159–161)" below.

## Merging the task's pull request (migration 104)

`done` used to be the end of the board's involvement while the code was still on a branch:
task PRs were drafts, nothing un-drafted them, and GitHub refuses to merge a draft — so a
task could pass every gate and reach `released` unmerged.

**Task PRs open ready for review.** `github.CreatePullRequest` sends `draft: false` and the
port method is `EnsurePullRequest`, so no caller can read "draft" and believe it. Nothing
in the codebase gated on draft state.

**The release engineer merges it, as a tool call, in `done`.** Not a server-side hook: a hook
would fire on a state change nobody was watching, at a moment nothing had re-checked the
build. `done` wakes the release engineer (`board.Dispatcher.doneMergeWake`, narrow enough
that the column stays terminal for everything else — this was QA's wake before migration 159
moved the subscription) and the agent calls `merge_task_pull_request`: squash-merge, then
delete the branch — refused unless the task is in `done`, its PR is open and unmerged, checks
are green, the review chain is satisfied (always, per the gate above), its before-deploy
steps are confirmed on an `on_merge` component (`release.Service.MergeGate`, migration 161 —
see "Releases" below), and the PR head is still `board_tasks.verified_sha`
(`domain.VerifiedCommitMatches`, the same comparison `releaseTargetGate` makes, asked of the
PR head). The verified SHA travels to GitHub as the merge's `sha` precondition, so a push
landing between gate and merge is a 409 rather than a silent merge of unreviewed code.

**Un-drafting needs GraphQL.** REST's "Update a pull request" documents five body
parameters and `draft` is not among them — a `PATCH` with `draft: false` reports success and
changes nothing, and the merge then fails with "Draft pull requests cannot be merged" for no
visible reason. `github.MarkPullRequestReady` uses the `markPullRequestReadyForReview`
mutation (keyed by the PR's `node_id`), which is what `gh pr ready` does. It survives as a
repair path for PRs opened before this change.

`board_tasks.merge_commit_sha` records the squash commit: the dispatcher's idempotency key
(an already-merged task stops waking the release engineer) and the commit a release deploys.
A successful merge opens (or joins) a release — `TaskPRMergeResult.Release` carries what —
covered next.

## Releases (migrations 159–161)

Merging used to be followed by guesswork: whether anything deployed was read off which
workflows and deploy targets happened to exist, a green deploy job was the whole of
"released", and QA — whose rules forbid touching production — was the agent asked to watch
it. A component now STATES how it ships; every merge opens a release that is deployed, soaked
and judged; and a dedicated **release-engineer** agent (role `release`) owns everything that
happens in `done` and `released`. QA was unsubscribed from both columns by migration 159; its
work now ends at its verdict in `in_qa`/`pm_uat`.

### Delivery profile

`project_components.delivery` is a `Fact[domain.ComponentDelivery]` like `name`/`role`/`stack`
— `mode` (`on_merge` | `dispatch` | `batch` | `none`), `executor` (`github_actions` | `vercel`
| `local` | `store`), `workflow`, `tag_pattern`, `local_command`, `verify` (`soak_minutes`,
`max_new_errors`, GET/HEAD-only `smoke` checks), `auto_rollback`. `domain.DeliveryConfirmed`
is what a release may act on without asking: an `Override` always confirms; a `Detected`
guess confirms only at `exact`/`high` confidence — a medium guess must not start dispatching
production deploys nobody asked for.

`projectmodel.detectDelivery` (`delivery_detect.go`) infers it from a component's own CI
checks and bound environments, in the priority order a human would read them: a `Mobile` fact
→ `batch`/`store` (medium); a production deploy check triggered by push to the default branch
→ `on_merge`/`github_actions` (high); else a dispatchable production deploy check →
`dispatch`/`github_actions` (medium); else a confirmed (or auto-confirmed) production
environment on Vercel → `on_merge`/`vercel` (high) — a frontend-role component also gets a
default `GET /` smoke check; else a tag-triggered release check → `batch`/`github_actions`,
`tag_pattern: "v{version}"` (medium); else `none` (high). `refreshDeliveryDetection` runs as
its own pass AFTER a scan's checks are reconciled and `cloud.Service.MatchScan` has written
any CONFIRMED environments — the Vercel rule needs a confirmed environment, which only exists
once matching has already returned — and `RefreshDelivery` re-runs it outside a scan whenever
a binding changes. A detection that newly crosses into exact/high fires the same hook a
human's confirm does: tasks that merged while the profile was unconfirmed, still sitting in
`done`, are opened.

A human confirms or edits the profile on the repository's Deploy tab (`DeliveryCard` /
`DeliveryEditDialog`); `PATCH /v1/components/:id` with `delivery` sets the `Override` (`null`
clears it back to the detected half). `projectmodel.Service.UpdateComponent` validates it
(`ComponentDelivery.Validate`) and, on a save that newly confirms it, calls the wired
`SetDeliveryConfirmedHook` — `release.Service.OpenPending` — synchronously.

### The release and its state machine

`domain.Release` (`releases` + `release_tasks`, migrations 159–160): one shipment of one
component — the merge commits it carries, how it deployed, what production looked like
afterward, and the verdict. `draft` (batch only, collecting) → `pending` → `deploying` →
`verifying` → `awaiting_verdict` → `released` | `rolled_back` | `failed` | `superseded` (+
`rolling_back` while a rollback's own deploy is watched). `Terminal()` =
released/rolled_back/superseded; `Open()` = pending/deploying/verifying/awaiting_verdict —
what a newer merge of the same component must TAKE OVER, not run beside; `Watched()` /
`Parks()` = deploying/verifying/rolling_back — what the sweeper advances and what parks a
card.

`release.Service.OpenForMerge` is called by `board.TaskPRService` right after a successful
merge (it never returns an error — a failure to open a release is reported in `Next`, with
the task left in `done`, because the merge already happened and cannot be undone here):

- No component resolvable (none recorded, more than one active component with no `.`-path
  fallback) → `Unconfirmed: true`, one system comment, no release row.
- Profile not `DeliveryConfirmed` → the same: `Unconfirmed: true`, one comment naming the
  component, no row.
- `none` → the task moves straight to `released` (`MoveReasonMergeReleasedNoDelivery`);
  `Released: true`.
- `batch` → joins the component's DRAFT release, creating one if none exists
  (`idx_releases_one_draft` makes a racing second `Create` fail; the loser re-reads the draft
  the winner just made and `AddTasks` instead). The task stays in `done`; nothing dispatches
  until a human cuts it.
- `on_merge` / `dispatch` → **take over** the component's currently open release, if any: mark
  it `superseded` (verdict `"superseded by <new version>"`), carry its tasks into the new one,
  and silently un-park (never wake) any card of the old release still parked on
  `release_watch` — the new release's card is the one that gets woken. Create the release at
  `Version = ShortSHA(mergeSHA)`: `on_merge` starts `deploying` immediately (the merge itself
  deploys); `dispatch` starts `pending` (the agent must call `deploy_release`).

`TaskPRMergeResult.Release` carries the `ReleaseOpening` (`mode`, `release_id`, `status`,
`unconfirmed`, `next`), so the merging agent knows its next step without another call.
`AutoReleased` / `repository.Service.AutoReleaseIfUndeployable` only fire on a build with no
`release.Service` wired at all — every normal install goes through `OpenForMerge` instead.

`OpenPending(repositoryID, componentID)` is the catch-up path a human's delivery confirm
triggers: every task of that component sitting in `done`, merged, and belonging to no release
yet is opened exactly as `OpenForMerge` would (a `dispatch` release opened this way has no
live agent run watching it, so it is explicitly woken).

### The sweeper

`SweepOnce` (default 30s, `Start`) lists every release in a `Watched()` status and advances
each with an optimistic `store.Update(r, expect=oldStatus)` — `ErrReleaseWrongStatus` means
someone else (another tick, an agent's `Finish`/`Rollback`) already moved it, and the release
is skipped rather than fought over.

**`deploying`.** Deploy state for `CommitSHA`: `github_actions` →
`deploywatch.Service.StatusForCommit` (the same commit-keyed signal `get_deploy_logs` reads a
job id from) watching `profile.Workflow`; `vercel` → if the component has a bound, confirmed production environment
with an account and resource, match `cloud.Deployments` by commit prefix (either direction,
≥7 chars) — `ready`→success, `error`/`canceled`→failure, else pending; no match or no
environment falls back to the commit-status signal (`vercel[bot]`'s status write). `pending`
past 60 minutes, `no_signal` past a 15-minute grace, or `unknown` past 60 minutes → `failed`.
`failure` → `failed` with the status detail, hand back immediately. `success` → the soak
begins: `DeployedAt = now`, `VerifyUntil = now + profile.Verify.SoakMinutes`, the verify
target resolved (below), every smoke check run once and one health sample taken; a smoke
failure on this FIRST round is an early stop straight to `awaiting_verdict` — otherwise →
`verifying`. Batch releases sweep their own executors here instead — `local` waits for
`LocalRun.FinishedAt`, `store` polls whether every started platform's build moved past its
baseline (120-minute timeout), `github_actions` shares the dispatch path above.

**Verify target.** The component's bound, confirmed production environment (`EnvironmentID`,
`BaseURL`, `HealthURL`); else the legacy `port.DeployTargetStore` target for env `prod`. Gaps
are recorded on `Checks.Notes` ("no health URL — health not probed", "no bound environment —
runtime errors not read", "no base URL — relative smoke paths skipped") rather than a probe
that silently checked nothing.

**`verifying`.** Each tick: one health sample (two consecutive failures is an early stop);
runtime errors read at most every 2 minutes and once at window end
(`len(new-error-groups) > profile.Verify.MaxNewErrors` is an early stop); when
`now >= VerifyUntil`, one final smoke round runs and the release moves to `awaiting_verdict`
regardless of the early-stop flag. Stored `Health`/`Smoke` are capped at the last 60/40
samples.

**Hand-back.** `handBack` claims the FIRST parked card of the release's tasks
(`TakeBlockedResourceTask(release_watch, taskID)`) and wakes it through
`board.Dispatcher.Dispatch` — payload `resumed_resource: release_watch`,
`release_status: <status>`, actor system, reason `MoveReasonResourceFree` — exactly like
`board/deploy_sweeper.go` wakes `deploy_watch`; every OTHER parked card of the same release is
claimed silently. If nothing was parked (the agent run that would have parked is gone — a cut
batch release, for instance, has never had one), the newest task of the release is woken
instead; it is still sitting in `done`. `Dispatcher.deployWatchWake` accepts
`resumed_resource == release_watch` alongside the legacy `deploy_watch`.

### The release engineer

Role `release` (migration 159), catalog `catalog/agents/release-engineer/`, subscribed to
`done` and `released`. Its tool policy (`catalog.releaseEngineerToolPolicy`) holds no
workspace writers and no commit tools: it ships and reverts through the release tools only,
never by editing. `done`'s `strip_writers` behaviour keeps `merge_task_pull_request` and the
`release_control` tools (`deploy_release`, `finish_release`, `rollback_release` —
`domain.ReleaseControlTools`, tested by `domain.IsReleaseControlTool`) even after every writer
is stripped; `released`'s `strip_writers` allows `release_control` only (no merge tool there).

Wake points:

- **Merge** — `done`'s `merge_pr_on_enter` behaviour (`doneMergeWake`, unchanged mechanism,
  now resolved to the release engineer's column subscription instead of QA's).
- **Sweeper hand-back** — `awaiting_verdict` or `failed`; the agent must read
  `query_runtime_logs`/`list_runtime_errors` since `deployed_at` wherever a runtime
  environment is bound before calling `finish_release`/`rollback_release` — a hard rule
  (`release-verify-before-finish`), not only a suggestion.
- **`OpenPending`** — a `dispatch` release opened by a human's delivery confirm, or a cut
  batch release (`Cut` calls `wakeNewestTask`), explicitly dispatches a run since there is no
  live one to hand back to.
- **A health incident inside `released`'s window** — see Attribution below.

### Verification

A release is never finished on a green deploy alone. `awaiting_verdict` evidence: the
release's own `Checks` (health samples, smoke results, new runtime error groups, `EarlyStop`,
`Notes`), plus `query_runtime_logs`/`list_runtime_errors` read fresh in THIS run wherever a
runtime environment is bound. A batch release with no bound environment (most desktop/mobile
components) has none of those to read — its evidence is the build/publish result
(`Deploy`/`LocalRun`/`StoreBuilds`) plus any smoke checks, and the agent must say so
explicitly in its note rather than silently skipping the step. `finish_release` and
`rollback_release` both require a non-empty `note` stating what was actually checked.

### Rollback

`Rollback` is allowed on `failed`, `awaiting_verdict`, or `released` within 24h of
`FinishedAt` AND still the component's newest released release (`LastReleased`) — otherwise
`ErrReleaseWrongStatus` naming why. `reason` must be `Valid()`; an agent's
`deploy_failed`/`verify_failed`/`health_incident` are accepted as stated (the evidence is on
the release); `manual` is human-only. `profile.AutoRollback == false` and the actor is an
agent → nothing executes: the proposal (reason, note, what would be reverted/redeployed) is
written as a comment on the newest task and `ErrRollbackNeedsHuman` is returned — a
SUCCESSFUL call from the tool's point of view (`{proposed: true}`), not an error to route
around.

**Provider rollback runs first, before the revert** (any mode except `batch`, and never for a
release that never deployed): if the component's bound production environment's provider
implements `port.CloudRollbacker` (`CanRollback`), the target deployment is the one whose
commit matches the previous released release's `CommitSHA` (prefix match), else the newest
READY deployment created before this release's own deploy started. `RollbackEnvironment`
success → `Mechanism = provider_rollback`, `ProviderDeploymentID` recorded, and production is
back on the earlier code in SECONDS rather than the minutes a redeploy or revert-push takes.
Any failure (`port.ErrCloudWriteDenied` — a Vercel token needs write scope, a Cloud Run
service account needs `run.services.update` — `port.ErrUnsupported`, no target found, or the
call itself erroring) is folded into `Rollback.Detail` and the mechanism below proceeds
regardless; a provider rollback attempt never fails the caller.

**The revert always runs too**, whether or not the provider rollback succeeded:
`RevertOnDefaultBranch` (`git.Client`) fetches, checks out a DETACHED worktree of
`origin/<default>` (the root checkout's working tree and index are never touched), runs
`git revert --no-edit --no-commit` on every task's merge commit newest-first, commits, and
pushes to the default branch — so the next release cannot ship the bad change again even
after an instant provider rollback. A conflict aborts the revert and names the conflicting
paths; a rejected push says the revert was not pushed and production is unchanged.
`RevertCommitOnDefaultBranch` (the old single-sha path, which used to `reset --hard` in the
root clone) now delegates to it.

**Then, per mode:** `dispatch` — if the provider rollback already succeeded, nothing more is
dispatched (production is already restored); otherwise the previous good release's commit is
redeployed (`LastReleased`, or the revert commit's own tag with no previous release).
`on_merge` — the revert push itself redeploys; `Mechanism = revert_push` unless the provider
path ran. `batch` — no redeploy is attempted AT ALL: a published desktop build or store build
cannot be unpublished by a revert, so the release goes straight to `rolled_back` and
`ManualSteps` leads with unpublishing or halting the artifact itself (the GitHub
Release/update feed for `github_actions`/`local`, the store rollout for `store`) before the
tasks' own rollback runbooks.

**The Vercel promote trap.** Sweeping a `rolling_back` release whose
`Mechanism == provider_rollback`: once `CurrentDeployment` confirms the earlier deployment is
live, a `dispatch` release is done (`rolled_back`); an `on_merge` release must ALSO wait for
the revert commit's own deployment to go READY and `PromoteDeployment` it — on Vercel,
`RollbackTo` pins production to one specific deployment and turns OFF automatic production
assignment, so without this step every later merge would build but never go live. A promote
failure fails the release with a reason that says production is SAFE (still on the earlier
deployment) but someone has to promote a deployment in the provider's console by hand.

`ManualSteps` is always `TaskRollbackRunbook` per task (`rollback_plan`/`before_deploy`/
`after_deploy`), prefixed for batch as above. Every rollback carries `Actor`, `StartedAt`,
`Reason`, `Note`, `Detail`.

### Batch releases (migration 160)

Desktop/mobile (and anything else a human wants to cut deliberately) collect merges into a
`draft` release instead of shipping on every merge. A human previews
(`GET /v1/releases/:id/cut-preview`) and cuts it (`POST .../cut`, `{confirm, version, notes}`):

- **Preview.** Previous version: the component's newest released release's `Version`, else
  the newest git tag matching the tag pattern's glob, else none. Suggested next version: patch
  bump when every carried task is `bug`, else minor; `0.1.0` with no previous; `""` (a human
  types it) when the previous version is not plain semver. Commit: the default branch's remote
  head, checked to actually contain every task's merge commit (`IsAncestor`) — a missing one
  names which task. Notes: generated markdown, `## <version>` then `### Features`/`### Fixes`
  by task type.
- **Cut** re-reads and freezes the component's CURRENT confirmed profile (a draft may have sat
  open under an older one), sets `Version`/`Tag`/`CommitSHA`/`Notes`/`CutAt`, moves to
  `pending`, stamps every carried task's before-deploy confirmation (cutting the release IS a
  human's confirmation — see below), and wakes the release engineer on the newest task.

Deploy per executor (`Deploy` on a `pending` batch release):

- **`github_actions`** — tags the cut commit; unlike `dispatch`, an "already exists" tag is a
  REAL failure (`ErrReleaseTagExists`) — a batch version is picked once and never silently
  re-used. The repository's own tag-triggered workflow builds and publishes; nothing is
  dispatched.
- **`local`** — `LocalCommand` (with `{version}` substituted) is argv-split (quotes respected,
  no shell syntax interpreted) and REJECTED outright if it contains `|`, `&&`, `;`, a backtick
  or `$(` — it runs via `adapter/localexec.Runner`, `exec.CommandContext` directly,
  `shell: false`, in a DETACHED worktree of the cut commit, env = the server's own +
  `RELEASE_VERSION`/`RELEASE_TAG`/`RELEASE_COMMIT`, logged to
  `<data dir>/releases/<release-id>.log`, 60-minute timeout. The runner's callback
  (`CompleteLocalRun`) records the exit code, log tail and a synthesized `Deploy` status; the
  sweeper (`sweepDeployingLocal`) reads `LocalRun.FinishedAt` and treats no report within 70
  minutes as the server having restarted mid-run, not a hang.
- **`store`** — starts a `storeops` release build on every platform with a linked, identified
  app, recording each one's baseline internal-channel build first; a platform whose start
  fails keeps its own error rather than aborting the others (all failed → `failed`). The
  sweeper polls each platform's track until its build number moves past the baseline
  (120-minute timeout).

A cut batch release still goes through `verifying`/`awaiting_verdict` exactly like an
on-merge one — most desktop/mobile components simply have no bound runtime environment, so
the soak window runs out with nothing to read but the build/publish evidence.

### Before/after-deploy runbook (migration 161)

A task's `before_deploy` text is work a HUMAN performs (a migration, a secret, a switch) — the
release engineer may not write to production. `board_tasks.before_deploy_confirmed_at`
records the human's confirmation
(`POST /v1/repositories/:id/tasks/:taskId/before-deploy/confirm`); any edit that changes
`before_deploy`'s text clears the confirmation.

- `on_merge` — `release.Service.MergeGate`, called from `taskpr_merge.go` right before
  anything touches GitHub, refuses the merge itself (the merge IS the deploy) while
  `task.BeforeDeployPending()`: one comment listing the steps, wrapped `ErrBeforeDeployPending`.
- `dispatch` — `Deploy`'s `beforeDeployGate` refuses while ANY task of the release has pending
  steps, commenting once (on the newest task) with the whole list.
- `batch` — `Cut` stamps every carried task's confirmation itself: cutting the release IS the
  human's confirmation, shown in `CutReleaseDialog` under "Before this ships".
- `none` — irrelevant; nothing deploys.
- Confirming, when the task sits in `done`, wakes the release engineer on it
  (`release.Service.WakeTask`, the same `release_watch` resumed payload `OpenPending` uses) so
  a merge/deploy that was only waiting on a human resumes immediately instead of on the next
  sweep.
- `Finish` posts one system comment per task carrying `after_deploy` text, once the tasks have
  already moved to `released`: "Released — do these after-deploy steps now: …".

### Attribution

`release.Service.AttributeRelease` (env `prod` only) finds the newest `released` release of
the repository whose `FinishedAt` (or `DeployedAt`) falls within `HealthWindow` (15m, mirrors
`deploywatch.DefaultHealthWindow`) before an incident's onset, and names its newest task — any
delivery mode, including a push-to-deploy `on_merge` component with no release-tool
involvement at all. The integrator wires a composite attributor: this release-keyed one
first, `deploywatch.AttributeRelease`'s older commit-keyed one as fallback (a repository with
no release history yet, or a release that predates migration 159's rollout). Either way the
woken run lands on `released`'s `columnInstruction` and calls `rollback_release` with
`health_incident` — there is no separate rollback code path any more.

### What is gone

`trigger_release`/`TriggerRelease`, `get_task_deploy_status`, `rollback_task_release`,
`deployops.Service.Rollback`/`RollbackForTask`, and the "deploy package"/release-train HTTP
endpoints (`repository/deploy_package.go`, `domain/deploy_package.go`) are removed — batch
releases replace the deploy package's job of shipping several tasks together. QA no longer
merges, watches or rolls back anything. `AutoReleaseIfUndeployable` is reachable only on a
build with no `release.Service` wired at all.

## Work order and the analysis reference (migration 106)

**`blocks` now blocks.** Its only reader was `repository.Service.validateMoveAllowed`, which
refuses a MOVE into `todo`/`in_progress` — catching a human dragging a card and nothing else,
while a task created straight into `todo` with an open blocker, a reconciler sweep, a sweeper
hand-back or an assignment event all reach the dispatcher without a move. The gate is now
`board.WorkOrder`, called from `Dispatcher.Dispatch` after the board event is written
(history stays complete) and before any agent is resolved, for `todo` and `in_progress` only
— the same pair the move guard uses, so a finished change in `code_review` is never stranded
behind a dependency it no longer has.

It is a **park**, not a refusal: a refused dispatch leaves the card looking unstarted with
nothing saying why. It is the fourth `domain.ResourceBlock` resource (`work_order`, beside
`mobile_device`, `llm_provider_code_quota`, `deploy_watch`) and the odd one out in what it waits
for — the other three are facts about the world outside the board. Everything else is
identical: `blocked_resource`, the `blocked` column with the reason, a comment naming every
blocker, and a sweeper.

`board.WorkOrderSweeper` (1 minute — the shortest of the four: one indexed query against the
same database) is shaped like `DeploySweeper`, since the park is per-TASK: list without
claiming, ask, take only the free ones. It asks the relation graph itself —
`ListBlockingSources` returns the UNFINISHED sources of a task's `blocks` rows, so an empty
answer *is* "everything it waited for is done". That one query also covers both ways a
blocker can stop existing: a deleted task takes its `task_relations` rows with it (ON DELETE
CASCADE, migration 022), and a blocker returning to `in_progress` reappears, so the park is a
standing question rather than a one-off verdict. An unreadable graph fails closed.

Direction is unchanged: a `blocks` row stores the BLOCKER as `source_task_id`, the opposite
arrow to `deploy_depends_on`, and flipping it would invert every stored row. No tool or
endpoint asks a caller to think in it — they take `blocked_by`. Cycles are refused where the
edge is written, with the chain in the message.

**`derived_from` is where the spec went.** An analiz run commits nothing; its spec and plan
are `task_documents` on the analiz task and nowhere else, so implementation tasks had no
route to their own specification. The relation is that route — source = implementation task,
target = analiz task, same direction as `deploy_depends_on` — and deliberately not an
ordering relation: `blocks` would re-gate a start a human already approved in
`analiz_review`, and `deploy_depends_on` would refuse to release the implementation until a
task that ships no code reached production.

`repository.Service.AnalysisReferences` resolves it in one call and `Runner.analysisContext`
renders the documents as a system message after the trigger and **before** the diff and PR
blocks: those say what has been done to the task, this says what the task is supposed to be.
Injected on every run, not only the first — a revision run fixes against the same spec and a
reviewer judges against it. The block names `list_task_documents` as the way to re-read a
truncated plan.

### Loop guards — the fifth resource, human_decision

`ResourceHumanDecision = "human_decision"` (`internal/domain/resource_block.go`) — no
sweeper claims it; only a human moving the card out of `blocked` clears it. Same park
mechanics as the other four: `BlockOnResource`, `blocked_origin_column`, a ParkJournal row.

| Guard | File | Called from | Parks when | Reason |
| --- | --- | --- | --- | --- |
| `PipelineBounceGuard` | `internal/application/board/pipeline_bounce_guard.go` | `PipelineRunner.finalize`, before `reportPipelineFailure` | failed pipeline for a head SHA that already has a prior genuine failed pipeline (same SHA, real provider verdict, created after the last human board event) | `pipeline_loop_parked` |
| `ReviewLoopGuard` | `internal/application/board/review_loop_guard.go` | `Dispatcher.Dispatch`, after the work-order gate | a `task.moved` into `need_revision` that is the `maxReviewLoopEntries`th (3) since the last human event (explicit `to_column` only; reconciler/sweeper synthetic moves ignored; 500-event window; fails open) | `review_loop_parked` |

Motivation: CI billing-blocked → identical red every cycle → 11 review cycles observed on
one task. `PipelineBounceGuard` posts its comment once, re-reading the task first — it never
overwrites another resource's park. Web: `blockedResource.human_decision` label and history
reasons exist in both locales (web repo).

## Claude Code executor and the quota park (migrations 101, 103)

The `claude_code` provider (`domain.LLMProviderClaudeCode`) adds a second run path beside
`agentLoop.RunTask`, chosen through `port.TaskExecutor` (one `Supports`, one `Execute` taking
a `domain.TaskExecution`). The runner branches on the PROVIDER, not on "is there an
executor":

```
domain.RequiresHostExecutor(agent.ProviderType)  →  executor (or a clear failure)
r.orchSvc != nil                                 →  RunSolo        (unchanged)
default                                          →  agentLoop.RunTask (unchanged)
```

Keying on the provider is what makes a host with no CLI say *"claude code binary not
available on this host…"* instead of falling through to a loop that would open an HTTP
connection to a provider with no endpoint. The executor is registered in `platform/runtime`
only when the binary resolves on PATH (`CLAUDE_CODE_BIN`).

**Nothing else about the run changes.** The workspace clone and `tt-<key>` checkout still
happen before; grounding checks, verify gate, commit/push, PR and column advance still happen
after. The executor reports its tool calls into `registry.ToolUsage` and its tokens into
`usage.TokenUsage` — the same accumulators the loop feeds — so the run row, KPIs and
grounding gates read a CLI-worked task like a loop-worked one. The CLI's native tool names
are mapped onto TaskTrooper equivalents for that ledger (`Read` → `read_file`, `Grep` →
`grep_code`, …); without it an analiz run would be rejected for never having read the
repository it read. `total_cost_usd` is logged and traced but deliberately **not** recorded
to `llm_usage`: those tokens were paid for by a flat-rate subscription.

### Chat on the same seam (migration 103)

Chat used to go straight to the agent loop whatever the provider was, so a `claude_code`
chat turn reached the OpenAI-compatible client and reported `unsupported protocol scheme ""`.
It now branches like the board's, on the provider, ahead of the orchestrator:

```
domain.RequiresHostExecutor(agent.ProviderType)  →  chatExecutor (or a clear failure)
useOrchestrator                                  →  RunSolo / RunMulti (unchanged)
default                                          →  agentLoop.Run / RunStream (unchanged)
```

`port.ChatExecutor` is a second interface beside `TaskExecutor` (the board has no use for
streaming, the session service none for `Execute`); `*claudecode.Executor` satisfies both and
`platform/runtime` hands **the same instance** to both callers, so a chat turn and a board
task share one usage-limit gate (a chat that succeeds clears it for the board; see the quota
park below). Chat never queues on the board's concurrency cap.

Three things make a chat different, all in `domain.ChatExecution`:

- **Continuity.** The first turn flattens the assembled history; every turn after runs
  `claude -p --resume <id> "<what the user typed>"` and sends nothing else — re-flattening a
  growing transcript would pay for the whole conversation every message *and* hand the model
  its own remembered context back as a fresh instruction. Migration 103 puts
  `sessions.cli_session_id` on the chat row (in the database, so a restart does not turn every
  open chat into a first turn), written after **every** turn including a failed one. A
  `--resume` the CLI refuses falls back to a fresh session built from the stored transcript.
- **Streaming.** Assistant text goes into the same `capture` callback `RunStream` feeds, so
  the SSE handler and every client are untouched. `streamingSink` fires `agent.SegmentBreak`
  when text is followed by a tool call — the same `reasoning_end` frame a streamed loop turn
  emits — or the browser would render the narration as the reply and then swap it for the
  shorter persisted text.
- **Tools.** A per-**turn** MCP token, minted after the concurrency slot and revoked when the
  turn ends, serving the chat's computed `workspacePolicy`. Per turn, not per conversation: a
  thread can stay open for days and a credential for the board tools must not be
  live while nobody is talking.

**Quota in a chat is not a park** — there is no card and no sweeper, only the person who
pressed enter. The `*domain.QuotaBlock` becomes a sentence for them
(`domain.NewQuotaNotice`, naming the reset in local time), riding the transcript under
`RateLimitNoticePrefix` and the SSE frame under `type: "rate_limited"`, reusing what a
provider rate limit already has.

### Every other agentic path: the router (`agent.Router`)

Two call sites consulting the seam left about twenty that did not — so a `claude_code` agent
could implement a task but not fix its own red build, settle its criteria, hand off a review
verdict, run a subtask, profile a repository or reflect on itself.

`agent.Router` implements `agent.Runner` (the loop's own three methods as an interface) and
every agentic consumer is handed it instead of the bare loop — `board.Runner`,
`orchestrator.Service`/`Executor`, `evolution.Service`:

```
provider is host-executed AND an executor is wired  →  executor.Execute
provider is host-executed AND none is wired         →  ErrHostExecutedProvider
otherwise                                           →  the HTTP loop, unchanged
```

The one thing a child process needs and the loop's signature does not carry is a directory,
read off the run context (`registry.EffectiveWorkspaceDir`) — the same value the loop's tools
are scoped to, so a CLI session and a loop run work in the same tree. **No workspace is a
refusal, not a guess**: running in the server's own directory would let the session edit the
tree it is hosted from. The single exception opts in explicitly
(`agent.WithScratchWorkspace`) and is self-reflection with web research, which touches no
repository and gets a temporary directory removed afterwards. `agent.WithCLILabel` names the
session in logs and at the MCP endpoint ("tt-7 verify-fix"); the loop ignores it.

The board runner keeps its own explicit `switch` and `port.TaskExecutor` field — its
host-executed case comes first, so the router's `default` only sees HTTP providers.

`RunStream` deliberately stays on the loop even for a host-executed provider (where the
loop's guard refuses it): a streamed CLI turn needs `port.ChatExecutor`, which the session
service consults directly. `Router.SupportsHostExecution` is what `catalog.Service` asks
before an agent may be **saved** onto a host-executed provider — one error at the moment of
choice replaces ten runtime failures that each describe their own symptom.

### The guard (and the fallback that used to be here)

`domain.ErrHostExecutedProvider` fails fast at `agent.Loop.run` / `RunStream`, before the
retry loop — a malformed URL looks transient to the retry classifier, which is why the
original error was said three times.

`llm.MultiProviderClient.Chat` / `ChatStream` is the last point the request is still a Go
value, and `guardHostExecuted` refuses **every** request naming a host-executed provider:

- **Agentic** (carries `Tools`) — a run that should have been dispatched by the router.
  `domain.ErrHostExecutedProvider`. Unguarded this is worse than a failure: with no entry in
  the client map, `resolve` falls through to `m.fallback` and the run would be *sent*, to
  another provider's endpoint on another provider's key.
- **Utility** (no tools; a schema extraction, summary, commit message, judge verdict) —
  `errHostExecutedUtility`, naming **which step** could not run (the schema name —
  `planner_output`, `golden_gate_verdict` — or the call site), **that the agent runs on
  Claude Code**, the **concrete reason** where there is one, and the **two fixes**: configure
  an HTTP provider for this agent, or turn the step off.

**This used to be a reroute, and removing it was deliberate.** A utility call was sent to the
active default HTTP provider with the model blanked. The reasoning — the CLI cannot
serve it anyway — was sound and the conclusion wrong: the default provider is one the operator
did not choose *for this agent*, so every reroute converted "this agent cannot serve this
step", which is true, specific and fixable, into a 404 from a provider nobody was thinking
about.

Both refusals wrap **`domain.ErrHostExecutedUnservable`**, and callers do two things with it:
**do not retry** (`llmretry.Classify` returns `Stop`, one check where the retry policy lives
rather than at twelve call sites each inside a loop built for a flaky endpoint) and **do not
degrade quietly** (a load-bearing step fails the run with the message attached; an optional one
degrades but logs *what* it skipped and *why*, with `permanent: true` separating "this agent
can never do this" from "the endpoint had a bad minute"). Logged at WARN with the provider,
model, named call and source location. The same sentence is what `session.runHostExecutedTurn`
returns when no `claude_code` executor is registered — a host with the binary missing from PATH.

### Which paths run on the CLI, and which need an HTTP provider

On a host **with** a runner, for an agent on `claude_code`:

| Path | Engine | Why |
| --- | --- | --- |
| board run (`board/runner.go`) | CLI | the original seam; explicit switch |
| build-gate fix round (`board/verify.go`) | CLI | router; runs in the task's own checkout |
| criteria sweep (`board/criteria_sweep.go`) | CLI | router |
| review verdict sweep + finalize (`board/review_sweep.go`) | CLI | router |
| orchestrator subtask (`orchestrator/executor.go`) | CLI | router; subtask workspace |
| agent reflection with `allow_web_research` (`evolution/reflect.go`) | CLI | router; scratch workspace |
| chat turn (`session/`) | CLI | `port.ChatExecutor`; resumes the CLI session |

And the steps needing an HTTP provider configured **for this agent** — previously rerouted to
the configured default, now refused, so "if refused" is what the operator sees:

| step (needs HTTP) | load-bearing? | if refused |
| --- | --- | --- |
| intake, planner (`orchestrator/{intake,planner}.go`) | **yes** | the run FAILS. `pipelineStepError` drops the misleading "after 3 attempts" for a permanent refusal |
| replanner (`orchestrator/replanner.go`) | no | repair loop abandoned; plan lands on `PlanStatusIncomplete`, skip logged at ERROR when permanent |
| verifier (`orchestrator/verifier.go`) | no (by design) | stays "inconclusive" — results already produced are never discarded — logged at ERROR **and** appended: *"⚠️ Verification did not run for this plan: …"*, because it will be skipped every run until a setting changes |
| synthesize (`orchestrator/service.go`) | no | the raw formatted task results stand; the error is now logged |
| golden suite (`evolution/golden.go`) | **yes** | abandoned at the first refusal (`goldenRun.Unservable`) rather than grinding to `Evaluated == 0` |
| golden gate judge (`evolution/golden.go`) | **yes** | the change set **REVERTS**. It used to be *kept* "on non-regression" — two rates of 0/0 satisfy `after >= before` |
| reflection without web research (`evolution/reflect.go`) | **yes** | propagated as `reflection llm call failed` |
| memory promotion / save classification (`evolution/promote.go`) | no | saved as a plain memory; the skip is now logged |
| commit message rewrite (`board/commitmsg.go`) | no | the commit lands with the task's own title and summary, logged with `permanent` |
| rolling summarize (`context/summarize.go`) | no | the loop drops the oldest messages instead of condensing, logged with `permanent` |
| loop wrap-up (`agent/loop.go`) | no | the run's own last message stands (unreachable for a CLI run) |

`appcontext.SummarizeRollingFor` takes a provider for this reason: model and provider are one
decision, and a summarize call naming the model alone was routed at the default provider, which
answers 400 to a name that means nothing to it.

**Billing.** Flat-rate CLI tokens go to `usage.TokenUsage` and never to `llm_usage`, so they
never touch the USD budget. The HTTP steps above are metered API calls and DO bill,
through the recording client. A `claude_code` run row can legitimately mix billed and unbilled
tokens: the work was free, the JSON-shaped bookkeeping was not.

**Follow-up steps resume the run's own session, not a fresh one.** The build-gate fix round,
the criteria sweep and the review verdict sweep/finalize can all run several times inside one
`Runner.execute`, and each used to open a brand-new `claude -p` with the ENTIRE flattened
history replayed — persona, project brief, memories, diff, the prior assistant turn — up to
six times on one task, on the person's own subscription quota. `Runner.execute` now shares a
`*agent.CLISession` holder across that whole call (`agent.ContextWithCLISession`); the router's
`execute` reads it, and when the holder already has an id AND the step's own history ends on a
fresh user instruction, it sends `claude -p --resume <id> "<prompt>"` with just that instruction
as `TaskExecution.Prompt` instead of the full history — the session already holds everything
before it, and Claude Code caches that prefix for an hour on a subscription, so a resume minutes
later is nearly free. A step with nothing new to say (its history does not end on a user
message) falls back to the ordinary fresh-history call. A `*domain.QuotaBlock` raised by one of
these follow-ups gets the same treatment as one from the main executor call: the run stamps its
usage so far and parks the task on the limit (see below) rather than failing it or handing on a
stale verdict, borrowing the run's own session id when that particular call's block carried none
of its own.

### The quota park

A CLI session stopped by the subscription's usage limit is a fact about a billing window, not
about the work. Failing the run would spend one of the task's three consecutive-failure lives
and throw away the session holding the half-finished change. So it is a **park**, modelled on
the device park, with one difference that shapes the rest:

| | device (`mobile_device`) | quota (`llm_provider_code_quota`) |
|---|---|---|
| carried as | `AgentResponse.ResourceBlock` (a tool said "not now") | `error` — `*domain.QuotaBlock` (the run produced nothing to carry it on) |
| released by | probing the hub: is a phone free | the clock: has the recorded reset passed |
| state lives in | `board_tasks.blocked_resource` | that, **plus** `task_agent_runs.quota_resume_at` / `cli_session_id` |
| sweeps on boot | no — an idle hub's lease is re-probed on the next sweep, not assumed free | yes — a recorded reset time survives a restart |

Migration 101 adds those two columns (partial index on `quota_resume_at`). They are on the row
rather than in memory because that is what makes the park survive a restart: `QuotaSweeper`
(1-minute) asks the database, and `BoardTaskStore.TakeQuotaResumable` does the due check and
the unpark in ONE statement against the task's *latest* parked run, so a task parked twice
cannot resume on the first park's expiry. One task per claim (`FOR UPDATE SKIP LOCKED`), but
the sweep loops until nothing is due or it hits `quotaSweepBatchCap` — unlike the single
phone there is no contention between two tasks whose reset has passed.

The park is a board move like any other: a `task.moved` event (`from_column` → `blocked`,
actor `system`, reason `quota_exhausted`) and the open column span closed through
`board.ParkJournal`. It does NOT go through `Dispatcher.Dispatch` — a blocked task is
dispatch-suspended anyway, dispatching would push a notification for a card that just
stopped, and the park happens inside the very run the dispatcher started.

The resume is a *continuation*: the parked run wrote its `cli_session_id`, the resuming run
(a new row) finds it in the task's previous runs and passes it as `ResumeSessionID`, and the
executor runs `claude -p --resume <id> <short continue prompt>` in the same workspace — no
persona, no project context, no re-stated task, all of which the session holds and would read
as a competing instruction. The id is only handed to the SAME agent whose park recorded it
(`latestCLISession`), or a column dispatching to several agents would start two
`claude --resume <same id>` processes in one workspace.

Two brakes, because a park costs the task nothing and a *repeating* park is therefore
invisible:

- the limit is only recognised on a session that FAILED (`session.failed`);
- five consecutive parks on one task (`maxConsecutiveQuotaParks`) turn the sixth into an
  ordinary failure, so a false positive surfaces within a day instead of cycling forever.
  `domain.QuotaParkWindow(consecutiveParks)` gives a caller that counts them an escalating
  fallback (30m → 1h → 2h → 4h → capped at 5h) for the case below with no reset time, so a
  string of guesses backs off toward the subscription's own window instead of re-hitting it
  every half hour.

**Detection is structured-first.** Claude Code 2.1.273 emits a `rate_limit_event` on the same
stream whenever the picture changes — `{status, resetsAt, rateLimitType, unifiedWindows:
{five_hour, seven_day}}` — and `quotaBlockFrom` (claudecode/quota.go) checks it before anything
else: a `status: "rejected"` event, or a result event's own `api_error_status: 429`, is the
CLI's own word for the limit, with `resetsAt` (falling back through the unified windows, then
to the default/escalated window) trusted over a guess from prose. The historical text match
(`usage limit reached` and the handful of other spellings the CLI has actually used) is still
the fallback for a build that reports neither — a successful run's answer can legitimately
quote the same phrase, which is why it is still gated on `session.failed` regardless of which
path found it.

**The account-wide gate.** One session hitting the limit means every *other* session on this
process's executor is spending against the same spent subscription, not a different fact worth
rediscovering by spawning anyway. `*claudecode.Executor` keeps an in-memory `quotaUntil` /
`quotaDetail`, armed (only ever extended, never shortened) whenever `finish` maps a session to
a `*domain.QuotaBlock`, and cleared the moment any session on the executor finishes without
error — a success is proof the limit lifted, and it is also what heals a gate armed on a false
positive. `Execute` checks the gate FIRST, before minting an MCP token or touching the
concurrency cap below, and returns a `QuotaBlock` synthesised on the spot (carrying the
caller's own `ResumeSessionID` through, so a re-parked task does not lose the session it would
resume) rather than spawning a CLI that would just report the same 429 again. The gate is
per-process, in memory, and lost on a restart — it is a fast-path short-circuit, not the
durable park; migration 101's board-row park above is what actually survives one.

**The concurrency cap.** With no cap, every board task whose column is free spawns its own
`claude` session at once, and a burst that all hit the shared usage limit mid-work all park
together — the opposite of a gate meant to slow the burn down. `Config.MaxConcurrentSessions`
(0 → `DefaultMaxConcurrentSessions` = 3, negative → unlimited) backs a buffered-channel
semaphore on the executor; `Execute` acquires a slot after the quota-gate check and before
`resolveMCP` (so a queued run never mints and holds a per-run token while it waits), honouring
context cancellation, and logs plus records a `claude_code_slot_wait` activity step when the
wait passes one second.

**Chat is exempt from both.** `ExecuteChat` never consults the gate and never takes a
concurrency slot: a chat turn has a person who just pressed enter, and making them wait behind
board runs — or bouncing them off a gate a board task armed — would turn an interactive reply
into a queue nobody asked to join. A chat turn's own session still reports the limit as
usual (see `QuotaNotice` above), and a chat turn that *succeeds* still clears the gate for
every other session on the executor, through the same `finish` wrapper the board path uses.

**The other three CLI executors have their own park, best-effort.** `cursor_agent`,
`opencode` and `antigravity` each carry a `quota.go` and the same `gateMu` /
`armQuotaGate` / `gatedQuotaBlock` shape as claudecode's, feeding the identical generic
`*domain.QuotaBlock` → board park machinery above — migration 101's columns and
`QuotaSweeper` do not know or care which CLI parked the row. What differs is confidence in
DETECTION: Claude Code's `rate_limit_event` is a documented, structured signal; the other
three have no published error schema, so their `quotaBlockFrom` matches the CLI's own wording
as reported against real installs (cursor-agent's "You've hit your usage limit", AGY's
"quota reached" with its own `Resets in <duration>` countdown, OpenCode's `error` event
relaying whichever upstream provider 429'd) rather than a vendor-confirmed contract. AGY's
reset countdown is parsed into `ResumeAt`; cursor-agent's calendar-day reset is used only
when it is still in the future; OpenCode's park always uses `DefaultQuotaParkWindow`, since
it proxies many providers and none of their reset formats are documented to survive
OpenCode's relay. `domain.QuotaBlock.Provider` records which of the four hit the limit, so
`UserMessage`/`QueuedMessage` name the right CLI instead of always saying "Claude Code".

**The tool ledger and activity trace, for all four.** `cursor`, `opencode` and `antigravity`
each now carry their own `trace.go`, the same shape as claudecode's: a `traceSink` that
writes every `OnToolUse`/`OnToolResult` into both `registry.ToolUsageFromContext(ctx)` (what
the grounding gates below read — `isUngroundedAnalysis`, `isUngroundedQA`) and the activity
trace the UI streams (`assistant_message`, `tool_call_start`, `tool_call_result`,
`iteration_start`), replacing what used to be a `noopSink` that discarded both. Before this,
a board run on any of the three non-Claude-Code providers recorded NOTHING into the ledger —
an analiz run that genuinely read the repository through the CLI's own native tools (not
TaskTrooper's MCP-served ones) still failed `isUngroundedAnalysis` and looped on
`ErrUngroundedAnalysis`, because the ledger looked untouched regardless of what happened.
MCP-served calls (`mcp__tasktrooper__*`) were never the problem — those already reach the
ledger through `mcpserver.Run.Ctx`, the SAME `*registry.ToolUsage` the board runner installed
via `registry.ContextWithToolUsage`, independent of which CLI is asking — `recordedElsewhere`
(a `strings.Contains(..., "tasktrooper")` guard, since none of the three document a fixed MCP
tool-naming prefix the way Claude Code's `mcp__<server>__<tool>` is documented) exists only to
avoid double-recording those. The gap was the CLI's OWN native tools (Read/Grep/Bash-alikes),
visible only in its own stdout stream, which nothing fed into the ledger. Each package's
`nativeToolNames` map — cursor's and antigravity's LOW confidence, opencode's higher — is
best-effort against public docs and reported issues, not a verified contract; see each file's
own comment for the confidence level and what a live session would need to confirm it.

### The tool endpoint (MCP)

A CLI session arrives with the CLI's own tools and no idea a board exists — so without
something more, a `claude_code` run cannot move its card, tick a criterion, comment on its PR
or search the semantic index, and its `domain.ToolPolicy` is a statement rather than a rule.
`internal/adapter/mcpserver` serves TaskTrooper's own tools over MCP streamable HTTP at
**`POST /mcp`** on the server's own port.

```
board run ── executor ── claude -p --mcp-config <per-run file> ─┐
                                                                │ Bearer <run token>
platform/runtime ── RunTokenRegistry ── adapter/mcpserver ◄──────┘
                                             │
                                             └─ registry.ExecuteWithPolicy(run ctx, call, run policy)
```

**Per-run tokens.** `RunTokenRegistry` (in-memory mutex map) mints 32 bytes of `crypto/rand`
per run and stores the runner's `runCtx` and its policy. The token is
written into a 0600 file, never onto a command line, which is world-readable in `ps` — and
revoked on every exit path: a finished session, a failure, or the quota park. In memory is
enough precisely because of that: a resumed park is a new run row with a fresh token, and a
restart is the strongest revocation there is. An unknown, revoked or expired token gets `401`
with a JSON-RPC error body.

A cancelled `Run.Ctx` deliberately does **not** invalidate the token: the call must fail as a
cancelled call, not as an auth failure.

Storing the run's *context* is what makes a CLI session's tool call indistinguishable from a
loop run's downstream: audit rows, the session action ledger, the tool-usage counters the
grounding gates read and the activity recorder all take attribution from it, and it carries
the run's cancellation, so a stopped run's in-flight tool call dies with it.

**What is served.** `DefinitionsForPolicy(run policy)` minus two groups:

| Dropped | Why |
|---|---|
| `run_terminal`, `read_file`, `write_file`, `edit_file`, `edit_lines`, `delete_file`, `move_file`, `grep_code`, `get_repo_tree` | The CLI's Bash/Read/Write/Edit/Grep/Glob are better at exactly these and are what the model was trained against. Two tools for one job is the classic way to make a model pick the worse one |
| `ask_user` | It does not return a result — it parks the run on a human answer, and a live session would sit on an open call while the run around it was parked. Clarification for `claude_code` runs needs a suspendable session first |

`codebase_search`, `get_symbol_skeleton` and `expand_symbol_context` deliberately **stay**:
they are backed by the semantic index and the CLI has no equivalent. Every board and domain
tool stays too. The same predicate gates `tools/call` — filtering an advertised list is not
access control.

**Mounted without the auth middlewares.** `/mcp` is neither `/v1` nor `/admin` and
`isPublicPath` names it explicitly: the caller is a CLI session holding *none* of this
server's credentials, and it authenticates with the per-run bearer token alone.
**There is no signed identity header here to read** — the token alone carries the run's
context (`Run.Ctx`, from the board runner's `runCtx`) and policy. No valid token ⇒ `401`;
never a default. `SetLoopbackOnly(true)` on this product: the listener already binds
`127.0.0.1`, and the only legitimate caller is a `claude` child process on this same machine.

`c.IP()` is the peer's real address (the fiber app is built with no `ProxyHeader`).

The session's own tool calls are recorded **once**: a `mcp__tasktrooper__*` call is counted
and traced by the registry when this endpoint executes it, so the executor's stream reader
skips those names (`claudecode/trace.go`).

**The session is pinned to this endpoint and told what it holds.** Two CLI behaviours made a
served tool as good as absent, handled where the session is spawned
(`claudecode/claudecode.go`, `claudecode/mcp.go`):

- `--strict-mcp-config` accompanies `--mcp-config`, and `--setting-sources` is `project,local`
  (config: `claude_code.setting_sources`). Otherwise the session inherits the operator's
  personal MCP servers, hooks and plugins: their hundreds of tools push the CLI past the
  threshold where it hides schemas behind `ToolSearch`, and the run spends turns *searching*
  for tools it already has.
- The system prompt carries a **tool manifest** — the exact `mcp__tasktrooper__<name>` strings
  this policy is served, from the same `DefinitionsForPolicy` call `tools/list` uses
  (`mcpserver.ServedToolNames`). Every other prompt here names tools as the registry does
  (`set_criterion_completed`), which is not a name any session can call. A resumed session is
  skipped: it already holds the definitions.
- `initGuard` fails the run when `system/init` lists MCP servers and `tasktrooper` is **not**
  among them. Absence only: `pending` is a handshake still in flight. Left running, such a
  session works on native tools, returns a plausible summary, is refused by the criteria gate
  and is dispatched again — silently, on the subscription. The init event is logged once per
  session (servers, statuses, native tool count).

**Hand-rolled, not the go-sdk.** The repo uses `modelcontextprotocol/go-sdk` as a *client*
(`adapter/mcp`), but not here: the HTTP surface is Fiber/fasthttp (the SDK's handler is an
`http.Handler`, so it would arrive through an adaptor that buffers the response, paying for
SSE and session machinery only to switch both off), the tool surface is per-run and would
need a whole `*mcp.Server` per bearer token, `Server.AddTool` *panics* on a non-object schema
and ours come from ~60 independent executors, and its schema validation sits between the
client and executors that already validate their arguments. What is left is one client, one
transport and five methods — `initialize`, `notifications/initialized`, `tools/list`,
`tools/call`, `ping` — answered as plain `application/json`. No SSE and no resumability
because there is nothing to push. `GET`/`DELETE` answer `405` rather than falling through to
the SPA, which would hand a protocol client an HTML page.

A tool failure comes back as `isError: true` with the error text, **not** as a JSON-RPC error:
the model picked the arguments, so the model has to read what was wrong with them. Results
carrying images (`mobile_screenshot`, `browser_screenshot`) map onto MCP `image` content next
to their text.

## Local simulators and emulators (migration 102)

The `mobile_*` tools were written against a physical Android phone reached through a bridge
sidecar (`adapter/deviceagent`, `MOBILE_BRIDGE_URL`) driving a remote Appium hub; iOS was
refused by name, because XCUITest needs a macOS host with Xcode. The same binary also runs
directly on this machine, where two more devices exist that the bridge cannot reach — so a
registration gained a **kind**, and the kind is the only thing that changes:

| | `remote_adb` (default) | `ios_simulator` | `android_emulator` |
|---|---|---|---|
| attached by | the bridge sidecar, over HTTP | `simctl boot` + `bootstatus -b` | `emulator -avd … -no-window -no-audio`, then `wait-for-device` + `sys.boot_completed` |
| `device_addr` | the phone's tailnet host:port | the simulator UDID | the AVD name |
| `device_udid` | the loopback port the bridge allocated | the same simulator UDID | the adb serial it booted on (`emulator-5554`) |
| detached by | `adb disconnect` via the bridge | `simctl shutdown` | `adb -s … emu kill` |
| Appium caps | `Android` / `UiAutomator2` | `iOS` / `XCUITest`, `appium:bundleId` | identical to `remote_adb` |
| platform version | `MOBILE_PLATFORM_VERSION` or the row | derived from the simctl runtime | as `remote_adb` |

**Everything else is deliberately identical.** The eleven tools, the per-device lease, the
`domain.ResourceBlock` park and `DeviceSweeper`, the deploy-target app guard, the QA grounding
gates and the role prompts are untouched — an agent cannot tell which kind it is driving,
which is the point of doing this as a kind rather than a second tool surface. Inside the tool
layer the difference is confined to `adapter/tools/mobile.capabilitiesFor`; `Pool` needed no
change, because it keys sessions on the UDID.

**`adapter/localdevice`** is the local twin of `deviceagent`: same four questions (what is
there, attach it, is it up, detach it), answered by running a binary on this machine. It
resolves `xcrun`, `adb` and `emulator` once at boot — PATH first, then
`ANDROID_HOME`/`ANDROID_SDK_ROOT` and the standard Android Studio location — and a host that
resolves none reports no capabilities, exactly as a Linux node behaves. `xcrun` is
additionally gated on `GOOS == darwin`, so a shim cannot make a Linux box claim simulators.

**There is no shell in that package.** Every command is `exec.Command` with a validated
argument vector, validated (`ValidateSimulatorUDID`, `ValidateAVDName`,
`ValidateEmulatorSerial`) *before* the exec — the arguments arrive over the settings API, so
interpolation into `sh -c` would be remote command execution on the operator's laptop. AVD
names may not begin with `-` or `.` (`emulator -avd --help` is argument injection even with no
shell, because the emulator parses its own argv), and an emulator serial must match
`emulator-<port>`, so nothing here can `emu kill` a plugged-in phone.

**The iOS refusal is lifted per host, not removed.** `domain.ValidPlatform` still refuses iOS
for `remote_adb`; the simulator path is gated on `LocalHost.SupportsIOSSimulators`, a question
about the machine rather than the requested value.

**`GET /v1/settings/mobile-devices/local-catalog`** reports what the host could drive
(`{"ios":[{udid,name,runtime}],"android":[avd names]}`), because a local device cannot be
typed in: a simulator is a UUID and an AVD an exact SDK name, and both fail late and
unhelpfully when mistyped. Registration validates against the same source. Two empty arrays on
a host with neither, as a `200` rather than a `404`.

Migration 102 adds `mobile_devices.kind` with `DEFAULT 'remote_adb'` — the default *is* the
correct backfill, since a bridge phone is the only kind that could have been registered
before. A blank kind normalises through `domain.MobileDevice.DeviceKind()`. Tool registration
is still driven by the *effective* devices: `MOBILE_BRIDGE_URL` being unset (the normal case
on a Mac) disables nothing, because the bridge only ever served one of the three kinds.

Mobile device access (`mobile.*`) runs entirely through `adapter/localdevice` on this
machine — see "Local simulators and emulators" above for the full `remote_adb` /
`ios_simulator` / `android_emulator` breakdown. There is no second, remote path: `mobile.Pool`
talks to one Appium hub on this host, and a park (`ResourceBlock{mobile_device}`) is released
by `DeviceSweeper` probing that same hub.

## Single install, no isolation (migration 133)

Migration 133 dropped the multi-tenant schema this code once shared with a hosted version of
the product: row-level security, every `tenant_id` column and tenant-scoped key, and the
`tenants` / `tenant_members` tables. What replaced it:

| Piece | Where | Rule |
|---|---|---|
| auth | `adapter/http/handler.go` (`authMiddleware`) | bearer `SERVER_API_KEY` check only — no identity is stamped on the request afterward |
| db access | `adapter/store/postgres/db.go` | every statement goes straight to the pool; no session-scoped setting |
| board seed | `application/bootseed` | seeds the default board once per install, gated by `install_state.board_seeded_at` so a board the user has since edited is never reseeded; `bootSeedMiddleware` retries a failed seed on every non-public request until it succeeds |
| boot steps | `application/bootseed` (`Step`) | seed mcp servers, llm providers, mobile devices, and kick off the skill-embedding backfill; run once per process, idempotent |
| background loops | `platform/runtime` `Run` | the root context carries no identity; every loop calls its tick directly on it |

There is no role system: every request that clears `authMiddleware` can do everything the
API exposes.

Filesystem paths are still derived from the configured workspace root rather than trusted
from a caller — see `.ai/workspace.md` for the full path table.

## Embeddings

`llm.MultiProviderClient` resolves the embedding provider per call (`llm.ProviderResolver.ResolveProviders(ctx)`),
not from a cached field, so a settings change is picked up by the next call rather than requiring
a restart. On this product the resolved provider is the bundled embedder the desktop shell
spawns (`EMBEDDINGS_BASE_URL`, OpenAI-compatible, `nomic-embed-text-v1.5`) — see
`docs/architecture.md`. `GET /v1/llm/embedding-status` reports readiness.

`workspace_indexes.embedding_model` / `embedding_dims` (migration 116) record what an index was
built with; `domain.EmbeddingProvenanceStale` is the read-time comparison against the currently
resolved embedding config, so a changed provider re-embeds an index whole rather than mixing
two coordinate systems into one `workspace_chunks` table. `SearchChunksByIndex` refuses a stale
index with `domain.ErrIndexEmbeddingStale` rather than returning a plausible-looking wrong
ranking.

## `POST /v1/agent-cli/claude/connect`

`agentcli.ProbeFunc` runs `claudecode.Probe`, which checks the `claude` binary and its signed-in
account on this host directly.

| Report | Result | HTTP |
|---|---|---|
| `claude` **and** `claude-account` `ok` | `Probe{BinaryPath, Version}` | 200 |
| `claude` `missing`/`unusable` | `ErrAgentCLIBinaryMissing` + remediation and the command to run | 400 `agent_cli_binary_missing` |
| `claude-account` `missing`/`unusable` | `ErrAgentCLIUnauthenticated`, so signed-out ≠ no-plan | 400 `agent_cli_unauthenticated` |
| either item absent | plain error naming a version-skew problem — never a sentinel | 500 |

The catalog snapshot for each agent-CLI flavor is written under `<workspace-root>/agent-cli/<flavor>`
and read straight off this host's disk.

## `toolchain.detect` — the repository's own pins, read where the files are

`toolchain.Default.Overlay(workDir)` resolves a repository's pinned toolchain versions against
the working copy and carries them on the context (`registry.ContextWithTaskEnv`), so every tool
that spawns a build/test process picks up the repository's own pin rather than whatever this
machine's PATH happens to resolve.
