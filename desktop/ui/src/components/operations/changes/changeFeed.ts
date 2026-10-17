import type { DeployEnvironment, Release, ReleaseChecks, ReleaseStatus } from "@/api";
import { releaseTime } from "@/components/operations/console/opsEntities";
import { inRange } from "@/components/operations/shell/OpsContext";
import type { OpsEntity, OpsRepository } from "@/components/operations/shell/useOpsEntities";
import { SHIPPED_RELEASE_STATUSES } from "@/hooks/useOpsConsole";
import { releaseEnvironment } from "@/lib/deployFeed";

/** What the change feed asks the server for: everything that reached an
 * environment, plus a release waiting to be deployed (Deploy applies to it). */
export const CHANGE_STATUSES: ReleaseStatus[] = ["pending", ...SHIPPED_RELEASE_STATUSES];

/** A release the sweeper or the release engineer is moving right now. */
export const MOVING_RELEASE_STATUSES: ReleaseStatus[] = ["deploying", "verifying", "awaiting_verdict", "rolling_back"];

/** What may still change on its own or wait on a person, whatever its age:
 * read on its own so paging back never leaves an old one out. */
export const IN_FLIGHT_RELEASE_STATUSES: ReleaseStatus[] = ["pending", ...MOVING_RELEASE_STATUSES];

export const CHANGES_LIVE_POLL_MS = 15_000;
export const CHANGES_IDLE_POLL_MS = 60_000;

/** The server keeps a release rollback-able this long after its verdict. */
export const ROLLBACK_WINDOW_MS = 24 * 60 * 60 * 1000;

export function isMovingRelease(r: Pick<Release, "status">): boolean {
  return MOVING_RELEASE_STATUSES.includes(r.status);
}

export function isInFlightRelease(r: Pick<Release, "status">): boolean {
  return IN_FLIGHT_RELEASE_STATUSES.includes(r.status);
}

export function changesPollMs(releases: Pick<Release, "status">[]): number {
  return releases.some(isMovingRelease) ? CHANGES_LIVE_POLL_MS : CHANGES_IDLE_POLL_MS;
}

export type ChangeStatusGroup = "inProgress" | "pending" | "released" | "rolledBack" | "failed";
export type ChangeStatusFilter = ChangeStatusGroup | "all";

export const CHANGE_STATUS_GROUPS: ChangeStatusGroup[] = ["inProgress", "pending", "released", "rolledBack", "failed"];

const STATUS_GROUP: Record<ReleaseStatus, ChangeStatusGroup> = {
  draft: "pending",
  pending: "pending",
  deploying: "inProgress",
  verifying: "inProgress",
  awaiting_verdict: "inProgress",
  rolling_back: "inProgress",
  released: "released",
  rolled_back: "rolledBack",
  failed: "failed",
  superseded: "pending",
};

export function changeStatusGroup(status: ReleaseStatus): ChangeStatusGroup {
  return STATUS_GROUP[status];
}

export function isChangeStatusFilter(value: string | null): value is ChangeStatusFilter {
  return value === "all" || CHANGE_STATUS_GROUPS.some((g) => g === value);
}

export function statusGroupCounts(releases: Pick<Release, "status">[]): Record<ChangeStatusGroup, number> {
  const counts = Object.fromEntries(CHANGE_STATUS_GROUPS.map((g) => [g, 0])) as Record<ChangeStatusGroup, number>;
  for (const r of releases) counts[changeStatusGroup(r.status)] += 1;
  return counts;
}

export function changeAt(r: Release): number {
  return Date.parse(releaseTime(r));
}

// A release in flight stays on the feed whatever the window: it is the
// change somebody is watching (or has to act on) right now.
export function inChangeWindow(r: Release, range: { from: number; to: number }): boolean {
  return inRange(changeAt(r), range) || isMovingRelease(r) || r.status === "pending";
}

export function inEnvironment(r: Release, env: DeployEnvironment): boolean {
  return releaseEnvironment(r) === env;
}

export interface ChangeDay {
  key: string;
  /** Local midnight of the day, epoch ms. */
  start: number;
  releases: Release[];
}

function localDayStart(at: number): number {
  if (!Number.isFinite(at)) return 0;
  const d = new Date(at);
  return new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
}

/** Newest first, one group per local calendar day. */
export function groupByDay(releases: Release[]): ChangeDay[] {
  const sorted = [...releases].sort((a, b) => changeAt(b) - changeAt(a) || b.created_at.localeCompare(a.created_at));
  const days: ChangeDay[] = [];
  for (const r of sorted) {
    const start = localDayStart(changeAt(r));
    const last = days.at(-1);
    if (last && last.start === start) last.releases.push(r);
    else days.push({ key: String(start), start, releases: [r] });
  }
  return days;
}

/** 0 for today, 1 for yesterday, … in local time. */
export function daysAgo(dayStart: number, now: number): number {
  return Math.round((localDayStart(now) - dayStart) / 86_400_000);
}

export function changeStartedAt(r: Release): string {
  return r.deploy_started_at ?? r.created_at;
}

export function changeDurationMs(r: Release): number | undefined {
  if (!r.finished_at) return undefined;
  const ms = Date.parse(r.finished_at) - Date.parse(changeStartedAt(r));
  return Number.isFinite(ms) && ms > 0 ? ms : undefined;
}

export type ImpactTone = "success" | "destructive" | "warning" | "muted";

export type ImpactChip =
  | { kind: "health"; ok: number; failed: number; tone: ImpactTone }
  | { kind: "smoke"; passed: number; failed: number; tone: ImpactTone }
  | { kind: "newErrors"; count: number; tone: ImpactTone }
  | { kind: "earlyStop"; reason: string; tone: ImpactTone }
  | { kind: "gaps"; notes: string[]; tone: ImpactTone }
  | { kind: "notChecked"; tone: ImpactTone }
  | { kind: "checking"; tone: ImpactTone };

/** The release's own verification facts as chips. These are what the soak
 * recorded, not a before/after comparison of metrics. */
export function impactChips(checks: ReleaseChecks | undefined, status: ReleaseStatus): ImpactChip[] {
  const chips: ImpactChip[] = [];
  const health = checks?.health ?? [];
  const smoke = checks?.smoke ?? [];
  const newErrors = checks?.new_errors ?? [];
  if (health.length > 0) {
    const ok = health.filter((h) => h.ok).length;
    const failed = health.length - ok;
    chips.push({ kind: "health", ok, failed, tone: failed > 0 ? "destructive" : "success" });
  }
  if (smoke.length > 0) {
    const passed = smoke.filter((s) => s.ok).length;
    const failed = smoke.length - passed;
    chips.push({ kind: "smoke", passed, failed, tone: failed > 0 ? "destructive" : "success" });
  }
  if (newErrors.length > 0) chips.push({ kind: "newErrors", count: newErrors.length, tone: "warning" });
  if (checks?.early_stop) chips.push({ kind: "earlyStop", reason: checks.early_stop, tone: "destructive" });
  if (chips.length === 0) {
    const waiting = status === "pending" || status === "deploying" || status === "verifying";
    chips.push(waiting ? { kind: "checking", tone: "muted" } : { kind: "notChecked", tone: "muted" });
  }
  if ((checks?.notes ?? []).length > 0) chips.push({ kind: "gaps", notes: checks!.notes!, tone: "muted" });
  return chips;
}

export type ChangeTrigger = "merge" | "dispatch" | "cut" | "batch" | "none";

export function changeTrigger(r: Pick<Release, "mode" | "cut_at">): ChangeTrigger {
  switch (r.mode) {
    case "on_merge":
      return "merge";
    case "dispatch":
      return "dispatch";
    case "batch":
      return r.cut_at ? "cut" : "batch";
    default:
      return "none";
  }
}

export interface ReleaseActionState {
  deploy: boolean;
  finish: boolean;
  rollback: boolean;
  /** Released, but past the window the server still rolls back in. */
  rollbackExpired: boolean;
}

// A hint for which buttons to offer; the server's 409 is what governs (a
// newer release shipped, a before-deploy step still unconfirmed, …).
export function releaseActions(r: Release, now: number): ReleaseActionState {
  const finishedAt = r.finished_at ? Date.parse(r.finished_at) : NaN;
  const releasedRecently = r.status === "released" && Number.isFinite(finishedAt) && now - finishedAt <= ROLLBACK_WINDOW_MS;
  return {
    deploy: r.status === "pending" && (r.mode === "dispatch" || r.mode === "batch"),
    finish: r.status === "awaiting_verdict" || r.status === "failed",
    rollback: r.status === "failed" || r.status === "awaiting_verdict" || releasedRecently,
    rollbackExpired: r.status === "released" && !releasedRecently,
  };
}

export interface ChangeOwner {
  entity?: OpsEntity;
  name: string;
  repositoryName?: string;
  repository?: OpsRepository;
}

/** Which entity a release belongs to: its component, or a repository's only
 * component when the release names none. */
export function changeOwner(
  r: Release,
  entitiesById: Map<string, OpsEntity>,
  repositoriesById: Map<string, OpsRepository>,
  entitiesByRepository: Map<string, OpsEntity[]>,
): ChangeOwner {
  const repository = repositoriesById.get(r.repository_id);
  const listed = entitiesByRepository.get(r.repository_id) ?? [];
  const entity = r.component_id
    ? entitiesById.get(r.component_id)
    : repository?.componentCount === 1 && listed.length === 1
      ? listed[0]
      : undefined;
  return {
    entity,
    name: entity?.name ?? repository?.name ?? "",
    repositoryName: repository?.name,
    repository,
  };
}

export function changeMatches(r: Release, query: string, owner: Pick<ChangeOwner, "name" | "repositoryName">): boolean {
  const q = query.trim().toLowerCase();
  if (!q) return true;
  const haystack = [
    r.version,
    r.tag,
    r.commit_sha,
    r.failure_reason,
    owner.name,
    owner.repositoryName,
    ...(r.tasks ?? []).flatMap((task) => [task.key, task.title]),
  ];
  return haystack.some((v) => typeof v === "string" && v.toLowerCase().includes(q));
}
