# server

The TaskTrooper backend: one Go binary that serves the HTTP API and runs the
agent loop — LLM call → tool execution → repeat — with built-in tools, MCP
servers, git workspaces, the QA/browser pipeline and the orchestration agents.

**Local is the only mode.** One machine, one user, no control plane. The
listener binds `127.0.0.1` only. With no `DATABASE_URL` it starts its own
Postgres.

The architecture is hexagonal: `internal/domain` and `internal/application`
hold the logic, `internal/port` the interfaces, `internal/adapter` everything
that touches the outside world, `internal/platform` the process-level plumbing.
See [.ai/architecture.md](.ai/architecture.md).

## Layout

| Path | What |
|---|---|
| `cmd/agent-server/` | the server binary (env → `runtime.Run`) |
| `cmd/migrate/` | applies `migrations/` out of band |
| `internal/` | domain / port / application / adapter / platform |
| `migrations/` | SQL migrations (embedded) |
| `resources/` | `config.yml`, `openapi.yaml` (embedded into the binary) |

## Run

```sh
go build ./...
go vet ./...
go test ./...

SERVER_API_KEY=local-dev-key MCP_SECRETS_KEY=$(openssl rand -base64 32) \
  go run ./cmd/agent-server
```

Or copy `.env.local.example` to `.env.local` and use `make dev` from the repo
root. Migrations run at boot; `go run ./cmd/migrate` applies them on their own.

Prerequisites: Go (version pinned in `go.mod`), `rg` (ripgrep) on `PATH` — the
`grep_code` tool shells out to it with no fallback. Browser tools want a
Chromium binary at `CHROME_BIN`; the `claude_code` provider wants the `claude`
CLI on `PATH`.

**`CGO_ENABLED=1`.** `smacker/go-tree-sitter` is a cgo package, so a
`CGO_ENABLED=0` build fails. The desktop app's `build-server.mjs` builds with
cgo on for both architectures.

## Environment

Required — the process refuses to boot without them:

| Var | Meaning |
|---|---|
| `SERVER_API_KEY` | the bearer token every request must carry |
| `MCP_SECRETS_KEY` | encrypts provider API keys, MCP secrets and store credentials at rest; must stay stable across runs |

Optional:

| Var | Default | Meaning |
|---|---|---|
| `DATABASE_URL` | — | External Postgres DSN. **Empty ⇒ embedded Postgres** under `$DATA_DIR/postgres`. |
| `DATA_DIR` | `./data` | Workspaces, RAG files, embedded Postgres data |
| `PORT` | `8085` | HTTP port; `0` picks a free one |
| `SHUTDOWN_ON_STDIN_CLOSE` | no | `1` makes stdin EOF start the same drain as SIGTERM. The desktop app sets it so the server stops cleanly on Windows and never outlives the app. |
| `EMBEDDED_POSTGRES_CACHE_DIR` | `$DATA_DIR/postgres-bin` | Where the Postgres binaries are downloaded and extracted (~30 MB, first start only) |
| `EMBEDDINGS_BASE_URL` | — | OpenAI-compatible host exposing `POST /v1/embeddings`. Set, an embedding provider pointing at it (`nomic-embed-text-v1.5`, 768 dims) is created at boot. Idempotent. |
| `CORS_ORIGINS` | `app://tasktrooper,http://localhost:3200,http://127.0.0.1:3200` | Origins allowed to call this server |
| `PUBLIC_BASE_URL` | `http://127.0.0.1:<port>` | The origin a Claude Code session calls TaskTrooper's tools back on |
| `CLAUDE_CODE_BIN` | `claude` | The Claude Code CLI |
| `CHROME_BIN` | — | Chromium for the `browser_*` tools |
| `MOBILE_APPIUM_HUB_URL` | — | The Appium hub the `mobile_*` tools drive. The desktop sets `http://127.0.0.1:4723` whenever Appium is installed. |
| `APPIUM_BIN` | — | The `appium` executable (on Windows its npm `.cmd` shim, run without `cmd.exe`). Set, and with `MOBILE_APPIUM_HUB_URL` plain `http` on loopback, the hub is this server's to run: the first mobile tool call that needs it starts `appium --address <host> --port <port> [--base-path <path>] --log-no-colors` in `$DATA_DIR/appium-hub` and waits up to 45 s for `GET /status`; it is stopped after 10 minutes with no tool call and no device lease open, and on shutdown. A hub already answering on the address is used as it is and never stopped. Unset (`make dev`), the hub is whoever's runs it. |
| `AGENT_CATALOG_REPO` | — | Where the six role agents sync from, at boot and on the `agent_catalog.interval`. A local directory is read in place; a git URL is fetched into `agent_catalog.cache_dir`. Set, catalogsync creates/merges the role agents by name; unset, no role agents are created. Dev defaults to `<repo>/catalog`; the desktop ships its own copy. See "Role agent catalog". |
| `CONFIG_PATH` | `resources/config.yml` | Config file; falls back to the copy compiled into the binary when the file is absent |
| `SHUTDOWN_GRACE` | `9m` | How long to keep working after SIGTERM before in-flight runs are cancelled |

`DATABASE_URL`, `SERVER_API_KEY` and `MCP_SECRETS_KEY` are dropped from the
process environment once read
([`internal/platform/runtime/envscrub.go`](internal/platform/runtime/envscrub.go))
so an agent's child process cannot reach them.

`resources/config.yml` substitutes further `${VAR}` values, all optional and
feature-scoped: the `MOBILE_*` Appium/device-agent settings, the agent-CLI
binaries (`ANTIGRAVITY_BIN`, `CURSOR_AGENT_BIN`, `OPENCODE_BIN`, and
`CLAUDE_CODE_BIN` above), `AGENT_CATALOG_REPO`, `BRIDGE_KEY_CI`,
`BRIDGE_KEY_CURSOR`. See [.ai/config-reference.md](.ai/config-reference.md).

## Role agent catalog

The six role agents (product-manager, system-architect, backend-developer,
frontend-developer, mobile-developer, qa-agent) no longer ship inside the
binary or as boot-time seed data. They live as plain files — agent, skills,
rules, tool policy — in `catalog/` at the repository root, and the server
syncs them into the database when `AGENT_CATALOG_REPO` points at a source:
once at boot, every `agent_catalog.interval`, and from the manual button on
the UI's Catalog page. A directory source is read in place; a git URL is
fetched into `agent_catalog.cache_dir` on each sync.

Adoption is by name: an existing agent with the same `Name` and no catalog
slug yet takes the upstream content, and agents the user has hand-edited are
left alone (`auto_pull_agent_updates` and `keep_skills_updated` decide how
far a sync may overwrite or merge). Unset source means no role agents are
created — the board starts empty and the sync stays idle.

The desktop bundles this directory under `Contents/Resources/catalog` and
passes it as `AGENT_CATALOG_REPO`; `make dev` defaults it to the checkout's
`catalog/`.

## Startup contract

Exactly one line goes to **stdout**, once the listener is bound:

```
LISTENING http://127.0.0.1:<port>
```

Everything else — including every log line — goes to stderr: colored console
lines on a terminal, one zerolog JSON object per line otherwise (which is what
`desktop/src/main/supervisor/server-log.ts` parses). The desktop app
spawns this binary with `PORT=0` and reads the port back off that line, then
polls `GET /health` until it answers 200. `/health` is public and answers 200
whenever the database is migrated, even with no LLM provider configured; the
body says `degraded` in that case.

## Toolchain pins

A board run reads the checkout's own version pins and starts the agent session
with the matching variable, so a repository is built with the toolchain it
declares. Only exact versions count; a range such as `>=18` is ignored.
`.tool-versions` and `mise.toml` are read first and win over the files below.

| variable | read from |
|---|---|
| `GOTOOLCHAIN` | `go.mod` (`toolchain`, then `go`), `.go-version` |
| `NODE_VERSION` | `.nvmrc`, `.node-version`, `package.json` `engines` |
| `PYTHON_VERSION` | `.python-version`, `pyproject.toml` |
| `RUBY_VERSION` | `.ruby-version`, `Gemfile` |
| `JAVA_VERSION` | `.java-version`, `.sdkmanrc` |
| `RUSTUP_TOOLCHAIN` | `rust-toolchain.toml`, `rust-toolchain` |
| `FLUTTER_VERSION` | `.fvmrc`, `.flutter-version` |

Parsing: `application/toolchain`. Confinement to `DATA_DIR/workspaces`: `adapter/localtoolchain`.

## Embedded Postgres

With `DATABASE_URL` empty, `internal/platform/embeddedpg` starts PostgreSQL 17
on a free loopback port: data in `$DATA_DIR/postgres`, binaries in
`$EMBEDDED_POSTGRES_CACHE_DIR`, downloaded from Maven Central on first start
(one log line before, one after). Before the first start on a build with
migration 133 (which drops the multi-tenant schema and cannot be undone), the
stopped cluster is copied to `$DATA_DIR/postgres-backup-pre-133`; to go back,
put that copy in place of `$DATA_DIR/postgres`. A `DATABASE_URL` database gets
no such copy. Postgres stops after the HTTP
shutdown drain, and a stale `postmaster.pid` left by a crash is cleared on the
next start.

## Optional: Postgres in Docker

`docker-compose.dev.yml` brings up Postgres 17 if you would rather not use the
embedded one. The server itself is not a service there: it binds 127.0.0.1 and
runs agent CLIs against working copies on this disk, neither of which survives
a container.

```sh
docker compose -f docker-compose.dev.yml up -d
DATABASE_URL=postgres://tasktrooper:tasktrooper@127.0.0.1:5432/tasktrooper?sslmode=disable \
SERVER_API_KEY=local-dev-key MCP_SECRETS_KEY=$(openssl rand -base64 32) \
  go run ./cmd/agent-server
```
