import type { ComponentSignals, MetricPoint, MetricSeries, SignalSeries } from "@/api";

export const QUEUE_SIGNAL_PREFIX = "queue.";

export const QUEUE_SIGNALS = {
  backlog: "queue.backlog",
  backlogAge: "queue.backlog_age",
  consumeRate: "queue.consume_rate",
  publishRate: "queue.publish_rate",
  deadLetter: "queue.dead_letter",
} as const;

/** What the Queues page reads from the series route, in one request. */
export const QUEUE_SERIES_SIGNALS: string[] = [
  QUEUE_SIGNALS.backlog,
  QUEUE_SIGNALS.backlogAge,
  QUEUE_SIGNALS.consumeRate,
  QUEUE_SIGNALS.publishRate,
  QUEUE_SIGNALS.deadLetter,
];

export type Combine = "sum" | "max";

// The oldest message is the worst queue's; every other queue measure (a
// backlog, a rate, dead letters) adds up across queues and partitions.
export function combineFor(signal: string): Combine {
  return signal === QUEUE_SIGNALS.backlogAge ? "max" : "sum";
}

/** Many series as one line: the values at one instant summed (or the
 * largest). An instant where every series is null stays a gap. */
export function combinePoints(series: MetricSeries[] | undefined, combine: Combine): MetricPoint[] {
  const list = series ?? [];
  if (list.length === 1) return list[0].points ?? [];
  const at = new Map<number, number | null>();
  for (const s of list) {
    for (const [x, y] of s.points ?? []) {
      if (!Number.isFinite(x)) continue;
      const prev = at.get(x);
      if (y === null || !Number.isFinite(y)) {
        if (prev === undefined) at.set(x, null);
        continue;
      }
      if (prev === undefined || prev === null) at.set(x, y);
      else at.set(x, combine === "sum" ? prev + y : Math.max(prev, y));
    }
  }
  return [...at.entries()].sort((a, b) => a[0] - b[0]);
}

export function lastValue(points: MetricPoint[] | undefined): { value: number; at: number } | undefined {
  const list = points ?? [];
  for (let i = list.length - 1; i >= 0; i -= 1) {
    const [x, y] = list[i];
    if (y !== null && Number.isFinite(y) && Number.isFinite(x)) return { value: y, at: x };
  }
  return undefined;
}

/** The signal's source reads only the value as it is now (a direct broker
 * kind), as the server flags it: bound, read, `live`. */
export function isLiveSignal(sig: SignalSeries | undefined): boolean {
  return Boolean(sig && sig.bound && !sig.error && sig.live === true);
}

export interface QueueSignalValue {
  value?: number;
  at?: number;
  previous?: number;
  /** The window as one line; a sparkline only when it has a trend. */
  points: MetricPoint[];
  live: boolean;
}

export function queueSignalValue(sig: SignalSeries | undefined, combine: Combine): QueueSignalValue {
  const points = combinePoints(sig?.series, combine);
  const last = lastValue(points);
  const previous = lastValue(combinePoints(sig?.previous, combine));
  return { value: last?.value, at: last?.at, previous: previous?.value, points, live: isLiveSignal(sig) };
}

export type BacklogTrend = "growing" | "draining" | "steady";

// Rates within 2 % of each other (or 0.01 msg/s) are read as balanced: a
// scrape's jitter is not a trend.
const STEADY_SHARE = 0.02;
const STEADY_FLOOR = 0.01;

/** Whether the backlog grows or drains, from what is published and consumed per second. */
export function backlogTrend(consume: number | undefined, publish: number | undefined): BacklogTrend | undefined {
  if (consume === undefined || publish === undefined) return undefined;
  const net = publish - consume;
  const steady = Math.max(STEADY_FLOOR, STEADY_SHARE * Math.max(Math.abs(consume), Math.abs(publish)));
  if (Math.abs(net) <= steady) return "steady";
  return net > 0 ? "growing" : "draining";
}

/** Whether any `queue.*` signal has a confirmed binding; undefined until the
 * signals are read. */
export function hasQueueBinding(signals: ComponentSignals | null | undefined): boolean | undefined {
  if (!signals) return undefined;
  return (signals.signals ?? []).some((s) => s.signal.startsWith(QUEUE_SIGNAL_PREFIX) && s.binding !== null && s.binding !== undefined);
}

/** The suggestions waiting for a `queue.*` signal. */
export function queueSuggestionCount(signals: ComponentSignals | null | undefined): number {
  return (signals?.signals ?? [])
    .filter((s) => s.signal.startsWith(QUEUE_SIGNAL_PREFIX))
    .reduce((n, s) => n + (s.suggestions ?? []).length, 0);
}

/** The signal whose binding a queue table was read through: the table's
 * own `signal`, else (an older answer) the binding's by id, else the first
 * bound `queue.*` signal on the same source. */
export function tableSignal(
  signals: ComponentSignals | null | undefined,
  table: { signal_binding_id: string; data_source_id: string; signal?: string },
): string | undefined {
  if (table.signal) return table.signal;
  const list = (signals?.signals ?? []).filter((s) => s.signal.startsWith(QUEUE_SIGNAL_PREFIX));
  return (
    list.find((s) => s.binding?.id === table.signal_binding_id)?.signal ??
    list.find((s) => s.binding?.data_source_id === table.data_source_id)?.signal
  );
}
