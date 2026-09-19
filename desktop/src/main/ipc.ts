import { ipcMain, type IpcMainEvent, type IpcMainInvokeEvent } from "electron";
import { CLOUD_CHANNELS, SHELL_CHANNELS, type ChooseDirectoryRequest } from "../ipc/channels.js";
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
  LogLine,
  PreflightReport,
  SupervisorSnapshot,
  UpdateStatus,
} from "../ipc/types.js";
import {
  ValidationError,
  validateChooseDirectory,
  validateDiagnosticsRequest,
  validateLogsRequest,
  validateOpenExternal,
  validateOverrides,
  validatePreferences,
  validatePreflightRequest,
  validateRestartChild,
  validateReveal,
} from "../ipc/validate.js";

/**
 * Every ipcMain handler, in one place.
 *
 * Each one does the same three things in the same order: check who is asking,
 * validate the payload, call into a service. Nothing here contains logic —
 * logic that lived in an IPC handler would be logic the app could not run
 * without a renderer attached, which is wrong for a supervisor that has to
 * keep the backend and its Claude Code sessions alive while the window is closed.
 *
 * ## The sender check
 *
 * The cloud handlers are the wide half, so they are guarded on EVERY call
 * rather than once at load, and the guard asks three questions:
 *
 *   1. Is the sender the exact WebContents we created for the web app? A
 *      second view, a popup, or the chrome's own renderer is not.
 *   2. Is it the TOP frame of that view? An iframe — including one injected by
 *      a compromised page — is a different frame and inherits nothing.
 *   3. Is that frame's origin still the trusted one? A view that navigated
 *      somewhere else keeps the preload but loses the powers.
 *
 * Any of those failing is a refusal, not a coerced default. `will-navigate` in
 * window.ts already prevents (3) from happening in the first place; this is
 * the check that does not depend on that one being complete.
 */

export interface IpcServices {
  appInfo(): AppInfo;
  supervisorState(): SupervisorSnapshot;
  cloudStatus(): CloudStatus;
  reloadCloud(): void;

  updateStatus(): UpdateStatus;
  checkForUpdate(): Promise<UpdateStatus>;
  /** Drain the tunnel, then hand off to Squirrel. Never returns. */
  restartToUpdate(): void;

  // --- the web app's local half ---
  hostInfo(): { app: string; version: string; platform: string };
  /**
   * The local backend's loopback origin, and the bearer it was started with.
   * Both synchronous because the preload needs them before there is a document;
   * both "" when the backend is not up.
   */
  apiBase(): string;
  apiToken(): string;

  runnerSnapshot(): HostRunnerSnapshot;
  connect(): Promise<HostRunnerSnapshot>;
  disconnect(): Promise<HostRunnerSnapshot>;
  restartChild(child: string): Promise<HostRunnerSnapshot>;
  logs(req: { child?: string; afterSeq?: number; limit?: number }): LogLine[];
  clearLogs(): void;

  getSettings(): HostSettings;
  setPreferences(patch: HostPreferences): Promise<HostSettings>;
  chooseWorkspace(): Promise<HostWorkspaceChoice | null>;
  chooseDirectory(options?: ChooseDirectoryRequest): Promise<string | null>;
  reveal(what: "workspace" | "previous-workspace" | "logs"): void;
  openExternal(url: string): boolean;

  preflight(force: boolean): Promise<PreflightReport>;
  diagnostics(force: boolean): Promise<Diagnostics>;
  setOverrides(patch: HostOverrides): Promise<Diagnostics>;
}

/** Answers "is this call really from the web app, right now?". */
export interface SenderGuard {
  isTrustedCloudSender(event: IpcMainInvokeEvent): boolean;
  /**
   * Identity only: is this our own cloud view, ignoring where it has navigated?
   *
   * Exists for the two channels answered during preload. `isTrustedCloudSender`
   * also checks the frame's origin, and at preload time there is no document
   * yet — the frame's URL is empty, so that check refuses the very page it was
   * written to trust. That is not a hole to widen for other channels: neither
   * takes an argument and neither changes anything.
   */
  isCloudWebContents(event: IpcMainEvent): boolean;
  /**
   * Answers the same question for the shell's own chrome.
   *
   * `ipcMain.handle` is process-wide, so a channel is reachable by any
   * WebContents that has an `ipcRenderer` — the hosted view's preload does.
   * The shell channels that only read were fine without this; the two that
   * update the app are not, on the same principle as the cloud guard: the
   * check belongs on the call, not on the assumption that nothing else can
   * make it.
   */
  isShellSender(event: IpcMainInvokeEvent): boolean;
}

/**
 * Wraps a handler so a validation failure is reported as itself rather than as
 * "an error occurred". A caller that sent a malformed payload has a bug and
 * should be told which field.
 */
function handle<T>(channel: string, fn: (payload: unknown, event: IpcMainInvokeEvent) => T | Promise<T>): void {
  ipcMain.handle(channel, async (event, payload: unknown) => {
    try {
      return await fn(payload, event);
    } catch (err) {
      if (err instanceof ValidationError) throw new Error(`invalid request: ${err.message}`);
      throw err;
    }
  });
}

export function registerIpc(services: IpcServices, guard: SenderGuard): void {
  /**
   * A cloud handler. The guard runs first, before the payload is even looked
   * at: an untrusted sender gets one refusal and no information about what the
   * channel would have accepted.
   */
  const cloud = <T>(channel: string, fn: (payload: unknown) => T | Promise<T>): void => {
    handle(channel, (payload, event) => {
      if (!guard.isTrustedCloudSender(event)) {
        throw new Error("refused: this request did not come from the TaskTrooper app running in this window.");
      }
      return fn(payload);
    });
  };

  /** A shell handler that changes something, and so has to know who is asking. */
  const shell = <T>(channel: string, fn: () => T | Promise<T>): void => {
    handle(channel, (_payload, event) => {
      if (!guard.isShellSender(event)) {
        throw new Error("refused: this request did not come from the TaskTrooper window.");
      }
      return fn();
    });
  };

  // --- the native chrome ---
  handle(SHELL_CHANNELS.appInfo, () => services.appInfo());
  handle(SHELL_CHANNELS.supervisorGet, () => services.supervisorState());
  handle(SHELL_CHANNELS.cloudStatus, () => services.cloudStatus());
  handle(SHELL_CHANNELS.updateGet, () => services.updateStatus());
  handle(SHELL_CHANNELS.cloudReload, () => {
    services.reloadCloud();
  });

  shell(SHELL_CHANNELS.updateCheck, () => services.checkForUpdate());
  // Returns immediately; the drain and the handoff to Squirrel happen after,
  // so the renderer is not waiting on a promise that resolves by the process
  // being replaced.
  shell(SHELL_CHANNELS.updateRestart, () => {
    services.restartToUpdate();
  });

  // --- the web app ---
  cloud(CLOUD_CHANNELS.hostInfo, () => services.hostInfo());

  /**
   * The two synchronous channels, and the only ones that answer an untrusted
   * sender at all — with "", which is what a browser would have used anyway.
   *
   * `sendSync` has no rejection path: an exception here surfaces in the page as
   * a thrown error during preload, which is a broken window rather than a
   * refused call.
   */
  const syncValue = (channel: string, read: () => string): void => {
    ipcMain.on(channel, (event) => {
      const value = guard.isCloudWebContents(event) ? read() : "";
      // Only the refusal is logged, and it earns its line: a page that gets no
      // base calls its own origin instead, and that failure surfaces several
      // layers from this decision.
      if (value === "") console.warn(`[${channel}] refused or not ready`);
      event.returnValue = value;
    });
  };
  syncValue(CLOUD_CHANNELS.apiBase, () => services.apiBase());
  syncValue(CLOUD_CHANNELS.apiToken, () => services.apiToken());

  cloud(CLOUD_CHANNELS.runnerSnapshot, () => services.runnerSnapshot());
  cloud(CLOUD_CHANNELS.runnerConnect, () => services.connect());
  cloud(CLOUD_CHANNELS.runnerDisconnect, () => services.disconnect());
  cloud(CLOUD_CHANNELS.runnerRestartChild, (payload) => services.restartChild(validateRestartChild(payload).child));
  cloud(CLOUD_CHANNELS.runnerLogs, (payload) => services.logs(validateLogsRequest(payload)));
  cloud(CLOUD_CHANNELS.runnerClearLogs, () => {
    services.clearLogs();
  });

  cloud(CLOUD_CHANNELS.settingsGet, () => services.getSettings());
  cloud(CLOUD_CHANNELS.settingsSetPreferences, (payload) => services.setPreferences(validatePreferences(payload)));
  cloud(CLOUD_CHANNELS.chooseWorkspace, () => services.chooseWorkspace());
  cloud(CLOUD_CHANNELS.chooseDirectory, (payload) =>
    services.chooseDirectory(validateChooseDirectory(payload)),
  );
  cloud(CLOUD_CHANNELS.reveal, (payload) => {
    services.reveal(validateReveal(payload).what);
  });
  cloud(CLOUD_CHANNELS.openExternal, (payload) => services.openExternal(validateOpenExternal(payload).url));

  cloud(CLOUD_CHANNELS.preflight, (payload) => services.preflight(validatePreflightRequest(payload).force ?? false));
  cloud(CLOUD_CHANNELS.diagnostics, (payload) => services.diagnostics(validateDiagnosticsRequest(payload).force ?? false));
  cloud(CLOUD_CHANNELS.overridesSet, (payload) => services.setOverrides(validateOverrides(payload)));
}
