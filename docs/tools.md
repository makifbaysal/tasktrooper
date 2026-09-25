---
title: Agent tools
description: What each tool an agent can call actually does, and which roles typically hold it.
---

Every agent — a role agent or one you built yourself — acts through a fixed
set of named tools. A [tool policy](tool-policies.md) decides which of these
a given agent may call; this page describes what each tool does. "Typically
held by" reflects the default role policies TaskTrooper ships with
(`backend-developer`, `frontend-developer`, `mobile-developer`,
`system-architect`, `qa-agent`, `product-manager`) — your own agents and
[custom agents](custom-agents.md) can be granted any subset.

## Shell and files

| Tool | What it does | Typically held by |
|---|---|---|
| `run_terminal` | Runs a shell command in the workspace, combined stdout/stderr | Developer roles, `system-architect`, `qa-agent` |
| `read_file` | Reads a workspace file with line numbers (up to 800 lines a call) | Every tool-scoped agent, including on unindexed repositories |
| `write_file` | Creates or overwrites a file | Whichever roles hold `run_terminal` (they travel together) |
| `edit_file` | Finds `old_string` and replaces it with `new_string`, once or everywhere | Same roles as `write_file` |
| `edit_lines` | Replaces, inserts after, or deletes a range of line numbers | Same roles as `write_file` |
| `delete_file` | Deletes a file, or a directory with `recursive: true` | Same roles as `write_file` |
| `move_file` | Renames or moves a file | Same roles as `write_file` |

`.git` and the workspace root itself are refused for every write tool.

## Web

| Tool | What it does | Typically held by |
|---|---|---|
| `fetch_url` | HTTP GET, HTML responses stripped to visible text | Every role that holds `roleWebTools` — developers, `product-manager`, `system-architect`, `qa-agent` |
| `download_file` | Downloads a binary asset (image, font, icon) into the workspace | Rides with the workspace write tools; lost in review/QA verdict columns |
| `web_search` | Keyless web search (DuckDuckGo, falling back to Bing) | Same roles as `fetch_url` |
| `search_boilerplate_catalog` | Searches the shared boilerplate repository for a starter stack to copy instead of writing one from scratch | Developer roles |

## Code exploration

Read-only, and available even on a repository with no semantic index yet.

| Tool | What it does | Typically held by |
|---|---|---|
| `codebase_search` | Semantic search over the indexed codebase | Developer roles, `system-architect` (not `qa-agent` — QA is deliberately black-box) |
| `grep_code` | Literal/regex search over the indexed codebase | Developer roles, `system-architect`, and `qa-agent` (QA's one code-lookup exception, for finding a start command or a route) |
| `get_repo_tree` | ASCII directory tree of the repository | Same as `grep_code` |
| `get_symbol_skeleton` | Signatures of a file or symbol without the full body | Developer roles, `system-architect` |
| `expand_symbol_context` | Pulls in the callers/callees around a symbol (dependency-graph BFS) | Developer roles, `system-architect` |

## Board

| Tool | What it does | Typically held by |
|---|---|---|
| `list_board_tasks` | Lists tasks on the board (repository-scoped) | Every role |
| `list_ready_tasks` | The unblocked `backlog`/`todo` queue, priority-sorted | Every role |
| `create_board_task` | Creates a task | `product-manager` (and any agent granted `roleBoardCreateTools`) |
| `move_board_task` | Moves a task to another column | Every role |
| `update_board_task` | Updates a task's fields (including `before_deploy`/`after_deploy`/`rollback_plan`) | Every role |
| `claim_board_task` | Claims an unassigned task | Every role |
| `delete_board_task` | Permanently deletes a task and everything on it | `product-manager` only |
| `add_task_comment` | Comments on a task | Every role |
| `list_task_comments` | Reads a task's comments | Every role |
| `add_task_document` | Attaches a document (spec, plan) to a task | `product-manager`, `system-architect` (create roles) |
| `update_task_document` | Edits an attached document | Same as `add_task_document` |
| `list_task_documents` | Reads a task's attached documents in full | Every role |
| `attach_task_file` | Links an already-uploaded file to a task | `product-manager`, `system-architect` |
| `list_acceptance_criteria` | Reads a task's acceptance criteria | Every role |
| `set_criterion_completed` | Marks a criterion done | Developer roles (whichever role implements it) |
| `cancel_criterion` | Drops a criterion from scope with a reason | Same roles as `set_criterion_completed` |
| `review_criterion` | Records a reviewer's own verdict on a criterion | `product-manager` (PM UAT), `qa-agent` |
| `record_test_cases` | Writes the QA test round for a task | `qa-agent` only |
| `set_test_case_result` | Updates one test case's result | `qa-agent` only |
| `list_test_cases` | Reads the recorded test round | Every role |
| `get_pipeline_status` | Reads the most recent QA-gate pipeline run | Developer roles, `system-architect`, `qa-agent` |
| `get_board_summary` | Board-wide counts and status | Every role |
| `list_projects` / `create_project` / `update_project` | Initiative projects | Read: every role. Write: `product-manager` |
| `list_repositories` | Lists registered repositories | Every role |
| `set_repository_projects` | Links a repository to projects | `product-manager` |
| `list_team` | Lists board member agents | Every role |
| `get_project_brief` | Reads the structured project model's brief: what a repository/component is, what it runs, what it talks to; fetched on demand, never injected into a run's context | `system-architect`, developer roles, `qa-agent`, `product-manager` |
| `list_component_checks` | Lists each component's CI checks — workflow, purpose, gate, the exact local command that reproduces it | `system-architect`, developer roles, `qa-agent` |
| `list_links` | Lists what a component talks to and what talks to it (other components, system resources) | `system-architect`, developer roles, `qa-agent`, `product-manager` |

## Cloud runtime

Where a component actually runs (a connected Vercel/GCP/AWS account, or a
custom URL) and what it is doing there right now. `get_environment` is the
lookup; the other three need one *bound* environment — an account and a
resource, not just a URL — and refuse with "no `<environment>` environment is
bound for `<component>` — the human connects it on the repository's Deploy
tab" otherwise.

| Tool | What it does | Typically held by |
|---|---|---|
| `get_environment` | Reads one component's environment(s): provider, resource, URL, health, binding status | `system-architect`, developer roles, `qa-agent`, `product-manager` |
| `query_runtime_logs` | Reads an environment's live application logs (not a CI job's output — that is `get_deploy_logs`), newest first | Developer roles, `qa-agent` |
| `list_runtime_errors` | Lists an environment's runtime errors, grouped and deduplicated, with a `new` flag for errors that started in the window | `system-architect`, developer roles, `qa-agent` |
| `list_deployments` | Lists an environment's recent deployments as the provider reports them | Developer roles, `qa-agent` |

## Pull requests

| Tool | What it does | Typically held by |
|---|---|---|
| `get_task_pull_request` | Reads a task's PR: state, files, review comments, diff | Developer roles, `system-architect`, `qa-agent`, `product-manager` (read-only) |
| `commit_task_changes` | Commits, pushes to the task branch, opens the PR if needed — the only way a chat's edits reach GitHub | Developer roles only |
| `comment_on_pull_request` | Posts a PR comment, or replies inside a review thread | Developer roles, `system-architect` |
| `merge_task_pull_request` | Squash-merges the task's PR and deletes the branch, then opens (or joins) a release | `release-engineer` only, and only in the Done column |

See [Git and pull requests](git-and-pull-requests.md) for the full merge
refusal matrix.

## Release

The release engineer's tool surface, from `merge_task_pull_request` onward.
See [Deploy targets and recipes](deploy.md) → "Releases" for the full flow.

| Tool | What it does | Typically held by |
|---|---|---|
| `get_release` | Reads the release covering a task: status, deploy result, health/smoke/error evidence, verdict, rollback | `release-engineer` only |
| `deploy_release` | Dispatches a `dispatch`-mode release, or a cut batch release's executor (tag/local command/store build); a deploy that never actually starts is left `pending` for a retry rather than `failed` | `release-engineer` only, and only Done/Released |
| `watch_release` | Watches a release through its deploy and soak window; parks the task while a system sweeper watches | `release-engineer` only, and only Done/Released |
| `run_smoke_checks` | Runs the release's frozen smoke checks against production right now, read-only | `release-engineer` only |
| `finish_release` | Confirms a release as shipped; the only way a task reaches Released | `release-engineer` only, and only Done/Released |
| `rollback_release` | Rolls a release back off production: reverts the merge, redeploys or lets the provider's own push-to-deploy redeploy (instant provider rollback only for an `on_merge` component); refused while a newer release of the component is open or has already shipped | `release-engineer` only, and only Done/Released |
| `get_deploy_logs` | Reads the log behind a deploy (a release's failed job by default), summarized | `release-engineer` only |
| `list_deploy_templates` | Lists the deploy recipe catalog | `release-engineer`, `product-manager` (via `get_deploy_target`) |
| `load_deploy_template` | Reads one recipe in full | Same as above |
| `get_deploy_target` | How a repository ships to an environment (the legacy per-env address record, unrelated to a component's delivery profile) | `release-engineer`, `product-manager` |
| `update_deploy_target` | Records the address an environment actually answers at (`base_url`/`health_url`/`logs_url`/`app_url` only) | `release-engineer`, `product-manager` |
| `record_local_deploy` | Records a break-glass deploy run made from a machine directly, so the Deployments page still reflects it | Ops-facing agents with board/deploy write access |

`get_deploy_logs` reads a CI/Actions job's output; a bound environment's own live logs and grouped errors come from `query_runtime_logs`/`list_runtime_errors` in [Cloud runtime](#cloud-runtime) instead.

See [Deploy targets and recipes](deploy.md).

## Production incidents

| Tool | What it does | Typically held by |
|---|---|---|
| `list_incidents` | Lists live production incidents | Ops-facing agents |
| `get_incident` | Full incident: alert payload, timeline, occurrences, remedy | Ops-facing agents |
| `propose_incident_remedy` | Records a diagnosis: kind, steps, evidence, confidence | Ops-facing agents |
| `resolve_incident` | Closes an incident after verifying recovery | Ops-facing agents |

See [Production incidents](incidents.md).

## Browser

Loopback-only (a developer starting its own dev server on `127.0.0.1` is the
intended use — see the outbound URL guard in [Data directory and
security](data-and-security.md)).

| Tool | What it does | Typically held by |
|---|---|---|
| `browser_navigate` | Opens a URL | Developer roles, `system-architect`, `qa-agent`, `product-manager` |
| `browser_screenshot` | Captures the current page | Same |
| `browser_click` | Clicks an element | Same |
| `browser_fill` | Fills a form field | Same |
| `browser_read_dom` | Reads the page's DOM/text | Same |
| `browser_wait_for` | Waits for a condition on the page | Same |
| `browser_set_viewport` | Changes the viewport size | Same |

## Mobile

Drives one device from the shared device park (`mobile.Pool`) — the first
`mobile_*` call in a run claims a free device, and every later call in that
run stays on it. Registered at all only when a device is attached; see
[Mobile devices and store releases](mobile-releases.md).

| Tool | What it does | Typically held by |
|---|---|---|
| `mobile_launch_app` | Installs/launches the app on the claimed device | `qa-agent`, `mobile-developer`, `product-manager` (UAT) |
| `mobile_screenshot` | Captures the device screen | Same |
| `mobile_read_ui` | Reads the current screen's UI tree | Same |
| `mobile_tap` | Taps a coordinate or element | Same |
| `mobile_type_text` | Types into the focused field | Same |
| `mobile_swipe` | Swipes the screen | Same |
| `mobile_wait_for` | Waits for a condition on the device | Same |
| `mobile_press_button` | Presses a hardware/system button (back, home) | Same |
| `mobile_rotate` | Rotates the device orientation | Same |
| `mobile_unlock_device` | Unlocks the device screen | Same |
| `mobile_release_device` | Ends the Appium session; leaves the simulator running for the next task | Same |

## Memory

| Tool | What it does | Typically held by |
|---|---|---|
| `save_memory` | Stores a memory for future runs of this agent | Every role |
| `search_memory` | Searches stored memories | Every role |
| `delete_memory` | Deletes a stored memory | Every role |

See [Memory](memory.md).

## Skills

| Tool | What it does | Typically held by |
|---|---|---|
| `load_skill` | Loads a skill's instructions into the run | Every role |
| `create_skill` | Writes a new skill | Every role that holds `load_skill` (same policy set) |

See [Skills and rules](skills-and-rules.md).

## MCP servers

MCP tools are named `mcp_<server_id>_<original_tool_name>`; their parameters
and descriptions come straight from the MCP server's own tool definitions.
`GET /v1/tools` shows which are currently active. See [MCP
servers](mcp-servers.md) for adding one.

The reverse direction also exists: a Claude Code CLI session TaskTrooper
spawns gets the registry's own tools back over `/mcp`, named
`mcp__tasktrooper__<name>` — this is how a headless `claude` run gets
`list_board_tasks`, `commit_task_changes`, and the rest, without the
repository's own checked-in `.mcp.json` ever being loaded.
