import { OPS_TRACES_MAX_WINDOW_MS, type Trace, type TraceSpan } from "@/api";

// Chart slots 1–7 tell services apart; slot 8 is red, which a span bar keeps
// for "this span failed".
export const SERVICE_SLOTS = 7;

// The thinnest a bar is drawn, as a share of the trace, so a span of a few
// microseconds in a trace of seconds is still something to point at.
export const MIN_BAR_WIDTH = 0.004;

export interface WaterfallRow {
  span: TraceSpan;
  depth: number;
  /** From the trace's first span start, in ms. */
  offsetMs: number;
  durationMs: number;
  /** Where the bar starts and how wide it is, as shares of the trace (0..1). */
  left: number;
  width: number;
  /** The service's chart slot, 1..SERVICE_SLOTS. */
  colorIndex: number;
  error: boolean;
  childCount: number;
  /** Ids of every span above this one, nearest last. */
  ancestors: string[];
}

export interface WaterfallService {
  name: string;
  colorIndex: number;
  spans: number;
  errors: number;
}

export interface Waterfall {
  rows: WaterfallRow[];
  /** Unix ms of the first span's start. */
  startAt: number;
  durationMs: number;
  services: WaterfallService[];
  errors: number;
}

const FRACTION = /T[^.]*\.(\d+)/;

/** A span start as unix ms with the sub-millisecond part kept: Date.parse
 * stops at milliseconds, and spans of one request are often microseconds apart. */
export function spanStartMs(start: string): number {
  const ms = Date.parse(start);
  if (!Number.isFinite(ms)) return Number.NaN;
  const m = FRACTION.exec(start);
  if (!m) return ms;
  const micro = Number(m[1].padEnd(9, "0").slice(3, 9));
  return ms + micro / 1e6;
}

function durationOf(span: TraceSpan): number {
  const d = Number(span.duration_ms);
  return Number.isFinite(d) && d > 0 ? d : 0;
}

export function isErrorSpan(span: TraceSpan): boolean {
  return span.status === "error";
}

/** The trace as rows in tree order — each span under its parent, siblings by
 * start — with each bar placed on the trace's time axis. A span whose parent
 * the trace does not hold starts a tree of its own, as a root does. */
export function buildWaterfall(trace: Pick<Trace, "spans">): Waterfall {
  const spans = (trace.spans ?? []).filter((s) => s && s.id);
  const starts = new Map<string, number>();
  for (const s of spans) starts.set(s.id, spanStartMs(s.start));
  const valid = spans.filter((s) => Number.isFinite(starts.get(s.id)));
  if (valid.length === 0) return { rows: [], startAt: 0, durationMs: 0, services: [], errors: 0 };

  const startAt = Math.min(...valid.map((s) => starts.get(s.id)!));
  const endAt = Math.max(...valid.map((s) => starts.get(s.id)! + durationOf(s)));
  const total = Math.max(endAt - startAt, ...valid.map(durationOf), 0.001);

  const ids = new Set(valid.map((s) => s.id));
  const children = new Map<string, TraceSpan[]>();
  const roots: TraceSpan[] = [];
  for (const s of valid) {
    const parent = s.parent_id && s.parent_id !== s.id && ids.has(s.parent_id) ? s.parent_id : undefined;
    if (parent) children.set(parent, [...(children.get(parent) ?? []), s]);
    else roots.push(s);
  }
  const order = new Map(valid.map((s, i) => [s.id, i]));
  const byStart = (a: TraceSpan, b: TraceSpan) => starts.get(a.id)! - starts.get(b.id)! || order.get(a.id)! - order.get(b.id)!;

  const services = new Map<string, WaterfallService>();
  const serviceOf = (name: string): WaterfallService => {
    let found = services.get(name);
    if (!found) {
      found = { name, colorIndex: (services.size % SERVICE_SLOTS) + 1, spans: 0, errors: 0 };
      services.set(name, found);
    }
    return found;
  };

  const rows: WaterfallRow[] = [];
  const seen = new Set<string>();
  const visit = (span: TraceSpan, depth: number, ancestors: string[]) => {
    if (seen.has(span.id)) return;
    seen.add(span.id);
    const kids = [...(children.get(span.id) ?? [])].sort(byStart);
    const offsetMs = starts.get(span.id)! - startAt;
    const durationMs = durationOf(span);
    const width = Math.min(1, Math.max(MIN_BAR_WIDTH, durationMs / total));
    const left = Math.min(1 - width, Math.max(0, offsetMs / total));
    const service = serviceOf(span.service || "");
    const error = isErrorSpan(span);
    service.spans += 1;
    if (error) service.errors += 1;
    rows.push({
      span,
      depth,
      offsetMs,
      durationMs,
      left,
      width,
      colorIndex: service.colorIndex,
      error,
      childCount: kids.length,
      ancestors,
    });
    for (const kid of kids) visit(kid, depth + 1, [...ancestors, span.id]);
  };
  for (const root of [...roots].sort(byStart)) visit(root, 0, []);
  // A parent cycle leaves spans no root reaches: list them as roots.
  for (const s of [...valid].sort(byStart)) if (!seen.has(s.id)) visit(s, 0, []);

  return {
    rows,
    startAt,
    durationMs: total,
    services: [...services.values()],
    errors: rows.filter((r) => r.error).length,
  };
}

/** The rows a reader sees with some spans folded: every row under a folded one is hidden. */
export function visibleRows(rows: WaterfallRow[], collapsed: ReadonlySet<string>): WaterfallRow[] {
  if (collapsed.size === 0) return rows;
  return rows.filter((r) => !r.ancestors.some((id) => collapsed.has(id)));
}

export interface WaterfallTick {
  ms: number;
  /** Share of the trace, 0..1. */
  at: number;
}

function niceStep(raw: number): number {
  if (!(raw > 0)) return 1;
  const power = 10 ** Math.floor(Math.log10(raw));
  for (const m of [1, 2, 5, 10]) if (m * power >= raw) return m * power;
  return 10 * power;
}

/** Round time marks across the trace (1-2-5 steps), the first at 0. */
export function waterfallTicks(durationMs: number, target = 5): WaterfallTick[] {
  if (!(durationMs > 0)) return [{ ms: 0, at: 0 }];
  const step = niceStep(durationMs / Math.max(1, target));
  const ticks: WaterfallTick[] = [];
  for (let ms = 0; ms <= durationMs + step * 1e-9; ms += step) ticks.push({ ms, at: Math.min(1, ms / durationMs) });
  return ticks;
}

/** A span or trace duration as people read one: µs under a millisecond, ms
 * under a second, then seconds, then minutes. */
export function formatSpanDuration(ms: number, locale: string): string {
  if (!Number.isFinite(ms)) return "—";
  const num = (v: number, digits: number) => new Intl.NumberFormat(locale, { maximumFractionDigits: digits }).format(v);
  const abs = Math.abs(ms);
  if (abs === 0) return `${num(0, 0)} ms`;
  if (abs < 1) return `${num(ms * 1000, abs < 0.01 ? 2 : 0)} µs`;
  if (abs < 1000) return `${num(ms, abs < 10 ? 2 : abs < 100 ? 1 : 0)} ms`;
  if (abs < 60_000) return `${num(ms / 1000, 2)} s`;
  return `${num(ms / 60_000, 1)} min`;
}

export interface SpanAttributeGroups {
  span: Array<[string, string]>;
  resource: Array<[string, string]>;
}

/** A span's attributes split into its own and its resource's (the server
 * prefixes those `resource.`), each sorted by key. */
export function spanAttributes(span: Pick<TraceSpan, "attributes">): SpanAttributeGroups {
  const own: Array<[string, string]> = [];
  const resource: Array<[string, string]> = [];
  for (const [k, v] of Object.entries(span.attributes ?? {})) {
    if (k.startsWith("resource.")) resource.push([k.slice("resource.".length), v]);
    else own.push([k, v]);
  }
  const byKey = (a: [string, string], b: [string, string]) => a[0].localeCompare(b[0]);
  return { span: own.sort(byKey), resource: resource.sort(byKey) };
}

/** The search window: the console's range, cut to the last 7 days of it
 * (Tempo's own search ceiling, which the server holds the route to). */
export function traceWindow(range: { from: number; to: number }): { since: number; until: number; clamped: boolean } {
  const until = range.to;
  const since = Math.max(range.from, until - OPS_TRACES_MAX_WINDOW_MS);
  return { since, until, clamped: since > range.from };
}

/** A minimum duration typed in ms: a whole, non-negative number, or nothing. */
export function parseMinDuration(raw: string | null | undefined): number | undefined {
  const text = (raw ?? "").trim();
  if (!/^\d+(\.\d+)?$/.test(text)) return undefined;
  const n = Math.round(Number(text));
  return Number.isFinite(n) && n > 0 ? n : undefined;
}

/** A trace id as the server takes it: 1–32 hex digits, lowercase. */
export function normalizeTraceId(raw: string | null | undefined): string | undefined {
  const id = (raw ?? "").trim();
  return /^[0-9a-fA-F]{1,32}$/.test(id) ? id.toLowerCase() : undefined;
}
