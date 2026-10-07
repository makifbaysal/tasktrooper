import { existsSync, realpathSync } from "node:fs";
import path from "node:path";
import type { PreflightId, PreflightReport } from "../../ipc/types.js";
import { catalogRoot } from "../services/app-scheme.js";
import { androidRootFrom, APPIUM_BASE_URL, itemById } from "../services/detect.js";
import { knownLoginShellPath } from "../services/login-env.js";
import { binaryDirs, mergePath, pathKeyOf, splitPath, withoutAppImage } from "../services/process-env.js";

/**
 * What the children are started with.
 *
 * The backend reads its whole configuration from its environment and scrubs the
 * secrets out of its own `environ` once it has, which is why this is env and
 * not a pipe: on macOS a same-user process can read another's environment
 * through KERN_PROCARGS2, and the window where that matters is the window
 * before the backend has read and cleared it.
 *
 * No shell is involved on any path. Values go from the Keychain into `spawn`'s
 * env as bytes, so a `$` or a backtick in any of them is a `$` or a backtick.
 */

/** The CLIs a child may exec by path, whose directories its PATH must reach. */
const CLI_IDS: PreflightId[] = ["claude", "opencode", "cursor-agent", "agy", "appium", "git"];

/**
 * The environment every child inherits: this process's, minus what would leak
 * Electron's own state into it, plus the PATH prefixes a GUI-launched .app does
 * not inherit, plus the Android SDK root the tools that were FOUND belong to.
 *
 * The backend SPAWNS `claude`, `git` and, for a mobile task, `adb`, `emulator`
 * and the Appium hub. `claude` and `appium` are Node programs whose shebang is
 * `#!/usr/bin/env node` and launchd's PATH has no node in it; `emulator` finds
 * its system images through `ANDROID_HOME`, and an emulator started without one
 * comes up and cannot find an image to boot. So the login shell's PATH is appended, and so
 * is the directory of every CLI detection found — the node an npm-installed CLI
 * was installed with lives beside it. Appended, not prepended: every agent
 * command and verify stage inherits this PATH, and putting a CLI's directory
 * first would silently swap the user's node, python or ruby for whatever
 * shares that directory (a Homebrew git pulling /opt/homebrew/bin ahead of
 * pyenv, an old nvm node ahead of the current one).
 *
 * The root is DERIVED from the adb that detection actually found, never asked
 * for. A root that disagrees with the adb being run is worse than no root at
 * all: the emulator would load one SDK's images and adb would talk to
 * another's server.
 *
 * `ownBinary` is for a child that is this app's own executable (the embedder):
 * inside an AppImage it needs the bundled libraries AppRun pointed it at, which
 * every other program must not see.
 */
export function childEnv(preflight: PreflightReport, opts: { ownBinary?: boolean } = {}): NodeJS.ProcessEnv {
  const env: NodeJS.ProcessEnv = opts.ownBinary ? { ...process.env } : withoutAppImage({ ...process.env });

  // Electron sets these for its own child processes. ELECTRON_RUN_AS_NODE in
  // particular makes any Node-based CLI a child launches behave as if it were
  // Electron, which is a failure with no useful message — and Appium is exactly
  // such a CLI.
  delete env.ELECTRON_RUN_AS_NODE;
  delete env.ELECTRON_IS_DEV;
  delete env.NODE_OPTIONS;

  const pathKey = pathKeyOf(env);
  const extras = process.platform === "darwin" ? ["/opt/homebrew/bin", "/usr/local/bin"] : [];
  const found = CLI_IDS.map((id) => itemById(preflight, id)).map((item) => (item?.status === "ok" ? item.path : undefined));
  const parts = mergePath([splitPath(env[pathKey]), extras, knownLoginShellPath(), binaryDirs(found)]);

  const root = androidRootFrom(itemById(preflight, "android-sdk")?.path);
  if (root !== "") {
    env.ANDROID_HOME = root;
    env.ANDROID_SDK_ROOT = root;
    // Appium's uiautomator2 driver shells out to adb by name, so the SDK's own
    // directories go on the FRONT: a second adb earlier on PATH would drive a
    // different server than the one this app reported.
    parts.unshift(path.join(root, "platform-tools"), path.join(root, "emulator"));
  }

  env[pathKey] = parts.join(path.delimiter);
  return env;
}

export const SERVER_CONTRACT_KEYS = [
  "DATABASE_URL",
  "PORT",
  "SHUTDOWN_ON_STDIN_CLOSE",
  "DATA_DIR",
  "CONFIG_PATH",
  "SERVER_API_KEY",
  "MCP_SECRETS_KEY",
  "PUBLIC_BASE_URL",
  "CORS_ORIGINS",
  "CLAUDE_CODE_BIN",
  "CURSOR_AGENT_BIN",
  "ANTIGRAVITY_BIN",
  "OPENCODE_BIN",
  "EMBEDDINGS_BASE_URL",
  "EMBEDDED_POSTGRES_CACHE_DIR",
  "CHROME_BIN",
  "MOBILE_APPIUM_HUB_URL",
  "APPIUM_BIN",
  "ALLOWED_ROOTS",
  "AGENT_CATALOG_REPO",
] as const;

export interface AgentServerEnvInputs {
  preflight: PreflightReport;
  /** Where the backend keeps its database, its RAG files and its workspaces. */
  dataDir: string;
  /** Where zonky's Postgres binaries are downloaded and extracted. */
  postgresCacheDir: string;
  /** `SERVER_API_KEY`: the bearer the UI sends, generated once per install. */
  apiToken: string;
  /** `MCP_SECRETS_KEY`: stable for the life of the install, or stored rows stop decrypting. */
  mcpSecretsKey: string;
  /** The embedder child's resolved loopback URL, when it has bound one. */
  embeddingsBaseURL?: string;
}

/**
 * The backend's environment.
 *
 * `PORT=0` on purpose: the backend picks a free port and prints it, so nothing
 * here has to guess one or keep two settings in agreement. `DATABASE_URL` is
 * deliberately absent — empty means embedded Postgres, which is the whole point
 * of the local mode.
 *
 * Every optional path is OMITTED rather than sent empty. The backend treats an
 * absent one as "this Mac cannot do that" and says so with a sentence; an empty
 * string would become an exec of "" inside a request, minutes later.
 */
export function agentServerEnv(inputs: AgentServerEnvInputs): NodeJS.ProcessEnv {
  const { preflight } = inputs;
  const claude = itemById(preflight, "claude");
  const cursorAgent = itemById(preflight, "cursor-agent");
  const antigravity = itemById(preflight, "agy");
  const opencode = itemById(preflight, "opencode");
  const chrome = itemById(preflight, "chrome");
  const appium = itemById(preflight, "appium");

  // Launched from a terminal, the app inherits that shell's environment. An
  // exported DATABASE_URL would silently point the backend at someone else's
  // database instead of its embedded one, so every key the backend reads is
  // this function's to set or to leave absent — never the parent shell's.
  const inherited = childEnv(preflight);
  for (const key of SERVER_CONTRACT_KEYS) delete inherited[key];

  const toEnvPath = (p?: string): string | undefined =>
    p && process.platform === "win32" ? p.replace(/\\/g, "/") : p;

  return {
    ...inherited,
    PORT: "0",
    SHUTDOWN_ON_STDIN_CLOSE: "1",
    DATA_DIR: toEnvPath(inputs.dataDir)!,
    EMBEDDED_POSTGRES_CACHE_DIR: toEnvPath(inputs.postgresCacheDir)!,
    SERVER_API_KEY: inputs.apiToken,
    MCP_SECRETS_KEY: inputs.mcpSecretsKey,
    ALLOWED_ROOTS: process.env.TASKTROOPER_ALLOWED_ROOTS ?? "*",
    ...(claude?.status === "ok" && claude.path ? { CLAUDE_CODE_BIN: toEnvPath(claude.path) } : {}),
    ...(cursorAgent?.status === "ok" && cursorAgent.path ? { CURSOR_AGENT_BIN: toEnvPath(cursorAgent.path) } : {}),
    ...(antigravity?.status === "ok" && antigravity.path ? { ANTIGRAVITY_BIN: toEnvPath(antigravity.path) } : {}),
    ...(opencode?.status === "ok" && opencode.path ? { OPENCODE_BIN: toEnvPath(opencode.path) } : {}),
    ...(inputs.embeddingsBaseURL ? { EMBEDDINGS_BASE_URL: inputs.embeddingsBaseURL } : {}),
    ...(chrome?.status === "ok" && chrome.path ? { CHROME_BIN: toEnvPath(chrome.path) } : {}),
    // The six role agents ship as the bundled catalog (Resources/catalog, or
    // the monorepo's catalog/ in dev) and the backend syncs them from it at
    // boot. Omitted when a checkout has no catalog yet — same "optional path
    // omitted, never empty" convention as the rest of this map.
    ...(existsSync(catalogRoot()) ? { AGENT_CATALOG_REPO: catalogRoot() } : {}),
    // Sent whenever Appium is INSTALLED. The backend starts the hub on that
    // address the first time a mobile tool needs it and stops it when idle, or
    // uses one somebody already runs there — the same hub either way.
    ...(appium?.status === "ok" && appium.path
      ? { MOBILE_APPIUM_HUB_URL: APPIUM_BASE_URL, APPIUM_BIN: toEnvPath(appium.path) }
      : {}),
  };
}

/**
 * The facts `agentServerEnv` and `childEnv` take from a preflight report,
 * reduced to a string two reports can be compared by.
 *
 * The backend is spawned on the GATING half of a sweep — lookups and cached
 * versions — and that half can be wrong in two ways the complete half then
 * corrects: a `claude` whose version had not been asked yet (and turns out
 * too old), and a remembered login PATH that has since moved. When the two
 * halves disagree on THIS, the environment the backend was given is wrong and
 * the supervisor restarts it; when they agree, nothing it reads changed.
 *
 * Paths are compared after resolving symlinks, because a version manager that
 * puts a per-shell directory on PATH (fnm does) names the same binary
 * differently on every launch. The login PATH itself is left out for the same
 * reason: it is not the same string twice for those users, and a backend
 * restart on every launch is a far worse price than a PATH one launch stale.
 * The account is left out because nothing here reads it.
 */
export function spawnFingerprint(preflight: PreflightReport): string {
  const canonical = (p: string): string => {
    try {
      return realpathSync(p);
    } catch {
      return p;
    }
  };
  const usable = (id: PreflightId): string | null => {
    const item = itemById(preflight, id);
    return item?.status === "ok" && item.path ? canonical(item.path) : null;
  };
  const androidPath = itemById(preflight, "android-sdk")?.path;
  return JSON.stringify([
    ...[...CLI_IDS, "chrome" as const].map(usable),
    androidPath ? androidRootFrom(canonical(androidPath)) || androidRootFrom(androidPath) : null,
  ]);
}
