---
title: Configuration
description: What the desktop app configures for you, the data directory layout, and where to change the defaults.
---

Nothing is required to configure by hand. The desktop app generates a bearer
token, points the backend at its own data directory, and starts the bundled
embedder, all before you see the window. Everything on this page is for
tuning behavior, not for getting started — see [First run and
setup](first-run.md) for that.

## What the desktop app sets up for you

On first launch, the desktop shell:

- generates `SERVER_API_KEY` (the bearer token) and `MCP_SECRETS_KEY` (the
  encryption key for stored provider secrets) and keeps them in `local.bin`
  under the data directory
- picks a data directory (see below) and passes it as `DATA_DIR`
- spawns the backend with `PORT=0` and reads back the port it actually bound
- points `EMBEDDINGS_BASE_URL` at its own bundled embedder, so code search
  and RAG work with no provider connected
- passes its own `apiToken` to the page so the UI can call the backend

You never edit a config file to get a working install. Everything below is
for changing a default.

## The data directory

Where it lives, and what's in it, is covered in full in [Data directory and
security](data-and-security.md). In short: one Postgres cluster, the local
embedding model, every repository's clones and per-task checkouts under
`workspaces/`, and `local.bin` holding the generated secrets.

## Where overrides go

There are three places, in the order most people reach for them:

1. **Admin settings pages**, in the running app — the right place for
   anything you'd change without a restart:
   - **Settings → LLM** — OpenAI-compatible endpoints (your own IP, Ollama,
     LM Studio, vLLM, OpenRouter, Groq, …), native providers (Gemini,
     Anthropic), and local agent CLIs (Claude Code and friends) — connect,
     disconnect, set a default, set a request timeout.
   - **Settings → Integrations** — GitHub, Vercel, App Store Connect, Google
     Play: the accounts TaskTrooper acts on your behalf through. Saved once,
     used by every project.
   - **Settings → Usage** — token usage by day and by model; nothing to
     configure here, but it's where you'd notice a runaway agent.
   - **Board Workflow** (per project) — the columns and the transition
     rules between them.
   - Per-repository settings (verify/build/test commands, pipeline job
     mapping, lifecycle gates, deploy targets, test strategy, incident
     policy) — see [Git and pull requests](git-and-pull-requests.md),
     [Deploy targets and recipes](deploy.md) and [Production
     incidents](incidents.md).
2. **Environment variables**, read once at backend boot (`server/README.md`).
   The desktop app sets the required ones itself; you'd only set these
   running the server yourself (see [Run from source](run-from-source.md)).
3. **`resources/config.yml`**, for the knobs with no settings-page
   equivalent yet — orchestration limits, board gate timing, evolution
   budgets, embedding throttling. Not something a packaged install expects
   you to edit; relevant mainly if you're running from source. `${VAR}` in
   the file is substituted from the environment.

## Environment variables

The backend refuses to start without the first two; everything else has a
default.

| Var | Default | Meaning |
|---|---|---|
| `SERVER_API_KEY` | — (required) | The bearer token every request must carry |
| `MCP_SECRETS_KEY` | — (required) | Encrypts provider API keys, MCP secrets and store credentials at rest — must stay the same across runs, or everything already encrypted becomes unreadable |
| `DATABASE_URL` | empty → embedded Postgres | External Postgres DSN. Empty starts an embedded Postgres 17 under `$DATA_DIR/postgres` |
| `DATA_DIR` | `./data` | Workspaces, RAG files, embedded Postgres data |
| `PORT` | `8085` | HTTP port; `0` picks a free one (what the desktop app uses) |
| `EMBEDDED_POSTGRES_CACHE_DIR` | `$DATA_DIR/postgres-bin` | Where the Postgres binaries are downloaded and extracted (first start only) |
| `EMBEDDINGS_BASE_URL` | — | An OpenAI-compatible host serving `POST /v1/embeddings`; set, a `local` embedding provider is created at boot (`nomic-embed-text-v1.5`, 768 dimensions) |
| `CORS_ORIGINS` | `app://tasktrooper,http://localhost:3200,http://127.0.0.1:3200` | Origins allowed to call this server |
| `PUBLIC_BASE_URL` | `http://127.0.0.1:<port>` | The origin a Claude Code session calls TaskTrooper's own tools back on |
| `CLAUDE_CODE_BIN` | `claude` | The Claude Code CLI, resolved on `PATH` |
| `CURSOR_AGENT_BIN` / `ANTIGRAVITY_BIN` / `OPENCODE_BIN` | `cursor-agent` / `agy` / `opencode` | The other agent CLIs, resolved on `PATH` |
| `CHROME_BIN` | — | Chromium binary for the `browser_*` tools |
| `CONFIG_PATH` | `resources/config.yml` | Config file path; falls back to the copy embedded in the binary if absent |
| `SHUTDOWN_GRACE` | `9m` | How long the server keeps working after SIGTERM before in-flight runs are cancelled |

`DATABASE_URL`, `SERVER_API_KEY` and `MCP_SECRETS_KEY` are scrubbed from the
process's own environment once read, so no child process (an agent CLI, a
shell tool) can read them back out.

## The most useful `config.yml` keys

The shipped defaults are tuned for a single local machine. A few are worth
knowing about even if you never open the file:

| Key | Default | What it controls |
|---|---|---|
| `claude_code.max_concurrent_sessions` | unset (no cap) | An optional host-wide cap on Claude Code sessions running at once, on top of the concurrency limits in **Settings**. Unset or `0` adds no cap of its own. |
| `claude_code.max_turns` | `100` | Turn budget for one Claude Code session before it returns what it has |
| `orchestration.max_parallel_tasks` | `3` | How many subtasks an orchestrated run executes in parallel |
| `orchestration.max_plan_tasks` | `10` | Max tasks in one orchestration plan |
| `board.pipeline_gate_timeout` | `45m` | How long a task waits in Code Review for a build/test result before the reviewer is dispatched anyway |
| `board.pipeline_gate_interval` | `2m` | How often the pipeline gate sweeper re-asks GitHub about an unfinished pipeline |
| `board.verification_enabled` | `false` | Post-run build/vet verification with an automatic fix loop (up to `verify_max_fix_attempts`, default `2`) before a broken task is sent back to In Progress |
| `board.require_criteria_complete` | `false` | Blocks a task from `ready_for_qa`/`done`/`released` while it still has open acceptance criteria |
| `evolution.enabled` | `false` | Master switch for agent self-evolution (periodic reflection, KPI evaluation) — see [Self-evolution and KPIs](self-evolution.md) |
| `evolution.golden_gate` | `false` (`true` in the shipped `config.yml`) | Runs the golden test suite before and after a proposed skill/rule change and keeps or rolls back the whole set based on an independent judge |
| `rag.enabled` | `false` | File upload and retrieval-augmented context |
| `rag.top_k` | `5` | Chunks injected into chat context per query |
| `tools.terminal.enabled` | `false` | Enables `run_terminal` |
| `tools.terminal.sandbox.mode` | `off` | `allowlist`, `blocklist`, or `off` for shell command restriction |
| `tools.web.enabled` | `false` | Enables `fetch_url` |
| `tools.search.enabled` | — | Enables `web_search` (DuckDuckGo, falling back to Bing; no API key needed) |
| `embedding.requests_per_minute` | `0` (unthrottled) | Caps embedding calls per minute; the shipped config tunes this for Mistral's free tier (`55`) |
| `prod_ops.probe_interval` | `1m` | How often the health monitor probes a deploy target; two consecutive failures open an incident, one success closes it |

Full reference, every key: `server/.ai/config-reference.md` in the
repository (not published on this site, since it documents server internals
rather than a screen you'll see).

## Running from source: `make dev` and `.env.local`

If you're building TaskTrooper yourself rather than running the packaged
app, copy `server/.env.local.example` to `server/.env.local` and fill in
`SERVER_API_KEY` and `MCP_SECRETS_KEY` (`openssl rand -base64 32` for the
latter); `make dev` from the repository root loads it and runs the backend
(embedded Postgres) plus the UI dev server in your terminal. See [Run from
source](run-from-source.md) for the full setup, prerequisites and verify
commands.
