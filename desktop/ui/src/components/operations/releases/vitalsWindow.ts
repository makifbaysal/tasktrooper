import type { ComponentSignals } from "@/api";
import type { OpsTimeRange } from "@/components/operations/shell/OpsContext";

export const MOBILE_VITAL_SIGNALS = ["mobile.crash_free_users", "mobile.crash_rate", "mobile.anr_rate", "mobile.cold_start"] as const;

const HOUR = 3_600_000;
const DAY = 24 * HOUR;

/** What the Releases page reads its vitals over: the last 7 days, whatever
 * the console's range (store state does not follow it), its end on the hour
 * so one window serves a whole hour of polls. */
export function vitalsWindow(now = Date.now()): OpsTimeRange {
  const to = Math.floor(now / HOUR) * HOUR;
  return { preset: "7d", from: to - 7 * DAY, to };
}

// Google Play's daily vitals land a day or two after the day they cover.
export const PLAY_LAG_MS = 2 * DAY;

// The vitals Play reports per day; cold start comes per app version.
const DAILY_SIGNALS = new Set<string>(["mobile.crash_free_users", "mobile.crash_rate", "mobile.anr_rate"]);

export const PLAY_VITALS_KIND = "google_play_vitals";

/** A daily vital bound to Google Play vitals with no value in a window that
 * ends within Play's delay is most likely not published yet, rather than
 * missing. Any other source (Sentry, New Relic) reports as it goes. */
export function vitalsLagLikely(
  signal: string,
  window: Pick<OpsTimeRange, "to">,
  sourceKind: string | undefined,
  now = Date.now(),
): boolean {
  return sourceKind === PLAY_VITALS_KIND && DAILY_SIGNALS.has(signal) && now - window.to < PLAY_LAG_MS;
}

/** Each bound signal's data source kind, from a component's signals list. */
export function boundSourceKinds(signals: ComponentSignals | null | undefined): Map<string, string> {
  return new Map(
    (signals?.signals ?? []).flatMap((s) => (s.binding ? [[s.signal, s.binding.data_source_kind] as [string, string]] : [])),
  );
}
