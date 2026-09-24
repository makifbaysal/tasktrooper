---
title: Git and pull requests
description: How TaskTrooper connects to GitHub, clones and branches your repositories, and moves a task's pull request from open to merged.
---

# Git and pull requests

Every repository on the board is a real git checkout on this machine, and
every task ships through an ordinary GitHub pull request. TaskTrooper does not
invent its own review or merge mechanism — it drives the one GitHub already
has, and the agents work through the same PR a human reviewer would open.

## Connecting GitHub

Settings → Integrations has the GitHub card. Connecting is a pasted [personal
access token](https://github.com/settings/tokens), not an OAuth redirect —
there is no gateway to hold an OAuth app's client secret, and a local server
cannot receive GitHub's callback. Paste a token and TaskTrooper verifies it
against `GET /user` before storing it (encrypted — see
[Data directory and security](data-and-security.md)); a bad token fails right
there instead of at the first push. The same card appears in the guided
first-run setup, one step after connecting an agent runtime.

The token needs enough scope for what agents actually do to your repositories:

- create and push branches, and open pull requests (`repo`)
- install and manage the push webhook TaskTrooper adds to a repository
  (`admin:repo_hook`)
- list the organizations you belong to, so private org repositories show up
  when importing (`read:org`)

A fine-grained token needs Contents, Pull requests and Webhooks write access,
plus Metadata read, on the repositories you import. Settings → Integrations
shows the connected account's login once the token is verified, and
Disconnect removes it.

## Importing a repository

Importing clones the repository into this runtime's own layout,
`<workspace>/repos/<name>` — a mirror clone used for indexing and as the
source `git clone` reads from when a task cuts its own branch. `kind`
(`backend` / `frontend` / `mobile` / `worker` / `monorepo`) is detected from
the working copy when you do not set it: Flutter or Xcode markers mean
mobile, several projects under `apps/`/`packages/` mean monorepo, a
React/Vue/Svelte/Vite/Next `package.json` means frontend, and everything else
defaults to backend.

TaskTrooper records the repository's `remote_url` on import and treats the
clone on disk as a cache, not the source of truth. If the folder is missing —
the data directory moved, or the checkout was deleted by hand — the
repository card says so, and the **Restore** action re-clones from the
recorded remote into the same layout and re-points the repository at it. This
is refused, without touching anything on disk, unless the folder is genuinely
missing and a remote is recorded; a folder that exists but is not a git
repository, or is unreadable, is also refused rather than silently adopted —
adopting the wrong checkout would let one repository's agent push into
another repository's history.

## Per-task workspace and branch

Before an agent starts, TaskTrooper makes sure there is a real git working
copy at the repository root that is genuinely *this* repository: an intact
checkout whose origin matches the recorded remote is reused; an intact
checkout with a different origin fails the run outright rather than adopting
it; a missing or empty folder is cloned fresh.

Each task then gets its own clone under `<workspace>/task-<task id>/`, checked
out on a branch named after the task's own key:

```
feature/<task-key>
```

for example `feature/t-12` or `feature/b-3`. The `feature/` prefix matters:
CI workflows that trigger on `feature/**` only run for branches with that
prefix, so this is what makes GitHub Actions pick the branch up at all. The
branch name is only ever the task's key — never a slug of the title, which
would depend on what language the board is used in and would drop letters a
naive slugger cannot transliterate. A task branch pushed under an older naming
scheme is not renamed; it gets a fresh branch matching the current scheme on
its next run.

A re-run of the task reuses its existing workspace and branch rather than
starting over. If the branch cut fails, the run falls back to the shared
repository root.

## Commits and the pull request

Writing a file is not enough to reach GitHub — an agent's edits only leave the
task's workspace when it calls the `commit_task_changes` tool, which commits,
pushes to the task branch, and opens the pull request if one does not exist
yet. This is the only path a change takes to reach a PR outside of the board
runner's own automatic post-run commit. Only the developer roles hold this
tool: a reviewer that could commit would be putting its own name on the branch
it is judging.

Task PRs are opened **ready for review**, not as drafts — GitHub refuses to
merge a draft PR, so nothing here creates one that would first need to be
un-drafted. `committed: false` is a normal, non-error result: either the
branch already matches the workspace, or the task has no working copy on this
machine yet, and either way retrying would not help.

When a task moves to PM UAT or Done, TaskTrooper also opens a PR via `gh pr
create --fill --draft` if the earlier commit step has not already opened one,
and appends the PR link as a system comment on the card.

## Review comments

`get_task_pull_request` reads back the PR's state (open/closed,
draft/merged/mergeable), the changed files, every review comment (with its
file, line and any parent it replies to), the plain PR conversation comments,
and a size-capped diff. Every developer role, the architect, QA and the
product manager can read it.

`comment_on_pull_request` posts a reply — either a new top-level PR comment,
or, given a review comment's id, a reply inside that comment's own thread.
Replying in-thread matters on GitHub: a top-level comment leaves a reviewer's
thread showing as unresolved even after it was answered. This tool is granted
to the developer roles and the system architect, who acts as the code
reviewer and answers the threads it opens on a PR it is judging.

## Merging on Done

Nothing in TaskTrooper merges a pull request except one tool call, made by one
role, in one column: **`merge_task_pull_request`, held only by the QA agent,
callable only once the task sits in Done.** The developer must not merge its
own branch, the architect reviews it rather than lands it, and the product
manager signs off on the product rather than on the git history — QA is the
role that most recently exercised the built thing, and Done is the column
that wakes it for exactly this reason.

The merge is a **squash merge**, and the task's branch is deleted afterward.
Before it runs, every one of these has to hold:

| Requirement | What it checks |
|---|---|
| Task is in Done | not still under review |
| A pull request exists, open and unmerged | nothing to merge otherwise |
| Checks are green | GitHub's own `mergeable_state` is `clean`/`has_hooks`, and the task's last pipeline run did not fail |
| Review chain satisfied | always — every stage the task's workflow requires (code review, QA, UAT) must have been visited and not rejected |
| PR head unchanged since the gate ran | the PR's head commit still matches the SHA the board last verified |

A `dirty`/`behind` mergeable state is treated as a real conflict: the tool's
remedy is to send the task back to Need Revision so a developer rebases it,
rather than trying to resolve anything automatically. A missing or `skipped`
CI pipeline does **not** block the merge — that describes a repository with
no CI wired up, where refusing would make the task unmergeable forever, and
GitHub's own mergeable state is what stands in for it. Every refusal here
comes back saying "nothing was merged, do not retry" — merging is
irreversible, and none of these conditions are fixed by trying again.

The merge commit's SHA is recorded on the task, next to the PR link on the
card, and is what the deploy watch on Done/Released monitors next (see
[Deploy targets and recipes](deploy.md)).

## CI status

TaskTrooper never runs your CI itself — it reads what GitHub Actions already
reports, two ways:

- **Polling.** While a task sits in Code Review, TaskTrooper repeatedly asks
  GitHub for the pipeline result on the task's branch, up to a bounded wait,
  before dispatching the reviewer (or sending the task straight to Need
  Revision on a failure).
- **The webhook.** One-click install from the repository's settings mints a
  fresh secret and registers a GitHub webhook for `push`, `workflow_run` and
  `check_suite`. The webhook endpoint (`POST /v1/github/webhook`) is public —
  it carries no bearer token — and instead authenticates every delivery by its
  HMAC signature (`X-Hub-Signature-256`) over the raw body, checked before
  anything that costs an API call. An unrecognized repository answers `204` so
  GitHub stops retrying it; a bad or missing signature answers `401`.

  - `push` to the default branch debounces a reindex and a project-profile
    refresh.
  - `workflow_run` and `check_suite` — once the run is **completed** —
    resolve any pipeline that is waiting on that commit's SHA: success or
    skipped hands the card to its reviewer, failure sends it to Need
    Revision.

Existing webhooks are also repaired automatically at boot, without needing a
manual reinstall.

## Repository settings

A repository's settings page (**Repository Settings**) lets you set
`verify_command`, `build_command` and `test_command` explicitly; left empty,
TaskTrooper auto-detects them from the working copy (and, with a Dockerfile
present, can build in a container instead). These, together with the
repository's default branch, are also read automatically into the
repository's project brief (the scan's git facts) — a background pass parses
the git history itself to report the default branch, the branch-naming
convention, and the **merge style** (merge commits versus squash/rebase for a
linear history) the repository already uses, so an agent's own commits and
PRs follow the same convention rather than guessing.

## See also

- [Deploy targets and recipes](deploy.md) — what happens to a commit after
  `merge_task_pull_request` lands it
- [Quality gates](quality-gates.md) — the review chain and pipeline gates that
  decide when a task may move into Done
- [Data directory and security](data-and-security.md) — how the GitHub token
  is stored
