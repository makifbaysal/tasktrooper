import type { MetricPoint, MetricSeries, SignalSeries } from "@/api";
import { headlineSeries } from "@/components/operations/charts/signalChart";

export const CWV_SIGNALS = ["web.lcp", "web.inp", "web.cls"] as const;
export const TRAFFIC_SIGNALS = ["web.page_views", "web.sessions", "web.visitors"] as const;

export type CwvSignal = (typeof CWV_SIGNALS)[number];

export type CwvBand = "good" | "needsImprovement" | "poor";

/** Google's Core Web Vitals thresholds, judged at the 75th percentile of
 * page loads, in the signal's own unit (ms; CLS is a unitless score):
 * at most `good` is good, above `poor` is poor, between needs improvement. */
export const CWV_THRESHOLDS: Record<CwvSignal, { good: number; poor: number }> = {
  "web.lcp": { good: 2500, poor: 4000 },
  "web.inp": { good: 200, poor: 500 },
  "web.cls": { good: 0.1, poor: 0.25 },
};

export function isCwvSignal(signal: string): signal is CwvSignal {
  return (CWV_SIGNALS as readonly string[]).includes(signal);
}

export function cwvBand(signal: string, value: number | null | undefined): CwvBand | undefined {
  if (!isCwvSignal(signal) || value == null || !Number.isFinite(value)) return undefined;
  const { good, poor } = CWV_THRESHOLDS[signal];
  if (value <= good) return "good";
  if (value > poor) return "poor";
  return "needsImprovement";
}

/** Where a value sits on the band meter, 0..1: the meter runs from 0 to half
 * again past the poor threshold, the good and needs-improvement segments
 * ending at their thresholds. */
export function bandPosition(signal: CwvSignal, value: number): number {
  const end = CWV_THRESHOLDS[signal].poor * 1.5;
  return Math.min(1, Math.max(0, value / end));
}

export function bandSegments(signal: CwvSignal): { good: number; needsImprovement: number; poor: number } {
  const { good, poor } = CWV_THRESHOLDS[signal];
  const end = poor * 1.5;
  return { good: good / end, needsImprovement: (poor - good) / end, poor: (end - poor) / end };
}

export interface LastValue {
  value: number;
  at: number;
}

/** The newest point with a value. */
export function lastValue(points: MetricPoint[] | undefined): LastValue | undefined {
  for (let i = (points?.length ?? 0) - 1; i >= 0; i--) {
    const [at, value] = points![i];
    if (value != null && Number.isFinite(value)) return { value, at };
  }
  return undefined;
}

export interface VitalRead {
  points: MetricPoint[];
  last?: LastValue;
  /** The percentile the value is, when the answer says: `p75` judges as
   * Google does; `p95` (a histogram read at p50/p95/p99) is stricter. */
  quantile?: string;
}

function quantileName(series: MetricSeries): string | undefined {
  const head = (series.name ?? "").split(" ")[0];
  return /^p\d{2}$/.test(head) ? head : undefined;
}

/** A Core Web Vital's tile value: the p75 series when the answer has one; a
 * single series of a source that reports its own percentile (the web
 * analytics recipes answer p75); else the headline p95 of a histogram read. */
export function vitalRead(signal: SignalSeries | undefined): VitalRead {
  const series = signal?.series ?? [];
  if (series.length === 0) return { points: [] };
  const p75 = series.find((s) => quantileName(s) === "p75");
  const chosen = p75 ?? (series.length === 1 && !quantileName(series[0]) ? series[0] : headlineSeries(series));
  const points = chosen?.points ?? [];
  return { points, last: lastValue(points), quantile: chosen ? quantileName(chosen) : undefined };
}
