import path from "node:path";
import type { ToolchainId, ToolchainReport } from "../../ipc/types.js";
import { OPENJDK_KEGS, jdkParents, kegJavaHome, toolDirs, userDotnetRoot, type Place } from "./locations.js";

/**
 * What the agents' processes need so that a tool reported Installed is one
 * they can run: the directories those tools live in, JAVA_HOME, and
 * DOTNET_ROOT for a per-user .NET SDK.
 *
 * The backend (local mode) and the runner with its executor (account mode)
 * are started with PATH built from the preflight, which knows a handful of
 * CLIs. A JDK in a Homebrew keg, OSV-Scanner in `%LOCALAPPDATA%\Programs`, a
 * DuckDB under `~/.duckdb` are on nobody's PATH — so both environments get
 * this appended. Appended, never prepended, for the reason `supervisor/env.ts`
 * gives: the person's own order still decides which of two copies wins.
 *
 * Two sources, merged: what the last toolchain report found (exact, but only
 * once a sweep has run in this process), and the install locations
 * themselves when they exist (available from the first launch).
 */

export interface ToolchainRuntime {
  /** Appended to PATH in this order. */
  pathDirs: string[];
  javaHome?: string;
  /**
   * `~/.dotnet` when the SDK lives there: `dotnet` itself finds its SDKs from
   * its own path, but a tool it installs (`dotnet-ef`) is an apphost that
   * looks only in the global locations unless this is set.
   */
  dotnetRoot?: string;
}

export const NO_RUNTIME: ToolchainRuntime = { pathDirs: [] };

/** How a tool reaches the agents: its program's directory on PATH, or (the JDK) that and JAVA_HOME. */
export type RuntimeExposure = "bin-dir" | "java-home";

export function runtimeFromReport(
  report: ToolchainReport | null,
  exposure: (id: ToolchainId) => RuntimeExposure | undefined,
  javaHomeOf: (javaPath: string) => string,
): ToolchainRuntime {
  if (!report) return NO_RUNTIME;
  const p = report.platform === "win32" ? path.win32 : path.posix;
  const out: ToolchainRuntime = { pathDirs: [] };
  for (const item of report.items) {
    const how = exposure(item.id);
    if (!how || item.status !== "installed" || !item.path || !p.isAbsolute(item.path)) continue;
    // An app bundle is how a GUI install was recognised, not a directory to run from.
    if (/\.app$/i.test(item.path)) continue;
    const dir = p.dirname(item.path);
    if (!out.pathDirs.includes(dir)) out.pathDirs.push(dir);
    if (how === "java-home") out.javaHome = javaHomeOf(item.path);
  }
  return out;
}

export interface Disk {
  isDir(dir: string): boolean;
  list(dir: string): string[];
}

/** The install locations on this machine that exist now — read at every spawn, so never stale. */
export function installedRuntime(place: Place, disk: Disk): ToolchainRuntime {
  const out: ToolchainRuntime = { pathDirs: toolDirs(place).filter((dir) => disk.isDir(dir)) };
  if (place.platform === "darwin") {
    for (const keg of OPENJDK_KEGS) {
      if (!disk.isDir(keg)) continue;
      out.pathDirs.push(path.posix.join(keg, "bin"));
      const home = kegJavaHome(keg);
      if (out.javaHome === undefined && disk.isDir(home)) out.javaHome = home;
    }
    return out;
  }
  const p = place.platform === "win32" ? path.win32 : path.posix;
  if (place.platform === "linux") {
    const root = userDotnetRoot(place);
    if (disk.isDir(p.join(root, "sdk"))) {
      out.pathDirs.push(root);
      out.dotnetRoot = root;
    }
  }
  for (const parent of jdkParents(place)) {
    const jdk17 = disk
      .list(parent)
      .filter((name) => /(^|[-_.])(jdk-?)?17([-_.]|$)|java-17|temurin-17/i.test(name))
      .sort()
      .reverse()[0];
    if (jdk17 === undefined) continue;
    const home = p.join(parent, jdk17);
    out.javaHome ??= home;
    out.pathDirs.push(p.join(home, "bin"));
  }
  return out;
}

export function mergeRuntime(first: ToolchainRuntime, second: ToolchainRuntime): ToolchainRuntime {
  const javaHome = first.javaHome ?? second.javaHome;
  const dotnetRoot = first.dotnetRoot ?? second.dotnetRoot;
  return {
    pathDirs: [...new Set([...first.pathDirs, ...second.pathDirs])],
    ...(javaHome !== undefined ? { javaHome } : {}),
    ...(dotnetRoot !== undefined ? { dotnetRoot } : {}),
  };
}

/**
 * `env` with the runtime applied: PATH gains the directories it does not
 * already have, at the end; JAVA_HOME and DOTNET_ROOT are set only where
 * none was — one the person exported is theirs to keep.
 */
export function withRuntime(
  env: NodeJS.ProcessEnv,
  runtime: ToolchainRuntime,
  platform: NodeJS.Platform = process.platform,
): NodeJS.ProcessEnv {
  const win = platform === "win32";
  const out: NodeJS.ProcessEnv = { ...env };
  const key = Object.keys(out).find((k) => (win ? k.toUpperCase() === "PATH" : k === "PATH")) ?? "PATH";
  const delimiter = win ? ";" : ":";
  const parts = (out[key] ?? "").split(delimiter).filter((part) => part !== "");
  const same = (a: string, b: string): boolean => (win ? a.toLowerCase() === b.toLowerCase() : a === b);
  for (const dir of runtime.pathDirs) if (!parts.some((part) => same(part, dir))) parts.push(dir);
  out[key] = parts.join(delimiter);
  const unset = (name: string): boolean => {
    const found = Object.keys(out).find((k) => (win ? k.toUpperCase() === name : k === name));
    return found === undefined || (out[found] ?? "").trim() === "";
  };
  if (runtime.javaHome !== undefined && unset("JAVA_HOME")) out.JAVA_HOME = runtime.javaHome;
  if (runtime.dotnetRoot !== undefined && unset("DOTNET_ROOT")) out.DOTNET_ROOT = runtime.dotnetRoot;
  return out;
}
