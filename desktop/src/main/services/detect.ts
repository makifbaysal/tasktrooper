import { execFile } from "node:child_process";
import { statSync, accessSync, constants, existsSync, readdirSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import { app } from "electron";
import { taskkillTree } from "../supervisor/reaper.js";
import { knownLoginShellPath, loginShellPath } from "./login-env.js";
import type { PreflightCache } from "./preflight-cache.js";
import { binaryDirs, mergePath, pathKeyOf, prependDirs, splitPath, withoutAppImage } from "./process-env.js";
import { launchFor } from "./winshim.js";
import type {
  Blocker,
  Overrides,
  PreflightId,
  PreflightItem,
  PreflightReport,
  PreflightSource,
} from "../../ipc/types.js";

/**
 * Detection, and the preflight it now produces.
 *
 * This file is the product. The old flow asked the user to find five paths on
 * their own machine and type them into a 134-line shell file — `CHROME_BIN`,
 * `CLAUDE_CODE_BIN`, `ANDROID_HOME`, a hub URL, a port — and every one of them
 * is discoverable. A question whose answer the computer can look up is a
 * question that should not be asked, and asking it produces failures several
 * layers from their cause.
 *
 * What changed is the OUTPUT. Detection used to answer "found / not found",
 * which is the right answer for a binary and the wrong one for a capability:
 * a whole task is delegated to a headless `claude` session, and the ways that
 * fails are mostly not "the binary is missing". So every check reports one of
 * three statuses and, when it is not `ok`, the sentence that fixes it — see
 * `PreflightItem`.
 *
 * Every probe is bounded and runs with no shell. A machine with a stale NFS
 * mount on PATH must not make Connect hang.
 */

const PROBE_TIMEOUT_MS = 6_000;

/**
 * `claude auth status` may refresh an OAuth token, so it can touch the
 * network. Bounded well above a probe and well below anyone's patience.
 */
const AUTH_PROBE_TIMEOUT_MS = 15_000;

/**
 * The `claude -p` fallback runs a real (tiny) session. It only ever runs on a
 * CLI too old to answer `auth status --json`, and it is the slowest thing in
 * the preflight, so it gets its own bound.
 */
const CLI_PROBE_TIMEOUT_MS = 45_000;

/**
 * The oldest `claude` this app will drive.
 *
 * Not a guess at a feature flag: 2.0 is where the CLI grew `auth status
 * --json`, the machine-readable answer the account check below is built on,
 * and where `--output-format stream-json` settled into the shape the runner
 * parses. An older binary is `unusable` rather than `missing` — it is there,
 * it just cannot be driven, and telling someone to install a thing they
 * already have is how a preflight loses their trust.
 */
export const MIN_CLAUDE_VERSION = "2.0.0";

/**
 * Where the Appium hub is, and there is exactly one answer.
 *
 * A constant, not an allocated port and not a setting — the embedder's and the
 * backend's own loopback ports are the exceptions, because both are
 * OS-assigned rather than fixed. 4723 is Appium's own default,
 * which means a hub the user started by hand is on it, a hub the backend starts
 * is on it, and neither has to be told about the other. Allocating one
 * instead would make this app's hub invisible to every Appium client on the
 * machine.
 *
 * Loopback only: a hub on another machine would make the backend's Appium proxy
 * a general-purpose request forwarder aimed by configuration.
 */
export const APPIUM_BASE_URL = "http://127.0.0.1:4723";

/**
 * How long the hub gets to answer `GET /status`.
 *
 * Short on purpose. It is a loopback process: it answers in milliseconds or it
 * is not there, and this probe runs on the start path where every second is one
 * somebody is watching.
 */
const APPIUM_STATUS_TIMEOUT_MS = 1_500;

function isExecutable(candidate: string): boolean {
  try {
    // Windows has no execute bit; X_OK there only says the file exists.
    if (process.platform === "win32") return statSync(candidate).isFile();
    accessSync(candidate, constants.X_OK);
    return true;
  } catch {
    return false;
  }
}

/**
 * The directories to search, in priority order.
 *
 * A GUI-launched .app inherits launchd's PATH, not a login shell's, so
 * `/opt/homebrew/bin` — where Homebrew put half of these tools — is simply
 * absent from `process.env.PATH`. Appending the standard prefixes is what
 * makes a double-clicked app behave like the terminal the user tested in.
 * Both Homebrew prefixes are listed because Apple Silicon and Intel Macs
 * disagree, and a user who migrated between them can have either.
 *
 * Everything added after the original list is appended, never interleaved:
 * the login shell's own PATH first (it knows what the user's profile does),
 * then the version managers' and package managers' default directories as a
 * last guess. A binary found before is still the one found.
 */
function searchDirs(): { dir: string; source: PreflightSource }[] {
  const home = os.homedir();
  const out: { dir: string; source: PreflightSource }[] = [];
  const add = (dir: string, source: PreflightSource): void => {
    if (dir !== "" && !out.some((e) => e.dir === dir)) out.push({ dir, source });
  };
  for (const dir of pathValue().split(path.delimiter)) add(dir, "path");
  if (process.platform === "darwin") {
    add("/opt/homebrew/bin", "homebrew");
    add("/usr/local/bin", "homebrew");
  }
  if (process.platform === "linux") add("/snap/bin", "path");
  if (process.platform === "win32") add(path.join(appDataDir(), "npm"), "npm-prefix");
  add(path.join(home, ".local", "bin"), "home");
  add(path.join(home, ".claude", "local"), "home");
  add(path.join(home, ".bun", "bin"), "home");
  add(path.join(home, ".opencode", "bin"), "home");
  // npm's global prefix. `npm prefix -g` would be authoritative but costs a
  // node startup per call; these are the three locations it actually uses.
  if (process.platform === "darwin") add("/opt/homebrew/lib/node_modules/.bin", "npm-prefix");
  add(path.join(home, ".npm-global", "bin"), "npm-prefix");
  // nvm installs node under ~/.nvm/versions/node/<version>/bin. The static
  // path ~/.nvm/versions does not contain binaries, so `which()` would never
  // find them there. Use NVM_BIN if set (the active version), and also glob
  // every installed version's bin/ so a binary installed under a non-active
  // node is still found.
  if (process.platform !== "win32") {
    const nvmBin = process.env.NVM_BIN;
    if (nvmBin) add(nvmBin, "npm-prefix");
    const nvmVersionsDir = path.join(home, ".nvm", "versions", "node");
    for (const entry of listDir(nvmVersionsDir)) {
      const binDir = path.join(nvmVersionsDir, entry, "bin");
      if (existsSync(binDir)) add(binDir, "npm-prefix");
    }
  }
  for (const dir of knownLoginShellPath()) add(dir, "path");
  for (const { dir, source } of fallbackDirs(home)) add(dir, source);
  return out;
}

/** `%APPDATA%`, where npm's global prefix is on Windows — not necessarily under the profile directory. */
function appDataDir(): string {
  return process.env.APPDATA ?? path.join(os.homedir(), "AppData", "Roaming");
}

function localAppDataDir(): string {
  return process.env.LOCALAPPDATA ?? path.join(os.homedir(), "AppData", "Local");
}

function listDir(dir: string): string[] {
  try {
    return readdirSync(dir);
  } catch {
    return [];
  }
}

/**
 * Where version managers and installers put binaries when nothing told the
 * GUI session about them. Searched last, after PATH and the login shell's PATH.
 */
export function fallbackDirs(home: string = os.homedir()): { dir: string; source: PreflightSource }[] {
  const out: { dir: string; source: PreflightSource }[] = [];
  if (process.platform === "win32") {
    const programFiles = process.env.ProgramFiles ?? "C:\\Program Files";
    const programFilesX86 = process.env["ProgramFiles(x86)"] ?? "C:\\Program Files (x86)";
    const local = localAppDataDir();
    out.push({ dir: path.join(local, "pnpm"), source: "npm-prefix" });
    out.push({ dir: path.join(programFiles, "nodejs"), source: "path" });
    // Git for Windows' own `cmd` directory: machine-wide, 32-bit, and the
    // per-user install its installer offers when it cannot elevate.
    out.push({ dir: path.join(programFiles, "Git", "cmd"), source: "path" });
    out.push({ dir: path.join(programFilesX86, "Git", "cmd"), source: "path" });
    out.push({ dir: path.join(local, "Programs", "Git", "cmd"), source: "home" });
    // Per-user installers — the default for anything that cannot ask for
    // admin — land in %LOCALAPPDATA%\Programs\<app>, with their CLI in bin\.
    const programs = path.join(local, "Programs");
    for (const entry of listDir(programs)) out.push({ dir: path.join(programs, entry, "bin"), source: "home" });
    return out;
  }
  // An unset, empty or relative variable means the tool's default; a relative
  // one taken at face value would make the cwd a search directory.
  const envDir = (name: string): string | undefined => {
    const value = process.env[name];
    return value && path.isAbsolute(value) ? value : undefined;
  };
  const dataHome = envDir("XDG_DATA_HOME") ?? path.join(home, ".local", "share");
  out.push({ dir: path.join(home, ".volta", "bin"), source: "home" });
  const pnpmHome = envDir("PNPM_HOME");
  if (pnpmHome) out.push({ dir: pnpmHome, source: "npm-prefix" });
  out.push({
    dir: process.platform === "darwin" ? path.join(home, "Library", "pnpm") : path.join(dataHome, "pnpm"),
    source: "npm-prefix",
  });
  const fnmDir = envDir("FNM_DIR");
  if (fnmDir) out.push({ dir: path.join(fnmDir, "aliases", "default", "bin"), source: "home" });
  out.push({ dir: path.join(dataHome, "fnm", "aliases", "default", "bin"), source: "home" });
  out.push({ dir: path.join(home, ".fnm", "aliases", "default", "bin"), source: "home" });
  out.push({ dir: path.join(home, ".fnm"), source: "home" });
  out.push({ dir: path.join(envDir("ASDF_DATA_DIR") ?? path.join(home, ".asdf"), "shims"), source: "home" });
  out.push({ dir: path.join(dataHome, "mise", "shims"), source: "home" });
  if (process.platform === "linux") {
    out.push({ dir: "/usr/local/bin", source: "path" });
    out.push({ dir: "/home/linuxbrew/.linuxbrew/bin", source: "homebrew" });
    out.push({ dir: path.join(home, ".linuxbrew", "bin"), source: "homebrew" });
  }
  return out;
}

/** A binary detection found, and how. */
export interface Located {
  path: string;
  source: PreflightSource;
}

/**
 * `dirs` is `searchDirs()` unless the caller already has it: a sweep computes
 * it once — it lists the nvm versions directory and reads the login PATH — and
 * hands it to every lookup rather than rebuilding it per binary.
 */
export function which(name: string, dirs: { dir: string; source: PreflightSource }[] = searchDirs()): Located | null {
  if (name.includes("/") || name.includes("\\")) return isExecutable(name) ? { path: name, source: "override" } : null;
  // On Windows a command is found by its extension: PATHEXT, .exe before .cmd.
  const names =
    process.platform === "win32"
      ? [...(process.env.PATHEXT ?? ".EXE;.CMD;.BAT").split(";").filter(Boolean).map((ext) => name + ext.toLowerCase()), name]
      : [name];
  for (const { dir, source } of dirs) {
    for (const candidateName of names) {
      const candidate = path.join(dir, candidateName);
      if (isExecutable(candidate)) return { path: candidate, source };
    }
  }
  return null;
}

/** PATH under whatever spelling this platform's environment uses, without an AppImage's own entries. */
function pathValue(): string {
  const env = withoutAppImage(process.env);
  return env[pathKeyOf(env)] ?? "";
}

/**
 * The environment probes run in: this process's, with the prefixes a
 * GUI-launched .app does not inherit put back on PATH.
 *
 * Finding a tool and running it are different problems, and only the first was
 * solved. `which()` searches its own list, so a Node-based CLI was located
 * fine — but its shebang is `#!/usr/bin/env node`, and launchd's PATH has no
 * node in it:
 *
 *   env: node: No such file or directory
 *
 * So every probe that asked a tool about itself came back empty, and empty was
 * read as absent. The login shell's PATH is appended for the same reason, and
 * `run()` puts the probed binary's own directory first.
 */
export function probeEnv(): NodeJS.ProcessEnv {
  const env = withoutAppImage({ ...process.env });
  const key = pathKeyOf(env);
  const extras = process.platform === "darwin" ? ["/opt/homebrew/bin", "/usr/local/bin"] : [];
  env[key] = mergePath([splitPath(env[key]), extras, knownLoginShellPath()]).join(path.delimiter);
  return env;
}

/** `probeEnv()` with the probed binary's directory first, so `env node` finds the node it was installed beside. */
function probeEnvFor(command: string): NodeJS.ProcessEnv {
  const env = probeEnv();
  const key = pathKeyOf(env);
  env[key] = prependDirs(splitPath(env[key]), binaryDirs([command])).join(path.delimiter);
  return env;
}

interface RunResult {
  code: number;
  stdout: string;
  stderr: string;
  /** Both streams joined, for tools that write their answer to either. */
  out: string;
  /** True when the process was killed for exceeding its bound. */
  timedOut: boolean;
}

/**
 * Run a probe, with no shell on any platform: a Windows `.cmd` shim becomes the
 * node invocation it would have made (`winshim.ts`), so a path with a space in
 * it and an argument with one both survive.
 *
 * On Windows the bound is enforced with `taskkill /T` rather than execFile's
 * own timeout, which kills only the direct child and leaves whatever it
 * started running.
 */
function run(command: string, args: string[], timeoutMs = PROBE_TIMEOUT_MS): Promise<RunResult> {
  return new Promise((resolve) => {
    const win = process.platform === "win32";
    let timedOut = false;
    let timer: NodeJS.Timeout | undefined;
    try {
      const launch = launchFor(command, args, probeEnvFor(command));
      const child = execFile(
        launch.command,
        launch.args,
        {
          env: launch.env,
          maxBuffer: 1024 * 1024,
          shell: false,
          windowsHide: true,
          ...(launch.windowsVerbatimArguments ? { windowsVerbatimArguments: true } : {}),
          ...(win ? {} : { timeout: timeoutMs }),
        },
        (err, stdout, stderr) => {
          clearTimeout(timer);
          timedOut = timedOut || (!!err && (err as { killed?: boolean }).killed === true);
          const code =
            err && typeof (err as { code?: unknown }).code === "number" ? (err as { code: number }).code : err ? 1 : 0;
          resolve({
            code,
            stdout: (stdout ?? "").trim(),
            stderr: (stderr ?? "").trim(),
            out: `${stdout ?? ""}${stderr ?? ""}`.trim(),
            timedOut,
          });
        },
      );
      if (win) {
        timer = setTimeout(() => {
          timedOut = true;
          if (child.pid !== undefined) void taskkillTree(child.pid);
          else child.kill();
        }, timeoutMs);
      }
    } catch (err) {
      clearTimeout(timer);
      const msg = err instanceof Error ? err.message : String(err);
      resolve({
        code: 1,
        stdout: "",
        stderr: msg,
        out: msg,
        timedOut: false,
      });
    }
  });
}

const firstLine = (text: string): string => text.split("\n")[0]?.trim() ?? "";

/** `2.1.220 (Claude Code)` → `2.1.220`. Absent when the line names no version. */
export function parseVersion(text: string): string | null {
  return /(\d+)\.(\d+)(?:\.(\d+))?/.exec(text)?.[0] ?? null;
}

/** -1, 0, 1. Missing segments count as zero, so `2.1` is `2.1.0`. */
export function compareVersions(a: string, b: string): number {
  const parts = (v: string): number[] => v.split(".").map((n) => Number.parseInt(n, 10) || 0);
  const [x, y] = [parts(a), parts(b)];
  for (let i = 0; i < Math.max(x.length, y.length); i += 1) {
    const diff = (x[i] ?? 0) - (y[i] ?? 0);
    if (diff !== 0) return diff > 0 ? 1 : -1;
  }
  return 0;
}

// --- the bundled backend ----------------------------------------------------

/**
 * Packaged: `Contents/Resources/bin`, which is what electron-builder's
 * extraResources puts there and what codesigning covers. A shipped app
 * resolving this off PATH would mean a notarized bundle running an unsigned
 * binary someone else installed.
 *
 * Dev: `desktop/bin`, which is what `npm run build:server` writes into.
 */
/** The bundled backend's file name on this platform. */
export const SERVER_BINARY = process.platform === "win32" ? "agent-server.exe" : "agent-server";

export function binDir(): string {
  return app.isPackaged ? path.join(process.resourcesPath, "bin") : path.join(app.getAppPath(), "bin");
}

/**
 * Where the backend keeps everything that has to survive a restart: the
 * embedded Postgres cluster, the RAG files, the session workspaces.
 *
 * Under userData rather than beside the app, because the app bundle is replaced
 * wholesale by every update.
 */
export function dataDir(): string {
  return path.join(app.getPath("userData"), "data");
}

/**
 * The executor's own data directory in account mode (`executor_data_dir`).
 * A sibling of `dataDir()` rather than inside it, so nothing an account keeps
 * on this machine mixes with local-mode data and either can be deleted alone.
 */
export function executorDataDir(): string {
  return path.join(app.getPath("userData"), "executor");
}

/**
 * Where the embedded Postgres binaries are downloaded and extracted.
 *
 * Deliberately NOT under `dataDir()`: it is a cache that can be deleted and
 * re-fetched, and keeping it out of the data directory is what makes "delete
 * the database" and "delete the download" two different actions.
 */
export function postgresCacheDir(): string {
  return path.join(app.getPath("userData"), "postgres-bin");
}

/**
 * The embedder's bundled entrypoint, on the same packaged/dev split as
 * `binDir()`.
 *
 * Packaged: `Contents/Resources/embedder`, which is the new `extraResources`
 * entry in `electron-builder.yml` — a flat directory (the JS bundle plus
 * `onnxruntime-web`'s staged `.wasm`/`.mjs` runtime files) rather than a
 * `node_modules` tree, which is why `engine.ts` points
 * `ort.env.wasm.wasmPaths` at this same directory explicitly instead of
 * relying on relative auto-resolution.
 *
 * Dev: `desktop/dist/embedder`, which is what `npm run build:embedder` writes
 * into — the same relationship `build:server`'s output has to `binDir()`'s dev
 * branch.
 *
 * Not itself a preflight item: a missing embedder bundle is a broken build of
 * this app rather than a fact about the user's machine — see `preflight()`'s
 * own note on why the embedder is not modelled there at all.
 */
export function embedderScriptPath(): string {
  return app.isPackaged
    ? path.join(process.resourcesPath, "embedder", "index.cjs")
    : path.join(app.getAppPath(), "dist", "embedder", "index.cjs");
}

/**
 * The backend this app ships.
 *
 * Required, and the one item on this list the user cannot install: a copy of
 * the app without it is a broken build, not a missing dependency, so the
 * remediation says so rather than offering a command that would not help.
 */
function probeAgentServer(): PreflightItem {
  const bundled = path.join(binDir(), SERVER_BINARY);
  if (isExecutable(bundled)) {
    return {
      id: "agent-server",
      label: "TaskTrooper server",
      required: true,
      status: "ok",
      path: bundled,
      source: app.isPackaged ? "bundled" : "dev-bin",
    };
  }
  return {
    id: "agent-server",
    label: "TaskTrooper server",
    required: true,
    status: "missing",
    detail: `Not found at ${bundled}.`,
    remediation: app.isPackaged
      ? "This copy of TaskTrooper is incomplete. Reinstall it from the latest build."
      : "Build it: npm run build:server",
    ...(app.isPackaged ? {} : { command: "npm run build:server" }),
  };
}

/** The bundled runner's and executor's file names on this platform, account mode only. */
export const RUNNER_BINARY = process.platform === "win32" ? "runner.exe" : "runner";
export const EXECUTOR_BINARY = process.platform === "win32" ? "executor.exe" : "executor";

/**
 * A binary this app ships for account mode, `probeAgentServer`'s rule: the
 * user cannot install it, so missing is a broken build, and in development
 * the remediation is the npm script that builds it.
 */
function probeBundled(id: "runner" | "executor", label: string, file: string, script: string, required: boolean): PreflightItem {
  const bundled = path.join(binDir(), file);
  if (isExecutable(bundled)) {
    return { id, label, required, status: "ok", path: bundled, source: app.isPackaged ? "bundled" : "dev-bin" };
  }
  return {
    id,
    label,
    required,
    status: "missing",
    detail: `Not found at ${bundled}.`,
    remediation: app.isPackaged
      ? "This copy of TaskTrooper is incomplete. Reinstall it from the latest build."
      : `Build it: npm run ${script}`,
    ...(app.isPackaged ? {} : { command: `npm run ${script}` }),
  };
}

/**
 * The database, which nobody installs.
 *
 * The backend runs an embedded Postgres and downloads its binaries (~30 MB)
 * the first time it starts. So this item is ALWAYS `ok` and never blocks — it
 * exists only so that download is a thing the user was told about in advance
 * rather than a minute of silence on a first launch, which is the one moment
 * this app looks broken when it is working.
 */
export function probePostgres(): PreflightItem {
  const cache = postgresCacheDir();
  // fergusstrange/embedded-postgres extracts under its RuntimePath; the bare
  // cache root is checked too so a layout change downgrades to "will download"
  // rather than to a wrong answer.
  const ext = process.platform === "win32" ? ".exe" : "";
  const extracted = [
    path.join(cache, "runtime", "bin", `postgres${ext}`),
    path.join(cache, "bin", `postgres${ext}`),
    path.join(cache, "runtime", "bin", "postgres"),
    path.join(cache, "bin", "postgres"),
  ];
  const found = extracted.find((candidate) => existsSync(candidate));
  if (found) {
    return {
      id: "postgres",
      label: "Embedded Postgres",
      required: false,
      status: "ok",
      path: found,
      source: "bundled",
    };
  }
  return {
    id: "postgres",
    label: "Embedded Postgres",
    required: false,
    status: "ok",
    detail: "Downloads on first start (~30 MB). Nothing to install.",
  };
}

// --- git --------------------------------------------------------------------

/**
 * git, and it is required now in its own right.
 *
 * The whole task happens inside a Claude Code session on this machine — clone,
 * edit, build, commit, push — and every one of those is git. Its absence is not
 * a degradation.
 */
/**
 * `xcode-select --install` only exists on macOS. Windows and Linux each need
 * their own remediation, and a distro-agnostic package manager command does
 * not exist, so Linux gets a sentence and no copyable command.
 */
export function gitRemediation(): { remediation: string; command?: string } {
  switch (process.platform) {
    case "win32":
      return { remediation: "Install Git for Windows.", command: "winget install --id Git.Git -e --source winget" };
    case "linux":
      return { remediation: "Install git from this distribution's package manager." };
    default:
      return { remediation: "Install Apple's command line tools, which include git.", command: "xcode-select --install" };
  }
}

/**
 * What `--version` said, when it was asked. Absent means "not asked yet",
 * which the gating half of a sweep says about every binary whose answer is not
 * cached; the complete half always asks.
 */
interface VersionAnswer {
  code: number;
  out: string;
}

function gitItem(found: Located | null, answer?: VersionAnswer): PreflightItem {
  if (!found) {
    return {
      id: "git",
      label: "git",
      required: true,
      status: "missing",
      detail: "Every task runs inside a headless agent session that clones, commits and pushes with git. Without it a task cannot start.",
      ...gitRemediation(),
    };
  }
  return {
    id: "git",
    label: "git",
    required: true,
    status: "ok",
    path: found.path,
    source: found.source,
    ...(answer?.code === 0 ? { version: parseVersion(answer.out) ?? firstLine(answer.out) } : {}),
  };
}

// --- claude -----------------------------------------------------------------

/**
 * The Claude Code CLI. Required: this is the whole reason a Mac is attached
 * rather than a cloud process doing the work.
 *
 * Searched more widely than PATH because the CLI installs itself in several
 * places depending on how it was installed — npm global, Homebrew, its own
 * `~/.claude/local`, a bun prefix, or the native installer's
 * `~/.local/bin` — and a user who installed it in a login shell will not have
 * that prefix on a GUI app's PATH.
 */
function claudeItem(found: Located | null, answer?: VersionAnswer): PreflightItem {
  if (!found) {
    return {
      id: "claude",
      label: "Claude Code CLI",
      required: false,
      status: "missing",
      detail: "Agents on the Claude Code provider run as headless sessions driven by this binary.",
      remediation: "Install the Claude Code CLI, then press Connect again.",
      command: "npm install -g @anthropic-ai/claude-code",
    };
  }

  // Not asked yet: found, and provisionally usable. Spawning the backend does
  // not wait for a Node CLI to boot just to print its version; the complete
  // half of the sweep asks, and the supervisor restarts the backend in the
  // rare case the answer changes what it was started with.
  if (!answer) {
    return { id: "claude", label: "Claude Code CLI", required: false, status: "ok", path: found.path, source: found.source };
  }

  const { code, out } = answer;
  const version = code === 0 ? parseVersion(out) : null;

  // A binary that will not answer `--version` is present and unusable, and the
  // two most common reasons are worth separating from "too old": a broken node
  // shebang, or a partially written install. Either way the text it produced is
  // more useful than anything this file could invent, so it is quoted.
  if (version === null) {
    return {
      id: "claude",
      label: "Claude Code CLI",
      required: false,
      status: "unusable",
      path: found.path,
      source: found.source,
      detail: out === "" ? "It did not answer `claude --version`." : `\`claude --version\` said: ${firstLine(out)}`,
      remediation: "Reinstall the Claude Code CLI.",
      command: "npm install -g @anthropic-ai/claude-code",
    };
  }

  if (compareVersions(version, MIN_CLAUDE_VERSION) < 0) {
    return {
      id: "claude",
      label: "Claude Code CLI",
      required: false,
      status: "unusable",
      path: found.path,
      source: found.source,
      version,
      detail: `Version ${version} is older than ${MIN_CLAUDE_VERSION}, which is the oldest this app can drive.`,
      remediation: "Update the Claude Code CLI.",
      command: "claude update",
    };
  }

  return {
    id: "claude",
    label: "Claude Code CLI",
    required: false,
    status: "ok",
    path: found.path,
    source: found.source,
    version,
  };
}

// --- the claude account -----------------------------------------------------

/**
 * The shape `claude auth status --json` prints. Every field is optional
 * because this is another program's output, not a type we control.
 */
interface ClaudeAuthStatus {
  loggedIn?: unknown;
  authMethod?: unknown;
  apiProvider?: unknown;
  subscriptionType?: unknown;
  email?: unknown;
  orgName?: unknown;
}

/**
 * The subscription tiers that include Claude Code.
 *
 * Read out of the CLI rather than assumed. `claude auth status --json` reports
 * `subscriptionType` only for `authMethod: "claude.ai"`, and it produces the
 * value by mapping the account's raw type through a fixed table:
 *
 *   claude_max → max   claude_pro → pro   claude_team → team
 *   claude_enterprise → enterprise
 *
 * An account whose raw type is not in that table — a FREE account is the one
 * that matters — falls through to `null`. So "logged into claude.ai, and
 * `subscriptionType` is null" is the observable that means "this account
 * cannot run Claude Code", and it is the only one the CLI offers.
 */
const CLAUDE_CODE_TIERS = new Set(["pro", "max", "team", "enterprise"]);

/** The tiers that definitely do NOT include Claude Code, spelled out. */
const FREE_TIERS = new Set(["free", "none"]);

const NOT_SIGNED_IN: Pick<PreflightItem, "status" | "detail" | "remediation" | "command"> = {
  status: "missing",
  detail: "This machine has the Claude Code CLI but no Claude account signed into it.",
  remediation: "Sign in to your Claude account in a terminal, then press Connect again.",
  command: "claude auth login",
};

const NO_CLAUDE_CODE_PLAN: Pick<PreflightItem, "status" | "detail" | "remediation"> = {
  status: "unusable",
  detail:
    "You are signed in, but this Claude account's plan does not include Claude Code. A free account can sign in " +
    "and cannot run it.",
  remediation:
    "Upgrade this account to Claude Pro, Max, Team or Enterprise at claude.ai/upgrade — or sign in with an " +
    "account that already has one of those.",
};

/**
 * Can this Mac's Claude account actually run Claude Code?
 *
 * This is the check the whole preflight exists for. Everything else fails
 * loudly and early; this one used to fail quietly and late — the CLI installs,
 * every "is it there" check passes, and the first real run dies several minutes
 * in with a message about a model. Two distinct causes hide behind that, and
 * they have nothing to do with each other:
 *
 *   not signed in            → `claude auth login`
 *   signed in, free account  → change the plan; no command fixes it
 *
 * The observable is `claude auth status --json`, which is the CLI's own
 * machine-readable answer:
 *
 *   {"loggedIn":false,"authMethod":"none","apiProvider":"firstParty"}      exit 1
 *   {"loggedIn":true,"authMethod":"claude.ai","subscriptionType":"max",…}  exit 0
 *
 * The exit code is `loggedIn ? 0 : 1`, so it carries no information the body
 * does not, and the body is what is read. A CLI too old to have the subcommand
 * prints a usage error instead of JSON; that is what the fallback below is for.
 *
 * Deliberately NOT decided from the exit code alone, and not from
 * `~/.claude.json`: the credentials live in the login Keychain, the JSON file
 * holds onboarding state rather than auth, and a file this app reads directly
 * is a file it has to keep reading correctly across CLI releases.
 */
async function probeClaudeAccount(claude: PreflightItem): Promise<PreflightItem> {
  const base: PreflightItem = {
    id: "claude-account",
    label: "Claude account",
    required: false,
    status: "missing",
  };

  // Nothing to ask. Reporting a second failure for the same cause would put two
  // blockers in front of a user with one problem.
  if (claude.status !== "ok" || !claude.path) {
    return {
      ...base,
      detail: "Not checked: the Claude Code CLI has to be usable first.",
      remediation: "Fix the Claude Code CLI above, then check again.",
    };
  }

  const result = await run(claude.path, ["auth", "status", "--json"], AUTH_PROBE_TIMEOUT_MS);

  if (result.timedOut) {
    return {
      ...base,
      status: "unusable",
      detail: `\`claude auth status\` did not answer within ${AUTH_PROBE_TIMEOUT_MS / 1000}s.`,
      remediation: "Check this machine's network, then check again. Signing in refreshes a token over the network.",
    };
  }

  const parsed = parseAuthStatus(result.stdout);
  if (parsed) return { ...base, ...classifyAuthStatus(parsed) };

  // No JSON: an older CLI without `auth status`, or one that failed before it
  // could print. Fall back to asking it to do the smallest real thing.
  return { ...base, ...(await probeClaudeAccountByRunning(claude.path)) };
}

function parseAuthStatus(stdout: string): ClaudeAuthStatus | null {
  if (!stdout.startsWith("{")) return null;
  try {
    const parsed: unknown = JSON.parse(stdout);
    if (typeof parsed !== "object" || parsed === null) return null;
    // `loggedIn` is the field this whole classification turns on. Without it
    // the document is not the one this code was written against, whatever else
    // it contains.
    if (typeof (parsed as ClaudeAuthStatus).loggedIn !== "boolean") return null;
    return parsed as ClaudeAuthStatus;
  } catch {
    return null;
  }
}

export function classifyAuthStatus(status: ClaudeAuthStatus): Partial<PreflightItem> {
  if (status.loggedIn !== true) return { ...NOT_SIGNED_IN };

  const method = typeof status.authMethod === "string" ? status.authMethod : "";
  const email = typeof status.email === "string" ? status.email : "";

  // Console keys, Bedrock and Vertex bill through an API account rather than a
  // Claude plan, so `subscriptionType` is absent by design and its absence says
  // nothing. Reading it here would report every API-key user as unable to run
  // Claude Code, which is the exact false negative this check must not produce.
  if (method !== "claude.ai") {
    return {
      status: "ok",
      source: "path",
      detail: `Signed in with ${method === "" ? "an API credential" : method}, which bills through an API account.`,
    };
  }

  const who = email === "" ? "a Claude account" : email;
  const raw = status.subscriptionType;
  const tier = typeof raw === "string" ? raw.toLowerCase() : "";

  if (CLAUDE_CODE_TIERS.has(tier)) {
    return { status: "ok", detail: `Signed in as ${who} on the ${tier} plan.` };
  }

  // The only two observations that justify the confident answer.
  //
  // `subscriptionType: null` is the CLI SAYING SO: it maps an account's raw
  // type through a fixed table (claude_max, claude_pro, claude_team,
  // claude_enterprise) and anything else — a free account above all — falls
  // out as null. A tier we recognise as free says it outright. Either way the
  // CLI has answered the question, and the answer is no.
  if (raw === null || FREE_TIERS.has(tier)) {
    return {
      ...NO_CLAUDE_CODE_PLAN,
      ...(email === "" ? {} : { detail: `${NO_CLAUDE_CODE_PLAN.detail} Signed in as ${email}.` }),
    };
  }

  // Everything else is "we could not tell", and it degrades to the honest
  // weaker message rather than to the confident wrong one.
  //
  // Three cases land here and they are all the same case: an unrecognised tier
  // (almost certainly a new paid one), a `subscriptionType` of some other type,
  // and the field being ABSENT. Absent used to be folded in with null above,
  // which is backwards — an absent field is LESS informative than an explicit
  // null, not more — and it meant a CLI release that renamed the key, or
  // emitted an object, would tell every paying customer on this product that
  // their plan does not include Claude Code. That answer is unactionable and
  // reads as the product being broken, which is the one wrong answer this check
  // cannot afford; a session that genuinely cannot run will say so itself,
  // plainly, within seconds.
  const because =
    raw === undefined
      ? "this CLI did not report a subscription type"
      : typeof raw === "string"
        ? `an unrecognised plan (${tier})`
        : "this CLI reported a subscription type in a shape this app does not know";
  return { status: "ok", detail: `Signed in as ${who}; ${because}, so its plan was not checked.` };
}

/**
 * The fallback for a CLI with no `auth status`: run the smallest possible real
 * session and read what it says.
 *
 * One turn, a two-word prompt, and it only ever runs when the machine-readable
 * path did not answer — so the cost is paid by old installs and by nobody else.
 * `claude -p` on a signed-out machine exits 1 and prints its own sentence:
 *
 *   Not logged in · Please run /login
 *
 * That string is the CLI's, not ours, so it is matched loosely and quoted back
 * rather than replaced.
 */
async function probeClaudeAccountByRunning(claudeBin: string): Promise<Partial<PreflightItem>> {
  // The two pinned flags go on this session too, and they are not optional
  // here just because it is "only a probe".
  //
  // This starts a REAL Claude Code session on the user's Mac. Without
  // `--strict-mcp-config` it loads whatever MCP servers they configured for
  // themselves; without `--setting-sources project,local` it loads their own
  // ~/.claude, so a `SessionStart` hook runs — during a preflight the user
  // asked for, from a process they did not start by hand. The runner refuses to
  // spawn a session without these; a probe that spawns one anyway is the same
  // hole with a smaller name.
  //
  // A CLI too old to parse them fails, and that failure IS the answer: this
  // path only runs on a CLI too old for `auth status --json`, "too old" is
  // exactly what the probe is trying to establish, and the branch below already
  // reports an unrecognised failure as "could not tell, update the CLI".
  const result = await run(
    claudeBin,
    ["-p", "say ok", "--max-turns", "1", "--strict-mcp-config", "--setting-sources", "project,local"],
    CLI_PROBE_TIMEOUT_MS,
  );
  if (result.code === 0) {
    return { status: "ok", detail: "Checked by running a one-turn session; this CLI is too old to be asked directly." };
  }

  const said = firstLine(result.out);
  if (/not logged in|please run \/login|\/login\b|authenticat/i.test(result.out)) {
    return { ...NOT_SIGNED_IN, detail: `${NOT_SIGNED_IN.detail} It said: ${said}` };
  }
  if (/subscription|upgrade|plan does not|not available on your plan|billing/i.test(result.out)) {
    return { ...NO_CLAUDE_CODE_PLAN, detail: `${NO_CLAUDE_CODE_PLAN.detail} It said: ${said}` };
  }

  // The honest weaker answer. This CLI is too old to be asked which of the two
  // it is, and the run itself did not say — so the message says exactly that
  // rather than picking one and being confidently wrong.
  return {
    status: "unusable",
    detail:
      said === ""
        ? "A one-turn Claude Code session failed, and this CLI is too old to be asked why."
        : `A one-turn Claude Code session failed: ${said}`,
    remediation:
      "Update the Claude Code CLI so this can be checked properly, then sign in and confirm your plan includes " +
      "Claude Code.",
    command: "claude update",
  };
}

// --- optional: the other agent CLIs -----------------------------------------

/**
 * Binary-presence checks for the other local agent CLIs a task can run on
 * (`internal/adapter/cli/{antigravity,cursor,opencode}` on the server
 * side). Optional, like `claude` itself: this Mac is not required to have any
 * particular one, because which CLI a task uses is chosen per AGENT, not per
 * Mac, and blocking the backend on a binary nobody selected would stop someone
 * who only uses another CLI or an API key.
 *
 * Deliberately binary-and-version only, with no `claude-account`-style split.
 * That split exists because `claude auth status --json` is a fast, documented,
 * machine-readable answer this app can trust; none of these three CLIs has an
 * equivalent this file can verify without either inventing a parse of output
 * whose exact shape is not confirmed, or running a real session (agy's own
 * auth check takes a full turn server-side, at a 3-minute bound — far past
 * what a preflight sweep run on every Connect can spend). Reporting "found" or
 * "not found" honestly beats reporting "signed in" on a guess. An
 * unauthenticated CLI still fails cleanly at the point a task actually runs
 * it; this check only prevents the earlier, more confusing failure of a
 * provider choice that could never have worked on this Mac at all.
 */
interface SimpleCli {
  id: "agy" | "cursor-agent" | "opencode";
  label: string;
  binaryName: string;
  installHint: string;
}

function simpleCliItem({ id, label, installHint }: SimpleCli, found: Located | null, answer?: VersionAnswer): PreflightItem {
  if (!found) {
    return {
      id,
      label,
      required: false,
      status: "missing",
      detail: `The ${label} binary is not on this machine's PATH.`,
      remediation: `Install ${label}, then check again — or use a different provider on your agents.`,
      command: installHint,
    };
  }
  return {
    id,
    label,
    required: false,
    status: "ok",
    path: found.path,
    source: found.source,
    ...(answer?.code === 0 ? { version: parseVersion(answer.out) ?? firstLine(answer.out) } : {}),
  };
}

/** In report order. A function, because the install hints read the platform. */
function simpleClis(): [SimpleCli, SimpleCli, SimpleCli] {
  return [
    { id: "agy", label: "Antigravity CLI", binaryName: "agy", installHint: "See antigravity.google/docs/cli for the install." },
    {
      id: "cursor-agent",
      label: "Cursor CLI",
      binaryName: "cursor-agent",
      installHint:
        process.platform === "win32" ? "See cursor.com/cli for the install." : "curl https://cursor.com/install -fsS | bash",
    },
    {
      id: "opencode",
      label: "OpenCode CLI",
      binaryName: "opencode",
      installHint: process.platform === "win32" ? "npm install -g opencode-ai" : "curl -fsSL https://opencode.ai/install | bash",
    },
  ];
}

// --- optional: chrome and the Xcode command line tools ----------------------

/**
 * The five app bundles the old setup script probed, in its order. Reused
 * rather than reinvented so the two agree about what counts as Chrome.
 */
const MAC_CHROME_CANDIDATES = [
  "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
  "/Applications/Chromium.app/Contents/MacOS/Chromium",
  "/Applications/Google Chrome Beta.app/Contents/MacOS/Google Chrome Beta",
  "/Applications/Google Chrome Canary.app/Contents/MacOS/Google Chrome Canary",
  "/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
];

const CHROME_CANDIDATES =
  process.platform === "darwin"
    ? MAC_CHROME_CANDIDATES
    : process.platform === "win32"
      ? [
          path.join(process.env.PROGRAMFILES ?? "C:\\Program Files", "Google", "Chrome", "Application", "chrome.exe"),
          path.join(process.env["PROGRAMFILES(X86)"] ?? "C:\\Program Files (x86)", "Google", "Chrome", "Application", "chrome.exe"),
          path.join(process.env.LOCALAPPDATA ?? path.join(os.homedir(), "AppData", "Local"), "Google", "Chrome", "Application", "chrome.exe"),
          path.join(process.env["PROGRAMFILES(X86)"] ?? "C:\\Program Files (x86)", "Microsoft", "Edge", "Application", "msedge.exe"),
        ]
      : [
          "/usr/bin/google-chrome",
          "/usr/bin/google-chrome-stable",
          "/usr/bin/chromium",
          "/usr/bin/chromium-browser",
          "/snap/bin/chromium",
          "/usr/bin/microsoft-edge",
        ];

function probeChrome(override?: string): PreflightItem {
  const candidates = override && override !== "" ? [override] : CHROME_CANDIDATES;
  const home = os.homedir();
  const all = [
    ...candidates,
    ...(process.platform === "darwin" ? MAC_CHROME_CANDIDATES.map((c) => path.join(home, c.replace(/^\//, ""))) : []),
  ];
  const found = all.find((c) => isExecutable(c));
  if (!found) {
    return {
      id: "chrome",
      label: "Chrome / Chromium",
      // Optional, and it must stay optional: it costs a Claude Code session its
      // browser tools and nothing else. Blocking Connect on it would stop a
      // user who only ever writes code.
      required: false,
      status: "missing",
      detail: "Without it, a session cannot drive a browser. Everything else works.",
      remediation: "Install Google Chrome from google.com/chrome if a task will need a browser.",
    };
  }
  return {
    id: "chrome",
    label: "Chrome / Chromium",
    required: false,
    status: "ok",
    path: found,
    source: override ? "override" : "app-bundle",
  };
}

function xcodeItem(found: Located | null, answer?: VersionAnswer): PreflightItem {
  if (!found) {
    return {
      id: "xcode-clt",
      label: "Xcode command line tools",
      required: false,
      status: "missing",
      detail: "Needed only for tasks that build Apple platform targets.",
      remediation: "Install them if a repository on this machine builds for Apple platforms.",
      command: "xcode-select --install",
    };
  }
  return {
    id: "xcode-clt",
    label: "Xcode command line tools",
    required: false,
    status: "ok",
    path: found.path,
    source: found.source,
    ...(answer === undefined || answer.code === 0
      ? {}
      : { detail: "They are present but did not answer; the install may be incomplete." }),
  };
}

// --- optional: the mobile toolchain -----------------------------------------

/**
 * Appium, and the two drivers that are what actually make it useful.
 *
 * All optional, and all reported even when they are missing, because the
 * failure this exists to prevent is the late one: a QA task reaches this Mac,
 * a session is created against a simulator, and the run dies several minutes in
 * with a connection error about a port — when "npm install -g appium" could
 * have been said before anybody pressed Connect.
 *
 * There is no toggle and no URL to type. Appium is enabled by being installed:
 * the backend is handed the binary (`APPIUM_BIN`) and starts a hub on
 * `APPIUM_BASE_URL` when a mobile task needs one, stops it once idle, and uses
 * one that is already answering there instead of starting its own. The old flow made the user
 * uncomment `MOBILE_APPIUM_HUB_URL` and choose a port, which is two decisions to
 * express one fact.
 *
 * The DRIVERS are separate items and only appear once Appium itself is here.
 * A Mac with no Appium showing three missing rows would be three rows about one
 * problem; a Mac with Appium and no `xcuitest` is a genuinely different
 * situation with its own one-line fix, and it is the one that otherwise fails
 * at session creation rather than at install time.
 *
 * `appium driver list --installed` writes its whole answer to STDERR, which is
 * why `out` is read rather than `stdout` — reading the wrong stream reported
 * both drivers missing on a machine that had just loaded them by name.
 */
interface AppiumAnswers {
  version?: VersionAnswer;
  /** `appium driver list --installed`. Absent: not asked yet, so no driver rows. */
  installed?: VersionAnswer;
  /** Whether a hub already answers on the shared port. Absent: not asked yet. */
  hubUp?: boolean;
}

/** `ios` is read by the caller before its first await: iOS simulators exist only on macOS. */
function appiumItems(found: Located | null, ios: boolean, answers: AppiumAnswers = {}): PreflightItem[] {
  if (!found) {
    return [
      {
        id: "appium",
        label: "Appium",
        required: false,
        status: "missing",
        detail:
          `Without it this machine cannot drive ${ios ? "an iOS simulator or an Android emulator" : "an Android emulator"} for a QA task. ` +
          "Everything that is not mobile automation works.",
        remediation: "Install Appium if a task will drive a simulator or an emulator.",
        command: "npm install -g appium",
      },
    ];
  }

  const { version, installed, hubUp } = answers;
  return [
    {
      id: "appium",
      label: "Appium",
      required: false,
      status: "ok",
      path: found.path,
      source: found.source,
      ...(version?.code === 0 ? { version: parseVersion(version.out) ?? firstLine(version.out) } : {}),
      // Which hub this Mac will use, said before it matters. "TaskTrooper
      // starts one" and "one is already running" are the same outcome for a
      // task and a different one for whoever is wondering why their own hub's
      // logs are filling up — or why no Appium process is running right now.
      detail: hubUp
        ? `An Appium server is already answering on ${APPIUM_BASE_URL}; TaskTrooper will use it rather than starting a second one.`
        : `Started on demand by the server: a hub on ${APPIUM_BASE_URL} comes up when a mobile task needs one and stops after 10 idle minutes.`,
    },
    ...(installed === undefined
      ? []
      : [
          ...(ios ? [appiumDriver("appium-xcuitest", "Appium driver: xcuitest (iOS)", installed.out, "xcuitest")] : []),
          appiumDriver("appium-uiautomator2", "Appium driver: uiautomator2 (Android)", installed.out, "uiautomator2"),
        ]),
  ];
}

function appiumDriver(id: PreflightId, label: string, listed: string, name: string): PreflightItem {
  if (listed.includes(name)) {
    return { id, label, required: false, status: "ok", source: "path" };
  }
  return {
    id,
    label,
    required: false,
    status: "missing",
    detail: `Appium is installed and this driver is not, so a session against ${
      name === "xcuitest" ? "an iOS simulator" : "an Android emulator"
    } would fail when it was created.`,
    remediation: `Install it if a task will drive ${name === "xcuitest" ? "an iOS simulator" : "an Android emulator"}.`,
    command: `appium driver install ${name}`,
  };
}

/**
 * Is something already serving Appium on the shared port?
 *
 * Only reported: the backend asks the same question itself before it starts a
 * hub, so a user who runs their own Appium — with their own drivers and
 * plugins — never has it fight them for 4723.
 */
export async function appiumHubIsAnswering(): Promise<boolean> {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), APPIUM_STATUS_TIMEOUT_MS);
  try {
    const res = await fetch(`${APPIUM_BASE_URL}/status`, { signal: controller.signal });
    return res.ok;
  } catch {
    return false;
  } finally {
    clearTimeout(timer);
  }
}

/**
 * The Android SDK, as two binaries with two different consequences.
 *
 * `adb` is how an emulator is talked to and is therefore the one that matters;
 * `emulator` is how one is STARTED, and a Mac without it can still drive an AVD
 * somebody launched from Android Studio. That distinction is reported rather
 * than collapsed, because "you cannot do Android" and "you cannot start an
 * Android device yourself" send a person to two different places.
 *
 * The paths are looked up here and passed to the runner, like `claude` and
 * `git`: a second search on the Go side with slightly different rules is how a
 * Mac drives one adb and reports another.
 */
function probeAndroidSdk(dirs: { dir: string; source: PreflightSource }[]): PreflightItem {
  const adb = which("adb", dirs) ?? androidSdkTool("platform-tools", "adb");
  const emulator = which("emulator", dirs) ?? androidSdkTool("emulator", "emulator");

  if (!adb) {
    return {
      id: "android-sdk",
      label: "Android SDK",
      required: false,
      status: "missing",
      detail: `Without adb this machine cannot reach an Android emulator.${process.platform === "darwin" ? " iOS simulators are unaffected." : ""}`,
      remediation:
        "Install the Android SDK if a task will drive an Android emulator — it needs both platform-tools (adb) and emulator.",
    };
  }
  if (!emulator) {
    return {
      id: "android-sdk",
      label: "Android SDK",
      required: false,
      status: "unusable",
      path: adb.path,
      source: adb.source,
      detail:
        "adb is here and the `emulator` binary is not, so this machine can drive an AVD that is already running and " +
        "cannot start one itself.",
      remediation: "Install the emulator package from Android Studio's SDK Manager to start AVDs from a task.",
    };
  }
  return { id: "android-sdk", label: "Android SDK", required: false, status: "ok", path: adb.path, source: adb.source };
}

/**
 * The SDK's own install locations, for the common case where it is not on PATH.
 *
 * Android Studio installs the tools and does not touch the shell profile, so
 * "not on PATH" is the ordinary state rather than the broken one — and a
 * GUI-launched .app has an even shorter PATH than the terminal the user tested
 * in.
 */
/** Where Android Studio puts the SDK when nobody overrode it, per platform. */
export function androidSdkDefaultRoot(): string {
  switch (process.platform) {
    case "win32":
      return path.join(process.env.LOCALAPPDATA ?? path.join(os.homedir(), "AppData", "Local"), "Android", "Sdk");
    case "linux":
      return path.join(os.homedir(), "Android", "Sdk");
    default:
      return path.join(os.homedir(), "Library", "Android", "sdk");
  }
}

/** `<root>/platform-tools/adb` — `adb.exe` on Windows, where the SDK's binaries carry the extension. */
export function androidToolPath(root: string, dir: string, name: string): string {
  return path.join(root, dir, process.platform === "win32" ? `${name}.exe` : name);
}

function androidSdkTool(dir: string, name: string): { path: string; source: PreflightSource } | null {
  const roots: string[] = [];
  for (const key of ["ANDROID_HOME", "ANDROID_SDK_ROOT"]) {
    const value = (process.env[key] ?? "").trim();
    if (value !== "") roots.push(value);
  }
  roots.push(androidSdkDefaultRoot());
  for (const root of roots) {
    const candidate = androidToolPath(root, dir, name);
    if (isExecutable(candidate)) return { path: candidate, source: "home" };
  }
  return null;
}

/**
 * The SDK root the tools that were FOUND belong to, derived from the adb path
 * rather than read from a setting.
 *
 * `<root>/platform-tools/adb` → `<root>`. A root that disagrees with the adb
 * being run is worse than no root at all: the emulator would load one SDK's
 * system images and adb would talk to another's server.
 */
export function androidRootFrom(adbPath: string | undefined): string {
  if (!adbPath) return "";
  const toolsDir = path.dirname(adbPath);
  if (path.basename(toolsDir) !== "platform-tools") return "";
  return path.dirname(toolsDir);
}

/** The `emulator` binary, wherever the sweep found it. */
export function emulatorPathFor(report: PreflightReport): string {
  const sdk = itemById(report, "android-sdk");
  if (!sdk || sdk.status !== "ok") return "";
  const root = androidRootFrom(sdk.path);
  const candidate = root === "" ? "" : androidToolPath(root, "emulator", "emulator");
  if (candidate !== "" && isExecutable(candidate)) return candidate;
  return which("emulator")?.path ?? "";
}

// --- the whole sweep --------------------------------------------------------

export interface PreflightOptions {
  overrides?: Overrides;
  /**
   * `--version` answers remembered between launches. Absent: every binary is
   * asked, every time — which is what the tests want.
   */
  cache?: PreflightCache;
  /** Ask every binary again, cached or not: "Check again" after an install must see the install. */
  force?: boolean;
  /**
   * A login-shell PATH is already in hand — one an earlier launch recorded —
   * so the gating half need not wait for this launch's own login shell. The
   * complete half always waits for it, and locates everything again if it
   * turned out different.
   */
  loginPathKnown?: boolean;
}

/** One sweep, in its two halves. */
export interface PreflightRun {
  /**
   * What spawning the backend depends on, and nothing slower: where every
   * binary is (lookups, no processes), the two required items, and whatever
   * versions the cache already knows. No `claude-account` row and no Appium
   * driver rows — those are answers only a process can give.
   */
  gating: Promise<PreflightReport>;
  /** Every check, including the ones that run a process. What the setup screen shows. */
  complete: Promise<PreflightReport>;
}

/** Where everything is, as one sweep's lookups found it. */
interface Discovery {
  /** The login PATH the lookups were made with, so the complete half can tell if it moved. */
  loginPath: string;
  agentServer: PreflightItem;
  postgres: PreflightItem;
  chrome: PreflightItem;
  androidSdk: PreflightItem;
  git: Located | null;
  claude: Located | null;
  /** Undefined off macOS, where there is no such item at all. */
  xcode: Located | null | undefined;
  appium: Located | null;
  ios: boolean;
  clis: [SimpleCli, Located | null][];
}

const loginPathKey = (): string => knownLoginShellPath().join("\0");

function discover(overrides: Overrides): Discovery {
  const dirs = searchDirs();
  const find = (override: string | undefined, name: string): Located | null =>
    which(override && override !== "" ? override : name, dirs);
  return {
    loginPath: loginPathKey(),
    agentServer: probeAgentServer(),
    postgres: probePostgres(),
    chrome: probeChrome(overrides.chromeBin),
    androidSdk: probeAndroidSdk(dirs),
    git: find(overrides.gitBin, "git"),
    claude: find(overrides.claudeBin, "claude"),
    xcode: process.platform === "darwin" ? (which("xcodebuild", dirs) ?? which("xcrun", dirs)) : undefined,
    appium: find(overrides.appiumBin, "appium"),
    ios: process.platform === "darwin",
    clis: simpleClis().map((cli): [SimpleCli, Located | null] => [cli, which(cli.binaryName, dirs)]),
  };
}

/** The list in its fixed order: the order a person should fix things in. */
function assemble(
  d: Discovery,
  parts: {
    git: PreflightItem;
    claude: PreflightItem;
    account?: PreflightItem;
    xcode?: PreflightItem;
    appium: PreflightItem[];
    clis: PreflightItem[];
  },
): PreflightReport {
  const items: PreflightItem[] = [
    d.agentServer,
    d.postgres,
    parts.git,
    parts.claude,
    ...(parts.account ? [parts.account] : []),
    d.chrome,
    ...(parts.xcode ? [parts.xcode] : []),
    ...parts.appium,
    d.androidSdk,
    ...parts.clis,
  ];
  return { generatedAt: Date.now(), items, ready: itemsAreReady(items) };
}

function gatingReport(d: Discovery, cache: PreflightCache | undefined): PreflightReport {
  const cached = (found: Located | null): VersionAnswer | undefined => {
    const hit = found ? cache?.version(found.path) : undefined;
    return hit ? { code: hit.code, out: hit.out } : undefined;
  };
  return assemble(d, {
    git: gitItem(d.git, cached(d.git)),
    claude: claudeItem(d.claude, cached(d.claude)),
    ...(d.xcode !== undefined ? { xcode: xcodeItem(d.xcode, cached(d.xcode)) } : {}),
    appium: appiumItems(d.appium, d.ios, { version: cached(d.appium) }),
    clis: d.clis.map(([cli, found]) => simpleCliItem(cli, found, cached(found))),
  });
}

/** How an answer is remembered: kept for a day, kept but asked again every sweep, or not at all. */
type Remember = "keep" | "recheck" | "skip";

const answered = (answer: RunResult): Remember => (answer.code === 0 ? "keep" : "skip");

/**
 * A `claude` this app can drive is remembered; one it cannot is remembered
 * too, but asked again every sweep. Remembered, so the next launch's gating
 * half starts the backend on the same answer this one ended on rather than on
 * a provisional "ok" it would have to restart over. Asked again, so updating
 * the CLI — or fixing the node its shebang needs, which does not touch the
 * binary — is seen in the very next sweep.
 */
const claudeAnswered = (answer: RunResult): Remember => {
  const version = answer.code === 0 ? parseVersion(answer.out) : null;
  return version !== null && compareVersions(version, MIN_CLAUDE_VERSION) >= 0 ? "keep" : "recheck";
};

async function versionOf(
  found: Located | null,
  opts: PreflightOptions,
  remember: (answer: RunResult) => Remember = answered,
): Promise<VersionAnswer | undefined> {
  if (!found) return undefined;
  const hit = opts.force ? undefined : opts.cache?.version(found.path);
  if (hit?.fresh) return { code: hit.code, out: hit.out };
  const answer = await run(found.path, ["--version"]);
  const how = remember(answer);
  // A failure is not remembered, and neither is the success it contradicts:
  // a later unforced sweep would show that one as if nothing had changed.
  if (how === "skip") opts.cache?.forgetVersion(found.path);
  else opts.cache?.setVersion(found.path, answer, { recheck: how === "recheck" });
  return { code: answer.code, out: answer.out };
}

async function completeReport(d: Discovery, opts: PreflightOptions): Promise<PreflightReport> {
  // The account probe starts the moment the CLI's own answer is in — at once
  // when that answer is cached — rather than after the slowest probe of all.
  const claude = versionOf(d.claude, opts, claudeAnswered).then((answer) => claudeItem(d.claude, answer));
  const account = claude.then(probeClaudeAccount);
  const appium = d.appium
    ? Promise.all([
        versionOf(d.appium, opts),
        run(d.appium.path, ["driver", "list", "--installed"]),
        appiumHubIsAnswering(),
      ]).then(([version, installed, hubUp]) => appiumItems(d.appium, d.ios, { version, installed, hubUp }))
    : Promise.resolve(appiumItems(null, d.ios));

  const [git, claudeDone, accountDone, xcode, appiumDone, clis] = await Promise.all([
    versionOf(d.git, opts).then((answer) => gitItem(d.git, answer)),
    claude,
    account,
    d.xcode === undefined ? Promise.resolve(undefined) : versionOf(d.xcode, opts).then((answer) => xcodeItem(d.xcode ?? null, answer)),
    appium,
    Promise.all(d.clis.map(([cli, found]) => versionOf(found, opts).then((answer) => simpleCliItem(cli, found, answer)))),
  ]);

  return assemble(d, {
    git,
    claude: claudeDone,
    account: accountDone,
    ...(xcode ? { xcode } : {}),
    appium: appiumDone,
    clis,
  });
}

/**
 * Start a sweep, in two halves.
 *
 * The gating half is lookups and nothing else, so the backend's start waits
 * for a few milliseconds of `stat` calls rather than for a login shell, a
 * `--version` per CLI, `appium driver list` and `claude auth status` (each a
 * Node process, several hundred milliseconds apiece, and the last one network
 * bound). Everything the backend's environment is built from is decided there:
 * `agentServerEnv` reads where the CLIs are, never the account — see
 * supervisor/env.ts.
 *
 * The complete half runs every probe, in parallel, because they are
 * independent and the slowest would otherwise set the floor for all of them.
 *
 * The embedding engine is not a check here. It is the bundled `embedder` child
 * (`main/supervisor/supervisor.ts`), started unconditionally at app init rather
 * than detected — there is nothing for a user to install or fix, so a download
 * in progress is not modelled as a preflight item at all. A stuck download
 * surfaces as that child's own status on Diagnostics, the same non-blocking
 * mechanism Appium's crash or absence already uses.
 */
export function startPreflight(opts: PreflightOptions = {}): PreflightRun {
  const overrides = opts.overrides ?? {};
  // Once per process, bounded, and empty on any failure; `which()` and the
  // probe environment read what it found.
  const loginPath = loginShellPath();
  const located = (async () => {
    if (!opts.loginPathKnown) await loginPath;
    return discover(overrides);
  })();
  const gating = located.then((d) => gatingReport(d, opts.cache));
  const complete = located.then(async (d) => {
    await loginPath;
    // A PATH that moved since the lookups (a seeded one that went stale) means
    // the lookups are looked up again; the probes then run against this
    // launch's own PATH either way.
    return completeReport(d.loginPath === loginPathKey() ? d : discover(overrides), opts);
  });
  // Callers that only want the complete half must not see an unhandled
  // rejection from the other one.
  gating.catch(() => undefined);
  return { gating, complete };
}

/** Run every check and assemble the whole report: `startPreflight`'s complete half. */
export function preflight(opts: PreflightOptions = {}): Promise<PreflightReport> {
  return startPreflight(opts).complete;
}

/** What the account-mode sweep needs beyond what the local one does. */
export interface AccountPreflightOptions extends PreflightOptions {
  /**
   * The ids of the providers in `providers.bin` — never their keys. Absent
   * reads as none.
   */
  providerIds?: readonly string[];
}

/**
 * Can this computer run an agent at all? Account mode only, as the `api-keys`
 * row.
 *
 * A member runs tasks with their own API keys (the executor), or with an
 * agent CLI the runner starts: Claude Code signed in to a plan that includes
 * it, OpenCode or Cursor. Any one is enough. The row is REQUIRED only when it
 * is the last way left — with no key and no usable CLI the runner would
 * attach and then answer every run with not_ready, so Connect is refused here
 * instead, with the sentence that fixes it. Keys need the executor, which is
 * what uses them.
 */
export function agentAccessItem(input: {
  providerIds: readonly string[];
  executor: PreflightItem | undefined;
  claude: PreflightItem | undefined;
  claudeAccount: PreflightItem | undefined;
  clis: readonly PreflightItem[];
  platform?: NodeJS.Platform;
}): PreflightItem {
  const ok = (item: PreflightItem | undefined): boolean => item?.status === "ok";
  const cliReady =
    (ok(input.claude) && ok(input.claudeAccount)) ||
    input.clis.some((cli) => (cli.id === "opencode" || cli.id === "cursor-agent") && ok(cli));
  const menu = (input.platform ?? process.platform) === "darwin" ? "menu bar" : "system tray";
  const where = `API keys… in the TaskTrooper ${menu} menu`;
  const base = { id: "api-keys" as const, label: "API keys", required: !cliReady };
  const count = input.providerIds.length;

  if (count > 0 && ok(input.executor)) {
    return {
      ...base,
      status: "ok",
      detail: `${count === 1 ? "One provider" : `${count} providers`} on this computer: ${input.providerIds.join(", ")}.`,
    };
  }
  if (count > 0) {
    return {
      ...base,
      status: "unusable",
      detail: "API keys are set, but this copy of TaskTrooper has no executor to run agents with them.",
      remediation: cliReady
        ? "Tasks that use an API provider wait until the executor is back. Reinstall TaskTrooper from the latest build."
        : "Reinstall TaskTrooper from the latest build, or install Claude Code, OpenCode or Cursor to run tasks with.",
    };
  }
  return {
    ...base,
    status: "missing",
    detail: "No API key is set on this computer. Keys stay here; TaskTrooper's servers never see them.",
    remediation: cliReady
      ? `Tasks that use an API provider wait until one is added under ${where}.`
      : `Add a key under ${where}, or install Claude Code (signed in), OpenCode or Cursor. Tasks need one of them to run here.`,
  };
}

/**
 * The preflight for account mode: no local backend, no embedded Postgres, no
 * Antigravity (the runner has no flavor for it). The runner binary stands in
 * for `agent-server`, and `git` is REQUIRED — the runner refuses to start
 * without `git_bin`. No agent CLI is required on its own: a member may work
 * with their own API keys alone, and `agentAccessItem` (the `api-keys` row)
 * is what refuses a computer that has no way to run an agent at all.
 *
 * The executor is optional: without it the runner answers agent.run with
 * not_ready, and the CLI runs still work. Claude Code, the mobile toolchain
 * and the other two host-executed CLIs stay optional too, and their presence
 * is how `main/runner/env.ts#runnerConfig` learns the paths it sends only
 * when detected — reusing this sweep rather than a second, narrower one keeps
 * those paths from drifting out of step with what the preflight screen showed.
 */
export async function accountPreflight(opts: AccountPreflightOptions): Promise<PreflightReport> {
  const overrides = opts.overrides ?? {};
  const dirs = searchDirs();
  const find = (override: string | undefined, name: string): Located | null =>
    which(override && override !== "" ? override : name, dirs);
  const gitBin = find(overrides.gitBin, "git");
  const claudeBin = find(overrides.claudeBin, "claude");
  // Read before the first await: iOS simulators exist only on macOS, and
  // xcuitest is a missing row anywhere else that no install can fix.
  const ios = process.platform === "darwin";
  const xcodeBin = ios ? (which("xcodebuild", dirs) ?? which("xcrun", dirs)) : undefined;
  const appiumBin = find(overrides.appiumBin, "appium");
  const clis = simpleClis()
    .filter((cli) => cli.id !== "agy")
    .map((cli): [SimpleCli, Located | null] => [cli, which(cli.binaryName, dirs)]);

  const claude = versionOf(claudeBin, opts, claudeAnswered).then((answer) => claudeItem(claudeBin, answer));
  const account = claude.then(probeClaudeAccount);
  const appium = appiumBin
    ? Promise.all([
        versionOf(appiumBin, opts),
        run(appiumBin.path, ["driver", "list", "--installed"]),
        appiumHubIsAnswering(),
      ]).then(([version, installed, hubUp]) => appiumItems(appiumBin, ios, { version, installed, hubUp }))
    : Promise.resolve(appiumItems(null, ios));

  const [git, claudeDone, accountDone, xcode, appiumDone, cliItems] = await Promise.all([
    versionOf(gitBin, opts).then((answer) => gitItem(gitBin, answer)),
    claude,
    account,
    xcodeBin === undefined ? Promise.resolve(undefined) : versionOf(xcodeBin, opts).then((answer) => xcodeItem(xcodeBin, answer)),
    appium,
    Promise.all(clis.map(([cli, found]) => versionOf(found, opts).then((answer) => simpleCliItem(cli, found, answer)))),
  ]);

  const executor = probeBundled("executor", "TaskTrooper executor", EXECUTOR_BINARY, "build:executor", false);
  const items: PreflightItem[] = [
    probeBundled("runner", "TaskTrooper runner", RUNNER_BINARY, "build:runner", true),
    executor,
    { ...git, required: true },
    agentAccessItem({
      providerIds: opts.providerIds ?? [],
      executor,
      claude: claudeDone,
      claudeAccount: accountDone,
      clis: cliItems,
    }),
    claudeDone,
    accountDone,
    ...(xcode ? [xcode] : []),
    ...appiumDone,
    probeAndroidSdk(dirs),
    ...cliItems,
  ];

  return { generatedAt: Date.now(), items, ready: itemsAreReady(items) };
}

/**
 * Whether every REQUIRED item passed. Optional ones never count.
 *
 * A named predicate rather than an inline `every`, so a test can assert the
 * rule on reports it constructs — including ones the probes cannot produce on
 * the machine the suite happens to run on. The version inlined in `preflight`
 * could only be asserted by restating it, which is a test that cannot fail.
 */
export function itemsAreReady(items: PreflightItem[]): boolean {
  return items.every((item) => !item.required || item.status === "ok");
}

/** The same rule, for a whole report. */
export function preflightReady(report: PreflightReport): boolean {
  return itemsAreReady(report.items);
}

export function itemById(report: PreflightReport, id: PreflightId): PreflightItem | undefined {
  return report.items.find((item) => item.id === id);
}

/** An empty report, for the moment before the first sweep has run. */
export function emptyReport(): PreflightReport {
  return { generatedAt: 0, items: [], ready: false };
}

/**
 * The first REQUIRED item that is not `ok`, as the blocker the supervisor
 * refuses to Connect on.
 *
 * Only required items produce one. Everything optional — Chrome, the Xcode
 * tools — is reported and never blocks, because interrupting someone about a
 * capability they may never use is worse than the capability quietly not being
 * there.
 */
export function firstBlocker(report: PreflightReport): Blocker | undefined {
  const failed = report.items.find((item) => item.required && item.status !== "ok");
  if (!failed) return undefined;
  const command = failed.command ?? "";
  return {
    id: failed.id,
    title: failed.status === "missing" ? `${failed.label} is missing` : `${failed.label} cannot be used`,
    because: failed.detail ?? `${failed.label} is required to run tasks on this machine.`,
    remediation: failed.remediation ?? `Fix ${failed.label} and try again.`,
    command,
    // Only a command this app could run unattended and the user can read first.
    // Anything pointing at a GUI download or an account change is shown, never
    // run.
    runnable: command.startsWith("npm install") || command.startsWith("brew install"),
  };
}
