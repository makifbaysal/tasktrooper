---
name: release-playbook
category: release
description: The full merge -> deploy -> verify -> finish/rollback procedure, per delivery mode, with example tool results
---
# Release playbook

You own a task from the moment the board signs it off in `done` until it either lands in `released` or bounces back to `need_revision` through a rollback. Four steps, in order, and each is only worth doing because the one before it happened: **merge → deploy → verify → finish or roll back.**

## 1. Merge

```
merge_task_pull_request({ task_id })
```

returns, among the usual merge fields, `release`:

```json
{
  "release": {
    "mode": "dispatch",
    "release_id": "b1e2...",
    "status": "pending",
    "next": "call deploy_release, then watch_release"
  }
}
```

`release.next` already tells you the next call — read it, do not guess. The four modes:

| `release.mode` | What it means | What you do |
|---|---|---|
| `none` | Nothing deploys this component (a library, docs) — the merge itself was the release. | Nothing. |
| `unconfirmed` | The component's delivery profile was never confirmed. The system already posted the one comment explaining this. | Nothing. |
| `batch` | This component ships in a human-cut batch (mobile and friends); release cutting arrives in a later version. | Nothing — the task stays in `done`. |
| `on_merge` | The merge itself deploys (a workflow on push, or Vercel's git integration). | `watch_release`. |
| `dispatch` | A deploy workflow needs to be dispatched at the release tag. | `deploy_release`, then `watch_release`. |

A merge refusal is final, not a retry — see your `done` column instructions for the two kinds and what each means.

## 2. Deploy (dispatch mode only)

```
deploy_release({ task_id })
```

Dispatches the profile's workflow at the release tag (`release/<sha12>`) and moves the release to `deploying`. Then call `watch_release`.

## 3. Watch

```
watch_release({ task_id })
```

While the deploy is running or the soak window is open, this call **parks your card** and your run ends — a system sweeper is watching the deploy and the health/smoke/error window for you. Do not poll, sleep, or call it again in a loop; you are woken automatically when there is something to decide. When you are woken, `watch_release` (or `get_release`) returns the settled release instead of a park.

## 4. Verdict — `awaiting_verdict`

Never skip straight to a verdict because the deploy looked green. Every time:

```
get_release({ task_id })              # checks already gathered: health, smoke, new_errors, notes, early_stop
query_runtime_logs({ ..., since: deployed_at })
list_runtime_errors({ ... })
```

Add a read-only check of your own (`browser_navigate`/`fetch_url`, GET/HEAD only) when the task's acceptance criteria point at something worth looking at directly.

Clean evidence:

```
finish_release({ task_id, note: "logs clean since deployed_at, no new error groups, smoke checks green" })
```

Evidence of a problem — a failed smoke check, two failed health samples, new error groups tied to the change:

```
rollback_release({ task_id, reason: "verify_failed", note: "checkout smoke check returned 500 since the deploy; 3 new error groups in list_runtime_errors, all in the checkout module" })
```

`reason` is one of `deploy_failed` (the deploy itself never went green), `verify_failed` (it went green but the soak found a problem), `health_incident` (an incident opened inside the window, handled from the `released` column). `note` is required — state what you actually checked, not just the conclusion.

## 5. Rollback outcomes

`rollback_release` either executes the rollback and returns `Status: rolling_back` with `rollback.manual_steps`, or — when `auto_rollback` is off for the component — writes the proposal as a comment and returns `proposed: true` with nothing executed. Both are correct, successful outcomes for their situation:

- **Executed** — perform every `manual_steps` item you can (see `rollback-runbook`), report which ones you could not, then call `watch_release` again to follow the redeploy.
- **`proposed: true`** — say plainly that a human must confirm it, and stop. Do not retry and do not look for another way to roll production back yourself.

## 6. Failed deploys

A release that lands in `failed` (the deploy itself never went green) is read the same way: `get_release`, `get_deploy_logs` if there is a failed job, then `rollback_release` with reason `deploy_failed` if the bad code is live or sitting on the default branch — otherwise there is nothing to undo, report what failed and stop.
