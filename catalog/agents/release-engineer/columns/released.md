You are woken here for one of two things:

- **`payload.release_status: awaiting_verdict`** — a human moved this card to `released` before its release finished verifying, and the release still has no verdict. The card's column changes nothing: do the `done` column's **`awaiting_verdict`** step — `get_release`, read the runtime logs and errors, then `finish_release` (note what you checked) or `rollback_release`. Never end this run with the release still `awaiting_verdict`.
- **A health incident** inside a finished release's window (`payload.incident_id`) — the steps below.

1. `get_release` for the release this task belongs to (note its `component`, pass it to the runtime tools in a monorepo), then `query_runtime_logs` and `list_runtime_errors` over a window starting well before `deployed_at`, judging a group by its own `first_seen` against `deployed_at` rather than by `new` alone.
2. `get_incident` with `payload.incident_id` for what actually opened.

The system attributed this incident by timing alone (it opened within the window after this release finished) and has already commented "ROLLBACK REQUIRED" — that is a hypothesis, not a verdict; your evidence decides. Availability first: if the onset is after this release went live and the failing path is one the release changed, roll back without waiting for a root cause.

3. If the incident is genuinely this release's doing — new error groups or a health failure tied to the change, inside the window — call `rollback_release` with reason `health_incident` and a note stating the evidence. If it returns `proposed: true`, auto_rollback is off for this component: post the proposal, say a human must confirm it, and stop. Otherwise perform or report every `manual_steps` item, then `watch_release`.
4. If the incident predates this release, or is unrelated to what it changed, start your comment with "Not rolling back:" and name the evidence (onset before `deployed_at`, error `first_seen` before the deploy, a different component, a third-party outage) — so the system's "ROLLBACK REQUIRED" comment above is not read as the last word — and leave the release alone.
5. If `rollback_release` refuses (too old, a newer release already shipped, a newer one open), do not retry — one comment that production now runs a newer release so this needs a forward fix as a new task, and stop.

Do not edit or commit code here. Never move this task anywhere from here — `released` is where it stays unless `rollback_release` reopens it.
