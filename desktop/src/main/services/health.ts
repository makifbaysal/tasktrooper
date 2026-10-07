/**
 * Waiting for the backend to be ready, which is one HTTP call repeated.
 *
 * `GET /health` answers 200 once the database is migrated, even when no LLM
 * provider is configured — so "200" is the whole readiness contract and this
 * file deliberately does not look at the body. A body-shaped gate would make
 * the desktop app's start depend on the backend's idea of "degraded", which is
 * a fact for the UI to show and not one for the window to wait on.
 */

/** One probe's budget. A loopback server answers in milliseconds or it is busy. */
const PROBE_TIMEOUT_MS = 3_000;

/**
 * How long between attempts. Very short at first: the backend prints
 * LISTENING once it has bound, so /health usually answers within a few
 * milliseconds of the wait starting, and a 400 ms sleep was most of the gap
 * between "bound" and "window on screen". Past the fast window it backs off,
 * so a backend that is slow to become healthy is not asked twenty times a
 * second for two minutes.
 */
export const FAST_POLL_MS = 50;
export const FAST_WINDOW_MS = 3_000;
export const SLOW_POLL_MS = 400;

/** The pause before the next attempt, `elapsedMs` into the wait. */
export function healthPollDelay(elapsedMs: number): number {
  return elapsedMs < FAST_WINDOW_MS ? FAST_POLL_MS : SLOW_POLL_MS;
}

export type HealthWaitResult =
  | { ok: true }
  | { ok: false; reason: "timeout" | "exited" | "aborted" };

export interface HealthWaitOptions {
  timeoutMs: number;
  /** False once the child has exited — there is nothing left to wait for. */
  alive: () => boolean;
  signal?: AbortSignal;
}

/** True when `GET <url>` answered with any 2xx inside the probe budget. */
export async function probeHealth(url: string): Promise<boolean> {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), PROBE_TIMEOUT_MS);
  try {
    const res = await fetch(url, { method: "GET", signal: controller.signal });
    return res.status >= 200 && res.status < 300;
  } catch {
    // Connection refused while it is still binding, and a timeout while it is
    // still migrating, are both "not yet" rather than failures to report.
    return false;
  } finally {
    clearTimeout(timer);
  }
}

export async function waitForHealth(url: string, opts: HealthWaitOptions): Promise<HealthWaitResult> {
  const started = Date.now();
  const deadline = started + opts.timeoutMs;
  for (;;) {
    if (opts.signal?.aborted) return { ok: false, reason: "aborted" };
    if (!opts.alive()) return { ok: false, reason: "exited" };
    if (await probeHealth(url)) return { ok: true };
    if (Date.now() >= deadline) return { ok: false, reason: "timeout" };
    await new Promise((resolve) => setTimeout(resolve, healthPollDelay(Date.now() - started)));
  }
}
