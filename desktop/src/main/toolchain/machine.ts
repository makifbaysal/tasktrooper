import { existsSync, lstatSync, readdirSync, statSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import { isExecutable, runProbe, searchDirs, which, whichAll } from "../services/detect.js";
import { fetchFile, type FetchLike } from "./fetch-file.js";
import type { StepMachine } from "./job.js";
import { execLogged } from "./process.js";
import type { HostFacts, ProbeKit, ToolchainPlatform } from "./types.js";

/**
 * The real machine behind `ProbeKit`, `HostFacts` and `StepMachine`. Detectors
 * and the recipe factories take those as arguments rather than looking at the
 * machine themselves, which is what lets the table be checked for every OS on
 * any one.
 */

export function currentPlatform(): ToolchainPlatform {
  return process.platform === "win32" || process.platform === "linux" ? process.platform : "darwin";
}

function list(dir: string): string[] {
  try {
    return readdirSync(dir);
  } catch {
    return [];
  }
}

function isDir(dir: string): boolean {
  try {
    return statSync(dir).isDirectory();
  } catch {
    return false;
  }
}

/**
 * One kit per sweep: the search directories (which read the login PATH and
 * list the nvm versions) are computed once, and `xcode-select -p` is asked
 * once however many detectors want it.
 */
export function createProbeKit(): ProbeKit {
  const dirs = searchDirs();
  const platform = currentPlatform();
  let developerDir: Promise<string | null> | undefined;
  const withExtra = (extraDirs: readonly string[]) =>
    extraDirs.length === 0 ? dirs : [...dirs, ...extraDirs.map((dir) => ({ dir, source: "home" as const }))];
  return {
    platform,
    arch: process.arch,
    home: os.homedir(),
    env: process.env,
    which: (name, extraDirs = []) => which(name, withExtra(extraDirs))?.path ?? null,
    whichAll: (name, extraDirs = []) => whichAll(name, withExtra(extraDirs)).map((found) => found.path),
    isExecutable,
    exists: existsSync,
    list,
    run: async (command, args) => {
      const answer = await runProbe(command, [...args]);
      return { code: answer.code, out: answer.out, timedOut: answer.timedOut };
    },
    developerDir: () => {
      if (platform !== "darwin") return Promise.resolve(null);
      developerDir ??= runProbe("/usr/bin/xcode-select", ["-p"]).then((answer) => {
        const dir = answer.stdout.split("\n")[0]?.trim() ?? "";
        return answer.code === 0 && dir.startsWith("/") && isDir(dir) ? dir : null;
      });
      return developerDir;
    },
  };
}

/**
 * winget is an App Execution Alias under WindowsApps: a reparse point that
 * `stat` cannot always follow, so the PATH search can miss it on a machine
 * where it runs fine. `lstat` sees the alias itself, and CreateProcess
 * resolves it.
 */
function appExecutionAlias(name: string): string | null {
  if (process.platform !== "win32" || name.includes("\\") || name.includes("/")) return null;
  const local = process.env.LOCALAPPDATA ?? path.join(os.homedir(), "AppData", "Local");
  const alias = path.join(local, "Microsoft", "WindowsApps", `${name}.exe`);
  try {
    lstatSync(alias);
    return alias;
  } catch {
    return null;
  }
}

/** Looked up afresh on every call: an earlier step may just have installed what this one needs. */
export function createHostFacts(): HostFacts {
  return {
    platform: currentPlatform(),
    arch: process.arch,
    home: os.homedir(),
    env: process.env,
    which: (name) => which(name)?.path ?? appExecutionAlias(name),
  };
}

export function createStepMachine(workDir: string, fetch?: FetchLike): StepMachine {
  return {
    workDir,
    host: createHostFacts,
    exec: (command, args, opts) => execLogged(command, args, opts),
    download: (url, dest, opts) => fetchFile(url, dest, { ...opts, ...(fetch ? { fetch } : {}) }),
  };
}
