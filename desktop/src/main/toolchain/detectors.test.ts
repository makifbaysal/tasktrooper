import { describe, expect, it, vi } from "vitest";
import type { ProbeKit, ProbeResult, ToolchainPlatform } from "./types.js";

vi.mock("electron", () => ({ app: { getAppPath: () => "/app", getPath: () => "/tmp/tt-detectors", isPackaged: false } }));

const { anyOf, appiumDriver, binary, jdk, javaMajor, xcode } = await import("./detectors.js");

/**
 * Detection rules on a fake machine: which paths exist, what each program
 * answers, and whether macOS's command line tools are there. Every rule that
 * is about a platform is checked on that platform without running there.
 */
interface FakeMachine {
  platform?: ToolchainPlatform;
  /** Executables and their answers to any arguments. */
  programs?: Record<string, ProbeResult | ((args: readonly string[]) => ProbeResult)>;
  /** `which(name)` answers. */
  onPath?: Record<string, string>;
  /** Every copy on the search path, when there is more than one. */
  copies?: Record<string, string[]>;
  dirs?: Record<string, string[]>;
  files?: string[];
  developerDir?: string | null;
  env?: NodeJS.ProcessEnv;
}

function kit(m: FakeMachine): ProbeKit & { ran: string[] } {
  const ran: string[] = [];
  const all = (name: string, extra: readonly string[]): string[] => {
    if (name.startsWith("/")) return m.programs?.[name] ? [name] : [];
    const onPath = m.copies?.[name] ?? (m.onPath?.[name] ? [m.onPath[name]] : []);
    return [...onPath, ...extra.map((dir) => `${dir}/${name}`).filter((p) => m.programs?.[p] !== undefined)];
  };
  return {
    ran,
    platform: m.platform ?? "darwin",
    arch: "arm64",
    home: "/Users/me",
    env: m.env ?? {},
    which: (name, extra = []) => all(name, extra)[0] ?? null,
    whichAll: (name, extra = []) => all(name, extra),
    isExecutable: (file) => m.programs?.[file] !== undefined,
    exists: (file) => (m.files ?? []).includes(file) || m.programs?.[file] !== undefined,
    list: (dir) => m.dirs?.[dir] ?? [],
    run: (command, args) => {
      ran.push([command, ...args].join(" "));
      const answer = m.programs?.[command];
      if (answer === undefined) return Promise.resolve({ code: 127, out: "not found", timedOut: false });
      return Promise.resolve(typeof answer === "function" ? answer(args) : answer);
    },
    developerDir: () => Promise.resolve(m.developerDir ?? null),
  };
}

const ok = (out: string): ProbeResult => ({ code: 0, out, timedOut: false });

describe("binary", () => {
  it("reports the version a found program answers with", async () => {
    const k = kit({ onPath: { node: "/opt/homebrew/bin/node" }, programs: { "/opt/homebrew/bin/node": ok("v22.11.0") } });
    await expect(binary({ names: ["node"], versionArgs: ["--version"] })(k)).resolves.toEqual({
      status: "installed",
      path: "/opt/homebrew/bin/node",
      version: "22.11.0",
    });
  });

  it("does not run macOS's /usr/bin placeholder when the command line tools are missing — that opens a dialog", async () => {
    const k = kit({ onPath: { git: "/usr/bin/git" }, programs: { "/usr/bin/git": ok("git version 2.39.5") }, developerDir: null });
    const found = await binary({ names: ["git"], versionArgs: ["--version"], appleShim: true })(k);
    expect(found.status).toBe("missing");
    expect(found.detail).toContain("placeholder");
    expect(k.ran).toEqual([]);

    const withTools = kit({ onPath: { git: "/usr/bin/git" }, programs: { "/usr/bin/git": ok("git version 2.39.5") }, developerDir: "/Library/Developer/CommandLineTools" });
    await expect(binary({ names: ["git"], versionArgs: ["--version"], appleShim: true })(withTools)).resolves.toMatchObject({
      status: "installed",
      version: "2.39.5",
    });
  });

  it("skips a path that only looks like the tool, and finds the real one in the extra directories", async () => {
    const store = "C:\\Users\\me\\AppData\\Local\\Microsoft\\WindowsApps\\python.exe";
    const k = kit({
      platform: "win32",
      onPath: { python: store },
      programs: { "C:\\Py312/py": ok("Python 3.12.7") },
    });
    const found = await binary({ names: ["python", "py"], versionArgs: ["--version"], rejectPath: /\\WindowsApps\\/i, extraDirs: () => ["C:\\Py312"] })(k);
    expect(found).toMatchObject({ status: "installed", version: "3.12.7" });
  });

  it("is unusable when the program will not answer, or is older than the floor", async () => {
    const broken = kit({ onPath: { claude: "/x/claude" }, programs: { "/x/claude": { code: 127, out: "env: node: No such file or directory", timedOut: false } } });
    await expect(binary({ names: ["claude"], versionArgs: ["--version"] })(broken)).resolves.toMatchObject({
      status: "unusable",
      detail: "`claude --version` said: env: node: No such file or directory",
    });
    const old = kit({ onPath: { claude: "/x/claude" }, programs: { "/x/claude": ok("1.0.3 (Claude Code)") } });
    await expect(binary({ names: ["claude"], versionArgs: ["--version"], minVersion: "2.0.0" })(old)).resolves.toMatchObject({
      status: "unusable",
      version: "1.0.3",
    });
  });

  it("reports the first copy that answers, past a version manager's shim that has no version selected", async () => {
    const shim = "/Users/me/.asdf/shims/python3";
    const brew = "/opt/homebrew/bin/python3";
    const noVersion: ProbeResult = { code: 126, out: "No version is set for command python3", timedOut: false };
    const k = kit({ copies: { python3: [shim, brew] }, programs: { [shim]: noVersion, [brew]: ok("Python 3.13.7") } });
    await expect(binary({ names: ["python3"], versionArgs: ["--version"] })(k)).resolves.toEqual({
      status: "installed",
      path: brew,
      version: "3.13.7",
    });
    const onlyShim = kit({ copies: { python3: [shim] }, programs: { [shim]: noVersion } });
    await expect(binary({ names: ["python3"], versionArgs: ["--version"] })(onlyShim)).resolves.toMatchObject({
      status: "unusable",
      path: shim,
      detail: "`python3 --version` said: No version is set for command python3",
    });
  });

  it("prefers a copy new enough over an older one found first", async () => {
    const k = kit({
      copies: { claude: ["/old/claude", "/new/claude"] },
      programs: { "/old/claude": ok("1.0.3 (Claude Code)"), "/new/claude": ok("2.1.4 (Claude Code)") },
    });
    await expect(binary({ names: ["claude"], versionArgs: ["--version"], minVersion: "2.0.0" })(k)).resolves.toMatchObject({
      status: "installed",
      path: "/new/claude",
    });
  });

  it("answers presence alone when asked not to run the program", async () => {
    const k = kit({ onPath: { flutter: "/opt/homebrew/bin/flutter" }, programs: { "/opt/homebrew/bin/flutter": ok("") } });
    await expect(binary({ names: ["flutter"] })(k)).resolves.toEqual({ status: "installed", path: "/opt/homebrew/bin/flutter" });
    expect(k.ran).toEqual([]);
  });
});

describe("anyOf", () => {
  it("takes the first that is installed, else an unusable over a missing answer", async () => {
    const missing = () => Promise.resolve({ status: "missing" as const, detail: "first place" });
    const unusable = () => Promise.resolve({ status: "unusable" as const, detail: "broken" });
    const installed = () => Promise.resolve({ status: "installed" as const, path: "/a" });
    const k = kit({});
    await expect(anyOf(missing, installed)(k)).resolves.toEqual({ status: "installed", path: "/a" });
    await expect(anyOf(missing, unusable)(k)).resolves.toMatchObject({ detail: "broken" });
    await expect(anyOf(missing, missing)(k)).resolves.toMatchObject({ detail: "first place" });
  });
});

describe("jdk", () => {
  it("reads both version spellings", () => {
    expect(javaMajor('openjdk version "17.0.12" 2024-07-16')).toBe(17);
    expect(javaMajor('java version "1.8.0_402"')).toBe(8);
    expect(javaMajor('openjdk version "21" 2023-09-19')).toBe(21);
    expect(javaMajor("nothing")).toBeNull();
  });

  it("finds Homebrew's keg-only openjdk@17, which nothing puts on PATH", async () => {
    const keg = "/opt/homebrew/opt/openjdk@17/bin/java";
    const k = kit({
      programs: {
        "/usr/libexec/java_home": { code: 1, out: "Unable to find any JVMs matching version \"17+\".", timedOut: false },
        [keg]: ok('openjdk version "17.0.12" 2024-07-16'),
      },
      onPath: { java: "/usr/bin/java" },
    });
    await expect(jdk(k)).resolves.toEqual({ status: "installed", path: keg, version: "17.0.12" });
    expect(k.ran.some((r) => r.startsWith("/usr/bin/java"))).toBe(false);
  });

  it("is unusable for a Java older than 17, and missing when there is none", async () => {
    const k = kit({ platform: "linux", onPath: { java: "/usr/bin/java" }, programs: { "/usr/bin/java": ok('openjdk version "11.0.24"') } });
    await expect(jdk(k)).resolves.toMatchObject({ status: "unusable", version: "11.0.24" });
    await expect(jdk(kit({ platform: "linux" }))).resolves.toMatchObject({ status: "missing" });
  });
});

describe("xcode and appium drivers", () => {
  it("finds an Xcode app by name, or through the selected developer directory", async () => {
    await expect(xcode(kit({ dirs: { "/Applications": ["Safari.app", "Xcode-16.2.app"] } }))).resolves.toMatchObject({
      status: "installed",
      path: "/Applications/Xcode-16.2.app",
    });
    await expect(xcode(kit({ developerDir: "/Volumes/T/Xcode.app/Contents/Developer" }))).resolves.toMatchObject({
      path: "/Volumes/T/Xcode.app",
    });
    await expect(xcode(kit({ developerDir: "/Library/Developer/CommandLineTools" }))).resolves.toMatchObject({ status: "missing" });
  });

  it("reads appium's driver list from either stream", async () => {
    const k = kit({ onPath: { appium: "/x/appium" }, programs: { "/x/appium": ok("- uiautomator2@3.8.0 [installed (npm)]") } });
    await expect(appiumDriver("uiautomator2")(k)).resolves.toMatchObject({ status: "installed" });
    await expect(appiumDriver("xcuitest")(k)).resolves.toMatchObject({ status: "missing" });
    await expect(appiumDriver("xcuitest")(kit({}))).resolves.toMatchObject({ status: "missing", detail: "Appium is not installed, so it has no drivers." });
  });
});
