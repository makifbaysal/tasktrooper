## runner

The program the desktop app runs on a member's computer in **account mode**
(`../src/main/runner/`). In local mode it does not run at all: the local
backend is `../../server`, supervised by `../src/main/supervisor/`.

- Module `github.com/makifbaysal/tasktrooper/runner`, Apache-2.0 like the rest
  of this repository (root `LICENSE`). It has its own `go.mod` because it is a
  Go program inside a TypeScript package, not a separate deliverable.
- The subscription rules are absolute: a CLI runs only on its owner's
  computer, for its owner's own work, and there is no shared runner pool.
  `CLAUDE_CODE_OAUTH_TOKEN` and `~/.claude` never go to any server. The Claude
  Code binary runs unmodified. By default OpenCode never runs with an
  Anthropic or Google provider (`policy.go`), and there is no Antigravity
  flavor.
- **Rules a service may change live behind configuration, never in code
  paths that disappear.** `policy` in the stdin document (`policy.go`) replaces
  the OpenCode provider refusals or turns off the scrubbing of this process's
  own credential environment; absent, every rule is the strict default. What
  this program itself holds — a run's MCP token, the providers' keys — is
  scrubbed whatever the policy says.
- The member's own API keys arrive from the desktop app on stdin
  (`providers`), go to the local executor on ITS stdin, and live in memory in
  both. They never cross the tunnel, never reach argv, an environment block, a
  file or a log line, and are scrubbed out of everything forwarded back.

A single-binary daemon. It dials out to the control plane over WebSocket,
holds that connection open as a reverse tunnel, and serves HTTP over the yamux
streams the control plane opens on it. The cloud coordinates; this computer
executes: Claude Code / OpenCode / Cursor sessions, checkouts, the local
executor (an agent run with the member's own keys), the devices attached to
this machine, and what this machine can do.

**Some of what it does, only it CAN do**, and that is the shape of everything
here. A Linux pod cannot run an iOS simulator; a cloud process resolving a
repository's toolchain overlay resolves it against a path that exists on this
machine and not in a pod; a member's API key may not leave their computer.
Those are capabilities that have nowhere else to live, and they are RPC
methods reached down the tunnel rather than local servers somebody else
discovers.

It lives inside the desktop app: `../src/main/runner/` starts it, hands it its
configuration, reads its logs, and stops it. It has no other caller and is not
published or run on its own.

## Layout

```
main.go          config, the control channel, logging, the tunnel and its reconnect loop
policy.go        the rules a paired service may change: OpenCode's refused
                 providers, the environment-credential scrubbing
executor.go      the local executor: start, readiness, restart with backoff,
                 the stop, and agent.run / llm.complete forwarded to it
executor_checkout.go  the post-run half forwarded to it the same way: verify
                 (streamed), git.status / git.diff / git.log, commit_push
rpc.go           the HTTP surface: routing, the streamed response, the status
                 mapping, the session's registry for mobile.release's cancel
runs.go          durable runs: the process-wide registry claude.run,
                 opencode.run, cursor.run, agent.run and verify hand off to, the seq on
                 every frame, the on-disk frame ring, run.attach, run.status,
                 the 30-minute retention and the shutdown
session.go       claude.run — spawn, stream, kill the process group
opencode_run.go  opencode.run — the same shape as session.go, minus the MCP
                 file (an env var instead) and the provider refusal that keeps
                 OpenCode off a Claude or Gemini subscription
cursor_run.go    cursor.run — the same shape again, with the two places
                 cursor-agent's own CLI forces a departure from it: the prompt
                 is argv (cursor-agent has no stdin mode), and the MCP server
                 is merged into the workspace's own .cursor/mcp.json rather
                 than a temp-dir file (cursor-agent has no --mcp-config flag)
redact.go        the scrubbing claude.run, opencode.run and cursor.run each
                 attach to their output: this process's own credential env
                 values, plus a run's own MCP token
mcp.go           the per-run MCP config file: the grammars, the write, the
                 removal, and the sweep for what a killed process left behind
mcp_surface.go   a CLI run's local tool surface: the executor's mcp.open before
                 the run, the CLI handed its loopback URL and bearer, mcp.close
                 after, and the fallback to the cloud's URL
workspace.go     workspace.prepare and workspace.ensure — clone or fetch, and
                 the git argv gates
toolchain.go     toolchain.detect — the pin files, and what they translate into
embeddings.go    embeddings.create — the local embedding engine's proxy, model
                 pinned, not_ready when this Mac has none configured
mobile.go        mobile.* — simctl and the Android SDK, and the Appium proxy
appium_hub.go    the Appium hub this runner starts on demand (appium_bin) and
                 stops when idle or on shutdown; adopts one already answering,
                 and takes back one its record proves a killed runner started
appium_hub_unix.go  that record's primitives: ps's start time and command
                 line, and stopping a hub that is not this process's child
                 (appium_hub_windows.go: none — the job object ends the hub)
mobile_release.go  mobile.release — a store build, streamed; the signing
                 material's own on-disk lifecycle and its redactor, which
                 redact.go generalises for the other three streamed methods
models.go        models.list — this Mac's live model catalog for cursor-agent
                 and opencode (claude_code's is a curated cloud-side constant;
                 antigravity is not a flavor here)
spawn.go         the one place a short-lived helper is run and its group reaped
proc.go          the process group as one killable thing; proc_unix.go is
                 Setpgid and kill(-pgid), proc_windows.go a job object with
                 KILL_ON_JOB_CLOSE — the only files that know which OS this is
launcher.go      how a configured CLI is exec'd: itself, or on Windows an npm
                 .cmd shim read and replaced by node + its script
paths.go         the one function that decides what "inside the workspace" means
preflight.go     preflight.report — relays what the supervisor pushed

main_test.go     config, the control channel, logging, backoff arithmetic, and
                 that every key the supervisor sends is a field this binary
                 declares (read out of ../src/main/runner/env.ts)
tunnel_test.go   the session and the reconnect loop, driven against an
                 in-process control plane (real HTTP, real WebSocket, real
                 yamux client — the transport is never mocked)
session_test.go  streaming, POST /cancel, and a closed stream that leaves the
                 run going until shutdown, against real processes that ignore
                 SIGTERM
runs_test.go     the registry and the ring with synthetic runs (every OS): seq,
                 the disk bound and the gap notice, status for unknown, running
                 and done, replay then retention GC, one live id at a time,
                 the bound on finished runs, shutdown, the capabilities
mcp_surface_test.go  claude.run and cursor.run handed the surface (never the
                 cloud's URL or token), what mcp.open is asked with, mcp.close at
                 the end, both tokens scrubbed from frames and log, the fallback
                 when the executor is absent, predates mcp.open or fails, and
                 local_tools in the preflight
runs_process_test.go  a dropped stream resumed with run.attach (every frame
                 once, in order) and a dropped tunnel the CLI never notices,
                 against real processes and the in-process control plane
opencode_run_test.go  the provider refusal (anthropic and google, before the
                 stream) and the required provider/model form
cursor_run_test.go  the argv shape (the `--` before the prompt, confirmed
                 against the installed binary), the workspace MCP file's
                 merge-and-restore, not_ready with no binary, cancellation
redact_test.go   the redactor itself, and one child per streamed method that
                 prints a credential env value and gets it scrubbed back
mcp_test.go      the config file as the CLI sees it — its flags, its contents,
                 its permissions, and that it is gone after all four endings
                 (and still there while a run outlives its stream)
policy_test.go   the tool policy, the effort level, the extra environment, and
                 the setting sources that are pinned rather than passed
methods_test.go  containment, the git URL gate, the branch a prepare actually
                 checks out (against a real repository), the embeddings pin
                 and its not_ready, the status mapping
mobile_test.go   the inventory, the boot, the shutdown and the proxy, against
                 real simctl/adb/emulator stand-ins that change each other's
                 answers, and a real hub. The three properties: the udid is not
                 what the caller asked for, the proxy does not serialise, and
                 `emu kill` never aims at a phone
appium_hub_test.go  the on-demand hub against a real process: this test binary
                 is the fake appium (TestMain hands it `--address …`), so
                 adopt-vs-spawn, one start for many callers, the cooldown, the
                 idle stop of an owned hub only, close, and that the devices
                 probe never spawns are all asserted on a live /status
appium_hub_unix_test.go  the record: written and forgotten, a leftover taken
                 back (on a call and at startup) and stopped, and every record
                 short of the whole proof leaving the hub alone
mobile_release_test.go  the signing material's own lifecycle and its redactor
toolchain_test.go  every pin file, the constraint-is-not-a-pin rule, and that
                 every name it emits is one claude.run's `env` accepts
rules_test.go    the hard rules below, enforced structurally
executor_test.go the executor against a real child process — this test binary
                 started with no arguments and TT_FAKE_EXECUTOR set: the stdin
                 config line, the listening line, the bearer, forwarding,
                 scrubbing, cancel by name, restart, the protocol check, stop
executor_checkout_test.go  the post-run methods against the same fake: the body
                 forwarded unchanged, the stream scrubbed, a verification
                 cancelled by its id, the push token scrubbed from the answer
runner_policy_test.go  the policy: strict when absent, replaceable, refused when
                 malformed
```

## The wire format

**HTTP/1.1 over the tunnel.** The control plane is the yamux CLIENT — it opens
the streams — and it reaches this Mac with `httputil.ReverseProxy` over an
`http.Transport`, so what lands on a stream is genuine HTTP framing. This
program is therefore an `http.Server` whose listener is the yamux session.

That is what the tunnel was built for: it carried HTTP to agent-server before
this rework, and keeping HTTP keeps status codes, headers and incremental
streaming — all three of which the callers need.

```
POST /claude.run          → 200 application/x-ndjson, streamed
POST /opencode.run        → 200 application/x-ndjson, streamed
POST /cursor.run          → 200 application/x-ndjson, streamed
POST /workspace.prepare   → 200 application/json
POST /workspace.ensure    → 200 application/json
POST /toolchain.detect    → 200 application/json
POST /embeddings.create   → the local embedding engine's own status and body, verbatim (409 not_ready with none configured)
GET  /mobile.devices      → 200 application/json
POST /mobile.boot         → 200 application/json
POST /mobile.shutdown     → 200 application/json
ANY  /mobile.appium/…     → the local Appium hub's own status and body, verbatim
POST /mobile.release      → 200 application/x-ndjson, streamed
GET  /preflight.report    → 200 application/json
POST /models.list         → 200 application/json
POST /agent.run           → the local executor's NDJSON, streamed
POST /llm.complete        → the local executor's status and body
POST /verify              → the local executor's NDJSON, streamed
POST /git.status          → the local executor's status and body
POST /git.diff            → the local executor's status and body
POST /git.log             → the local executor's status and body
POST /commit_push         → the local executor's status and body
POST /run.attach          → 200 application/x-ndjson, streamed (404 unknown_run)
POST /run.status          → 200 application/json
POST /cancel              → 200 application/json
```

| method | body | answer |
|---|---|---|
| `POST /claude.run` | `workspace` (relative), `prompt`, optional `id`, `model`, `permission_mode`, `max_turns`, `session_id`, `resume`, `timeout_ms`, `mcp`, `tools`, `effort`, `env` | NDJSON: one `started`, then `output` events, then exactly one `done` carrying `{workspace, exit_code, signal?, duration_ms}`; 409 `not_ready` when `claude_bin` was not sent |
| `POST /opencode.run` | `workspace` (relative), `prompt`, `model` (REQUIRED, `provider/model`), optional `id`, `resume`, `timeout_ms`, `mcp`, `env` | NDJSON, same shape as claude.run's; by default (`policy`) `model`'s provider may not be `anthropic` or `google` (case-insensitive) — refused before the 200 |
| `POST /cursor.run` | `workspace` (relative), `prompt`, optional `id`, `model`, `resume`, `timeout_ms`, `mcp`, `env` | NDJSON, same shape again; 409 `not_ready` when `cursor_agent_bin` was not sent |
| `POST /workspace.prepare` | `repo_url`, `dir` (relative, REQUIRED), optional `branch`, `depth` | `{path, rel, branch, head, cloned}` |
| `POST /workspace.ensure` | `dir` (relative, REQUIRED — one or more path segments under `agents/`) | `{path, rel}` |
| `POST /toolchain.detect` | `workspace` (relative, REQUIRED) | `{v, workspace, path, pins[], env{}, read[]}` |
| `POST /embeddings.create` | OpenAI-shaped: `input`, optional `model`, `encoding_format` | the local embedding engine's response, unchanged; `409 not_ready` when this Mac has none configured |
| `GET /mobile.devices` | — | `{v, ios[], android[], android_running[], capabilities{}}` |
| `POST /mobile.boot` | `kind`, `id`, optional `timeout_ms` | `{v, kind, id, udid, platform_version?, name?, already_booted, duration_ms}` |
| `POST /mobile.shutdown` | `kind`, `id` | `{v, kind, id, was_running}` |
| `ANY /mobile.appium/…` | whatever an Appium client sends | whatever the hub answers, verbatim |
| `POST /mobile.release` | `workspace`, `platform`, `channel`, `script`, `script_sha256` (REQUIRED), optional `secrets`, `build_number`, `rollout`, `skip_upload`, `timeout_ms` | NDJSON, same shape as claude.run's; the signing material in `secrets` is redacted out of every line of it — see mobile_release.go |
| `POST /models.list` | `flavor` (`cursor` or `opencode` — not `antigravity`, which this runner does not drive; `claude_code`'s list is a cloud-side constant and never reaches this Mac) | `{v, flavor, output}` — the CLI's own raw stdout, unparsed |
| `GET /preflight.report` | — | the desktop app's environment report, verbatim |
| `POST /agent.run` | the executor contract's body (`server/.ai/executor.md`): optional `id` (the call id, as claude.run's), `run_id` (REQUIRED), `kind`, `agent`, `prompt` or `messages`, `workspace` (relative, must exist), `mcp`, `timeout_ms` | the executor's NDJSON unchanged, in claude.run's envelope (`{"v":1,"id":…,"event":"started"}`, `"event":"event"` with a `payload`, one `"event":"done"`), scrubbed of the keys this runner holds; 409 `not_ready` with no executor |
| `POST /llm.complete` | `{provider_id, model, system, messages, max_tokens}` | the executor's status and body, verbatim but scrubbed |
| `POST /verify` | `id` (REQUIRED, the call id), `workspace` (relative), optional `commands`, `verify_command`, `quality`, `env`, `timeout_ms` — `server/.ai/executor.md` | the executor's NDJSON unchanged (`verify_stage` / `verify_output` events, one `done` whose `result` is the verdict), scrubbed; 409 `not_ready` with no executor |
| `POST /git.status`, `/git.diff`, `/git.log` | `workspace` (relative), and `base`, `name_only`, `max_bytes`, `limit` per route | the executor's status and body, verbatim but scrubbed |
| `POST /commit_push` | `workspace` (relative), `message`, `branch`, optional `github_token` | the executor's status and body, verbatim, scrubbed of the keys and of `github_token` |
| `POST /cancel` | `{"id": "…"}` | `{"v":1,"id":"…","cancelled":true|false}` |

`POST /cancel` names a streamed call: `claude.run`, `opencode.run`,
`cursor.run`, `mobile.release`, an `agent.run` by its `id` (its `run_id`
when it was sent none), or a `verify` by its `id`. Every other
method is cancelled by the caller closing the connection, which `http.Server`
turns into a cancelled request context — the same trigger, without an id to
| `POST /run.attach` | `{"id": "…", "after_seq": N}` (`after_seq` optional, 0) | NDJSON: the run's frames with `seq > after_seq` from its buffer, then live, until its `done`; a `gap` line first when the buffer no longer holds some of them; 404 `unknown_run` for an id this machine holds no run for |
| `POST /run.status` | `{"id": "…"}` | `{"v":1,"id":"…","state":"running"|"done"|"unknown","last_seq":N}` |
| `POST /cancel` | `{"id": "…"}` | `{"v":1,"id":"…","cancelled":true|false}` |

`POST /cancel` names a streamed call: `claude.run`, `opencode.run`,
`cursor.run`, `mobile.release`, a `verify`, or an `agent.run` by its `id` (its
`run_id` when it was sent none). The five durable runs (below) stop on nothing else
but their own timeout and this runner shutting down. Every other method is
cancelled by the caller closing the connection, which `http.Server` turns
into a cancelled request context — the same trigger, without an id to
register. `mobile.boot` is the long one and is the reason this is worth stating:
closing the request stops the WAIT and the polling; it does not un-boot a device
that has already come up. `mobile.shutdown` is how a device goes back down.

### The streamed response

`claude.run` is the reason this is a stream and not a document. It runs for
minutes and produces output the whole time; buffering it would leave the cloud
blind for the length of the task, which is the experience this rework exists to
end. One JSON object per line, **flushed as each is written**:

```
{"v":1,"id":"c-91","event":"started","seq":1}
{"v":1,"id":"c-91","event":"output","stream":"stdout","data":"…","seq":2}
{"v":1,"id":"c-91","event":"done","ok":true,"result":{"exit_code":0},"seq":3}
{"v":1,"id":"c-91","event":"done","ok":false,"error":{"code":"…","message":"…"},"seq":3}
```

`seq` is on every frame of a durable run (below) and on no other stream
(`mobile.release` has none).

- **The first line is always `started`, and it carries the call id.** When the
  caller chose one it is echoed; when it did not, this is the only way it ever
  learns the id — and without the id it could never cancel. It also says the
  wait for a session slot is over, which "no output yet" cannot.
- **Exactly one `done`, always, and it is the last line.** A caller that has
  read it knows there is nothing more. `call.finish` drops a second one rather
  than writing it.
- **A line too long to forward is CUT, not dropped, and never stops the
  stream.** `--output-format stream-json` puts a whole tool result on one line,
  so a Read of a real source file exceeds any limit worth setting. Past
  `outputLineLimit` the rest of that line is consumed and counted and the event
  carries a marker saying how much was cut; the next line is forwarded
  normally. The consuming is the load-bearing half: a forwarder that stops
  reading leaves the child blocked on a 64 KiB pipe, and a blocked child never
  reaches EOF, never lets `streams.Wait()` return, and never gets as far as the
  code that would kill it — the call hangs forever with a live `claude` in it.
  `session_test.go` drives a real over-long line and asserts the lines AFTER it
  arrive.
- **`id` is on every line.** This side invents one only when the caller did
  not, so the two ends' logs can always be joined.
- **A failure after the 200 lives in the `done`, not in the status.** The status
  was decided before the work started, which is why callers switch on the
  `code` string and not on the number.

### Durable runs — `run.attach` and `run.status`

`claude.run`, `opencode.run`, `cursor.run` and `agent.run` **outlive the
stream that started them and the tunnel session that carried it.** The cloud's
load balancer cuts a WebSocket at an hour and tasks run longer than that; a
reconnect used to kill every run in flight. (`verify` belongs on this list
too, but neither this runner nor the executor has such a method yet; a new
long method joins by going through `queueDurable`/`serveDurable`.)

- **Two phases.** Until a run has its session slot it is the request's: the
  id is reserved (a second live run under it is `bad_request`), and a caller
  that goes away or cancels while it is queued takes it with it — it never
  started, `run.status` says `unknown`, and the caller sends it again. Once it
  has the slot and its MCP file (or, for `agent.run`, the executor's 200), it
  is the registry's (`state.runs`, `runs.go`): its context descends from the
  runner's, not the request's, and the request only streams it.
- **A run stops on `POST /cancel`, its own `timeout_ms`, or this runner shutting
  down, and on nothing else.** A caller that stops reading, a session that
  ends, a tunnel that drops: the run goes on, and so does its MCP file, which
  is removed when the run ends. Shutdown (stdin EOF or SIGTERM) cancels every
  run beside the drain, and `main` waits for them — inside `drainBudget`, as
  before — then removes every buffer. The session semaphore is the registry's
  too, so a run that outlived its session still holds its slot.
- **`seq` is the runner's, on every frame**: 1 for `started`, contiguous, the
  `done` last. It is added as the last top-level field of the frame's own
  bytes (`withSeq`), so an executor frame keeps its `payload.seq` and is
  otherwise forwarded unchanged, and a `seq` a frame already carried is the
  one a decoder drops.
- **Every frame lands in a bounded ring on disk** before any caller sees it, so
  a live stream and a replay read the same thing. Segment files of a quarter
  of the cap (`run_buffer_max_bytes`, 16 MiB by default) under
  `<runner_data_dir>/runs/<random>/`, 0700 and 0600; the oldest whole segment
  goes when the total passes the cap, and the newest is never evicted, so the
  `done` is always there. Frames are scrubbed before they are buffered: no
  token reaches the disk this way. A disk that refuses moves that run's
  buffer to memory, with a warning.
- **`POST /run.attach {id, after_seq}`** streams the frames with
  `seq > after_seq`, then live ones, until the `done`; any number of callers
  may attach at once. When the buffer no longer holds some of them, the first
  line says so and carries no seq of its own:
  `{"v":1,"id":"c-91","event":"gap","from_seq":1,"to_seq":2047}`. An attach to
  a finished run replays it and ends. A caller that closes an attach ends
  that attach and nothing else. An id this machine holds no run for — never
  started, forgotten, or from before a restart — is 404 `unknown_run`.
- **`POST /run.status {id}`** is `{"v":1,"id":"c-91","state":"running","last_seq":214}`;
  `state` is `running`, `done` or `unknown` (with `last_seq` 0).
- **A finished run is kept 30 minutes**, then forgotten and its buffer
  removed; at most 64 finished runs are kept, the oldest dropped first. A new
  run under a finished run's id replaces it. A restarted runner knows no runs:
  it sweeps what an earlier one left under `runs/` at startup.
- **`preflight.report` says so**: `"capabilities":["run.attach","run.status"]`
  is appended to the desktop app's report (the app's bytes are otherwise
  untouched), so the cloud can feature-detect before it relies on either.
  So is `"local_tools":[…]` (see the local tool surface below) once an
  executor that serves surfaces has answered.

The cloud's loop: read the stream, remember the last `seq`; on a broken stream
or a new tunnel session, `run.status`; `running` or `done` → `run.attach` with
that `seq`; `unknown` → the run is lost (or never started) and is the cloud's
to send again.

### `mcp` — how a session gets TaskTrooper's own tools

Optional, and it is what lets a run tick an acceptance criterion, record a
verdict or move a board card. When a task ran in the cloud, agent-server
mounted its `/mcp` endpoint on loopback and handed the CLI a per-run token;
runs happen on the user's Mac now, so the control plane sends the endpoint and
the token WITH the call.

```json
"mcp": {
  "url": "https://<public base>/api/mcp",
  "token": "<per-run bearer token>",
  "server_name": "tasktrooper"
}
```

- **The token goes in a FILE, never in argv.** This is the same rule the prompt
  already obeys and for the same reason: argv is world-readable on macOS, so a
  per-run bearer token on a command line is a credential in every `ps` on the
  machine for the length of the run. Only the file's PATH is an argument. The
  test asserts it from inside the child — the fake CLI echoes its own `$*`.
- **The file lives exactly as long as the run, however the run ends.** Success,
  a non-zero exit, `POST /cancel`, and this runner shutting down are four
  different code paths out of the run, and the removal is the cleanup
  `serveDurable` runs after the run's body returns — or at once, when the run
  never starts — so all four pass through it. A dropped stream or tunnel is
  not an ending: the run goes on, and its file with it. `mcp_test.go` asserts
  each of the four separately — a test that covered only the happy path would
  miss the three endings that matter.
- **A per-run directory in the user's temp dir**, named from `crypto/rand`,
  mode 0700, with the file inside it 0600. This is the one thing this program
  puts on disk outside the workspace root; see the hard rules for why.
- **The one exit that cannot run a `defer` is a SIGKILL**, so `sweepMCPConfigs`
  removes leftovers at startup — directories with the `tasktrooper-mcp-` prefix
  that belong to this uid. It is safe there because the desktop app runs one
  runner and drains it before starting another.
- **`--strict-mcp-config` is passed on EVERY run**, with or without an `mcp`.
  Without it the CLI also loads the MCP servers the person who owns the Mac
  configured for themselves, and somebody's personal servers turning up inside
  a work task is both a surprise and a scope nobody granted. A run with no
  `mcp` therefore gets a config file with no servers in it — which is "no
  TaskTrooper tools" written down, not a default invented here.
- **Validated before the 200**, like everything else that can be refused: the
  url must be absolute `https` with no userinfo (the token is a bearer
  credential) — except loopback (`127.0.0.1`, `localhost`, `[::1]`), where
  plain `http` is also accepted for local trials, because a url that never
  leaves this machine cannot put the token on a network wire either way —
  the token printable ASCII with no whitespace (it becomes an HTTP
  header value, and a newline in one is a header injection), and `server_name` a
  plain identifier (it is a key in a file the CLI parses and half of every tool
  name the model calls). A malformed object is `bad_request`, not a broken
  config file and a failure minutes later.
- **Absent is valid.** It means this run gets none of those tools. Nothing here
  invents a url or a token the cloud did not send.
- **`tool_policy` and `index` are optional and forwarded, never read here**:
  JSON objects of at most 64 KiB, for the local tool surface below.

#### The local tool surface — `mcp.open` before a CLI run

The cloud's MCP serves coordination tools (board, documents, criteria,
memory, `ask_user`); tools that must act on THIS computer — `browser_*`,
`codebase_search`, `get_symbol_skeleton` and `expand_symbol_context` (the
executor's local code index), `download_file`, `http_request` to the member's
own localhost — cannot come from there. So when a `claude.run`, `opencode.run`
or `cursor.run` carries an `mcp` and this runner has an executor, once the run
has its slot (`mcp_surface.go`):

1. `POST /exec/mcp.open` with `{run_id: <call id>, workspace, tool_policy?,
   cloud_mcp: {url, token, server_name}, index?, env?, timeout_ms?}`.
   `tool_policy` is `mcp.tool_policy` when the cloud sent one, else the MCP
   half of `claude.run`'s `tools` with the `mcp__<server>__` prefix removed
   (none when that half is empty); `index` is `mcp.index`; `env` the run's;
   `timeout_ms` the run's own plus `drainBudget`, so the surface cannot end
   before the run's kill sequence has.
2. The executor answers `{url: "http://127.0.0.1:<port>/mcp", token,
   server_name}` — the same `server_name`, so `mcp__<server>__<tool>` names
   and `--allowedTools` are unchanged. The CLI's MCP config (the file for
   claude, the env for opencode, the workspace's `.cursor/mcp.json` for
   cursor) gets that url and token **instead of the cloud's**: the cloud's
   bearer never reaches the CLI; the executor holds it and proxies the
   coordination tools with it.
3. `POST /exec/mcp.close {run_id}` when the run ends, however it ends —
   beside the MCP file's removal. The surface also ends at its own timeout
   and with the executor.

**Anything short of a surface falls back to today's behaviour** — the cloud's
url and token in the CLI's config: no executor, an executor answering 404
(`unsupported_method`, one that predates `mcp.open`), one not ready, one
that cannot reach the cloud, or an answer that is not a loopback URL and a
well-formed token. The run never fails for want of a surface. **Both tokens
are scrubbed** from everything forwarded (`MCP_TOKEN`, `MCP_LOCAL_TOKEN`),
and neither is logged; a refusal from the executor is scrubbed of the cloud
token before it is logged.

**`preflight.report` carries `"local_tools":[…]`**, the executor's own
`local_tools` from its health answer: what a surface here serves of its own,
so the cloud can stop withholding those tools from runs on this computer.
Absent with no executor or one that serves no surfaces.

**TODO — the member's own MCP servers.** A member's stdio or http MCP servers
on this computer are not on the surface; `--strict-mcp-config` keeps them out
of every run, deliberately. They would plug into the same surface rather than
the CLI's config: the executor already speaks to MCP servers
(`server/internal/adapter/mcp`, stdio and http clients) and registers their
tools under `mcp_<server>_<tool>` names that a tool policy's
`allow_mcp_servers` scopes. So: the desktop app sends the member's chosen
servers (commands and env on the runner's stdin, never argv; OAuth tokens
from the app's own store), the runner hands them to the executor on its
stdin, the executor connects them per surface (or keeps one connection per
server) and registers their tools on the surface's registry beside the local
ones, and the cloud's `tool_policy.allow_mcp_servers` decides which a run
gets. Nothing here would need a second config file or a second token.

### `tools`, `effort`, `env` — and the flag that is not a parameter

Three optional parameters and one unconditional flag. They exist because a run
on somebody's Mac was silently losing things a run in the cloud had.

```json
"tools": ["Read", "Bash", "mcp__tasktrooper__update_criterion"],
"effort": "high",
"env": { "GOTOOLCHAIN": "go1.24.0" }
```

- **`tools` is the agent's policy, and it is SPLIT across the CLI's two flags.**
  The wire stays one flat array — which flag understands which name is a fact
  about the CLI, not about the policy. Measured against claude 2.1.220 rather
  than read off `--help`:

  | argv | what the session is offered |
  |---|---|
  | `--tools Read,Bash` | built-ins Read and Bash, and EVERY `mcp__` tool |
  | `--tools Read,mcp__tt__update` | built-in Read only; the `mcp__` entry does nothing |
  | `--tools ""` | no built-ins; `mcp__` tools intact |
  | `--allowedTools mcp__tt__update` | nothing removed — it is a permission grant |
  | `--disallowedTools mcp__tt__record` | that one tool removed |

  So native names go to `--tools` and `mcp__` names to `--allowedTools`. An
  unknown name in `--tools` is IGNORED, not refused, which is why sending MCP
  names there read like a policy and was a no-op.

  **A policy naming no built-in gets `--tools ""`, never no flag.** Omitting it
  means the CLI's whole built-in surface, Bash included — the accidental
  widening this parameter exists to prevent, arriving through the back door of
  a naive split. An **empty array is still refused** for the same reason.

  **What this does not do is narrow the MCP surface, and nothing here can.**
  Only `--disallowedTools` removes an `mcp__` tool, and it needs the COMPLEMENT
  of the allow-list — this side never learns the server's full tool list. The
  MCP half stays enforced by the server the per-run token authorises, which is
  where the scoping already lives. Each entry passes the plain-identifier
  grammar and each half is joined with commas into ONE argument, unambiguous
  because the grammar forbids a comma, a space and a leading dash.
- **`effort`** is the CLI's own knob under the same grammar. This side owns
  only the guarantee that the value is a word and not an option.
- **`env` extends the child's environment; it never replaces it — and it is
  an ALLOWLIST.** A repository that pins its own Go or Node version is honoured
  here the way it is locally, and **that is the whole of what this parameter is
  for**. Names must be environment-variable names AND must be on the list in
  `checkEnv` (version pins, a few non-interactivity knobs) or under the
  reserved `TT_` prefix. Everything else is refused, with the list in the
  message.

  It was a denylist and the denylist did not hold. `GIT_EXEC_PATH` (git runs
  its helpers from there and prepends it to PATH), `GIT_TEMPLATE_DIR` (hooks
  copied into every clone), `GIT_DIR`/`GIT_WORK_TREE` (git pointed outside the
  workspace root), `XDG_CONFIG_HOME` (reinstates `git/config`, so
  `core.sshCommand`, after `HOME` and `GIT_CONFIG*` were refused),
  `HTTPS_PROXY` (repoints all traffic including the model's, after
  `ANTHROPIC_BASE_URL` was refused for that), `NODE_PATH`, `PYTHONPATH`,
  `PERL5OPT`, `RUBYOPT`, `JAVA_TOOL_OPTIONS`, `GOFLAGS`, `CC`, `LESSOPEN` —
  every one got through. The list is not the point: every language runtime and
  every tool a session shells out to ships its own "load this file" variable,
  and a repository adding a tool adds names nobody here has heard of. A
  denylist claims the set is closed and it is not, so the rule below could not
  be true while this was one. Values are appended last, so os/exec's
  last-occurrence-wins makes the caller's value beat an inherited one
  deterministically.
- **`--setting-sources project,local` is passed on EVERY run and is not a
  parameter.** With the flag left off, the CLI's default also loads the user's
  own `~/.claude`, so a `SessionStart` hook or a plugin somebody installed for
  themselves executes inside a board task on their Mac. That is the same class
  of problem as a repository's `.mcp.json`, and it gets the same answer as
  `--strict-mcp-config` above — the two exist for one reason and should be
  changed together. It is unconditional on purpose: **a security property the
  caller has to remember to ask for is not a security property**, and the cloud
  omitting a field once would silently run somebody's hooks. `policy_test.go`
  asserts it on a call that sends no parameters at all, and asserts that the
  value does not name `user`.
- **`--disallowedTools AskUserQuestion` is passed on EVERY run too.** In
  `--print` nobody answers the CLI's own question card and the pod never sees
  it, so a question asked through it is lost; the MCP `ask_user` is the one the
  pod carries back to the human. `policy_test.go` asserts it on a call with no
  parameters.
- **All three are refused before the 200**, like everything else that can be
  refused. Absent means no flag and no extra environment, which is exactly
  today's behaviour for a caller that has not been changed.

### `opencode.run` and `cursor.run` — the other two host-executed CLIs

Same shape as `claude.run`: refuse everything refusable before the 200, one
`started`, `output` events flushed as they arrive, exactly one `done`, kill the
process **group** on cancellation, share the same concurrency semaphore and
drain accounting. What differs is each CLI's own contract, never this
program's rules.

- **`opencode.run`'s `model` is REQUIRED and must be `provider/model`.**
  OpenCode has no curated model list the way `claude_code`'s is (a cloud-side
  constant); a run that fell back to whatever OpenCode picked by default would
  be a run this Mac cannot account for. The provider is checked
  case-insensitively and two are refused outright, before the 200:
  `anthropic` (Anthropic models run through `claude.run`, which is what keeps
  a Claude subscription authenticating only the unmodified `claude` binary)
  and `google` (Gemini subscription use is off) — the default `policy`; a
  service that sends `policy.opencode_refused_providers` replaces the list. `github-copilot/…`, `openai/…`,
  `opencode/…` and `openrouter/…` are examples of what is allowed.
- **`cursor.run`'s prompt is an argv element, not stdin — the one place this
  method departs from the other two, and not by choice.** cursor-agent has no
  stdin-prompt mode; confirmed against the installed binary, not read off
  `--help`. What this runner adds beyond mirroring cursor-agent's own argv
  contract (`backend/agent-server/internal/adapter/cli/cursor/executor.go`) is
  a literal `--` immediately before the prompt: without it, a prompt beginning
  with `-` is parsed as an unknown OPTION by cursor-agent's own parser —
  confirmed directly — which is exactly the "an operand that becomes a flag is
  a command" hazard the hard rules below name, landing on the one field here
  that cannot be run through a grammar first because it is the task's own
  text.
- **`cursor.run`'s MCP server lives in the workspace, not beside it.**
  cursor-agent has no `--mcp-config` flag; the only place it reads MCP servers
  from is `<workspace>/.cursor/mcp.json`, so that is where `cursor_run.go`
  writes one — merged into whatever a developer already has there, restored to
  exactly that (or removed, if nothing was there) however the run ends. This is
  the one on-disk credential in this package that is *inside* the workspace
  root rather than the mcp.go exception that lives outside it; cursor-agent
  gives it no other place to be, and the same four-endings discipline that
  protects claude's temp-dir token file protects this one too.
- **`cursor.run`'s `resume` is `--resume <chatId>`**, confirmed against the
  installed binary's own `--help`. This is not what agent-server's *local*
  cursor executor does — it flattens conversation history into a fresh prompt
  every turn and never drives this flag — but that is a decision agent-server
  made for its own calling convention, not a limit of cursor-agent itself.
- **`claude_bin`, `cursor_agent_bin` and `opencode_bin` missing means
  `not_ready`**, the same way a `mobile.*` call answers `not_ready` for a
  capability this Mac lacks: a Mac without Claude Code, Cursor or OpenCode
  installed is a common state, not a misconfiguration — a member may work
  with their own API keys alone (`agent.run`).

### The redactor — secrets never leave through output

`claude`, `opencode` and `cursor-agent` all inherit this process's **full**
environment unmodified (`os.Environ()`, extended by the call's own `env`, never
replaced) — `claude` has to authenticate exactly as it would running locally,
and the other two are under the same rule for the same reason: stripping a
CLI's environment to keep a credential out of its child would also strip the
login that makes it work.

What is scrubbed instead is the **transcript**. `redact.go`'s
`credentialRedactor` builds a redactor, per call, from two sources: this
process's own environment, read for a fixed set of credential-bearing names
(`CLAUDE_CODE_OAUTH_TOKEN`, `ANTHROPIC_API_KEY`, `ANTHROPIC_AUTH_TOKEN`,
`OPENAI_API_KEY`, `GEMINI_API_KEY`, `GOOGLE_API_KEY`, `CURSOR_API_KEY`,
`GITHUB_TOKEN`, `GH_TOKEN`), and the run's own MCP bearer token, when it has
one — both of them, the cloud's and the local surface's, when the run has a
surface (`heldSecretRedactor`). It generalises `mobile_release.go`'s `newSecretRedactor` — the same
short-value floor (nothing under 8 characters is searched for, so a redactor is
never the reason a legitimately short word disappears from a log), the same
per-physical-line matching for a value a forwarder splits across frames, the
same JSON-escaped variant — rather than a second scrubbing pass with its own
rules to drift from that one's. It is attached to `claude.run`, `opencode.run`
and `cursor.run`'s `call.redact`, the same field `mobile.release` already used
for its signing material.

### `mobile.*` — the devices attached to this Mac

A Linux pod cannot run an iOS simulator. That sentence is the whole reason these
methods exist: a Mac is a machine with devices on it, and mobile QA works
because of that and for no other reason. This half used to live in agent-server,
which ran *beside* this program and could exec `xcrun` itself; it was removed
with that server, correctly, because nothing else was driving it. The consumer
is in the cloud now and reaches this Mac down the tunnel.

**`remote_adb` is not here and must never be.** A physical phone reached through
the cluster's adb bridge is a separate path with a separate sidecar, and this
runner refuses that kind BY NAME — a message reading "unknown kind" would send
somebody hunting for a typo in a kind that is perfectly real. The emulator-serial
grammar is the other half of the same rule: `adb -s <serial> emu kill` is only
ever aimed at an `emulator-NNNN`, so nothing here can end a process on a phone
somebody plugged into their laptop. `adb devices` on a Mac with a phone attached
reports it; `mobile.devices` does not.

#### `GET /mobile.devices`

No parameters. A GET, like `preflight.report`, because it is a read.

```json
{
  "v": 1,
  "ios": [
    {"udid":"11111111-…","name":"iPhone 15","runtime":"iOS 17.4","platform_version":"17.4","state":"Shutdown"}
  ],
  "android": ["Pixel_7_API_34"],
  "android_running": [{"avd":"Pixel_7_API_34","serial":"emulator-5560"}],
  "capabilities": {
    "ios_simulators":    {"available": true},
    "android_emulators": {"available": false, "detail": "this Mac has no Android SDK platform-tools (adb), so it cannot reach an emulator"},
    "appium":            {"configured": true, "reachable": true, "on_demand": true,
                          "base_url": "http://127.0.0.1:4723", "drive_path": "/mobile.appium"}
  }
}
```

- **`ios` and `android` are the names agent-server's `domain.LocalDeviceCatalog`
  already uses**, with the same meaning: every usable simulator (booted or not,
  with unavailable ones — devices whose runtime was uninstalled — dropped), and
  the AVD names the SDK defines. `android_running` is additional, and it is the
  one that carries a serial.
- **Every array is non-nil.** `[]` says "this Mac was asked and has none", which
  is a different statement from a null and is the one a settings page renders.
- **A `capability` that is false always carries a `detail`,** and the detail is
  the sentence to put in front of a person. "Unavailable" on its own sends them
  looking in the wrong place.
- **`android_emulators.available` can be true with `android` empty.** adb without
  the `emulator` binary is a real and usable Mac: somebody started an AVD from
  Android Studio and it can be driven perfectly well; what this Mac cannot do is
  START one. The `detail` says exactly that.
- **`appium.configured` and `appium.reachable` are two facts with two remedies.**
  Not configured means no Appium is installed here (an install). Configured and
  not reachable means a call through `drive_path` would not get to a hub: one
  this runner does not start is not answering (a restart), or the hub it starts
  on demand failed its last start (the detail carries Appium's own last lines,
  for the 30 s a call would be handed that failure). `drive_path` is reported
  so no caller has to hold a copy of this runner's routing table.
- **`on_demand` means this runner starts the hub itself** (`appium_bin` was
  sent). Nothing answering is then the ordinary idle state, and it is reported
  as `reachable: true`: read as down, a task parked on a free device would wait
  for a hub that only its own run starts. **This probe never starts a hub** —
  it is polled by settings pages and sweepers, and a probe that started one
  would keep it running forever.
- **A failure on one half never fails the other.** A broken Android SDK must not
  make the simulators disappear; it becomes that half's `detail`.

#### `POST /mobile.boot`

```json
{"kind": "ios_simulator", "id": "11111111-2222-3333-4444-555555555555"}
{"kind": "android_emulator", "id": "Pixel_7_API_34", "timeout_ms": 240000}
```

- **`kind`** — `ios_simulator` or `android_emulator`. Required.
- **`id` is the device's STABLE identity**, in whatever terms its kind makes that
  question answerable: the simctl UDID for a simulator, the AVD NAME for an
  emulator. It is never an adb serial.
- **`timeout_ms`** is optional and bounds the wait for the device to become
  usable. Default 240000 (4 minutes, which a cold AVD genuinely takes); allowed
  range 2000…900000.

`id` here is the DEVICE. `claude.run`'s optional `id` is the CALL, the handle
`POST /cancel` names. They share a spelling and nothing else, and a caller whose
transport splices its own call id into every request's params will overwrite
this one — the cloud side hit exactly that, and asked a Mac to boot a device
named `c-9f3a1b2c-7`. `mobile.boot` and `mobile.shutdown` take no call id;
closing the connection is how they are abandoned.

```json
{"v":1,"kind":"android_emulator","id":"Pixel_7_API_34",
 "udid":"emulator-5560","platform_version":"14","name":"Pixel_7_API_34",
 "already_booted":false,"duration_ms":38211}
```

- **`udid` is the field this method exists for, and it is NOT `id`.** It is what
  goes into `appium:udid` verbatim. For a simulator the two collapse — simctl's
  identity is stable. For an emulator they do not: the AVD is a name and the
  serial it comes up on is allocated by the emulator at boot, so a caller that
  assumed `emulator-5554` would drive whichever emulator is on that console port.
- **`platform_version` is `appium:platformVersion`**, from simctl's runtime for a
  simulator and `ro.build.version.release` for an emulator. Best effort on
  Android: a getprop that failed is not a boot that failed, and the field is
  simply absent.
- **Idempotent, which is what makes it safe at the start of every run.** A device
  that is already up is a success with `already_booted:true`, never a conflict.
  A simulator still goes through `simctl bootstatus -b` either way: `simctl boot`
  returns as soon as the boot has been *started*, and a session created against a
  half-booted device fails in ways that read to an agent as a broken app.
- **Nothing is opened on the user's screen.** No `Simulator.app`, and the
  emulator is `-no-window -no-audio`. The boot was asked for by the cloud on
  behalf of a task, and a window that steals focus every time a QA task starts is
  somebody's afternoon.
- **The on-demand hub comes up beside the device.** A boot is what precedes a
  session, and a cold simulator and a cold XCUITest driver each take long
  enough that paying for them one after the other is noticeable, so the hub's
  start runs concurrently with the boot and the answer waits for both. A hub
  that will not start fails the call with `upstream`, saying the device is up
  and why Appium is not — the retry is a cheap `already_booted`.
- **Refusals, all before the 200:** a `kind` this Mac does not drive, an `id`
  that fails its grammar, a device this Mac does not have — `bad_request`. A
  toolchain that is not installed — `not_ready`, with the sentence naming what is
  missing. A device that will not come up in time — `upstream`.

#### `POST /mobile.shutdown`

Same `kind` and `id`. `{"v":1,"kind":…,"id":…,"was_running":true}`.

Idempotent in the other direction: a device that is already down is a success
with `was_running:false`. The caller's intent is a state, not a transition, and a
4xx for "it is already how you wanted it" would make callers treat a success as a
fault.

#### `ANY /mobile.appium/…` — how a session is driven

**Point an ordinary Appium client at `<this runner's base>/mobile.appium` and it
works unchanged.** Everything after the prefix is the hub's own path, so
`POST /session`, `POST /session/{id}/element`, `GET /session/{id}/screenshot` and
`DELETE /session/{id}` all land where they should. The bare prefix is the hub's
root. Any method, because Appium uses four of them.

- **Verbatim, in both directions.** The hub's status code, its `Content-Type` and
  its body come back untouched, up to 24 MiB — the same bound the caller reads,
  so nothing is cut here that would have survived there. This is the same rule
  `embeddings.create` follows, and for a sharper reason: **Appium's error bodies
  carry the W3C error the caller classifies, and the one that matters most is the
  refusal of a second session against a device that already has one.** Rewriting
  a 4xx from the hub into a failure of this Mac's would turn "the device is busy,
  park the task" into "something broke", which is the difference between a task
  that resumes and one that fails.
- **THE LEASE IS APPIUM'S, and nothing here may become a second one.** This
  runner holds no lock, no queue and no registry of which device is busy.
  Concurrent requests are forwarded concurrently — including two `POST /session`
  for the same device, where the second one gets the hub's refusal. That refusal
  is what makes the lease mutual across the cloud's replicas; a mutex on this
  side would replace a cross-process lock with a per-Mac one and silently break
  the parking path. `mobile_test.go` drives two concurrent creates against a hub
  that answers neither until both have arrived, so an implementation that
  serialised fails rather than passing slowly.
- **`Authorization` is deliberately NOT forwarded**, and neither is anything else
  but `Content-Type` and `Accept`. The hub is a loopback process on this Mac; it
  holds no credential and has no use for one, and handing it a bearer token
  minted for the tunnel is a place for that token to be logged that nobody chose.
  **Do not set a hub token for a Mac-hosted hub** — there is nothing to
  authenticate to.
- **The ceiling is 6 minutes per call**, above the caller's own 3-minute bound on
  session creation, and the request's context cancels it earlier.
- **Every call ensures the on-demand hub first** (after the refusals below, so
  a malformed call starts nothing), and holds it for its whole length — the
  idle stop never pulls the hub out from under a call in flight. That includes
  a `DELETE /session/…` and a `GET /sessions`: a caller that only wants to know
  whether a device is free asks `mobile.devices`, which never starts one.
- **`not_ready` when this Mac has no Appium**, `upstream` when a hub is
  configured and not answering (with the address in the message, because "not
  running" and "running somewhere else" are two causes that only the address
  separates) or when the on-demand hub would not start (with Appium's own last
  lines), and `bad_request` for a path containing a `..` segment.

#### Appium is started ON DEMAND by this runner, not expected on the machine

The decision, and it is a decision rather than an accident. It mirrors what
agent-server's `adapter/local/appiumhub` does for the local edition; this module
cannot import that package, so `appium_hub.go` is a small copy of its rules on
this program's own process layer.

- **The desktop app sends `appium_bin` when Appium is installed**, beside
  `appium_base_url` = `http://127.0.0.1:4723` — Appium's own default, a constant
  rather than an allocated port, the same rule LM Studio's address follows.
  Installed means enabled; there is no toggle and no URL to type.
- **Nothing starts at launch.** A hub is a ~100 MB Node process and most members
  with Appium installed never drive a device. A proxied call or a `mobile.boot`
  starts it; concurrent callers share ONE start (a single flight they all wait
  on, and a caller that gives up does not cancel it). It is spawned without a
  shell through `launcherFor` (an npm `appium.cmd` shim becomes node + its
  script) and `startProcessGroup` (its own process group, or a
  KILL_ON_JOB_CLOSE job on Windows), with `--address <host> --port <port>
  [--base-path <path>] --log-no-colors` from the configured base and nothing
  from a caller, and is ready when `GET /status` answers — up to 45 s, because
  the first start after a driver install loads every driver.
- **Its environment is this process's minus the agent CLIs' credentials**
  (`credentialEnvNames`), plus `ANDROID_HOME`/`ANDROID_SDK_ROOT` derived from
  the `adb_bin` detection found — a GUI-launched runner has none, and
  UiAutomator2 finds adb through it.
- **A start that failed is reported with Appium's last lines** (`upstream`, not
  `not_ready`: agent-server turns `not_ready` into a "no computer" park, and a
  broken Appium install frees itself never) **and not retried for 30 s** — a
  caller in that window is handed the same failure instead of paying another
  45 s for the same answer.
- **If something is already answering on that port, it is ADOPTED** and never
  stopped. A user running their own Appium — with their own drivers and
  plugins — should not have this program fight them for the port.
- **A hub this runner started is stopped after 10 idle minutes** — no proxied
  call in flight and none for that long. Appium sessions are not tracked: the
  cloud creates them with a `newCommandTimeout` well under ten minutes, so a
  session nobody has called for that long has already been ended by Appium. The
  idle watch exists only while an owned hub runs.
- **It outlives a tunnel session and not the process.** A thirty-second
  reconnect must not end an Appium session the cloud holds, so the hub belongs
  to `state`, not to a `runnerServer`. Shutdown — stdin EOF or SIGTERM — stops
  it BESIDE the drain rather than after it: the drain cancels every proxied
  call anyway, and the supervisor's grace covers the drain, not the drain plus
  a hub.
- **A hub a killed runner left is taken back, not adopted — when it can be
  PROVEN to be one.** On macOS and Linux a SIGKILLed runner leaves its hub
  running in its own process group, answering on the port, where it would read
  as the member's own and run forever. So the runner writes down the hub it
  started — `{pid, start, hub}`, the start time as `ps -o lstart=` prints it
  (locale and zone pinned) — in `appium-hub.json` under the user's cache
  directory (`os.UserCacheDir()/TaskTrooper/runner`), and deletes it when it
  stops that hub. At startup (`resume`) and on every ensure that finds a hub
  answering, a record is honoured only if ALL of these hold: the same hub URL,
  the pid alive, the same start time, and a command line containing `appium`
  and `--port <our port>`. Then the hub is owned again — the idle stop and
  `close` apply to it — and is stopped by group (`kill(-pid)`, it was started
  with `Setpgid`) after its start time is checked again before each signal,
  which is what keeps a recycled pid from ever being signalled. Anything short
  of that is somebody else's hub, adopted and never stopped. Windows needs
  none of this: the job takes the hub down with the runner.
- **It is a capability, never a dependency.** Nothing waits for it at startup;
  its absence or its crash never becomes the supervisor's state. A Mac with no
  Appium runs every task that does not touch a device.
- **It is in the preflight either way, as an OPTIONAL item** — `appium`, plus
  `appium-xcuitest` and `appium-uiautomator2` once Appium itself is there, plus
  `android-sdk`. Optional because this app cannot know whether this member does
  mobile work, and blocking Connect on a hub somebody may never use would stop a
  person who only ever writes code. Reported because the failure it prevents is
  the late one: a QA task that reaches a Mac with no `xcuitest` driver dies at
  session creation, minutes in, when `appium driver install xcuitest` could have
  been said before anybody pressed Connect.

The alternative — expecting the user to run `appium` in a terminal — was rejected
for that last reason. The preflight could still say "the binary is here", and a
binary that is here and not running is exactly the failure this app's whole
detection philosophy exists to avoid. Without `appium_bin` (an older desktop)
the hub at `appium_base_url` is whoever's runs it, and calls go straight to it.

### `toolchain.detect` — what a checkout says it needs

The cloud cannot answer this. It resolves a repository's toolchain overlay
against a path that exists on this Mac and not in a pod, so `Detect()` there
reads nothing, and the only variable that survives its portability check is
`GOTOOLCHAIN` — because that one is a directive rather than a location. Real
detection has to run where the checkout is.

```json
{"workspace": "repos/acme-api"}
```

`workspace` is required, relative, and goes through `resolveInWorkspace` like
every other path a caller names. It must already exist: answering "pins nothing"
for a checkout that was never made is a lie with the right shape.

```json
{
  "v": 1,
  "workspace": "repos/acme-api",
  "path": "/Users/you/TaskTrooper/repos/acme-api",
  "pins": [
    {"language":"node","version":"20.11.0","exact":true,"source":".tool-versions",
     "env_name":"NODE_VERSION","env_value":"20.11.0"},
    {"language":"node","version":">=20","exact":false,"source":"package.json"},
    {"language":"go","version":"go1.24.3","exact":true,"source":"go.mod",
     "env_name":"GOTOOLCHAIN","env_value":"go1.24.3"},
    {"language":"terraform","version":"1.7.5","exact":true,"source":".tool-versions"}
  ],
  "env": {"NODE_VERSION":"20.11.0","GOTOOLCHAIN":"go1.24.3"},
  "read": [".tool-versions","go.mod","package.json"]
}
```

- **`env` is the point of the whole method: pass it to `claude.run` as its `env`,
  unchanged.** Every name in it is one `checkEnv` accepts, and every value is in
  the form the tool actually takes — `GOTOOLCHAIN` wants `go1.24.0`, not
  `1.24.0`, and that translation lives with the parser rather than in a mapping
  table the cloud would have to keep in step. A test asserts every name this
  method can emit against `claude.run`'s own allowlist, in the same package, so
  the two cannot drift.
- **`pins` is everything found, IN PRECEDENCE ORDER per language**, and two files
  pinning one language BOTH appear. The disagreement is the repository's and is
  reported rather than resolved silently; the order says which one a resolver
  should believe. `env` takes the first exact pin per language, which is that
  order's first-wins answer.
  The order is: `.tool-versions` → `mise.toml`/`.mise.toml` → the language's own
  dotfile (`go.mod`, `.nvmrc`, `.node-version`, `.python-version`,
  `.ruby-version`, `.java-version`, `.sdkmanrc`, `rust-toolchain.toml`,
  `rust-toolchain`, `.fvmrc`, `.fvm/fvm_config.json`) → a manifest's constraint
  (`pubspec.yaml`, `package.json`, `pyproject.toml`, `Gemfile`).
- **`exact:false` is a CONSTRAINT, and it never becomes an environment value.**
  ">=3.11", "^20" and "lts/hydrogen" name sets of runtimes. Resolving one here
  would be this program deciding what a repository meant, minutes before a
  session silently built against it. `rustup`'s channels (`stable`,
  `nightly-2024-03-01`) are exact, because they are what rustup takes.
- **A language with no allowlisted name is reported without one.** `dart`,
  `terraform`, anything a `.tool-versions` happens to name: the declaration is
  real and the pass-through is not available, and saying both is more useful than
  dropping it or inventing a name `claude.run` would refuse.
- **Absence is absence.** A language with no pin file does not appear. Nothing is
  reported as "system", "default" or "latest", because none of those is something
  the checkout said. `read` is what makes an empty answer readable: no files read
  means the checkout declares nothing, and files read with no pins means they are
  there and say nothing this side recognises. Those are different problems.
- **What it reads is what a repository DECLARES, never what it contains.** A
  directory full of `.py` says somebody wrote Python; it does not say which
  Python. Only files whose purpose is to state a version are opened.
- **Only the named directory, never below it.** A monorepo pins per package, and
  walking to find those would mean choosing which of several answers is the
  repository's. A caller that wants a package's pins names that package as the
  `workspace` — `repos/acme-api/web` is a workspace like any other.
- **It installs nothing and can install nothing.** It reads files. It spawns no
  process at all, which is why there is no argv gate in it: the only
  caller-supplied value is the path. A pin file that is a symlink is skipped —
  the names are this program's own, so a link at one of them was put there by
  whatever is in the checkout.
- **Bounded:** 1 MiB per file, 128 characters per version, 128 pins per answer.

### `agent.run` and `llm.complete` — the local executor

The executor is agent-server's headless mode (`server/cmd/executor`, contract
in `server/.ai/executor.md`, built by `npm run build:executor` into
`bin/executor`): an agent loop with the member's
own API keys, local tools in the workspace, coordination tools from the cloud's
MCP. This runner starts it, keeps it running and forwards to it; it never runs
an agent itself.

- **Started at runner startup, when `executor_bin` was sent**, and owned by
  `state`, not by a tunnel session — a reconnect does not restart it. One JSON
  line on its stdin: `listen` (`127.0.0.1:0`), a fresh 32-byte `token` per
  start, `data_dir` (`executor_data_dir`), `workspace_root` (this runner's own
  `workspace_dir`, so `repos/<repo>/task-<id>` means the same folder to both),
  `embeddings_base_url` (the live value, when there is one) and `providers`.
  stdin then stays open; its closing is the executor's shutdown request.
- **Ready means two facts:** the first `EXECUTOR_LISTENING http://127.0.0.1:<port>`
  line on its stdout (refused unless loopback), then `GET /exec/health` with
  the bearer answering `protocol: 1`. Another protocol is fatal and not
  retried — the same binary cannot answer differently.
- **An exit is restarted** with 1 s → 30 s exponential backoff, reset after a
  minute up. A call that arrives while it is starting waits up to 20 s, then is
  `not_ready` with the last failure in the message.
- **Stopped beside the drain**, like the Appium hub: stdin closed, 15 s, then
  the process group killed. That fits inside the supervisor's grace.
- **Forwarding is byte-for-byte except for two things.** The executor speaks
  claude.run's envelope — `{"v":1,"id":…,"event":"started"}`, `"event":"event"`
  frames with a `payload`, one `"event":"done"` — echoing the request's `id`
  (or `run_id`), so the cloud's existing stream reader takes it unchanged.
  Every line is scrubbed of the providers' keys and the run's MCP token (and,
  per `policy`, this process's credential environment). And when the
  executor's stream ends without a `done`, this side writes one in the same
  envelope — `upstream`, or `cancelled` when the caller cancelled — because
  the caller is promised exactly one.
- **Refused before it is forwarded:** a missing or malformed `run_id`, a
  `workspace` outside the workspace root, an `mcp` that fails `checkMCP`.
- **Durable, like `claude.run`.** The request to the executor is made on the
  run's own context, so it outlives the caller's stream; a non-200 from the
  executor is still relayed as the answer's status, before anything starts.
- **Cancellation** is `POST /cancel` naming the `id`, the run's timeout or
  shutdown, and the executor is also told by name
  (`POST /exec/cancel {run_id}`, best effort, 3 s).
- **The executor has no concurrency cap of its own**, so `agent.run` takes a
  slot of the session semaphore like every CLI run.
- `agent.run` shares the session semaphore and the drain accounting with the
  CLI runs; `llm.complete` shares the drain accounting.

### `verify`, `git.*`, `commit_push` — the post-run half

A board run's verify → fix → commit → push → pull request needs the checkout,
which is on this computer, so the cloud drives that half through these five
methods after `agent.run` or a CLI run (`server/.ai/executor.md`, "The
post-run half"). This runner forwards them to the executor exactly as it
forwards `agent.run` and `llm.complete`, and adds nothing but:

- **The refusals it can make first:** a `workspace` outside the workspace
  root, a malformed body or `id`, and, for `verify`, no `id` at all — it is
  the handle `POST /cancel` stops the pass by.
- **`verify` is a run:** it takes a session slot (it builds and tests), is
  registered under its `id` for `POST /cancel`, tells the executor by that id
  (`POST /exec/cancel {id}`) when its caller goes, and gets a `done` of this
  side's when the executor's stream ends without one.
- **`commit_push`'s `github_token` is a held secret for that call:** forwarded
  to the executor, which hands it to git through the environment only, and
  scrubbed out of whatever comes back, like the providers' keys.
- An older runner answers these `unsupported_method` (404), and the cloud
  falls back to the agent committing and pushing itself.

### Cancellation

Every trigger must kill the process **group** — see the hard rules.

1. **`POST /cancel`** naming the call id. The run reserves its id before it
   writes a byte, so a caller that chose an id can cancel one it has not read
   yet, queued or running. A cancel for a call that already finished answers
   `cancelled:false` and is **not** an error: that race is ordinary, and a 4xx
   would make callers treat a successful stop as a fault.
2. **The run's own `timeout_ms`.**
3. **This runner shutting down** (`runRegistry.shutdown`, beside the drain).
4. **For everything that is not a durable run** — and for a durable run still
   queued for a slot — **the client closing the response body.** `http.Server`
   turns that into a cancelled request context, and a session ending cancels
   every request it was serving, because every request context descends from
   the server's `BaseContext`. A started durable run is not a request any
   more: a closed body ends its stream, and the run goes on for a later
   `run.attach`.

A session that is TEARING DOWN also refuses new runs. `drain` latches a flag
under the same lock the run registry uses and waits for the runs in flight, so
a request that arrives mid-teardown is answered `cancelled` rather than
spawning a session nobody will be left to stop. That accounting is a counter
and not a `sync.WaitGroup` on purpose: `Add` racing a blocked `Wait` panics,
and a panic here aborts the daemon — which orphans precisely the process groups
the ordered teardown exists to reap.

The two grace periods are DERIVED from each other. `drainBudget` here
(`claudeGrace + claudeReapTimeout`) is the authoritative one, and
`../src/main/runner/child.ts` computes its own SIGTERM grace from a copy of
it; `rules_test.go` fails if the two drift. They were chosen independently
once — 8s there against 25s here — and quitting the app mid-task therefore
SIGKILLed the runner before it had reaped its `claude` groups.

### Status codes

Each code maps to exactly one status, and the code string is repeated in the
body — `{"v":1,"error":{"code":"…","message":"…"}}` — so a caller may switch on
either and can never see them disagree.

| code | status | |
|---|---|---|
| `bad_request` | 400 | malformed, or naming something outside the workspace |
| `unsupported_method` | 404 | no such path, or the wrong verb on one |
| `not_ready` | 409 | the answer genuinely does not exist yet |
| `unknown_run` | 404 | `run.attach` for an id this machine holds no run for |
| `cancelled` | **499** | the caller asked, or the tunnel went away |
| `upstream` | 502 | something this Mac depends on failed |
| `internal` | 500 | a bug here |

**499, not 408.** A 408 is a REQUEST timeout — the server gave up waiting for
the client to finish sending — and every cancellation here is the opposite: the
request arrived, the work started, and the client went away. 499 is the code
everyone already reads that way.

**Two methods do not produce a status of their own**, and both are proxies to a
process on this Mac. `embeddings.create` passes LM Studio's status back
untouched, because a 404 from it means "that model is not loaded" and
re-labelling that as a 502 from this Mac would send the caller hunting for a
network problem that does not exist. `/mobile.appium/…` passes the hub's status
back for a sharper reason: its 4xx bodies carry the W3C error the caller
classifies, and the refusal of a second session against a busy device is the
lease. In both cases only "could not reach it at all" is a 502 of this Mac's.

`not_ready` (409) now has a second use beside "the preflight has not arrived
yet": **a capability this Mac does not have.** A `mobile.*` call on a Mac with no
Xcode, no Android SDK or no Appium is `not_ready` with the sentence naming what
is missing — not `bad_request`, because the caller did not ask wrongly, and not
`upstream`, because nothing failed. An Appium that IS installed and whose
on-demand hub would not start is the opposite case and is `upstream`:
something failed, and agent-server reads `not_ready` as a park that a broken
install would never release. `cursor.run` and `models.list` answer it the
same way for a missing `cursor_agent_bin` or `opencode_bin`, `claude.run`
for a missing `claude_bin`, and
`embeddings.create` answers it when this Mac has no local embedding engine
configured — an ordinary case, not a misconfiguration.


## Hard rules

- **Nothing reachable from the network.** Every connection to the outside world
  is one this process dials. A Mac behind a home router, with no port forwarded
  and no inbound rule, is exactly the point. The one loopback socket that used
  to exist — the database listener, which replaced Google's cloud-sql-proxy —
  went with the database.

  It **does** run an `http.Server`, and that is not a hole: its listener is the
  yamux session, which yields streams the control plane opened on a connection
  this process dialled outward. What would be a hole is `net.Listen`, and that
  is what `rules_test.go` forbids — the server type is fine, binding is not.
- **No port for anything else.** Not for health checks, not for metrics, not
  for debugging. The supervisor learns this program's state from its log lines,
  which is why they are JSON.
- **Spawning is the job; a shell never is.** This rule USED to be "never spawn
  anything", and running Claude Code sessions is why it changed. What it was
  protecting survives: children are spawned with an argv array and no shell, and
  **nothing a caller supplies reaches argv as free text** — the prompt goes on
  stdin, the MCP token goes in a file, and `model`, `permission_mode`,
  `session_id`, `tools`, `effort`, the names in `env`, `branch`, `dir`,
  `repo_url` and the mobile `kind` and `id` each pass a grammar first. A leading
  `-` is refused everywhere, because an operand that becomes a flag is a command
  — and the emulator binary proves it without a shell anywhere in sight:
  `emulator -avd --help` and `-avd -ports:5554` are argument injection into a
  program parsing its own argv.

  **A Windows batch file is a shell.** CreateProcess on a `.cmd` runs cmd.exe
  on it, which re-parses every argument — cursor.run's prompt is argv — and
  cuts the line at 8191 characters. So an npm `.cmd` shim is never exec'd:
  `launcherFor` reads it and runs `node <script>` instead, and a batch file it
  cannot read that way is `not_ready`. A real `claude.exe` is used as is.

  The mobile grammars are agent-server's own, carried over rather than
  reinvented: a simctl UUID, an AVD name that cannot begin with a dash or a dot,
  and an `emulator-NNNN` serial. `toolchain.detect` adds nothing here because it
  spawns nothing at all.
- **Cancellation kills the process GROUP.** `claude` spawns git, node,
  compilers and test runners; `git clone` starts a transport helper; `adb` forks
  a server. Signalling the parent alone leaves the tree running against a
  checkout nobody is watching. Every child is started through
  `startProcessGroup` (proc.go): on macOS and Linux that is `Setpgid`, and
  cancelling sends SIGTERM then SIGKILL to `-pgid`, through `killProcessGroup`,
  which refuses to signal a pid that has already been reaped — a recycled pid
  belongs to somebody else. It learns that through `reaped` (the caller's
  `exited` channel, closed after Wait, and `os.Process`'s own state), never
  `cmd.ProcessState`, which Wait writes with no lock — `go test -race` holds
  that shut. On Windows it is a job object, killed at once with
  `TerminateJobObject` (there is no SIGTERM, and a child has a windowless
  console of its own, so CTRL_BREAK cannot reach it either); the job is
  KILL_ON_JOB_CLOSE while the child runs, so a runner that dies any way at all
  takes its trees with it. `spawn.go` is the one place a short-lived helper is
  run, so that guard cannot be lost in a copy.

  **One child is deliberately exempt, and it is the only one: the emulator.**
  `emulator -avd X` is not a command that returns once the device is up — it IS
  the emulator, and it has to outlive the request that asked for it, which is the
  entire point of booting one. Tying it to this process would be worse than
  tying it to the call: a thirty-second network drop restarts the runner, and a
  device a parked task is waiting on would be destroyed by a reconnect. An
  emulator's lifetime belongs to the device, not to the tunnel. It still gets
  `Setpgid` — not so this program can kill it, but so a SIGTERM aimed at the
  runner's group on quit does not take a booting emulator with it — and it is
  still reaped, so no zombie accumulates. On Windows it gets no job and breaks
  away from the one Electron put the runner in (`startDetached`). It is released by `mobile.shutdown`, by
  the user, or by a reboot, and by nothing else here.

  **The Appium hub is long-lived and NOT exempt.** It outlives the call that
  started it, but not this process: it is started through `startProcessGroup`
  like every other child and stopped through `killProcessGroup` — after its
  idle time, or on shutdown (`appium_hub.go`).
- **A lock this program does not hold is a lock it must not invent.** The
  device lease is Appium's: a second session against a device that already has
  one fails, and that failure is what makes the lease mutual across the cloud's
  replicas. The Appium proxy therefore forwards concurrent requests concurrently
  and rewrites nothing. A mutex, a queue or a per-device registry here would
  replace a cross-process lock with a per-Mac one, and the symptom would be two
  runs quietly driving one phone.
- **Nothing this program dials leaves the Mac except the tunnel.** LM Studio and
  the Appium hub are both configured as base URLs, and both are refused unless
  the host is loopback (`checkLoopbackURL`). A base URL naming another host would
  turn either proxy into a general-purpose request forwarder aimed by
  configuration — which is the same hole `embeddings.create` was fenced against,
  and `/mobile.appium/…` is wider because it forwards any method and any path.
  The proxy also re-parses the URL it built and asserts the host did not move.
- **Everything on disk is under the workspace root.** `resolveInWorkspace` is
  the only function that decides what that means, and callers name relative
  paths only. `toolchain.detect` reads files under it and opens no symlink: the
  pin-file names are this program's own, so a link at one of them was put there
  by the checkout, and following it reads outside the root through a name that
  looks like it is inside.

  The one exception is the per-run MCP config, and it is an exception on
  purpose: the workspace is a folder the user opens in Finder and may keep on a
  cloud-synced volume — the desktop app warns about that rather than refusing it
  — and neither a sync client nor a Finder window is somewhere a bearer token
  belongs. It goes in a 0700 directory in the user's own temp dir instead. The
  rule it appears to bend is about paths a CALLER names, and no caller names
  this one: the path is generated here and only the runner ever sees it.

  The Appium hub's record is the second, for the same reason and no secret:
  `appium-hub.json` (a pid, a start time, the hub URL) in a 0700 directory
  under the user's own cache directory, written whole by a rename and named by
  nobody but this program.

  The durable runs' buffers are the third: scrubbed transcripts in
  `<runner_data_dir>/runs/<random>/`, 0700 and 0600, named by nobody but this
  program, bounded per run and in number, removed 30 minutes after a run
  ends, at shutdown, and at the next startup.
- **git's transports are not all fetches.** `ext::` takes a command;
  `--upload-pack` names one. `checkRepoURL` allows plain https and ssh and
  nothing else.
- **A credential this program writes down is a credential it deletes.** The
  MCP token is the only one, it is on disk only while its run is, and the four
  endings are tested one at a time. See `mcp` in the wire format above.
- **What a session may load is PINNED here, never asked for.**
  `--strict-mcp-config` and `--setting-sources project,local` are on every run,
  including one whose caller sent no parameters at all. Between them they keep
  the user's own MCP servers, hooks, plugins and settings out of a board task
  running on their Mac. Neither is a field the control plane can send, because
  a security property that depends on the caller remembering it is not one —
  the cloud omitting it once would be somebody's `SessionStart` hook executing
  inside a task. `env` is fenced for the same reason: `CLAUDE_CONFIG_DIR` or
  `PATH` from the wire would undo both flags without touching either — which is
  why `env` is an **allowlist** and not a list of names to avoid. This rule is
  an absolute, and a denylist cannot deliver an absolute: it is a claim that
  the set of dangerous names is closed, and every runtime a session shells out
  to ships another one.

  **The allowlist did not change for either new method, and that is deliberate.**
  Neither takes a caller-supplied environment: `mobile.*` hands its children this
  process's own environment (which carries no credential by construction — the
  configuration is on stdin precisely because a same-user process can read
  another's environment on macOS; the Appium hub's has the agent CLIs' own
  credential variables removed besides), and `toolchain.detect` spawns nothing.
  `toolchain.detect` only ever EMITS names that are already on the list, and a
  test asserts that against `envNameAllowed` rather than restating it.
- **These are tested, not just written down.** `rules_test.go` parses this
  package's own non-test source and fails on every `net.Listen`, on
  `ListenAndServe`, on `syscall.Exec` and on a shell binary appearing as a
  literal — aliased imports included. `session_test.go` enforces the
  process-group rule behaviourally, against a fake CLI that ignores SIGTERM and
  spawns a child, and it does so for both cancellation triggers. `mcp_test.go`
  reads the token file and the argv from inside the child, which is the only
  place both can be seen at once. `policy_test.go` reads the argv and the
  environment from inside the child for the same reason.

## Configuration: stdin, and it stays open

No environment variables. No flags. No config file. The **first line** of stdin
is one JSON document, and stdin then stays open as a control channel.

```json
{
  "tm_base_url": "https://tasktrooper.ai",
  "runner_token": "...",
  "tenant_id": "...",
  "member_uid": "...",
  "workspace_dir": "/Users/you/TaskTrooper",
  "claude_bin": "/opt/homebrew/bin/claude",
  "git_bin": "/usr/bin/git",

  "embeddings_base_url": "http://127.0.0.1:1234",
  "embedding_model": "nomic-embed-text-v1.5",

  "xcrun_bin": "/usr/bin/xcrun",
  "adb_bin": "/Users/you/Library/Android/sdk/platform-tools/adb",
  "emulator_bin": "/Users/you/Library/Android/sdk/emulator/emulator",
  "appium_base_url": "http://127.0.0.1:4723",
  "appium_bin": "/opt/homebrew/bin/appium",

  "cursor_agent_bin": "/usr/local/bin/cursor-agent",
  "opencode_bin": "/opt/homebrew/bin/opencode",

  "executor_bin": "/Applications/TaskTrooper.app/Contents/Resources/bin/executor",
  "executor_data_dir": "/Users/you/Library/Application Support/TaskTrooper/executor",
  "providers": [{"id": "openai", "type": "openai", "api_key": "sk-…", "models": ["gpt-4.1"]}],

  "policy": {"opencode_refused_providers": ["anthropic", "google"], "redact_credentials": true},

  "runner_data_dir": "/Users/you/Library/Application Support/TaskTrooper/runner",
  "run_buffer_max_bytes": 16777216,

  "reconnect_max_backoff": "30s"
}
```

Every field is required except `reconnect_max_backoff`, the five mobile ones,
the two embeddings ones, the three host-executed CLIs' binaries
(`claude_bin` among them), the executor's three, `policy`,
`runner_data_dir` and `run_buffer_max_bytes`, and a
missing or malformed required field is a startup error on stderr with a
non-zero exit — never a zero-value default silently wired in. **Unknown fields
are refused too** (`DisallowUnknownFields`), which is why `main_test.go` reads
`../src/main/runner/env.ts` and checks every key it sends against this
struct: the document crosses two languages with no compiler between them, and it
has already broken once that way. `antigravity_bin` is one such unknown field
on purpose: this runner has no Antigravity flavor.

- **The five mobile fields are optional because their ABSENCE is meaningful.** A
  Mac with no Xcode has no simulators; a Mac with no Android SDK has no
  emulators; a Mac with no Appium cannot drive either. That is a common state,
  not a misconfiguration, so it is carried as an absence — `mobile.devices`
  reports which half is missing and why, and `mobile.boot` refuses with the same
  sentence. An empty string standing in for a path would be an exec of `""`
  inside a call, minutes later, with an error naming nothing. A field that IS
  present must still be an absolute path.
- **`appium_bin` makes the hub at `appium_base_url` this runner's to start**
  (see "Appium is started ON DEMAND" above). It is refused without
  `appium_base_url`, and with one that is not plain `http` — the hub is started
  on that address, and Appium serves no TLS there.
- **`adb_bin` and `emulator_bin` travel separately** because they fail
  differently: adb is how an emulator is talked to and `emulator` is how one is
  started, and a Mac with the first and not the second can drive an AVD somebody
  launched from Android Studio.
- **`embeddings_base_url` and `embedding_model` are optional too, for the same
  shape of reason.** A Mac with no local embedding engine configured is an
  ordinary case, not a misconfiguration, and `embeddings.create` answers `409
  not_ready` for it rather than this program refusing to start. PRESENT and
  wrong is still refused exactly as before — a base URL that is not loopback,
  or (once the embedder starts) a request naming a model that is not the pin —
  because a local engine that exists is still held to the rules a local engine
  is held to. A later `embeddings-base-url` control message (below) is
  accepted whether or not this document set one, which is what lets an
  embedder that started after the runner attached — or restarted on a new
  port — turn the capability on without a reconnect.
- **`claude_bin`, `cursor_agent_bin` and `opencode_bin` are optional for the
  same reason the mobile fields are.** A Mac without Claude Code, Cursor or
  OpenCode installed is a common state — a member may run every task with
  their own API keys through the executor — and `claude.run`, `cursor.run`
  and `models.list` answer `not_ready` for the missing one rather than
  exec'ing `""`. Present, each must be an absolute path. There is no `antigravity_bin`: this runner has no
  Antigravity flavor, so a supervisor sending it fails loudly at startup
  (`DisallowUnknownFields`) instead of this Mac quietly accepting a capability
  it does not implement.

- **`executor_bin` is optional and `executor_data_dir` comes with it.** Absent,
  `agent.run` and `llm.complete` answer `not_ready`. `providers`
  (`{id, type, base_url, api_key, models, timeout_seconds}`) is validated here
  (ids and types are identifiers, a key has no control characters, a
  `base_url` is https or loopback http because the key travels to it, no
  duplicate ids, at most 64) and is never logged; which types exist is the
  executor's to say.
- **`policy` is optional and every field in it is.** See `policy.go`: absent is
  strict, and a malformed provider name or an unknown field is a startup error.
- **`runner_data_dir`** is where this runner keeps the durable runs' buffers
  (`runs/` under it); absolute. The desktop app sends `userData/runner`;
  absent, it is `os.UserCacheDir()/TaskTrooper/runner`, beside the Appium
  record. **`run_buffer_max_bytes`** caps one run's buffer, 64 KiB…1 GiB,
  16 MiB when absent.

- **`member_uid`** is the field teams added. A paired Mac belongs to a MEMBER of
  a tenant, not to the tenant; several Macs may be paired to one tenant, and the
  control plane routes a task's run to the Mac of its assignee. It goes out as
  `X-Runner-Member` beside `X-Runner-Tenant` on the dial.
- **`tenant_id`, not `tenant_uid`.** Every name in this document is the pairing
  bundle's, and the bundle's are the control plane's `runnerBundle`. This one was
  `tenant_uid` for a release — a name kept from when a tenant WAS a Firebase uid
  — and the desktop app's parser looked for it, so it refused every bundle the
  server actually sent and pairing could not succeed at all. Neither repo's
  tests saw it: this side never parsed a real server payload. A name invented on
  this side is a name that can disagree with the wire.
- **Every binary path is passed, not looked up** — `claude_bin`, `git_bin`,
  `xcrun_bin`, `adb_bin`, `emulator_bin`, `appium_bin`, `cursor_agent_bin`,
  `opencode_bin`, `executor_bin`.
  Detection lives in `../src/main/services/detect.ts` and nowhere else; a
  second search here with slightly different rules is how a Mac runs one
  `claude` and reports another, or drives one adb while reporting the SDK of a
  different one.
- **`embeddings_base_url` and `appium_base_url` must be loopback.** Both name a
  process on the user's own machine. A base URL naming another host would make
  `embeddings.create` — or, worse, `/mobile.appium/…`, which forwards any method
  on any path — a general-purpose request forwarder aimed by configuration.
- **`embedding_model` is a pin, not a default.** A request naming a different
  model is refused rather than served: vectors from two models are not
  comparable and need not even share a dimension count.

**Why stdin and not the environment.** On macOS a process running as the same
user can read another process's entire environment block through
`KERN_PROCARGS2` — `ps eww <pid>` does exactly that. A bearer token passed to a
child in its environment is therefore readable by any app the user launches.
`argv` is worse: it is world-readable, which is also why the prompt for a Claude
Code session travels on the CLI's stdin rather than as an argument.

**Why stdin stays open.** It carries the environment preflight, which this
program serves over the tunnel but does not produce — probing lives in the
desktop app, and a second implementation here would drift into describing a
different machine than the one the user is looking at.

**Its closing is a shutdown request**, drained exactly like SIGTERM
(`watchControl`). On Windows there is no SIGTERM to send, so ending stdin is
how the supervisor asks; on every OS it is how this process learns the app
that started it has died. The supervisor therefore holds the pipe open for the
runner's whole life and ends it first in `stop()`.

```json
{"type":"preflight","report":{ … }}
```

A control line this build does not understand is logged and skipped. The tunnel
is the valuable thing this process holds; tearing it down over an unknown
message would make every future control message a compatibility hazard.

## Logging

JSON on stdout, one object per line. Not switchable — the supervisor is the
only consumer, and it parses `tunnel attached`, `tunnel detached` and the
reconnect warning into state it renders itself. Those three message strings are
a contract with `../src/main/runner/runner-log.ts`; changing one is a
two-file change.

The runner token must never appear in a log line. There is a test for it.

**yamux logs through the same logger**, not to stderr where it defaults
(`yamuxLogger` and `yamuxRecord` in `main.go`). Its lines arrive as ordinary
records — `level`, `message`, `time`, no new field names, the `yamux: ` prefix
kept in the message — so the supervisor needs no case for them. Its `[ERR]` and
`[WARN]` tags become the record's level while a session is live, because a
keepalive that failed is the reason the tunnel is about to drop and is worth
seeing. Once shutdown has started they are debug: tearing the session down is
how this program quits, the mux then calling the closed connection an error is
not a fault, and an `ERR` in the log tail on every clean quit teaches people to
ignore the log they will need when something is genuinely wrong.

## Development

```sh
go build ./... && go vet ./... && go test ./...
```

Test files that drive `#!/bin/sh` stand-ins are tagged `//go:build !windows`;
helpers the portable tests share live in untagged files (`logsink_test.go`).
`GOOS=windows go vet ./...` must stay clean. `appium_hub_test.go` is untagged:
its fake appium is this test binary, which `TestMain` (main_test.go) hands to
`fakeAppium` when it is started with `--address`, so it runs on Windows too.

`session_test.go` spends about fifteen seconds waiting out real SIGTERM grace
periods — POST /cancel and a shutdown — and `mcp_test.go` spends fifteen more
on the same two endings, because "the token file is gone" is only worth
asserting after the process group has actually been reaped. That is the cost of
testing a signal escalation rather than asserting that a function was called.

`mobile_test.go` runs against shell stand-ins for `xcrun`, `adb` and `emulator`
that share a state directory and change each other's answers — `emulator -avd`
writes a file and the next `adb devices` reports a device because of it — so a
boot is exercised end to end rather than against a constant the test wrote. Its
concurrency test deliberately has no timeout in the fake hub: a hub that gave up
after a few seconds would let a SERIALISING proxy pass slowly, which is the
implementation the test exists to reject.

The desktop app builds it with `npm run build:runner` (host OS and arch, or
`RUNNER_GOOS`/`RUNNER_GOARCH`, into `../bin/`; `runner.exe` for Windows) and
`npm run build:runner:universal` (macOS arm64+amd64 via `lipo`).
