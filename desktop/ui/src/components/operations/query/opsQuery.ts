import { OPS_ERRORS_MAX_WINDOW_MS, type OpsLogEntry } from "@/api";
import { OPS_AUTO_ADVANCE_MS, type OpsTimeRange } from "@/components/operations/shell/OpsContext";
import type { OpsEntity } from "@/components/operations/shell/useOpsEntities";

export interface ErrorsWindow {
  since: number;
  /** The range reached further back than the server reads (30 days). */
  clamped: boolean;
  /** A custom range that ends in the past: errors have no end time, so the
   * inbox still counts up to now. */
  openEnded: boolean;
}

export function errorsWindow(range: Pick<OpsTimeRange, "from" | "to">, now: number): ErrorsWindow {
  const earliest = now - OPS_ERRORS_MAX_WINDOW_MS;
  const clamped = range.from < earliest;
  return {
    since: clamped ? earliest : range.from,
    clamped,
    openEnded: range.to < now - OPS_AUTO_ADVANCE_MS,
  };
}

/** A query's identity for caching: a preset window slides with the clock, so
 * only its name is part of it; a custom window is its bounds. */
export function windowKey(range: Pick<OpsTimeRange, "preset" | "from" | "to">): string {
  return range.preset === "custom" ? `custom:${range.from}-${range.to}` : range.preset;
}

export function parseIdList(value: string | null): string[] {
  if (!value) return [];
  return [...new Set(value.split(",").map((s) => s.trim()).filter(Boolean))];
}

export function formatIdList(ids: string[]): string | null {
  const unique = [...new Set(ids.filter(Boolean))];
  return unique.length > 0 ? unique.join(",") : null;
}

export interface EnvironmentRef {
  entity?: OpsEntity;
  resource?: string;
}

/** Who an environment id belongs to, for the rows and notices that only
 * carry ids. */
export function environmentIndex(entities: OpsEntity[]): Map<string, EnvironmentRef> {
  const index = new Map<string, EnvironmentRef>();
  for (const entity of entities) {
    for (const env of entity.environments) {
      index.set(env.id, { entity, resource: env.resource_name || env.url || undefined });
    }
  }
  return index;
}

export function entityIndex(entities: OpsEntity[]): Map<string, OpsEntity> {
  return new Map(entities.map((e) => [e.id, e]));
}

const FRACTION = /\.(\d+)/;

// Date.parse keeps milliseconds only; providers stamp nanoseconds, and two
// entries a microsecond apart must still sort and compare as different.
function subMillis(ts: string): number {
  const m = FRACTION.exec(ts);
  if (!m) return 0;
  return Number(m[1].padEnd(9, "0").slice(3, 9));
}

function millis(ts: string): number {
  const ms = Date.parse(ts);
  return Number.isFinite(ms) ? ms : 0;
}

export function compareTimestamps(a: string, b: string): number {
  const d = millis(a) - millis(b);
  return d !== 0 ? d : subMillis(a) - subMillis(b);
}

export function logIdentity(e: OpsLogEntry): string {
  const fields = e.fields
    ? Object.keys(e.fields)
        .sort()
        .map((k) => `${k}=${e.fields?.[k]}`)
        .join("\u0002")
    : "";
  return [
    e.environment_id,
    e.timestamp,
    e.severity,
    e.message,
    e.source ?? "",
    e.method ?? "",
    e.path ?? "",
    e.status_code ?? "",
    e.trace_id ?? "",
    fields,
  ].join("\u0001");
}

export function oldestTimestamp(entries: OpsLogEntry[]): string | undefined {
  let oldest: string | undefined;
  for (const e of entries) if (oldest === undefined || compareTimestamps(e.timestamp, oldest) < 0) oldest = e.timestamp;
  return oldest;
}

export function newestTimestamp(entries: OpsLogEntry[]): string | undefined {
  let newest: string | undefined;
  for (const e of entries) if (newest === undefined || compareTimestamps(e.timestamp, newest) > 0) newest = e.timestamp;
  return newest;
}

// A "load older" query (`until` = the oldest shown) is inclusive, so every
// entry at exactly that instant comes back again. They
// are dropped as a multiset: two identical lines at one instant both stay, but
// neither shows twice.
function dropRepeats(shown: OpsLogEntry[], fetched: OpsLogEntry[], boundary: string | undefined): OpsLogEntry[] {
  if (boundary === undefined) return fetched;
  const seen = new Map<string, number>();
  for (const e of shown) {
    if (compareTimestamps(e.timestamp, boundary) !== 0) continue;
    const id = logIdentity(e);
    seen.set(id, (seen.get(id) ?? 0) + 1);
  }
  if (seen.size === 0) return fetched;
  return fetched.filter((e) => {
    if (compareTimestamps(e.timestamp, boundary) !== 0) return true;
    const id = logIdentity(e);
    const left = seen.get(id) ?? 0;
    if (left === 0) return true;
    seen.set(id, left - 1);
    return false;
  });
}

export interface MergedLogs {
  entries: OpsLogEntry[];
  added: number;
}

export function appendOlder(shown: OpsLogEntry[], older: OpsLogEntry[]): MergedLogs {
  const fresh = dropRepeats(shown, older, oldestTimestamp(shown));
  return { entries: fresh.length > 0 ? [...shown, ...fresh] : shown, added: fresh.length };
}

/** A live-tail read from `since` (an overlap before the newest shown, so an
 * entry a provider ingested late is not skipped): every entry already shown
 * from `since` on is dropped as a multiset, and what is new goes in by time,
 * newest first — a late one below newer lines already shown. */
export function mergeNewer(
  shown: OpsLogEntry[],
  newer: OpsLogEntry[],
  since: string,
  cap = Infinity,
): MergedLogs & { trimmed: boolean } {
  const seen = new Map<string, number>();
  for (const e of shown) {
    if (compareTimestamps(e.timestamp, since) < 0) continue;
    const id = logIdentity(e);
    seen.set(id, (seen.get(id) ?? 0) + 1);
  }
  const fresh = newer.filter((e) => {
    const id = logIdentity(e);
    const left = seen.get(id) ?? 0;
    if (left === 0) return true;
    seen.set(id, left - 1);
    return false;
  });
  if (fresh.length === 0) return { entries: shown, added: 0, trimmed: false };
  const merged = [...fresh, ...shown].sort((a, b) => compareTimestamps(b.timestamp, a.timestamp));
  if (merged.length <= cap) return { entries: merged, added: fresh.length, trimmed: false };
  return { entries: merged.slice(0, cap), added: fresh.length, trimmed: true };
}

/** Where a live-tail read starts: `overlapMs` before the newest shown, and
 * never at or after `now - marginMs` — the server refuses a `since` that is
 * not before its own now. */
export function tailSince(newest: string | undefined, fallback: number, now: number, overlapMs: number, marginMs: number): string {
  const from = newest === undefined ? fallback : Date.parse(newest) - overlapMs;
  const at = Number.isFinite(from) ? from : fallback;
  return new Date(Math.min(at, now - marginMs)).toISOString();
}
