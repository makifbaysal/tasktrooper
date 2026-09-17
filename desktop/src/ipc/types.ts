/**
 * The vocabulary the three Electron processes share: supervisor states, the
 * child process, the environment preflight, workspace validation.
 *
 * Electron-bound by subject matter even though it imports nothing, which is why
 * it lives here and not in `src/shared/`: nothing in `src/shared/` may know
 * that a supervisor or a preload bridge exists.
 */

// --- children ---------------------------------------------------------------

/**
 * The processes the supervisor runs, in START order.
 *
 * The embedder goes first because the backend is handed its resolved loopback
 * URL as `EMBEDDINGS_BASE_URL`, and that value has to exist before the backend
 * is spawned. The backend is the product: no window is shown until it answers
 * `/health`. Appium is a capability — started beside the backend, waited for by
 * nothing.
 *
 * This is not the stop order. `Supervisor#stop()` takes the backend down FIRST,
 * because it is the process holding the Claude Code sessions that may still be
 * calling Appium.
 */
export const CHILD_IDS = ["embedder", "agent-server", "appium"] as const;
export type ChildId = (typeof CHILD_IDS)[number];

/**
 * The children whose health IS this app's health.
 *
 * One, and that is the point of the list existing: Appium and the embedder are
 * capabilities, not dependencies. A Mac with no Appium runs every task that
 * does not touch a device, and a Mac whose embedder is still downloading its
 * model runs every task that does not need embeddings yet; reporting the whole
 * supervisor as `degraded` for either would put a warning in front of somebody
 * it does not apply to.
 */
export const GATING_CHILD_IDS: readonly ChildId[] = ["agent-server"];

/**
 * One child's lifecycle.
 *
 * `starting` and `waiting-health` are separate because they fail differently
 * and the user has to be told which: a process that never spawned is a missing
 * binary, and a process that spawned but never became ready is a database or a
 * migration.
 *
 * `crashed` is a terminal observation; `restarting` is the supervisor's
 * response to it while the backoff timer runs. Keeping them apart is what lets
 * the status page say "exited 5s ago, retrying in 8s" instead of flickering.
 */
export type ChildState =
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

export interface ChildStatus {
  id: ChildId;
  state: ChildState;
  /** Absent unless the process is currently alive. */
  pid?: number;
  /** Why it is in this state, in words meant for the user. */
  detail?: string;
  lastExitCode?: number | null;
  lastExitSignal?: string | null;
  startedAt?: number;
  exitedAt?: number;
  /** How many times the supervisor has restarted it since the last start. */
  restarts: number;
  /** Epoch ms the next restart attempt is scheduled for, while `restarting`. */
  nextRestartAt?: number;
  /**
   * False for a child this machine cannot run, or does not need to.
   *
   * Appium is the one that sets it: absent from this Mac, or already running
   * because the user started their own. Either way it is stated on Status
   * rather than treated as a failure — `detail` carries the sentence.
   */
  enabled: boolean;
}

// --- supervisor -------------------------------------------------------------

/**
 * `degraded` is the state a start script cannot represent: the process is
 * alive, but it is mid-restart or not answering, which is not "running" and is
 * emphatically not "stopped".
 */
export type SupervisorState =
  | "idle"
  | "preflight"
  | "starting"
  | "running"
  | "degraded"
  | "stopping"
  | "stopped"
  | "failed";

export interface SupervisorSnapshot {
  state: SupervisorState;
  /** Which child the current `starting`/`stopping` step is waiting on. */
  step?: ChildId;
  /** One line for a person: "starting the backend…". Narration, not a log. */
  detail?: string;
  since: number;
  children: ChildStatus[];
  /** `http://127.0.0.1:<port>` once the backend has answered. */
  apiBase?: string;
  /**
   * A blocker the user must clear. Only ever set for a REQUIRED preflight item;
   * everything optional degrades silently.
   */
  blocker?: Blocker;
}

export interface Blocker {
  /** The preflight item that failed, so the UI can scroll to it. */
  id: PreflightId;
  title: string;
  because: string;
  /** What to do about it, in words. Always present. */
  remediation: string;
  /** A command to copy, when the remediation is one. */
  command: string;
  runnable: boolean;
}

export interface LogLine {
  /** Monotonic within a session; the renderer uses it to de-duplicate. */
  seq: number;
  child: ChildId | "supervisor";
  at: number;
  stream: "stdout" | "stderr";
  text: string;
  /** Present when the line arrived as JSON and parsed (the backend only). */
  level?: string;
}

// --- what the user actually sets --------------------------------------------

/**
 * The entire user-facing configuration. Three fields.
 *
 * Everything else this app needs — the `claude` binary, `git`, a port, a
 * database — is detected, allocated or started. A settings form for any of it
 * would be a form asking the user to tell the computer something the computer
 * can find out, and getting any of it wrong produces a failure several layers
 * from its cause.
 */
export interface UserSettings {
  /** Where Claude Code sessions clone and work. Absolute, and visible. */
  workspaceDir: string;
  launchAtLogin: boolean;
  /** Start the local backend as soon as the app launches. */
  autoConnect: boolean;
  /** Desktop notifications for board events the stakeholder must act on. */
  notifications: NotificationPreferences;
}

/**
 * One toggle per trigger group. `humanNeeded` covers both a blocked question
 * and a blocked human-decision park — both mean the same thing to the
 * stakeholder ("the board is stuck on you"), so they share one switch.
 */
export interface NotificationPreferences {
  enabled: boolean;
  analizReview: boolean;
  humanUat: boolean;
  humanNeeded: boolean;
  agentComments: boolean;
}

/**
 * Manual overrides, which exist only in the diagnostics view and only for what
 * detection actually failed to find. An override for something that was
 * detected is a way to break a working install.
 */
export interface Overrides {
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

// --- the preflight ----------------------------------------------------------

/**
 * The environment checklist, by id, in the order a person should fix things in.
 *
 * `claude-account` is separate from `claude` on purpose and it is the item this
 * whole list exists for. A free Claude account can install the CLI, pass every
 * "is it there" check, and then fail on the first request of a real run —
 * minutes later, inside a log the user is not reading, with a message about a
 * model rather than about a plan. Splitting "the binary is here" from "this
 * account may use it" is what lets the second one be said before the run.
 */
export const PREFLIGHT_IDS = [
  "agent-server",
  // Never blocking: an absent Postgres is one the backend downloads on its
  // first start. It is listed so that download is a thing the user was told
  // about rather than a minute of silence.
  "postgres",
  "git",
  "claude",
  "claude-account",
  "chrome",
  "xcode-clt",
  // The mobile half, all optional. It is here for the reason the whole list is
  // here: a capability that fails at the moment somebody needs it, with no
  // earlier signal, is the failure this app exists to prevent. A QA task that
  // runs on a Mac with no Appium fails minutes in, inside a session, with a
  // connection error about a port — and the answer ("npm install -g appium")
  // could have been said before anyone pressed anything.
  "appium",
  "appium-xcuitest",
  "appium-uiautomator2",
  "android-sdk",
  // The other local agent CLIs a task can run on, all optional — which one a
  // task uses is chosen per AGENT, not per Mac. See detect.ts's probeSimpleCLI
  // for why these are binary-presence only, with no claude-account-style auth
  // split.
  "agy",
  "cursor-agent",
  "opencode",
] as const;
export type PreflightId = (typeof PREFLIGHT_IDS)[number];

/**
 * Three outcomes, not two.
 *
 * `missing` and `unusable` are different questions with different answers: a
 * `claude` that is not installed is `npm install -g`, and a `claude` that is
 * installed but signed into an account without Claude Code is a plan change.
 * Collapsing them into "not ok" is how a user ends up reinstalling a binary
 * that was never the problem.
 */
export type PreflightStatus = "ok" | "missing" | "unusable";

/** How something was found, so a surprising result can be explained. */
export type PreflightSource =
  | "path"
  | "bundled"
  | "dev-bin"
  | "homebrew"
  | "npm-prefix"
  | "home"
  | "app-bundle"
  | "override"
  | "network";

export interface PreflightItem {
  id: PreflightId;
  /** For a person, not a log: "Claude account". */
  label: string;
  /** Required items block the start. Optional ones never do. */
  required: boolean;
  status: PreflightStatus;
  /** Absolute path, or the address that answered. Present when found. */
  path?: string;
  version?: string;
  source?: PreflightSource;
  /** What was observed. Present whenever it explains the status. */
  detail?: string;
  /**
   * Exactly what to do about it, in a sentence. Present for every item that is
   * not `ok`, and absent for every item that is — a remediation on a passing
   * check is advice nobody asked for.
   */
  remediation?: string;
  /** The command form of the remediation, when there is one to copy. */
  command?: string;
}

export interface PreflightReport {
  /** Epoch ms. The report is a snapshot; a stale one must be visibly stale. */
  generatedAt: number;
  items: PreflightItem[];
  /** True when every REQUIRED item is `ok`. Starting is refused when it is not. */
  ready: boolean;
}

export interface Diagnostics {
  preflight: PreflightReport;
  workspaceDir: string;
  /** Absolute paths the supervisor resolved, for the read-only view. */
  binDir: string;
  /** Where the backend keeps the database, the RAG files and its workspaces. */
  dataDir: string;
  dataFree?: number;
  app: AppInfo;
}

// --- workspace --------------------------------------------------------------

/**
 * The result of checking a candidate workspace directory. Warnings do not
 * block — a folder in Dropbox works, right up until a git checkout inside it
 * corrupts — so they are said plainly and the choice is left with the user.
 */
export interface WorkspaceCheck {
  path: string;
  ok: boolean;
  /** Set when the path cannot be used at all. */
  error?: string;
  warnings: string[];
  freeBytes?: number;
  exists: boolean;
}

// --- shell ------------------------------------------------------------------

export interface AppInfo {
  version: string;
  electron: string;
  chrome: string;
  node: string;
  platform: string;
  arch: string;
  packaged: boolean;
  /** Where the UI's API calls go. Empty until the backend has answered. */
  origin: string;
}

// --- updates ----------------------------------------------------------------

/**
 * Where this copy of the app is in the update cycle.
 *
 * `unsupported` is not an error and must not be drawn as one. It is the honest
 * state of a development run and of a `npm run package` build, neither of which
 * carries an update feed; a build that cannot update itself should say nothing
 * at all rather than showing a permanently sad status.
 *
 * `ready` is the only phase that asks the user for anything, and what it asks
 * for is a restart they choose. Applying an update relaunches the app — see
 * `main/services/updater.ts` — and a Claude Code session may be mid-run, so
 * nothing here may happen on its own.
 */
export type UpdatePhase =
  | "unsupported"
  | "idle"
  | "checking"
  | "current"
  | "available"
  | "ready"
  | "error";

export interface UpdateStatus {
  phase: UpdatePhase;
  /** The version on the feed, once one is known. Never the running version. */
  version?: string;
  /** 0–100 while downloading. */
  percent?: number;
  /** One sentence for a person: what is happening, or what went wrong. */
  detail?: string;
  /** Epoch ms of the last completed check, successful or not. */
  checkedAt?: number;
  /** The feed this build reads. Not a secret; the first thing to check. */
  feed?: string;
}

/**
 * Whether the bundled web app — which is the entire visible product — actually
 * loaded.
 *
 * The one screen the shell still owns is the honest failure: a backend that
 * could not start, a build with no web assets in it. An Electron window showing
 * a blank white rectangle in that case is the worst possible answer, so the
 * main process reports the load's outcome and the chrome renders it.
 */
export interface CloudStatus {
  state: "loading" | "ready" | "failed";
  /** The URL that was being loaded. Shown, because "which server?" is the first question. */
  url: string;
  /** Chromium's error code, when it failed. Negative, e.g. -105 for DNS. */
  code?: number;
  /** Chromium's own description, or one sentence from the supervisor. */
  description?: string;
}
