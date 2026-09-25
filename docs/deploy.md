---
title: Deploy targets and recipes
description: How a repository ships to each environment, the recipe catalog, and what happens between a task reaching Done and its release going live.
---

# Deploy targets and recipes

A **deploy target** is one row per (repository, environment): which provider
it ships through, the values that provider's workflow needs, a health URL,
and whether a failed release rolls back automatically. Deploy targets live on
the repository's **Deploy targets** page (**Settings → Deploy targets**,
reached from a repository), and the cross-repository view of what is live
where is the **Deployments** page under Operations.

## Providers and the recipe catalog

Each provider is an embedded markdown recipe — YAML frontmatter plus a GitHub
Actions workflow — rendered against a target's saved values. `list_deploy_templates`
and `load_deploy_template` (see [Agent tools](tools.md)) are how an agent
reads the catalog and one recipe in full, including its required secrets, its
smoke check and its rollback command.

| Provider | Recipe | Kinds | Variables |
|---|---|---|---|
| GCP Cloud Run | `gcp-cloud-run` | backend, worker, monorepo | `gcp_project_id`, `gcp_region`, `artifact_repo`, `service_name`, `health_url` |
| GCP GKE | `gcp-gke` | backend, worker, monorepo | `gcp_project_id`, `gcp_region`, `cluster_name`, `namespace`, `deployment_name` |
| AWS ECS Fargate | `aws-ecs-fargate` | backend, worker, monorepo | `aws_region`, `ecr_repository`, `ecs_cluster`, `ecs_service`, `task_family` |
| AWS Lambda | `aws-lambda` | backend, worker | `aws_region`, `function_name`, `alias_name`, `artifact_path`, `health_url` |
| Vercel | `vercel` | frontend | `vercel_scope`, `vercel_project`, `health_url` |
| Fly.io | `fly-io` | backend, worker | `fly_app`, `health_url` |

Every recipe targets `stage`, `preprod` and `prod`. A `{{var}}` placeholder in
the rendered workflow is filled from the target's saved values; GitHub Actions'
own `${{ ... }}` expressions are left untouched. A required variable nobody
has filled in yet stays visible in the rendered workflow as
`{{key — SET THIS}}`, so a half-configured recipe is obvious rather than
silently wrong. Each recipe also carries a `rollback_hint` — the one-line
command its own rollback mechanism runs (an alias shift, a traffic split, a
`kubectl rollout undo`, an alias-URL rollback, and so on).

Mobile releases (App Store Connect and Google Play) are a separate path with
no recipe — see [Mobile devices and store releases](mobile-releases.md).

## Health URL and rollback policy

Two fields on a target matter beyond the recipe itself:

- **`health_url`** — what the release engineer's soak window and the
  production health monitor probe to decide whether an environment is up
  (see [Production incidents](incidents.md)). It only ever answers "is it
  up"; it says nothing about a deploy that came up but is failing a
  migration on boot.
- **`auto_rollback`** — this legacy, per-environment field still drives the
  generic incident remedy (below) for a component that has no release
  history to attribute an incident to. For a component that ships through
  the release engine, use the delivery profile's own `auto_rollback` instead
  (**Releases**, below) — it is what actually decides whether
  `rollback_release` may execute on its own or only writes a proposal.

`health_url` and `logs_url` are both validated by the same outbound URL guard
that covers every agent-writable address (see
[Data directory and security](data-and-security.md)) before they are saved,
and re-validated on every fetch afterward — a name that resolved to a public
address when it was saved is free to answer `127.0.0.1` later.

## The Deployments page

Operations → Deployments shows a matrix of every repository against every
environment it deploys to, refreshed roughly every 15 seconds. Selecting a
cell opens the run's detail — the same GitHub Actions run, commit status or
GitHub Deployment the release engine reads (see **Releases**, below) — with
rollback where the target allows it.

## Hosting links

A repository ships to exactly one place, and which place that is lives under
**Repository settings → Deploy**, next to that repository's addresses — the
same section the operations matrix opens at `/repositories/{id}/deploy`. One
scope at a time, with the provider picked rather than stacked.

A monorepo picks the scope once, at the top of the section: the repository
itself or one of its sub-projects. The hosting link, the per-environment
addresses and the deploy templates all follow that pick, because the server
stores each of them per `(repository, sub_project_path)`. A scope whose kind is
mobile gets the store panel instead of the hosting one — it publishes through a
store console and has no hosted runtime to point at. Vercel and Google Cloud are wired; a monorepo picks the scope (the
repository itself or one sub-project) next to the provider, and each provider
reports whether that scope is linked.

- **Vercel** — the linked project, its production URL, framework, root
  directory, the latest deployment and, kept apart from it, the last failed one.
- **Google Cloud** — the bound Cloud Run service or GKE cluster and what it is
  doing: image, latest ready revision, traffic split, or the cluster's status,
  control-plane version and node pools. The binding is kept even when the
  service account is gone or cannot read the resource; the panel says so
  instead of hiding which resource is bound.

Unlinking is local: `DELETE` on the link or binding only stops TaskTrooper
reading it, and nothing inside Vercel or Google Cloud is changed.

## Vercel account and detection

Settings → Integrations connects a Vercel account with a personal token,
verified against `/v2/user` before it is stored; a team can be chosen there
too, or left as the personal account. Once connected, a repository's
**Hosting** detection (`GET /v1/repositories/{id}/hosting/detect`) looks for
Vercel project markers in the working copy (`.vercel/project.json`, a git-link
match, the repository name) and proposes a candidate; only one decisive match
counts as `exact` and gets linked automatically, everything else asks you to
confirm. A confirmed Vercel hosting link on a repository's root area fills in
the empty parts of its `prod` deploy target — base URL, health URL, and the
recipe's own variables — instead of asking you to type them twice.

## Releases

Once a task is signed off in Done, a dedicated **release engineer** agent
owns everything that happens to it from there: merging its pull request,
shipping it, watching production afterward, and either confirming it or
rolling it back. Nobody else merges, deploys or rolls back — not even QA,
whose job ends at its test verdict.

### Set a component's delivery profile first

Every **component** (a repository, or one part of a monorepo) has a
**delivery profile** on its Deploy tab: HOW a merge of that component ships.

| Mode | What happens on merge |
|---|---|
| `on_merge` | The merge itself deploys (a workflow that runs on every push to the default branch, or a Vercel git integration). The release engineer only watches, verifies and, if needed, rolls back. |
| `dispatch` | The release engineer calls a tool to fire a deploy workflow at a tag for this exact commit, once the merge lands. |
| `batch` | The merge just queues into a **draft release** for the component; nothing ships until a human cuts it (see **Batch releases**, below). This is the default for mobile components. |
| `none` | Nothing deploys this component (a library, documentation) — the merge itself is the release. |

TaskTrooper detects a likely profile from your CI workflows and bound
environments during a scan (a mobile component → `batch`/App Store or Play;
a workflow that runs on every push to `main` → `on_merge`; a workflow with a
manual trigger → `dispatch`; a confirmed Vercel environment → `on_merge`; a
tag-triggered release workflow → `batch`), and a high-confidence detection is
used automatically. **A medium-confidence guess is never acted on until you
confirm it** on the Deploy tab — a merged task simply waits in Done, with one
comment saying why, until someone does. Confirming (or editing) the profile
there is what releases any tasks that piled up waiting.

Each profile also carries: which GitHub Actions workflow to dispatch or
watch, how long to soak production after a deploy before asking for a
verdict (`soak_minutes`, default 10), how many brand-new runtime error
groups are tolerated before that soak stops early, a list of read-only
smoke checks (GET/HEAD only — nothing that could write to production), and
whether a bad release may be rolled back automatically or only proposed.

### From merge to a verdict

1. **The release engineer merges the pull request** (`done` wakes it, the
   same wake QA used to hold) — squash-merge, delete the branch — once the
   checks are green and the review chain is complete. This opens (`on_merge`
   / `dispatch`) or joins (`batch`) a **release** for the component. If the
   task carries `before_deploy` steps that a human has not confirmed yet on
   an `on_merge` component, the merge itself is refused until someone does
   (see **Before you ship**, below).
2. **The deploy happens** — immediately for `on_merge`, or once the release
   engineer dispatches it for `dispatch`/a cut batch release.
3. **A soak window runs.** A system sweeper — no agent, no tokens spent —
   watches the deploy settle, then samples the environment's health, runs
   the profile's smoke checks, and watches for brand-new runtime error
   groups, for the configured number of minutes (or until something clearly
   fails, whichever comes first). The task's card sits parked the whole
   time; nothing polls or waits idly.
4. **The release engineer is woken for a verdict.** It reads the runtime
   logs and errors since the deploy, wherever the component has a bound
   environment (see "Hosting links" and "Vercel account and detection",
   above), looks at the soak evidence, and either **finishes** the release
   (every task it carries moves to Released) or **rolls it back**. A
   release is never finished on a green deploy alone — the agent has to
   have actually read the evidence in that same run.
5. **`after_deploy` is posted** as a comment on each task once the release
   finishes, if the task has any after-deploy steps written down.

A schema migration is treated specially: `has_migration` is detected from
the branch diff, and the release engineer's merge is refused unless the
task's own staging deploy already succeeded — a migration is the one class
of change build and test cannot judge, since both stay green while the
rollout itself breaks production.

### Rolling a release back

A rollback is called for on real evidence — a failed deploy, a failing
smoke check, a health check gone red, or new runtime error groups tied to
the change — never a hunch, and it always does two things:

1. **Reverts the change on the default branch** (a `git revert` of the
   merge commits, pushed) — this always happens, so the next release cannot
   ship the same bad code again.
2. **Restores production as fast as it can.** If the component's bound
   production environment is on a provider TaskTrooper can roll back
   natively (Vercel, Google Cloud Run — see **Provider write scopes**,
   below), that runs FIRST, in seconds. Otherwise (or in addition, for an
   `on_merge` component, to re-enable automatic deploys afterward) the
   previous good version is redeployed, or the revert's own push simply
   redeploys on merge. A `batch` release (desktop, mobile) is the exception:
   nothing is redeployed at all, because a published desktop build or an
   app-store submission cannot be unpublished by a revert — the rollback's
   manual steps then lead with unpublishing or halting that artifact
   yourself.

If the profile's `auto_rollback` is off, nothing is executed automatically:
the release engineer writes up the proposal as a comment on the task, and a
human confirms it from the release's detail drawer instead.

Every rollback names its **manual steps** — the part no mechanism can
perform on its own: undoing a database migration, flipping a feature flag,
purging a CDN, unpublishing an artifact — pulled from the task's own
`rollback_plan`/`before_deploy`/`after_deploy` fields. The release engineer
is expected to perform or explicitly report every one of them.

### Before you ship: `before_deploy` confirmation

A task's `before_deploy` text (run a migration, set a secret, flip a
switch) is work only a HUMAN can do — the release engineer never writes to
production. Until you confirm it on the task ("Confirm before-deploy
steps"):

- an `on_merge` component's merge is refused outright (the merge IS the
  deploy);
- a `dispatch` component's `deploy_release` is refused instead, once it has
  merged;
- cutting a batch release confirms every carried task's steps for you — the
  cut itself is your confirmation.

Confirming wakes the release engineer immediately if the task was waiting
on it, instead of leaving it for the next sweep.

### Batch releases: desktop, mobile and anything else you cut by hand

A `batch` component's merges collect into a single **draft release** instead
of shipping on every merge. The Deploy tab's Releases card shows the draft
first — "Next release — N merged tasks" — with a **Cut release** button.
Cutting:

- suggests the next version (a semver patch bump if every carried task is a
  bug fix, otherwise a minor bump — or `0.1.0` with nothing released yet),
  generates release notes grouped into Features/Fixes, and shows the exact
  commit it would cut;
- lets you edit the version and the notes before confirming;
- is itself your confirmation of every carried task's `before_deploy` steps.

From there it deploys through whichever **executor** the profile names:

| Executor | What cutting does |
|---|---|
| `github_actions` | Tags the cut commit; your own tag-triggered workflow builds and publishes it. A version that was already released refuses to re-cut, rather than silently overwriting it. |
| `local` | Runs the profile's build-and-publish command on this machine, in a clean detached checkout of the cut commit, and logs it — the release drawer shows the exit code and the log tail. |
| `store` | Starts an App Store / Google Play release build for every platform the repository has a linked app for (see [Mobile devices and store releases](mobile-releases.md)). |

A cut batch release still goes through the same soak-and-verdict flow as
any other — most desktop/mobile components simply have no bound runtime
environment to read logs from, so the evidence is the build/publish result
and any smoke checks instead.

### Provider write scopes for instant rollback

Rolling back through the provider itself (the fastest path, seconds instead
of minutes) needs write access, not just read access, to that provider:

- **Vercel** — the connected account's token needs write scope (a
  read-only token can list deployments but is refused when the release
  engineer tries to roll one back or promote another).
- **Google Cloud Run** — the connected service account needs the
  `run.services.update` permission, to shift traffic between revisions.

Without it, nothing breaks — the rollback simply skips the provider step
(recorded on the release) and falls back to redeploying/reverting as
before, just slower.

## See also

- [Production incidents](incidents.md) — how a failed deploy or a failing
  health check turns into an incident, and how it gets attributed back to
  the release that caused it
- [Agent tools](tools.md) — the full release and deploy tool list
- [Git and pull requests](git-and-pull-requests.md) — how a task's commit
  reaches the merge that opens its release
- [Mobile devices and store releases](mobile-releases.md) — the `store`
  batch executor in full
