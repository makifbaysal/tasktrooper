/**
 * Every IPC channel this app has, in one list.
 *
 * The list is exhaustive on purpose. `contextIsolation` and a sandboxed
 * renderer are only worth having if the surface they gate is small enough to
 * read, and a preload that forwards arbitrary channel names re-opens exactly
 * the hole those two settings close. So: named channels, typed payloads,
 * validated in the main process before anything acts on them.
 *
 * Three namespaces:
 *
 *   SHELL_* — the native chrome: a title bar with a status pill, a reload
 *             button, the update affordance and the screen that says so when
 *             the backend did not come up. It asks for almost nothing.
 *   KEYS_*  — the API-key window, this app's own page in a window of its own
 *             (`main/keys/`). The only channels that take a secret in, and
 *             the only page that may call them; none sends one back.
 *   CLOUD_* — the WebContentsView running the web app: our own bundle from
 *             `app://tasktrooper` in local mode, the account's web app from
 *             its origin in account mode. It is the widest half of the surface
 *             — Connect, the workspace picker, the preflight, pairing — so
 *             every handler on it verifies the sender on every call, not once
 *             at load, against the one origin the current mode trusts. See
 *             main/ipc.ts.
 */

import type {
  AccountState,
  AppInfo,
  ChildId,
  CloudStatus,
  Diagnostics,
  LogLine,
  PreflightReport,
  McpServersState,
  ProviderKeysState,
  ProviderKeyType,
  RunnerPairingBundle,
  RunnerPairingSummary,
  SupervisorSnapshot,
  UpdateStatus,
} from "./types.js";
import type {
  HostLogsRequest,
  HostOverrides,
  HostPreferences,
  HostRunnerSnapshot,
  HostSettings,
  HostWorkspaceChoice,
} from "./host.js";

export const SHELL_CHANNELS = {
  appInfo: "shell:app:info",
  /** The supervisor state, for the status pill in the title bar. */
  supervisorGet: "shell:supervisor:get",
  /** Retry loading the web app after it failed. */
  cloudReload: "shell:cloud:reload",
  /** Whether the web app is loading, up, or unreachable. */
  cloudStatus: "shell:cloud:status",

  /**
   * Auto-update, for the shell's own popup and the tray. The page has the same
   * three on its bridge for Settings (see `CLOUD_CHANNELS.updateGet`). The
   * last two are the only shell channels that DO something, so both are
   * guarded on the sender being this window — see `main/ipc.ts`.
   */
  updateGet: "shell:update:get",
  updateCheck: "shell:update:check",
  updateRestart: "shell:update:restart",

  /**
   * Account mode, for the chrome's own failure screen: when the account's web
   * app cannot be reached, this runs TaskTrooper locally FOR NOW — the
   * account stays signed in and paired, and the next launch tries it again.
   * Guarded on the sender like the update pair.
   */
  accountState: "shell:account:state",
  accountUseLocalForNow: "shell:account:use-local-for-now",
} as const;

/**
 * The API-key window's three calls (`preload/keys.ts`). Guarded on the sender
 * being that window's own top frame on its own page — never the web app's
 * view, whichever origin it holds, and never the chrome.
 *
 * Keys go in on `set` and are never read back: every answer is the list with
 * `hasKey`, never a value.
 */
export const KEYS_CHANNELS = {
  list: "shell:keys:list",
  set: "shell:keys:set",
  remove: "shell:keys:remove",
  /** What the page that opened the window asked it to focus on; null when nothing. */
  prefill: "shell:keys:prefill",
  /** The member's own MCP servers, the window's second section. Same guard. */
  mcpList: "shell:keys:mcp-list",
  mcpSet: "shell:keys:mcp-set",
  mcpRemove: "shell:keys:mcp-remove",
} as const;

/** Main → the API-key window, one-way: the prefill changed while it was open. */
export const KEYS_EVENTS = {
  prefill: "shell:keys:event:prefill",
} as const;

/** Main → the native chrome. One-way; it never replies. */
export const SHELL_EVENTS = {
  supervisorState: "shell:event:supervisor-state",
  cloudStatus: "shell:event:cloud-status",
  updateStatus: "shell:event:update-status",
  /**
   * The window entered or left full screen. The chrome drops its drag strip
   * while it is: the web app's view then covers that strip, but macOS still
   * hit-tests the drag region of the window's own contents underneath, so
   * every click on the page's header became a window drag instead.
   */
  fullScreen: "shell:event:full-screen",
  accountState: "shell:event:account-state",
} as const;

/**
 * The web app's bridge.
 *
 * Every one of these is a named action with a fixed argument shape. There is no
 * generic invoke, no path the page may name, no command the page may compose.
 */
export const CLOUD_CHANNELS = {
  hostInfo: "cloud:host-info",

  /**
   * Where the page's own API calls go: the local backend's loopback origin,
   * with no path suffix.
   *
   * Answered synchronously, and one of two channels that are. The page reads it
   * on every request path, so it has to be a value by the time the first line
   * of app code runs — an async hop would put an await in front of every call
   * in the app, and a late arrival would mean the first requests went nowhere.
   */
  apiBase: "cloud:api-base",
  /**
   * The bearer for that backend. Synchronous for the same reason, and behind
   * the same sender check.
   *
   * This process generated the token and started the server with it; handing it
   * to the page this process also serves grants nothing that is not already
   * reachable on this machine by the user running the app.
   */
  apiToken: "cloud:api-token",

  /**
   * `"local"` or `"account"`, synchronous like the two above: a page needs to
   * know which world it is in before its first render — the account's web app
   * authenticates with its own cookie and must not look for `apiToken`.
   */
  accountMode: "cloud:account:mode",
  accountState: "cloud:account:state",
  /**
   * Leave local mode for the account at an origin (absent: the configured
   * one). Stops the local backend and loads the account's sign-in page in
   * this window. A no-op when already signed in there.
   */
  accountSignIn: "cloud:account:sign-in",
  /** Stop the runner, forget the pairing, clear the account session, run locally again. */
  accountSignOut: "cloud:account:sign-out",
  /**
   * Open the API-key window, optionally focused on one provider (`KeysPrefill`:
   * identity and address, never a key). Nothing comes back: the page — the
   * account's, from a remote origin — may bring the window up, and that is
   * all. The keys are typed into the window, which is this app's own.
   */
  accountOpenKeys: "cloud:account:open-keys",

  runnerSnapshot: "cloud:runner:snapshot",
  runnerConnect: "cloud:runner:connect",
  runnerDisconnect: "cloud:runner:disconnect",
  runnerRestartChild: "cloud:runner:restart-child",
  runnerLogs: "cloud:runner:logs",
  /**
   * Whether the page wants `CLOUD_EVENTS.runnerLogs` pushed. Sent by the
   * preload's `subscribeLogs` on the first subscriber and when the last one
   * leaves, so lines cross IPC only while a log view is open; the page catches
   * up on what it missed with `runnerLogs` and `afterSeq`.
   */
  runnerLogsStream: "cloud:runner:logs-stream",
  runnerClearLogs: "cloud:runner:clear-logs",

  /**
   * Account mode only. Pairing this computer is the same act as starting its
   * runner: `runnerPair` checks the bundle's origin against the account's,
   * stores it encrypted, and starts the runner. `runnerRestart` is
   * `restartChild` without a `ChildId` — account mode supervises one child.
   */
  runnerPair: "cloud:runner:pair",
  runnerUnpair: "cloud:runner:unpair",
  runnerPairingInfo: "cloud:runner:pairing-info",
  runnerRestart: "cloud:runner:restart",

  settingsGet: "cloud:settings:get",
  settingsSetPreferences: "cloud:settings:set-preferences",
  chooseWorkspace: "cloud:settings:choose-workspace",
  chooseDirectory: "cloud:dialog:choose-directory",
  reveal: "cloud:settings:reveal",

  /**
   * A PR link on a task card, opened without ever routing through the hosted
   * view's own navigation — see window.ts's `hardenCloudNavigation` for why
   * that matters: a `target="_blank"` anchor asks THIS process to decide, and
   * asking here means the answer can never be "the shell's loading screen,
   * forever" the way a navigation gone wrong could.
   */
  openExternal: "cloud:open-external",

  /**
   * Which agent-chat session, if any, is on screen right now — the main
   * process cannot see the SPA's own router, so the page has to say. Fired on
   * mount/session-change and on unmount with `null`; it is not a request/
   * response the page waits on, just a fact for the shell to keep.
   */
  chatReportFocus: "cloud:chat:report-focus",

  /**
   * The environment preflight — what this Mac can and cannot do, item by item,
   * with the sentence that fixes each one.
   *
   * Its own channel rather than a field on `diagnostics` because the two answer
   * different questions for different screens. Diagnostics is "what did this
   * app resolve, and where", read once by somebody debugging; the preflight is
   * the checklist a user works down, re-read after every fix.
   */
  preflight: "cloud:preflight:get",

  diagnostics: "cloud:diagnostics:get",
  overridesSet: "cloud:diagnostics:set-overrides",

  /**
   * Auto-update, for Settings → General. The page is this app's own bundle on
   * `app://tasktrooper`, the same code the chrome is. Restart only applies an
   * update Squirrel has already downloaded and signature-checked, and does
   * nothing when none is staged; the page cannot name a version, a feed or a
   * file.
   */
  updateGet: "cloud:update:get",
  updateCheck: "cloud:update:check",
  updateRestart: "cloud:update:restart",
} as const;

/** Main → the web app. Push, so the page never polls the supervisor. */
export const CLOUD_EVENTS = {
  runnerState: "cloud:event:runner-state",
  runnerLogs: "cloud:event:runner-logs",
  updateStatus: "cloud:event:update-status",
} as const;

export type ShellChannel = (typeof SHELL_CHANNELS)[keyof typeof SHELL_CHANNELS];
export type KeysChannel = (typeof KEYS_CHANNELS)[keyof typeof KEYS_CHANNELS];
export type ShellEvent = (typeof SHELL_EVENTS)[keyof typeof SHELL_EVENTS];
export type CloudChannel = (typeof CLOUD_CHANNELS)[keyof typeof CLOUD_CHANNELS];
export type CloudEvent = (typeof CLOUD_EVENTS)[keyof typeof CLOUD_EVENTS];

// --- payloads ---------------------------------------------------------------

export interface RestartChildRequest {
  child: ChildId;
}

export interface RevealRequest {
  /** A NAME, not a path — the main process supplies the path. */
  what: "workspace" | "previous-workspace" | "logs";
}

export interface OpenExternalRequest {
  url: string;
}

export interface LogsStreamRequest {
  on: boolean;
}

/** `null`/`null` means no chat screen is open. */
export interface ChatFocusRequest {
  agentId: string | null;
  sessionId: string | null;
}

export interface ChooseDirectoryRequest {
  title?: string;
  defaultPath?: string;
  buttonLabel?: string;
}

export interface DiagnosticsRequest {
  /** Re-probe rather than answering from the launch-time detection. */
  force?: boolean;
}

/**
 * The preflight, re-run or not.
 *
 * `force` matters more here than on diagnostics: the whole point of that screen
 * is that the user goes away, installs something, and comes back — so the
 * answer they get after that must be the one from AFTER, and a cached report
 * would tell them their fix did not work.
 */
export interface PreflightRequest {
  force?: boolean;
}

/** What `POST /api/runner/pair` returned. */
export interface PairRequest {
  bundle: RunnerPairingBundle;
}

export interface AccountSignInRequest {
  /** An origin, `https://host[:port]`; absent means the configured one. */
  origin?: string;
}

/**
 * Add a provider or change one. A built-in type's id is the type and may be
 * left out; a custom endpoint's id is a UUID — left out, a new one is made.
 * An absent or empty `api_key` keeps the stored one.
 */
export interface KeySetRequest {
  id?: string;
  type: ProviderKeyType;
  base_url?: string;
  models?: string[];
  api_key?: string;
}

/**
 * What the account's page may hand the key window: which provider to ask a key
 * for. Identity and address only — there is no key field, here or anywhere on
 * the way in. `id` is a built-in type name or a custom endpoint's UUID.
 */
export interface KeysPrefill {
  id: string;
  type: ProviderKeyType;
  base_url?: string;
  models?: string[];
  name?: string;
}

export interface KeyRemoveRequest {
  id: string;
}

/**
 * Add an MCP server or change one: a stdio `command` (with `args` and `env`)
 * or an http `url` (with `headers`). In `env` and `headers`, an empty value
 * keeps the stored one under that name and a name left out is removed, so a
 * server can be changed without typing its tokens again.
 */
export interface McpServerSetRequest {
  name: string;
  command?: string;
  args?: string[];
  env?: Record<string, string>;
  url?: string;
  headers?: Record<string, string>;
}

export interface McpServerRemoveRequest {
  name: string;
}

/** What the API-key window finds on `window.tasktrooperKeys`. */
export interface KeysBridge {
  list(): Promise<ProviderKeysState>;
  set(request: KeySetRequest): Promise<ProviderKeysState>;
  remove(id: string): Promise<ProviderKeysState>;
  prefill(): Promise<KeysPrefill | null>;
  mcpList(): Promise<McpServersState>;
  mcpSet(request: McpServerSetRequest): Promise<McpServersState>;
  mcpRemove(name: string): Promise<McpServersState>;
  onPrefill(cb: (prefill: KeysPrefill | null) => void): () => void;
}

/**
 * What the native chrome can ask for.
 *
 * Almost all of it is a read-out: the chrome is a title bar, and a title bar
 * that could start a process would be a title bar worth attacking. Two
 * exceptions arrived with auto-update, and they are the reason the two mutating
 * channels check their sender in the main process like the cloud ones do:
 *
 *  - `checkForUpdate` makes one request to the update feed.
 *  - `restartToUpdate` drains the backend and hands off to Squirrel, which
 *    replaces the bundle and relaunches. It is the user's own press, from this
 *    app's own chrome, and it is the only way an update is ever applied.
 */
export interface ShellBridge {
  appInfo(): Promise<AppInfo>;
  supervisorState(): Promise<SupervisorSnapshot>;
  cloudStatus(): Promise<CloudStatus>;
  reloadCloud(): Promise<void>;
  updateStatus(): Promise<UpdateStatus>;
  checkForUpdate(): Promise<UpdateStatus>;
  restartToUpdate(): Promise<void>;
  onSupervisorState(cb: (snapshot: SupervisorSnapshot) => void): () => void;
  onCloudStatus(cb: (status: CloudStatus) => void): () => void;
  onUpdateStatus(cb: (status: UpdateStatus) => void): () => void;
  onFullScreen(cb: (fullScreen: boolean) => void): () => void;
  accountState(): Promise<AccountState>;
  /**
   * Run locally for now, still signed in. The failure screen's way on when
   * the account's page cannot be reached; only an explicit sign-out unpairs.
   */
  useLocalForNow(): Promise<AccountState>;
  onAccountState(cb: (state: AccountState) => void): () => void;
}

/** Re-exported so the preload and the main process name one shape. */
export type {
  HostLogsRequest,
  HostOverrides,
  HostPreferences,
  HostRunnerSnapshot,
  HostSettings,
  HostWorkspaceChoice,
};
export type {
  AccountState,
  Diagnostics,
  LogLine,
  PreflightReport,
  RunnerPairingBundle,
  RunnerPairingSummary,
  UpdateStatus,
};

export const SHELL_BRIDGE_KEY = "tasktrooper";

export const KEYS_BRIDGE_KEY = "tasktrooperKeys";

/**
 * The global the web app looks for. Two leading underscores, matching
 * `ui/src/lib/desktop-bridge.ts` — its presence is the contract, so the name is
 * not ours to choose freely.
 */
export const CLOUD_BRIDGE_KEY = "__tasktrooperDesktop";
