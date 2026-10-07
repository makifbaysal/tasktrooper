---
title: Mobile devices and store releases
description: Connecting App Store Connect and Google Play, promoting builds through channels, and letting QA drive real simulators and emulators on this Mac.
---

# Mobile devices and store releases

Mobile work has two separate halves: getting a build in front of a store
(App Store Connect, Google Play), and letting QA actually drive the app on a
device while it works the task. Neither needs anything installed beyond
what is described below — there is no separate mobile server to run.

## Connecting the stores

Settings → Integrations holds both store credentials, saved once for the
whole workspace rather than per repository:

- **App Store Connect** — an API key from App Store Connect → Users and
  Access → Integrations (`key_id`, `issuer_id`, and the `.p8` private key).
  It signs and uploads every iOS release.
- **Google Play Console** — a service account key (JSON) from the Google
  Cloud project linked to your Play Console. It uploads and promotes every
  Android release.

Either credential is validated against the store's own API before it is
saved — a rejected key is never stored. Once connected, a repository is
bound to one app picked from that account: App Store Connect can list its
apps directly, while Google Play's own Developer API has no listing
endpoint at all, so the picker falls back to the separate Reporting API
(which a service account may not be able to reach) or lets you type the
bundle ID / package name by hand.

Signing itself is handled for you: an iOS distribution certificate and
provisioning profile are minted through App Store Connect, and an Android
upload keystore is generated locally (a self-signed key, 25-year validity).
Both are encrypted at rest and pushed to the repository's GitHub Actions
secrets whenever they are minted or renewed; a background sweep renews
anything expiring within 30 days.

## First publish versus update

A repository's store presence moves through one lifecycle per platform,
tracked as `unregistered → onboarding → test_ready → live` (with one
backward edge, `test_ready → onboarding`, for a re-verification failure).
The first publish to either store cannot be automated — App Store Connect
cannot create an app record for you, Google Play cannot create an app or
accept its first upload, and iOS builds need a macOS runner regardless — so
TaskTrooper opens a guided onboarding checklist task instead. Once an app
reaches `live`, every later production deploy is an ordinary store submit
with no manual step, using the store's own promote-without-rebuild path.

A background sweep (every five minutes by default) re-verifies an
onboarding checklist against the store APIs, advances an app to `test_ready`
once it clears, detects when it goes live, and watches a live app's pending
review — a rejection or a halted Play rollout is turned into a production
incident (see [Production incidents](incidents.md)).

## Channels

Promotion is one step forward at a time, never skipped — both stores use the
middle channel for exactly the review or testing step that skipping would
bypass:

| Channel | iOS (App Store Connect) | Android (Google Play) |
|---|---|---|
| `internal` | TestFlight internal groups | `internal` track |
| `external` | TestFlight external groups, plus Beta App Review | `alpha` / `beta` track |
| `production` | App Store version | `production` track |

## The Mobile Apps page

Operations → Mobile Apps lists every repository's iOS and Android app in one
place, with its current lifecycle state, review state, and last released and
last submitted version — the cross-repository view that used to require
opening each repository's own deploy settings, or the store consoles
themselves. Selecting an app opens its detail, where a build can be started,
tracks can be read live from the console, and a channel can be promoted
(promoting to `production` requires an explicit confirmation).

Which machine actually builds and uploads a release is
`release_engine` on the repository: `auto` (the default) tries GitHub
Actions first and falls back to a paired Mac only when Actions definitely
cannot run (no workflow, billing, quota); `github_actions` and `local` each
name one engine with no fallback. When neither can run, the release is
blocked rather than silently retried — most commonly because an iOS build
needs a macOS runner and none is available.

## QA driving a real device

QA (and the mobile-developer and product-manager roles, for their own
walkthroughs) can drive an iOS simulator or an Android emulator directly
through the eleven `mobile_*` tools, all built on [Appium](https://appium.io).
What has to be installed depends on what you want to drive:

| To drive | Needs |
|---|---|
| An iOS simulator | Xcode (macOS only) |
| An Android emulator | The Android SDK — specifically `platform-tools` (`adb`) and the `emulator` package |
| Either one | [Appium](https://appium.io), plus the matching driver: `appium driver install xcuitest` for iOS, `appium driver install uiautomator2` for Android |
| Headless browser QA | A `CHROME_BIN` Chrome/Chromium binary — see [Agent CLIs and API providers](runtimes.md) |

Appium is enabled simply by being installed — there is no toggle and no URL
to configure. If nothing is already answering on `127.0.0.1:4723`,
TaskTrooper's backend starts an Appium hub there itself the first time a
mobile task needs one, and stops it after 10 minutes with no mobile tool call
and no device held; if something already is answering, it uses that one
instead of fighting over the port, and never stops it.
Missing pieces are reported rather than silently skipped, so a QA task
failing for lack of a driver says so instead of failing several minutes into
a run with a raw connection error.

## Device kinds and the shared park

Every registered device — a physical phone, an iOS simulator or an Android
emulator — has a **kind**, and the kind is the only thing that changes how
it is reached:

| Kind | What it is | Runs where |
|---|---|---|
| `remote_adb` (default) | A physical Android phone reached over an adb bridge | Anywhere |
| `ios_simulator` | An `xcrun simctl` simulator on this machine | macOS with Xcode only |
| `android_emulator` | An Android SDK AVD on this machine | Any host with the Android SDK |

Everything else about a device is identical regardless of kind: the same
eleven tools drive it, and devices are **shared**. A run's first `mobile_*`
call takes the first free registered device and keeps it for the rest of
that run; a device already in use reports busy rather than failing, and the
task parks until one frees up — released automatically after five minutes of
inactivity, or immediately by `mobile_release_device`. Releasing a device
deliberately leaves the simulator or emulator itself running, so the next
task to claim it does not pay for a cold boot.

`mobile_launch_app` only opens the package or bundle ID recorded on the
repository's own deploy target — a human writes that field, since a guard an
agent can also write is not a guard.

## See also

- [Agent tools](tools.md) — the full `mobile_*` tool list
- [Deploy targets and recipes](deploy.md) — how a release actually ships once
  an app is live
- [Production incidents](incidents.md) — what happens when a store review is
  rejected or a rollout halts
