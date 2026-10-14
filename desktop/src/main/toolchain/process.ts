import { spawn, type ChildProcess } from "node:child_process";
import path from "node:path";
import { probeEnvFor } from "../services/detect.js";
import { launchFor } from "../services/winshim.js";
import { signalTree, taskkillTree } from "../supervisor/reaper.js";
import { CancelledError, InstallError, type ExecOptions } from "./types.js";

/**
 * Running an installer: no shell on any platform (a Windows `.cmd` becomes
 * the program it would have run, `winshim.ts`), its own process group on
 * POSIX so a cancel takes down everything it started, and its output as
 * lines.
 *
 * The environment is the probes' (`probeEnv`: the login PATH and the
 * Homebrew prefixes a GUI app does not inherit) with the program's own
 * directory first, plus what the recipe adds. Nothing secret is added here,
 * and nothing goes on argv but the recipe's own fixed arguments.
 */

/** How long an installer gets after SIGTERM before the group is killed. */
export const KILL_GRACE_MS = 5_000;

/**
 * How long after the installer exits its output may still be draining. Past
 * it the step goes on: an installer that started a daemon which inherited its
 * stdout (a service it registered) would otherwise hold the step open for as
 * long as that daemon runs.
 */
export const OUTPUT_DRAIN_MS = 2_000;

/**
 * Lines on `\n`, `\r\n` and a bare `\r` — progress bars redraw with `\r`,
 * and the last redraw is the line worth keeping.
 */
export class OutputLines {
  #partial = "";

  push(text: string): string[] {
    this.#partial += text;
    const parts = this.#partial.split(/\r\n|\r|\n/);
    this.#partial = parts.pop() ?? "";
    if (this.#partial.length > 16 * 1024) {
      parts.push(this.#partial);
      this.#partial = "";
    }
    return parts.filter((line) => line.trim() !== "");
  }

  flush(): string[] {
    const rest = this.#partial;
    this.#partial = "";
    return rest.trim() === "" ? [] : [rest];
  }
}

function stopTree(child: ChildProcess): void {
  const pid = child.pid;
  if (pid === undefined) {
    child.kill();
    return;
  }
  if (process.platform === "win32") {
    void taskkillTree(pid);
    return;
  }
  signalTree(pid, "SIGTERM");
  const timer = setTimeout(() => {
    if (child.exitCode === null && child.signalCode === null) signalTree(pid, "SIGKILL");
  }, KILL_GRACE_MS);
  timer.unref?.();
}

export interface ExecRequest extends ExecOptions {
  signal: AbortSignal;
}

/** Resolves with the exit code (128 + n for a signal); rejects with `CancelledError` once `signal` aborts. */
export function execLogged(command: string, args: readonly string[], req: ExecRequest): Promise<number> {
  return new Promise((resolve, reject) => {
    if (req.signal.aborted) {
      reject(new CancelledError());
      return;
    }
    const env = { ...probeEnvFor(command), ...req.env };
    const launch = launchFor(command, [...args], env);
    let child: ChildProcess;
    try {
      child = spawn(launch.command, launch.args, {
        env: launch.env,
        ...(req.cwd !== undefined ? { cwd: req.cwd } : {}),
        stdio: [req.input !== undefined ? "pipe" : "ignore", "pipe", "pipe"],
        shell: false,
        ...(launch.windowsVerbatimArguments ? { windowsVerbatimArguments: true } : {}),
        detached: process.platform !== "win32",
        windowsHide: true,
      });
    } catch (err) {
      reject(new InstallError(`Could not start ${path.basename(command)}: ${err instanceof Error ? err.message : String(err)}`));
      return;
    }

    const emit = (lines: string[]): void => {
      for (const line of lines) req.onLine?.(line);
    };
    for (const stream of [child.stdout, child.stderr]) {
      if (!stream) continue;
      const splitter = new OutputLines();
      stream.on("data", (chunk: Buffer) => emit(splitter.push(chunk.toString("utf8"))));
      stream.on("end", () => emit(splitter.flush()));
    }
    if (req.input !== undefined && child.stdin) {
      // A child that exits before reading all of it turns the write into EPIPE.
      child.stdin.on("error", () => undefined);
      child.stdin.end(req.input);
    }

    const onAbort = (): void => stopTree(child);
    req.signal.addEventListener("abort", onAbort, { once: true });
    let settled = false;
    const settle = (fn: () => void): void => {
      if (settled) return;
      settled = true;
      req.signal.removeEventListener("abort", onAbort);
      fn();
    };
    child.once("error", (err) =>
      settle(() => reject(new InstallError(`Could not start ${path.basename(command)}: ${err.message}`))),
    );
    const finish = (code: number | null, signal: NodeJS.Signals | null): void =>
      settle(() => {
        if (req.signal.aborted) reject(new CancelledError());
        else resolve(code ?? (signal ? 128 + (signalNumber(signal) ?? 0) : 1));
      });
    child.once("close", finish);
    child.once("exit", (code, signal) => {
      const timer = setTimeout(() => finish(code, signal), OUTPUT_DRAIN_MS);
      timer.unref?.();
    });
  });
}

function signalNumber(signal: NodeJS.Signals): number | undefined {
  return ({ SIGHUP: 1, SIGINT: 2, SIGKILL: 9, SIGTERM: 15 } as Partial<Record<NodeJS.Signals, number>>)[signal];
}
