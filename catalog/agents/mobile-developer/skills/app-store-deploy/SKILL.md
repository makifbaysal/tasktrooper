---
name: app-store-deploy
category: deployment
description: Use when a mobile app needs a release pipeline - fastlane GitHub Actions to TestFlight (iOS) and Play internal track (Android), with code signing and the release naming that maps to TaskTrooper's prod deploy
---
# App Store Deploy (mobile)

## Overview

Mobile does not deploy to a cloud you run — it deploys to **Apple's and Google's stores**, so there is no GCP/AWS split. The CD pipeline builds a signed artifact and uploads it to a test track (TestFlight / Play internal), which is the mobile equivalent of a staged deploy. Read [[ci-cd-pipeline-authoring]] for the workflow naming/dispatch contract; this skill fills in the store steps.

**Core principle:** Signing is the hard part, not the upload. Get reproducible, secret-driven signing right and the release is a one-command fastlane lane.

## Pick the boilerplate

`search_boilerplate_catalog` → `deploy mobile`. Copy its fastlane config + `deploy-stage/preprod/prod.yml` into the app and set the secrets its README lists.

## Environment mapping

- `stage` → internal test track: **TestFlight** (iOS) / Play **internal** track (Android).
- `preprod` → **TestFlight external** group / Play **closed** (beta) track.
- `prod` → App Store / Play **production** — the workflow file must still contain `prod`/`production`/`release` so the pipeline's `prod_deploy` check detects it, but nothing dispatches it directly: mobile ships through a `batch` delivery profile. A merged mobile task joins the component's draft release and waits in `done`; a human cuts the release on the Deploy tab, and the release engineer then starts the build (`store` executor) or lets a tag push trigger it (`github_actions` executor — see [[ci-cd-pipeline-authoring]]'s batch section).

## iOS — TestFlight

- Build on a **macOS runner** (`runs-on: macos-14`).
- Signing via fastlane **match** (certs/profiles in a private git repo, decrypted with `MATCH_PASSWORD`) — never check certificates into the app repo.
- Auth to App Store Connect with an **API key** (`.p8` + key id + issuer id), not an Apple ID password.
- Lane: `fastlane build_app` → `upload_to_testflight`. For prod, submit for review (`fastlane deliver`).

## Android — Play

- Build the **AAB** (`bundleRelease`); sign with the upload keystore from a base64 secret decoded at runtime.
- Auth with a **Play service-account JSON** (`SUPPLY_JSON_KEY_DATA` secret); upload with fastlane `supply(track: 'internal')` (or `production` for prod).

## Secrets (these ARE real secrets)

Unlike cloud deploys, mobile signing needs genuine secrets in GitHub `secrets:` — App Store Connect API key, `MATCH_PASSWORD`, keystore + passwords, Play service-account JSON. Store them as encrypted secrets, decode at runtime, never commit or log them. There is no OIDC path for the stores.

## Gate & smoke

The "health check" is the store's own processing/validation. Fail the job if fastlane upload fails or the build is rejected. A successful upload is the store gate passing at `ready_for_qa` time — it still does not move the task anywhere: mobile is a `batch` delivery profile, so a merged mobile task waits in `done`, joined into the component's draft release, until a human cuts it.

## When the store executor ships a cut release

For a `store`-executor batch component, `deploy_release` (called by the release engineer once a human has cut the release) is `storeops.Service.StartBuild` for every platform with a linked app — the same build this skill's stage lane produces, just started directly instead of by this workflow's own trigger. It records the internal-channel build number before starting (`baseline_build`) and the release is verified once the internal channel shows a build other than that baseline (`build`). A platform whose start fails keeps its `error` on the release; the others still proceed.

## Common Mistakes

- Certificates or keystores committed to the app repo instead of match/secrets.
- Using an Apple ID + password instead of an App Store Connect API key → 2FA breaks CI.
- Uploading a debug/unsigned build.
- Bumping to the prod track on every merge — prod is `workflow_dispatch` only.

## Red Flags

- Signing material in the repo tree or printed in logs.
- A build job on `ubuntu` trying to build iOS (needs macOS).
- Same version/build number reused → store rejects the upload.
