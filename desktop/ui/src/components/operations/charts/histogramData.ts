export interface HistogramBucket {
  from: number;
  to: number;
  /** One count per series, in the series' order. */
  counts: number[];
}

const SECOND = 1_000;
const MINUTE = 60 * SECOND;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;

const STEPS = [
  SECOND,
  5 * SECOND,
  10 * SECOND,
  15 * SECOND,
  30 * SECOND,
  MINUTE,
  2 * MINUTE,
  5 * MINUTE,
  10 * MINUTE,
  15 * MINUTE,
  30 * MINUTE,
  HOUR,
  2 * HOUR,
  3 * HOUR,
  6 * HOUR,
  12 * HOUR,
  DAY,
];

/** The smallest round bucket width that cuts `spanMs` into at most `target` bars. */
export function histogramStep(spanMs: number, target = 60): number {
  if (!Number.isFinite(spanMs) || spanMs <= 0) return SECOND;
  const wanted = spanMs / Math.max(1, target);
  return STEPS.find((s) => s >= wanted) ?? Math.ceil(wanted / DAY) * DAY;
}

/** Counts `items` into round-width buckets over [from, to], one count per
 * series. Bucket edges sit on multiples of the step so labels read as round
 * times; an item outside the window is not counted. */
export function bucketize<T>(
  items: readonly T[],
  at: (item: T) => number,
  seriesOf: (item: T) => number,
  seriesCount: number,
  from: number,
  to: number,
  step = histogramStep(to - from),
): HistogramBucket[] {
  if (!Number.isFinite(from) || !Number.isFinite(to) || to <= from || seriesCount <= 0) return [];
  const start = Math.floor(from / step) * step;
  const count = Math.max(1, Math.ceil((to - start) / step));
  const buckets: HistogramBucket[] = Array.from({ length: count }, (_, i) => ({
    from: start + i * step,
    to: start + (i + 1) * step,
    counts: new Array<number>(seriesCount).fill(0),
  }));
  for (const item of items) {
    const t = at(item);
    if (!Number.isFinite(t) || t < from || t > to) continue;
    const series = seriesOf(item);
    if (series < 0 || series >= seriesCount) continue;
    const i = Math.min(count - 1, Math.floor((t - start) / step));
    buckets[i].counts[series] += 1;
  }
  return buckets;
}

export function bucketTotal(bucket: HistogramBucket): number {
  return bucket.counts.reduce((a, b) => a + b, 0);
}

/** A round axis top at or above `max`: 1, 2 or 5 times a power of ten. */
export function niceCeiling(max: number): number {
  if (!Number.isFinite(max) || max <= 0) return 1;
  const power = 10 ** Math.floor(Math.log10(max));
  for (const m of [1, 2, 5, 10]) if (m * power >= max) return m * power;
  return 10 * power;
}
