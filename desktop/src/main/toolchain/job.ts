import { randomUUID } from "node:crypto";
import { EventEmitter } from "node:events";
import type {
  ToolchainId,
  ToolchainJobState,
  ToolchainJobStep,
  ToolchainStepState,
} from "../../ipc/types.js";
import { LogRing } from "./log-ring.js";
import {
  CancelledError,
  type Detection,
  type DownloadOptions,
  type ExecOptions,
  type HostFacts,
  type Installer,
  type StepContext,
  type StepProgress,
} from "./types.js";

/**
 * The install job: one at a time, its steps one after another — brew, winget
 * and apt each hold a lock, and two installs racing for one fail in ways that
 * read like the tool being broken.
 *
 * A step's life: queued → running (→ waiting, for an installer the person
 * finishes) → done | failed | skipped | cancelled. It is decided by
 * detection, before and after, not by the installer's exit code: an
 * already-installed tool is skipped, and an install that exited 0 but left
 * nothing findable is a failure that says so ("Installed, but not found
 * afterwards"). A step whose dependency did not install is skipped with an
 * error naming it, rather than run to fail on its own.
 *
 * Observers hear `change` (with whether the phase moved, which the publisher
 * pushes at once) and `finished`; they read `snapshot()` themselves, so a
 * flood of log lines costs one snapshot per push, not one per line.
 */

export const INTERRUPTED = "Interrupted when the app quit.";

export const LINGERING = "Still finishing in the background — it can't be stopped from here.";

/**
 * How long a cancelled step gets to end before the job stops waiting for it:
 * past process.ts's SIGTERM → SIGKILL grace, with room for the exit. An
 * installer running as administrator (pkexec, sudo, a UAC prompt) cannot be
 * signalled by this process at all, and the job must not sit in `running`
 * for as long as apt or a vendor installer takes to finish on its own.
 */
export const STOP_GRACE_MS = 7_000;

const FINISHED: ReadonlySet<ToolchainStepState> = new Set(["done", "failed", "skipped", "cancelled"]);

export function isFinished(state: ToolchainStepState): boolean {
  return FINISHED.has(state);
}

/** What the runner needs from the machine to give a step its context. */
export interface StepMachine {
  readonly workDir: string;
  host(): HostFacts;
  exec(command: string, args: readonly string[], opts: ExecOptions & { signal: AbortSignal }): Promise<number>;
  download(url: string, dest: string, opts: DownloadOptions & { signal: AbortSignal }): Promise<void>;
}

export interface JobRunnerDeps {
  /** The installer for an id on this machine now; null when the tool does not apply here. */
  installerFor(id: ToolchainId): Installer | null;
  label(id: ToolchainId): string;
  affectsRuntime(id: ToolchainId): boolean;
  detect(id: ToolchainId): Promise<Detection>;
  machine: StepMachine;
  now?: () => number;
  newJobId?: () => string;
  sleep?: (ms: number, signal: AbortSignal) => Promise<void>;
  stopGraceMs?: number;
}

interface StepRecord {
  view: ToolchainJobStep;
  log: LogRing;
  /** Whether a step that needs this one may go ahead: installed now, or already was. */
  satisfied?: boolean;
}

interface JobEvents {
  change: [phaseChanged: boolean];
  finished: [state: ToolchainJobState];
  settled: [id: ToolchainId];
}

function abortableSleep(ms: number, signal: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    if (signal.aborted) {
      reject(new CancelledError());
      return;
    }
    const timer = setTimeout(() => {
      signal.removeEventListener("abort", onAbort);
      resolve();
    }, ms);
    const onAbort = (): void => {
      clearTimeout(timer);
      reject(new CancelledError());
    };
    signal.addEventListener("abort", onAbort, { once: true });
  });
}

const messageOf = (err: unknown): string => (err instanceof Error ? err.message : String(err));

export class ToolchainJobRunner {
  readonly #deps: JobRunnerDeps;
  readonly #now: () => number;
  readonly #events = new EventEmitter<JobEvents>();
  #phase: ToolchainJobState["phase"] = "idle";
  #jobId: string | undefined;
  #startedAt: number | undefined;
  #finishedAt: number | undefined;
  #steps: StepRecord[] = [];
  #restartRecommended = false;
  #current: { record: StepRecord; controller: AbortController } | null = null;
  #cancelled = false;
  #stopped = false;
  /** Asked for while a cancelled job was still winding down: the next job. */
  #next: ToolchainId[] = [];
  #loop: Promise<void> = Promise.resolve();

  constructor(deps: JobRunnerDeps, initial?: ToolchainJobState) {
    this.#deps = deps;
    this.#now = deps.now ?? Date.now;
    if (initial && initial.phase !== "idle") {
      this.#phase = initial.phase;
      if (initial.jobId !== undefined) this.#jobId = initial.jobId;
      if (initial.startedAt !== undefined) this.#startedAt = initial.startedAt;
      if (initial.finishedAt !== undefined) this.#finishedAt = initial.finishedAt;
      this.#steps = initial.steps.map((step) => {
        const { log, ...view } = step;
        return { view: { ...view }, log: new LogRing(log ?? []) };
      });
    }
  }

  snapshot(): ToolchainJobState {
    const steps = this.#steps.map(({ view, log }) => ({ ...view, ...(log.size > 0 ? { log: log.lines } : {}) }));
    const finished = steps.filter((s) => FINISHED.has(s.state)).length;
    const active = steps.find((s) => s.state === "running" || s.state === "waiting");
    const partial = active?.percent !== undefined ? active.percent / 100 : 0;
    return {
      phase: this.#phase,
      ...(this.#jobId !== undefined ? { jobId: this.#jobId } : {}),
      ...(this.#startedAt !== undefined ? { startedAt: this.#startedAt } : {}),
      ...(this.#finishedAt !== undefined ? { finishedAt: this.#finishedAt } : {}),
      steps,
      percent: steps.length === 0 ? 0 : Math.min(100, Math.round(((finished + partial) / steps.length) * 100)),
      ...(this.#restartRecommended ? { restartRecommended: true } : {}),
    };
  }

  /** Ids the running job will still get to: queued, running or waiting. */
  pendingIds(): ToolchainId[] {
    if (this.#phase !== "running" || this.#cancelled) return [];
    return this.#steps.filter((r) => !FINISHED.has(r.view.state)).map((r) => r.view.id);
  }

  get running(): boolean {
    return this.#phase === "running";
  }

  onChange(listener: (phaseChanged: boolean) => void): () => void {
    this.#events.on("change", listener);
    return () => this.#events.off("change", listener);
  }

  onFinished(listener: (state: ToolchainJobState) => void): () => void {
    this.#events.on("finished", listener);
    return () => this.#events.off("finished", listener);
  }

  /** A step reached its end state: detection results from before it are stale. */
  onSettled(listener: (id: ToolchainId) => void): () => void {
    this.#events.on("settled", listener);
    return () => this.#events.off("settled", listener);
  }

  /** Resolves when the current job's loop has ended. */
  idle(): Promise<void> {
    return this.#loop;
  }

  /**
   * `ids` is a plan: dependencies first, nothing installed. Starts a job, or
   * appends what the running one does not already have. An id whose step
   * already ended without installing it (failed, cancelled, skipped for a
   * dependency) is a new attempt: its old step goes and a new one is queued
   * at the end, so the job never lists one tool twice.
   */
  enqueue(ids: readonly ToolchainId[]): ToolchainJobState {
    if (ids.length === 0 || this.#stopped) return this.snapshot();
    if (this.#phase === "running") {
      if (this.#cancelled) {
        this.#next = [...new Set([...this.#next, ...ids])];
        return this.snapshot();
      }
      let added = false;
      for (const id of ids) {
        const at = this.#steps.findIndex((r) => r.view.id === id);
        const existing = this.#steps[at];
        if (existing && (!FINISHED.has(existing.view.state) || existing.satisfied)) continue;
        if (existing) this.#steps.splice(at, 1);
        this.#steps.push(this.#record(id));
        added = true;
      }
      if (added) this.#emit(false);
      return this.snapshot();
    }
    this.#phase = "running";
    this.#jobId = (this.#deps.newJobId ?? randomUUID)();
    this.#startedAt = this.#now();
    this.#finishedAt = undefined;
    this.#steps = [...new Set(ids)].map((id) => this.#record(id));
    this.#restartRecommended = false;
    this.#cancelled = false;
    this.#emit(true);
    this.#loop = this.#run();
    return this.snapshot();
  }

  /** Kills the running step's process tree; queued steps are cancelled without starting. */
  cancel(): ToolchainJobState {
    if (this.#phase !== "running" || this.#cancelled) return this.snapshot();
    this.#cancelled = true;
    const at = this.#now();
    for (const record of this.#steps) {
      if (record.view.state === "queued") this.#settle(record, "cancelled", { error: "Cancelled." }, at);
    }
    if (this.#current) {
      this.#current.controller.abort();
      this.#settle(this.#current.record, "cancelled", { error: "Cancelled." }, at);
    }
    this.#emit(false);
    return this.snapshot();
  }

  /** Back to idle once a job is over; a running job is not dismissed. */
  dismiss(): ToolchainJobState {
    if (this.#phase !== "done") return this.snapshot();
    this.#phase = "idle";
    this.#jobId = undefined;
    this.#startedAt = undefined;
    this.#finishedAt = undefined;
    this.#steps = [];
    this.#restartRecommended = false;
    this.#emit(true);
    return this.snapshot();
  }

  /**
   * Quit: the running step's process tree is killed and every unfinished step
   * is failed as interrupted. Nothing is emitted — the page is going away —
   * and the answer is what the caller persists.
   */
  shutdown(): ToolchainJobState {
    if (this.#phase !== "running") return this.snapshot();
    this.#stopped = true;
    this.#current?.controller.abort();
    const at = this.#now();
    for (const record of this.#steps) {
      if (!FINISHED.has(record.view.state)) this.#settle(record, "failed", { error: INTERRUPTED }, at);
    }
    this.#phase = "done";
    this.#finishedAt = at;
    return this.snapshot();
  }

  #record(id: ToolchainId): StepRecord {
    return {
      view: { id, label: this.#deps.label(id), state: "queued", installKind: this.#deps.installerFor(id)?.installKind ?? "manual" },
      log: new LogRing(),
    };
  }

  #emit(phaseChanged: boolean): void {
    if (this.#stopped) return;
    this.#events.emit("change", phaseChanged);
  }

  async #run(): Promise<void> {
    // The first queued step each time, not an index: a retried step is
    // removed from where it was and appended, which would shift an index.
    for (;;) {
      if (this.#cancelled || this.#stopped) break;
      const record = this.#steps.find((r) => r.view.state === "queued");
      if (!record) break;
      try {
        await this.#runStep(record);
      } catch (err) {
        if (!FINISHED.has(record.view.state)) this.#settle(record, "failed", { error: messageOf(err) });
      }
    }
    if (this.#stopped) return;
    const at = this.#now();
    for (const record of this.#steps) {
      if (!FINISHED.has(record.view.state)) this.#settle(record, "cancelled", { error: "Cancelled." }, at);
    }
    this.#phase = "done";
    this.#finishedAt = at;
    this.#current = null;
    this.#emit(true);
    this.#events.emit("finished", this.snapshot());
    const next = this.#next;
    this.#next = [];
    if (next.length > 0) this.enqueue(next);
  }

  async #runStep(record: StepRecord): Promise<void> {
    const id = record.view.id;
    const controller = new AbortController();
    const { signal } = controller;
    this.#current = { record, controller };
    try {
      this.#update(record, { state: "running", startedAt: this.#now(), detail: "Checking whether it is already here…" });
      const before = await this.#detect(id);
      if (signal.aborted) return this.#aborted(record);
      if (before.status === "installed") {
        this.#settle(record, "skipped", { error: "Already installed." }, undefined, true);
        return;
      }

      const installer = this.#deps.installerFor(id);
      if (!installer || installer.installKind === "manual") {
        const where = installer?.url ? ` Install it from ${installer.url}` : "";
        this.#settle(record, "failed", { error: `TaskTrooper cannot install this on this computer.${where}` });
        return;
      }
      record.view.installKind = installer.installKind;

      const unmet = await this.#unmetDependency(installer.dependsOn);
      if (signal.aborted) return this.#aborted(record);
      if (unmet) {
        this.#settle(record, "skipped", { error: unmet });
        return;
      }

      this.#update(record, { detail: `Installing with ${installer.method}…` });
      let failure: unknown;
      const ran = installer.run(this.#context(record, signal)).then(
        () => undefined,
        (err: unknown) => {
          failure = err;
        },
      );
      const lingering = await this.#endOrGrace(ran, signal);
      if (signal.aborted) return this.#aborted(record, lingering);

      this.#update(record, { detail: "Checking the install…" });
      const after = await this.#detect(id);
      if (signal.aborted) return this.#aborted(record);
      if (after.status === "installed") {
        if (this.#deps.affectsRuntime(id)) this.#restartRecommended = true;
        this.#settle(record, "done", { detail: after.version ? `Installed ${after.version}.` : "Installed." }, undefined, true);
        return;
      }
      const why = after.detail ?? (after.status === "unusable" ? "it is there but does not work" : "it is still missing");
      this.#settle(record, "failed", {
        error: failure !== undefined ? messageOf(failure) : `Installed, but not found afterwards: ${why}`,
      });
    } finally {
      if (this.#current?.record === record) this.#current = null;
    }
  }

  #aborted(record: StepRecord, lingering = false): void {
    if (this.#stopped) {
      this.#settle(record, "failed", { error: INTERRUPTED });
      return;
    }
    this.#settle(record, "cancelled", { error: "Cancelled." });
    if (lingering) {
      record.view.detail = LINGERING;
      this.#emit(false);
    }
  }

  /** True when the step was cancelled and its installer had not ended a grace period later. */
  #endOrGrace(ran: Promise<void>, signal: AbortSignal): Promise<boolean> {
    const graceMs = this.#deps.stopGraceMs ?? STOP_GRACE_MS;
    return new Promise((resolve) => {
      let timer: ReturnType<typeof setTimeout> | undefined;
      const onAbort = (): void => {
        timer = setTimeout(() => resolve(true), graceMs);
      };
      if (signal.aborted) onAbort();
      else signal.addEventListener("abort", onAbort, { once: true });
      void ran.then(() => {
        signal.removeEventListener("abort", onAbort);
        clearTimeout(timer);
        resolve(false);
      });
    });
  }

  async #unmetDependency(deps: readonly ToolchainId[]): Promise<string | null> {
    for (const dep of deps) {
      const step = this.#steps.find((r) => r.view.id === dep);
      if (step && FINISHED.has(step.view.state)) {
        if (step.satisfied) continue;
        return `Skipped: ${this.#deps.label(dep)} did not install.`;
      }
      const found = await this.#detect(dep);
      if (found.status === "installed") continue;
      const installer = this.#deps.installerFor(dep);
      const where = installer?.installKind === "manual" && installer.url ? ` Install it from ${installer.url}` : "";
      return `Skipped: it needs ${this.#deps.label(dep)}, which is not installed.${where}`;
    }
    return null;
  }

  async #detect(id: ToolchainId): Promise<Detection> {
    try {
      return await this.#deps.detect(id);
    } catch (err) {
      return { status: "missing", detail: `Could not check: ${messageOf(err)}` };
    }
  }

  #context(record: StepRecord, signal: AbortSignal): StepContext {
    const machine = this.#deps.machine;
    const log = (line: string): void => {
      record.log.push(line);
      this.#emit(false);
    };
    const sleep = this.#deps.sleep ?? abortableSleep;
    return {
      id: record.view.id,
      label: record.view.label,
      signal,
      host: machine.host(),
      workDir: machine.workDir,
      exec: (command, args, opts = {}) =>
        machine.exec(command, args, {
          ...opts,
          signal,
          onLine: (line) => {
            log(line);
            opts.onLine?.(line);
          },
        }),
      download: (url, dest, opts = {}) => machine.download(url, dest, { ...opts, signal }),
      progress: (update) => this.#progress(record, update),
      log,
      detect: (other) => this.#detect(other ?? record.view.id),
      sleep: (ms) => sleep(ms, signal),
    };
  }

  #progress(record: StepRecord, update: StepProgress): void {
    if (FINISHED.has(record.view.state)) return;
    if (update.waiting === true) record.view.state = "waiting";
    else if (update.waiting === false && record.view.state === "waiting") record.view.state = "running";
    if (update.detail !== undefined) record.view.detail = update.detail;
    if (update.percent === null) delete record.view.percent;
    else if (update.percent !== undefined) record.view.percent = Math.max(0, Math.min(100, Math.round(update.percent)));
    this.#emit(false);
  }

  #update(record: StepRecord, patch: Partial<ToolchainJobStep>): void {
    if (FINISHED.has(record.view.state)) return;
    Object.assign(record.view, patch);
    this.#emit(false);
  }

  #settle(
    record: StepRecord,
    state: ToolchainStepState,
    text: { error?: string; detail?: string },
    at: number = this.#now(),
    satisfied = false,
  ): void {
    if (FINISHED.has(record.view.state)) return;
    const view = record.view;
    view.state = state;
    view.finishedAt = at;
    delete view.percent;
    if (text.detail !== undefined) view.detail = text.detail;
    else delete view.detail;
    if (text.error !== undefined) view.error = text.error;
    record.satisfied = satisfied;
    this.#emit(false);
    if (!this.#stopped) this.#events.emit("settled", view.id);
  }
}
