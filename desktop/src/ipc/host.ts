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
 * Nothing on this bridge is a credential for a person. In local mode
 * `apiToken` is the bearer for a loopback server this same process started; it
 * is the one value that travels shell → page, and it grants access to nothing
 * that is not already on this machine. In account mode the page is the
 * account's web app on its own origin and signs in there with its own cookie:
 * it gets no `apiBase` and no `apiToken`, and the pairing it hands this side
 * goes in, never back out (`runner.pairing()` omits the token).
 */

import type {
  AccountMode,
  AccountState,
  AnalyticsState,
  Blocker,
  ChildId,
  ChildState,
  Diagnostics,
  LogLine,
  NotificationPreferences,
  PreflightReport,
  RunnerPairingBundle,
  RunnerPairingSummary,
  TunnelStatus,
  UpdateStatus,
  WorkspaceCheck,
} from "./types.js";
import type { ChooseDirectoryRequest, KeysPrefill } from "./channels.js";

/**
 * One child process, as the page renders it.
 *
 * No pid: the page shows what is running and whether it is healthy, which is
 * all a person acts on.
 */
export interface HostChild {
  id: ChildId;
  state: ChildState;
  /** False for a child this Mac cannot run. Nothing sets it today. */
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
  /** Account mode only — the runner's tunnel to the control plane. */
  tunnel?: TunnelStatus;
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

  /**
   * Account mode only. `pair` takes the bundle `POST /api/runner/pair`
   * returned, refuses one for another origin than the account's, stores it
   * encrypted and starts the runner; `unpair` stops the runner and forgets it.
   * `pairing` is the stored bundle WITHOUT its token, `null` when unpaired.
   */
  pair(bundle: RunnerPairingBundle): Promise<HostRunnerSnapshot>;
  unpair(): Promise<HostRunnerSnapshot>;
  pairing(): Promise<RunnerPairingSummary | null>;
  /** Restart the runner — account mode's `restartChild`, one child, no id. */
  restart(): Promise<HostRunnerSnapshot>;

  logs(req?: HostLogsRequest): Promise<LogLine[]>;
  subscribeLogs(cb: (lines: LogLine[]) => void): () => void;
  clearLogs(): Promise<void>;

  settings(): Promise<HostSettings>;
  setPreferences(patch: HostPreferences): Promise<HostSettings>;
  chooseWorkspace(): Promise<HostWorkspaceChoice | null>;
  chooseDirectory(options?: ChooseDirectoryRequest): Promise<string | null>;
  reveal(what: "workspace" | "previous-workspace" | "logs"): Promise<void>;

  /**
   * Open an external link (a task's PR, say) in the user's real browser
   * instead of asking the hosted view to navigate there — which is what used
   * to leave the app stuck on its own loading screen with no way back.
   * Resolves false, rather than rejecting, for a link that is neither an
   * `https:` URL nor `http:` to loopback (a local preview): that is an answer
   * the page can show, not an exception.
   */
  openExternal(url: string): Promise<boolean>;

  /**
   * Tells the shell which agent-chat session, if any, is on screen — the main
   * process cannot render a screen itself, so it needs this to decide whether
   * a finished turn's notification would just repeat what the user is already
   * looking at. `null` means no chat screen is open.
   */
  reportChatFocus(focus: { agentId: string; sessionId: string } | null): Promise<void>;

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
 * Auto-update, for Settings. The same service the chrome's popup and the tray
 * read; this is a second window onto it, not a second updater.
 */
export interface DesktopUpdatesHost {
  status(): Promise<UpdateStatus>;
  subscribe(cb: (status: UpdateStatus) => void): () => void;
  /** One request to the feed. Resolves with the status, never rejects on a failed check. */
  check(): Promise<UpdateStatus>;
  /**
   * Drain the local processes, install the staged update and relaunch. A
   * no-op unless the status is `ready`.
   */
  restart(): Promise<void>;
}

/**
 * The anonymous usage count. `set(false)` stops sending at once and deletes
 * the install's random id. Refused for the account's remote page.
 */
export interface DesktopAnalyticsHost {
  get(): Promise<AnalyticsState>;
  set(on: boolean): Promise<AnalyticsState>;
}

/**
 * Local or account, switched at runtime.
 *
 * `signIn` stops the local backend and loads the account's sign-in page in
 * this window; the web app there pairs the runner with `runner.pair`.
 * `signOut` stops the runner and the executor, forgets the pairing, clears the
 * account's session and starts the local backend again. Local data is left
 * as it was. Both resolve once the switch is done — or never, for the page
 * that asked, when the switch replaced that page. `state()` carries more than
 * the page's half declares (`switching`, `paired`, `error`); it is a
 * superset, so the two stay assignable.
 *
 * While the member runs locally for now (`state().temporaryLocal`), `signIn`
 * goes back to the account they are still signed in to, and `signOut` is the
 * one call that forgets it.
 *
 * `openKeys` brings up the API-key window, which is this app's own — the keys
 * are typed there, never into the page that asked. It answers nothing and its
 * optional `prefill` names a provider (id, type, address, models, name) but
 * never carries a key, so the account's page can offer the button without ever
 * holding one.
 */
export interface DesktopAccountHost {
  signIn(origin?: string): Promise<void>;
  signOut(): Promise<void>;
  state(): Promise<AccountState>;
  openKeys(prefill?: KeysPrefill): Promise<void>;
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
  /**
   * Bumped when the bridge gains something a page may need to feature-test.
   * 1: `account`, `mode` and the runner's pairing calls.
   * 2: `account.openKeys()` and `account.state().temporaryLocal`.
   * 3: `account.openKeys(prefill)` focuses the key window on one provider.
   * 4: `analytics.get()` / `analytics.set(on)`.
   */
  bridgeVersion: 4;
  /** Which world this page is in, read synchronously before the first render. */
  mode: AccountMode;
  account: DesktopAccountHost;
  runner: DesktopRunnerHost;
  updates: DesktopUpdatesHost;
  analytics: DesktopAnalyticsHost;
}

export type { AccountMode, AccountState, ChooseDirectoryRequest, RunnerPairingBundle, RunnerPairingSummary, UpdateStatus };
