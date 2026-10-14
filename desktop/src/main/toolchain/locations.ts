import path from "node:path";
import type { ToolchainPlatform } from "./types.js";

/**
 * Where the installs this app runs put their programs, when that is not a
 * directory every PATH already has. One list, read twice: by detection (so
 * the tool is found) and by the children's environment (so the agents find
 * it too) — a directory known to one and not the other is a tool reported
 * Installed that no agent can run.
 */

export interface Place {
  readonly platform: ToolchainPlatform;
  readonly home: string;
  readonly env: Readonly<NodeJS.ProcessEnv>;
}

const join = (place: Place, ...parts: string[]): string =>
  place.platform === "win32" ? path.win32.join(...parts) : path.posix.join(...parts);

export function localAppData(place: Place): string {
  return place.env.LOCALAPPDATA ?? path.win32.join(place.home, "AppData", "Local");
}

export function programFiles(place: Place): string {
  return place.env.ProgramFiles ?? "C:\\Program Files";
}

/** A per-user program: `~/.local/bin` on macOS and Linux, its own folder under `%LOCALAPPDATA%\Programs` on Windows. */
export function userBinDir(place: Place, name: string): string {
  return place.platform === "win32" ? join(place, localAppData(place), "Programs", name) : join(place, place.home, ".local", "bin");
}

/** winget's portable packages (uv, DuckDB, Godot) are linked here. */
export function wingetLinksDir(place: Place): string {
  return join(place, localAppData(place), "Microsoft", "WinGet", "Links");
}

export function goBinDirs(place: Place): string[] {
  return place.platform === "win32" ? [join(place, programFiles(place), "Go", "bin")] : ["/usr/local/go/bin"];
}

export function dockerBinDirs(place: Place): string[] {
  if (place.platform === "win32") return [join(place, programFiles(place), "Docker", "Docker", "resources", "bin")];
  return place.platform === "darwin" ? ["/Applications/Docker.app/Contents/Resources/bin"] : [];
}

/** DuckDB's own installer keeps versions under `~/.duckdb/cli` with `latest` pointing at one. */
export function duckdbDirs(place: Place): string[] {
  return place.platform === "win32" ? [wingetLinksDir(place)] : [join(place, place.home, ".duckdb", "cli", "latest")];
}

/** Homebrew's openjdk@17 is keg-only: nothing links it onto PATH or registers it with java_home. */
export const OPENJDK_KEGS = ["/opt/homebrew/opt/openjdk@17", "/usr/local/opt/openjdk@17"];

/** A keg's JAVA_HOME: the JDK bundle inside it, not the keg root. */
export function kegJavaHome(keg: string): string {
  return path.posix.join(keg, "libexec", "openjdk.jdk", "Contents", "Home");
}

/** Directories JDK installers create, one JDK per entry, where they are not on PATH. */
export function jdkParents(place: Place): string[] {
  if (place.platform === "win32") {
    return ["Eclipse Adoptium", "Java", "Microsoft", "Zulu"].map((vendor) => join(place, programFiles(place), vendor));
  }
  return place.platform === "linux" ? ["/usr/lib/jvm"] : [];
}

/** Every per-user or vendor directory above that holds a program the agents may call. */
export function toolDirs(place: Place): string[] {
  const dirs = [
    userBinDir(place, "osv-scanner"),
    ...duckdbDirs(place),
    ...goBinDirs(place),
    ...dockerBinDirs(place),
  ];
  if (place.platform === "win32") dirs.push(wingetLinksDir(place), join(place, place.home, ".local", "bin"));
  return [...new Set(dirs)];
}
