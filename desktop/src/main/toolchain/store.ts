import { mkdirSync, readFileSync, renameSync, writeFileSync } from "node:fs";
import { mkdir, writeFile } from "node:fs/promises";
import path from "node:path";
import {
  TOOLCHAIN_IDS,
  type ToolchainId,
  type ToolchainJobState,
  type ToolchainJobStep,
  type ToolchainStepState,
} from "../../ipc/types.js";
import { INTERRUPTED, isFinished } from "./job.js";
import { LOG_LINES, cleanLine } from "./log-ring.js";

/**
 * The last job, in `userData/toolchain-state.json`, so the header and
 * Settings still show what happened after a relaunch.
 *
 * A job persisted as `running` is one the app did not live to finish — it
 * quit or crashed mid-install. It comes back `done`, with every step that had
 * not finished failed as interrupted: claiming it is still running would show
 * a progress bar nothing is driving. `restartRecommended` is not kept — the
 * relaunch restarted the backend.
 *
 * The file is this app's own but still read as untrusted: anything that does
 * not have the job's shape is an idle job.
 */

const STATES: ReadonlySet<string> = new Set<ToolchainStepState>([
  "queued",
  "running",
  "waiting",
  "done",
  "failed",
  "skipped",
  "cancelled",
]);
const KINDS: ReadonlySet<string> = new Set(["auto", "interactive", "manual"]);

export const IDLE: ToolchainJobState = { phase: "idle", steps: [], percent: 0 };

const str = (v: unknown, max = 600): string | undefined => (typeof v === "string" ? v.slice(0, max) : undefined);
const num = (v: unknown): number | undefined => (typeof v === "number" && Number.isFinite(v) ? v : undefined);

function parseStep(raw: unknown): ToolchainJobStep | null {
  if (typeof raw !== "object" || raw === null) return null;
  const o = raw as Record<string, unknown>;
  if (typeof o.id !== "string" || !(TOOLCHAIN_IDS as readonly string[]).includes(o.id)) return null;
  if (typeof o.state !== "string" || !STATES.has(o.state)) return null;
  if (typeof o.installKind !== "string" || !KINDS.has(o.installKind)) return null;
  const step: ToolchainJobStep = {
    id: o.id as ToolchainId,
    label: str(o.label, 120) ?? o.id,
    state: o.state as ToolchainStepState,
    installKind: o.installKind as ToolchainJobStep["installKind"],
  };
  const startedAt = num(o.startedAt);
  const finishedAt = num(o.finishedAt);
  const percent = num(o.percent);
  const detail = str(o.detail);
  const error = str(o.error);
  if (startedAt !== undefined) step.startedAt = startedAt;
  if (finishedAt !== undefined) step.finishedAt = finishedAt;
  if (percent !== undefined) step.percent = Math.max(0, Math.min(100, percent));
  if (detail !== undefined) step.detail = detail;
  if (error !== undefined) step.error = error;
  if (Array.isArray(o.log)) {
    const log = o.log.filter((l): l is string => typeof l === "string").map(cleanLine).slice(-LOG_LINES);
    if (log.length > 0) step.log = log;
  }
  return step;
}

export function parseJobState(raw: unknown): ToolchainJobState | null {
  if (typeof raw !== "object" || raw === null) return null;
  const o = raw as Record<string, unknown>;
  if (o.phase !== "running" && o.phase !== "done") return null;
  if (!Array.isArray(o.steps)) return null;
  const steps = o.steps.map(parseStep);
  if (steps.some((s) => s === null)) return null;
  const state: ToolchainJobState = { phase: o.phase, steps: steps as ToolchainJobStep[], percent: num(o.percent) ?? 0 };
  const jobId = str(o.jobId, 64);
  const startedAt = num(o.startedAt);
  const finishedAt = num(o.finishedAt);
  if (jobId !== undefined) state.jobId = jobId;
  if (startedAt !== undefined) state.startedAt = startedAt;
  if (finishedAt !== undefined) state.finishedAt = finishedAt;
  return state;
}

/** What a persisted job means to a process that did not run it. */
export function recoverJob(state: ToolchainJobState, now: number): ToolchainJobState {
  const steps = state.steps.map((step): ToolchainJobStep => {
    if (isFinished(step.state)) return { ...step };
    const { percent: _percent, detail: _detail, ...rest } = step;
    return { ...rest, state: "failed", error: INTERRUPTED, finishedAt: now };
  });
  const { restartRecommended: _restart, ...rest } = state;
  return {
    ...rest,
    phase: "done",
    steps,
    percent: steps.length === 0 ? 0 : 100,
    ...(state.phase === "running" ? { finishedAt: now } : {}),
  };
}

export class ToolchainStateStore {
  readonly #file: string;
  #writing: Promise<void> = Promise.resolve();
  #queued: ToolchainJobState | null = null;
  /** Set by `saveNow`: a background write still in flight must not land over the final one. */
  #closed = false;

  constructor(file: string) {
    this.#file = file;
  }

  get file(): string {
    return this.#file;
  }

  load(now: number = Date.now()): ToolchainJobState {
    try {
      const parsed = parseJobState(JSON.parse(readFileSync(this.#file, "utf8")));
      return parsed ? recoverJob(parsed, now) : IDLE;
    } catch {
      return IDLE;
    }
  }

  /** Coalesced: a burst of saves is one write of the newest state. */
  save(state: ToolchainJobState): void {
    if (this.#closed) return;
    const pending = this.#queued !== null;
    this.#queued = state;
    if (pending) return;
    this.#writing = this.#writing.then(async () => {
      await new Promise((resolve) => setImmediate(resolve));
      const latest = this.#queued;
      this.#queued = null;
      if (latest === null || this.#closed) return;
      const tmp = `${this.#file}.${process.pid}.tmp`;
      try {
        await mkdir(path.dirname(this.#file), { recursive: true });
        await writeFile(tmp, JSON.stringify(latest), "utf8");
        // Synchronous from the check to the rename: `saveNow` (also
        // synchronous) cannot run in between and then be overwritten by an
        // older state landing after it.
        if (!this.#closed) renameSync(tmp, this.#file);
      } catch {
        // Best-effort: a lost write is a header that forgets the last job.
      }
    });
  }

  /** For quit, where nothing waits for a promise. */
  saveNow(state: ToolchainJobState): void {
    this.#closed = true;
    this.#queued = null;
    const tmp = `${this.#file}.${process.pid}.final.tmp`;
    try {
      mkdirSync(path.dirname(this.#file), { recursive: true });
      writeFileSync(tmp, JSON.stringify(state), "utf8");
      renameSync(tmp, this.#file);
    } catch {
      // Best-effort; see `save`.
    }
  }

  flush(): Promise<void> {
    return this.#writing;
  }
}
