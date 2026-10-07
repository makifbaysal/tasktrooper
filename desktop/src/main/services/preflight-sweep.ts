import type { Overrides, PreflightReport } from "../../ipc/types.js";
import type { PreflightRun } from "./detect.js";
import { loginShellPath, seedLoginShellPath } from "./login-env.js";
import type { PreflightCache } from "./preflight-cache.js";

/**
 * One preflight at a time, shared by everybody who asks.
 *
 * At launch the setup screen's detection and the backend's start each used to
 * run a whole sweep of their own, at the same moment — two login shells' worth
 * of PATH, two `--version` per CLI, two `claude auth status` — roughly
 * eighteen child processes for one set of answers. Now a sweep in flight is
 * joined rather than repeated, and a finished one is reused while it is
 * recent. Only `force` (the "Check again" button, an override that just
 * changed) always starts a new one.
 */

/** How long a finished sweep answers a caller that did not force a new one. */
export const PREFLIGHT_REUSE_MS = 30_000;

export interface Sweep {
  readonly id: number;
  /** The overrides it ran with: a sweep for other overrides answers a different question. */
  readonly key: string;
  readonly gating: Promise<PreflightReport>;
  readonly complete: Promise<PreflightReport>;
  /** The complete report, once it has arrived. */
  readonly result: PreflightReport | null;
  /** When the complete half settled; null while it is still running. */
  readonly finishedAt: number | null;
}

export interface SweepRequest {
  overrides: Overrides;
  /** Start a new sweep whatever is running or recent. */
  force?: boolean;
  /** How old a finished sweep may be and still be the answer. `PREFLIGHT_REUSE_MS` by default. */
  maxAgeMs?: number;
}

export interface PreflightSweepsDeps {
  start(opts: { overrides: Overrides; force: boolean }): PreflightRun;
  /** Called once for every sweep actually started, before anybody awaits it. */
  onSweep?: (sweep: Sweep) => void;
  now?: () => number;
}

type MutableSweep = { -readonly [K in keyof Sweep]: Sweep[K] };

export function overridesKey(overrides: Overrides): string {
  const set = Object.entries(overrides).filter(([, value]) => value !== undefined && value !== "");
  set.sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0));
  return JSON.stringify(set);
}

export class PreflightSweeps {
  readonly #deps: PreflightSweepsDeps;
  readonly #now: () => number;
  #current: MutableSweep | null = null;
  #nextId = 1;

  constructor(deps: PreflightSweepsDeps) {
    this.#deps = deps;
    this.#now = deps.now ?? Date.now;
  }

  /** The newest sweep started, finished or not. */
  get latest(): Sweep | null {
    return this.#current;
  }

  sweep(request: SweepRequest): Sweep {
    const key = overridesKey(request.overrides);
    const current = this.#current;
    if (!request.force && current && current.key === key) {
      if (current.finishedAt === null) return current;
      if (current.result && this.#now() - current.finishedAt < (request.maxAgeMs ?? PREFLIGHT_REUSE_MS)) return current;
    }

    const run = this.#deps.start({ overrides: request.overrides, force: request.force === true });
    const sweep: MutableSweep = {
      id: this.#nextId++,
      key,
      gating: run.gating,
      complete: run.complete,
      result: null,
      finishedAt: null,
    };
    this.#current = sweep;
    run.complete.then(
      (report) => {
        sweep.result = report;
        sweep.finishedAt = this.#now();
      },
      () => {
        sweep.finishedAt = this.#now();
        // A failed sweep is never the answer to the next caller.
        if (this.#current === sweep) this.#current = null;
      },
    );
    this.#deps.onSweep?.(sweep);
    return sweep;
  }
}

/**
 * Put the login PATH an earlier launch recorded to use at once, and start this
 * launch's own login shell to replace it. When the fresh answer differs it is
 * recorded for next time — and the sweep that ran on the old one locates
 * everything again in its complete half (see `startPreflight`).
 */
export function primeLoginShellPath(cache: PreflightCache): Promise<string[]> {
  const remembered = cache.loginPath;
  if (remembered && remembered.length > 0) seedLoginShellPath(remembered);
  const fresh = loginShellPath();
  // An empty answer is a login shell that failed or timed out, not a PATH.
  // Recording it would replace a good one, and the next launch would seed
  // nothing and still count its login PATH as known.
  void fresh.then((entries) => {
    if (entries.length > 0) cache.setLoginPath(entries);
  });
  return fresh;
}
