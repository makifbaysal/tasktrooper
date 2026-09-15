import { contextBridge, ipcRenderer, type IpcRendererEvent } from "electron";
import { CLOUD_BRIDGE_KEY, CLOUD_CHANNELS, CLOUD_EVENTS } from "../ipc/channels.js";
import type {
  DesktopHost,
  DesktopRunnerHost,
  HostLogsRequest,
  HostOverrides,
  HostPreferences,
  HostRunnerSnapshot,
  HostSettings,
  HostWorkspaceChoice,
} from "../ipc/host.js";
import type { ChildId, Diagnostics, LogLine, PreflightReport } from "../ipc/types.js";

/**
 * The preload for the view that renders the web app — which is the entire
 * visible product.
 *
 *  - Every function here is a named action on a named channel. There is no
 *    generic `invoke(channel, args)`, because one would hand the page the whole
 *    ipcMain surface.
 *  - Every payload is validated in the main process before anything acts on it.
 *  - Every call's sender is verified in the main process against this exact
 *    view's top frame and origin, on every call.
 *  - Nothing here takes a command, and the only path the page can influence is
 *    the workspace folder — which it can ask to open a native picker for, and
 *    cannot name.
 *
 * There is no session, no sign-in and no OAuth on this bridge. The one value
 * that travels shell → page is `apiToken`, the bearer for a loopback server
 * this same process started.
 */

/**
 * One call, with Electron's wrapper taken off the failure.
 *
 * `ipcRenderer.invoke` rejects with "Error invoking remote method
 * 'cloud:runner:connect': Error: <the real message>". Every message the main
 * process throws here was written for a person to read, and the page renders it
 * directly — so the prefix has to come off on this side.
 */
async function call<T>(channel: string, payload?: unknown): Promise<T> {
  try {
    return (await ipcRenderer.invoke(channel, payload)) as T;
  } catch (err) {
    const raw = err instanceof Error ? err.message : String(err);
    const match = /Error invoking remote method '[^']+': (?:Error: )?(.*)/s.exec(raw);
    throw new Error((match?.[1] ?? raw).trim());
  }
}

function subscribe<T>(channel: string, cb: (payload: T) => void): () => void {
  // The page's callback is a function proxied across the context bridge.
  // Calling it is fine; storing it beyond the listener's life is not, so the
  // unsubscribe below is the only thing that outlives this closure.
  const listener = (_event: IpcRendererEvent, payload: T): void => {
    try {
      cb(payload);
    } catch {
      // A throwing page callback must not take down our listener list; the page
      // will see its own error in its own console.
    }
  };
  ipcRenderer.on(channel, listener);
  return () => {
    ipcRenderer.removeListener(channel, listener);
  };
}

const runner: DesktopRunnerHost = {
  snapshot: () => call<HostRunnerSnapshot>(CLOUD_CHANNELS.runnerSnapshot),
  subscribe: (cb) => subscribe<HostRunnerSnapshot>(CLOUD_EVENTS.runnerState, cb),

  connect: () => call<HostRunnerSnapshot>(CLOUD_CHANNELS.runnerConnect),
  disconnect: () => call<HostRunnerSnapshot>(CLOUD_CHANNELS.runnerDisconnect),
  restartChild: (child: ChildId) =>
    call<HostRunnerSnapshot>(CLOUD_CHANNELS.runnerRestartChild, { child }),

  logs: (req?: HostLogsRequest) => call<LogLine[]>(CLOUD_CHANNELS.runnerLogs, req ?? {}),
  subscribeLogs: (cb) => subscribe<LogLine[]>(CLOUD_EVENTS.runnerLogs, cb),
  clearLogs: () => call<void>(CLOUD_CHANNELS.runnerClearLogs),

  settings: () => call<HostSettings>(CLOUD_CHANNELS.settingsGet),
  setPreferences: (patch: HostPreferences) =>
    call<HostSettings>(CLOUD_CHANNELS.settingsSetPreferences, patch),
  chooseWorkspace: () => call<HostWorkspaceChoice | null>(CLOUD_CHANNELS.chooseWorkspace),
  reveal: (what) => call<void>(CLOUD_CHANNELS.reveal, { what }),
  openExternal: (url) => call<boolean>(CLOUD_CHANNELS.openExternal, { url }),

  preflight: (force?: boolean) => call<PreflightReport>(CLOUD_CHANNELS.preflight, { force }),

  diagnostics: (force?: boolean) => call<Diagnostics>(CLOUD_CHANNELS.diagnostics, { force }),
  setOverrides: (patch: HostOverrides) => call<Diagnostics>(CLOUD_CHANNELS.overridesSet, patch),
};

/**
 * The two synchronous reads, made once while the preload runs.
 *
 * They have to be values before the page's first line executes: `api.ts` reads
 * the base and the token on every request, so a promise here would mean an
 * await in front of every call in the app. `sendSync` blocks this renderer for
 * one main-process property read, at a moment when there is no document to
 * block.
 *
 * An untrusted sender is answered with an empty string rather than an error, so
 * the page falls back to browser behaviour instead of failing to start over a
 * channel it should not have reached.
 */
function syncString(channel: string): string | undefined {
  const value: unknown = ipcRenderer.sendSync(channel);
  return typeof value === "string" && value !== "" ? value : undefined;
}

const apiBase = syncString(CLOUD_CHANNELS.apiBase);
const apiToken = syncString(CLOUD_CHANNELS.apiToken);

const host: DesktopHost = {
  ...(apiBase !== undefined ? { apiBase } : {}),
  ...(apiToken !== undefined ? { apiToken } : {}),
  info: () => call<{ app: string; version: string; platform: string }>(CLOUD_CHANNELS.hostInfo),
  runner,
};

contextBridge.exposeInMainWorld(
  CLOUD_BRIDGE_KEY,
  Object.freeze({ ...host, runner: Object.freeze(runner) }),
);
