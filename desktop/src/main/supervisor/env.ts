import path from "node:path";
import type { PreflightReport } from "../../ipc/types.js";
import { androidRootFrom, APPIUM_BASE_URL, itemById } from "../services/detect.js";

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

/**
 * The environment every child inherits: this process's, minus what would leak
 * Electron's own state into it, plus the PATH prefixes a GUI-launched .app does
 * not inherit, plus the Android SDK root the tools that were FOUND belong to.
 *
 * The backend SPAWNS `claude`, `git` and, for a mobile task, `adb` and
 * `emulator`. `claude` is a Node program whose shebang is `#!/usr/bin/env node`
 * and launchd's PATH has no node in it; `emulator` finds its system images
 * through `ANDROID_HOME`, and an emulator started without one comes up and
 * cannot find an image to boot.
 *
 * The root is DERIVED from the adb that detection actually found, never asked
 * for. A root that disagrees with the adb being run is worse than no root at
 * all: the emulator would load one SDK's images and adb would talk to
 * another's server.
 */
export function childEnv(preflight: PreflightReport): NodeJS.ProcessEnv {
  const env: NodeJS.ProcessEnv = { ...process.env };

  // Electron sets these for its own child processes. ELECTRON_RUN_AS_NODE in
  // particular makes any Node-based CLI a child launches behave as if it were
  // Electron, which is a failure with no useful message — and Appium is exactly
  // such a CLI.
  delete env.ELECTRON_RUN_AS_NODE;
  delete env.ELECTRON_IS_DEV;
  delete env.NODE_OPTIONS;

  // Windows spells it Path, and a spread of process.env loses the
  // case-insensitive lookup that made the spelling not matter.
  const pathKey = Object.keys(env).find((k) => k.toUpperCase() === "PATH") ?? "PATH";
  const parts = (env[pathKey] ?? "").split(path.delimiter).filter((p) => p !== "");
  for (const extra of process.platform === "darwin" ? ["/opt/homebrew/bin", "/usr/local/bin"] : []) {
    if (!parts.includes(extra)) parts.push(extra);
  }

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
  "ALLOWED_ROOTS",
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
    // Sent whenever Appium is INSTALLED, whether this app started the hub or
    // adopted one already on the port — it is the same hub either way.
    ...(appium?.status === "ok" ? { MOBILE_APPIUM_HUB_URL: APPIUM_BASE_URL } : {}),
  };
}

/**
 * Appium's argv. Two flags, both fixed.
 *
 * `--address 127.0.0.1` is not a default worth relying on: Appium binds every
 * interface unless told otherwise, and a hub on 0.0.0.0 is a remote-control
 * interface for every device attached to somebody's laptop, reachable from the
 * coffee shop's network.
 */
export function appiumArgs(): string[] {
  const url = new URL(APPIUM_BASE_URL);
  return ["--address", url.hostname, "--port", url.port];
}
