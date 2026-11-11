import type { LogSeverity, OpsLogEntry, Trace, TraceSpan } from "@/api";
import type { OpsStatus } from "@/components/operations/console/opsStatus";

/** The telemetry product's read API, as the app server's proxy returns it
 * (paths below the product's `/api/v1`). Only the fields the screens use. */

export type TelemetryHealth = "ok" | "warning" | "critical";

export interface TelemetryService {
  id: string;
  name: string;
  type: string;
  language: string;
  version: string;
  rpm: number;
  errorRate: number;
  p95: number;
  prevRpm: number;
  prevErrorRate: number;
  prevP95: number;
  status: TelemetryHealth;
  instances: number;
  lag?: number;
  lastSeen?: number;
  spark?: number[];
}

export interface TelemetryEndpoint {
  id: string;
  serviceId: string;
  method: string;
  route: string;
  rpm: number;
  errorRate: number;
  p50: number;
  p95: number;
  p99: number;
  monitored: boolean;
  criticality: string;
  sub?: { process: string; source: string; topic: string; lag: number } | null;
}

export interface TelemetryDependency {
  id: string;
  serviceId: string;
  name: string;
  kind: string;
  avgMs: number;
  p95: number;
  errorRate: number;
  callsPerMin: number;
}

export interface TelemetrySeriesSet {
  timestamps: number[] | null;
  series: Array<{ id: string; label: string; data: number[] | null }> | null;
}

export interface TelemetryTraceSummary {
  id: string;
  ts: number;
  dur: number;
  status: number;
  service: string;
  method: string;
  route: string;
  isError: boolean;
}

export interface TelemetrySpan {
  spanId: string;
  parentSpanId?: string;
  service: string;
  name: string;
  kind: string;
  start: number;
  dur: number;
  status: number;
  failed: boolean;
  errorType?: string;
  attrs?: Record<string, string> | null;
  resource?: Record<string, string> | null;
}

export interface TelemetryTrace {
  id: string;
  ts: number;
  dur: number;
  services: string[] | null;
  errors: number;
  spans: TelemetrySpan[] | null;
}

export interface TelemetryLogLine {
  id: string;
  ts: number;
  level: string;
  service: string;
  message: string;
  traceId?: string | null;
  route?: string | null;
  source?: string | null;
}

export interface TelemetryLogPage {
  lines: TelemetryLogLine[] | null;
  nextBefore: number;
  levels: Record<string, number> | null;
  services: Record<string, number> | null;
}

export interface TelemetryErrorIssue {
  fingerprint: string;
  service: string;
  type: string;
  message: string;
  route: string;
  firstSeen: number;
  lastSeen: number;
  count: number;
  sampleTraceId?: string;
  status: string;
}

export interface TelemetryErrorFrame {
  fn: string;
  file: string;
  line: number;
  col: number;
  inApp: boolean;
}

export interface TelemetryErrorDetail extends TelemetryErrorIssue {
  release: string;
  platform: string;
  stack: string;
  frames: TelemetryErrorFrame[] | null;
  symbolicated: boolean;
}

export interface TelemetryMonitor {
  id: string;
  name: string;
  kind: string;
  target: string;
  condition: string;
  severity: string;
  state: string;
  source: string;
  lastChange: number;
  channels: string[] | null;
  status: string;
  lastValue?: number | null;
  mutedUntil?: number;
}

export interface TelemetryIncident {
  id: string;
  monitorId: string;
  kpiId?: string;
  title: string;
  severity: string;
  state: string;
  startedAt: number;
  value: string;
  service: string;
  endpoint?: string;
  ackedAt?: number;
  resolvedAt?: number;
  peakValue?: number | null;
}

export interface TelemetryIncidentEvent {
  id: string;
  ts: number;
  kind: string;
  data: unknown;
}

export interface TelemetryDeploy {
  service: string;
  env: string;
  version: string;
  ts: number;
  source: string;
  url?: string;
  commitMessage?: string;
  author?: string;
  state?: string;
}

export interface TelemetryKpi {
  id: string;
  name: string;
  description: string;
  owner: string;
  target: { kind: string; serviceId: string; ref: string };
  goodWhen: string;
  objective: number;
  window: string;
  current: number;
  windowGood: number;
  budgetRemaining: number;
  burnRate1h: number;
  burnRate6h: number;
  status: TelemetryHealth;
  noData?: boolean;
}

export interface TelemetryRcaStats {
  requests: number;
  rpm: number;
  avgMs: number;
  p50: number;
  p95: number;
  errorRate: number;
}

export interface TelemetryRcaChild {
  dep: string;
  kind: string;
  deltaMsPerReq: number;
  share: number;
  current: { callsPerReq?: number; msPerReq?: number; errorRate?: number };
  baseline: { callsPerReq?: number; msPerReq?: number; errorRate?: number };
}

export interface TelemetryEndpointRca {
  service: string;
  endpoint: string;
  current: TelemetryRcaStats;
  baseline: TelemetryRcaStats;
  ownMs: number[];
  deltaAvgMs: number;
  children: TelemetryRcaChild[] | null;
}

export type TelemetryRange = "15m" | "30m" | "1h" | "6h" | "24h" | "7d";

const MIN = 60_000;
const RANGE_CEILINGS: Array<[number, TelemetryRange]> = [
  [15 * MIN, "15m"],
  [30 * MIN, "30m"],
  [60 * MIN, "1h"],
  [6 * 60 * MIN, "6h"],
  [24 * 60 * MIN, "24h"],
];

/** The product answers only these windows: the console's picks round up to
 * the next one, so a "3h" view reads 6h rather than a window it did not ask. */
export function telemetryRange(range: { from: number; to: number }): TelemetryRange {
  const span = range.to - range.from;
  for (const [limit, name] of RANGE_CEILINGS) if (span <= limit) return name;
  return "7d";
}

export function telemetryHealthStatus(health: TelemetryHealth | string | undefined): OpsStatus {
  if (health === "critical") return "critical";
  if (health === "warning") return "warning";
  return "healthy";
}

/** Series timestamps arrive as unix ms; an older build sent seconds. */
function toMs(ts: number): number {
  return ts < 1e11 ? ts * 1000 : ts;
}

export interface TelemetryChartSeries {
  id: string;
  label: string;
  points: Array<[number, number | null]>;
}

export function seriesFromSet(set: TelemetrySeriesSet | undefined, ids?: string[]): TelemetryChartSeries[] {
  const stamps = set?.timestamps ?? [];
  if (!set?.series || stamps.length === 0) return [];
  return set.series
    .filter((s) => !ids || ids.includes(s.id))
    .map((s) => ({
      id: s.id,
      label: s.label,
      points: stamps.map((ts, i): [number, number | null] => {
        const v = s.data?.[i];
        return [toMs(ts), typeof v === "number" && Number.isFinite(v) ? v : null];
      }),
    }));
}

export function sparkPoints(values: number[] | undefined): Array<[number, number | null]> {
  return (values ?? []).map((v, i): [number, number | null] => [i, Number.isFinite(v) ? v : null]);
}

const LEVELS: Record<string, LogSeverity> = {
  trace: "debug",
  debug: "debug",
  info: "info",
  warn: "warning",
  warning: "warning",
  error: "error",
  fatal: "critical",
  critical: "critical",
  panic: "critical",
};

export function logSeverity(level: string | undefined): LogSeverity {
  return LEVELS[(level ?? "").toLowerCase()] ?? "info";
}

/** A log page as the console's own log table reads it; the product's lines
 * belong to no component, so the ids are empty. */
export function toLogEntries(lines: TelemetryLogLine[] | null | undefined): OpsLogEntry[] {
  return (lines ?? []).map((l) => ({
    timestamp: new Date(toMs(l.ts)).toISOString(),
    severity: logSeverity(l.level),
    message: l.message,
    source: l.service,
    path: l.route ?? undefined,
    trace_id: l.traceId ?? undefined,
    fields: l.source ? { source: l.source } : undefined,
    component_id: "",
    environment_id: "",
  }));
}

/** A trace as the console's waterfall reads it. */
export function toConsoleTrace(raw: TelemetryTrace): Trace {
  const origin = toMs(raw.ts);
  const spans: TraceSpan[] = (raw.spans ?? []).map((s) => {
    const attributes: Record<string, string> = { ...(s.attrs ?? {}) };
    for (const [k, v] of Object.entries(s.resource ?? {})) attributes[`resource.${k}`] = v;
    if (s.status) attributes["status.code"] = String(s.status);
    const parent = s.parentSpanId && /[^0]/.test(s.parentSpanId) ? s.parentSpanId : undefined;
    return {
      id: s.spanId,
      parent_id: parent,
      service: s.service,
      name: s.name,
      kind: s.kind,
      start: new Date(origin + (Number.isFinite(s.start) ? s.start : 0)).toISOString(),
      duration_ms: s.dur,
      status: s.failed ? "error" : "ok",
      status_message: s.errorType || undefined,
      attributes,
    };
  });
  return { id: raw.id, spans, truncated: false };
}

export function formatRatio(ratio: number, locale: string): string {
  if (!Number.isFinite(ratio) || ratio === 0) return "0%";
  const pct = ratio * 100;
  const digits = pct >= 10 ? 1 : pct >= 1 ? 2 : 3;
  return `${new Intl.NumberFormat(locale, { maximumFractionDigits: digits }).format(pct)}%`;
}

export function formatMs(ms: number, locale: string): string {
  if (!Number.isFinite(ms)) return "-";
  if (ms >= 1000) return `${new Intl.NumberFormat(locale, { maximumFractionDigits: ms >= 10_000 ? 0 : 2 }).format(ms / 1000)} s`;
  if (ms > 0 && ms < 1) return `${Math.round(ms * 1000)} µs`;
  return `${new Intl.NumberFormat(locale, { maximumFractionDigits: ms < 10 ? 1 : 0 }).format(ms)} ms`;
}

export function formatCount(n: number, locale: string): string {
  if (!Number.isFinite(n)) return "-";
  return new Intl.NumberFormat(locale, { notation: "compact", maximumFractionDigits: 1 }).format(n);
}

/** "METHOD /route" or the process name, the form the product's endpoint filters take. */
export function endpointLabel(e: Pick<TelemetryEndpoint, "method" | "route" | "sub">): string {
  if (e.sub?.process) return e.sub.process;
  return [e.method, e.route].filter(Boolean).join(" ");
}

export function isOpenIncidentState(state: string): boolean {
  return state !== "resolved";
}

export const INCIDENT_STATES = ["open", "firing", "pending", "acknowledged", "resolved"] as const;
export const ERROR_STATUSES = ["open", "resolved", "ignored"] as const;
export const LOG_LEVELS = ["error", "warn", "info", "debug"] as const;
export const SERVICE_TYPE_GROUPS = ["http", "worker", "web", "mobile"] as const;

/** Log volume per bucket in the order the console's histogram stacks severities
 * (critical, error, warning, info, debug). The product folds fatal into error. */
export function logVolumeBuckets(set: TelemetrySeriesSet | undefined): Array<{ from: number; to: number; counts: number[] }> {
  const stamps = set?.timestamps ?? [];
  if (!set?.series || stamps.length === 0) return [];
  const byId = new Map(set.series.map((s) => [s.id, s.data ?? []]));
  const step = stamps.length > 1 ? toMs(stamps[1]) - toMs(stamps[0]) : 60_000;
  const pick = (ids: string[], i: number) => ids.reduce((sum, id) => sum + (byId.get(id)?.[i] ?? 0), 0);
  return stamps.map((ts, i) => {
    const from = toMs(ts);
    return { from, to: from + step, counts: [0, pick(["error"], i), pick(["warn", "warning"], i), pick(["info"], i), pick(["debug"], i)] };
  });
}
