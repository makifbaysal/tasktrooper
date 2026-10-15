---
title: Agent CLIs and API providers
description: How a run actually executes — local CLI sessions, API providers, model selection, and version pins.
---

Every agent has a **Provider**, chosen on its Settings tab, and that choice
decides how its sessions actually run: as a local process on this machine, or
as an HTTP call to a model API.

## The four local CLIs

Claude Code, Cursor, Antigravity and OpenCode all run the same way: as a
**headless CLI session on this host**, using whatever subscription or
account that CLI is already signed in with on this machine — not an API key
TaskTrooper holds. There is no "enabled" flag for any of them; the switch is
simply whether the binary is on `PATH` (or the path named by its own
environment variable — `CLAUDE_CODE_BIN`, `CURSOR_AGENT_BIN`,
`ANTIGRAVITY_BIN`, `OPENCODE_BIN`). A provider whose binary is missing is
still selectable on an agent, but every run on it fails with one sentence
naming where it can actually run — never a silent fallback to some other
provider or endpoint.

| CLI | Provider value | Binary |
|---|---|---|
| Claude Code | `claude_code` | `claude` |
| Cursor | `cursor_agent` | `cursor-agent` |
| Antigravity | `antigravity` | `agy` |
| OpenCode | `opencode` | `opencode` |

Because these are local processes rather than endpoints, none of them can be
"connected", "tested", or used for embeddings the way an API provider can —
there is no base URL and no key to test. They are simply available on an
agent, or not, and every run's deadline (`run_timeout`, one hour by default
for all four) is what catches a session that never exits on its own.

## How a run is started

A run on any of the four CLIs is started **headless** (`claude -p`,
`cursor-agent -p --force`, `agy -p`, `opencode run`) with the task's prompt,
inside the same workspace directory the run's own tools are scoped to — a
board run's task checkout, or nothing at all for a call that touches no
repository. No workspace is ever a hard refusal rather than a guess: running
inside the server's own directory would let a session edit the code hosting
it.

TaskTrooper's own board tools reach the session over MCP, at a fresh
`/mcp` endpoint the server mounts for the run, bound to `127.0.0.1` and
guarded by a **token minted for that one run** (or, in chat, that one turn)
and revoked when it ends. The session sees these as `mcp__tasktrooper__<name>`
tools, and its config is written with `--strict-mcp-config` — which also
means **neither a repository's own checked-in `.mcp.json` nor your personal
Claude Code settings under `~/.claude` are ever loaded** into a TaskTrooper
session. Nothing there was chosen for this product, and letting it in would
mean a board task silently ran with hooks, hooks hosts, or hosts, permission
rules you never intended for it. If you want an agent to reach your own MCP
servers, [add them explicitly](mcp-servers.md) instead.

If you connect your own MCP servers to TaskTrooper, those are the "other
direction" — a session calling *out* to servers you configured — and are
namespaced `mcp_<server_id>_<tool_name>` rather than
`mcp__tasktrooper__<name>`; see [MCP servers](mcp-servers.md) for that half.

## What the child process can and cannot see

A local CLI session runs as a child process with a deliberately narrow
environment: an allow-list that excludes `DATABASE_URL`, `SERVER_API_KEY` and
the secrets-encryption key, plus an explicit pass-through of
`CLAUDE_CONFIG_DIR`, `CLAUDE_CODE_OAUTH_TOKEN` and your proxy variables.
`ANTHROPIC_API_KEY` is deliberately never forwarded, even if you have one set
on this machine for something else — handing a Claude Code session a key it
was never given would move it onto metered billing instead of the
subscription it is actually signed in with.

## Model selection

Each agent picks its **Model** and, optionally, a stronger **Model (heavy)**
independently of its provider — see [Your own agents](custom-agents.md) for
the field. On a local CLI, both may be left empty: the session then runs
with no `--model` flag at all, which is what a subscription user typically
wants, since the CLI itself picks whichever model the account defaults to.
None of the four CLIs validate the model name you give them, so a typo does
not fail fast — it costs the run.

## API providers

For agents that are not backed by a local CLI, TaskTrooper talks to a model
API directly:

| Provider | Notes |
|---|---|
| OpenAI (GPT) | Requires an API key |
| Anthropic (Claude) | Requires an API key; native API, not the CLI |
| Google Gemini | Requires an API key; with none, falls back to Vertex AI via GCP application default credentials |
| Groq | Requires an API key; free-tier friendly, fast tool-calling models |
| Custom (OpenAI-compatible) | Any OpenAI-compatible `/v1` endpoint — LM Studio, Ollama, vLLM, OpenRouter, or your own IP |

These are configured on the **LLM Connection** settings page (Settings →
LLM Connection): add a named endpoint with its base URL, optional API key,
default model and timeout, and it becomes selectable as a Provider on any
agent once connected. Keys are stored **encrypted at rest**, never on argv or
in a config file you'd check in. You can add as many named OpenAI-compatible
endpoints as you like — one entry per LM Studio instance, per OpenRouter
account, and so on — alongside the four built-in native providers.

## Version pins

A handful of environment variables can be set on a task's session to steer a
version manager the session already trusts toward the right toolchain
version for that repository — `GOTOOLCHAIN`, `NODE_VERSION`,
`PYTHON_VERSION`, `RUBY_VERSION`, `JAVA_VERSION`, `FLUTTER_VERSION`,
`RUST_TOOLCHAIN`, `RUSTUP_TOOLCHAIN`, plus a handful of terminal/locale
variables (`CI`, `TERM`, `NO_COLOR`, `FORCE_COLOR`, `TZ`, and others) and
your own `TT_`-prefixed names. TaskTrooper does not itself parse a
repository's `.tool-versions`, `go.mod` or `.nvmrc` — it passes these
allow-listed names through to the child process environment, and whatever
version manager the repository already relies on (asdf, nvm, rbenv, and so
on) resolves the actual version from those pin files the normal way. Anything
outside this allow-list is refused rather than passed through silently, since
a name that points at a path, a library, a config file or a proxy could
change what the session actually runs, not just how it reports itself.

## Executor configuration

Each of the four local CLIs has its own small config section
(`claude_code`, `antigravity`, `cursor_agent`, `opencode` in the server's
configuration) covering things like the binary path, the run timeout, and —
for Claude Code specifically — the maximum turns per session (100 by
default), which CLI settings sources are loaded (`project,local` by default —
your own user-level `~/.claude` is deliberately excluded, for the same
reason a repository's `.mcp.json` is), and an optional cap on how many
board sessions may run concurrently on that CLI at once, on top of the
limits in Settings (see [Usage limits and concurrency](usage-limits.md)).
These are host-level settings, not per-agent ones — every agent on a given
CLI shares them.

## Chat vs. board sessions

A board task and a chat conversation both run on the same executor once an
agent is assigned, but they carry the run differently. A board run is a
fresh session scoped to the task's own workspace. A chat is multi-turn: the
CLI's own conversation id is kept alongside the session, and each new
message resumes it (`--resume <id>` on Claude Code) rather than starting
over — so an agent chat that has been open for days does not replay its
whole history into the model on every message, and the CLI's own
in-conversation state (files it opened earlier in the thread, for example)
carries forward naturally.

## Follow-up steps inside one run

A single board run is not always one CLI invocation end to end. Steps that
happen after the main session finishes — fixing a failed build, settling
open acceptance criteria, answering a review verdict — resume that same CLI
session with just the new instruction, rather than replaying the whole
flattened conversation back into a brand-new one. This is also why a run's
`--resume` target and its MCP token are tracked per run rather than per
message: a follow-up step is a continuation of the same session, holding the
same context the main step already built up, not a fresh one paying to
rediscover it.
