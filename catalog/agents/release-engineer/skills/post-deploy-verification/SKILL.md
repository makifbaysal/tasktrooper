---
name: post-deploy-verification
category: release
description: What to look for in runtime logs and errors after a deploy, and how to pick read-only checks from the task's acceptance criteria
---
# Post-deploy verification

A deploy that went green tells you the process started. It says nothing about whether the change it shipped actually works, or whether it broke something else that happened to keep running. Verification is what closes that gap, and it happens every time — not only when something looks wrong.

## Read the evidence already gathered

`get_release` returns `checks`: health samples (`ok`/`status`/`latency_ms`), smoke results (per configured check), `new_errors` (runtime error groups first seen after the deploy), and `notes` — gaps the sweeper hit (no health URL, no bound environment, no base URL). A note is not a pass; it means that part was never checked, and you say so rather than reading silence as "fine".

## Read the logs and errors yourself

```
query_runtime_logs({ environment_id, since: release.deployed_at })
list_runtime_errors({ environment_id })
```

Look for:

- Anything that started erroring only after `deployed_at` — a stack trace, a 5xx burst, a repeated warning that was not there before.
- A new error group whose message or path lines up with what the task actually changed — that is signal, not noise.
- An error group that already existed before this deploy — pre-existing noise is not evidence against THIS release. Say so explicitly if you decide not to act on it ("error group X predates this deploy, first seen <date>").

## Pick your own read-only checks

The task's acceptance criteria describe what changed. Turn the ones with an observable surface into a GET/HEAD request or a page load:

- An API criterion ("returns 201 with the new field") → `fetch_url` the real endpoint, read-only, with data that already exists — never data you create for the check.
- A UI criterion ("the settings page shows X") → `browser_navigate` to the real production page and `browser_read_dom`/`browser_screenshot` for the piece that changed.
- A criterion with no observable surface after the fact (an internal refactor, a migration) has nothing to check here — the deploy/health/error evidence is what stands in for it.

Never write, seed, or clean up anything in production to run a check — if a criterion can only be verified by creating data, the smoke check (or the developer's own instrumentation) is the tool for that, not you improvising a write.

## When the evidence is incomplete

A `notes` entry ("no health URL — health not probed", "no bound environment — runtime errors not read") is a gap, not a green light. State it in your `finish_release`/`rollback_release` note exactly as it is: what you could check, what you could not, and why you are finishing anyway (or not).
