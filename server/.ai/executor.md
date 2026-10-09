# Executor

`cmd/executor` is a headless mode of this module. It runs agent turns and
one-shot model calls on the user's computer, with the user's own provider keys
and local tools. It has no board, no Postgres and no store of its own: the
caller on the other end of the HTTP surface is the source of truth, and what a
local run would have written to its stores reaches that caller as events.

The parent that starts it is the runner, or the desktop in development. The
local edition (`cmd/agent-server`) does not use it and is unchanged.

| layer | package |
|---|---|
| process | `cmd/executor` (thin), `platform/executor` (config, providers, local tools, listener) |
| HTTP | `adapter/executorapi` (net/http, NDJSON, bearer, redaction) |
| runs | `application/executor` (validation, per-run registry and loop, events) |
| coordination tools | `port.RemoteToolConnector` → `adapter/mcp.RemoteConnector` |

## Process contract

1. The parent writes **one JSON line** on stdin and keeps stdin open. EOF on
   stdin (or SIGINT/SIGTERM) means shut down. On shutdown every run is
   cancelled, still sends its `done` frame, and the process trees runs started
   are stopped.
2. The first and only stdout line is `EXECUTOR_LISTENING http://127.0.0.1:<port>`.
   Logs go to stderr.
3. A config the executor refuses exits with status 2 before it listens.

```json
{"listen":"127.0.0.1:0","token":"<≥16 chars>","data_dir":"/…/executor",
 "workspace_root":"/…/workspaces","embeddings_base_url":"http://127.0.0.1:…",
 "providers":[{"id":"anthropic","type":"anthropic","base_url":"","api_key":"sk-…","models":["claude-sonnet-4-5"]},
              {"id":"<uuid>","type":"openai_compatible","base_url":"https://openrouter.ai/api/v1","api_key":"…","models":["…"]}]}
```

- `listen` defaults to `127.0.0.1:0` and must be a loopback address.
- `token` is the bearer every route requires.
- `workspace_root` is the root the runner clones into (`repos/<repo>/task-<id>`).
  It is created if it is missing. `data_dir` is optional and is created too.
- `providers[].type` is one of `anthropic`, `openai`, `groq`, `gemini`,
  `local`, or `openai_compatible` (aliases `endpoint` and `custom`).
  `openai_compatible` needs a `base_url`. Agent CLI types (`claude_code`, …)
  are refused, because the runner starts those itself.
- `models[0]` is the provider's default model. Optional `timeout_seconds` is
  per provider.
- Keys live only in memory. They are never logged and never put on argv, and
  `Config` prints without them. `embeddings_base_url` is accepted and unused
  until the index arrives (Faz 3).

## HTTP API (loopback, `Authorization: Bearer <token>` on every route)

Errors use the runner's shape, `{"v":1,"error":{"code","message",…}}`, with
this status map:

| code | status |
|---|---|
| `bad_request` | 400 |
| `unauthorized` | 401 |
| `unsupported_method` | 404 |
| `conflict` | 409 |
| `rate_limited` | 429 |
| `cancelled` | 499 |
| `internal` | 500 |
| `not_implemented` | 501 |
| `upstream` | 502 |
| `timeout` | 504 |

Provider keys, the bearer and the run's MCP token are scrubbed from every
response body and frame.

### `GET /exec/health`

Returns `{"ok":true,"version":"…","protocol":1,"active_runs":0}`.

### `POST /exec/agent.run`

```json
{"id":"<optional: runner call id>","run_id":"r-1","kind":"board|chat",
 "agent":{"name":"backend-developer","system_prompt":"…","provider_id":"anthropic","model":"",
          "tool_policy":{"allow_tools":["read_file","run_terminal","move_board_task"]},"max_turns":0,"effort":""},
 "prompt":"…","messages":[{"role":"user","content":"…"}],
 "workspace":"repos/app/task-<id>",
 "mcp":{"url":"https://app.tasktrooper.ai/api/mcp","token":"<per-run>","server_name":"tasktrooper"},
 "timeout_ms":0}
```

- Messages are assembled as `system_prompt`, then `messages`, then `prompt`
  (as a user turn).
- An empty `model` uses the provider's default.
- `max_turns` 0 keeps the configured limits (30 for chat, 80 for board).
- `kind: board` runs `RunTask`. `kind: chat` runs `RunStream` and streams text.
- `workspace` must already exist under `workspace_root`. With no workspace, the
  run gets no file or terminal tools.
- A run that fails validation, or reuses the `run_id` of a live run (409),
  gets an HTTP error and no stream.

Otherwise the answer is `200 application/x-ndjson`, flushed line by line, in
the runner's `claude.run` envelope:

```
{"v":1,"id":"r-1","event":"started"}
{"v":1,"id":"r-1","event":"event","payload":{"seq":1,"at":"…","kind":"step","step":"iteration_start","data":{"iteration":1,"message_count":2}}}
{"v":1,"id":"r-1","event":"event","payload":{"seq":4,"at":"…","kind":"tool_call","call_id":"c1","name":"read_file","source":"local","arguments":"{\"path\":\"main.go\"}"}}
{"v":1,"id":"r-1","event":"event","payload":{"seq":5,"at":"…","kind":"tool_result","call_id":"c1","name":"read_file","source":"local","is_error":false,"content":"…","duration_ms":3}}
{"v":1,"id":"r-1","event":"event","payload":{"seq":6,"at":"…","kind":"usage","provider_id":"anthropic","model":"claude-sonnet-4-5","prompt_tokens":812,"completion_tokens":40,"total_tokens":852}}
{"v":1,"id":"r-1","event":"event","payload":{"seq":9,"at":"…","kind":"text","delta":"Done"}}
{"v":1,"id":"r-1","event":"done","ok":true,"result":{"final_text":"…","usage":{"llm_calls":3,"prompt_tokens":…,"completion_tokens":…,"total_tokens":…,"cache_read_tokens":0,"cache_write_tokens":0},"tool_usage":["list_board_tasks","read_file"],"tool_counts":{"read_file":1,"list_board_tasks":1},"duration_ms":4210}}
{"v":1,"id":"r-1","event":"done","ok":false,"error":{"code":"budget_exhausted","message":"…","partial":"…","run":{"usage":{…},"tool_usage":[…],"duration_ms":…}}}
```

- `id` is the request's `id` when given (so a stream the runner forwards
  unchanged matches the cloud's call id), otherwise the `run_id`.
- `seq` is the order on the wire.
- Event kinds:
  - `step` is every activity step the loop records, with the same type and
    payload a local run stores.
  - `tool_call` and `tool_result` carry `source` `local` or `remote`. Content
    is capped at 4000 characters, with `truncated` set when cut.
  - `text` is a streamed delta, or `segment_break: true` (chat only).
  - `usage` is one per model call, including summaries and the wrap-up.
- `done` comes exactly once.
  - `result.tool_usage` lists the tools that **succeeded** at least once. The
    caller's evidence gates read it. `tool_errors` counts failures.
  - On failure, `error.run` carries the same summary.
  - `budget_exhausted` adds the loop's wrap-up as `partial`.
  - `rate_limited` adds `retry_after_ms`.
  - `clarification` and `resource_block` ride on a successful result when the
    loop stopped for one.
- Failure codes: `cancelled`, `timeout`, `upstream` (provider or coordination
  endpoint), `rate_limited`, `budget_exhausted`, `bad_request`, `internal`.
- `/exec/cancel`, the caller disconnecting, a write failure, `timeout_ms` and
  shutdown all cancel the run.

### `POST /exec/llm.complete`

Request: `{"provider_id","model","system","messages":[{role,content}],"max_tokens","response_format"?,"timeout_ms"?}`.

Response: `{"text","usage":{prompt_tokens,…},"stop_reason"}`.

This is a one-shot call with no tools. Use it for titles, commit messages,
summaries, query rewrites, and planner steps (`response_format` takes the
domain JSON / JSON-schema format).

### `POST /exec/cancel`

Request: `{"run_id":"r-1"}`, or `{"id":"<stream id>"}` so a runner can forward
its own cancel body.

Response: `{"v":1,"run_id":"r-1","cancelled":true|false}`. A run that already
ended answers `false` and is not an error.

### Reserved

`POST /exec/index.ensure` and `POST /exec/index.search` answer 501 until the
local code index lands (Faz 3).

## Tools of a run

The run's registry is built in this order:

1. The coordination endpoint's tools from `mcp.url`/`mcp.token` (`adapter/mcp`
   streamable HTTP client), under the names the endpoint serves them by.
2. This computer's tools on top. On a name clash **local wins**.

The tools are:

- **Local, with a workspace:** `read_file`, `write_file`, `edit_file`,
  `edit_lines`, `delete_file`, `move_file`, `grep_code`, `get_repo_tree`,
  `get_symbol_skeleton` (by file), `run_terminal` (confined to
  `workspace_root`, working dir injected from the run), `download_file`.
- **Local, always:** `fetch_url`, `http_request`, `web_search`, `browser_*`.
- **Withheld from the remote side:** `download_file`, `start_task_preview`,
  `commit_task_changes`, `deploy_release`, `codebase_search`,
  `expand_symbol_context`, `get_symbol_skeleton`, `browser_*` and
  `mcp_filesystem_*`. Served from the cloud, these would act on the server's
  disk, processes or index.

The agent's `tool_policy` decides the remaining tools as usual. `ask_user` and
other clarifications come from the endpoint, which records them itself.

## How the loop's dependencies are met without a store

| loop dependency | executor |
|---|---|
| `port.LLMClient` | in-memory `llm.MultiProviderClient` keyed by provider id, wrapped per run to meter usage into `usage` events and totals |
| `port.ToolRegistry` | per-run `registry.New()` + `WorkspaceRegistry`, wrapped to emit `tool_call`/`tool_result` |
| history budget, model limits, summariser, run token cap | from the embedded `config.yml`. Limits resolve the provider id to its wire type |
| activity (`activity.FromContext`) | an `ActivityStore` whose `AppendStep` emits a `step` event and keeps nothing |
| screenshot archiver | none: images still go to the model, but get no attachment ids |
| auditing / action-recording registries | none: the coordination endpoint audits the board tools it executes |
| tool-usage tracker | `registry.ContextWithToolUsage`, read into `done.tool_usage` |
| background processes | `proctree` scope per run, killed when the run ends |
