A task in `done` is finished as *work* — reviewed, tested, accepted. Nothing here is testing. **Land it, then find out what production did with it.**

### 1. Merge

1. Read the PR (`get_task_pull_request`) and the build (`get_pipeline_status`). The checks must be green and the PR must still be at the commit the task was verified at.
2. Call `merge_task_pull_request`. It squash-merges, deletes the task branch, and opens (or joins) a release — the result carries `release` (mode, next step). Do not comment that you merged it and never paste the PR link or number: the board already shows both.

**A refusal is final, not a retry:**

- **A conflict** — `dirty`, or `behind` on a repository that requires up-to-date branches. Rebasing is the developer's work. Move the task to `need_revision` with one comment saying its branch conflicts with the base and has to be brought up to date.
- **Anything else** — checks not green, PR already merged or closed, the head no longer the signed-off commit. Write the reason in one comment and stop; only a new round of review moves this forward.

### 2. Act on `release.mode`

- **`none`** or **`unconfirmed`** or **`batch`** — nothing to do; stop. (`unconfirmed` and `batch` already carry their own explanation on the card.)
- **`on_merge`** — call `watch_release`.
- **`dispatch`** — call `deploy_release`, then `watch_release`.

### 3. When woken

- **`awaiting_verdict`** — `get_release`, then read `query_runtime_logs` (since `deployed_at`) and `list_runtime_errors`, and run any extra read-only checks the task's acceptance criteria call for. Then call `finish_release` (note exactly what you checked) when the evidence is clean, or `rollback_release` (reason, note) when it is not. After a rollback: perform or report every `manual_steps` item, then call `watch_release` again.
- **`failed`** — `get_release`; if a job failed, `get_deploy_logs`. If the bad code is live or sitting on the default branch, call `rollback_release` (reason `deploy_failed`). If nothing actually shipped and there is nothing to undo, report what failed and stop.

Never move this task to `released` yourself — only `finish_release` does that.
