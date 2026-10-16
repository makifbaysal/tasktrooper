import {
  ApiError,
  type MobileStoreApp,
  type MobileStorePlatform,
  type StoreAppView,
  type StoreChannel,
  type StoreCredentialProvider,
  type StoreTracks,
  type TrackRelease,
  type TrackStatus,
} from "@/api";
import type { ExplorerRow } from "@/components/operations/console/opsEntities";
import { deriveOpsHealth } from "@/components/operations/console/opsStatus";
import type { OpsEntity, OpsRepository } from "@/components/operations/shell/useOpsEntities";
import { NEXT_STORE_CHANNEL, STORE_CHANNELS } from "@/components/operations/StoreReleaseControls";
import type { BadgeProps } from "@/components/ui/badge";

export const STORE_PROVIDER: Record<MobileStorePlatform, StoreCredentialProvider> = {
  ios: "asc",
  android: "google_play",
};

const PLATFORM_ORDER: MobileStorePlatform[] = ["ios", "android"];

export function byPlatform(a: Pick<MobileStoreApp, "platform">, b: Pick<MobileStoreApp, "platform">): number {
  return PLATFORM_ORDER.indexOf(a.platform) - PLATFORM_ORDER.indexOf(b.platform);
}

/** The app the page shows: the one asked for in the URL, else the first. */
export function pickApp<T extends Pick<MobileStoreApp, "platform">>(apps: T[], wanted: string | null): T | undefined {
  return apps.find((a) => a.platform === wanted) ?? apps[0];
}

const LIFECYCLE_STATES = new Set(["unregistered", "onboarding", "test_ready", "live"]);

export function lifecycleLabelKey(state: string): string {
  return `operations.mobileReleases.lifecycle.${LIFECYCLE_STATES.has(state) ? state : "unknown"}`;
}

export function lifecycleVariant(state: string): BadgeProps["variant"] {
  switch (state) {
    case "live":
      return "success";
    case "test_ready":
      return "info";
    case "onboarding":
      return "warning";
    default:
      return "secondary";
  }
}

const REVIEW_STATES = new Set(["waiting_for_review", "in_review", "approved", "rejected"]);

export function reviewLabelKey(state: string): string {
  return `operations.mobileReleases.review.${REVIEW_STATES.has(state) ? state : "unknown"}`;
}

export function reviewVariant(state: string): BadgeProps["variant"] {
  switch (state) {
    case "approved":
      return "success";
    case "rejected":
      return "destructive";
    case "waiting_for_review":
    case "in_review":
      return "warning";
    default:
      return "outline";
  }
}

// Apple's phased release opens to a fixed share per day; the server derives
// user_fraction from the day with this same table (appstore.phasedFraction).
export const IOS_PHASED_FRACTIONS = [0.01, 0.02, 0.05, 0.1, 0.2, 0.5, 1];

export const IOS_PHASED_DAYS = IOS_PHASED_FRACTIONS.length;

/**
 * How far a production release has reached, 0–100, or null when the store
 * does not say. Play omits userFraction on a completed release, so a live
 * release with no fraction is everyone; a draft or one in review reaches nobody.
 */
export function rolloutPercent(release: TrackRelease | undefined): number | null {
  if (!release?.has_release) return null;
  const fraction = release.user_fraction;
  const staged = fraction !== undefined && fraction > 0 && fraction < 1;
  const share = staged ? fractionToPercent(fraction) : null;
  switch (release.status) {
    case "live":
      return share ?? 100;
    case "rolling_out":
    case "halted":
      if (share !== null) return share;
      return fraction !== undefined && fraction >= 1 ? 100 : null;
    case "draft":
    case "in_review":
    case "approved":
    case "none":
      return 0;
    default:
      return share;
  }
}

// Hundredths of a percent, the finest share Play keeps; 0.07 * 100 is not 7 in floating point.
function fractionToPercent(fraction: number): number {
  return Math.round(fraction * 10_000) / 100;
}

export interface RolloutTarget {
  /** 0–100, in hundredths of a percent. */
  percent: number;
  /** What the server takes: above 0, at most 1. */
  fraction: number;
}

/** A typed share ("12.5", "0,5") as Play takes it. One that rounds to
 * nothing is refused: Play reads a zero fraction as a completed release. */
export function parseRolloutTarget(text: string): RolloutTarget | null {
  const trimmed = text.trim();
  if (trimmed === "") return null;
  const raw = Number(trimmed.replace(",", "."));
  if (!Number.isFinite(raw)) return null;
  const hundredths = Math.round(raw * 100);
  if (hundredths <= 0 || hundredths > 10_000) return null;
  return { percent: hundredths / 100, fraction: hundredths / 10_000 };
}

export function formatPercent(percent: number, lang: string): string {
  const digits = percent > 0 && percent < 10 && !Number.isInteger(percent) ? 1 : 0;
  try {
    return percent.toLocaleString(lang, { maximumFractionDigits: digits });
  } catch {
    return percent.toFixed(digits);
  }
}

/** Day N of Apple's seven-day phased release, when the fraction is one of its days. */
export function phasedReleaseDay(platform: MobileStorePlatform, release: TrackRelease | undefined): number | null {
  if (platform !== "ios" || !release?.has_release) return null;
  if (release.status !== "rolling_out" && release.status !== "halted") return null;
  const fraction = release.user_fraction;
  if (fraction === undefined) return null;
  const index = IOS_PHASED_FRACTIONS.findIndex((f) => Math.abs(f - fraction) < 1e-6);
  return index < 0 ? null : index + 1;
}

export type PromoteBlock = "emptySource" | "notLive" | "notReady";

export interface PromoteStep {
  from: StoreChannel;
  to: StoreChannel;
  blocked: PromoteBlock | null;
  /** Production is the one the server checks a typed phrase for. */
  needsPhrase: boolean;
}

/**
 * The only promotion out of `from`: the next channel, never the one after it.
 * Mirrors storeops.PromoteChannel's own checks so the button is not offered
 * where the server would refuse.
 */
export function promoteStep(
  app: Pick<MobileStoreApp, "state">,
  tracks: StoreTracks,
  from: StoreChannel,
): PromoteStep | null {
  const to = NEXT_STORE_CHANNEL[from];
  if (!to) return null;
  const toProduction = to === "production";
  let blocked: PromoteBlock | null = null;
  if (!tracks[from].has_release) blocked = "emptySource";
  else if (toProduction && app.state !== "live") blocked = "notLive";
  else if (app.state !== "live" && app.state !== "test_ready") blocked = "notReady";
  return { from, to, blocked, needsPhrase: toProduction };
}

export function promoteSteps(app: Pick<MobileStoreApp, "state">, tracks: StoreTracks): PromoteStep[] {
  return STORE_CHANNELS.map((channel) => promoteStep(app, tracks, channel)).filter((s): s is PromoteStep => s !== null);
}

export type RolloutBlock =
  | "notLive"
  | "noRelease"
  | "notRollingOut"
  | "fullyRolledOut"
  | "halted"
  | "alreadyHalted"
  | "notHalted";

export interface AndroidRolloutState {
  current: number | null;
  change: RolloutBlock | null;
  halt: RolloutBlock | null;
  resume: RolloutBlock | null;
}

/** Which of Play's production rollout moves apply now, and why not when one does not. */
export function androidRolloutState(app: Pick<MobileStoreApp, "state">, production: TrackRelease | undefined): AndroidRolloutState {
  const current = rolloutPercent(production);
  if (app.state !== "live") return { current, change: "notLive", halt: "notLive", resume: "notLive" };
  if (!production?.has_release) return { current, change: "noRelease", halt: "noRelease", resume: "noRelease" };
  switch (production.status) {
    case "rolling_out":
      return { current, change: null, halt: null, resume: "notHalted" };
    case "halted":
      return { current, change: "halted", halt: "alreadyHalted", resume: null };
    case "live":
      return { current, change: "fullyRolledOut", halt: null, resume: "notHalted" };
    default:
      return { current, change: "notRollingOut", halt: "notRollingOut", resume: "notHalted" };
  }
}

export const ROLLOUT_PRESETS = [1, 2, 5, 10, 20, 50, 100];

/** Play only grows a staged rollout; pulling it back is a halt. */
export function isValidRolloutTarget(target: number, current: number | null): boolean {
  if (!Number.isFinite(target) || target <= 0 || target > 100) return false;
  return current === null || target > current;
}

export type IOSBlock = "notLive" | "notRead" | "inReview" | "notApproved" | "alreadyApproved" | "nothingQueued";

export interface IOSProductionState {
  /** The version a submit or release acts on: the one queued behind what
   * customers have, else production's own while nothing is on sale. */
  queued?: { version: string; status?: TrackStatus };
  submit: IOSBlock | null;
  release: IOSBlock | null;
}

const ON_SALE: (TrackStatus | undefined)[] = ["live", "rolling_out", "halted"];

/** Submit and Release from the channel as the store reported it just now —
 * never the row's review_state, which only follows a submit the release
 * pipeline made. */
export function iosProductionState(app: Pick<MobileStoreApp, "state">, production: TrackRelease | undefined): IOSProductionState {
  if (app.state !== "live") return { submit: "notLive", release: "notLive" };
  if (!production) return { submit: "notRead", release: "notRead" };
  const onSale = production.has_release && ON_SALE.includes(production.status);
  const queued = production.pending_version
    ? { version: production.pending_version, status: production.pending_status }
    : production.has_release && !onSale
      ? { version: production.version || production.build || "", status: production.status }
      : undefined;
  switch (queued?.status) {
    case "draft":
      return { queued, submit: null, release: "notApproved" };
    case "in_review":
      return { queued, submit: "inReview", release: "inReview" };
    case "approved":
      return { queued, submit: "alreadyApproved", release: null };
    default:
      return { queued, submit: "nothingQueued", release: "nothingQueued" };
  }
}

export function hasTracks(app: Pick<StoreAppView, "tracks" | "tracks_synced_at">): boolean {
  return Boolean(app.tracks && app.tracks_synced_at);
}

export type RefusalKind = "invalid" | "notReady" | "credentials" | "notFound" | "timeout" | "failed";

export interface Refusal {
  kind: RefusalKind;
  detail: string;
}

const GO_PACKAGE_PREFIX = /\b(?:storeops|appstore|googleplay|domain|port|pipeline):\s+/g;

/** The server's own sentence, minus the Go package prefixes it carries along. */
export function cleanServerMessage(message: string): string {
  const cleaned = message.replace(GO_PACKAGE_PREFIX, "").replace(/\s+/g, " ").trim();
  return cleaned ? cleaned.charAt(0).toUpperCase() + cleaned.slice(1) : "";
}

/** What kind of no a store action got, from the status its handler maps it to. */
export function describeRefusal(err: unknown): Refusal {
  const detail = err instanceof Error ? cleanServerMessage(err.message) : "";
  if (!(err instanceof ApiError)) return { kind: "failed", detail };
  switch (err.status) {
    case 400:
      return { kind: "invalid", detail };
    case 409:
      return { kind: "notReady", detail };
    case 424:
      return { kind: "credentials", detail };
    case 404:
      return { kind: "notFound", detail };
    case 408:
      return { kind: "timeout", detail };
    default:
      return { kind: "failed", detail };
  }
}

export const INTEGRATIONS_PATH = "/settings/integrations";

/** The repository-level store page, for apps no mobile component carries. */
function storeAppsPath(repositoryId: string): string {
  return `/operations/mobile/app/${encodeURIComponent(repositoryId)}`;
}

/** One explorer row per repository whose store apps would otherwise be
 * unreachable: no listed component of it has the `mobile` role, the only one
 * with a Releases tab. */
export function orphanStoreAppRows(
  apps: Pick<StoreAppView, "repository_id" | "repository_name" | "platform">[],
  entities: Pick<OpsEntity, "role" | "repositoryId">[],
  repositories: Pick<OpsRepository, "id" | "name" | "projectIds">[],
): ExplorerRow[] {
  const carried = new Set(entities.filter((e) => e.role === "mobile").map((e) => e.repositoryId));
  const byRepository = new Map<string, typeof apps>();
  for (const app of apps) {
    if (carried.has(app.repository_id)) continue;
    byRepository.set(app.repository_id, [...(byRepository.get(app.repository_id) ?? []), app]);
  }
  const repoById = new Map(repositories.map((r) => [r.id, r]));
  return [...byRepository].map(([repositoryId, own]) => {
    const repo = repoById.get(repositoryId);
    const name = repo?.name ?? own[0]?.repository_name ?? "";
    return {
      key: `storeApps:${repositoryId}`,
      kind: "storeApps",
      href: storeAppsPath(repositoryId),
      platforms: [...new Set(own.map((a) => a.platform))].sort((a, b) => PLATFORM_ORDER.indexOf(a) - PLATFORM_ORDER.indexOf(b)),
      name,
      type: "mobile",
      repositoryId,
      repositoryName: name,
      projectIds: repo?.projectIds ?? [],
      incidents: [],
      health: deriveOpsHealth({ connected: false, incidents: [] }),
    };
  });
}
