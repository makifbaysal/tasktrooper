import type { Incident, Release } from "@/api";
import { releaseActions } from "@/components/operations/changes/changeFeed";
import { incidentEnvironment, isOpenIncident } from "@/components/operations/console/opsStatus";
import { releaseEnvironment } from "@/lib/deployFeed";

/** release.DefaultHealthWindow: how long after a release finished a
 * production incident is still put down to it (release/attribution.go). */
export const RELEASE_HEALTH_WINDOW_MS = 15 * 60_000;

export type RollbackSuggestionReason = "failed" | "verify" | "incident";

export interface RollbackSuggestion {
  reason: RollbackSuggestionReason;
  /** The issue the release is held responsible for (reason `incident`). */
  incident?: Incident;
  /** The release's own words for it: its failure, or why the soak stopped. */
  detail?: string;
}

function finishedAt(r: Pick<Release, "finished_at" | "deployed_at">): number {
  const at = Date.parse(r.finished_at ?? r.deployed_at ?? "");
  return Number.isFinite(at) ? at : NaN;
}

function onsetOf(i: Pick<Incident, "first_seen_at" | "last_seen_at">): number {
  const at = Date.parse(i.first_seen_at || i.last_seen_at);
  return Number.isFinite(at) ? at : NaN;
}

/** The release an incident is put down to, as release.AttributeRelease does:
 * production only, the newest released one that finished at or before the
 * incident began and no longer than the health window before it. */
export function attributedRelease(incident: Incident, releases: Release[]): Release | undefined {
  if (incidentEnvironment(incident.env || "prod") !== "production") return undefined;
  const onset = onsetOf(incident);
  if (!Number.isFinite(onset)) return undefined;
  let best: Release | undefined;
  for (const r of releases) {
    if (r.repository_id !== incident.repository_id || r.status !== "released") continue;
    if (releaseEnvironment(r) !== undefined && releaseEnvironment(r) !== "production") continue;
    const finished = finishedAt(r);
    if (!Number.isFinite(finished) || finished > onset || onset - finished > RELEASE_HEALTH_WINDOW_MS) continue;
    if (!best || finished > finishedAt(best)) best = r;
  }
  return best;
}

/** The soak's evidence alone says the release is unhealthy (ReleaseChecks.Failing). */
export function checksFailing(r: Pick<Release, "checks" | "profile">): boolean {
  const c = r.checks;
  if (!c) return false;
  if (c.early_stop) return true;
  if ((c.smoke ?? []).some((s) => !s.ok)) return true;
  if (c.impact?.signals?.some((s) => s.breached)) return true;
  return (c.new_errors ?? []).length > (r.profile?.verify?.max_new_errors ?? 0);
}

/** A rollback waiting on a person. The server keeps no proposal on the
 * release: with auto rollback off, the release engineer's rollback is only
 * proposed (a task comment and a `rollback_suggested` notification) and the
 * release waits — failed, awaiting a verdict on failing evidence, or released
 * with a production issue put down to it. That is what this reads back. */
export function rollbackSuggestion(r: Release, openIncidents: Incident[], now: number): RollbackSuggestion | null {
  if (r.profile?.auto_rollback) return null;
  if (!releaseActions(r, now).rollback) return null;
  if (r.status === "failed") return { reason: "failed", detail: r.failure_reason };
  if (r.status === "awaiting_verdict" && checksFailing(r)) return { reason: "verify", detail: r.checks?.early_stop };
  const incident = openIncidents
    .filter((i) => isOpenIncident(i) && attributedRelease(i, [r])?.id === r.id)
    .sort((a, b) => onsetOf(a) - onsetOf(b))[0];
  return incident ? { reason: "incident", incident } : null;
}

/** For an issue's panel: the release a live issue is put down to, when a
 * person has to approve rolling it back. */
export function incidentRollbackRelease(incident: Incident, releases: Release[], now: number): Release | undefined {
  if (!isOpenIncident(incident)) return undefined;
  const r = attributedRelease(incident, releases);
  if (!r || r.profile?.auto_rollback || !releaseActions(r, now).rollback) return undefined;
  return r;
}
