/**
 * The contract between this shell and the web app it serves.
 *
 * The desktop app is the web app in a window, plus a supervisor. Every screen
 * the user sees is the web app's; the only thing this shell adds is the set of
 * things a browser tab cannot do — run the backend, open a native folder
 * picker, and say what this Mac can and cannot run. Those arrive as
 * `window.__tasktrooperDesktop`, installed by `preload/cloud.ts` and answered
 * by the main process.
 *
 * This file is one half of a contract whose other half is
 * `ui/src/lib/desktop-bridge.ts`. The two are separate declarations because the
 * SPA builds in a browser with no knowledge of this package; they are kept
 * structurally identical instead — a change here that is not made there shows
 * up as a page that calls a method the shell does not have.
 *
 * There is no sign-in anywhere in this product, so nothing on this bridge is a
 * credential for a person. `apiToken` is the bearer for a loopback server this
 * same process started; it is the one value that travels shell → page, and it
 * grants access to nothing that is not already on this machine.
 */

import type {
  Blocker,
  ChildId,
  ChildState,
  Diagnostics,
  LogLine,
  NotificationPreferences,
  PreflightReport,
  WorkspaceCheck,
} from "./types.js";

/**
 * One child process, as the page renders it.
 *
 * No pid: the page shows what is running and whether it is healthy, which is
 * all a person acts on.
 */
export interface HostChild {
  id: ChildId;
  state: ChildState;
  /** False for a child this Mac cannot run — Appium, absent or already running. */
  enabled: boolean;
  restarts: number;
  detail?: string;
  nextRestartAt?: number;
  exitedAt?: number;
}

/**
 * The supervisor, reduced to what the page renders.
 *
 * `phase` is coarse on purpose: it is the same vocabulary a browser-only copy
 * of the page would use, and it keeps the web app from having to know the child
 * list to say what is going on.
 */
export interface HostRunnerSnapshot {
  phase: "idle" | "connecting" | "connected" | "degraded" | "stopping" | "failed";
  /** One line for a person: "starting the backend…". Narration, not a log. */
  detail?: string;
  since: number;
  children: HostChild[];
  /** `http://127.0.0.1:<port>` while the backend is up. */
  apiBase?: string;
  /**
   * A REQUIRED preflight item that is not `ok`, which is why the backend was
   * not started. Optional items never produce one. `blocker.id` names the item,
   * so the setup screen can point at the row rather than repeating the
   * sentence.
   */
  blocker?: Blocker;
}

/** The whole user-facing configuration. Four fields, and one of them is a path. */
export interface HostSettings {
  workspaceDir: string;
  launchAtLogin: boolean;
  autoConnect: boolean;
  notifications: NotificationPreferences;
}

/**
 * What the page may set directly: two switches, plus the notification
 * preferences (always sent as a full object — see `validate.ts`).
 *
 * The workspace folder is deliberately NOT here. A page can ask for the native
 * picker — which the user then drives — but it cannot name a path, because
 * naming a path is naming where this app creates directories and where a Claude
 * Code session is pointed.
 */
export interface HostPreferences {
  launchAtLogin?: boolean;
  autoConnect?: boolean;
  notifications?: NotificationPreferences;
}

/** The result of the native folder picker: null when the user cancelled. */
export interface HostWorkspaceChoice {
  check: WorkspaceCheck;
  /** Present when the choice was valid and has been applied. */
  settings?: HostSettings;
}

/** Manual overrides, for what detection actually failed to find. */
export interface HostOverrides {
  claudeBin?: string;
  gitBin?: string;
  chromeBin?: string;
  appiumBin?: string;
}

export interface HostLogsRequest {
  child?: ChildId | "supervisor";
  afterSeq?: number;
  limit?: number;
}

/**
 * The local half: everything a browser tab cannot do, and nothing else.
 *
 * There is no generic "run this" and no filesystem read or write. Each entry is
 * a named action whose arguments come from a fixed set (a child id, a boolean)
 * or from a native dialog the user drove.
 */
export interface DesktopRunnerHost {
  snapshot(): Promise<HostRunnerSnapshot>;
  /** Push, not poll — the main process is the only thing that knows. */
  subscribe(cb: (snapshot: HostRunnerSnapshot) => void): () => void;

  /** Start the backend: preflight, workspace, embedder, agent-server, /health. */
  connect(): Promise<HostRunnerSnapshot>;
  disconnect(): Promise<HostRunnerSnapshot>;
  restartChild(child: ChildId): Promise<HostRunnerSnapshot>;

  logs(req?: HostLogsRequest): Promise<LogLine[]>;
  subscribeLogs(cb: (lines: LogLine[]) => void): () => void;
  clearLogs(): Promise<void>;

  settings(): Promise<HostSettings>;
  setPreferences(patch: HostPreferences): Promise<HostSettings>;
  chooseWorkspace(): Promise<HostWorkspaceChoice | null>;
  reveal(what: "workspace" | "previous-workspace" | "logs"): Promise<void>;

  /**
   * Open an external link (a task's PR, say) in the user's real browser
   * instead of asking the hosted view to navigate there — which is what used
   * to leave the app stuck on its own loading screen with no way back.
   * Resolves false, rather than rejecting, for a link that is not an
   * `https:` URL: that is an answer the page can show, not an exception.
   */
  openExternal(url: string): Promise<boolean>;

  /**
   * The environment checklist, and the whole of the setup screen's data.
   *
   * `force` re-runs every probe. It is what the "Check again" button calls
   * after the user has gone away and installed something, and it must not be
   * answered from a cache — a report from before the fix says the fix did not
   * work.
   */
  preflight(force?: boolean): Promise<PreflightReport>;

  diagnostics(force?: boolean): Promise<Diagnostics>;
  setOverrides(patch: HostOverrides): Promise<Diagnostics>;
}

/**
 * What the web app finds on `window.__tasktrooperDesktop`.
 *
 * The presence of this object IS the "we are running in the desktop shell"
 * contract, and only our own preload can install it.
 */
export interface DesktopHost {
  info(): Promise<{ app: string; version: string; platform: string }>;
  /**
   * Where the page's API calls go: `http://127.0.0.1:<port>`, no path suffix.
   *
   * A value rather than a call, and it has to be: `api.ts` reads it on every
   * request path, so a promise would put an await in front of every call in the
   * app. The window is not created until the backend has answered, so this is
   * never absent in a working shell.
   */
  apiBase?: string;
  /**
   * The bearer the local backend was started with. Read synchronously for the
   * same reason `apiBase` is.
   */
  apiToken?: string;
  runner: DesktopRunnerHost;
}
