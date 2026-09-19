// The bridge to the TaskTrooper desktop shell.
//
// The desktop app is this app in a window, plus the local machine. The shell
// starts an embedded Postgres and the Go server on this Mac, serves these
// assets, and runs the agent CLIs; every screen a user sees is served from
// this origin.
//
// What the shell adds is the set of things a browser tab cannot do: supervise
// those child processes, open a native folder picker, and report what this Mac
// can and cannot run (the environment preflight — Claude account, Xcode CLT, …).
//
// Those arrive as `window.__tasktrooperDesktop`, installed by the shell's
// preload. Its presence *is* the "we are running inside the desktop shell"
// contract, and only that shell can install it — there is no postMessage
// handler, no global function and no custom event a page could impersonate.
//
// Every `runner.*` call is validated in the shell's main process and its
// sender origin is verified on every call — this file cannot grant itself
// anything, and a page on another origin cannot reach the bridge at all.
//
// This interface is one half of a contract whose other half is
// `desktop/src/ipc/host.ts`. They are separate declarations because this app
// builds in a browser with no knowledge of that package; keep them in step.

/**
 * The processes the shell supervises. `agent-server` is the Go backend this
 * app talks to, so `connect()`/`disconnect()` on the host below start and stop
 * the backend itself.
 */
export type DesktopChildId = "embedder" | "agent-server" | "appium";

export type DesktopChildState =
  | "idle"
  | "starting"
  | "waiting-health"
  | "healthy"
  | "crashed"
  | "restarting"
  | "stopping"
  | "stopped"
  | "failed"
  | "skipped";

export interface DesktopChild {
  id: DesktopChildId;
  state: DesktopChildState;
  /** False for a child this Mac cannot run. Nothing sets it today. */
  enabled: boolean;
  restarts: number;
  detail?: string;
  nextRestartAt?: number;
  exitedAt?: number;
}

/**
 * A REQUIRED preflight item that is not `ok`, which is why Connect was
 * refused. Optional items never produce one of these.
 */
export interface DesktopBlocker {
  /** The preflight item that failed — see `DesktopPreflightId` below. */
  id: string;
  title: string;
  because: string;
  /** What to do about it, in words. Always present. */
  remediation: string;
  /** The command form of the remediation, when there is one. Never run for the user. */
  command: string;
  runnable: boolean;
}

export interface DesktopRunnerSnapshot {
  phase: "idle" | "connecting" | "connected" | "degraded" | "stopping" | "failed";
  /** One line for a person: "starting the database…". Narration, not a log. */
  detail?: string;
  since: number;
  children: DesktopChild[];
  blocker?: DesktopBlocker;
}

export interface DesktopLogLine {
  /** Monotonic within a session; used to de-duplicate overlapping batches. */
  seq: number;
  child: DesktopChildId | "supervisor";
  at: number;
  stream: "stdout" | "stderr";
  text: string;
  level?: string;
}

/**
 * Desktop notification switches. `humanNeeded` covers both a blocked question
 * and a blocked human-decision park — both mean "the board is stuck on you".
 */
export interface DesktopNotificationPreferences {
  enabled: boolean;
  analizReview: boolean;
  humanUat: boolean;
  humanNeeded: boolean;
  agentComments: boolean;
}

export interface DesktopSettings {
  workspaceDir: string;
  launchAtLogin: boolean;
  autoConnect: boolean;
  notifications: DesktopNotificationPreferences;
}

/**
 * What this page may set directly: two switches, plus the notification
 * preferences (sent as a full object).
 *
 * The workspace folder is deliberately not here — this page can ask for the
 * native picker, which the user then drives, but it cannot name a path.
 */
export interface DesktopPreferences {
  launchAtLogin?: boolean;
  autoConnect?: boolean;
  notifications?: DesktopNotificationPreferences;
}

export interface DesktopWorkspaceCheck {
  path: string;
  ok: boolean;
  error?: string;
  warnings: string[];
  freeBytes?: number;
  exists: boolean;
}

export interface DesktopWorkspaceChoice {
  check: DesktopWorkspaceCheck;
  /** Present when the choice was valid and has been applied. */
  settings?: DesktopSettings;
}

export interface DesktopDiagnostics {
  /** The same checklist `preflight()` returns — diagnostics no longer probes anything of its own. */
  preflight: DesktopPreflightReport;
  workspaceDir: string;
  binDir: string;
  dataFree?: number;
  app: {
    version: string;
    electron: string;
    chrome: string;
    node: string;
    platform: string;
    arch: string;
    packaged: boolean;
    origin: string;
  };
}

/**
 * The environment checklist, by id — mirrors `PREFLIGHT_IDS` in
 * `desktop/src/ipc/types.ts`. `claude-account` is split from `claude` (the
 * binary) on purpose: they are two different questions with two different
 * fixes, and collapsing them into one item is how a user ends up reinstalling
 * something that was never the problem.
 */
export const DESKTOP_PREFLIGHT_IDS = [
  "agent-server",
  "postgres",
  "git",
  "claude",
  "claude-account",
  "chrome",
  "xcode-clt",
  // The mobile half, all optional, mirroring the shell's own list. Optional is
  // a judgement about who is looking: neither side can know whether this member
  // does mobile work, and blocking Connect on a hub somebody may never use
  // would stop a person who only ever writes code.
  "appium",
  "appium-xcuitest",
  "appium-uiautomator2",
  "android-sdk",
  // The other local agent CLIs a task can run on, all optional — mirrors the
  // shell's own list; see that file's comment for why these are
  // binary-presence only.
  "agy",
  "cursor-agent",
  "opencode",
] as const;
export type DesktopPreflightId = (typeof DESKTOP_PREFLIGHT_IDS)[number];

/**
 * Three outcomes, not two: `missing` (never installed / never reachable) and
 * `unusable` (found, but not usable — signed out, wrong plan, model not
 * loaded) take different remediations, so collapsing them into one "failing"
 * state would lose the distinction the remediation text depends on.
 */
export type DesktopPreflightStatus = "ok" | "missing" | "unusable";

export type DesktopPreflightSource =
  | "path"
  | "bundled"
  | "dev-bin"
  | "homebrew"
  | "npm-prefix"
  | "home"
  | "app-bundle"
  | "override"
  | "network";

export interface DesktopPreflightItem {
  id: DesktopPreflightId;
  /** For a person, not a log: "Claude account". */
  label: string;
  /** Required items block Connect; optional ones never do. */
  required: boolean;
  status: DesktopPreflightStatus;
  /** Absolute path, or the address that answered. Present when found. */
  path?: string;
  version?: string;
  source?: DesktopPreflightSource;
  /** What was observed. Present whenever it explains the status. */
  detail?: string;
  /** Present for every item that is not `ok`; absent for one that is. */
  remediation?: string;
  /** The command form of the remediation, when there is one to copy. */
  command?: string;
}

export interface DesktopPreflightReport {
  /** Epoch ms. A snapshot, not a live value — a stale report must read as stale. */
  generatedAt: number;
  items: DesktopPreflightItem[];
  /** True only when every REQUIRED item is `ok`. Connect is refused otherwise. */
  ready: boolean;
}

/** Manual overrides, and only for what detection actually failed to find. */
export interface DesktopOverrides {
  claudeBin?: string;
  gitBin?: string;
  chromeBin?: string;
  /**
   * Appium, for the one case detection genuinely cannot cover: a hub installed
   * somewhere none of the search prefixes reach. There is deliberately no
   * override for `adb`, `emulator` or `xcrun` — those live in fixed places the
   * SDK and Xcode put them, and an override for a tool that was found is a way
   * to break a working install.
   */
  appiumBin?: string;
}

export interface ChooseDirectoryRequest {
  title?: string;
  defaultPath?: string;
  buttonLabel?: string;
}

/**
 * The local half. Everything a browser tab cannot do, and nothing else: no
 * generic "run this", no path this page may name, no secret returned.
 */
export interface DesktopRunnerHost {
  snapshot(): Promise<DesktopRunnerSnapshot>;
  subscribe(cb: (snapshot: DesktopRunnerSnapshot) => void): () => void;

  connect(): Promise<DesktopRunnerSnapshot>;
  disconnect(): Promise<DesktopRunnerSnapshot>;
  restartChild(child: DesktopChildId): Promise<DesktopRunnerSnapshot>;

  logs(req?: { child?: DesktopChildId | "supervisor"; afterSeq?: number; limit?: number }): Promise<DesktopLogLine[]>;
  subscribeLogs(cb: (lines: DesktopLogLine[]) => void): () => void;
  clearLogs(): Promise<void>;

  settings(): Promise<DesktopSettings>;
  setPreferences(patch: DesktopPreferences): Promise<DesktopSettings>;
  chooseWorkspace(): Promise<DesktopWorkspaceChoice | null>;
  chooseDirectory(options?: ChooseDirectoryRequest): Promise<string | null>;
  reveal(what: "workspace" | "previous-workspace" | "logs"): Promise<void>;

  /**
   * Open an external link — a task's PR — in the real browser instead of the
   * hosted view navigating to it. Resolves false for a link the shell refuses
   * to open (not `https:`) rather than throwing, since that is an answer this
   * page can show a person.
   */
  openExternal(url: string): Promise<boolean>;

  diagnostics(force?: boolean): Promise<DesktopDiagnostics>;
  setOverrides(patch: DesktopOverrides): Promise<DesktopDiagnostics>;

  /**
   * The environment preflight checklist — the bundled server binary, the
   * Postgres cache, the Claude binary and its account, and the rest of
   * `DESKTOP_PREFLIGHT_IDS`. `force` re-runs every probe rather than
   * answering from a cache — the "Check again" button after the user fixes
   * something must not report the state from before the fix.
   */
  preflight(force?: boolean): Promise<DesktopPreflightReport>;
}

/** The hook the desktop preload installs on `window`. Absent in a browser. */
export interface TaskTrooperDesktopHost {
  info?: () => Promise<{ app: string; version: string; platform: string }>;
  /** The local half. Absent in a browser. */
  runner?: DesktopRunnerHost;
  /**
   * Where this app's API calls go: "http://127.0.0.1:<port>", no path suffix.
   *
   * Stated by the shell rather than compiled in, because the port is picked at
   * launch. A value rather than a method: `api.ts` reads it on every request,
   * and an async hop would put an await in front of every call in the app.
   */
  apiBase?: string;
  /** The bearer token for that server, generated on first run. Sync, same reason. */
  apiToken?: string;
}

declare global {
  interface Window {
    /** Presence of this marker *is* the "we are in the desktop shell" contract. */
    __tasktrooperDesktop?: TaskTrooperDesktopHost;
  }
}

/**
 * The shell's local capabilities, or null in a browser.
 *
 * Read at call time rather than captured once: the preload installs the marker
 * before any of this app's code runs, but treating it as a live lookup keeps
 * the browser case a plain `null` instead of a module-load-order question.
 */
export function desktopRunner(): DesktopRunnerHost | null {
  return window.__tasktrooperDesktop?.runner ?? null;
}
