A task in `done` is finished as *work* — reviewed, tested, accepted. Nothing here is testing. **Land it, then find out what production did with it.**

### 1. Merge

1. Read the PR (`get_task_pull_request`) and the build (`get_pipeline_status`). The checks must be green and the PR must still be at the commit the task was verified at.
2. Call `merge_task_pull_request`. It squash-merges, deletes the task branch, and opens (or joins) a release — the result carries `release` (mode, next step). Do not comment that you merged it and never paste the PR link or number: the board already shows both.

**A refusal is final, not a retry:**

- **A conflict** — `dirty`, or `behind` on a repository that requires up-to-date branches. Rebasing is the developer's work. Move the task to `need_revision` with one comment saying its branch conflicts with the base and has to be brought up to date.
- **Before-deploy steps pending** — on an `on_merge` component the merge IS the deploy, so it refuses while a human has not confirmed this task's before-deploy steps. The system already commented the steps on the task. Stop — do not retry; you are woken the moment a human presses **Confirm before-deploy steps**.
- **Anything else** — checks not green, PR already merged or closed, the head no longer the signed-off commit. Write the reason in one comment and stop; only a new round of review moves this forward.

### 2. Act on `release.mode`

- **`release.unconfirmed: true`**, or mode **`none`** — nothing to do; stop. (An unconfirmed profile already carries its own explanation on the card.)
- **`batch`** — the merge joined the component's draft release (created if there was none). Nothing to do; stop — a human cuts it later on the Deploy tab, and you are woken with a `pending` release when they do.
- **`on_merge`** — call `watch_release`.
- **`dispatch`** — call `deploy_release`, then `watch_release`. `deploy_release` refuses the same way as the merge does when any task of the release still has pending before-deploy steps: the system comments them on the newest task, and you stop — do not retry; you are woken once a human confirms.

### 3. When woken

- **`pending`** — either a `dispatch` release opened after its component's delivery profile was confirmed, or a `batch` release a human just cut. Either way: call `deploy_release`, then `watch_release`. For a cut batch release, `deploy_release` creates the tag (`github_actions`), runs the local build/publish command (`local`), or starts the store build (`store`) — read which executor from `get_release` if you need to know what to expect.

- **`awaiting_verdict`** — `get_release`, then, where the component has a bound runtime environment, read `query_runtime_logs` (since `deployed_at`) and `list_runtime_errors`; run any extra read-only checks the task's acceptance criteria call for. A batch release with no bound runtime environment (most desktop/mobile components) has nothing to read there — its evidence is the build/publish result (`get_release`'s workflow run, `local_run`, or `store_builds`) plus any smoke checks; say so explicitly in your note rather than skipping the check silently. Then call `finish_release` (note exactly what you checked) when the evidence is clean, or `rollback_release` (reason, note) when it is not. After a rollback: perform or report every `manual_steps` item, then call `watch_release` again.
- **`failed`** — `get_release`; for `github_actions`, `get_deploy_logs` if a job failed; for a batch `local` run, read `get_release`'s `local_run.tail` (and its log path) instead; for `store`, `get_release`'s `store_builds` names the failing platform. If the bad code is live or sitting on the default branch, call `rollback_release` (reason `deploy_failed`). If nothing actually shipped and there is nothing to undo, report what failed and stop.

A batch rollback only reverts the default branch — nothing is redeployed, since a published desktop or store build cannot be unpublished by a revert. `manual_steps` leads with unpublishing or halting that artifact; perform or report that step first.

Never move this task to `released` yourself — only `finish_release` does that.
