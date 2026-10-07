import { spawn } from "node:child_process";
import os from "node:os";
import path from "node:path";
import { withoutAppImage } from "./process-env.js";

/**
 * The PATH the user's terminal has, recovered once for a GUI launch.
 *
 * A double-clicked app inherits launchd's or the desktop session's PATH, not a
 * login shell's, so nvm, fnm, volta, asdf, mise, pnpm and `~/.local/bin` are
 * all absent — and a CLI whose shebang is `#!/usr/bin/env node` fails with
 * "env: node: No such file or directory", which every probe read as "missing".
 * Asking the login shell is the only source that knows what the user's profile
 * does; guessing directories (detect.ts still does, as a fallback) cannot keep
 * up with every version manager.
 *
 * Only PATH is taken. A profile exports tokens and keys as often as anything
 * else, and none of those belong in this app's environment or its children's.
 *
 * Never on Windows, where GUI apps get the user's PATH from the registry.
 */

export const LOGIN_SHELL_TIMEOUT_MS = 5_000;

const BEGIN = "__TASKTROOPER_ENV_BEGIN__";
const END = "__TASKTROOPER_ENV_END__";

/** `env -0` where it exists (GNU, BSD, macOS), plain `env` otherwise; the parser accepts either. */
const SCRIPT = `printf '%s' '${BEGIN}'; command env -0 2>/dev/null || command env; printf '%s' '${END}'`;

/**
 * The argv that makes `shell` a login shell running the env dump, or null for a
 * shell whose flags are not known to mean that.
 *
 * Interactive as well as login for the POSIX family, because nvm, fnm and most
 * version managers are initialised from `.bashrc`/`.zshrc`, which a
 * non-interactive login shell does not read. fish reads `config.fish` either
 * way and wants the flags separately.
 */
export function loginShellArgs(shell: string): string[] | null {
  const name = path.basename(shell).toLowerCase();
  if (name === "fish") return ["-l", "-c", SCRIPT];
  if (["bash", "zsh", "sh", "dash", "ksh", "mksh", "yash"].includes(name)) return ["-i", "-l", "-c", SCRIPT];
  return null;
}

/**
 * The variables between the two markers, or null when the markers are not
 * there. Anything a profile prints around them — a banner, a theme's instant
 * prompt — is outside the markers and ignored.
 */
export function parseLoginEnv(stdout: string): Record<string, string> | null {
  const start = stdout.indexOf(BEGIN);
  if (start < 0) return null;
  const end = stdout.indexOf(END, start + BEGIN.length);
  if (end < 0) return null;
  const body = stdout.slice(start + BEGIN.length, end);
  const records = body.includes("\0") ? body.split("\0") : body.split("\n");
  const out: Record<string, string> = {};
  for (const record of records) {
    const eq = record.indexOf("=");
    if (eq <= 0) continue;
    const key = record.slice(0, eq);
    // A plain `env` prints a multi-line value (a bash exported function) as
    // several lines; only a line that starts like a variable is one.
    if (!/^[A-Za-z_][A-Za-z0-9_]*$/.test(key)) continue;
    out[key] = record.slice(eq + 1);
  }
  return out;
}

/** PATH entries worth keeping: absolute, so `.` or an empty entry cannot make the cwd a search directory. */
export function loginPathEntries(env: Record<string, string> | null): string[] {
  return (env?.PATH ?? "").split(":").filter((entry) => entry.startsWith("/"));
}

let pending: Promise<string[]> | null = null;
let known: string[] = [];
let settled = false;
let seeded = false;

/**
 * The login shell's PATH entries, asked for once per process. Resolves to an
 * empty list on Windows, on an unknown shell, on a failure and on a timeout —
 * this is an improvement on the fallback directories, never a requirement.
 */
export function loginShellPath(): Promise<string[]> {
  if (process.platform === "win32") return Promise.resolve([]);
  pending ??= readLoginShellPath().then(
    (entries) => {
      settled = true;
      // Empty is a shell that failed or timed out: a seeded PATH from an
      // earlier launch is the better answer for this one too.
      if (entries.length > 0 || !seeded) known = entries;
      return known;
    },
    () => {
      settled = true;
      if (!seeded) known = [];
      return known;
    },
  );
  return pending;
}

/**
 * What `loginShellPath()` has resolved to so far — or, until it has, the
 * PATH `seedLoginShellPath()` was given. Empty when there is neither.
 */
export function knownLoginShellPath(): string[] {
  return known;
}

/**
 * Use a PATH an earlier launch recorded until this launch's own login shell
 * answers, which then replaces it. A login shell costs a few hundred
 * milliseconds on every launch and its answer almost never changes between
 * two of them, so the backend's start should not wait to find that out.
 */
export function seedLoginShellPath(entries: string[]): void {
  if (settled) return;
  known = entries;
  seeded = true;
}

/** True once there is a PATH to use: this launch's own answer, or a seeded one. */
export function loginShellPathKnown(): boolean {
  return settled || seeded || process.platform === "win32";
}

function userShell(): string {
  const fromEnv = process.env.SHELL ?? "";
  if (fromEnv.startsWith("/")) return fromEnv;
  try {
    const shell = os.userInfo().shell ?? "";
    return shell.startsWith("/") ? shell : "";
  } catch {
    return "";
  }
}

function readLoginShellPath(): Promise<string[]> {
  const shell = userShell();
  const args = shell === "" ? null : loginShellArgs(shell);
  if (!args) return Promise.resolve([]);

  const env = withoutAppImage({ ...process.env });
  delete env.ELECTRON_RUN_AS_NODE;
  delete env.NODE_OPTIONS;

  return new Promise((resolve) => {
    let stdout = "";
    let done = false;
    const finish = (entries: string[]): void => {
      if (done) return;
      done = true;
      clearTimeout(timer);
      resolve(entries);
    };

    let child: ReturnType<typeof spawn>;
    try {
      // Its own process group, so a profile that starts something in the
      // background (an agent, a tmux attach that hangs) is killed with it.
      child = spawn(shell, args, { env, stdio: ["ignore", "pipe", "ignore"], detached: true });
    } catch {
      resolve([]);
      return;
    }

    const timer = setTimeout(() => {
      if (child.pid !== undefined) {
        try {
          process.kill(-child.pid, "SIGKILL");
        } catch {
          child.kill("SIGKILL");
        }
      }
      finish(loginPathEntries(parseLoginEnv(stdout)));
    }, LOGIN_SHELL_TIMEOUT_MS);
    timer.unref?.();

    // Resolved at the end marker rather than on close: close waits for every
    // process holding stdout, and a profile that backgrounds something
    // without detaching it would cost the full timeout on every launch.
    child.stdout?.on("data", (chunk: Buffer) => {
      if (stdout.length < 1024 * 1024) stdout += chunk.toString("utf8");
      if (stdout.includes(END)) finish(loginPathEntries(parseLoginEnv(stdout)));
    });
    child.once("error", () => finish([]));
    child.once("close", () => finish(loginPathEntries(parseLoginEnv(stdout))));
  });
}
