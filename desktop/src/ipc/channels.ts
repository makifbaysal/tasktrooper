/**
 * Every IPC channel this app has, in one list.
 *
 * The list is exhaustive on purpose. `contextIsolation` and a sandboxed
 * renderer are only worth having if the surface they gate is small enough to
 * read, and a preload that forwards arbitrary channel names re-opens exactly
 * the hole those two settings close. So: named channels, typed payloads,
 * validated in the main process before anything acts on them.
 *
 * Two namespaces:
 *
 *   SHELL_* — the native chrome: a title bar with a status pill, a reload
 *             button, the update affordance and the screen that says so when
 *             the backend did not come up. It asks for almost nothing.
 *   CLOUD_* — the WebContentsView running the bundled SPA. Our own code, served
 *             from `app://tasktrooper`, but it is still the widest half of the
 *             surface — Connect, the workspace picker, the preflight and
 *             diagnostics all live there — so every handler on it verifies the
 *             sender on every call, not once at load. See main/ipc.ts.
 */

import type {
  AppInfo,
  ChildId,
  CloudStatus,
  Diagnostics,
  LogLine,
  PreflightReport,
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
   * Auto-update. These three are deliberately on the SHELL half: the
   * affordance lives in the title bar and the tray, and updating replaces the
   * whole app, which is not a capability to put on the page's bridge at any
   * width. The last two are also the only shell channels that DO something, so
   * both are guarded on the sender being this window — see `main/ipc.ts`.
   */
  updateGet: "shell:update:get",
  updateCheck: "shell:update:check",
  updateRestart: "shell:update:restart",
} as const;

/** Main → the native chrome. One-way; it never replies. */
export const SHELL_EVENTS = {
  supervisorState: "shell:event:supervisor-state",
  cloudStatus: "shell:event:cloud-status",
  updateStatus: "shell:event:update-status",
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

  runnerSnapshot: "cloud:runner:snapshot",
  runnerConnect: "cloud:runner:connect",
  runnerDisconnect: "cloud:runner:disconnect",
  runnerRestartChild: "cloud:runner:restart-child",
  runnerLogs: "cloud:runner:logs",
  runnerClearLogs: "cloud:runner:clear-logs",

  settingsGet: "cloud:settings:get",
  settingsSetPreferences: "cloud:settings:set-preferences",
  chooseWorkspace: "cloud:settings:choose-workspace",
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
} as const;

/** Main → the web app. Push, so the page never polls the supervisor. */
export const CLOUD_EVENTS = {
  runnerState: "cloud:event:runner-state",
  runnerLogs: "cloud:event:runner-logs",
} as const;

export type ShellChannel = (typeof SHELL_CHANNELS)[keyof typeof SHELL_CHANNELS];
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
export type { Diagnostics, LogLine, PreflightReport, UpdateStatus };

export const SHELL_BRIDGE_KEY = "tasktrooper";

/**
 * The global the web app looks for. Two leading underscores, matching
 * `ui/src/lib/desktop-bridge.ts` — its presence is the contract, so the name is
 * not ours to choose freely.
 */
export const CLOUD_BRIDGE_KEY = "__tasktrooperDesktop";
