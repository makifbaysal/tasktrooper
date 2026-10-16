A task in `done` is finished as *work* — reviewed, tested, accepted. Nothing here is testing. **Land it, then find out what production did with it.**

### 1. Merge

1. Read the PR (`get_task_pull_request`, `include_diff: false` — you never review code) and the build (`get_pipeline_status`). The checks must be green and the PR must still be at the commit the task was verified at.
2. Call `merge_task_pull_request`. It squash-merges, deletes the task branch, and opens (or joins) a release — the result carries `release` (mode, next step). Do not comment that you merged it and never paste the PR link or number: the board already shows both.

**A refusal is final, not a retry:**

- **A conflict** — `dirty`, or `behind` on a repository that requires up-to-date branches. Rebasing is the developer's work. Move the task to `need_revision` with one comment saying its branch conflicts with the base and has to be brought up to date.
- **Before-deploy steps pending** — on an `on_merge` component the merge IS the deploy, so it refuses while a human has not confirmed this task's before-deploy steps. The system already commented the steps on the task. Stop — do not retry; you are woken the moment a human presses **Confirm before-deploy steps**.
- **Delivery profile not confirmed** — the merge refuses while the component's delivery profile is unconfirmed (its own workflow could deploy the merge unwatched). The system already commented. Stop — do not retry; you are woken when a human confirms the profile on the Deploy tab.
- **A deploy dependency is not released yet** — on an `on_merge` component the merge refuses while a task this one `deploy_depends_on` has not reached `released`. The system already commented which. Stop — do not retry; you are woken when that task is released.

**Everything else the merge can refuse with — map it, do not just write a comment and stop:**

| The refusal says | Do |
|---|---|
| last pipeline FAILED; `blocked` with a red required check (read `get_task_pull_request`); `unstable` with a real failing check | `move_board_task` → `need_revision`, one comment naming the failing job/check (`get_pipeline_status`) |
| head moved after sign-off; no verified commit stamped | `need_revision`, one comment: "pushed after sign-off (head `<sha7>`) — needs review again" |
| `blocked` only because a required review/approval is missing | one comment for a human (approve on GitHub), stop |
| required checks fail on the base branch too (pre-existing) | one comment ("base branch is red, not this PR"), stop. Do **not** send it to the developer |
| mergeability `unknown` | `get_task_pull_request` once more, then **one** more merge attempt; still unknown → comment, stop |
| PR closed unmerged / no PR recorded / PR number unreadable / GitHub not connected / merged outside the board | one comment for a human, stop |
| "was merged as …, nothing further is needed" | `get_release` and continue at §2 with its `mode` |

### 2. Act on `release.mode`

- mode **`none`** — the merge was the release; nothing to do, stop. (`release.unconfirmed: true` only appears for a task merged outside this flow; stop as well — the card already says why.)
- **`batch`** — the merge joined the component's draft release (created if there was none). Nothing to do; stop — a human cuts it later on the Deploy tab, and you are woken with a `pending` release when they do.
- **`on_merge`** — call `watch_release`.
- **`dispatch`** — call `deploy_release`, then `watch_release`. `deploy_release` refuses the same way as the merge does when any task of the release still has pending before-deploy steps or an unreleased deploy dependency: the system comments them on the newest task, and you stop — do not retry; you are woken once a human confirms or the dependency ships.

### 3. When woken

- **No release yet** (`get_release` says nothing opened one) — the task is not merged: go through step 1 (merge).
- **`payload.release_rollback: true`** (an incident attributed to this release, dispatched while the attributed task was still in `done`) — follow the `released` column's incident steps: `get_incident(payload.incident_id)`, `get_release`, `get_deploy_impact`, gather evidence, then `rollback_release` (reason `health_incident`) or a "Not rolling back:" comment naming the evidence. With `auto_rollback` off, `rollback_release` only proposes (`proposed: true`) and a human is asked to approve it — stop there.
- **`pending`** — either a `dispatch` release opened after its component's delivery profile was confirmed, or a `batch` release a human just cut. Either way: call `deploy_release`, then `watch_release`. For a cut batch release, `deploy_release` creates the tag (`github_actions`), runs the local build/publish command (`local`), or starts the store build (`store`) — read which executor from `get_release` if you need to know what to expect.

- **`awaiting_verdict`** — `get_release` for its `component` (pass that to the runtime tools in a monorepo — an omitted `component` silently reads the root one) and the checks already gathered; `checks.impact` is what the soak read from deploy impact against the profile's `max_error_rate_delta`/`max_p95_delta_pct` (a breached one is also the `early_stop`), and `get_deploy_impact` reads it again now. Where the component has a bound runtime environment, read `query_runtime_logs` (since `deployed_at`) and `list_runtime_errors` over a window starting well before `deployed_at`, judging a group by its own `first_seen` against `deployed_at` rather than by `new` alone (see `post-deploy-verification`); run any extra read-only checks the task's acceptance criteria call for, across every task in `release.tasks`, not only the one you were woken on. A batch release with no bound runtime environment (most desktop/mobile components) has nothing to read there — its evidence is the build/publish result (`get_release`'s workflow run, `local_run`, or `store_builds`) plus any smoke checks; say so explicitly in your note rather than skipping the check silently. Then call `finish_release` (note exactly what you checked) when the evidence is clean, or `rollback_release` (reason, note) when it is not. After a rollback: perform or report every `manual_steps` item, then call `watch_release` again.
- **`failed`** — `get_release`; for `github_actions`, `get_deploy_logs` if a job failed; for a batch `local` run, read `get_release`'s `local_run.tail` (and its log path) instead; for `store`, `get_release`'s `store_builds` names the failing platform. If the bad code is live or sitting on the default branch, call `rollback_release` (reason `deploy_failed`). If nothing actually shipped and there is nothing to undo, report what failed and stop.

A batch rollback only reverts the default branch — nothing is redeployed, since a published desktop or store build cannot be unpublished by a revert. `manual_steps` leads with unpublishing or halting that artifact; perform or report that step first.

Do not edit or commit code here — that happened before this task ever reached `done`. Never move this task to `released` yourself — only `finish_release` does that.
