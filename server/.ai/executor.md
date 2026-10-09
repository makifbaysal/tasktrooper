# Executor

`cmd/executor` is a headless mode of this module. It runs agent turns and
one-shot model calls on the user's computer, with the user's own provider keys
and local tools, and keeps the code index of the checkouts on it. It has no
board and no store of record: the caller on the other end of the HTTP surface
is the source of truth, and what a local run would have written to its stores
reaches that caller as events. The only database it starts is a disposable
index cache (see "Local code index").

The parent that starts it is the runner, or the desktop in development. The
local edition (`cmd/agent-server`) does not use it and is unchanged.

| layer | package |
|---|---|
| process | `cmd/executor` (thin), `platform/executor` (config, providers, local tools, listener) |
| HTTP | `adapter/executorapi` (net/http, NDJSON, bearer, redaction) |
| runs | `application/executor` (validation, per-run registry and loop, events) |
| coordination tools | `port.RemoteToolConnector` → `adapter/mcp.RemoteConnector` |
| code index | `port.LocalCodeIndex` → `application/localindex` (the server's indexer and injector over its own store); `platform/executor` wires the embedder (`embedder.go`) and the store (`indexstore.go`) |
| post-run half | `application/executor/checkout.go`: the verification pass is `board.VerifyWorkspace` (the board's own verify step), git is `port.CheckoutGit` → `adapter/vcs/git.Checkout` |

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
 "workspace_root":"/…/workspaces","embeddings_base_url":"http://127.0.0.1:…/v1",
 "embeddings_source":"onnx-int8","postgres_cache_dir":"/…/postgres-bin",
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
  `Config` prints without them.
- `embeddings_base_url` is the desktop embedder's OpenAI-compatible base. It
  must be on this computer (loopback IP or `localhost`): every chunk of code
  the index embeds is sent to it. Without it the index answers `not_ready`
  until `/exec/embeddings.set` names one; that route also follows the
  embedder to a new port without a restart.
- `embeddings_source` names that engine in the index provenance; empty means
  `onnx-int8` (the desktop's int8 ONNX `nomic-embed-text-v1.5`). Set `tei` (or
  another name) for any other engine, so its vectors never mix with these.
- `postgres_cache_dir` is where the index store's Postgres 17 binaries are
  (the desktop's own `postgres-bin` spares a ~30 MB download). Empty means
  `data_dir/index-pg/postgres-bin`. Without `data_dir` there is no index.

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
| `upstream` | 502 |
| `not_ready` | 503 |
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
 "index":{"repo_key":"<stable repository key>","branch":"tt/task-<id>","wait_ms":0},
 "env":{"GOTOOLCHAIN":"go1.22.1+auto","NODE_VERSION":"20.11.1"},
 "timeout_ms":0}
```

- Messages are assembled as `system_prompt`, then `messages`, then `prompt`
  (as a user turn).
- An empty `model` uses the provider's default.
- `max_turns` 0 keeps the configured limits (30 for chat, 80 for board).
- `kind: board` runs `RunTask`. `kind: chat` runs `RunStream` and streams text.
- `workspace` must already exist under `workspace_root`. With no workspace, the
  run gets no file or terminal tools.
- `index` (optional) names the code index the run reads. See "Runs that name
  an index" below.
- `env` (optional) is the repository's toolchain environment. For this run
  only, it is added to the environment of every process the run's tools start
  (`run_terminal` and the background processes it leaves), after the
  toolchain this computer resolves from the checkout's own version files, so
  the run's values win. At most 64 entries; names are `[A-Za-z_][A-Za-z0-9_]*`;
  values up to 4 KiB without NUL. `PATH` is refused (400): a PATH from the
  coordination side names another machine's directories, and this computer
  builds its own from the checkout.
- A run that fails validation, or reuses the `run_id` of a live run (409),
  gets an HTTP error and no stream.

Otherwise the answer is `200 application/x-ndjson`, flushed line by line, in
the runner's `claude.run` envelope:

```
{"v":1,"id":"r-1","event":"started"}
{"v":1,"id":"r-1","event":"event","payload":{"seq":1,"at":"…","kind":"index_progress","phase":"indexing","files_processed":40,"files_total":212}}
{"v":1,"id":"r-1","event":"event","payload":{"seq":2,"at":"…","kind":"step","step":"index_ensure","data":{"repo_key":"…","branch":"tt/task-1","status":"completed","files":212,"chunks":980,"seeded_from":"base","duration_ms":5120}}}
{"v":1,"id":"r-1","event":"event","payload":{"seq":3,"at":"…","kind":"step","step":"index_context","data":{"repo_key":"…","branch":"tt/task-1","injected":true,"tools":3,"duration_ms":180}}}
{"v":1,"id":"r-1","event":"event","payload":{"seq":4,"at":"…","kind":"step","step":"iteration_start","data":{"iteration":1,"message_count":3}}}
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
  - `attachment` is one per image a tool result carries (a screenshot), sent
    right after that call's `tool_result`: `{call_id, mime, size, data_base64}`,
    `size` being the decoded bytes. Above 2 MB the data is left out and the
    frame is `{call_id, mime, size, too_large: true}`. The image still reaches
    the model either way; the frame is what lets the caller archive it and
    cite it as evidence.
  - `text` is a streamed delta, or `segment_break: true` (chat only).
  - `usage` is one per model call, including summaries, the wrap-up and a
    chat run's index query rewrite.
  - `index_progress` is the run's own index pass (only for a run that names an
    index and has a workspace): `phase` (`store`, `waiting`, `seeded`,
    `indexing`), `files_processed`/`files_total` while indexing,
    `seeded_from` when seeded. At most one indexing count per 250 ms.
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
its own cancel body. An index pass or a verification is cancelled by
`{"id":"<its stream id>"}`.

Response: `{"v":1,"run_id":"r-1","cancelled":true|false}`. A run that already
ended answers `false` and is not an error.

### `POST /exec/index.ensure`

Brings the index of one checkout up to date. A run that names an index does
this itself, so this route is for a caller that wants an index ready ahead of
a run, or a progress bar.

```json
{"id":"<optional stream id>","workspace":"repos/app/task-1","repo_key":"<stable repository key>","branch":"tt/task-1"}
```

- `workspace` is the checkout, relative to `workspace_root`, and must exist.
- `repo_key` is the caller's stable key for the repository (the cloud's
  repository id). Up to 200 characters, no control characters.
- `branch` empty is the repository's base index. A task branch gets an index
  of its own (git-safe name, up to 255 characters).
- `id` defaults to `index:<repo_key>@<branch>`. A second pass under the same
  id while one runs is 409; a pass of the same index under another id (a run's
  own pass, say) waits for the first, then finds little left to do.

The answer is the `agent.run` envelope: `started`, `index_progress` events,
then one `done` whose `result` is the index state:

```
{"v":1,"id":"index:app@tt/task-1","event":"started"}
{"v":1,"id":"index:app@tt/task-1","event":"event","payload":{"seq":1,"at":"…","kind":"index_progress","phase":"store"}}
{"v":1,"id":"index:app@tt/task-1","event":"event","payload":{"seq":2,"at":"…","kind":"index_progress","phase":"seeded","seeded_from":"base"}}
{"v":1,"id":"index:app@tt/task-1","event":"event","payload":{"seq":3,"at":"…","kind":"index_progress","phase":"indexing","files_processed":1,"files_total":1}}
{"v":1,"id":"index:app@tt/task-1","event":"done","ok":true,"result":{"repo_key":"app","branch":"tt/task-1","status":"completed","files":212,"chunks":980,"symbols":450,"commit_sha":"…","embedding_model":"nomic-embed-text-v1.5@onnx-int8","embedding_dims":768,"seeded_from":"base","duration_ms":5120}}
```

- Only files whose bytes changed since the last pass are embedded again; an
  unchanged checkout embeds nothing.
- `seeded_from` is `base` when a new branch index started from the
  repository's base index, or `branch:<name>` when there was no usable base and
  it started from the most recently indexed branch of the same repository.
- Failure codes: `bad_request`, `not_ready` (no embedder or no `data_dir`),
  `upstream` (the embedder failed), `cancelled`, `internal`.

### `POST /exec/index.search`

Request: `{"repo_key","branch"?,"query","k"?}`. `k` defaults to 5 and is
capped at 50.

Response (hybrid search: vector similarity fused with trigram text matches):

```json
{"index":{"repo_key":"app","branch":"tt/task-1","status":"completed","files":212,"chunks":980,"symbols":450,"embedding_model":"nomic-embed-text-v1.5@onnx-int8","embedding_dims":768},
 "results":[{"path":"auth/refresh.go","symbol":"RefreshToken","kind":"function","language":"go","signature":"func RefreshToken(…) (…)","start_line":12,"end_line":40,"score":0.031,"snippet":"func RefreshToken(…"}]}
```

- A branch searches its own index once it is complete, the base index until
  then; `index.branch` says which one answered.
- `not_ready` (503): no finished index for the repository, or one built with
  another embedding model or engine (run `index.ensure` again), or no embedder.
- `score` is the fused rank score, comparable within one answer only.

### `POST /exec/embeddings.set`

Moves the index to the embedding engine's new address. The desktop restarts
its embedder on another port now and then; the executor runs on, and so do its
runs.

Request: `{"embeddings_base_url":"http://127.0.0.1:<port>/v1","embeddings_source":""}`.
Response: `{"v":1,"embeddings_base_url":"…","embeddings_source":"onnx-int8"}`.

- The same loopback rule as the config applies (400 otherwise). An empty URL
  leaves no engine: index calls answer `not_ready` until the next set.
- The model is asked again at the new address. Same model and engine, same
  provenance: nothing is re-embedded. Another `embeddings_source` makes every
  index stale, and each is re-embedded whole on its next pass.
- A call in flight finishes against the old address; the indexer retries a
  refused connection, which reaches the new one.

## The post-run half: verify, git, commit and push

A board run's agent turn is half of it. The other half — build and test the
checkout, feed a failure back into a fix round, commit, push, open the pull
request — needs the checkout, so for a run on this computer the caller drives
it here, after `agent.run`, through these routes. The caller keeps the
decisions (which checks, how many fix rounds, the commit message, the branch,
the pull request); the build, the tests and this computer's git credentials
stay on this computer. Every `workspace` is relative to `workspace_root` and
must exist.

### `POST /exec/verify`

One verification pass, exactly the board's own (`board.VerifyWorkspace`):

```json
{"id":"<optional stream id>","workspace":"repos/app/task-1",
 "commands":[{"dir":"api","argv":["go","test","./..."]}],
 "verify_command":"make check",
 "quality":{"coverage":{"enabled":true,"threshold":80},"mutation":{"enabled":false}},
 "env":{"GOTOOLCHAIN":"go1.24.3"},
 "timeout_ms":0}
```

- The checks are picked as a board run picks them: `commands` (the project
  model's required checks, each run in `dir` under the checkout) when there
  are any, else `verify_command` (the repository's own, split on spaces),
  else what the checkout's build files declare (`go.mod` → build and vet,
  `package.json`'s `build` script, `Cargo.toml`, …). Sending neither is "auto".
  An npm install runs first where a `package-lock.json` has no
  `node_modules`; when it fails, the pass reports nothing rather than a
  failure.
- Each stage runs with the toolchain this computer resolves from the
  checkout's own version files, then `env` (the same rules as `agent.run`'s:
  no `PATH`).
- `quality` runs the coverage and mutation gates the repository enforces once
  the build passes, and reports the ones it has but does not enforce in
  `result.advisory` (the caller may measure those after the hand-off).
  Without it nothing beyond the build runs.
- `id` defaults to `verify:<workspace>`. One pass per checkout at a time: a
  second one, or a second one under the same id, is 409.
- `timeout_ms` bounds the whole pass; each stage also has its own (5 minutes,
  15 for an install).

The answer is the `agent.run` envelope: `started`, `verify_stage` events
(`phase` `started` then `finished`, with `outcome` `passed` | `failed` |
`unverified`, `exit_code`, `duration_ms`), `verify_output` events (the stage's
stdout and stderr as they come, in chunks of at most 16 KiB, at most 256 KiB
per stage, then one `truncated: true`), and one `done`:

```
{"v":1,"id":"c-7","event":"started"}
{"v":1,"id":"c-7","event":"event","payload":{"seq":1,"at":"…","kind":"verify_stage","phase":"started","name":"check:api:go","dir":"api","command":["go","test","./..."]}}
{"v":1,"id":"c-7","event":"event","payload":{"seq":2,"at":"…","kind":"verify_output","stage":"check:api:go","data":"--- FAIL: TestRefresh…"}}
{"v":1,"id":"c-7","event":"event","payload":{"seq":3,"at":"…","kind":"verify_stage","phase":"finished","name":"check:api:go","dir":"api","command":["go","test","./..."],"outcome":"failed","exit_code":1,"duration_ms":5120}}
{"v":1,"id":"c-7","event":"done","ok":true,"result":{"passed":false,"report":"$ go test ./...\n--- FAIL: TestRefresh…","stages":[…],"advisory":{"coverage":false,"mutation":false},"duration_ms":5190}}
```

- A failing build is a verdict, not a failed call: `ok` is true and
  `result.passed` false. `result.report` is what the board hands a fix round
  (each failing command and the tail of its output); on a pass it carries the
  notes (`[unverified] …` for a tool that is not installed here, coverage
  figures).
- Failure codes: `bad_request`, `conflict`, `cancelled`, `timeout`.

### `POST /exec/git.status`, `git.diff`, `git.log`

One document each way. Request `{"workspace", "timeout_ms"?}` plus:

| route | request | answer |
|---|---|---|
| `git.status` | — | `{"branch","head","upstream","ahead","behind","clean","files":[{"path","orig_path"?,"index","worktree"}]}` (`branch` empty when detached, `index`/`worktree` the porcelain letters, `?` untracked) |
| `git.diff` | `base`?, `name_only`?, `max_bytes`? | `{"base","files":[…],"stat","patch","truncated"}` |
| `git.log` | `base`?, `limit`? | `{"commits":[{"sha","subject","author","date"}]}` |

- `git.diff` with no `base` is the task's whole change: the working tree
  against its merge-base with the remote's default branch (`origin/HEAD`,
  else `origin/main`, `origin/master`), the diff a hand-off is judged on.
  `"base":"HEAD"` is the uncommitted changes alone. `files` is `--name-only`;
  `patch` is capped at `max_bytes` (256 KiB, at most 4 MiB). No base to
  compare with answers an empty `base` and no files.
- `git.log` is the newest `limit` commits (20, at most 200) of HEAD, or of
  `base..HEAD`.
- A `base` is a revision name that cannot begin with a dash; anything else is
  `bad_request`, and so is a workspace that is not a git checkout.

### `POST /exec/commit_push`

```json
{"workspace":"repos/app/task-1","message":"feat(auth): refresh tokens (tt-12)","branch":"feature/tt-12","github_token":"<optional>"}
```

Commits everything the working tree holds (`git add -A`) onto `branch` and
pushes it to origin (`git push -u origin HEAD`).

- **The branch is cut here when it does not exist**, from what is checked out
  with the working tree carried along: a task's first run starts on the
  default branch when the task branch is not on origin yet. A checkout on
  another branch while `branch` exists here too is `conflict` and nothing is
  touched — switching would leave that work behind. `branch` may not be the
  remote's default branch (`conflict`), and must be a branch name that cannot
  begin with a dash.
- **Credentials are this computer's own.** `github_token`, when sent, reaches
  git only as `GIT_CONFIG_COUNT`/`GIT_CONFIG_KEY_0`/`GIT_CONFIG_VALUE_0`
  (`http.https://github.com/.extraheader`) in the push's environment — never
  argv, never the checkout's config — and the commit is authored as that
  token's GitHub user; it is scrubbed out of the answer. Without one, git uses
  whatever this computer's own login is.
- Nothing to commit is not an error: the branch is still pushed.

Answer: `{"branch","branch_created","head_before","head","committed","subject","files":[…]}`
— `files` is the task's changed files after the commit, `subject` the head
commit's.

Failure codes: `bad_request`, `conflict`, `upstream` (the push failed; with
`"reason":"workflow_scope"` when GitHub refused it because the token may not
change `.github/workflows` — nothing but a better token helps), `timeout`
(10 minutes by default), `cancelled`.

## Runs that name an index

With `index` on `agent.run`:

1. **Pass.** If the run has a workspace, its checkout is indexed first
   (incrementally, exactly as `index.ensure` does), with `index_progress`
   events and then an `index_ensure` step. The run waits for the pass up to
   `index.wait_ms` (default three minutes, at most thirty). A pass that takes
   longer goes on in the background, and the step says `still_indexing`; it
   also outlives the run, so the next run finds the index ready. Only shutdown
   stops it.
2. **Context.** The index context the server's own runs get (relevant chunks,
   repository tree, symbol skeleton) is injected before the first turn: after
   the history for a board run, before the turn being answered for a chat. A
   chat rewrites its query with the run's own provider and model (metered like
   any other call); a board run does not rewrite, and nothing else is called.
   An `index_context` step reports it.
3. **Tools.** `codebase_search`, `expand_symbol_context` and
   `get_symbol_skeleton` (by symbol as well as by file) are served from this
   computer's index, scoped to the run's repository and branch: the branch's
   own index once complete, the base index until then.

None of this can fail the run. No embedder, a failed pass, or no finished
index make the run go on without context or index tools, and the
`index_ensure` / `index_context` step carries the `error`.

## Local code index

| piece | how |
|---|---|
| store | the executor's own embedded Postgres 17, started on first use under `data_dir/index-pg` (cluster in `postgres/`, scratch in `runtime/`), on a free loopback port. It never touches the local edition's `DATA_DIR/postgres`, and a cluster left running by a crash is adopted on the next start. The normal migrations run, and `postgres.IndexStore` is the store: chunks, symbols, edges, file hashes, pgvector when available. Each `repo_key` gets a placeholder `repositories` row keyed by a stable UUID derived from the key. It is a cache: deleting `index-pg` costs one full pass per repository. |
| indexer | the server's `indexer.Service` (tree-sitter chunks of at most 150 lines, each embedded from at most 4,000 characters; incremental by content hash, with a git-diff shortcut). |
| embedder | `embeddings_base_url`, pinned to the one model its `/v1/models` lists (asked again until it answers). Every index is stamped `model@source`, e.g. `nomic-embed-text-v1.5@onnx-int8`; a search against an index stamped otherwise is `not_ready`, and the next pass re-embeds it whole. |
| branches | a task branch's first pass copies the base index's rows (and their provenance), then embeds only the files that differ. With no usable base, the most recently indexed branch of the same repository is copied instead. A base index built with another model or engine is dropped rather than copied. Each repository keeps its 8 most recently indexed branch indexes. |
| search | the server's hybrid search (`SearchChunksHybrid`) and injector (`indexer.Injector`). |

Code and vectors never leave the computer: the embedder must be on loopback,
and nothing in the index is sent anywhere but to a run's own model as context
or tool output.

A lighter store is possible: a single file per repository (chunks, hashes and
vectors in Go, brute-force cosine) would drop the second Postgres. It is not
built: the Postgres path reuses the server's store, indexer, hybrid search and
their tests unchanged.

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
- **Local, when the run names an index:** `codebase_search`,
  `expand_symbol_context`, and `get_symbol_skeleton` by symbol (it replaces
  the by-file one).
- **Withheld from the remote side:** `download_file`, `start_task_preview`,
  `commit_task_changes`, `deploy_release`, `codebase_search`,
  `expand_symbol_context`, `get_symbol_skeleton`, `browser_*` and
  `mcp_filesystem_*`. Served from the cloud, these would act on the server's
  disk, processes or index. The index-backed three are withheld even when the
  run names no index: such a run goes without them rather than search an index
  of another copy of the code.

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
