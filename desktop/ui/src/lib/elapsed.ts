export const ELAPSED_FINE_SPAN_MS = 60_000;
export const ELAPSED_FINE_TICK_MS = 1000;
export const ELAPSED_COARSE_TICK_MS = 30_000;

/**
 * Seconds matter only in a run's first minute. After that a whole-minute
 * figure is all anyone reads, and ticking every second re-rendered for
 * nothing for as long as the run lasted.
 */
export function elapsedTickMs(elapsedMs: number): number {
  return elapsedMs < ELAPSED_FINE_SPAN_MS ? ELAPSED_FINE_TICK_MS : ELAPSED_COARSE_TICK_MS;
}

/** "0:42" in the first minute, then "12m", then "1h 5m" — never finer than elapsedTickMs updates it. */
export function formatLiveElapsed(ms: number): string {
  const total = Math.max(0, Math.floor(ms / 1000));
  if (total * 1000 < ELAPSED_FINE_SPAN_MS) return `0:${String(total).padStart(2, "0")}`;
  const minutes = Math.floor(total / 60);
  if (minutes < 60) return `${minutes}m`;
  return `${Math.floor(minutes / 60)}h ${minutes % 60}m`;
}
