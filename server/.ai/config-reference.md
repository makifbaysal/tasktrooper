# Configuration Reference

Config path defaults to `resources/config.yml`; override with `-config`. `${VAR}` is
substituted in the YAML and again after unmarshaling for nested fields. Secrets:
see `.env.local.example`.

## `llm`

| Key | Type | Default | Description |
|---|---|---|---|
| `base_url` | string | `""` | Bootstrap-only OpenAI-compatible `/v1` endpoint; providers are added in the UI and stored in the DB |
| `model` | string | `""` | Bootstrap-only model; normally empty — each agent carries its own |
| `api_key` | string | `""` | Bootstrap-only key for the fallback endpoint |
| `max_iterations` | int | `10` | Max agent loop iterations |
| `run_max_total_tokens` | int | `0` | Mid-run circuit breaker over `PromptTokens+CompletionTokens` of one run; ends it at the next iteration boundary through the exhausted-budget path. `0` disables |
| `timeout` | duration | `120s` | LLM HTTP timeout |

**Provider catalog** (`domain.AllLLMProviderDefinitions`, not YAML-configured): every declared
provider is `Available:true` today, including the four host-executed CLIs (`claude_code`,
`cursor_agent`, `antigravity`, `opencode` — see their own sections below). `local_runner` is a
leftover provider type for the old hosted product's remote-Mac embeddings path; it is absent from
this catalog and plays no part here. This product's own local embedder is the separate `local`
provider type, bootstrapped from `EMBEDDINGS_BASE_URL` — see the `embedding` section below.

## `server`

| Key | Type | Default | Description |
|---|---|---|---|
| `port` | int | `8080` | Listen port |
| `api_key` | string | `""` | Legacy single API key (`${SERVER_API_KEY}`) |
| `api_keys[]` | array | `[]` | Legacy config-file client keys; new keys are created in the UI and stored hashed. The desktop app clears both fields and sets `SERVER_API_KEY` itself |
| `public_base_url` | string | `""` | Origin a Claude Code session calls back on for the MCP endpoint (`${PUBLIC_BASE_URL}`); empty defaults to `http://<listen-addr>` |

## `storage`

| Key | Type | Default | Description |
|---|---|---|---|
| `postgres.dsn` | string | `""` | PostgreSQL DSN (`${POSTGRES_DSN}`) |
| `postgres.max_conns` | int | `10` | Pool size |
| `sessions.ttl` | duration | `24h` | Session expiration |

Empty `postgres.dsn` disables sessions, jobs, audit and RAG persistence.

## `jobs`

| Key | Type | Default | Description |
|---|---|---|---|
| `max_concurrent` | int | `3` | Worker pool size |
| `timeout` | duration | `10m` | Per-job execution timeout |

## `rag`

| Key | Type | Default | Description |
|---|---|---|---|
| `enabled` | bool | `false` | Enable file upload and RAG |
| `storage_dir` | string | `./data/files` | On-disk file storage |
| `chunk_size` | int | `1000` | Characters per chunk |
| `chunk_overlap` | int | `200` | Overlap between chunks |
| `top_k` | int | `5` | Chunks injected into chat context |

The embedding model is not configured here: the UI's LLM settings pick it and it is
pinned on the shared client (`MultiProviderClient.SetEmbeddingModel`).

## `tools.default_policy`

Merged with per-request and per-API-key policies.

| Key | Type | Description |
|---|---|---|
| `allow_mcp_servers` | []string | Whitelist MCP server IDs |
| `allow_tools` | []string | Whitelist tool names (wildcards supported) |
| `deny_mcp_servers` | []string | Blacklist MCP server IDs |
| `deny_tools` | []string | Blacklist tool names (deny wins) |

## `tools.terminal`

| Key | Type | Default | Description |
|---|---|---|---|
| `enabled` | bool | `false` | Enable `run_terminal` |
| `working_dir` | string | `"/tmp"` | Default working directory |
| `timeout` | duration | `60s` | Command timeout |
| `sandbox.mode` | string | `off` | `allowlist`, `blocklist`, or `off` |
| `sandbox.allowed_commands` | []string | `[]` | First-token allowlist |
| `sandbox.blocked_patterns` | []string | `[]` | Substring blocklist |
| `sandbox.restrict_working_dir` | bool | `false` | Restrict `working_dir` to the configured path |

## `tools.search`

| Key | Type | Description |
|---|---|---|
| `enabled` | bool | Enable `web_search` |
| `max_results` | int | Max search results |

No provider key: `web_search` queries DuckDuckGo and falls back to Bing when
DuckDuckGo answers with a bot wall.

## `tools.web`

| Key | Type | Default | Description |
|---|---|---|---|
| `enabled` | bool | `false` | Enable `fetch_url` |
| `max_response_bytes` | int | `1048576` | Max response size |

### Outbound URL guard (`ALLOW_LOOPBACK_TOOL_URLS`)

Every tool that dials an LLM-chosen URL — `fetch_url`, browser tools, search
providers, MCP HTTP endpoints, health probes, job callbacks — goes through
`internal/platform/urlguard`: it refuses loopback, link-local, RFC1918, ULA, CGNAT,
multicast and the embedded-IPv4 forms `net.ParseIP` misses, pins the dial to the
vetted IP (so a DNS rebind cannot slip past) and re-checks every redirect hop. Its
transport sets `Proxy: nil`, because a proxy hides the real destination —
self-hosted users behind an HTTP proxy lose these tools.

`ALLOW_LOOPBACK_TOOL_URLS` is a **process env var, not a config key**, so nothing with
only server config or API access can flip it — only whoever launches the process can.
It opens loopback only; metadata, RFC1918 and CGNAT stay shut.

| Surface | Unset | Effect |
|---|---|---|
| `fetch_url`, search, MCP, health probes, job callbacks | loopback **blocked** | set true to allow |
| browser tools | loopback **allowed** | set false to block |

The browser default is inverted because the QA agent boots the app it just built on
`127.0.0.1:PORT` and already holds `run_terminal` on this machine.

## `tools.mobile`

Connects a real device to the `mobile_*` tool set through Appium (`mobile_launch_app`,
`mobile_tap`, `mobile_type_text`, `mobile_swipe`, `mobile_screenshot`,
`mobile_read_ui`, `mobile_wait_for`, `mobile_press_button`, `mobile_rotate`,
`mobile_unlock_device`, `mobile_release_device`).

| Key | Env | Description |
|---|---|---|
| `hub_url` | `MOBILE_APPIUM_HUB_URL` | Appium server (service address in a cluster, `http://127.0.0.1:4723` locally) |
| `device_udid` | `MOBILE_DEVICE_UDID` | Which device; `100.x.y.z:5555` for wireless adb |
| `platform_version` | `MOBILE_PLATFORM_VERSION` | Optional capability. Not used for simulators — the version is derived from the simctl runtime (`iOS 17.4` → `17.4`) |
| `device_pin` | `MOBILE_DEVICE_PIN` | Screen-lock PIN; Android only |
| `auth_token` | `MOBILE_APPIUM_TOKEN` | Bearer token for the hub |
| `bridge_url` | `MOBILE_BRIDGE_URL` | The adb sidecar (`cmd/device-agent`); `remote_adb` devices only |
| `bridge_token` | `MOBILE_BRIDGE_TOKEN` | Bearer token for the sidecar |

### Device kinds (`kind`, migration 102)

`kind` is optional in the settings API; empty means `remote_adb`.

| `kind` | What | `device_addr` | `device_udid` | Runs where |
|---|---|---|---|---|
| `remote_adb` (default) | A physical phone over the adb bridge | Tailnet `host:port` | The loopback address the bridge assigns | Anywhere |
| `ios_simulator` | An `xcrun simctl` simulator on this machine | Simulator UDID | Same UDID | macOS with Xcode only |
| `android_emulator` | An AVD on this machine | AVD name | The adb serial at boot (`emulator-5554`) | Any host with adb |

- Local kinds need no new env: `xcrun`, `adb`, `emulator` are looked up on PATH, then
  under `ANDROID_HOME`/`ANDROID_SDK_ROOT` and the Android Studio directory. A host with
  none of them (i.e. every Linux node) reports no local devices.
- The iOS refusal is per host: still refused for `remote_adb` (XCUITest needs macOS
  with Xcode); for `ios_simulator` the host is checked for an actual simulator.
- `GET /v1/settings/mobile-devices/local-catalog` returns what the host can drive:
  `{"ios":[{"udid","name","runtime"}],"android":["Pixel_7_API_34"]}`. Registration is
  validated against this catalog — a local device cannot be typed in by hand. With
  none, the answer is `200` with two empty arrays, not `404`.
- There is no `enabled` key: with `hub_url` or `device_udid` empty the tools are not
  registered at all.
- **Devices are shared.** Appium does the leasing; a busy device counts as `blocked`,
  the task parks, and `DeviceSweeperInterval` (10 min) resumes it from the column it
  left once the device frees up. A lease drops after 5 min of inactivity;
  `mobile_release_device` releases immediately. Phones, simulators and emulators are
  separate leases in one pool.
- **The tool set does not vary by kind**; the only difference is the capability set in
  `capabilitiesFor` (`ios_simulator` → XCUITest + `appium:bundleId`; Android →
  `appPackage`, `autoGrantPermissions`, PIN unlock).
- `mobile_launch_app` opens only the package registered on the deploy target
  (`repository_deploy_targets.app_package`, `app_url` for the artifact); a human
  writes those fields — a guard the agent can write is not a guard.

## `tools.boilerplate_catalog`

| Key | Type | Default | Description |
|---|---|---|---|
| `enabled` | bool | `false` | Enable `search_boilerplate_catalog` |

Which repo it searches is the admin setting `boilerplate_catalog_repo`
(`GET/PUT /v1/settings`), not config. Defaults to `github.com/makifbaysal/boilerplates`; accepts `owner/repo`, `github.com/owner/repo`
or a full URL. Reads `<repo>/.ai/catalog.yaml` off `main` (falls back to `master`) on
every call — no restart needed.

## `tools.max_tool_output_chars`

| Key | Type | Default | Description |
|---|---|---|---|
| `max_tool_output_chars` | int | `16000` | Max chars of a tool result sent back to the LLM |

## `orchestration`

| Key | Type | Default | Description |
|---|---|---|---|
| `enabled` | bool | `true` | Enable multi-agent orchestration |
| `fast_path` | bool | `true` | Skip orchestration unless `orchestrate: true` |
| `max_parallel_tasks` | int | `3` | Parallel subtask limit |
| `max_plan_tasks` | int | `10` | Max tasks per plan |
| `skill_retrieval_top_k` | int | `5` | Top skills for the planner via embedding search |
| `subtask_history_mode` | string | `isolated` | `isolated` or `full` session history per subtask |
| `dependency_output_max_chars` | int | `4000` | Truncate dependency results in subtask prompts |
| `synthesis_enabled` | bool | `false` | LLM synthesis of the final orchestration output |

## `context`

| Key | Type | Default | Description |
|---|---|---|---|
| `max_tokens` | int | `32000` | Context budget |
| `reserve_output` | int | `4096` | Reserved for completion |
| `summarize_threshold` | int | `24000` | Rolling summary threshold |
| `keep_recent_messages` | int | `10` | Messages kept during trim |

## `mapping` / `indexer` / `graph`

| Key | Default | Description |
|---|---|---|
| `indexer.query_rewrite` | `false` | Rewrites the task text into 2 extra code-search queries (multi-query retrieval) |
| `indexer.allowed_roots` | `[]` | Roots a repository or session may be pointed at, **in addition to** `storage.sessions.workspace_root`, which is always allowed |

`allowed_roots` only ever WIDENS the set. Empty means nothing extra — not
"anywhere", which is what it used to mean and what let `POST /v1/repositories/open`
index any readable directory on this host. Set it only when this install keeps
its checkouts outside the managed workspace.

pgvector: migration 036 tries `vector` + `pg_trgm` (image `pgvector/pgvector:pg16`).
With them, workspace chunk search uses an HNSW index and text search a trigram+RRF
hybrid; without, the in-Go cosine fallback stays on.

## `embedding`

Paces embedding calls for every caller (indexer, RAG upload, query rewrite) — they
share one `MultiProviderClient` and therefore one quota. Unpaced, a repository index
fires one call per chunk and a hosted provider answers `429`, failing the index at the
first rejected chunk.

| Key | Type | Default | Description |
|---|---|---|---|
| `requests_per_minute` | int | `0` | Cap on embedding calls; `0` = unthrottled. Provider allows N req/s → set `N*60` |
| `max_retries` | int | `5` | Retries after `429`/`503`; negative disables |
| `retry_backoff` | duration | `2s` | First wait after a `429`, doubled per attempt; used when no `Retry-After` is sent |
| `max_retry_wait` | duration | `60s` | Cap on one wait, `Retry-After` included |
| `request_timeout` | duration | `90s` | Budget for one call; the indexer's per-chunk deadline derives from this plus retry waits |
| `query_cache_entries` | int | `2048` | LRU of query vectors keyed by (provider, model, exact text). `0` = default, negative disables |

A `Retry-After` (or backoff wait) also delays the *following* calls, so one rejection
slows the stream instead of producing a burst of new ones. The query cache sits
outside `RecordingClient` (`CachingEmbedder` → `RecordingClient` → provider), so a hit
is never billed; index-time chunks share it but `indexer/incremental.go`'s SHA-256
dedup keeps them from evicting query entries.

Code defaults suit a local embedding model (unthrottled). The shipped `config.yml` is
tuned for Mistral — `requests_per_minute: 55`, `max_retries: 8`, `request_timeout: 60s`
— whose free tier allows 1 req/s and sends no `Retry-After`. Throttling counts
requests, not tokens (55 req/min with `chunk_max_lines: 150` stays under the 500k
tokens/minute limit).

**Local embedder model.** `domain.PinnedLocalEmbeddingModel` = `nomic-embed-text-v1.5`
(`domain.PinnedLocalEmbeddingDimensions` = `768`) is the model `Service.BootstrapEmbeddings`
pins the `local` provider to on a fresh install, from `EMBEDDINGS_BASE_URL` — the desktop app's
bundled embedder (any local OpenAI-compatible embeddings host serving the same model works too,
in dev). Pinned because a vector from another model is not comparable to what is already
indexed; not YAML-configured. `embedding_llm_provider`/`embedding_llm_model` empty ("auto")
resolves to it once `EMBEDDINGS_BASE_URL` bootstraps the `local` provider.

## `tools.mcp_servers[]`

| Key | Type | Required | Description |
|---|---|---|---|
| `id` | string | yes | Server ID; tools namespaced as `mcp_<id>_<tool>` |
| `enabled` | bool | yes | Connect on startup |
| `transport` | string | yes | `stdio` or `http` |
| `command` | string | stdio | Executable |
| `args` | []string | stdio | Command arguments |
| `env` | map | no | Subprocess environment |
| `url` | string | http | MCP HTTP endpoint |
| `headers` | map | no | HTTP headers |
| `allowed_tools` | []string | no | Tool whitelist; empty = all |

## Evolution (`evolution`)

Periodic reflections, KPI evaluation, impact tracking.

| Key | Default | Description |
|---|---|---|
| `enabled` | `false` | Master switch for the evolution ticker |
| `allow_web_research` | `false` | Reflection runs with `web_search`/`fetch_url` |
| `tick_interval` | `10m` | Ticker cadence (impact sweep, KPI sweep, due-reflection check) |
| `reflect_interval` | `24h` | Minimum gap between periodic reflections per agent |
| `revision_debounce` | `30m` | Minimum gap between revision-triggered mini-reflections |
| `impact_window` | `168h` | Observation window before/after a change |
| `min_events_for_impact` | `3` | Fewer after-events → `insufficient_data` |
| `max_skill_changes` / `max_rule_changes` | `3` | Per-reflection change caps |
| `max_skills_per_agent` | `25` | Standing skill budget; at budget a `create` is rejected (merge/update/delete first). Also caps the agent's own `create_skill` |
| `max_rules_per_agent` | `15` | Standing rule budget; same behaviour |
| `golden_gate` | `false` (config.yml ships `true`) | Golden suite before AND after applied changes; an independent judge keeps or rolls back the whole set |
| `max_memory_changes` | `5` | Per-reflection memory change cap |
| `memory_max_count` | `200` | Per-agent memory cap; oldest evicted |
| `evidence_max_chars` | `24000` | Reflection evidence truncation |
| `model` / `provider_type` | _(empty)_ | Reflection + golden eval override (a cheap model is recommended) |
| `judge_model` / `judge_provider_type` | _(empty)_ | Independent model that makes the golden-gate decision; the reflection model when empty |

## Board quality gates (`board`)

| Key | Default | Description |
|---|---|---|
| `verification_enabled` | `false` | Post-run build/vet verification + automatic fix loop |
| `verify_max_fix_attempts` | `2` | Fix attempts; still broken → the task returns to `in_progress` + a system comment |
| `require_criteria_complete` | `false` | AC gate: a task with open criteria cannot move to `ready_for_qa`/`done`/`released` |
| `task_type_models` | `{}` | Model override by task type, e.g. `{analiz: small-model}` |
| `pipeline_gate_timeout` | `45m` | Upper bound on waiting for the build/test result in `code_review`; after it the reviewer is assigned anyway (`gate_reason=timeout`). **Must exceed `pipelineMaxWait` (30m)** — otherwise a live pipeline about to answer is abandoned. The remaining 15m is the allowance for host restarts and the Actions queue |
| `pipeline_gate_interval` | `2m` | How often `PipelineGateSweeper` re-asks GitHub; every unfinished pipeline is one round-trip. It sweeps once immediately at boot — a restart is the most common way to lose the poller |

### QA pipeline (admin settings / repository fields, outside `config.yml`)

| Key | Scope | Default | Description |
|---|---|---|---|
| `pipeline_container_runtime` | Admin settings | `""` (`auto`) | `podman`/`docker`/`auto`. With neither, the container build stage is skipped. A change needs a restart — `PipelineRunner` resolves it once |
| `verify_command` | Repository | `""` | Inline verify-gate command; auto-detect when empty |
| `build_command` | Repository | `""` | QA pipeline build override; when empty, auto-detect or (with a Dockerfile) a container build |
| `test_command` | Repository | `""` | QA pipeline test override; auto-detect when empty |

## Prod ops (`prod_ops`)

| Key | Default | Description |
|---|---|---|
| `monitor_enabled` | `true` | Probes deploy targets' `health_url` |
| `probe_interval` | `1m` | Two consecutive failed probes open an incident; the first successful probe closes it |

## Deploy ops (`deploy_ops`)

| Key | Default | Description |
|---|---|---|
| `monitor_enabled` | `true` | Mirrors Actions deploy runs into `deployment_runs`, matches console dispatches, turns a failed deploy into an incident. When off, rollback cannot find the commit to return to and release attribution is impossible |
| `poll_interval` | `2m` | At most one GitHub call per sweep, per (repository × environment) |
| `health_window` | `15m` | How long after a successful deploy an incident is attributed to the releasing task; `auto_rollback` fires inside this window. Deliberately short — it authorizes rolling back a specific card's release. The 45-minute general correlation in `prodops/remedy.go` is independent of it and only writes advice |

## Store ops (`storeops`)

| Key | Default | Description |
|---|---|---|
| `poll_interval` | `5m` | Sweeps store app rows: onboarding checklist, review status, signing-asset renewal. Without a cipher (`MCP_SECRETS_KEY`/`SERVER_API_KEY`) the monitor does not start; the server still boots |

## Claude Code executor (`claude_code`)

Agents whose `provider_type` is `claude_code` run in a headless CLI session
(`claude -p`) on this host, on the subscription that CLI is signed in with — board
tasks and chat alike. Everything around a board run is unchanged (clone, branch,
grounding, verify gate, commit/PR, column advance).

| Key | Env | Default | Description |
|---|---|---|---|
| `binary` | `CLAUDE_CODE_BIN` | `claude` | The CLI to run, resolved on PATH at boot |
| `max_turns` | — | `100` | Turn budget for one session; a session that hits it returns what it has |
| `setting_sources` | — | `project,local` | Which CLI settings files a session loads. The operator's own `~/.claude` (hooks, plugins, permission rules) is out: none of it was chosen for TaskTrooper and all of it would otherwise run inside board tasks. Set `user,project,local` only if this host authenticates through a user-level apiKeyHelper |
| `run_timeout` | — | `1h` | Deadline for one session — the only thing that ever gives up on a wedged CLI, since a subprocess has no provider timeout and the run's heartbeat keeps the row fresh. A plain run failure, never a quota park |
| `max_concurrent_sessions` | — | `3` | How many board CLI sessions run at once. `-1` is unlimited. The subscription's usage limit is shared by every session on the account, so N sessions hitting it in parallel all park together; the cap keeps the burn sequential enough that the sessions that started actually finish. Chat turns never queue on it |

- **No `enabled` flag**: the switch is whether the binary is on PATH. Absent, no
  executor is registered and a `claude_code` run — board or chat — fails with one
  sentence (`domain.ErrHostExecutedProvider`) naming where it *can* run.
- **Model** is a `--model` alias from the curated `domain.ClaudeCodeModels()`
  (`""`, `fable`, `opus`, `sonnet`, `haiku`, `opus[1m]`, `sonnet[1m]`); the CLI does
  not validate it, so a typo costs a run. Empty is a first-class choice — `--model`
  is omitted entirely. See [API → GET /v1/models](api-spec.md#get-v1models).
- **Chat is multi-turn**: the CLI conversation id lives on `sessions.cli_session_id`
  (migration 103) and each turn is `--resume <id>`. A spent usage limit parks a board
  task but becomes a message in chat, naming the local renewal time.
- **The provider cannot be connected, tested, activated or used for embeddings** —
  all of those mean "dial this base URL with this key" and there is neither. It is
  selectable on an agent, which is where it means something.
- **Tool policy** governs the MCP half at execution (`DefinitionsForPolicy`) and the
  CLI's native half through `--tools`; the session is pinned with
  `--strict-mcp-config` and told its exact `mcp__tasktrooper__*` names. See
  [Architecture → the tool endpoint](architecture.md#the-tool-endpoint-mcp).
- **The MCP endpoint has no configuration of its own**: mounted whenever the executor
  is registered, on the server's own port, with one bearer token per run (or per chat
  turn), minted at the start and revoked at the end.
- **Child environment** is `internal/platform/childenv`, an allowlist (so no
  `DATABASE_URL`, `SERVER_API_KEY` or `MCP_SECRETS_KEY`) plus an explicit passthrough of
  `CLAUDE_CONFIG_DIR`, `CLAUDE_CODE_OAUTH_TOKEN` and the proxy variables.
  `ANTHROPIC_API_KEY` is deliberately **not** forwarded: it would hand a child a
  secret it was never given and move the session onto metered billing.

## Antigravity executor (`antigravity`)

Agents whose `provider_type` is `antigravity` run in a headless `agy -p` session on this
host (`internal/adapter/agentcli/antigravity`). No `max_turns`/system-prompt flag exists on
the CLI, so a run's whole history is folded into one prompt; MCP tools reach the session via
`.agents/mcp_config.json`, written into the workspace only for the run's lifetime.

| Key | Env | Default | Description |
|---|---|---|---|
| `binary` | `ANTIGRAVITY_BIN` | `agy` | Resolved on PATH at boot |
| `run_timeout` | — | `1h` | Deadline for one session |

No `enabled`/`max_turns` flags — same "binary on PATH is the switch" rule as `claude_code`.

A session that reports AGY's own "quota reached" (with its `Resets in <duration>` countdown,
when present) parks the task the same way a Claude Code usage limit does — see
`architecture.md`'s quota-park section for the shared mechanics and the confidence caveat.

## Cursor executor (`cursor_agent`)

Agents whose `provider_type` is `cursor_agent` run in a headless `cursor-agent -p --force`
session (`internal/adapter/agentcli/cursor`). Its catalog (role + skills) was already
rendered into `.cursor/rules/*.mdc` by `agentfs.FlavorCursor`; MCP tools reach the session by
merging into `.cursor/mcp.json` — the same file the Cursor IDE reads — restored to its exact
original bytes when the run ends, since a repository may already have one committed.

| Key | Env | Default | Description |
|---|---|---|---|
| `binary` | `CURSOR_AGENT_BIN` | `cursor-agent` | Resolved on PATH at boot |
| `run_timeout` | — | `1h` | Deadline for one session |

Auth is probed via `cursor-agent status`, not a real turn — the CLI reports it directly.

A session that reports cursor-agent's own usage-limit wording parks the task the same way a
Claude Code usage limit does — see `architecture.md`'s quota-park section for the shared
mechanics and the confidence caveat.

## OpenCode executor (`opencode`)

Agents whose `provider_type` is `opencode` run in a headless `opencode run` session
(`internal/adapter/agentcli/opencode`). No agentfs flavor beyond the shared `.claude/skills`
directory: OpenCode's own docs confirm it reads that layout, but nothing confirms a custom
role file becomes the ACTIVE persona from `run`, so role + rules go into the prompt instead.
MCP tools reach the session via `OPENCODE_CONFIG_CONTENT` (inline JSON env var) — nothing is
written to the workspace at all.

OpenCode 2.x (`@opencode/cli`, `opencode --version` prints `opencode v2.…`) runs every
`opencode run` on a shared background service that was started with someone else's
environment, so it never sees that env var. On 2.x each run therefore gets a private
`opencode serve --stdio` started with the config (tools direct, not behind code mode), waits
until `/api/mcp` reports the `tasktrooper` server connected, and attaches with
`opencode run --server`; the server's lease ends with the run. The version is read once per
binary and re-read when the file changes.

| Key | Env | Default | Description |
|---|---|---|---|
| `binary` | `OPENCODE_BIN` | `opencode` | Resolved on PATH at boot |
| `run_timeout` | — | `1h` | Deadline for one session |

A known upstream bug can end a run without its final `step_finish` event; a clean exit with
real output is treated as success rather than a hard failure.

A session whose `error` event relays a provider rate limit parks the task the same way a
Claude Code usage limit does, always on the default window — see `architecture.md`'s
quota-park section for the shared mechanics and why OpenCode never gets a real reset time.
