# server

The TaskTrooper backend: HTTP API + agent runtime, one Go binary. **Local is the only mode** — one machine, one user, no control plane. The
desktop app spawns this binary; `make dev` runs it in a terminal.

Detailed docs:

- [Coding Standards](.ai/coding-standards.md)
- [Testing Standards](.ai/testing-standards.md)
- [Architecture Overview](.ai/architecture.md)
- [API Specification](.ai/api-spec.md)
- [Tool Reference](.ai/tool-reference.md)
- [Orchestration Agents](.ai/orchestration-agents.md)
- [Configuration Reference](.ai/config-reference.md)
- [Workspace](.ai/workspace.md)
- [Repositories & Projects](.ai/projects.md)
- [Executor](.ai/executor.md) — `cmd/executor`, the headless mode a runner drives: its stdin/stdout contract and `/exec/*` API

This file and `README.md` are the current word on how the process is
configured and started; the docs above cover the domain model, the tool
surface, the API and the orchestration agents.

## Code comments

Do not add code comments unless truly necessary — a non-obvious invariant, a
workaround, or a WHY that isn't clear from the code itself. Never explain WHAT
the code does; well-named identifiers already do that.

## Layout

```
cmd/agent-server/   server binary (env → internal/platform/runtime.Run)
cmd/executor/       headless executor (stdin config → internal/platform/executor.Start)
cmd/skillvectors/   embeds catalog skills into catalog/skills.vectors.json (build time)
cmd/migrate/        migration runner; migrations/ must stay in this module
                    because the embed path is relative to it
internal/
  domain/           entities and value objects
  port/             interfaces (LLMClient, ToolExecutor, ToolRegistry, …)
  application/      agent loop, config loading, registry, board/orchestration
  adapter/          http, llm, agentcli/*, tools/*, mcp, mcpserver, github,
                    postgres — anything external (agentcli = agent CLIs run as
                    a local process, e.g. Claude Code; mcp = MCP client,
                    mcpserver = the /mcp endpoint those CLIs call back on)
  platform/         process plumbing: runtime wiring, embeddedpg, secrets
migrations/         SQL migrations (embedded)
resources/          config.yml, openapi.yaml (embedded into the binary)
```

Hexagonal rules apply: `domain` and `application` must not import adapters;
new external integrations go behind a `port` interface with an adapter
implementing it.

## The local-mode invariants

| Invariant | Where |
|---|---|
| Auth is the `SERVER_API_KEY` bearer token | `adapter/http/handler.go` (`authMiddleware`) |
| The listener binds `127.0.0.1` only | `platform/runtime/runtime.go` |
| Exactly one stdout line: `LISTENING http://127.0.0.1:<port>`; logs go to stderr | `platform/runtime/runtime.go` (`ConfigureLogger`) |
| Empty `DATABASE_URL` ⇒ embedded Postgres 17 | `platform/embeddedpg` |

Migration 133 dropped the multi-tenant schema (row-level security, every
`tenant_id`, `tenants`). It cannot be undone, so the embedded cluster is copied
to `$DATA_DIR/postgres-backup-pre-133` before its first start on that build.

## Issue sync

`internal/application/issuesync` imports GitHub and Jira issues (by hand, from a
two-minute poller for labelled issues, and from GitHub's `issues` webhook when it
can reach this server), has the product manager turn each into board tasks, and
writes back to the issue. The conversion is a normal chat turn with the agent
holding `product_manager` (`session.Service.SendMessage`, so agent CLIs run it on
this machine); tasks it opens with `create_board_task` are linked to the issue
through `repository.Service.SetTaskCreatedObserver`, keyed by the chat session on
the tool call's context. Its prompt is `catalog/system/prompts/issuesync/convert_request.md`.
Migration 183 holds `issue_imports` (one per issue) and `issue_links` (one per
task). Details: [API Specification](.ai/api-spec.md#issue-sync-migration-183).

## Build

`CGO_ENABLED=1` — `smacker/go-tree-sitter` is a cgo package.

```sh
go build ./... && go vet ./... && go test ./...
```

Tests that need Postgres start their own embedded instance
(`platform/database.StartEmbedded`); nothing external has to be running.
