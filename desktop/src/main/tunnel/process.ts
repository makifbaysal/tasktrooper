import { spawn } from "node:child_process";
import { existsSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import path from "node:path";
import { probeEnv } from "../services/detect.js";
import { launchFor } from "../services/winshim.js";
import { LineSplitter } from "../supervisor/log-buffer.js";

/** A running tunnel program, as the manager sees it. Faked in tests. */
export interface TunnelProcess {
  onLine(cb: (line: string) => void): void;
  onExit(cb: (code: number | null, signal: string | null) => void): void;
  /** SIGTERM, then SIGKILL after a grace period; resolves when it is gone. */
  kill(): Promise<void>;
}

export type TunnelSpawner = (command: string, args: string[], env: Record<string, string>) => TunnelProcess;

const KILL_GRACE_MS = 5_000;

/**
 * `shell: false`, secrets only in `env`. stdout and stderr are merged into one
 * line stream: cloudflared logs on stderr, ngrok (with --log=stdout) on stdout.
 */
export const spawnTunnel: TunnelSpawner = (command, args, extraEnv) => {
  const launch = launchFor(command, args, { ...probeEnv(), ...extraEnv });
  const child = spawn(launch.command, launch.args, {
    env: launch.env,
    stdio: ["ignore", "pipe", "pipe"],
    shell: false,
    windowsHide: true,
    ...(launch.windowsVerbatimArguments ? { windowsVerbatimArguments: true } : {}),
  });
  let exited = false;
  const lineCallbacks: ((line: string) => void)[] = [];
  const exitCallbacks: ((code: number | null, signal: string | null) => void)[] = [];
  for (const stream of [child.stdout, child.stderr]) {
    const splitter = new LineSplitter();
    stream.on("data", (chunk: Buffer) => {
      for (const line of splitter.push(chunk.toString("utf8"))) for (const cb of lineCallbacks) cb(line);
    });
  }
  child.on("error", (err) => {
    for (const cb of lineCallbacks) cb(`failed to start: ${err.message}`);
    // A program that could not start never emits "exit": say it ended, or every waiter hangs.
    if (!exited) {
      exited = true;
      for (const cb of exitCallbacks) cb(null, null);
    }
  });
  child.on("exit", (code, signal) => {
    exited = true;
    for (const cb of exitCallbacks) cb(code, signal);
  });
  return {
    onLine: (cb) => void lineCallbacks.push(cb),
    onExit: (cb) => void exitCallbacks.push(cb),
    kill: () =>
      new Promise<void>((resolve) => {
        if (exited) return resolve();
        const timer = setTimeout(() => child.kill("SIGKILL"), KILL_GRACE_MS);
        child.once("exit", () => {
          clearTimeout(timer);
          resolve();
        });
        child.kill("SIGTERM");
      }),
  };
};

/** The few file operations the tunnel code needs, so tests can fake a disk. */
export interface TunnelFiles {
  read(file: string): string | null;
  write(file: string, text: string, mode?: number): void;
  remove(file: string): void;
  exists(file: string): boolean;
}

export const nodeTunnelFiles: TunnelFiles = {
  read: (file) => {
    try {
      return readFileSync(file, "utf8");
    } catch {
      return null;
    }
  },
  write: (file, text, mode = 0o600) => {
    mkdirSync(path.dirname(file), { recursive: true });
    writeFileSync(file, text, { mode });
  },
  remove: (file) => rmSync(file, { force: true }),
  exists: (file) => existsSync(file),
};

export interface RunResult {
  /** Exit code; null when killed by the timeout. */
  code: number | null;
  /** Every line the program printed, in order. */
  lines: string[];
  timedOut: boolean;
}

/**
 * Run a short-lived command to its end through the same spawner the tunnel uses
 * (so it inherits the GUI-safe PATH and the Windows shim handling), collecting
 * its output. Killed after `timeoutMs`.
 */
export function runToEnd(spawner: TunnelSpawner, command: string, args: string[], env: Record<string, string> = {}, timeoutMs = 60_000): Promise<RunResult> {
  return new Promise((resolve) => {
    const lines: string[] = [];
    let settled = false;
    let timedOut = false;
    const proc = spawner(command, args, env);
    const timer = setTimeout(() => {
      timedOut = true;
      void proc.kill();
    }, timeoutMs);
    timer.unref?.();
    proc.onLine((line) => void lines.push(line));
    proc.onExit((code) => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      resolve({ code: timedOut ? null : code, lines, timedOut });
    });
  });
}

/** A provider API answered with a non-2xx status. */
export class HttpStatusError extends Error {
  constructor(readonly status: number) {
    super(`HTTP ${status}`);
    this.name = "HttpStatusError";
  }
}
