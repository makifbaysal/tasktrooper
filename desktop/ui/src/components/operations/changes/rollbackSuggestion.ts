import { ROLLBACK_REFUSALS, type Incident, type Release, type ReleaseRollbackProposal } from "@/api";

/** A rollback the server says waits on a person: approvable, or proposed but
 * refused now (`refusal` says why). Nothing is inferred on this side — a
 * release with no `rollback_proposal` suggests nothing. */
export type RollbackSuggestion =
  | { kind: "approvable"; proposal: ReleaseRollbackProposal }
  | { kind: "refused"; proposal: ReleaseRollbackProposal; refusal: string };

export function rollbackSuggestion(r: Pick<Release, "rollback_proposal" | "rollback_allowed" | "rollback_refusal">): RollbackSuggestion | null {
  const proposal = r.rollback_proposal;
  if (!proposal) return null;
  if (r.rollback_allowed === true) return { kind: "approvable", proposal };
  return { kind: "refused", proposal, refusal: r.rollback_refusal ?? "" };
}

/** The refusal's translation key under `operations.changes.rollbackSuggestion.refusals`,
 * or undefined for a code this build does not know (shown as sent). */
export function refusalKey(refusal: string): string | undefined {
  return ROLLBACK_REFUSALS.some((r) => r === refusal) ? refusal : undefined;
}

/** For an issue's panel: the release whose proposal the server put down to
 * this issue — the proposal names it, or the issue's task (the one
 * attribution writes on it) is one the release carries. Never by repository
 * alone; the proposal naming the issue wins, then the newest proposal. */
export function incidentRollbackRelease(incident: Pick<Incident, "id" | "task_id">, releases: Release[]): Release | undefined {
  const named = (r: Release) => r.rollback_proposal?.incident_id === incident.id;
  const carries = (r: Release) => Boolean(incident.task_id) && (r.tasks ?? []).some((task) => task.id === incident.task_id);
  const proposedAt = (r: Release) => Date.parse(r.rollback_proposal?.proposed_at ?? "") || 0;
  return releases
    .filter((r) => r.rollback_proposal && (named(r) || carries(r)))
    .sort((a, b) => Number(named(b)) - Number(named(a)) || proposedAt(b) - proposedAt(a))[0];
}

/** A dismiss route answers the release; an empty answer is applied locally. */
export function withoutProposal(release: Release, answer: Release | undefined): Release {
  return answer && typeof answer === "object" && answer.id ? answer : { ...release, rollback_proposal: null };
}
