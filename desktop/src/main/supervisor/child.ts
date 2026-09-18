import { spawn, type ChildProcess } from "node:child_process";
import { EventEmitter } from "node:events";
import type { ChildId, ChildState, ChildStatus } from "../../ipc/types.js";
import { LineSplitter } from "./log-buffer.js";

/**
 * One supervised child process.
 *
 * This is the piece a start script cannot have. A script starts a child once
 * and waits on it; if the backend dies at 3am the script is still running and
 * the window is simply showing a dead API. Here, a child that exits is
 * observed, logged as an exit rather than as silence, and restarted on a capped
 * exponential backoff.
 *
 * The backoff resets after a stable run: a process that stood for a while was
 * working, so whatever just broke it is a new problem, not a continuation of
 * the last one.
 */

export interface ChildSpec {
  id: ChildId;
  command: string;
  args: string[];
  env: NodeJS.ProcessEnv;
  cwd?: string;
  /**
   * Give the child a stdin pipe and close it to ask the child to stop. Only for
   * a child that watches its stdin (the backend, via SHUTDOWN_ON_STDIN_CLOSE).
   */
  stdinPipe?: boolean;
  /** Restart on an unexpected exit. False for one-shot children (none today). */
  restart: boolean;
}

export interface ChildEvents {
  log: [stream: "stdout" | "stderr", text: string];
  state: [status: ChildStatus];
  /** An exit the supervisor did not ask for. */
  crashed: [code: number | null, signal: NodeJS.Signals | null];
  exited: [code: number | null, signal: NodeJS.Signals | null];
}

const MIN_BACKOFF_MS = 1_000;
const MAX_BACKOFF_MS = 30_000;
/** How long a run has to last before its backoff is considered paid off. */
const STABLE_AFTER_MS = 60_000;
/**
 * How long a child gets between SIGTERM and SIGKILL.
 *
 * The backend drains on SIGTERM: it stops accepting, cancels the Claude Code
 * sessions it is supervising, reaps their process groups and shuts Postgres
 * down. A grace shorter than that drain orphans the very tree the ordered
 * teardown exists to take down. A GUI app quitting cannot hang forever either —
 * there is no terminal to Ctrl-C twice — so this is generous and bounded rather
 * than unlimited.
 */
const TERM_GRACE_MS = 30_000;

export class SupervisedChild extends EventEmitter<ChildEvents> {
  readonly id: ChildId;
  #spec: ChildSpec;
  #proc: ChildProcess | null = null;
  #state: ChildState = "idle";
  #detail: string | undefined;
  #startedAt: number | undefined;
  #exitedAt: number | undefined;
  #lastExitCode: number | null = null;
  #lastExitSignal: string | null = null;
  #restarts = 0;
  #backoffMs = MIN_BACKOFF_MS;
  #restartTimer: NodeJS.Timeout | null = null;
  #nextRestartAt: number | undefined;
  #enabled = true;
  /** True between stop() and the exit it caused, so that exit is not a crash. */
  #stopping = false;

  constructor(spec: ChildSpec) {
    super();
    this.id = spec.id;
    this.#spec = spec;
  }

  get state(): ChildState {
    return this.#state;
  }

  get running(): boolean {
    return this.#proc !== null && this.#proc.exitCode === null && !this.#proc.killed;
  }

  get pid(): number | undefined {
    return this.#proc?.pid;
  }

  status(): ChildStatus {
    return {
      id: this.id,
      state: this.#state,
      ...(this.#proc?.pid !== undefined && this.running ? { pid: this.#proc.pid } : {}),
      ...(this.#detail !== undefined ? { detail: this.#detail } : {}),
      lastExitCode: this.#lastExitCode,
      lastExitSignal: this.#lastExitSignal,
      ...(this.#startedAt !== undefined ? { startedAt: this.#startedAt } : {}),
      ...(this.#exitedAt !== undefined ? { exitedAt: this.#exitedAt } : {}),
      restarts: this.#restarts,
      ...(this.#nextRestartAt !== undefined ? { nextRestartAt: this.#nextRestartAt } : {}),
      enabled: this.#enabled,
    };
  }

  /** Replace the spec. Takes effect on the next start, not the running process. */
  update(spec: ChildSpec): void {
    this.#spec = spec;
  }

  setEnabled(enabled: boolean, detail?: string): void {
    this.#enabled = enabled;
    if (!enabled) this.#setState("skipped", detail);
  }

  #setState(state: ChildState, detail?: string): void {
    this.#state = state;
    this.#detail = detail;
    this.emit("state", this.status());
  }

  /**
   * Spawn the process. Resolves as soon as the spawn itself succeeds or fails
   * — waiting for the child to become USEFUL is the supervisor's job, not this
   * class's, because what "useful" means (here: /health answers 200) is a
   * fact about the product rather than about a pid.
   */
  start(): Promise<void> {
    if (this.running) return Promise.resolve();
    this.#cancelRestart();
    this.#setState("starting");

    return new Promise((resolve, reject) => {
      let settled = false;
      let proc: ChildProcess;
      try {
        proc = spawn(this.#spec.command, this.#spec.args, {
          env: this.#spec.env,
          ...(this.#spec.cwd !== undefined ? { cwd: this.#spec.cwd } : {}),
          stdio: [this.#spec.stdinPipe ? "pipe" : "ignore", "pipe", "pipe"],
          // No shell by default to protect arguments and secrets. On Windows,
          // batch scripts (.cmd, .bat) require shell: true or spawn throws EINVAL.
          shell: process.platform === "win32" && /\.(cmd|bat)$/i.test(this.#spec.command),
          // Its own process group, so a stray SIGINT reaching this app does
          // not race the ordered teardown by killing the children first.
          detached: process.platform !== "win32",
          // Without it a GUI app gets a console window per child on Windows.
          windowsHide: true,
        });
      } catch (err) {
        this.#setState("failed", err instanceof Error ? err.message : String(err));
        reject(err instanceof Error ? err : new Error(String(err)));
        return;
      }

      this.#proc = proc;
      // The child exiting first turns the eventual end() into EPIPE.
      proc.stdin?.on("error", () => undefined);
      this.#stopping = false;
      this.#startedAt = Date.now();
      this.#exitedAt = undefined;
      this.#nextRestartAt = undefined;

      this.#pipe(proc, "stdout");
      this.#pipe(proc, "stderr");

      proc.once("spawn", () => {
        if (settled) return;
        settled = true;
        this.#setState("waiting-health");
        resolve();
      });

      proc.once("error", (err) => {
        this.#setState("failed", err.message);
        if (!settled) {
          settled = true;
          reject(err);
        }
      });

      proc.once("exit", (code, signal) => {
        this.#onExit(code, signal);
        if (!settled) {
          settled = true;
          reject(new Error(`${this.id} exited immediately (code ${code ?? "null"}, signal ${signal ?? "none"})`));
        }
      });
    });
  }

  /** Called by the supervisor once its health gate passed. */
  markHealthy(detail?: string): void {
    if (this.#state === "waiting-health" || this.#state === "starting") this.#setState("healthy", detail);
  }

  #pipe(proc: ChildProcess, stream: "stdout" | "stderr"): void {
    const source = stream === "stdout" ? proc.stdout : proc.stderr;
    if (!source) return;
    const splitter = new LineSplitter();
    source.on("data", (chunk: Buffer) => {
      for (const line of splitter.push(chunk)) this.emit("log", stream, line);
    });
    source.on("end", () => {
      for (const line of splitter.flush()) this.emit("log", stream, line);
    });
  }

  #onExit(code: number | null, signal: NodeJS.Signals | null): void {
    const ranFor = this.#startedAt ? Date.now() - this.#startedAt : 0;
    this.#proc = null;
    this.#exitedAt = Date.now();
    this.#lastExitCode = code;
    this.#lastExitSignal = signal;

    if (this.#stopping) {
      this.#stopping = false;
      this.#setState("stopped");
      this.emit("exited", code, signal);
      return;
    }

    // See the note on STABLE_AFTER_MS: a long run pays off its own backoff.
    if (ranFor >= STABLE_AFTER_MS) this.#backoffMs = MIN_BACKOFF_MS;

    this.#setState("crashed", describeExit(code, signal, ranFor));
    this.emit("crashed", code, signal);
    this.emit("exited", code, signal);
  }

  /**
   * Schedule the restart the supervisor decided to allow. Separate from
   * `crashed` on purpose: whether a crash is worth restarting is a question
   * about the whole product, and that knowledge lives in the supervisor.
   */
  scheduleRestart(onDue: () => void): number {
    if (!this.#spec.restart) return 0;
    const delay = this.#backoffMs;
    this.#nextRestartAt = Date.now() + delay;
    this.#setState("restarting", `restarting in ${Math.round(delay / 1000)}s`);
    this.#cancelRestart();
    this.#restartTimer = setTimeout(() => {
      this.#restartTimer = null;
      this.#nextRestartAt = undefined;
      this.#restarts += 1;
      onDue();
    }, delay);
    this.#backoffMs = Math.min(this.#backoffMs * 2, MAX_BACKOFF_MS);
    return delay;
  }

  #cancelRestart(): void {
    if (this.#restartTimer) {
      clearTimeout(this.#restartTimer);
      this.#restartTimer = null;
    }
    this.#nextRestartAt = undefined;
  }

  resetBackoff(): void {
    this.#backoffMs = MIN_BACKOFF_MS;
  }

  resetCounters(): void {
    this.#restarts = 0;
    this.#backoffMs = MIN_BACKOFF_MS;
    this.#lastExitCode = null;
    this.#lastExitSignal = null;
  }

  /**
   * SIGTERM, then SIGKILL after `TERM_GRACE_MS`.
   *
   * TERM first, always: it is what lets the backend finish the requests it is
   * serving, cancel the Claude Code sessions it is supervising and shut its
   * database down cleanly, and what stops a quit from leaving a `claude`
   * process behind holding a checkout open.
   */
  async stop(): Promise<void> {
    this.#cancelRestart();
    const proc = this.#proc;
    if (!proc || proc.exitCode !== null) {
      this.#setState("stopped");
      return;
    }
    this.#stopping = true;
    this.#setState("stopping");

    const exited = new Promise<void>((resolve) => proc.once("exit", () => resolve()));
    // Closing stdin is the request that works everywhere. Windows has no
    // SIGTERM: kill() there is TerminateProcess, which would skip the backend's
    // drain and orphan its Postgres.
    proc.stdin?.end();
    if (process.platform !== "win32") {
      try {
        proc.kill("SIGTERM");
      } catch {
        // Already gone between the check and the signal.
      }
    }

    const timer = new Promise<"timeout">((resolve) => setTimeout(() => resolve("timeout"), TERM_GRACE_MS));
    const outcome = await Promise.race([exited.then(() => "exited" as const), timer]);
    if (outcome === "timeout") {
      this.emit("log", "stderr", `[supervisor] ${this.id} did not stop within ${TERM_GRACE_MS / 1000}s; killing it`);
      try {
        if (process.platform === "win32") proc.kill();
        else proc.kill("SIGKILL");
      } catch {
        // Raced with its own exit; the await below settles either way.
      }
      await exited;
    }
    this.#stopping = false;
    this.#setState("stopped");
  }
}

function describeExit(code: number | null, signal: NodeJS.Signals | null, ranForMs: number): string {
  const ran = ranForMs >= 1000 ? `after ${Math.round(ranForMs / 1000)}s` : "immediately";
  if (signal) return `killed by ${signal} ${ran}`;
  return `exited with code ${code ?? "unknown"} ${ran}`;
}
