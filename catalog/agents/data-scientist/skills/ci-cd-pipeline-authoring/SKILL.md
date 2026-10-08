---
name: ci-cd-pipeline-authoring
category: ci-cd
description: Use when a repository needs CI/CD - author GitHub Actions workflows so TaskTrooper's pipeline auto-detects your validate/build/test jobs and stage/preprod/prod deploys, and so a dispatch delivery profile can dispatch the production deploy
---
# CI/CD Pipeline Authoring

## Overview

TaskTrooper does not run your build — it **reads your GitHub Actions workflows** and maps their jobs and files onto its own pipeline. A task entering `code_review` waits for your `validate`/`build`/`test` jobs on its pull request (`get_pipeline_status` shows them); once a component's delivery profile is `dispatch`, the release engineer dispatches your production deploy workflow at the release tag after the merge. If your workflows are not named the way the detector expects, the pipeline silently skips them and the task stalls at that gate.

**Core principle:** The workflow you author is the contract with the platform. Name jobs and files with the keywords the detector matches, or the stage does not exist as far as TaskTrooper is concerned.

## Two kinds of pipeline slot

**Status slots** (mapped to a *job name* in any workflow) — read for pass/fail on the PR at `code_review`:

| Slot | Job name must contain (by repo kind) |
|------|--------------------------------------|
| `validate` | `lint`, `vet`, `staticcheck` (backend/worker) · `lint`, `tsc`, `typecheck` (frontend) · `lint`, `swiftlint`, `analyze` (mobile) |
| `build` | `build`, `compile`, `docker` (backend/worker/frontend) · `build`, `xcodebuild`, `archive`, `fastlane` (mobile) |
| `test` | `test`, `vitest`, `jest` |

A mobile `build` job must **not** contain `docker` (excluded). Give each job a clear `name:` — the detector matches the job's display name.

**Deploy slots** (mapped to a *workflow file*) — dispatched, one workflow file per slot:

| Slot | Workflow `name:` / file must contain |
|------|--------------------------------------|
| `stage_deploy` | `stage`, `staging`, `preview` |
| `preprod_deploy` | `preprod`, `pre-prod`, `pre-production` |
| `prod_deploy` | `prod`, `production`, `release` (must NOT contain `preprod`) |

## Rules

- **CI** lives in one workflow `<id>-ci.yml` with jobs named `validate`/`lint`, `build`, `test`. **Deploy** lives in separate files, one per environment: `<id>-deploy-stage.yml`, `<id>-deploy-preprod.yml`, `<id>-deploy-prod.yml`.
- **A `dispatch` delivery profile's deploy workflow declares `on: workflow_dispatch` with no required inputs.** The platform dispatches with the branch ref only — it passes no inputs, so a workflow that requires one can never be released this way. Bake the environment into the file; read config from repo/environment **vars**, not inputs.
- **An `on_merge` delivery profile deploys on push to the default branch, on purpose.** `push: { branches: [main] }` on the stage (or the component's only) deploy is correct there — the merge itself deploying is the whole point of that mode, and the release engineer watches, verifies and rolls back rather than dispatching. Preprod/prod still default to dispatch-only (`workflow_dispatch`) so a human/QA gate controls them, unless the component is explicitly `on_merge` end to end.
- **Authenticate with GitHub OIDC**, never long-lived cloud keys. GCP → Workload Identity Federation; AWS → an IAM role via `aws-actions/configure-aws-credentials`. The job needs `permissions: { id-token: write, contents: read }`.
- **Config in vars, secrets only for true secrets.** Project id, region, service name, image repo → repo/environment `vars`. With OIDC there are usually no cloud secrets at all. Never commit a service-account JSON or access key.
- Deploy job ends with a **health-check / smoke step** that fails the job on a bad deploy. This is a build-time gate, not the release verdict: the release engineer's post-deploy verification is what actually decides `released` vs a rollback.
- Pin third-party actions to their current major (or a commit SHA) — check `gh api repos/<owner>/<action>/releases/latest` rather than trusting a cached version number; `actions/checkout@v7` is current as of this writing, not `@v5`.
- Don't author deploy YAML from scratch: `search_boilerplate_catalog` for `deploy <cloud> <type>` (e.g. `deploy gcp backend`), copy that folder's workflow templates, and rename them to `<id>-deploy-<env>.yml`.

## Batch release workflows (a `batch` delivery profile: desktop, mobile, and anything else a human cuts by hand)

A `batch` component never dispatches a workflow — cutting the release creates a tag (or, for a `local` executor, runs a command on the release machine). Author the deploy side of a `batch` component's pipeline around that:

- **`github_actions` executor** — the release/publish workflow triggers on the tag push itself, not on a branch push or `workflow_dispatch`: `on: { push: { tags: ['v*'] } }` (match the component's delivery profile `tag_pattern`, `v*` for the default `v{version}`). Read the version being released from `GITHUB_REF_NAME` (the tag TaskTrooper just pushed) — never from a version file or a bump commit, because cutting a batch release never touches one. **The workflow must not create its own tag or version** (no `semantic-release`, `changesets`, `release-please` step): TaskTrooper pushes the tag that triggers this workflow, and a tool that tries to push a pre-existing tag of its own fails the release.
- **`local` executor** — there is no workflow to author; the command configured on the delivery profile runs directly on this machine in a detached worktree of the cut commit. It receives `RELEASE_VERSION`, `RELEASE_TAG`, and `RELEASE_COMMIT` in its environment — read the version from there, the same way a tag-triggered workflow reads `GITHUB_REF_NAME`.
- **`store` executor** — see the mobile developer's app-store-deploy skill; there is no workflow either, the store build is started directly.

## Worked Example

`on:` block a `dispatch` deploy workflow needs so the platform can dispatch it:

```yaml
name: myapi-deploy-prod          # "prod" → prod_deploy slot
on:
  workflow_dispatch: {}          # platform dispatches with ref only, no inputs
permissions:
  id-token: write                # OIDC
  contents: read
jobs:
  deploy:
    runs-on: ubuntu-latest
    environment: production       # env-scoped vars/secrets + protection rules
    steps:
      - uses: actions/checkout@v7
      # auth via OIDC (WIF / configure-aws-credentials), then deploy the image,
      # then a smoke step that curls the health endpoint and fails on non-200.
```

Because the file name and `name:` contain `prod`, the detector maps this workflow to the component's `prod_deploy` check — the file name a `dispatch` delivery profile names as its `workflow`. The release engineer dispatches it at the release tag (`release/<sha12>`) after the merge, then verifies the deploy (health, smoke checks, runtime errors) before finishing or rolling back.

## Common Mistakes

- A `dispatch`-profile deploy workflow with `on: push` only and no `workflow_dispatch` → it can never be dispatched; either switch the component to `on_merge`, or add `workflow_dispatch`.
- A `workflow_dispatch` with a `required: true` input → dispatch fails (platform sends no inputs).
- One `deploy.yml` for all envs → the detector can only map one workflow per slot; you lose stage/preprod separation. Split per env.
- A prod workflow named `deploy-preprod-and-prod` → `preprod` substring rules it out of the prod slot.
- A batch release workflow that bumps its own version or pushes its own tag → collides with the tag TaskTrooper already pushed.
- Committing cloud keys instead of using OIDC.

## Red Flags

- Job names like `job1`/`step` that contain none of the detector keywords → the slot stays empty and the pipeline gate never sees it.
- Deploy that reports success even when the health check failed.
- Secrets that could be vars sitting in `secrets:`.
