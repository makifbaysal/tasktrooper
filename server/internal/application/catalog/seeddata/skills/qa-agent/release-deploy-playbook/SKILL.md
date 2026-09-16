---
name: release-deploy-playbook
category: qa
description: Merge, watch the deploy the merge produced, and roll it back when production says no
---

# Release Deploy Playbook

You own a task's code from the moment the board signs it off. That is three
things in order — **merge → watch → roll back** — and each one is only worth
doing because the one before it happened.

## 1. Merge first — a release of unmerged code releases nothing

The first thing you do in `done` is merge the task's pull request with
`merge_task_pull_request` (squash + branch delete). Until that happens the change
is on a branch and nothing you deploy contains it. Read the PR
(`get_task_pull_request`) and the build (`get_pipeline_status`) first.

A merge that goes through needs no comment: the tool records the merge commit on
the task and the board shows it, so writing "merged #42 as abc1234" is the same
fact for the third time. Never paste the PR link either — it is a field on the
card.

A refusal is where you have a decision to make, and there are two kinds:

- **A conflict** — GitHub reports `dirty` (the branch conflicts with its base) or
  `behind` (the base moved and this repository requires up-to-date branches).
  Rebasing somebody's branch is the developer's job, not QA's: move the task to
  `need_revision` with one comment saying the branch conflicts with the base and
  has to be brought up to date, and stop.
- **Anything else** — red checks, an already-closed PR, a head commit that is no
  longer the one the task was verified at, an incomplete review chain. Post the
  reason on the task and stop.

Do not trigger a release for a task whose PR did not merge.

The merge records its commit on the task. **Everything after this point is keyed
on that commit** — not on the branch, not on "the latest deploy". The default
branch carries everyone else's merges too, and a watch that read the branch would
report somebody else's deploy as yours.

## 2. Release, if this repository releases that way

Two shapes exist and you do not choose between them:

- **A repository with a prod deploy workflow.** Call `trigger_release` with the
  task id. It dispatches the workflow at the task's own merge commit. The task's
  `before_deploy` and `rollback_plan` are posted on the card automatically at
  dispatch, and `after_deploy` when it succeeds — do **not** write a pre-deploy
  checklist comment yourself; put the content in those fields
  (`update_board_task`) so it is the same text every release.
- **A repository that deploys on push.** There is nothing to trigger: the merge
  already started the deploy. Skip straight to the watch.
- **A repository that deploys from a machine.** Some projects ship with a script
  rather than a workflow — `deploy/scripts/*.sh`, a `make deploy` target, a
  documented sequence in the README or under `.ai/`. That is a real deploy path
  and it is yours to run (`run_terminal`) when nothing deploys the merge commit
  automatically. Two rules: deploy the DEFAULT branch at your merge commit
  (`git fetch origin && git checkout <default branch> && git pull` first — the
  task branch was deleted by the merge and the workspace is still standing on
  it), and follow the documented steps exactly, in order. Do not invent a
  command and do not reach for a cloud CLI the project never mentions; if the
  steps need a credential this machine does not have, that is a `blocked` task,
  not a place to improvise.

- **A repository with nothing configured at all.** `merge_task_pull_request`'s
  result already tells you: `auto_released: true` means this repository has no
  `deploy_target` in any environment, so the merge you just did already WAS the
  release — the task is in `released`. Do not call `trigger_release` and do not
  look for a deploy to watch; skip straight past this whole section.

If `trigger_release` reports the repo uses batched releases, do not try to force a
deploy. Note that the change is queued for the next batch and leave the task in
`done`.

## 3. Watch the deploy — `get_task_deploy_status`

One tool, both shapes. It resolves what production did with **this task's merge
commit**, from whichever of three signals exists:

| Signal | Where it comes from |
| --- | --- |
| `actions_run` | the deploy JOB inside an Actions run for that commit (the run's build and test jobs are ignored — a red unit test is not a failed deploy) |
| `commit_status` | the commit status a push-to-deploy provider's bot writes (`success \| Vercel`). Read over the GitHub API; no provider credentials exist anywhere in this system |
| `deployment_status` | a GitHub Deployment opened against the commit |

Four states, four moves:

- **`pending`** — the call parks this task and your run ends. **Do not poll, wait
  or sleep**; there is no tool for it and there is nothing to gain. A sweeper is
  watching the deploy for you and will wake this task when it settles. Pass
  `wait: false` only when you are reporting on a deploy rather than waiting for
  one.
- **`success`** — production is running this task's code. Write nothing: the
  board carries the release and a confirmation comment is noise. A health window
  opens here (see below).
- **`no_signal`** — nothing deployed this commit, and the two reasons behind that
  need different answers:
  - **the repository deploys from a machine** → run its own documented deploy
    procedure now (see step 2), then verify the environment answers. If those
    steps fail, report what failed with its output and stop — do not improvise
    another route to production.
  - **CI could not run at all** — Actions out of minutes, over the spending
    limit, or disabled for the repository. The pipeline comment on the task says
    so when that is what happened, and it is neither the change's fault nor
    yours to fix. With no local deploy path either, move the task to `blocked`
    with one comment: the change is merged, it is NOT deployed, and this is why.

  A merged, undeployed task must never be left sitting in `done` looking shipped.
- **`failure`** — go to step 4.

### The health window

After a successful deploy, an incident opened on that environment for the next
~15 minutes is attributed to **this** release, by commit — not by the generic
"something deployed recently" correlation. If one opens, you are woken again with
the incident on the card and a rollback runbook under it. That is a
`health_incident`, and it is handled exactly like a failed deploy.

## 4. Read the log before you touch anything — `get_deploy_logs`

`get_deploy_logs` returns a **summary**: the error-looking lines lifted out,
followed by the tail. Two sources:

- `source: actions_job` (default) — the failing deploy job's CI log. Omit
  `job_id` and it uses this task's failing job.
- `source: logs_url` — the environment's own log endpoint, if one is recorded on
  the deploy target. `health_url` only ever answers "it is up"; an app that came
  up and is logging a failed migration on boot is invisible to it, and this is
  how you see that. Record the endpoint with `update_deploy_target` (`logs_url`)
  once, if the application exposes one — never point it at a cloud provider's log
  console, and never invent a route that does not exist.

Post what actually failed, with the relevant lines. Not the whole log.

> Your own shell may also have tooling the host machine happens to provide.
> That capability comes from the machine you are running on, not from this
> system, and nothing here depends on it: the playbook above works with no
> such tooling at all.

## 5. Roll back — `rollback_task_release`

Call it with `trigger=deploy_failed` (the watch said `failure`) or
`trigger=health_incident` (the environment went red inside this release's
window). You do not choose the mechanism:

- a repository **with** a deploy workflow → the last known-good commit is tagged
  and that workflow is re-dispatched at it;
- a repository **without** one → the merge commit is reverted on the default
  branch and pushed, because on a push-to-deploy host a new commit *is* the
  redeploy. Nothing is ever force-pushed; a revert that conflicts fails loudly
  rather than guessing.

It refuses, changing nothing, when the task never merged, when its commit is not
what the environment is currently running (someone released after you — rolling
back would undo *their* change), or when nothing actually went wrong. Those are
final: report them, do not retry.

**`proposed: true` means `auto_rollback` is off for that environment.** Nothing
was executed and that is correct. Post the proposal, say plainly that a human has
to confirm it, and stop. Do not look for another way to roll production back.

### The half no tool can do

Every result carries `manual_steps`, and they are yours.

A rollback undoes **code**. It does not reverse a database migration, turn a
feature flag back off, purge a CDN, roll back a third-party configuration change
or un-send anything. The task's own `rollback_plan`, `before_deploy` and
`after_deploy` fields — written by the developer who made the change — say which
of those apply, and they are posted on the card when the rollback is requested.

Read them. Perform every step you can. Then say, on the card, **explicitly**,
which ones you could not perform and who has to. A schema change is the usual
one: the code is back, the column is not.

A rollback reported as complete when half of it was not is worse than one that
asks for help. If the task recorded no rollback plan at all, say that too — the
absence is a finding for the next release.

## Record where the environment answers

After the first successful deploy of a stage or prod environment, check the
target with `get_deploy_target`. If its `base_url` is empty, nobody has written
down where that environment actually lives — read the served URL out of the
deploy job's output (the Cloud Run/Vercel/ECS deploy step prints it, or the
workflow's own summary does) and record it with `update_deploy_target`, along
with the health endpoint and, if the app serves one, the log endpoint. It writes
only those address fields. Do this once per environment: every later QA run, UAT
pass, health probe and deploy watch resolves the address from there instead of
guessing it.
