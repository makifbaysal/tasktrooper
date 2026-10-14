import { realpathSync } from "node:fs";
import path from "node:path";
import type { PreflightItem } from "../../ipc/types.js";
import { compareVersions, parseVersion } from "../services/detect.js";
import { OPENJDK_KEGS, jdkParents, localAppData, wingetLinksDir } from "./locations.js";
import type { Detection, Detector, ProbeKit, ToolchainPlatform } from "./types.js";

/**
 * Detector building blocks. A tool's detection is assembled from these in the
 * registry — a binary on the search path, an app bundle, the first of several
 * that answers — so the rules every tool shares (Apple's placeholders, the
 * Microsoft Store's python stub, an old version) are written once.
 */

const firstLine = (text: string): string => text.split("\n")[0]?.trim() ?? "";

const missing = (detail: string): Detection => ({ status: "missing", detail });

/** Enough copies to get past a version manager's shims, few enough that a sweep stays bounded. */
const MAX_CANDIDATES = 6;

export interface BinaryProbe {
  /** Searched in order, every copy of each, until one answers. */
  names: readonly string[];
  /** How a missing one is named in its detail; the first name by default. */
  what?: string;
  /** How to ask its version. Absent: presence is the answer — for tools whose first run is slow or does work. */
  versionArgs?: readonly string[];
  /** Searched after the preflight's own directories. */
  extraDirs?: (kit: ProbeKit) => string[];
  /** Older than this is `unusable`: it is here, it just cannot be what the agents need. */
  minVersion?: string;
  /**
   * macOS ships `/usr/bin/git`, `/usr/bin/python3`, … as placeholders that
   * open the "install command line tools?" dialog when run. Without the tools
   * they are not the program, and asking one its version would put that
   * dialog in front of the person from a background sweep.
   */
  appleShim?: boolean;
  /** Paths that only look like the tool (the Microsoft Store's python alias). */
  rejectPath?: RegExp;
}

/**
 * The first copy that answers is the one reported. The first one FOUND is not
 * enough: an asdf or pyenv shim with no version selected sits ahead of a
 * working python3 on PATH and answers "No version is set", and Homebrew's
 * copy installed after it would still look missing. When none answers, the
 * first one's failure is what is said.
 */
export function binary(probe: BinaryProbe): Detector {
  return async (kit) => {
    const extra = probe.extraDirs?.(kit) ?? [];
    const candidates: string[] = [];
    for (const name of probe.names) {
      for (const found of kit.whichAll(name, extra)) {
        if (!probe.rejectPath?.test(found) && !candidates.includes(found)) candidates.push(found);
      }
    }
    const shown = probe.what ?? probe.names[0] ?? "it";
    if (candidates.length === 0) return missing(`${shown} was not found on this computer.`);

    let fallback: Detection | undefined;
    let older: Detection | undefined;
    for (const found of candidates.slice(0, MAX_CANDIDATES)) {
      if (kit.platform === "darwin" && probe.appleShim && path.posix.dirname(found) === "/usr/bin" && (await kit.developerDir()) === null) {
        fallback ??= missing(`Only macOS's placeholder ${found} is here; it needs the Xcode command line tools.`);
        continue;
      }
      if (!probe.versionArgs) return { status: "installed", path: found };

      const asked = `${path.basename(found)} ${probe.versionArgs.join(" ")}`;
      const answer = await kit.run(found, probe.versionArgs);
      if (answer.code !== 0) {
        const said = firstLine(answer.out);
        fallback ??= {
          status: "unusable",
          path: found,
          detail: answer.timedOut
            ? `\`${asked}\` did not answer in time.`
            : said === ""
              ? `\`${asked}\` failed.`
              : `\`${asked}\` said: ${said}`,
        };
        continue;
      }
      const version = parseVersion(answer.out) ?? undefined;
      if (probe.minVersion && version && compareVersions(version, probe.minVersion) < 0) {
        older ??= {
          status: "unusable",
          path: found,
          version,
          detail: `Version ${version} is older than ${probe.minVersion}, the oldest the agents can use.`,
        };
        continue;
      }
      return { status: "installed", path: found, ...(version ? { version } : {}) };
    }
    return older ?? fallback ?? missing(`${shown} was not found on this computer.`);
  };
}

/** The first candidate that exists (an `.app`, an installed `.exe`). */
export function file(candidates: (kit: ProbeKit) => string[], what: string): Detector {
  return async (kit) => {
    const found = candidates(kit).find((candidate) => kit.exists(candidate));
    return found ? { status: "installed", path: found } : missing(`${what} was not found on this computer.`);
  };
}

/** Entries of `dir` whose names match, as full paths — `/Applications/Xcode-beta.app`, `jdk-17.0.12-hotspot`. */
export function matching(kit: ProbeKit, dir: string, pattern: RegExp): string[] {
  return kit
    .list(dir)
    .filter((name) => pattern.test(name))
    .sort()
    .reverse()
    .map((name) => path.join(dir, name));
}

/**
 * Composite: the first detector that finds it installed wins; otherwise a
 * found-but-unusable answer beats a missing one, and the first missing one
 * (the primary place) speaks for the rest.
 */
export function anyOf(...detectors: Detector[]): Detector {
  return async (kit) => {
    let unusable: Detection | undefined;
    let first: Detection | undefined;
    for (const detect of detectors) {
      const result = await detect(kit);
      if (result.status === "installed") return result;
      if (result.status === "unusable") unusable ??= result;
      first ??= result;
    }
    return unusable ?? first ?? missing("Not found on this computer.");
  };
}

export function byPlatform(detectors: Partial<Record<ToolchainPlatform, Detector>>): Detector {
  return async (kit) => {
    const detect = detectors[kit.platform];
    return detect ? detect(kit) : missing("Not used on this operating system.");
  };
}

/** A preflight row as a toolchain answer: the two lists agree about Chrome and the Android SDK by asking the same probe. */
export function fromPreflight(item: PreflightItem): Detection {
  const status = item.status === "ok" ? "installed" : item.status;
  return {
    status,
    ...(item.path !== undefined ? { path: item.path } : {}),
    ...(item.version !== undefined ? { version: item.version } : {}),
    ...(item.detail !== undefined && status !== "installed" ? { detail: item.detail } : {}),
  };
}

// --- Apple's own -------------------------------------------------------------

export const xcodeCommandLineTools: Detector = async (kit) => {
  const dir = await kit.developerDir();
  return dir === null
    ? missing("`xcode-select -p` names no developer directory, so the command line tools are not installed.")
    : { status: "installed", path: dir };
};

/** Full Xcode: an app bundle in /Applications, or the selected developer directory living inside one. */
export const xcode: Detector = async (kit) => {
  const bundle = matching(kit, "/Applications", /^Xcode.*\.app$/)[0];
  if (bundle) return { status: "installed", path: bundle };
  const dir = await kit.developerDir();
  const inApp = dir === null ? undefined : /^(.*?\.app)\/Contents\/Developer/.exec(dir)?.[1];
  return inApp ? { status: "installed", path: inApp } : missing("No Xcode app is in /Applications.");
};

// --- Java --------------------------------------------------------------------

export const MIN_JAVA_MAJOR = 17;

/** `openjdk version "17.0.12" 2024-07-16` → 17; the old `"1.8.0_402"` spelling → 8. */
export function javaMajor(versionOutput: string): number | null {
  const m = /version "(\d+)(?:\.(\d+))?/.exec(versionOutput);
  if (!m) return null;
  const first = Number.parseInt(m[1] ?? "", 10);
  return first === 1 ? Number.parseInt(m[2] ?? "", 10) : first;
}

/** JAVA_HOME for a `java` binary: `<home>/bin/java`, after the symlinks Homebrew and alternatives add. */
export function javaHomeOf(javaPath: string): string {
  let real = javaPath;
  try {
    real = realpathSync(javaPath);
  } catch {
    // A path that will not resolve is used as it is; sdkmanager reports it.
  }
  return path.dirname(path.dirname(real));
}

function javaCandidates(kit: ProbeKit, javaHomeAnswer: string | null): string[] {
  const exe = kit.platform === "win32" ? "java.exe" : "java";
  const out: string[] = [];
  const javaHome = (kit.env.JAVA_HOME ?? "").trim();
  if (javaHome !== "") out.push(path.join(javaHome, "bin", exe));
  if (kit.platform === "darwin") {
    for (const keg of [...OPENJDK_KEGS, "/opt/homebrew/opt/openjdk", "/usr/local/opt/openjdk"]) out.push(`${keg}/bin/java`);
    if (javaHomeAnswer) out.push(path.join(javaHomeAnswer, "bin", "java"));
  }
  for (const parent of jdkParents(kit)) {
    for (const dir of matching(kit, parent, /^(java|jdk|temurin|openjdk|zulu)/i)) out.push(path.join(dir, "bin", exe));
  }
  // /usr/bin/java on a Mac without a JDK opens a "you need a JDK" dialog when run.
  for (const onPath of kit.whichAll("java")) {
    if (!(kit.platform === "darwin" && onPath === "/usr/bin/java")) out.push(onPath);
  }
  return [...new Set(out)];
}

export const jdk: Detector = async (kit) => {
  let javaHomeAnswer: string | null = null;
  if (kit.platform === "darwin" && kit.isExecutable("/usr/libexec/java_home")) {
    const answer = await kit.run("/usr/libexec/java_home", ["-v", `${MIN_JAVA_MAJOR}+`]);
    const home = firstLine(answer.out);
    if (answer.code === 0 && home.startsWith("/")) javaHomeAnswer = home;
  }
  let older: Detection | undefined;
  for (const candidate of javaCandidates(kit, javaHomeAnswer)) {
    if (!kit.isExecutable(candidate)) continue;
    const answer = await kit.run(candidate, ["-version"]);
    const major = javaMajor(answer.out);
    if (answer.code !== 0 || major === null) continue;
    const version = /version "([^"]+)"/.exec(answer.out)?.[1] ?? String(major);
    if (major >= MIN_JAVA_MAJOR) return { status: "installed", path: candidate, version };
    older ??= {
      status: "unusable",
      path: candidate,
      version,
      detail: `Java ${major} is older than ${MIN_JAVA_MAJOR}, which the Android tools need.`,
    };
  }
  return older ?? missing(`No Java ${MIN_JAVA_MAJOR} or newer was found.`);
};

// --- Appium drivers ----------------------------------------------------------

/**
 * `appium driver list --installed` writes its answer to stderr — `out` is both
 * streams, which is why it is what is read (see `services/detect.ts`).
 */
export function appiumDriver(driver: string): Detector {
  return async (kit) => {
    const appium = kit.which("appium");
    if (!appium) return missing("Appium is not installed, so it has no drivers.");
    const answer = await kit.run(appium, ["driver", "list", "--installed"]);
    if (answer.code === 0 && answer.out.includes(driver)) return { status: "installed", path: appium };
    return missing(`Appium is installed and its ${driver} driver is not.`);
  };
}

/** winget's portable packages are linked here; a GUI app's PATH predates the first one. */
export function wingetLinks(kit: ProbeKit): string[] {
  return kit.platform === "win32" ? [wingetLinksDir(kit)] : [];
}

export function pythonInstallDirs(kit: ProbeKit): string[] {
  if (kit.platform !== "win32") return [];
  return [
    ...matching(kit, path.win32.join(localAppData(kit), "Programs", "Python"), /^Python3\d+$/),
    ...matching(kit, kit.env.ProgramFiles ?? "C:\\Program Files", /^Python3\d+$/),
  ];
}
