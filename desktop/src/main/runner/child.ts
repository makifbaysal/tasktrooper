import { spawn, type ChildProcess } from "node:child_process";
import { EventEmitter } from "node:events";
import { LineSplitter } from "../supervisor/log-buffer.js";
import { killTree, signalTree } from "../supervisor/reaper.js";
import type { ChildState, ChildStatus } from "../../ipc/types.js";

/**
 * The runner: account mode's one supervised child.
 *
 * A start script starts a child once and waits on it; if the tunnel daemon
 * dies at 3am the script is still running and the control plane has simply
 * stopped hearing from this computer. Here, an exit is observed, logged as an exit
 * rather than as silence, and restarted on a capped exponential backoff that
 * resets after a stable run — a process that stood for a while was working,
 * so whatever just broke it is a new problem, not a continuation of the last.
 */

export interface RunnerChildSpec {
  command: string;
  args: string[];
  env: NodeJS.ProcessEnv;
  /**
   * The first line written to the runner's stdin, which then STAYS OPEN.
   *
   * This is how the runner is configured, and the reason it is a pipe rather
   * than an environment variable is that a same-user process on macOS can
   * read another's environment through `KERN_PROCARGS2`. It stays open
   * because it is also a control channel: `send()` pushes later preflight
   * updates down the same pipe, and the runner relays the latest one over the
   * tunnel. Its closing is the runner's shutdown request — see `stop()`.
   */
  stdin: string;
}

export interface RunnerChildEvents {
  log: [stream: "stdout" | "stderr", text: string];
  state: [status: ChildStatus];
  /** An exit this class did not ask for. */
  crashed: [code: number | null, signal: NodeJS.Signals | null];
  exited: [code: number | null, signal: NodeJS.Signals | null];
}

const MIN_BACKOFF_MS = 1_000;
const MAX_BACKOFF_MS = 30_000;
/** How long a run has to last before its backoff is considered paid off. */
const STABLE_AFTER_MS = 60_000;

/**
 * The runner's own worst-case drain, copied from `desktop/runner/session.go`.
 *
 * `claudeGrace` (5s) is what each in-flight Claude Code session gets between
 * SIGTERM and SIGKILL; `claudeReapTimeout` (20s) bounds the wait for the group
 * to be reaped afterwards. Runs drain concurrently, so the worst case is one
 * run's, not the sum of all of them — the Go constant `drainBudget`.
 *
 * Duplicated here because a TypeScript file cannot import a Go constant, and
 * `desktop/runner/rules_test.go`'s `TestSupervisorGraceCoversThisProgramsDrain`
 * fails if the two ever disagree. That test is what makes this a derivation
 * rather than a coincidence — do not change these numbers without it.
 */
const RUNNER_CLAUDE_GRACE_MS = 5_000;
const RUNNER_REAP_TIMEOUT_MS = 20_000;
const RUNNER_DRAIN_BUDGET_MS = RUNNER_CLAUDE_GRACE_MS + RUNNER_REAP_TIMEOUT_MS;

/**
 * How long the runner gets between the stop request and the forced kill.
 *
 * DERIVED, not chosen: it was picked independently once (8s here against the
 * runner's 25s worst case), and quitting the app mid-task killed the runner
 * before it had finished reaping its `claude` process groups — orphaning the
 * tree the ordered teardown exists to take down, and leaving a run's MCP
 * bearer token on disk until the next startup sweep. A process being asked to
 * drain must be given the drain it advertises; the margin covers the runner's
 * own work either side of that (closing the yamux session, the websocket, its
 * last log lines).
 */
const TERM_GRACE_MS = RUNNER_DRAIN_BUDGET_MS + 5_000;

export class RunnerChild extends EventEmitter<RunnerChildEvents> {
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
  /** True between stop() and the exit it caused, so that exit is not a crash. */
  #stopping = false;

  get state(): ChildState {
    return this.#state;
  }

  get running(): boolean {
    return this.#proc !== null && this.#proc.exitCode === null && !this.#proc.killed;
  }

  status(): ChildStatus {
    return {
      id: "runner",
      state: this.#state,
      ...(this.#proc?.pid !== undefined && this.running ? { pid: this.#proc.pid } : {}),
      ...(this.#detail !== undefined ? { detail: this.#detail } : {}),
      lastExitCode: this.#lastExitCode,
      lastExitSignal: this.#lastExitSignal,
      ...(this.#startedAt !== undefined ? { startedAt: this.#startedAt } : {}),
      ...(this.#exitedAt !== undefined ? { exitedAt: this.#exitedAt } : {}),
      restarts: this.#restarts,
      ...(this.#nextRestartAt !== undefined ? { nextRestartAt: this.#nextRestartAt } : {}),
      enabled: true,
    };
  }

  #setState(state: ChildState, detail?: string): void {
    this.#state = state;
    this.#detail = detail;
    this.emit("state", this.status());
  }

  /**
   * Spawn the process. Resolves as soon as the spawn itself succeeds or fails
   * — waiting for the runner to become USEFUL (the tunnel has attached) is the
   * caller's job, because that fact comes from a log line, not from a pid.
   */
  start(spec: RunnerChildSpec): Promise<void> {
    this.#cancelRestart();
    this.#setState("starting");

    return new Promise((resolve, reject) => {
      let settled = false;
      let proc: ChildProcess;
      try {
        proc = spawn(spec.command, spec.args, {
          env: spec.env,
          // stdin is a pipe this process holds open for the runner's whole
          // life: the runner reads its closing as "stop", so a stdin that
          // was not a pipe, or one ended early, would stop it at once.
          stdio: ["pipe", "pipe", "pipe"],
          // No shell, ever: the pairing bundle's token and the workspace path
          // both land in this document, and handing either to /bin/sh would
          // make a backtick in a token a command again.
          shell: false,
          // Its own process group, so a stray SIGINT reaching this app does
          // not race the ordered teardown by killing the runner first. Not on
          // Windows, where detached means a new console window of its own.
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
      this.#stopping = false;
      this.#startedAt = Date.now();
      this.#exitedAt = undefined;
      this.#nextRestartAt = undefined;

      this.#pipe(proc, "stdout");
      this.#pipe(proc, "stderr");
      proc.stdin?.on("error", () => undefined);
      proc.stdin?.write(spec.stdin);

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
          reject(new Error(`runner exited immediately (code ${code ?? "null"}, signal ${signal ?? "none"})`));
        }
      });
    });
  }

  /** Called once the tunnel has attached. */
  markHealthy(): void {
    if (this.#state === "waiting-health" || this.#state === "starting") this.#setState("healthy");
  }

  /**
   * Push one control line down the runner's still-open stdin — a preflight
   * update today, and nothing else yet.
   *
   * Returns false when there is nothing to write to, the normal case between a
   * crash and a restart; a caller that treated it as an error would be
   * reporting the child's own state twice.
   */
  send(line: string): boolean {
    const stdin = this.#proc?.stdin;
    // writableEnded: stop() has already asked the runner to go, and a write
    // after end() is an error rather than a late message.
    if (!stdin || stdin.destroyed || stdin.writableEnded) return false;
    return stdin.write(line);
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

    if (ranFor >= STABLE_AFTER_MS) this.#backoffMs = MIN_BACKOFF_MS;

    this.#setState("crashed", describeExit(code, signal, ranFor));
    this.emit("crashed", code, signal);
    this.emit("exited", code, signal);
  }

  /** Schedule the restart the caller decided to allow. */
  scheduleRestart(onDue: () => void): number {
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

  resetCounters(): void {
    this.#cancelRestart();
    this.#restarts = 0;
    this.#backoffMs = MIN_BACKOFF_MS;
    this.#lastExitCode = null;
    this.#lastExitSignal = null;
  }

  /**
   * Ask the runner to stop, wait `TERM_GRACE_MS`, then kill its whole tree.
   *
   * Asking first, always: it is what lets the runner cancel the Claude Code
   * sessions it is supervising and close its yamux session and websocket
   * cleanly, which is what makes the control plane see a detach rather than a
   * session that stopped answering — and what stops a quit from leaving a
   * `claude` process behind.
   *
   * Ending stdin is the request, on every OS: the runner drains on EOF exactly
   * as it does on SIGTERM, and on Windows there is no SIGTERM — `kill()` there
   * is TerminateProcess, which skips the drain entirely. SIGTERM is still sent
   * where it exists, as `supervisor/child.ts` does: the runner treats the two
   * alike, and a signal does not depend on the pipe still being writable.
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
    const pid = proc.pid;
    const win = process.platform === "win32";
    proc.stdin?.end();
    if (!win) {
      if (pid !== undefined) signalTree(pid, "SIGTERM");
      else this.#signal(proc, "SIGTERM");
    }

    let timer: NodeJS.Timeout | undefined;
    const timeout = new Promise<"timeout">((resolve) => {
      timer = setTimeout(() => resolve("timeout"), TERM_GRACE_MS);
    });
    const outcome = await Promise.race([exited.then(() => "exited" as const), timeout]);
    clearTimeout(timer);
    if (outcome === "timeout") {
      this.emit(
        "log",
        "stderr",
        `[supervisor] runner did not stop within ${TERM_GRACE_MS / 1000}s; ${win ? "terminating it" : "sending SIGKILL"}`,
      );
      // On Windows only the runner itself: its sessions sit in job objects
      // that die with it, while the emulator it started broke away on purpose
      // and must outlive it as it does on unix — `taskkill /T` follows parent
      // pids and would take the emulator down too.
      if (win) proc.kill();
      else if (pid !== undefined) await killTree(pid);
      else this.#signal(proc, "SIGKILL");
      await exited;
    }
    this.#stopping = false;
    this.#setState("stopped");
  }

  #signal(proc: ChildProcess, signal: NodeJS.Signals): void {
    try {
      proc.kill(signal);
    } catch {
      // Raced with its own exit; the caller's await settles either way.
    }
  }
}

function describeExit(code: number | null, signal: NodeJS.Signals | null, ranForMs: number): string {
  const ran = ranForMs >= 1000 ? `after ${Math.round(ranForMs / 1000)}s` : "immediately";
  if (signal) return `killed by ${signal} ${ran}`;
  return `exited with code ${code ?? "unknown"} ${ran}`;
}
