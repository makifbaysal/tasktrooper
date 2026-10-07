# Workspace

The system is a single implicit workspace. The team layer was removed (migration 038). Board columns, membership, column subscriptions, and the task key prefix are global; every profile/agent works in the same board.

## Data Model

| Table | Purpose |
|-------|---------|
| `board_settings` | Single row (`id=1`): legacy `key_prefix`; task keys no longer use it (see below) |
| `board_task_counters` | One row per task type: `last_number`, monotonic — task numbers are never reused |
| `board_columns` | Global column slugs, labels, order, backlog flag |
| `board_members` | Catalog agents active on the board |
| `agent_column_subscriptions` | Which agents listen to which column slugs (with optional `task_type_filter`) |
| `repositories` | Code repos bound to the board (no `team_id`) |
| `board_tasks` | Board tasks; `task_number` unique per task type (key = `T-`/`B-`/`A-` + number), `board_column` slug validated against `board_columns` |
| `task_comments` | User and agent comments on tasks |
| `board_events` | Audit trail for task lifecycle events |
| `task_agent_runs` | Headless agent executions triggered by board events |
| `sessions.agent_id` | Direct agent chats scoped by `agent_id` (optional `repository_id`) |
| `session_actions` | Ledger of board records a chat's agents created or changed (063) |

## Agent Chats

User-initiated chats are scoped by `agent_id` (optionally `repository_id` so board tools work in context). `POST /v1/sessions` with `agent_id`; `GET /v1/sessions?agent_id=`. When `agent_id` is set, `session.Service` loads catalog agent prompt, skills, rules, and tool policy (orchestration disabled). The agent must be a board member.

### Session action ledger

An agent loop's tool calls and results live only inside that loop — only the final assistant text is persisted. A later turn therefore had no id for the task it had just created, and re-created it instead of moving it.

`registry.ActionRecordingRegistry` (wrapping the audited registry, so chat, orchestrated subtasks and board runs are all covered) writes every state-changing board tool call into `session_actions`: tool, verb, entity kind, entity id/key, title, column, priority, full result payload. Read-only tools are not recorded. Classification and the prompt rendering live in `domain.ClassifyBoardAction` / `domain.SessionActionDigest`.

Two consumers:

- `session.Service.buildMessageHistory` injects the digest as a system message directly ahead of the newest user turn, so the agent addresses existing records by id. `isolatedSubtaskHistory` preserves this one system message (`domain.IsSessionActionDigest`) while still stripping session-level prompts.
- `GET /v1/sessions/:id` returns an `actions` array; the web chat renders each as a `SessionActionCard` under the reply that produced it, opening the board's `TaskDetailDrawer` via `ChatTaskDrawer`.

Persisted clarifications are also replayed into history (`prompt.ClarificationHistoryNote`) — the questions previously lived only in a JSONB column the model never saw, so it read the user's answer without knowing what had been asked.

## Dispatch Rules

| Event | Agents triggered |
|-------|------------------|
| `task.created` in column X | Subscribers of column X |
| `task.moved` to column X | Subscribers of column X |
| `task.commented` + assignee + `need_revision` | Assignee only |
| `task.commented` (other) | Assignee if set, else column subscribers |
| `task.assigned` | New assignee |

Dispatcher writes `board_events`, creates `task_agent_runs`, and enqueues `board.Runner` jobs. Column subscriptions are resolved from `agent_column_subscriptions` (global) via `BoardConfigStore.AgentsForColumn`.

## On-disk layout under `storage.sessions.workspace_root`

Every path is a direct child of the configured root (`workspace.ResolveRoot`);
there is no per-tenant subtree.

| Path | What |
|---|---|
| `<root>/repos/<name>` | index mirror clone, one per repository (`workspace.RepoDir`) |
| `<root>/task-<task-id>` | a task's own checkout (`workspace.TaskDir`) |
| `<root>/<session-id>` | an unbound chat's scratch dir (`workspace.SessionDir`) |
| `<root>/agents/<agent-id>` | an agent chat's persistent scratch dir (`workspace.AgentDir`) |
| `<root>/agent-cli/<flavor>` | agent-CLI catalog snapshot |

`repos/<name>` is the only non-uuid component directly under the root, which is
how anything walking the tree tells a mirror clone from a task/session/agent
directory.

**Legacy layout.** An install last started before migration 133 may still have
its data under `<root>/tenants/<local-tenant-id>/…`; `workspace.FlattenLegacyLayout`
moves it up into the layout above once at boot.

## Headless Agent Runs

`board.Runner` loads catalog agent config, creates an ephemeral session for activity tracking, injects repository workspace and index context, and runs `agent.Loop` with a board trigger message. Agents use `claim_project_task`, `add_task_comment`, and existing board tools.

### Working copy is a cache, not the source of truth

`repositories.remote_url` (064) records the git origin. The clone under `<workspace_root>/repos/<name>` can disappear (a wiped data directory) and is restored from that URL.

Before any agent starts, `board.Runner.ensureWorkingCopy` requires a real git working copy at the repo root **that is this repository**: intact and its origin matches `remote_url` → use it; intact but a different origin → **fail the run** (adopting a checkout is only safe if it is the right one); missing/empty → clone from `remote_url`; no `remote_url`, or the root exists with non-git contents → **fail the run** with an operator-facing error. It previously called `os.MkdirAll` here, so a stale path silently became an empty directory, `HasGit` went false, the clone/branch gate below was skipped, and the agent ran in an empty tree and asked the human for the repository path.

`remote_url` is written on import/open and backfilled from `git remote get-url origin` while a clone is still present (`repository.Service.syncRemoteURL`) — rows predating the column carry `''`.

### `repositories.root_path` is host-absolute

`root_path`, `sessions.workspace_dir`, `sessions.project_root` and
`workspace_indexes.root_path` are absolute paths on this machine. `root_path`
is written once by `repository.Service.Open` (validated by
`workspace.ValidateProjectRoot`) and never rewritten; the session/index
equivalents are written by `ensureSessionWorkspace` and `indexer.Service`
(`UpdateIndexTree`) from this host's own workspace root.

Still host-absolute and read by nothing but the API: `task_agent_runs.workspace_path`
(`board.Runner`), returned on `/runs` for display only.

`postgres.RepositoryStore`/`SessionStore`/`IndexStore` still carry a
`localizeRootPath` / `hostRoots` path-reanchoring step (`SetHostRoots`,
`GetByRootPath`'s directory-name fallback) from when one database could
be read by two different hosts sharing the same rows. On this single-machine
product every stored path already belongs to the one host that wrote it, so
the reanchoring is a no-op.

### Per-task git workspace

When the repo root is a git repository, the runner clones it into `<workspace_root>/task-<taskID>/`, checks out `feature/<task-key>` (e.g. `feature/t-12`, `feature/b-3`), and uses that clone as the run's effective workspace (`task_agent_runs.workspace_path`). On success it commits and pushes the branch. A re-run reuses the existing workspace/branch. On workspace setup failure, falls back to the repo root.

`EnsureTaskWorkspace` fetches the project root and then the fresh clone before branching, and cuts the task branch from `origin/<default branch>` — otherwise every task branched off whatever stale state the shared root happened to hold. A fetch failure is not fatal (offline runs still work) but is logged and drops the branch back to the cloned HEAD.

When a task moves to `pm_uat` or `done`, `repository.Service` creates a draft PR via `gh pr create --fill --draft` (idempotent) and appends the PR URL as a system comment.

### Clarification notifications

If a board run's response contains a clarification request (`ask_user`), the runner opens a new agent chat session (agent + repository bound) with the clarification stored on the assistant message, and sends a macOS notification via `osascript`.

The product manager (`product_manager` role) is never a task's assignee: it works `pm_uat` through its column subscription. `repository.Service` refuses it on create, update and claim (`domain.ErrAssigneeNotAssignable`, HTTP code `assignee_not_assignable`), and a system-opened task whose role purpose resolves to it is left unassigned.

## API

| Method | Path |
|--------|------|
| GET | `/v1/board/config` (settings + columns + members + subscriptions) |
| GET/PUT | `/v1/board/settings` (key_prefix) |
| GET/PUT | `/v1/board/columns` |
| GET/PUT | `/v1/board/members` |
| GET/PUT | `/v1/board/subscriptions` |
| GET | `/v1/activity` |
| GET | `/v1/tasks` (board tasks — released older than `domain.ReleasedBoardWindow` = 7d excluded) |
| GET | `/v1/tasks/released?q=&limit=` (released archive: all of them, newest first, search over key/title/description) |
| GET | `/v1/tasks/lookup?key=` |
| GET/POST | `/v1/repositories`, `/v1/repositories/open` |
| GET/POST | `/v1/projects` (initiative projects) |
| GET/PATCH/DELETE | `/v1/projects/:projectId` |

Task comments and runs: `/v1/repositories/:id/tasks/:taskId/comments`, `/runs`.

## Config

```yaml
board:
  dispatch_enabled: true
```

## Agent performance (global)

Migration 038 reverted team-scoped scoring: `agent_performance_scores` is UNIQUE(agent_id); `agent_score_events`, `agent_memories`, `agent_reflections`, `agent_evolution_events`, `agent_kpi_results` are agent-global (no `team_id`). Prometheus: `bridge_agent_score{agent_id}`.

UI: `/agents/:agentId/performance` shows score + trend sparkline, KPI attainment cards + composite, evolution timeline (impact badges, before/after diff), reflections, memories (deletable), score event table, and an "Analyze now" button.

API: `GET /v1/agents/:agentId/performance|score-events|evolution-events|reflections|memories`, `POST .../reflect` (202; 409 while in flight), `DELETE .../memories/:memoryId`.

Memory listings (`/v1/agents/:agentId/memories`, `/v1/memories/shared`) take `repository_id` + `repo_scope` and creates take `repository_id` — see [Memory scopes](orchestration-agents.md#memory-scopes-migration-065). Memories are agent-global only in the sense that there is no team layer; the repository dimension is migration 065.

## Workspace lifecycle

Everything above this section is the whole lifecycle: `ensureWorkingCopy` clones
`remote_url` into `root_path` on this host, `EnsureTaskWorkspace` checks a task out
under `<workspace_root>/task-<id>`, and the agent catalog is materialised into that
checkout. `repository.Service.EnsureIndexMirror` restores `root_path` from
`remote_url` before an index pass the same way.

## What the code index covers

The indexer walks the repository root honouring `.gitignore` and a repository-root
`.tasktrooperignore` (same pattern syntax; always applied), and keeps only files with an
indexable extension. Generated code and mocks (mock/generated directories, `mock_*`,
`*.pb.go`, `*_gen.go`, `// Code generated ... DO NOT EDIT.` and `@generated` headers, ...)
are left out unless `indexer.index_generated` is `true`. Files that leave the list drop
their chunks on the next incremental pass.
