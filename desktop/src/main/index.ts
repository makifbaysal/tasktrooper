import { randomUUID } from "node:crypto";
import { accessSync, appendFileSync, constants, mkdirSync, statSync } from "node:fs";
import path from "node:path";
import { BrowserWindow, Menu, app, powerMonitor, session, type IpcMainEvent, type IpcMainInvokeEvent } from "electron";
import { CLOUD_EVENTS, SHELL_EVENTS, type KeySetRequest, type McpServerSetRequest } from "../ipc/channels.js";
import type {
  HostOverrides,
  HostPreferences,
  HostRunnerSnapshot,
  HostSettings,
  HostWorkspaceChoice,
} from "../ipc/host.js";
import type {
  AccountState,
  AppInfo,
  CloudStatus,
  Diagnostics,
  LogLine,
  PreflightReport,
  RunnerPairingBundle,
  RunnerPairingSummary,
  SupervisorSnapshot,
  UpdateStatus,
} from "../ipc/types.js";
import { ACCOUNT_MODE_RUNS_EMBEDDER, ModeController } from "./account/mode.js";
import { ACCOUNT_HOME_ROUTE, accountPartition, defaultAccountOrigin } from "./account/origin.js";
import { PairingStore } from "./config/pairing.js";
import { McpServerStore } from "./config/mcp-servers.js";
import { ProviderStore } from "./config/providers.js";
import { SecretStore, type LocalSecrets } from "./config/secrets.js";
import { SettingsStore } from "./config/settings.js";
import { checkWorkspace, pickDirectory, pickWorkspace, reveal } from "./config/workspace.js";
import { applicationMenuTemplate } from "./app-menu.js";
import { registerIpc, type IpcServices } from "./ipc.js";
import { KeyService } from "./keys/keys.js";
import { KeysWindow } from "./keys/window.js";
import { launchedHidden, setLaunchAtLogin } from "./login-item.js";
import { quitSequence } from "./quit.js";
import { isOwnPage, isTrustedFrame } from "./sender-guard.js";
import { APP_ORIGIN, registerAppSchemePrivileges, serveAppScheme } from "./services/app-scheme.js";
import { binDir, dataDir } from "./services/detect.js";
import { NotificationWatcher } from "./services/notifications.js";
import {
  FEED_DEBUG_ENV,
  INERT_UPDATER_BACKEND,
  UpdateService,
  installsOnQuit,
  resolveFeed,
  type UpdaterBackend,
} from "./services/updater.js";
import { RunnerSupervisor } from "./runner/supervisor.js";
import { Supervisor } from "./supervisor/supervisor.js";
import { AppTray } from "./tray.js";
import { openExternally, Shell } from "./window.js";

/**
 * The main process: wiring, and only wiring.
 *
 * Everything with behaviour lives in supervisor/, runner/, account/, config/
 * or services/, so this file reads as the list of decisions the app makes at
 * the top level — what happens at launch, what happens on quit, and which
 * sender is allowed to ask for any of it.
 *
 * Two modes, switched at runtime by `account/mode.ts`. Local: `supervisor`
 * runs the backend and the embedder, and the window shows `app://tasktrooper`.
 * Account: `runnerSupervisor` runs the runner, and the window shows the
 * account's web app from its origin. Almost every function below that touches
 * a supervisor asks which mode is current rather than branching on a build.
 */

// One instance. Two supervisors on one Mac would each start a backend against
// the same data directory, and embedded Postgres would refuse the second — or,
// worse, not refuse it.
const gotInstanceLock = app.requestSingleInstanceLock();
if (!gotInstanceLock) app.quit();

// Windows attributes a toast to the AppUserModelID of the process that raised
// it, and shows nothing for one that matches no Start-menu shortcut. The NSIS
// installer stamps its shortcut with electron-builder's `appId`, so this must
// be that string exactly, and set before the first notification.
if (process.platform === "win32") app.setAppUserModelId("ai.tasktrooper.desktop");

const settingsStore = new SettingsStore();
const secretStore = new SecretStore();
const supervisor = new Supervisor();
const providerStore = new ProviderStore(app.getPath("userData"));
const mcpServerStore = new McpServerStore(app.getPath("userData"));
const runnerSupervisor = new RunnerSupervisor(new PairingStore(app.getPath("userData")), providerStore, {
  mcpServers: mcpServerStore,
  // The embedder is the local supervisor's and runs in both modes; the runner
  // only needs to know where it is.
  embeddings: () => (ACCOUNT_MODE_RUNS_EMBEDDER ? supervisor.embedderUrl() : Promise.resolve(null)),
});

/** The API-key window: this app's own page, the only one that may hand over a key. */
const keysWindow = new KeysWindow();

/**
 * Local or account. Built before the window, which asks it which origin to
 * trust; its steps reach for things declared further down, and only run once
 * the app is ready.
 */
const modeController = new ModeController({
  settings: settingsStore,
  rules: { allowLoopbackHttp: !app.isPackaged },
  defaultOrigin: defaultAccountOrigin({ packaged: app.isPackaged }),
  stopLocal: async () => {
    notifications.stop();
    await (ACCOUNT_MODE_RUNS_EMBEDDER ? supervisor.disconnect() : supervisor.drain());
  },
  startLocal: () => startLocal(),
  stopAccount: async () => {
    await runnerSupervisor.unpair();
  },
  pauseAccount: async () => {
    await runnerSupervisor.disconnect();
  },
  resumeAccount: () => startRunner(),
  showAccount: (_origin, route) => {
    servedBase = null;
    shellWindow.retire();
    shellWindow.serve(route);
  },
  showLocal: () => {
    servedBase = null;
    shellWindow.retire();
  },
  clearAccountSession: async (origin) => {
    const partition = session.fromPartition(accountPartition(origin));
    await partition.clearStorageData();
    await partition.clearCache();
  },
  paired: () => runnerSupervisor.paired,
});

const accountMode = (): boolean => modeController.mode === "account";

/** The one origin the web app's view may hold right now. */
function trustedOrigin(): string {
  return accountMode() ? modeController.origin : APP_ORIGIN;
}

/**
 * The API key and the MCP secrets key this install was generated with, held in
 * memory after the first read. `null` only when the login keychain would not
 * provide an encryption key, which is a state the supervisor reports rather
 * than one this file can fix.
 */
let secrets: LocalSecrets | null = null;
/** Why `secrets` is null, in the store's own words. */
let secretsError: string | undefined;
let tray: AppTray | null = null;

/**
 * Polls the backend the moment it becomes reachable and stops the moment it
 * doesn't, so it never notifies about a task snapshot the backend cannot
 * currently confirm. Built once, at `whenReady` — see the `supervisor.on("state", ...)`
 * listener below for its start/stop and `quit.run()` for its teardown.
 */
const notifications = new NotificationWatcher({
  apiBase: () => supervisor.apiBase ?? null,
  apiToken: () => secrets?.api_token ?? null,
  getPreferences: () => settingsStore.get().notifications,
  onNotificationClick: (route) => showWindow(route ?? "/board"),
  isWindowFocused: () => shellWindow.window?.isFocused() ?? false,
  onBattery: () => powerMonitor.isOnBatteryPower(),
  isPageOnScreen: () => shellWindow.pageOnScreen,
});

/**
 * Auto-update, built at `whenReady` because resolving the feed reads
 * `app.isPackaged` and `app.getPath` — and, when there is a feed, only once
 * electron-updater has loaded (see `startUpdates`).
 *
 * `null` until then, and it stays usable when the feed is absent — a
 * development run and a `npm run package` build both report `unsupported` and
 * do nothing, which is the honest state rather than a failure to draw.
 */
let updates: UpdateService | null = null;

/**
 * The updater's own log file, at `app.getPath("logs")` — `~/Library/Logs/TaskTrooper`
 * on macOS. The title bar has room for one word when a check fails
 * ("update check failed"); this is where the actual reason goes, since the
 * hover tooltip on that word is easy to miss and there was previously nowhere
 * else to look at all. Best-effort: a log write failing must never be why an
 * update check itself fails.
 */
function logUpdaterLine(line: string): void {
  try {
    const dir = app.getPath("logs");
    mkdirSync(dir, { recursive: true });
    appendFileSync(path.join(dir, "updater.log"), `${new Date().toISOString()} ${line}\n`, "utf8");
  } catch {
    // Best-effort — see above.
  }
}

/** The base the window was last told about, so a new port can be noticed. */
let servedBase: string | null = null;

/** Undoes the current log stream to the page, or null when none is open. */
let stopLogStream: (() => void) | null = null;

/**
 * Push the supervisor's log batches to the web app while it has asked for
 * them, and not otherwise. Every line used to cross IPC whether or not a log
 * view was open, which was nearly always "not". A page that navigates or
 * reloads has asked for nothing yet, so either ends the stream.
 */
function streamLogs(on: boolean): void {
  if (!on) {
    stopLogStream?.();
    return;
  }
  const contents = shellWindow.cloudContents;
  if (stopLogStream || !contents || contents.isDestroyed()) return;
  const forward = (lines: LogLine[]): void => toCloud(CLOUD_EVENTS.runnerLogs, lines);
  const onNavigation = (details: { isMainFrame: boolean; isSameDocument: boolean }): void => {
    if (details.isMainFrame && !details.isSameDocument) stop();
  };
  const source = accountMode() ? runnerSupervisor : supervisor;
  const stop = (): void => {
    source.off("logs", forward);
    contents.off("did-start-navigation", onNavigation);
    contents.off("destroyed", stop);
    stopLogStream = null;
  };
  source.on("logs", forward);
  contents.on("did-start-navigation", onNavigation);
  contents.once("destroyed", stop);
  stopLogStream = stop;
}

/**
 * Build the updater. A build without a feed gets one that does nothing and
 * never loads electron-updater; one with a feed loads it first, off the launch
 * path — its first check is twenty seconds away regardless.
 */
function startUpdates(): void {
  const feed = resolveFeed({ packaged: app.isPackaged, resourcesPath: process.resourcesPath });
  const begin = (backend: UpdaterBackend): void => {
    updates = new UpdateService({
      backend,
      feed,
      installOnQuit: installsOnQuit(process.platform, process.env),
      onStatus: (status) => {
        tray?.updateStatus(status);
        broadcast(SHELL_EVENTS.updateStatus, status);
        toCloud(CLOUD_EVENTS.updateStatus, status);
      },
      debug: !!process.env[FEED_DEBUG_ENV],
      logLine: logUpdaterLine,
    });
    tray?.updateStatus(updates.status);
    broadcast(SHELL_EVENTS.updateStatus, updates.status);
    toCloud(CLOUD_EVENTS.updateStatus, updates.status);
    updates.start();
  };
  if (feed.kind === "none") {
    begin(INERT_UPDATER_BACKEND);
    return;
  }
  void import("electron-updater").then(
    ({ autoUpdater }) => begin(autoUpdater as unknown as UpdaterBackend),
    (err: unknown) => logUpdaterLine(`could not load the updater: ${err instanceof Error ? err.message : String(err)}`),
  );
}

// Before whenReady, and it has to be: the privilege table is read once while
// the network service starts. See services/app-scheme.ts.
registerAppSchemePrivileges();

const shellWindow = new Shell({
  origin: trustedOrigin,
  partition: () => (accountMode() ? accountPartition(modeController.origin) : undefined),
  onCloudStatus: (status) => broadcast(SHELL_EVENTS.cloudStatus, status),
  onFullScreen: (fullScreen) => broadcast(SHELL_EVENTS.fullScreen, fullScreen),
  // `quit` is assigned further down, but this closure is not called until the
  // window's close event actually fires — well after that assignment runs.
  quitStarted: () => quit.started,
  onSessionEnd: () => void quit.run(),
});

// --- helpers ----------------------------------------------------------------

/**
 * A push to a renderer, and the try/catch is not defensive tidiness.
 *
 * `isDestroyed()` is false for a WebContents whose render frame has already
 * gone, which is exactly the window during teardown when the supervisor is
 * emitting a state change per child stop. `send` then throws "Render frame was
 * disposed before WebFrameMain could be accessed" out of a child's exit
 * handler, where nothing is waiting to catch it.
 */
function send(contents: Electron.WebContents | null | undefined, channel: string, payload: unknown): void {
  // Once the drain is running there is nobody to tell, and the supervisor emits
  // a state change per child stop — which is exactly when the frames go.
  if (quit.started) return;
  if (!contents || contents.isDestroyed()) return;
  try {
    contents.send(channel, payload);
  } catch {
    // The frame went away between the check and the send; there is nobody left
    // to tell, and the state it would have carried is re-read on next create.
  }
}

function broadcast(channel: string, payload: unknown): void {
  const window = shellWindow.window;
  send(window && !window.isDestroyed() ? window.webContents : null, channel, payload);
}

/** Push to the web app. Only ever the `CLOUD_EVENTS` it subscribes to. */
function toCloud(channel: string, payload: unknown): void {
  send(shellWindow.cloudContents, channel, payload);
}

function reconfigure(): void {
  runnerSupervisor.configure(settingsStore.get(), settingsStore.overrides());
  supervisor.configure({
    secrets,
    ...(secretsError !== undefined ? { secretsError } : {}),
    settings: settingsStore.get(),
    overrides: settingsStore.overrides(),
  });
}

/**
 * Read (or on a first run, create) the local secrets, and try again on every
 * start while they are missing: a Linux keyring that was locked at launch can
 * be unlocked before the user presses Connect, and the refusal tells them to.
 *
 * A failure leaves `secrets` null on purpose. `supervisor.connect()` refuses
 * with the sentence that explains it, which is where the user is looking —
 * throwing here would be an app that exits at launch with a dialog nobody can
 * act on.
 */
function loadSecrets(): void {
  if (secrets) return;
  try {
    secrets = secretStore.ensure();
    secretsError = undefined;
  } catch (err) {
    secrets = null;
    secretsError = err instanceof Error ? err.message : String(err);
  }
  reconfigure();
}

/**
 * The supervisor snapshot, reduced to what the web app renders.
 *
 * The narrowing is the point: the page must not learn a pid or a path from
 * here. It gets what is running, whether it is healthy, and one line saying why
 * not.
 */
function activeSnapshot(): SupervisorSnapshot {
  return accountMode() ? runnerSupervisor.snapshot() : supervisor.snapshot();
}

function hostSnapshot(snapshot: SupervisorSnapshot = activeSnapshot()): HostRunnerSnapshot {
  const phase = ((): HostRunnerSnapshot["phase"] => {
    switch (snapshot.state) {
      case "preflight":
      case "starting":
        return "connecting";
      case "running":
        return "connected";
      case "degraded":
        return "degraded";
      case "stopping":
        return "stopping";
      case "failed":
        return "failed";
      default:
        return "idle";
    }
  })();

  return {
    phase,
    ...(snapshot.detail !== undefined ? { detail: snapshot.detail } : {}),
    since: snapshot.since,
    ...(snapshot.apiBase !== undefined ? { apiBase: snapshot.apiBase } : {}),
    children: snapshot.children.map((child) => ({
      id: child.id,
      state: child.state,
      enabled: child.enabled,
      restarts: child.restarts,
      ...(child.detail !== undefined ? { detail: child.detail } : {}),
      ...(child.nextRestartAt !== undefined ? { nextRestartAt: child.nextRestartAt } : {}),
      ...(child.exitedAt !== undefined ? { exitedAt: child.exitedAt } : {}),
    })),
    ...(snapshot.blocker !== undefined ? { blocker: snapshot.blocker } : {}),
    ...(snapshot.tunnel !== undefined ? { tunnel: snapshot.tunnel } : {}),
  };
}

/**
 * An override names a path this app will spawn, and it arrives from a page.
 * Validation has already made it an absolute path with no control characters;
 * this makes it a file that exists and that the user can execute. A stored
 * override that is neither would fail at the next start, several layers from
 * the moment it was typed.
 */
function checkedOverrides(patch: HostOverrides): HostOverrides {
  const out: HostOverrides = {};
  for (const [key, value] of Object.entries(patch) as [keyof HostOverrides, string | undefined][]) {
    if (value === undefined) continue;
    if (value === "") {
      out[key] = "";
      continue;
    }
    let usable = false;
    try {
      if (statSync(value).isFile()) {
        accessSync(value, constants.X_OK);
        usable = true;
      }
    } catch {
      usable = false;
    }
    if (!usable) throw new Error(`${value} is not a file this machine can run.`);
    out[key] = value;
  }
  return out;
}

// --- the drain --------------------------------------------------------------

/**
 * Quit. The backend is drained rather than killed, because SIGTERM is what lets
 * it finish the requests it is serving, cancel the Claude Code sessions it is
 * supervising and shut its database down cleanly.
 *
 * `app.quit()` on its own would tear this process down and orphan the children,
 * so quitting is deferred until the drain resolves. The sequence and its second
 * ending — installing a staged update — live in `quit.ts`, where the order is
 * asserted rather than read.
 */
const quit = quitSequence({
  stopUpdates: () => updates?.stop(),
  stopNotifications: () => notifications.stop(),
  destroyTray: () => {
    tray?.destroy();
    tray = null;
  },
  // Both, whichever mode is current: a session that switched modes can have
  // an embedder from local mode and a runner from account mode, and draining
  // an idle supervisor costs nothing.
  drain: async () => {
    const [local] = await Promise.all([supervisor.drain(), runnerSupervisor.drain()]);
    return local;
  },
  installUpdate: () => updates?.install() ?? false,
  exit: (code) => app.exit(code),
});

/**
 * The one way the app ever comes back by itself, and it takes a press to get
 * here.
 *
 * A staged update is applied either way — Squirrel's ShipIt swaps the bundle
 * when this process exits, so a plain Quit applies it too, silently and without
 * returning. What this adds is the relaunch, which is what the button says it
 * does. Both endings drain first; `quit.ts` owns that.
 */
function restartToUpdate(): void {
  if (!updates?.ready) return;
  void quit.run({ applyUpdate: true });
}

// --- services ---------------------------------------------------------------

function appInfo(): AppInfo {
  return {
    version: app.getVersion(),
    electron: process.versions.electron ?? "",
    chrome: process.versions.chrome ?? "",
    node: process.versions.node,
    platform: process.platform,
    arch: process.arch,
    packaged: app.isPackaged,
    origin: accountMode() ? modeController.origin : (supervisor.apiBase ?? ""),
  };
}

/**
 * After the API keys or the MCP servers changed: the runner reads them at spawn and hands them to
 * the executor on its stdin, so a running one restarts — stop and connect,
 * which keeps the pairing. One that was refused for having no way to run an
 * agent starts now that it may have one. Answers whether either happened.
 */
function providersChanged(): boolean {
  if (!accountMode()) return false;
  if (runnerSupervisor.running) {
    void runnerSupervisor.restart();
    return true;
  }
  if (runnerSupervisor.paired && runnerSupervisor.snapshot().blocker?.id === "api-keys") {
    void runnerSupervisor.connect();
    return true;
  }
  void runnerSupervisor.detect().catch(() => undefined);
  return false;
}

const keyService = new KeyService({
  store: providerStore,
  mcpStore: mcpServerStore,
  changed: providersChanged,
  log: (line) => console.warn(line),
  newId: () => randomUUID(),
});

/**
 * The pairing the account's web app hands over, checked against the account
 * this computer is signed in to before the runner sees it: its `tm_base_url`
 * is where the runner dials with a bearer token.
 */
async function pairRunner(bundle: RunnerPairingBundle): Promise<HostRunnerSnapshot> {
  const refusal = modeController.checkPairing(bundle);
  if (refusal) throw new Error(refusal);
  return hostSnapshot(await runnerSupervisor.pair(bundle));
}

const services: IpcServices = {
  appInfo,
  supervisorState: activeSnapshot,
  cloudStatus: (): CloudStatus => shellWindow.status,
  reloadCloud: () => shellWindow.reloadCloud(),

  updateStatus: (): UpdateStatus => updates?.status ?? { phase: "unsupported", detail: "The updater has not started yet." },
  checkForUpdate: async (): Promise<UpdateStatus> =>
    (await updates?.check()) ?? { phase: "unsupported", detail: "The updater has not started yet." },
  restartToUpdate,

  accountMode: () => modeController.mode,
  accountState: (): AccountState => modeController.state(),
  accountSignIn: (origin?: string) => modeController.signIn(origin),
  accountSignOut: () => modeController.signOut(),
  accountUseLocalForNow: () => modeController.useLocalForNow(),
  openKeys: (prefill) => keysWindow.open(prefill),

  keysList: () => keyService.list(),
  keysSet: (request: KeySetRequest) => keyService.set(request),
  keysRemove: (id: string) => keyService.remove(id),
  keysPrefill: () => keysWindow.prefill,
  keysMcpList: () => keyService.listMcp(),
  keysMcpSet: (request: McpServerSetRequest) => keyService.setMcp(request),
  keysMcpRemove: (name: string) => keyService.removeMcp(name),

  hostInfo: () => ({ app: "tasktrooper-desktop", version: app.getVersion(), platform: process.platform }),

  // Local mode: the page is served from APP_ORIGIN, which has no gateway
  // behind it, so it has to be told where the API lives — and, because the
  // backend picks its own port, that address is not knowable until it has
  // answered. The window is not created until then, so an empty answer here
  // means something is wrong rather than something is early.
  //
  // Account mode: nothing, and the token above all. The page is the account's
  // web app on its own origin, talking to its own API with its own cookie;
  // the local backend's bearer is not something a remote origin may hold.
  apiBase: () => (accountMode() ? "" : (supervisor.apiBase ?? "")),
  apiToken: () => (accountMode() ? "" : (secrets?.api_token ?? "")),

  runnerSnapshot: () => hostSnapshot(),
  connect: async () => {
    if (accountMode()) return hostSnapshot(await runnerSupervisor.connect());
    loadSecrets();
    return hostSnapshot(await supervisor.connect());
  },
  disconnect: async () =>
    hostSnapshot(accountMode() ? await runnerSupervisor.disconnect() : await supervisor.disconnect()),
  restartChild: async (child) => {
    if (accountMode()) return hostSnapshot(await runnerSupervisor.restart());
    return hostSnapshot(await supervisor.restartChild(child as never));
  },
  logs: (req) => {
    if (!accountMode()) return supervisor.logs(req.child as never, req.afterSeq ?? 0, req.limit);
    const child = req.child === undefined ? undefined : req.child === "supervisor" ? "supervisor" : "runner";
    return runnerSupervisor.logs(child, req.afterSeq ?? 0, req.limit);
  },
  streamLogs,
  clearLogs: () => (accountMode() ? runnerSupervisor.clearLogs() : supervisor.clearLogs()),

  pair: pairRunner,
  unpair: async () => hostSnapshot(await runnerSupervisor.unpair()),
  pairingInfo: (): RunnerPairingSummary | null => runnerSupervisor.pairingInfo(),
  restartRunner: async () => hostSnapshot(await runnerSupervisor.restart()),

  getSettings: (): HostSettings => {
    const { workspaceDir, launchAtLogin, autoConnect, notifications: prefs } = settingsStore.get();
    return { workspaceDir, launchAtLogin, autoConnect, notifications: prefs };
  },

  setPreferences: async (patch: HostPreferences): Promise<HostSettings> => {
    const next = settingsStore.set(patch);
    reconfigure();
    if (patch.launchAtLogin !== undefined) setLaunchAtLogin(patch.launchAtLogin);
    return Promise.resolve(next);
  },

  /**
   * The workspace folder, and the only way it can change.
   *
   * The page cannot name a path; it asks for the native picker, the user drives
   * it, and the result is validated before it is stored. Changing it while the
   * backend is up restarts it, because the folder is part of the environment it
   * was started with and read once — a UI claiming one folder while the running
   * process resolves paths against another is worse than a restart.
   */
  chooseWorkspace: async (): Promise<HostWorkspaceChoice | null> => {
    const chosen = await pickWorkspace(settingsStore.get().workspaceDir);
    if (!chosen) return null;
    if (!chosen.ok) return { check: chosen };
    settingsStore.set({ workspaceDir: chosen.path });
    reconfigure();
    const active = accountMode() ? runnerSupervisor : supervisor;
    if (active.running) {
      await active.disconnect();
      void active.connect();
    }
    return { check: chosen, settings: services.getSettings() };
  },

  chooseDirectory: (options) => pickDirectory(options),

  reveal: (what) => {
    if (what === "workspace") reveal(settingsStore.get().workspaceDir);
    else if (what === "previous-workspace") {
      const previous = settingsStore.previousWorkspaceDir();
      if (previous) reveal(previous);
    } else reveal(app.getPath("userData"));
  },

  openExternal: (url) => openExternally(url),

  reportChatFocus: (focus) => {
    notifications.setFocusedChat(
      focus.agentId && focus.sessionId ? { agentId: focus.agentId, sessionId: focus.sessionId } : null,
    );
  },

  /**
   * The environment checklist. Answered from the last sweep unless the caller
   * asks for a fresh one, because the caller that asks is the one whose user
   * has just installed something.
   */
  preflight: (force: boolean): Promise<PreflightReport> => runPreflight(force),

  diagnostics: (force: boolean): Promise<Diagnostics> => buildDiagnostics(force),
  setOverrides: async (patch: HostOverrides): Promise<Diagnostics> => {
    // An override names a binary this app will run — the runner execs it with
    // a task's prompt on its stdin. That is not a decision a remote origin's
    // page gets to make; in account mode it is refused here, and made in local
    // mode, on this app's own page.
    if (accountMode()) throw new Error("Paths to programs can only be changed while TaskTrooper runs locally.");
    settingsStore.setOverrides(checkedOverrides(patch));
    reconfigure();
    return buildDiagnostics(true);
  },
};

/**
 * Is this call really from the web app's view, right now?
 *
 * Three questions, all of which must answer yes: the exact WebContents we
 * created for it, its top frame, and an origin that is still the trusted one —
 * `app://tasktrooper` in local mode, the account's origin in account mode,
 * never both. A popup, an iframe, the chrome's own renderer, a view that
 * navigated elsewhere and the other mode's origin all fail at least one.
 */
function isTrustedCloudSender(event: IpcMainInvokeEvent): boolean {
  try {
    return isTrustedFrame(event, shellWindow.cloudContents, trustedOrigin());
  } catch {
    return false;
  }
}

/**
 * The same view, without the origin question.
 *
 * A preload runs before its document commits, so the frame has no URL yet and
 * the origin check above refuses it — which would leave the page with no API
 * base and no token, sending every request at its own `app://` origin where the
 * static handler answers each one with index.html.
 *
 * Only the synchronous channels use this. None takes an argument and none
 * changes anything, and in account mode the two that carry values answer "".
 */
function isCloudWebContents(event: IpcMainEvent): boolean {
  const contents = shellWindow.cloudContents;
  return contents !== null && !contents.isDestroyed() && event.sender === contents;
}

/**
 * Is this call from the shell's own chrome?
 *
 * The title bar is our code under `file://` (or the Vite dev server), and it is
 * the top frame of the window itself — not the `WebContentsView` composited on
 * top of it. Comparing the WebContents is enough and does not need an origin
 * check the way the cloud guard does: there is exactly one of these, we made
 * it, and it never navigates (`hardenShellNavigation` in window.ts).
 */
function isShellSender(event: IpcMainInvokeEvent): boolean {
  const contents = shellWindow.window?.webContents;
  return !!contents && !contents.isDestroyed() && event.sender === contents;
}

/**
 * Is this call from the API-key window's own page? That exact WebContents, its
 * top frame, on the URL it was opened with. The web app's view fails the
 * first question whichever origin it holds — `app://tasktrooper` in local
 * mode, the account's in account mode — and so does the chrome.
 */
function isKeysSender(event: IpcMainInvokeEvent): boolean {
  try {
    return isOwnPage(event, keysWindow.contents, keysWindow.pageUrl);
  } catch {
    return false;
  }
}

/**
 * The preflight, run again or answered from the last sweep — always the
 * COMPLETE report, waiting for one in flight rather than handing the setup
 * screen the half a start was allowed to go ahead on.
 *
 * "The last sweep" is never nothing: detection runs at launch, so the setup
 * screen has answers the moment it opens. `force` is what the "Check again"
 * button calls, and it must actually re-probe — a cached report after a fix
 * tells the user their fix did not work.
 */
function runPreflight(force: boolean): Promise<PreflightReport> {
  if (accountMode()) {
    const current = runnerSupervisor.preflight;
    if (!force && current.items.length > 0) return Promise.resolve(current);
    return runnerSupervisor.detect();
  }
  return supervisor.detect(force ? { force: true } : { maxAgeMs: Number.POSITIVE_INFINITY });
}

async function buildDiagnostics(force: boolean): Promise<Diagnostics> {
  const settings = settingsStore.get();
  const check = checkWorkspace(settings.workspaceDir);
  return {
    preflight: await runPreflight(force),
    workspaceDir: settings.workspaceDir,
    binDir: binDir(),
    dataDir: dataDir(),
    ...(check.freeBytes !== undefined ? { dataFree: check.freeBytes } : {}),
    app: appInfo(),
  };
}

// --- lifecycle --------------------------------------------------------------

function showWindow(route?: string): void {
  shellWindow.show();
  if (route) shellWindow.navigate(route);
}

app.on("second-instance", () => showWindow());

/**
 * Start the backend and, once it answers, put the web app on screen.
 *
 * This is the local launch path and what the tray's Start calls. A failure is
 * not thrown at anybody: the supervisor already narrated it, and the chrome's
 * own offline screen is where a user is looking.
 */
async function startBackend(): Promise<void> {
  loadSecrets();
  const snapshot = await supervisor.connect();
  if (snapshot.state === "failed" && !accountMode()) {
    shellWindow.markUnavailable(snapshot.detail ?? "The local server did not start.");
  }
}

/**
 * Local mode coming back after an account: the embedder too when account mode
 * stopped it (`ACCOUNT_MODE_RUNS_EMBEDDER` off), then the backend.
 */
async function startLocal(): Promise<void> {
  if (!ACCOUNT_MODE_RUNS_EMBEDDER) void supervisor.startEmbedder();
  await startBackend();
}

/**
 * Account mode's start: the runner, once this computer is paired. Unpaired is
 * not a failure — the account's web app pairs it after sign-in — and a runner
 * that does not attach is the page's to show, not the window's: the page is
 * already on screen, served from the account's origin.
 */
async function startRunner(): Promise<void> {
  if (!runnerSupervisor.paired) return;
  await runnerSupervisor.connect();
}

/** What the tray's Start does in the current mode. */
function startActive(): void {
  void (accountMode() ? startRunner() : startBackend());
}

app.whenReady().then(
  () => {
    // app.quit() does not stop this callback; a losing instance must not spawn
    // a backend or an embedder it would then orphan.
    if (!gotInstanceLock) return;

    // Before any window exists: the first thing the shell does is load a URL in
    // this scheme, and a handler registered after that is a blank frame.
    serveAppScheme();

    // The chrome next, before anything that can be slow — the keychain read in
    // loadSecrets(), spawning children: it owns the "starting…" screen, which
    // is what the user looks at while the backend comes up, and its renderer
    // loads in parallel with everything below. Its IPC handlers are registered
    // first because its renderer starts asking as soon as it exists. In local
    // mode the web app's own view is attached by the `server` handler below,
    // once /health has answered; in account mode there is nothing local to
    // wait for, and the account's web app is served at once.
    registerIpc(services, { isTrustedCloudSender, isCloudWebContents, isShellSender, isKeysSender });
    shellWindow.create({ hidden: launchedHidden() });
    const launchedInAccountMode = accountMode();
    if (launchedInAccountMode) shellWindow.serve(ACCOUNT_HOME_ROUTE);

    // As early as possible: the PATH the last launch's login shell reported is
    // put to use at once, and this launch's own login shell (a second or so
    // reading a profile) runs beside everything below instead of in front of
    // the first preflight.
    supervisor.warmUp();

    Menu.setApplicationMenu(
      Menu.buildFromTemplate(
        applicationMenuTemplate(process.platform, !app.isPackaged, { openKeys: () => keysWindow.open() }),
      ),
    );

    loadSecrets();

    // First among the children: the backend — or in account mode the runner
    // and its executor — is handed this child's resolved loopback URL, and a
    // cold model download benefits from every second before that. Never
    // awaited — it never blocks app startup, and the supervisor reports its
    // own failures. The model itself loads on the first request
    // (`ACCOUNT_MODE_RUNS_EMBEDDER`).
    void supervisor.reapStale();
    if (!launchedInAccountMode || ACCOUNT_MODE_RUNS_EMBEDDER) void supervisor.startEmbedder();

    // The tray outlives the window, which is the point: closing the window must
    // not stop the backend, and without a tray there would then be no way back
    // to it.
    tray = new AppTray({
      showWindow,
      start: startActive,
      stop: () => void (accountMode() ? runnerSupervisor.disconnect() : supervisor.disconnect()),
      quit: () => void quit.run(),
      checkForUpdate: () => void updates?.check(),
      restartToUpdate,
      subject: () => (accountMode() ? "the runner" : "the local server"),
      openKeys: () => keysWindow.open(),
      banner: () =>
        modeController.temporaryLocal
          ? {
              label: "Using TaskTrooper without your account for now",
              action: `Back to ${new URL(modeController.origin).host}`,
              run: () => void modeController.signIn().catch(() => undefined),
            }
          : null,
    });
    tray.create();
    tray.update(activeSnapshot());

    // After the tray, which renders the update state; `startUpdates` hands it
    // the real one as soon as there is one.
    startUpdates();

    // The login item's own record of where the app lives goes stale on Linux
    // when an AppImage is replaced by a newer file, and predates the --hidden
    // argument on Windows; rewriting it at launch keeps it pointing here. A
    // dev run shares the installed app's settings and must not repoint the
    // login item at node_modules' Electron.
    if (app.isPackaged && process.platform !== "darwin" && settingsStore.get().launchAtLogin) setLaunchAtLogin(true);

    // A Linux shutdown or logout reaches this process as logind's
    // PrepareForShutdown; preventDefault takes the delay lock that gives the
    // drain its few seconds. macOS already routes shutdown through before-quit,
    // and Windows through the window's session-end (see window.ts).
    if (process.platform === "linux") {
      powerMonitor.on("shutdown", (event?: Electron.Event) => {
        event?.preventDefault();
        void quit.run();
      });
    }

    // An earlier run's orphan that the boot's bounded sweep gave up on (a slow
    // `ps` or PowerShell listing) would otherwise live until the next launch,
    // which on a laptop that only ever sleeps can be weeks away.
    powerMonitor.on("resume", () => void supervisor.reapStale());

    // Each supervisor speaks for the window only in its own mode: during a
    // switch the outgoing one is still narrating its stop, and that belongs to
    // the page that is going away.
    supervisor.on("state", (snapshot: SupervisorSnapshot) => {
      if (!accountMode()) {
        tray?.update(snapshot);
        broadcast(SHELL_EVENTS.supervisorState, snapshot);
        toCloud(CLOUD_EVENTS.runnerState, hostSnapshot(snapshot));
      }
      // The watcher polls only while there is a backend to poll — `start()` is
      // idempotent, so calling it on every "running" event is harmless.
      if (supervisor.apiBase && !accountMode()) notifications.start();
      else notifications.stop();
    });
    runnerSupervisor.on("state", (snapshot: SupervisorSnapshot) => {
      if (!accountMode()) return;
      tray?.update(snapshot);
      broadcast(SHELL_EVENTS.supervisorState, snapshot);
      toCloud(CLOUD_EVENTS.runnerState, hostSnapshot(snapshot));
    });
    modeController.on("state", (state: AccountState) => {
      broadcast(SHELL_EVENTS.accountState, state);
      tray?.update(activeSnapshot());
    });
    // The embedder runs in both modes and a restart moves its port; a runner
    // already attached hears about it on its control channel.
    supervisor.on("embedder", (url: string) => {
      if (ACCOUNT_MODE_RUNS_EMBEDDER) runnerSupervisor.setEmbeddingsBaseURL(url);
    });

    // The moment somebody who just started a run walks away from it is when
    // the watcher should notice the run, not up to an idle interval later.
    app.on("browser-window-blur", () => notifications.nudge());

    /**
     * The backend's address, which is the one thing the window cannot be
     * created without.
     *
     * `PORT=0` means a restarted backend comes back on a DIFFERENT port, and
     * the page read its base once during preload. So a changed address is a
     * reload, not a no-op: without it the window would spend the rest of its
     * life calling a port nothing is listening on.
     */
    supervisor.on("server", (baseUrl) => {
      if (baseUrl === null || accountMode()) return;
      const moved = servedBase !== null && servedBase !== baseUrl;
      servedBase = baseUrl;
      shellWindow.serve();
      if (moved) shellWindow.reloadCloud();
    });

    // The preflight runs at launch rather than at the first start, so the setup
    // screen has answers the moment someone opens it. The start below joins
    // this same sweep rather than running its own.
    // A failure is narrated by the supervisor itself.
    if (launchedInAccountMode) {
      void runnerSupervisor.detect().then(
        () => tray?.update(activeSnapshot()),
        () => undefined,
      );
    } else {
      void supervisor.detect().then(
        () => tray?.update(supervisor.snapshot()),
        () => undefined,
      );
    }

    if (launchedInAccountMode) {
      if (settingsStore.get().autoConnect) void startRunner();
    } else if (settingsStore.get().autoConnect) {
      void startBackend();
    } else {
      shellWindow.markUnavailable(
        `The local server is not set to start automatically. Start it from the TaskTrooper ${
          process.platform === "darwin" ? "menu-bar" : "tray"
        } icon.`,
      );
    }
  },
  () => {
    const giveUp = new Promise<void>((resolve) => setTimeout(resolve, 10_000).unref());
    const drained = Promise.all([supervisor.drain(), runnerSupervisor.drain()]).then(
      () => undefined,
      () => undefined,
    );
    void Promise.race([drained, giveUp]).then(() => app.exit(1));
  },
);

// A menu-bar (or tray) app: closing the window hides the UI, it does not stop
// the backend. Quit is an explicit choice, from the tray or Cmd/Ctrl-Q, and it
// drains. On a Linux session with no tray to show the icon in, launching the
// app again is the way back: `second-instance` shows the window.
app.on("window-all-closed", () => {
  // Deliberately empty on every platform.
});

app.on("activate", () => {
  if (BrowserWindow.getAllWindows().length === 0) shellWindow.create();
  else showWindow();
});

app.on("before-quit", (event) => {
  // Once the drain is under way this handler has to get out of the way, or it
  // would cancel the very quit the drain is finishing — including the one
  // Squirrel raises when it takes over to install an update.
  if (quit.started) return;
  event.preventDefault();
  void quit.run();
});

// A terminal Ctrl-C, `kill`, or a closing console would otherwise end this
// process without the drain and leave every detached child running.
for (const signal of ["SIGTERM", "SIGINT", "SIGHUP"] as const) {
  process.on(signal, () => void quit.run());
}
