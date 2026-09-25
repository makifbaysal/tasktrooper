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
| `batch` | This component (desktop, mobile and friends) ships in a human-cut batch: the merge joined the component's draft release. | Nothing — the task stays in `done` until a human cuts it. |
| `on_merge` | The merge itself deploys (a workflow on push, or Vercel's git integration). | `watch_release`. |
| `dispatch` | A deploy workflow needs to be dispatched at the release tag. | `deploy_release`, then `watch_release`. |

A merge refusal is final, not a retry — see your `done` column instructions for the two kinds and what each means.

## 2. Deploy

```
deploy_release({ task_id })
```

**`dispatch` mode** dispatches the profile's workflow at the release tag (`release/<sha12>`) and moves the release to `deploying`. Then call `watch_release`.

**A cut `batch` release** (status `pending`, mode `batch` — you are woken with it, you never cut it) does something different per executor:

| Executor | What `deploy_release` does |
|---|---|
| `github_actions` | Creates the release tag at the cut commit; the repository's own tag-triggered workflow builds and publishes. A tag that already exists is a **real refusal** here — unlike `dispatch`, a batch version is never re-used, so report it instead of retrying. |
| `local` | Runs the profile's command on this machine, in a detached worktree of the cut commit, and logs it (`local_run` on the release). |
| `store` | Starts a store build for every platform the repository has a linked app for (`store_builds` on the release). |

Then call `watch_release` the same as for `dispatch`.

## 3. Watch

```
watch_release({ task_id })
```

While the deploy is running or the soak window is open, this call **parks your card** and your run ends — a system sweeper is watching the deploy and the health/smoke/error window for you. Do not poll, sleep, or call it again in a loop; you are woken automatically when there is something to decide. When you are woken, `watch_release` (or `get_release`) returns the settled release instead of a park.

## 4. Verdict — `awaiting_verdict`

Never skip straight to a verdict because the deploy looked green. Every time:

```
get_release({ task_id })              # checks already gathered: health, smoke, new_errors, notes, early_stop
query_runtime_logs({ ..., since: deployed_at })   # where the component has a bound runtime environment
list_runtime_errors({ ... })                      # where the component has a bound runtime environment
```

Most desktop and mobile components have no bound runtime environment — there are no logs or runtime errors to read. That is not a gap to paper over: your evidence is `get_release`'s build/publish result (the workflow run, `local_run`, or `store_builds`) plus any smoke checks, and your `finish_release`/`rollback_release` note says explicitly that no runtime environment is bound.

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

For a `batch` release, "executed" only ever reverts the default branch — there is no redeploy to follow, and no `watch_release` call after it: a published desktop build or a store build cannot be unpublished by a revert. Say that on the card, and see `rollback-runbook` for the manual unpublish/halt step it puts first.

## 6. Failed deploys

A release that lands in `failed` (the deploy itself never went green) is read the same way: `get_release`, then per executor — `github_actions` → `get_deploy_logs` if there is a failed job; a batch `local` run → `local_run.tail` (and its log path) on the release itself; a batch `store` build → `store_builds` names the platform and its error. Then `rollback_release` with reason `deploy_failed` if the bad code is live or sitting on the default branch — otherwise there is nothing to undo, report what failed and stop.
