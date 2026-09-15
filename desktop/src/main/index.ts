import { accessSync, constants, statSync } from "node:fs";
import { BrowserWindow, app, type IpcMainEvent, type IpcMainInvokeEvent } from "electron";
import { autoUpdater } from "electron-updater";
import { CLOUD_EVENTS, SHELL_EVENTS } from "../ipc/channels.js";
import type {
  HostOverrides,
  HostPreferences,
  HostRunnerSnapshot,
  HostSettings,
  HostWorkspaceChoice,
} from "../ipc/host.js";
import type {
  AppInfo,
  CloudStatus,
  Diagnostics,
  PreflightReport,
  SupervisorSnapshot,
  UpdateStatus,
} from "../ipc/types.js";
import { SecretStore, type LocalSecrets } from "./config/secrets.js";
import { SettingsStore } from "./config/settings.js";
import { checkWorkspace, pickWorkspace, reveal } from "./config/workspace.js";
import { registerIpc, type IpcServices } from "./ipc.js";
import { quitSequence } from "./quit.js";
import { APP_ORIGIN, originOf, registerAppSchemePrivileges, serveAppScheme } from "./services/app-scheme.js";
import { binDir, dataDir } from "./services/detect.js";
import { FEED_DEBUG_ENV, UpdateService, resolveFeed, type UpdaterBackend } from "./services/updater.js";
import { Supervisor } from "./supervisor/supervisor.js";
import { AppTray, setLaunchAtLogin } from "./tray.js";
import { openExternally, Shell } from "./window.js";

/**
 * The main process: wiring, and only wiring.
 *
 * Everything with behaviour lives in supervisor/, config/ or services/, so this
 * file reads as the list of decisions the app makes at the top level — what
 * happens at launch, what happens on quit, and which sender is allowed to ask
 * for any of it.
 */

// One instance. Two supervisors on one Mac would each start a backend against
// the same data directory, and embedded Postgres would refuse the second — or,
// worse, not refuse it.
if (!app.requestSingleInstanceLock()) app.quit();

const settingsStore = new SettingsStore();
const secretStore = new SecretStore();
const supervisor = new Supervisor();

/**
 * The API key and the MCP secrets key this install was generated with, held in
 * memory after the first read. `null` only when the login keychain would not
 * provide an encryption key, which is a state the supervisor reports rather
 * than one this file can fix.
 */
let secrets: LocalSecrets | null = null;
let tray: AppTray | null = null;

/**
 * Auto-update, built at `whenReady` because resolving the feed reads
 * `app.isPackaged` and `app.getPath`.
 *
 * `null` until then, and it stays usable when the feed is absent — a
 * development run and a `npm run package` build both report `unsupported` and
 * do nothing, which is the honest state rather than a failure to draw.
 */
let updates: UpdateService | null = null;

/** The base the window was last told about, so a new port can be noticed. */
let servedBase: string | null = null;

// Before whenReady, and it has to be: the privilege table is read once while
// the network service starts. See services/app-scheme.ts.
registerAppSchemePrivileges();

const shellWindow = new Shell({
  origin: () => APP_ORIGIN,
  onCloudStatus: (status) => broadcast(SHELL_EVENTS.cloudStatus, status),
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

/** Push to the web app. Only ever the two things it subscribes to. */
function toCloud(channel: string, payload: unknown): void {
  send(shellWindow.cloudContents, channel, payload);
}

function reconfigure(): void {
  supervisor.configure({
    secrets,
    settings: settingsStore.get(),
    overrides: settingsStore.overrides(),
  });
}

/**
 * The supervisor snapshot, reduced to what the web app renders.
 *
 * The narrowing is the point: the page must not learn a pid or a path from
 * here. It gets what is running, whether it is healthy, and one line saying why
 * not.
 */
function hostSnapshot(snapshot: SupervisorSnapshot = supervisor.snapshot()): HostRunnerSnapshot {
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
    if (!usable) throw new Error(`${value} is not a file this Mac can run.`);
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
  destroyTray: () => {
    tray?.destroy();
    tray = null;
  },
  drain: () => supervisor.drain(),
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
    origin: supervisor.apiBase ?? "",
  };
}

const services: IpcServices = {
  appInfo,
  supervisorState: () => supervisor.snapshot(),
  cloudStatus: (): CloudStatus => shellWindow.status,
  reloadCloud: () => shellWindow.reloadCloud(),

  updateStatus: (): UpdateStatus => updates?.status ?? { phase: "unsupported", detail: "The updater has not started yet." },
  checkForUpdate: async (): Promise<UpdateStatus> =>
    (await updates?.check()) ?? { phase: "unsupported", detail: "The updater has not started yet." },
  restartToUpdate,

  hostInfo: () => ({ app: "tasktrooper-desktop", version: app.getVersion(), platform: process.platform }),

  // The page is served from APP_ORIGIN, which has no gateway behind it, so it
  // has to be told where the API lives — and, because the backend picks its own
  // port, that address is not knowable until it has answered. The window is not
  // created until then, so an empty answer here means something is wrong rather
  // than something is early.
  apiBase: () => supervisor.apiBase ?? "",
  apiToken: () => secrets?.api_token ?? "",

  runnerSnapshot: () => hostSnapshot(),
  connect: async () => hostSnapshot(await supervisor.connect()),
  disconnect: async () => hostSnapshot(await supervisor.disconnect()),
  restartChild: async (child) => hostSnapshot(await supervisor.restartChild(child as never)),
  logs: (req) => supervisor.logs(req.child as never, req.afterSeq ?? 0, req.limit),
  clearLogs: () => supervisor.clearLogs(),

  getSettings: (): HostSettings => settingsStore.get(),

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
    const next = settingsStore.set({ workspaceDir: chosen.path });
    reconfigure();
    if (supervisor.running) {
      await supervisor.disconnect();
      void supervisor.connect();
    }
    return { check: chosen, settings: next };
  },

  reveal: (what) => {
    if (what === "workspace") reveal(settingsStore.get().workspaceDir);
    else if (what === "previous-workspace") {
      const previous = settingsStore.previousWorkspaceDir();
      if (previous) reveal(previous);
    } else reveal(app.getPath("userData"));
  },

  openExternal: (url) => openExternally(url),

  /**
   * The environment checklist. Answered from the last sweep unless the caller
   * asks for a fresh one, because the caller that asks is the one whose user
   * has just installed something.
   */
  preflight: (force: boolean): Promise<PreflightReport> => runPreflight(force),

  diagnostics: (force: boolean): Promise<Diagnostics> => buildDiagnostics(force),
  setOverrides: async (patch: HostOverrides): Promise<Diagnostics> => {
    settingsStore.setOverrides(checkedOverrides(patch));
    reconfigure();
    return buildDiagnostics(true);
  },
};

/**
 * Is this call really from the web app's view, right now?
 *
 * Three questions, all of which must answer yes: the exact WebContents we
 * created for it, its top frame, and an origin that is still the trusted one. A
 * popup, an iframe, the chrome's own renderer and a view that navigated
 * elsewhere all fail at least one.
 */
function isTrustedCloudSender(event: IpcMainInvokeEvent): boolean {
  const contents = shellWindow.cloudContents;
  if (!contents || contents.isDestroyed() || event.sender !== contents) return false;

  const frame = event.senderFrame;
  const main = contents.mainFrame;
  if (!frame || frame.processId !== main.processId || frame.routingId !== main.routingId) return false;

  try {
    return originOf(frame.url) === APP_ORIGIN;
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
 * Only the two synchronous channels use this. Neither takes an argument and
 * neither changes anything.
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
 * The preflight, run again or answered from the last sweep.
 *
 * "The last sweep" is never nothing: detection runs at launch, so the setup
 * screen has answers the moment it opens. `force` is what the "Check again"
 * button calls, and it must actually re-probe — a cached report after a fix
 * tells the user their fix did not work.
 */
function runPreflight(force: boolean): Promise<PreflightReport> {
  const current = supervisor.preflight;
  if (!force && current.items.length > 0) return Promise.resolve(current);
  return supervisor.detect();
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
 * This is the launch path and also what the tray's Connect calls. A failure is
 * not thrown at anybody: the supervisor already narrated it, and the chrome's
 * own offline screen is where a user is looking.
 */
async function startBackend(): Promise<void> {
  const snapshot = await supervisor.connect();
  if (snapshot.state === "failed") {
    shellWindow.markUnavailable(snapshot.detail ?? "The local server did not start.");
  }
}

app.whenReady().then(
  () => {
    // Before any window exists: the first thing the shell does is load a URL in
    // this scheme, and a handler registered after that is a blank frame.
    serveAppScheme();

    try {
      secrets = secretStore.ensure();
    } catch {
      // Left null on purpose. `supervisor.connect()` refuses with the sentence
      // that explains it, which is where the user is looking — throwing here
      // would be an app that exits at launch with a dialog nobody can act on.
      secrets = null;
    }
    reconfigure();

    // Unconditionally, and first among the children: the backend is handed this
    // child's resolved loopback URL, and a cold model download benefits from
    // every second before that. Never awaited — it never blocks app startup,
    // and the supervisor reports its own failures.
    void supervisor.startEmbedder();

    // Built before the tray, because the tray renders the update state.
    updates = new UpdateService({
      backend: autoUpdater as unknown as UpdaterBackend,
      feed: resolveFeed({
        packaged: app.isPackaged,
        resourcesPath: process.resourcesPath,
      }),
      onStatus: (status) => {
        tray?.updateStatus(status);
        broadcast(SHELL_EVENTS.updateStatus, status);
      },
      debug: !!process.env[FEED_DEBUG_ENV],
    });

    // The tray outlives the window, which is the point: closing the window must
    // not stop the backend, and without a tray there would then be no way back
    // to it.
    tray = new AppTray({
      showWindow,
      start: () => void startBackend(),
      stop: () => void supervisor.disconnect(),
      quit: () => void quit.run(),
      checkForUpdate: () => void updates?.check(),
      restartToUpdate,
    });
    tray.create();
    tray.update(supervisor.snapshot());
    tray.updateStatus(updates.status);

    supervisor.on("state", (snapshot: SupervisorSnapshot) => {
      tray?.update(snapshot);
      broadcast(SHELL_EVENTS.supervisorState, snapshot);
      toCloud(CLOUD_EVENTS.runnerState, hostSnapshot(snapshot));
    });
    supervisor.on("logs", (lines) => toCloud(CLOUD_EVENTS.runnerLogs, lines));

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
      if (baseUrl === null) return;
      const moved = servedBase !== null && servedBase !== baseUrl;
      servedBase = baseUrl;
      shellWindow.serve();
      if (moved) shellWindow.reloadCloud();
    });

    registerIpc(services, { isTrustedCloudSender, isCloudWebContents, isShellSender });

    // The chrome, immediately: it owns the "starting…" screen, which is what
    // the user looks at while the backend comes up. The web app's own view is
    // attached by the `server` handler above, once /health has answered.
    shellWindow.create();
    updates.start();

    // The preflight runs at launch rather than at the first start, so the setup
    // screen has answers the moment someone opens it.
    void supervisor.detect().then(() => {
      tray?.update(supervisor.snapshot());
    });

    if (settingsStore.get().autoConnect) {
      void startBackend();
    } else {
      shellWindow.markUnavailable(
        "The local server is not set to start automatically. Start it from the TaskTrooper menu-bar icon.",
      );
    }
  },
  () => app.exit(1),
);

// A menu-bar app: closing the window hides the UI, it does not stop the
// backend. Quit is an explicit choice, from the tray or Cmd-Q, and it drains.
app.on("window-all-closed", () => {
  // Deliberately empty on macOS.
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
