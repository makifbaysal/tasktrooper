import { spawn } from "node:child_process";
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
