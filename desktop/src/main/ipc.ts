import { ipcMain, type IpcMainEvent, type IpcMainInvokeEvent } from "electron";
import {
  CLOUD_CHANNELS,
  KEYS_CHANNELS,
  SHELL_CHANNELS,
  type ChooseDirectoryRequest,
  type KeySetRequest,
  type KeysWindowPrefill,
  type McpServerSetRequest,
} from "../ipc/channels.js";
import type {
  HostOverrides,
  HostPreferences,
  HostRunnerSnapshot,
  HostSettings,
  HostWorkspaceChoice,
} from "../ipc/host.js";
import type {
  AccountMode,
  AccountState,
  AnalyticsState,
  AppInfo,
  CloudStatus,
  Diagnostics,
  LogLine,
  PreflightReport,
  McpServersState,
  ProviderKeysState,
  RunnerPairingBundle,
  RunnerPairingSummary,
  SupervisorSnapshot,
  ToolchainId,
  ToolchainJobState,
  ToolchainReport,
  UpdateStatus,
} from "../ipc/types.js";
import type { McpServerConsent } from "./keys/keys.js";
import {
  ValidationError,
  validateAnalyticsIdentify,
  validateAnalyticsSet,
  validateAnalyticsTrack,
  validateChatFocus,
  validateChooseDirectory,
  validateDiagnosticsRequest,
  validateKeyRemove,
  validateKeySet,
  validateKeysWindowPrefill,
  validateMcpServerRemove,
  validateMcpServerSet,
  validateLogsRequest,
  validateLogsStream,
  validateOpenExternal,
  validateOverrides,
  validatePairRequest,
  validatePreferences,
  validatePreflightRequest,
  validateRestartChild,
  validateReveal,
  validateSignIn,
  validateToolchainId,
  validateToolchainIds,
  validateToolchainReportRequest,
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
 *   3. Is that frame's origin still the trusted one — `app://tasktrooper` in
 *      local mode, the account's origin in account mode, never both? A view
 *      that navigated somewhere else keeps the preload but loses the powers.
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

  // --- account mode ---
  accountMode(): AccountMode;
  accountState(): AccountState;
  accountSignIn(origin?: string): Promise<AccountState>;
  accountSignOut(): Promise<AccountState>;
  /** Run locally for now, still signed in — the chrome's failure screen. */
  accountUseLocalForNow(): Promise<AccountState>;
  /** Bring up the API-key window. */
  openKeys(prefill?: KeysWindowPrefill): void;
  /** The account's MCP Servers page: this computer's own servers, the same list the key window keeps. */
  accountMcpList(): McpServersState;
  accountMcpSet(request: McpServerSetRequest): McpServersState;
  accountMcpRemove(name: string): McpServersState;
  /** What `accountMcpSet(request)` would run that is not running already; null when nothing. */
  accountMcpConsent(request: McpServerSetRequest): McpServerConsent | null;
  /** The native "Add the MCP server …?" dialog; true when the person agreed. */
  confirmMcpServer(consent: McpServerConsent): Promise<boolean>;

  analyticsState(): AnalyticsState;
  setAnalytics(on: boolean): AnalyticsState;
  trackAnalytics(name: string, params: Record<string, string | number>): void;
  identifyAnalytics(userId: string | null): void;

  // --- the API-key window ---
  keysList(): ProviderKeysState;
  keysSet(request: KeySetRequest): ProviderKeysState;
  keysRemove(id: string): ProviderKeysState;
  keysPrefill(): KeysWindowPrefill | null;
  keysMcpList(): McpServersState;
  keysMcpSet(request: McpServerSetRequest): McpServersState;
  keysMcpRemove(name: string): McpServersState;

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
  /** Push log batches to the page (true) or stop (false). */
  streamLogs(on: boolean): void;
  clearLogs(): void;

  // Account mode only.
  pair(bundle: RunnerPairingBundle): Promise<HostRunnerSnapshot>;
  unpair(): Promise<HostRunnerSnapshot>;
  pairingInfo(): RunnerPairingSummary | null;
  restartRunner(): Promise<HostRunnerSnapshot>;

  getSettings(): HostSettings;
  setPreferences(patch: HostPreferences): Promise<HostSettings>;
  chooseWorkspace(): Promise<HostWorkspaceChoice | null>;
  chooseDirectory(options?: ChooseDirectoryRequest): Promise<string | null>;
  reveal(what: "workspace" | "previous-workspace" | "logs"): void;
  openExternal(url: string): boolean;
  reportChatFocus(focus: { agentId: string | null; sessionId: string | null }): void;

  preflight(force: boolean): Promise<PreflightReport>;
  diagnostics(force: boolean): Promise<Diagnostics>;
  setOverrides(patch: HostOverrides): Promise<Diagnostics>;

  // --- toolchain setup ---
  toolchainReport(force: boolean): Promise<ToolchainReport>;
  /**
   * The install order for `ids`: their missing dependencies first, nothing
   * installed, nothing manual, nothing the running job already has queued.
   */
  toolchainPlan(ids: ToolchainId[]): Promise<ToolchainId[]>;
  toolchainState(): ToolchainJobState;
  /** Plans `ids` and starts (or appends to) the job. */
  toolchainInstall(ids: ToolchainId[]): Promise<ToolchainJobState>;
  /** Starts (or appends to) the job with exactly `planned`, which is already a plan — no planning again. */
  toolchainEnqueue(planned: ToolchainId[]): ToolchainJobState;
  /** The native "Install developer tools?" dialog for a plan; true when the person said Install. */
  confirmToolchainInstall(planned: ToolchainId[]): Promise<boolean>;
  toolchainCancel(): ToolchainJobState;
  toolchainDismiss(): ToolchainJobState;
  /** Opens the tool's page from the table in the real browser. */
  toolchainOpenPage(id: ToolchainId): Promise<void>;
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
  /**
   * The API-key window's own page, its top frame — and nothing else: not the
   * web app's view on either mode's origin, not the chrome. The only sender
   * that may hand this process a key.
   */
  isKeysSender(event: IpcMainInvokeEvent): boolean;
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
  handle(SHELL_CHANNELS.accountState, () => services.accountState());
  shell(SHELL_CHANNELS.accountUseLocalForNow, () => services.accountUseLocalForNow());

  // --- the API-key window ---
  /**
   * The guard runs before the payload is read, like the cloud one: a sender
   * that is not the key window learns nothing, including whether its key
   * would have been accepted.
   */
  const keys = <T>(channel: string, fn: (payload: unknown) => T | Promise<T>): void => {
    handle(channel, (payload, event) => {
      if (!guard.isKeysSender(event)) {
        throw new Error("refused: API keys are changed only in TaskTrooper's own API keys window.");
      }
      return fn(payload);
    });
  };
  keys(KEYS_CHANNELS.list, () => services.keysList());
  keys(KEYS_CHANNELS.set, (payload) => services.keysSet(validateKeySet(payload)));
  keys(KEYS_CHANNELS.prefill, () => services.keysPrefill());
  keys(KEYS_CHANNELS.remove, (payload) => services.keysRemove(validateKeyRemove(payload).id));
  keys(KEYS_CHANNELS.mcpList, () => services.keysMcpList());
  keys(KEYS_CHANNELS.mcpSet, (payload) => services.keysMcpSet(validateMcpServerSet(payload)));
  keys(KEYS_CHANNELS.mcpRemove, (payload) => services.keysMcpRemove(validateMcpServerRemove(payload).name));

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
      if (value === "" && services.accountMode() === "local") console.warn(`[${channel}] refused or not ready`);
      event.returnValue = value;
    });
  };
  syncValue(CLOUD_CHANNELS.apiBase, () => services.apiBase());
  syncValue(CLOUD_CHANNELS.apiToken, () => services.apiToken());
  syncValue(CLOUD_CHANNELS.accountMode, () => services.accountMode());

  cloud(CLOUD_CHANNELS.accountState, () => services.accountState());
  cloud(CLOUD_CHANNELS.accountSignIn, (payload) => services.accountSignIn(validateSignIn(payload).origin));
  cloud(CLOUD_CHANNELS.accountSignOut, () => services.accountSignOut());
  cloud(CLOUD_CHANNELS.accountOpenKeys, (payload) => {
    services.openKeys(payload === undefined || payload === null ? undefined : validateKeysWindowPrefill(payload));
  });

  /**
   * This computer's own MCP servers, from the account's page. A server is a
   * command the runner starts here, so a change that runs something new waits
   * for the person's yes in a native dialog, exactly as the toolchain's
   * installs do; a remote page can ask, never agree.
   */
  const accountOnly = (): void => {
    if (services.accountMode() !== "account") throw new Error("This computer's own MCP servers are kept in account mode only.");
  };
  cloud(CLOUD_CHANNELS.accountMcpList, () => {
    accountOnly();
    return services.accountMcpList();
  });
  let confirmingMcp = false;
  cloud(CLOUD_CHANNELS.accountMcpSet, async (payload) => {
    const request = validateMcpServerSet(payload);
    accountOnly();
    const consent = services.accountMcpConsent(request);
    if (consent) {
      if (confirmingMcp) throw new Error("busy");
      confirmingMcp = true;
      try {
        if (!(await services.confirmMcpServer(consent))) throw new Error("declined");
      } finally {
        confirmingMcp = false;
      }
    }
    return services.accountMcpSet(request);
  });
  cloud(CLOUD_CHANNELS.accountMcpRemove, (payload) => {
    const { name } = validateMcpServerRemove(payload);
    accountOnly();
    return services.accountMcpRemove(name);
  });

  cloud(CLOUD_CHANNELS.runnerSnapshot, () => services.runnerSnapshot());
  cloud(CLOUD_CHANNELS.runnerConnect, () => services.connect());
  cloud(CLOUD_CHANNELS.runnerDisconnect, () => services.disconnect());
  cloud(CLOUD_CHANNELS.runnerRestartChild, (payload) => services.restartChild(validateRestartChild(payload).child));
  cloud(CLOUD_CHANNELS.runnerLogs, (payload) => services.logs(validateLogsRequest(payload)));
  cloud(CLOUD_CHANNELS.runnerLogsStream, (payload) => {
    services.streamLogs(validateLogsStream(payload).on);
  });
  cloud(CLOUD_CHANNELS.runnerClearLogs, () => {
    services.clearLogs();
  });

  cloud(CLOUD_CHANNELS.runnerPair, (payload) => services.pair(validatePairRequest(payload).bundle));
  cloud(CLOUD_CHANNELS.runnerUnpair, () => services.unpair());
  cloud(CLOUD_CHANNELS.runnerPairingInfo, () => services.pairingInfo());
  cloud(CLOUD_CHANNELS.runnerRestart, () => services.restartRunner());

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
  cloud(CLOUD_CHANNELS.chatReportFocus, (payload) => {
    services.reportChatFocus(validateChatFocus(payload));
  });

  cloud(CLOUD_CHANNELS.preflight, (payload) => services.preflight(validatePreflightRequest(payload).force ?? false));
  cloud(CLOUD_CHANNELS.diagnostics, (payload) => services.diagnostics(validateDiagnosticsRequest(payload).force ?? false));
  cloud(CLOUD_CHANNELS.overridesSet, (payload) => services.setOverrides(validateOverrides(payload)));

  // Both modes: Settings → General is the one place this switch lives — the
  // local bundle's, or in account mode the account's own page.
  cloud(CLOUD_CHANNELS.analyticsGet, () => services.analyticsState());
  cloud(CLOUD_CHANNELS.analyticsSet, (payload) => services.setAnalytics(validateAnalyticsSet(payload).on));
  cloud(CLOUD_CHANNELS.analyticsTrack, (payload) => {
    const { name, params } = validateAnalyticsTrack(payload);
    services.trackAnalytics(name, params);
  });
  cloud(CLOUD_CHANNELS.analyticsIdentify, (payload) => {
    services.identifyAnalytics(validateAnalyticsIdentify(payload).userId);
  });

  /**
   * Toolchain setup, in both modes. What runs is decided by the id and main's
   * table, never by the page; the page can only choose which tools.
   *
   * An install asked for by the account's page — a remote origin — needs the
   * person's own yes in a native dialog listing exactly what will be
   * installed, and then installs exactly that list: a plan made again after
   * the dialog could add a dependency nobody agreed to. One dialog at a time —
   * a page cannot stack a second request behind the first one's answer.
   * Local mode's page is this app's own bundle, the same code as this
   * dialog, so it does not ask twice.
   */
  cloud(CLOUD_CHANNELS.toolchainReport, (payload) =>
    services.toolchainReport(validateToolchainReportRequest(payload).force ?? false),
  );
  cloud(CLOUD_CHANNELS.toolchainPlan, (payload) => services.toolchainPlan(validateToolchainIds(payload).ids));
  cloud(CLOUD_CHANNELS.toolchainState, () => services.toolchainState());
  let confirming = false;
  cloud(CLOUD_CHANNELS.toolchainInstall, async (payload) => {
    const { ids } = validateToolchainIds(payload);
    if (services.accountMode() !== "account") return services.toolchainInstall(ids);
    if (confirming) throw new Error("busy");
    confirming = true;
    try {
      const planned = await services.toolchainPlan(ids);
      if (planned.length === 0) return services.toolchainState();
      if (!(await services.confirmToolchainInstall(planned))) throw new Error("declined");
      return services.toolchainEnqueue(planned);
    } finally {
      confirming = false;
    }
  });
  cloud(CLOUD_CHANNELS.toolchainCancel, () => services.toolchainCancel());
  cloud(CLOUD_CHANNELS.toolchainDismiss, () => services.toolchainDismiss());
  cloud(CLOUD_CHANNELS.toolchainOpenPage, (payload) => services.toolchainOpenPage(validateToolchainId(payload).id));

  cloud(CLOUD_CHANNELS.updateGet, () => services.updateStatus());
  cloud(CLOUD_CHANNELS.updateCheck, () => services.checkForUpdate());
  cloud(CLOUD_CHANNELS.updateRestart, () => {
    services.restartToUpdate();
  });
}
