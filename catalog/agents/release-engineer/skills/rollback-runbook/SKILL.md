---
name: rollback-runbook
category: release
description: What a rollback mechanism restores, what it cannot, and how to work through manual_steps
---
# Rollback runbook

`rollback_release` always reverts the release's task merge commits on the default branch and pushes that revert — production can never ship the bad change again, whatever else happens. What restores the RUNNING service depends on the component's delivery mode:

- **`dispatch`** — the previous good release's commit is tagged and the deploy workflow is dispatched at it (`Mechanism: workflow_dispatch`). No previous release: it dispatches at the revert commit instead.
- **`on_merge`** — the revert push itself is the redeploy (`Mechanism: revert_push`); there is nothing else to trigger.
- **`batch`** (desktop, mobile) — nothing is redeployed. `Status` goes straight to `rolled_back`: a published desktop build or a store build cannot be unpublished by reverting a git commit, so there is no redeploy for `watch_release` to follow.

For `dispatch`/`on_merge`, `Status` becomes `rolling_back` and you are expected to call `watch_release` again to follow the redeploy through to `rolled_back`.

## What a revert cannot undo

`git revert` undoes code. It does not:

- reverse a database migration,
- turn a feature flag back off,
- purge a CDN or cache,
- roll back a third-party configuration change,
- un-send a webhook, an email, or anything else the code triggered while it ran.

`rollback.manual_steps` is exactly this list, built from each rolled-back task's own `rollback_plan`/`before_deploy`/`after_deploy` fields — the developer who made the change is the only one who knew which of these applied.

## Batch releases add one more, first

For a `batch` release, `manual_steps` puts one entry ahead of the tasks' own: unpublish or halt the artifact the revert cannot touch.

- `github_actions`/`local` — unpublish or mark broken whatever was published for the release tag: the GitHub Release, any package manager cask or update feed pointing at it.
- `store` — halt or stop that platform's store rollout (Operations → Apps) for the build that shipped.

Treat it like any other manual step: perform it if you can, report it if you cannot, and put it first in your report — it is the step that actually stops the bad build reaching more users, not the code revert.

## Work through it

1. Read every item in `manual_steps`.
2. Perform every one you can with the tools you have (there are no workspace writers here — most manual steps are outside this system's reach and are for a human).
3. Say explicitly, on the card, which ones you performed and which you could not, and who has to finish them. "Reverted the code; the column added by migration 042 is still there and needs a human to drop it" is a complete report. "Rolled back" with nothing else is not.

A rollback reported as complete when half of it was not is worse than one that admits what it did not do — the next release will ship on top of whatever was left half-undone.

## No plan recorded

If a rolled-back task recorded no rollback plan at all, say that too. The absence is a finding for whoever reviews this release next, not something to paper over with "nothing else to do here".
