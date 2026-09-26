---
name: test-environment-selection
category: qa
description: Decide where to test - the task's own Vercel preview when it is built from the PR head, else boot the task branch locally in the workspace, or use the repository's stage deploy target - and never run any test against production
---
# Test Environment Selection

## Overview

Before executing a single scenario, decide WHERE the product will run. There are exactly three allowed places:

1. **Preview — the task's own per-branch deployment (preferred when available).** When a component is deployed on Vercel with a per-branch `preview` environment, Vercel builds the task branch into its own preview deployment. `get_task_preview` returns it. It is the task's exact code, deployed the way production is, and nobody else's change is on it.
2. **Local — the task workspace (fallback).** Build and boot the backend, worker, and/or frontend from the task branch inside the task workspace. Fully isolated; the default whenever there is no usable preview.
3. **Stage — the repository's `stage` deploy target.** Used when the project is configured for stage verification, or when the app cannot boot locally (managed secrets, heavy infra the workspace lacks).

**Production is never a test environment.** No scenario is ever executed against the prod base URL or prod data — not a "harmless" GET, not seeding, not cleanup. Automated suites likewise never touch prod, stage, or any shared database (see test-database-seeding).

## How to decide

1. Call `get_task_preview`. **Use the preview** for a component when its entry has `status: "ready"` AND `built_from_pr_head: true`. Anything else is not the code under test:
   - `status: "building"` → wait and call `get_task_preview` again; do not test the previous build meanwhile.
   - `built_from_pr_head: false` → the preview is an older commit; wait for the head's build the same way.
   - `status: "error"`, `"canceled"` or `"none"`, or an empty `previews` list → no preview; go to step 2.
2. Read the repository settings (`list_repositories`): `build_command`, `test_command`, `verify_command` — and any comment the developer left on the task (a clean run leaves none). These are the project's declared way of running and checking itself.
3. **Local is possible?** Dependencies resolvable, config/example env present, no unavailable external secrets → boot in the workspace following backend-manual-testing / frontend-manual-testing.
4. **Local is not possible or the project verifies on stage?** Call `get_deploy_target(repository_id, env="stage")` and use the target's `base_url` as the address for every request. If no stage target is configured either, you are blocked — see below.

## Preview rules

- **Address.** Use `open_url` in the browser (`browser_navigate`) — for a protected preview it carries the bypass and sets a cookie, so the pages it links to load too. For HTTP requests (curl, an API client) use `branch_url` (or `url` when there is none) and send every header in `request_headers` on every request.
- **Protected without a bypass.** When the entry notes that the preview is behind Vercel Deployment Protection with no Protection Bypass for Automation, the preview answers every automated request with a login page. Do not report that page as a failure. Test locally instead and leave one comment asking the human to add "Protection Bypass for Automation" in the Vercel project (Settings → Deployment Protection).
- **Never write the bypass secret** — nor `open_url`, which contains it — into a comment, test case, document or bug report: it opens the project's previews to whoever reads it. Refer to "the preview bypass" instead.
- **A preview is not prod, but its backing services may be.** If the preview talks to a shared database or a third-party account, avoid destructive scenarios there exactly as on stage; they run locally only.
- **Name what you tested.** Your verdict comment states the preview's `branch_url` and the commit (`commit_sha`) it was built from.

## Stage rules

- **Confirm the change is actually there.** Stage verdicts are meaningless if the task's branch is not deployed: check the repo's pipeline/deploy state (`get_pipeline_status`), a version/build endpoint, or ask via task comment. Never "test" code that is not running in the environment.
- **Stage is shared.** Namespace your test data (`qa-<task-id>-...` prefixes), avoid destructive scenarios (mass deletes, migrations, load tests) — those run locally only — and clean up what you created.
- The stage `base_url` is for requests; the `health_url` is only a liveness probe.

## When none is possible

If there is no usable preview, the app cannot boot in the workspace AND no stage target exists (or the change is not deployed there), do not fake a verdict and do not read the code as a substitute. Post a comment stating exactly what is missing (example env file, seed script, run instructions, stage target) and move the task to need_revision: a change that cannot be executed cannot be verified, so it is not done.

## Red Flags

- A connection string or base URL in your commands points at prod → stop immediately.
- "I'll just check this one thing on prod" → no. Preview, local or stage, always.
- Testing a preview whose `commit_sha` is not the PR head → the verdict describes an earlier version.
- Testing on stage without confirming the deploy → verdicts describe the previous version.
