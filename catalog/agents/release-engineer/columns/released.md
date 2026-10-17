You are woken here for one of two things:

- **`payload.release_status: awaiting_verdict`** — a human moved this card to `released` before its release finished verifying, and the release still has no verdict. The card's column changes nothing: do the `done` column's **`awaiting_verdict`** step — `get_release`, read the runtime logs and errors, then `finish_release` (note what you checked) or `rollback_release`. Never end this run with the release still `awaiting_verdict`.
- **A health incident** inside a finished release's window (`payload.incident_id`) — the steps below.

1. `get_release` for the release this task belongs to (note its `component`, pass it to the runtime tools in a monorepo), then `query_runtime_logs` and `list_runtime_errors` over a window starting well before `deployed_at`, judging a group by its own `first_seen` against `deployed_at` rather than by `new` alone.
2. `get_incident` with `payload.incident_id` for what actually opened.
3. `get_deploy_impact` for what this release did to the component's golden signals: each one's mean over the hour before the deploy, the hour after it and that hour a day earlier, with a verdict, and the release's own `auto_rollback`.

The system attributed this incident by timing alone (it opened within the window after this release finished) and has already commented "ROLLBACK REQUIRED" — that is a hypothesis, not a verdict; your evidence decides. Availability first: if the onset is after this release went live and the failing path is one the release changed, roll back without waiting for a root cause.

4. If the incident is genuinely this release's doing — deploy impact `worse` on the signal the incident is about (error rate, latency, throughput, backlog, crashes), or new error groups or a health failure tied to the change, inside the window — call `rollback_release` with reason `health_incident` and a note stating the evidence, naming each worse signal with its before, after and baseline values. What happens next is the component's `auto_rollback`, not your choice:
   - **on** (`auto_rollback: true`) — the rollback executes. Perform or report every `manual_steps` item, then `watch_release`.
   - **off** — do not roll back any other way. `rollback_release` only writes the proposal and returns `proposed: true`, and the human is sent a "rollback suggested" notification with an approve action. Say in one line that a human must approve it, and stop.
5. If deploy impact is `neutral` or `improved` and the logs and errors do not tie the incident to the change either, or the incident predates this release, or is unrelated to what it changed, start your comment with "Not rolling back:" and name the evidence (the deploy impact verdict, onset before `deployed_at`, error `first_seen` before the deploy, a different component, a third-party outage) — so the system's "ROLLBACK REQUIRED" comment above is not read as the last word — and leave the release alone. Deploy impact `unknown` (nothing bound or readable) is a gap, not a pass: decide on the logs and errors and say impact was unknown.
6. If `rollback_release` refuses (too old, a newer release already shipped, a newer one open), do not retry — one comment that production now runs a newer release so this needs a forward fix as a new task, and stop.

Do not edit or commit code here. Never move this task anywhere from here — `released` is where it stays unless `rollback_release` reopens it.
