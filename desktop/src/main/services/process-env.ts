import path from "node:path";

/**
 * PATH and environment surgery shared by the probes (`detect.ts`) and the
 * children (`supervisor/env.ts`). Pure: every function takes the environment it
 * works on, so the rules are asserted on literal inputs rather than on whatever
 * machine the suite runs on.
 */

/** The key PATH is stored under. Windows spells it `Path`, and a spread of process.env loses the case-insensitive lookup. */
export function pathKeyOf(env: NodeJS.ProcessEnv): string {
  return Object.keys(env).find((k) => k.toUpperCase() === "PATH") ?? "PATH";
}

export function splitPath(value: string | undefined, delimiter: string = path.delimiter): string[] {
  return (value ?? "").split(delimiter).filter((p) => p !== "");
}

const sameDir = (platform: NodeJS.Platform) => (a: string, b: string) =>
  platform === "win32" ? a.toLowerCase() === b.toLowerCase() : a === b;

/**
 * The first list in its own order, then every entry of the later lists it does
 * not already have. Appending rather than interleaving is deliberate: whatever
 * resolved first before still resolves first, and the later lists only make
 * findable what was not.
 */
export function mergePath(lists: string[][], platform: NodeJS.Platform = process.platform): string[] {
  const same = sameDir(platform);
  const out: string[] = [];
  for (const list of lists) {
    for (const entry of list) {
      if (entry === "" || out.some((e) => same(e, entry))) continue;
      out.push(entry);
    }
  }
  return out;
}

/**
 * Directories every PATH already has, whose order a CLI's own directory must
 * not disturb: moving `/usr/bin` in front of `/usr/local/bin` because git lives
 * there would hand every child the distribution's python and node instead of
 * the user's.
 */
const SYSTEM_DIRS = new Set(["/usr/bin", "/bin", "/usr/sbin", "/sbin"]);

/**
 * Put each directory at the front, in the order given, moving it if it was
 * already present. A Node CLI's shebang is `#!/usr/bin/env node`, and the node
 * it was installed with lives beside it (nvm, fnm, volta, Homebrew, an npm
 * prefix) — so the directory the CLI was found in has to win the lookup.
 */
export function prependDirs(parts: string[], dirs: string[], platform: NodeJS.Platform = process.platform): string[] {
  const same = sameDir(platform);
  const front: string[] = [];
  for (const dir of dirs) {
    if (dir === "" || SYSTEM_DIRS.has(dir) || front.some((d) => same(d, dir))) continue;
    if (platform !== "win32" && !dir.startsWith("/")) continue;
    front.push(dir);
  }
  return [...front, ...parts.filter((p) => !front.some((d) => same(d, p)))];
}

/** The directories of the binaries detection found, for `prependDirs`. */
export function binaryDirs(paths: (string | undefined)[], platform: NodeJS.Platform = process.platform): string[] {
  const p = platform === "win32" ? path.win32 : path.posix;
  return paths.filter((x): x is string => !!x && p.isAbsolute(x)).map((x) => p.dirname(x));
}

/** Variables an AppImage's AppRun sets for Electron's own process and nobody else's. */
const APPIMAGE_ONLY = ["APPDIR", "APPIMAGE", "ARGV0", "OWD", "APPIMAGE_UUID"];

/**
 * The environment without what an AppImage's AppRun added to it.
 *
 * AppRun prepends `$APPDIR` to PATH, `$APPDIR/usr/lib` to LD_LIBRARY_PATH,
 * `$APPDIR/usr/share` to XDG_DATA_DIRS and sets GSETTINGS_SCHEMA_DIR — all for
 * Electron. Inherited by git, claude, Postgres or Chrome they load this app's
 * bundled libraries instead of the system's, which fails as a symbol error in
 * somebody else's program. A no-op outside an AppImage. Not for a child that
 * IS this app's own binary (the embedder): that one needs the bundled libraries.
 */
export function withoutAppImage(env: NodeJS.ProcessEnv): NodeJS.ProcessEnv {
  const appdir = env.APPDIR;
  if (!env.APPIMAGE || !appdir) return env;
  const out: NodeJS.ProcessEnv = { ...env };
  const inside = (entry: string): boolean => entry === appdir || entry.startsWith(`${appdir.replace(/\/+$/, "")}/`);

  for (const key of ["PATH", "LD_LIBRARY_PATH", "XDG_DATA_DIRS", "GSETTINGS_SCHEMA_DIR"]) {
    const actual = key === "PATH" ? pathKeyOf(out) : key;
    const value = out[actual];
    if (value === undefined) continue;
    const kept = value.split(":").filter((entry) => entry !== "" && !inside(entry));
    if (kept.length === 0 && key !== "PATH") delete out[actual];
    else out[actual] = kept.join(":");
  }
  for (const key of APPIMAGE_ONLY) delete out[key];
  return out;
}
