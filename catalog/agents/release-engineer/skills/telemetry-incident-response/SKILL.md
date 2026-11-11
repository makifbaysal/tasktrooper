---
name: telemetry-incident-response
category: operations
description: How to work an alert that TaskTrooper Telemetry opened - a task titled "Incident:" whose brief says Telemetry opened it. Find the root cause with the telemetry_* tools, then either open the right developer's task with the findings or tell the user what only they can fix.
---

# Telemetry incident response

When a monitor or an SLO in the connected TaskTrooper Telemetry fires, the system opens a TaskTrooper incident and hands you a card titled `Incident: …`. Its brief names the service, the endpoint, the telemetry incident id and a rules-engine first pass. That first pass is a hypothesis. **You own the alert from here to the end**: diagnose it from the telemetry, then route it, and you never edit code yourself.

If a `telemetry_*` tool answers that telemetry is not connected or rejected its key, nothing can be diagnosed from it. Say that on the card, call `propose_incident_remedy` (kind `config`) asking the user to reconnect it in Settings, TaskTrooper Telemetry, and stop. Do not diagnose from the code alone and present it as a finding.

## 1. Diagnose (read only, in this order)

1. `get_incident` with the TaskTrooper id from the brief, then `telemetry_get_incident` with the telemetry id: start time, value and peak, the monitor, the timeline. Its `rootCause` is a lead to verify, never the conclusion.
2. `telemetry_list_incidents` for the same service: an earlier incident with the same shape means the previous fix did not hold; say why this time differs.
3. `telemetry_get_service` with the service name: error rate, latency, the endpoints ranked with their ids, deploys, dependencies. Pick a `range` that covers the start (6h for something that began two hours ago). Take the slowest or most failing endpoint's `id` slug and call it again with `endpoint_id` for the root-cause comparison: the top child is the dependency that got slower or errored, a large own-time delta is the endpoint's own code. No baseline requests means no comparison; retry `baseline: "1d"` or `"7d"`, else say the baseline is missing.
4. `telemetry_search_traces` with `errors_only` (errors) or `sort: "slowest"` and `min_ms` (latency), the endpoint in `"METHOD /route"` form, `limit` 5 to 10. Open one or two with `trace_id` and find the failing or slow span: its dependency, status, db statement, exception.
5. `telemetry_search_logs` with the `trace_id` first, then the service with `level: "error"` and separately `"warn"` (levels are exact), and a `q` taken from the exception.
6. `telemetry_list_errors` for the service, then one group with `fingerprint`: which exception, since when (a `firstSeen` just after a deploy is a new error from that deploy), the stack.
7. `telemetry_list_deploys` for the service and for the dependency the trace pointed at: a deploy right before the start is a correlation, not proof. Then `list_deployments` and `get_deploy_impact` on our side to tie it to a release and a task.
8. When it points at infrastructure: `telemetry_api_get` with `infra/hosts`, `infra/databases`, `infra/k8s/pods`, `infra/clusters` and so on.

Rules: **cite or do not claim** (a trace id, a log timestamp, a deploy version, an error fingerprint for every statement of cause). An empty result after an endpoint filter is suspect, not healthy: the route form and the slug are different things. If a result says `truncated`, narrow and call again before concluding. Log values arrive masked; a masked value is not a bug. If the evidence is thin, the cause is **unknown** and you say which signal is missing.

## 2. Classify

| Cause | Examples | Who acts |
|---|---|---|
| **Our code** | exception from a new deploy, slow query we wrote, bad retry loop, regression in an endpoint | the developer, through a task you open |
| **Infrastructure or configuration** | database out of connections or disk, node pressure, expired certificate, missing or rotated secret, wrong env var, quota | the user |
| **Capacity** | sustained saturation with no code change | the user (scale), possibly a code task to reduce load |
| **A vendor or dependency** | third-party API down, cloud region incident | the user (switch, wait, escalate) |
| **Unknown** | not enough telemetry | the user, with exactly what to turn on |

A rollback is a fix for code that just shipped: if the deploy right before the start is the cause and the component has a bound release, say so on the card and use the normal release path (`rollback_release`, which proposes to the user when auto rollback is off).

## 3a. Our code: open the developer's task

`create_board_task` for the component's developer (backend, frontend, mobile, data or game by the component's area), in the repository of the affected code (`list_repositories` if it is not the card's own). The description carries the whole case so the developer needs none of the telemetry tools:

- what broke, when, how bad (error rate, latency, affected endpoint), in two sentences;
- the evidence: trace ids, the failing span, log lines with timestamps, the error fingerprint and stack, the deploy and the task that shipped it;
- the fix you expect and the test that proves it (the failing request, the missing guard);
- the telemetry incident id, so the card links back.

Park your own card on it (`update_board_task` with `blocked_by`) when the repository's workflow supports it, and add one `add_task_comment` on your card naming the task you opened. Then `propose_incident_remedy` with kind `code_fix`, the summary and the evidence, so the TaskTrooper incident shows the diagnosis and who has it.

## 3b. The user must act: say exactly what

`propose_incident_remedy` with kind `config`, `capacity` or `dependency`. Its `summary` is what the user is notified with, so it starts with the instruction: "An incident opened on orders-api: the database is out of connections; you need to raise the connection limit to 200 on the production Postgres." Then `steps` the user can run or click, each as command, expected output and what to do if it fails, and `evidence` with the telemetry facts behind it. Set confidence honestly.

If you cannot go on without an answer (which of two environments, whether a restart is acceptable, whether a vendor outage may be waited out), use `ask_user` with concrete options instead of guessing; work stops until they answer.

## 4. Close

Resolve nothing on the strength of a fix having shipped. After the developer's change is released (or the user reports the infrastructure fixed), re-read `telemetry_get_service` and `telemetry_list_incidents`; only when telemetry shows the service healthy and the incident resolved, call `resolve_incident` with a note naming that evidence. The system also resolves the TaskTrooper incident by itself when telemetry resolves its own.

## Never

- Edit code, commit, or open a pull request yourself.
- Call a state-changing request on production, or try to change telemetry (acknowledge, resolve, create monitors): these tools only read.
- Invent a service name, an id or a cause. Service names come from the brief and `telemetry_get_service`; trace ids from the search; incident ids from the list.
- Treat a quiet telemetry as a healthy service when its own answer says it was unreachable.
