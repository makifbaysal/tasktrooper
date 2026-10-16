---
title: Production incidents
description: How alerts and failed health checks become deduplicated incidents, what gets suggested as a fix, and how much of the fix the board is allowed to carry out.
---

# Production incidents

TaskTrooper watches production the same way an on-call engineer would: alerts
come in, repeats fold together instead of paging you three times for one
outage, a rules engine takes a first pass at what happened, and — depending
on how much you trust it for that repository — it either stops at a written
proposal or carries the fix through the board itself.

## Where an incident comes from

Every incident lands in one deduplicated table regardless of source:

- **The alert webhook.** `POST /v1/repositories/{id}/incidents` accepts
  Alertmanager, Sentry and GCP Cloud Monitoring payloads, or a generic JSON
  body (`{title, severity, env, detail, fingerprint, resolved}`) for anything
  else you can point at a webhook. `?env=` and `?source=` fill in whatever
  the payload itself omits. A payload marked as a recovery (Alertmanager's
  `resolved`, for instance) closes the matching incident instead of opening
  one.
- **The health monitor.** TaskTrooper polls every deploy target's `health_url`
  on its own schedule (`prod_ops.probe_interval`, three minutes by default).
  **Two consecutive failed probes open an incident; one successful probe
  closes it.** This asymmetry is deliberate — a single blip should not page
  anyone, but recovery should not wait for a second confirmation once the
  service answers again.
- **A failed deploy.** A pipeline run's own stage, preprod or prod deploy
  failing is reported the same way as an external alert.

## Alert tokens

The alert webhook above needs the app's own API key, which an alerting system
does not have. Give it an **alert token** instead: create one per alerter with
`POST /v1/repositories/{id}/alert-tokens` (`{"label": "Alertmanager",
"default_env": "prod"}`) and point the alerter at the `ingest_path` it returns,
`/v1/alerts/ingest/<token>`. That URL takes exactly the same payloads as the
webhook, `?env=` and `?source=` included; `default_env` is used when neither
the payload nor `?env=` names an environment. The token is shown once — only
a hash of it is kept — and listing tokens shows each one's label and when it
last delivered an alert. Delete a token
(`DELETE /v1/repositories/{id}/alert-tokens/{tokenId}`) to revoke it. Treat
the URL as a secret: anyone holding it can open incidents on that repository.

Each token is rate limited, so a leaked one cannot flood the board: by
default 60 requests a minute (with a burst of 10) and at most 20 **new**
incidents an hour (`prod_ops.alert_ingest` in the configuration). Past either
limit the alerter gets `429 {"error":"rate_limited"}`. The hourly cap only
stops new incidents — an alert that repeats an incident already open still
counts as an occurrence, and a recovery still closes its incident.

In the desktop app the server listens on `127.0.0.1` only, so an alerter on
another machine cannot reach this URL directly; put a tunnel of your own in
front of it (for example `cloudflared` or `ngrok` forwarding to the local
port) and give the alerter the tunnel's address with the same path. The
desktop app picks a new local port each time it starts, so the tunnel has to
follow it.

## Notifications

A new incident at `high` or `critical` — and a `critical` one that keeps
recurring — shows a desktop notification on macOS, as do its recovery and a
fix proposed by an agent. The same alert notifies at most once every 30
minutes, and so does its recovery, even when it flaps: an alert that keeps
resolving and firing again opens a new incident each time, but you hear about
the first outage and the first recovery, not every cycle. Medium and low
incidents stay on the Incidents page without interrupting you.

## Severity

Every alert normalizes onto four levels — `critical`, `high`, `medium`,
`low` — from whatever vocabulary the source uses (`P1`, `fatal`, `warning`,
`sev2`, and so on); anything unrecognized becomes `medium` rather than being
silently dropped to the bottom. Severity only ever escalates on a recurrence,
never downgrades on its own.

## Deduplication

An incident is keyed by `(repository, environment, fingerprint)`, and only
one **open** incident may exist per key at a time — a partial unique index
enforces it at the database level. A storm of the same alert folds into that
one incident's occurrence count instead of opening a second card; the same
fingerprint recurring months after the first one was resolved opens a new
incident, because a resolved or ignored incident is terminal and no longer
part of the dedupe set.

## Suggested remedies

The rules engine (no LLM call, just facts) looks at the incident together
with recent deploys, any prior resolved incident with the same fingerprint,
and the environment's deploy target, and classifies it into one shape of fix:

| Kind | When it fires |
|---|---|
| Rollback | a deploy finished within 45 minutes before the incident started |
| The remedy that worked last time | this exact fingerprint was resolved before, and its old fix is replayed |
| Config | the error signature points at configuration |
| Dependency | the error signature points at a dependency |
| Capacity | the error signature points at resource exhaustion |
| Code defect | none of the above fits |

An incident the rules engine cannot classify still gets a diagnostic
checklist rather than silence — there is always something written on the
card. Every remedy carries a confidence score, and a short list of the
evidence it rests on (the recent deploy, the earlier occurrence, the probe
history) so a human reading it can judge the guess quickly. Whoever wrote the
current proposal is recorded too — the automatic first pass, an agent that
diagnosed further, or a human who wrote one by hand — so a later re-triage
never overwrites a human's own write-up.

## Per-repository policy

Each repository has an **incident policy** (Deploy targets page, or `PUT
/v1/repositories/{id}/incident-policy`) that decides how far an incident is
allowed to go on its own:

| Policy | What happens |
|---|---|
| **Off** — record only | The incident is logged and shown; nothing is dispatched |
| **Suggest** (default) | A diagnosis task is opened and must stop at a written proposal — a human decides whether to act on it |
| **Auto fix** | The diagnosis task may carry the fix all the way through the board — tests, review and deploy still gate it exactly as any other task would |

## The Incidents page

Operations → Production incidents lists every live incident (open, triaging,
proposed or fixing) by default, with a filter to include resolved and ignored
ones. Selecting an incident shows its severity, environment, source,
first-seen and last-seen times, occurrence count, the current remedy and its
confidence, the raw alert payload, and its full timeline. From there you can
**Triage** (re-run the remedy engine), **Resolve**, or **Ignore** it — an
ignored incident stays terminal and a later recurrence of the same
fingerprint opens a fresh one rather than reopening it.

## How an incident becomes a task

Under the **Suggest** or **Auto fix** policy, an incident above the triage
threshold opens a diagnosis task on the board, carrying the incident's
details and the rules engine's first-pass remedy. From there it moves like
any other card: under Suggest, the assigned role investigates and writes up
(or corrects) the proposal, then stops — a human applies it. Under Auto fix,
the same task is allowed to carry a code change through Code Review, QA and
deploy, so a rollback, a config change or a fix ships without a human having
to re-open the door at every step. Either way, the ordinary release gates on
[Deploy targets and recipes](deploy.md) still apply — an incident policy
widens what an agent is allowed to attempt, not what is allowed to ship
unreviewed.

The 15-minute window right after a release finishes is treated specially: an
incident whose onset falls inside it is attributed to that specific release
and its newest task, which is what wakes the **release engineer** on the
task's card in Released — see [Deploy targets and recipes](deploy.md) →
"Releases" — rather than only producing generic "roll back the last deploy"
advice with no card to act on. The release engineer reads the logs and the
incident, and either calls `rollback_release` (executed automatically when
the component's delivery profile has `auto_rollback` on, or written up as a
proposal for a human to confirm otherwise) or explains why the incident is
not this release's doing and leaves it alone. A component with no release
history to attribute to falls back to the older, environment-keyed remedy
above, gated by the deploy target's own `auto_rollback` field instead.

## See also

- [Deploy targets and recipes](deploy.md) — health URLs, rollback mechanisms
  and the release flow an incident's remedy plugs into
- [Agent tools](tools.md) — `list_incidents`, `get_incident`,
  `propose_incident_remedy`, `resolve_incident`
