import { EventEmitter } from "node:events";
import { app } from "electron";
import type {
  Blocker,
  ChildId,
  LogLine,
  Overrides,
  PreflightReport,
  SupervisorSnapshot,
  SupervisorState,
  UserSettings,
} from "../../ipc/types.js";
import { CHILD_IDS, GATING_CHILD_IDS } from "../../ipc/types.js";
import { keyStoreHelp } from "../config/keystore.js";
import type { LocalSecrets } from "../config/secrets.js";
import {
  APPIUM_BASE_URL,
  appiumHubIsAnswering,
  dataDir,
  embedderScriptPath,
  emptyReport,
  firstBlocker,
  itemById,
  postgresCacheDir,
  startPreflight,
} from "../services/detect.js";
import { waitForHealth } from "../services/health.js";
import { mobileAutomationInUse } from "../services/mobile-demand.js";
import { loginShellPathKnown } from "../services/login-env.js";
import { PreflightCache } from "../services/preflight-cache.js";
import { PREFLIGHT_REUSE_MS, PreflightSweeps, primeLoginShellPath, type Sweep } from "../services/preflight-sweep.js";
import { ensureWorkspace } from "../config/workspace.js";
import { join } from "node:path";
import { SupervisedChild, type ChildSpec } from "./child.js";
import { ChildRegistry, reapWithin } from "./reaper.js";
import { parseEmbedderListening } from "./embedder-log.js";
import { LogStore } from "./log-buffer.js";
import { agentServerEnv, appiumArgs, childEnv, spawnFingerprint } from "./env.js";
import { parseServerListening, renderServerLine, serverLineLevel } from "./server-log.js";

/**
 * The supervisor: one start, one process that matters, and a state machine the
 * UI subscribes to.
 *
 *   preflight  → every required environment check must pass, or the start is
 *                refused and the refusal NAMES the check that failed. Only the
 *                gating half of the sweep is waited for (see
 *                `startPreflight`); the rest finishes beside the start, and a
 *                backend whose environment it turns out to change is
 *                restarted once with the full answers.
 *   start      → the embedder (already running since app init), then the
 *                backend, and, when this Mac has Appium and the backend says
 *                something uses mobile automation, a hub after it.
 *   ready      → the backend prints `LISTENING http://127.0.0.1:<port>` and
 *                then answers `GET /health` with a 200. Only at that point does
 *                this app have an API base to hand the window.
 *
 * **Appium is a child and deliberately not a gate.** It starts after the
 * backend rather than before it, nothing waits for it, and its absence or its
 * crash never becomes the supervisor's state — `GATING_CHILD_IDS` is what says
 * so. A start that failed because an optional capability was slow is the shape
 * this app exists to avoid. And it starts only once something uses it (see
 * `#startAppiumIfUsed`): a hub is a ~100 MB Node process, and most installs
 * that have Appium never register a device.
 *
 * Everything a start script asked a human for, this decides: the binary paths
 * (detected), the port (the backend picks it), the database (embedded), the
 * hub's address (Appium's own default), the workspace (a folder the user picked
 * once). What it cannot decide — a missing `claude`, an account without a
 * Claude Code plan — becomes a `blocker` on the snapshot, with the sentence
 * that fixes it.
 */

export interface SupervisorEvents {
  state: [snapshot: SupervisorSnapshot];
  logs: [lines: LogLine[]];
  /**
   * The backend's base URL once it is answering, or null once it is not.
   *
   * Its own event because the URL is not stable: `PORT=0` means a restarted
   * backend comes back on a different port, and the window holds the old one
   * until something reloads it. `main/index.ts` is what notices.
   */
  server: [baseUrl: string | null];
}

/**
 * How long the backend gets to print its `LISTENING` line.
 *
 * Generous, and it has to be: the FIRST start downloads ~30 MB of Postgres
 * binaries from Maven Central, runs initdb, and applies every migration before
 * it binds anything. Later starts take a second or two. Bounded anyway, because
 * a start that never finishes is indistinguishable from a hang.
 */
const LISTENING_TIMEOUT_MS = 240_000;

/** Once it has bound a port, readiness is quick — or something is wrong with it. */
const HEALTH_TIMEOUT_MS = 120_000;

/** Log lines are flushed to the renderer in batches; a busy child emits hundreds a second. */
const LOG_FLUSH_MS = 120;

/**
 * How long `#connect()` waits for the embedder's `EMBEDDER_LISTENING` line.
 *
 * Near-instant in practice: the embedder binds its port BEFORE it downloads or
 * loads a model, and it is started at app init. This bounds only the wait for
 * the port bind, never for the model behind it — and running out of it is not
 * fatal, because embeddings are a capability and the backend starts happily
 * without one.
 */
const EMBEDDER_URL_WAIT_MS = 8_000;

const REAP_CAP_MS = 5_000;

/**
 * How often a backend whose install does not use mobile automation is asked
 * again. Backed by a re-check on every run the notification watcher sees
 * start (`recheckAppium`), which is when a first device actually gets dialled.
 */
const APPIUM_DEMAND_RECHECK_MS = 5 * 60_000;

const APPIUM_DEFERRED_DETAIL =
  "Appium is installed. TaskTrooper starts its hub once a repository or a registered device uses mobile automation.";

/**
 * Between the backend's exit and the message that quotes its last line. The
 * exit can be reported before the pipes have delivered the line that explains
 * it, which is usually the whole diagnosis.
 */
const EXIT_DRAIN_MS = 100;

export class Supervisor extends EventEmitter<SupervisorEvents> {
  readonly #children = new Map<ChildId, SupervisedChild>();
  // The backend's zerolog lines are stored raw and rendered when read: far
  // more of them are written than are ever looked at.
  readonly #logs = new LogStore(2000, { "agent-server": renderServerLine });

  #state: SupervisorState = "idle";
  #step: ChildId | undefined;
  #detail: string | undefined;
  #blocker: Blocker | undefined;
  #since = Date.now();

  /**
   * The embedder's resolved loopback URL, learned from its `EMBEDDER_LISTENING
   * <port>` stdout line. Updated on every such line, not just the first: the
   * embedder binds an OS-assigned port, so a restart gets a new one.
   */
  #embedderUrl: string | null = null;

  /**
   * The backend's base URL, learned from its `LISTENING` line and cleared when
   * it stops. Not the same thing as "the backend is ready" — `#serverReady`
   * is, and it only becomes true after `/health` answers.
   */
  #serverUrl: string | null = null;
  #serverReady = false;

  #settings: UserSettings | null = null;
  #secrets: LocalSecrets | null = null;
  /** Why `#secrets` is null, when the store said — a locked Linux keyring reads differently from a first run. */
  #secretsError: string | undefined;
  #overrides: Overrides = {};
  /** The newest COMPLETE report. A gating report is only ever used by the start that waited for it. */
  #preflight: PreflightReport = emptyReport();
  #preflightSweep = 0;

  readonly #preflightCache = new PreflightCache(join(app.getPath("userData"), "preflight-cache.json"));
  readonly #sweeps = new PreflightSweeps({
    start: ({ overrides, force }) =>
      startPreflight({ overrides, force, cache: this.#preflightCache, loginPathKnown: loginShellPathKnown() }),
    onSweep: (sweep) => this.#watchSweep(sweep),
  });

  /**
   * Which sweep the running backend's environment came from, and what that
   * environment was built from (`spawnFingerprint`). `final` when it was
   * that sweep's complete report, which leaves nothing to reconcile.
   */
  #spawnedFrom: { sweepId: number; fingerprint: string; final: boolean } | null = null;

  /** Waiters for the backend's next `LISTENING` line. */
  readonly #listeningWaiters = new Set<(url: string) => void>();
  /** Waiters for the embedder's `EMBEDDER_LISTENING` line. */
  readonly #embedderWaiters = new Set<(url: string) => void>();

  #startAbort: AbortController | null = null;
  #pendingLogs: LogLine[] = [];
  #logFlushTimer: NodeJS.Timeout | null = null;
  /** Serialises start/stop so two clicks cannot interleave two teardowns. */
  #transition: Promise<unknown> = Promise.resolve();

  readonly #registry = new ChildRegistry(join(app.getPath("userData"), "children.json"), (line) =>
    this.#note(line),
  );
  #reaped: Promise<void> = Promise.resolve();
  /** The sweep in flight, which a second caller joins rather than racing. */
  #reaping: Promise<void> | null = null;

  /** `#configureAppium` decided this app runs the hub, and nothing has used mobile automation yet. */
  #appiumDeferred = false;
  #appiumRecheck: NodeJS.Timeout | null = null;

  constructor() {
    super();
    for (const id of CHILD_IDS) {
      const child = new SupervisedChild(
        { id, command: "", args: [], env: {}, restart: true },
        {
          onSpawn: (entry) => this.#registry.record(entry),
          onExit: (cid, pid) => this.#registry.forget(cid, pid),
        },
      );
      child.on("log", (stream, text) => this.#onChildLog(id, stream, text));
      child.on("state", () => this.#emitState());
      child.on("crashed", () => this.#onChildCrashed(id));
      this.#children.set(id, child);
    }
  }

  // --- observation ---------------------------------------------------------

  snapshot(): SupervisorSnapshot {
    return {
      state: this.#state,
      ...(this.#step !== undefined ? { step: this.#step } : {}),
      ...(this.#detail !== undefined ? { detail: this.#detail } : {}),
      ...(this.#blocker !== undefined ? { blocker: this.#blocker } : {}),
      ...(this.apiBase !== null ? { apiBase: this.apiBase } : {}),
      since: this.#since,
      children: CHILD_IDS.map((id) => this.#child(id).status()),
    };
  }

  logs(child?: ChildId | "supervisor", afterSeq = 0, limit?: number): LogLine[] {
    return this.#logs.read(child, afterSeq, limit);
  }

  clearLogs(): void {
    this.#logs.clear();
  }

  get running(): boolean {
    return this.#state === "starting" || this.#state === "running" || this.#state === "degraded";
  }

  /** Where the UI's API calls go, or null until the backend has answered. */
  get apiBase(): string | null {
    return this.#serverReady ? this.#serverUrl : null;
  }

  /** The newest complete report, or an empty one before any sweep has finished. */
  get preflight(): PreflightReport {
    return this.#preflight;
  }

  // --- configuration -------------------------------------------------------

  configure(opts: {
    secrets: LocalSecrets | null;
    secretsError?: string;
    settings: UserSettings;
    overrides: Overrides;
  }): void {
    this.#secrets = opts.secrets;
    this.#secretsError = opts.secretsError;
    this.#settings = opts.settings;
    this.#overrides = opts.overrides;
  }

  /**
   * The environment checks, complete, without starting anything.
   *
   * Joins a sweep already running and reuses one that finished within
   * `maxAgeMs` (`PREFLIGHT_REUSE_MS` by default) — the launch's detection and
   * the launch's start are the same sweep. `force` always runs a new one.
   */
  detect(opts: { force?: boolean; maxAgeMs?: number } = {}): Promise<PreflightReport> {
    return this.#sweep(opts).complete;
  }

  /**
   * Put the login PATH an earlier launch recorded to use, and start this
   * launch's own login shell. Call once, as early as possible: the first
   * sweep's lookups then need not wait for a shell to read its profile.
   */
  warmUp(): void {
    void primeLoginShellPath(this.#preflightCache);
  }

  #sweep(opts: { force?: boolean; maxAgeMs?: number }): Sweep {
    return this.#sweeps.sweep({ overrides: this.#overrides, ...opts });
  }

  #watchSweep(sweep: Sweep): void {
    sweep.complete.then(
      (report) => {
        if (sweep.id >= this.#preflightSweep) {
          this.#preflight = report;
          this.#preflightSweep = sweep.id;
        }
        this.#emitState();
        this.#reconcile(sweep, report);
      },
      (err: unknown) => this.#note(`Checking this machine failed: ${describe(err)}`),
    );
  }

  /**
   * The backend was started on a gating report; its sweep's complete report
   * just arrived. When the two disagree on anything the backend's environment
   * is built from — a `claude` that turned out too old, a CLI only the fresh
   * login PATH could find — restart it once, on the complete report, so it is
   * not left running with an environment the setup screen contradicts.
   */
  #reconcile(sweep: Sweep, report: PreflightReport): void {
    const spawned = this.#spawnedFrom;
    if (!spawned || spawned.final || spawned.sweepId !== sweep.id) return;
    if (spawnFingerprint(report) === spawned.fingerprint) {
      spawned.final = true;
      return;
    }
    void this.#serialise(async () => {
      if (this.#spawnedFrom !== spawned || !this.running) return;
      this.#note("The full check of this machine changed what the local server needs to know; restarting it.");
      await this.#stop("stopped");
      await this.#connect(sweep);
    });
  }

  // --- lifecycle -----------------------------------------------------------

  /**
   * Start the embedder, unconditionally — not gated behind the backend the way
   * Appium is. Called once, from `main/index.ts`, as early in app init as
   * possible: the backend is handed this child's resolved loopback URL, and a
   * cold model download benefits from every second before that.
   *
   * Never throws. A Mac where this cannot start is not a reason to refuse the
   * backend, only a reason embeddings stay unavailable until it can.
   */
  async startEmbedder(): Promise<void> {
    await this.reapStale();
    const child = this.#child("embedder");
    child.update(this.#embedderSpec());
    try {
      await child.start();
      // Marked healthy on spawn, not on a probe: this child gates nothing, so
      // there is no readiness worth waiting for beyond the process existing.
      // Its model state is a fact for `POST /v1/embeddings` callers (a 503
      // until the download is done, then a load on the first request), not for
      // this state machine.
      child.markHealthy();
    } catch (err) {
      this.#note(`embedder did not start: ${describe(err)}. Embeddings are unavailable until it does.`);
    }
  }

  /**
   * Kill children an earlier run of this app left running. Called at boot,
   * before every embedder spawn, and on wake from sleep — an orphan the boot's
   * bounded sweep gave up on would otherwise run until the next launch.
   * `connect()` waits for the latest one. Joins a sweep already in flight;
   * bounded, never rejects, and never touches a child this run started.
   */
  reapStale(): Promise<void> {
    if (!this.#reaping) {
      this.#reaping = reapWithin(REAP_CAP_MS, {
        registry: this.#registry,
        userData: app.getPath("userData"),
        log: (line) => this.#note(line),
      }).finally(() => {
        this.#reaping = null;
      });
      this.#reaped = this.#reaping;
    }
    return this.#reaping;
  }

  connect(): Promise<SupervisorSnapshot> {
    return this.#serialise(() => this.#connect());
  }

  disconnect(): Promise<SupervisorSnapshot> {
    return this.#serialise(() => this.#stop("stopped"));
  }

  /**
   * The quit path: everything `disconnect()` stops, and then the embedder.
   *
   * The embedder is deliberately left running through a plain Disconnect (see
   * `#stop()`) so a restart a minute later does not pay for a model reload it
   * did not need to lose — but it must not survive the app quitting. Every
   * child is spawned `detached: true`, so nothing reaps it on its own.
   */
  drain(): Promise<SupervisorSnapshot> {
    return this.#serialise(async () => {
      const result = await this.#stop("stopped");
      await this.#child("embedder").stop();
      return result;
    });
  }

  restartChild(id: ChildId): Promise<SupervisorSnapshot> {
    return this.#serialise(async () => {
      const child = this.#child(id);
      child.resetBackoff();
      // The next one binds a different port (`PORT=0`), and a remembered URL
      // would have the readiness wait poll the old one.
      if (id === "agent-server") this.#forgetServer();
      await child.stop();
      // The embedder restarts regardless of `this.running`: unlike the backend
      // and Appium it is not scoped to a start session, so a manual restart
      // from Diagnostics must work whether or not the backend is up.
      if (id !== "embedder" && !this.running) return this.snapshot();
      // Somebody asked for the hub by name; that is demand enough.
      if (id === "appium" && this.#appiumDeferred) this.#undeferAppium();
      await this.#startChild(id);
      return this.snapshot();
    });
  }

  /**
   * Ask again whether anything uses mobile automation, and start the hub if
   * so. `main/index.ts` calls it when a run starts, which is when a first
   * device would actually be dialled. Cheap and idempotent: a no-op unless the
   * hub is waiting on demand.
   */
  recheckAppium(): void {
    void this.#startAppiumIfUsed();
  }

  #serialise<T>(fn: () => Promise<T>): Promise<T> {
    const next = this.#transition.then(fn, fn);
    // Swallow on the chain itself so one rejection does not poison every later
    // transition; the caller still sees its own rejection through `next`.
    this.#transition = next.catch(() => undefined);
    return next;
  }

  /** `reuse` is the sweep to start from: a reconcile restarts on the very report that asked for it. */
  async #connect(reuse?: Sweep): Promise<SupervisorSnapshot> {
    if (this.running) return this.snapshot();

    const settings = this.#settings;
    const secrets = this.#secrets;
    if (!settings) return this.#fail("no settings loaded");
    if (!secrets) {
      if (this.#secretsError) return this.#fail(this.#secretsError);
      const { failure, remedy } = keyStoreHelp();
      return this.#fail(`This machine has no local credentials yet: ${failure}. ${remedy}.`);
    }

    this.#blocker = undefined;
    this.#setState("preflight");

    this.#note("Checking what this machine can do…");
    // The sweep and the stale-child reaper run side by side, and only the
    // spawn waits for both: neither needs the other, and on Windows each can
    // take seconds (PowerShell process listings; a probe per CLI).
    const sweep = reuse ?? this.#sweep({ maxAgeMs: PREFLIGHT_REUSE_MS });
    const [gated] = await Promise.all([sweep.result ? Promise.resolve(sweep.result) : sweep.gating, this.#reaped]);

    // REFUSED, not attempted, while a required check fails, and the refusal
    // names the check. Starting anyway would move the failure into a Claude
    // Code session minutes later, which is what this preflight exists to end.
    const blocker = firstBlocker(gated);
    if (blocker) {
      this.#blocker = blocker;
      return this.#fail(`${blocker.title}. ${blocker.remediation}`);
    }

    this.#note("Preparing the workspace folder…");
    try {
      ensureWorkspace(settings.workspaceDir);
    } catch (err) {
      return this.#fail(`Could not create ${settings.workspaceDir}: ${describe(err)}`);
    }

    // Bounded, and NOT fatal. The embedder binds its port before it downloads
    // anything, so this is near-instant; a Mac where it has not managed even
    // that gets a backend with no embedding provider configured, which costs
    // RAG and nothing else.
    const embeddingsBaseURL = await this.#resolveEmbedderUrl();
    if (!embeddingsBaseURL) {
      this.#note("The embedding engine has not bound a port yet; starting without it. Search will be unavailable.");
    }

    // The freshest answer this sweep has by now: its complete half may have
    // landed while the workspace and the embedder were being waited for.
    const report = sweep.result ?? gated;
    const spec = this.#agentServerSpec(report, secrets, embeddingsBaseURL);
    if ("error" in spec) return this.#fail(spec.error);
    this.#spawnedFrom = { sweepId: sweep.id, fingerprint: spawnFingerprint(report), final: sweep.result !== null };

    const child = this.#child("agent-server");
    child.resetCounters();
    child.setEnabled(true);
    child.update(spec.value);

    // Decided before the backend starts, because the backend is told at spawn
    // whether it has a hub to proxy to, and reads that once.
    await this.#configureAppium(report);

    this.#serverUrl = null;
    this.#serverReady = false;

    const abort = new AbortController();
    this.#startAbort = abort;
    this.#setState("starting");
    this.#step = "agent-server";
    this.#note("Starting the local server…");

    try {
      await child.start();
    } catch (err) {
      await this.#stop("failed");
      return this.#fail(`The local server failed to start: ${describe(err)}`);
    }

    const ready = await this.#waitForServer(abort.signal, child);
    if (!ready.ok) {
      if (abort.signal.aborted) {
        await this.#stop("stopped");
        return this.snapshot();
      }
      await this.#stop("failed");
      return this.#fail(ready.message);
    }

    child.markHealthy();
    this.#step = undefined;
    this.#startAbort = null;
    this.#serverReady = true;
    this.#setState("running");
    this.#note("The local server is ready.");
    this.emit("server", this.#serverUrl);
    // Not awaited, and it cannot be: it asks the backend a question and then
    // takes this same transition chain, which this start is still holding.
    void this.#startAppiumIfUsed();
    return this.snapshot();
  }

  async #stop(final: SupervisorState): Promise<SupervisorSnapshot> {
    this.#startAbort?.abort();
    this.#startAbort = null;
    this.#appiumDeferred = false;
    this.#cancelAppiumRecheck();

    if (this.#state !== "idle" && this.#state !== "stopped") {
      this.#setState("stopping");
      this.#note("Stopping…");
    }

    // A drain, never a kill, and the ORDER matters: the backend goes first,
    // whatever its position in CHILD_IDS. It is the process holding the Claude
    // Code sessions that may still be calling Appium, so stopping the hub first
    // would pull it out from under a run that has not finished draining. The
    // embedder is excluded entirely — it survives a plain Disconnect so a
    // restart does not pay for a model reload; `drain()` stops it separately.
    const stopOrder: ChildId[] = [
      "agent-server",
      ...CHILD_IDS.filter((id) => id !== "agent-server" && id !== "embedder"),
    ];
    for (const id of stopOrder) {
      const child = this.#child(id);
      if (child.state === "idle" || child.state === "skipped") continue;
      this.#step = id;
      this.#emitState();
      await child.stop();
    }

    this.#step = undefined;
    this.#spawnedFrom = null;
    this.#forgetServer();
    if (final !== "failed") {
      this.#setState(final);
      this.#note("Stopped.");
    }
    return this.snapshot();
  }

  // --- wiring --------------------------------------------------------------

  #child(id: ChildId): SupervisedChild {
    const child = this.#children.get(id);
    if (!child) throw new Error(`unknown child ${id}`);
    return child;
  }

  #forgetServer(): void {
    this.#serverUrl = null;
    this.#serverReady = false;
    this.emit("server", null);
  }

  #agentServerSpec(
    report: PreflightReport,
    secrets: LocalSecrets,
    embeddingsBaseURL: string | null,
  ): { value: ChildSpec } | { error: string } {
    const server = itemById(report, "agent-server");
    if (!server?.path) return { error: "The server binary is missing from this copy of TaskTrooper." };
    return {
      value: {
        id: "agent-server",
        command: server.path,
        args: [],
        stdinPipe: true,
        env: agentServerEnv({
          preflight: report,
          dataDir: dataDir(),
          postgresCacheDir: postgresCacheDir(),
          apiToken: secrets.api_token,
          mcpSecretsKey: secrets.mcp_secrets_key,
          ...(embeddingsBaseURL !== null ? { embeddingsBaseURL } : {}),
        }),
        restart: true,
      },
    };
  }

  /**
   * Decide whether this app runs an Appium hub, and say so on Status either
   * way.
   *
   * Three outcomes, and the middle one is why this is a decision rather than an
   * unconditional spawn:
   *
   *   not installed          → disabled, with the sentence that installs it.
   *   already on the port    → disabled, and ADOPTED. Somebody is running their
   *                            own hub, with their own drivers and plugins.
   *                            Starting a second one would lose the port, crash,
   *                            and restart-loop against a working server.
   *   installed and nothing
   *   on the port            → this app runs it, once something uses it
   *                            (`#startAppiumIfUsed`). Until then it is
   *                            disabled with the sentence that says so.
   *
   * In all three the backend is told the same address, because it is the same
   * hub as far as the proxy is concerned.
   */
  async #configureAppium(report: PreflightReport): Promise<void> {
    const child = this.#child("appium");
    child.resetCounters();
    this.#appiumDeferred = false;

    const appium = itemById(report, "appium");
    if (appium?.status !== "ok" || !appium.path) {
      child.setEnabled(false, appium?.remediation ?? "Appium is not installed, so mobile automation is unavailable.");
      return;
    }
    if (await appiumHubIsAnswering()) {
      this.#adoptAppium();
      return;
    }
    this.#appiumDeferred = true;
    child.setEnabled(false, APPIUM_DEFERRED_DETAIL);
    child.update({
      id: "appium",
      command: appium.path,
      args: appiumArgs(),
      // Appium needs no secret and is given none: a clean PATH and the Android
      // SDK root, which is everything its drivers look for.
      env: childEnv(report),
      restart: true,
    });
  }

  #adoptAppium(): void {
    this.#child("appium").setEnabled(
      false,
      `An Appium server is already running on ${APPIUM_BASE_URL}; TaskTrooper is using it rather than starting a second one.`,
    );
  }

  #undeferAppium(): void {
    this.#appiumDeferred = false;
    this.#cancelAppiumRecheck();
    this.#child("appium").setEnabled(true);
  }

  #cancelAppiumRecheck(): void {
    if (this.#appiumRecheck) clearTimeout(this.#appiumRecheck);
    this.#appiumRecheck = null;
  }

  /**
   * The hub's lazy start. Asks the running backend whether a repository or a
   * registered device uses mobile automation (`mobileAutomationInUse`, which
   * answers yes when it cannot tell), and starts the hub the first time it
   * does. Otherwise asks again later, and whenever `recheckAppium()` says a
   * run started.
   *
   * There is no request to wait for instead: the backend dials the hub
   * directly. The port is checked again at the start, because somebody may
   * have started their own hub on it since `#configureAppium` looked.
   */
  async #startAppiumIfUsed(): Promise<void> {
    if (!this.#appiumDeferred) return;
    this.#cancelAppiumRecheck();
    const base = this.apiBase;
    const token = this.#secrets?.api_token;
    // No base is a backend mid-restart: not an answer, so ask again later.
    const used = base && token ? await mobileAutomationInUse(base, token) : false;

    if (!used) {
      if (this.#appiumDeferred && !this.#appiumRecheck) {
        this.#appiumRecheck = setTimeout(() => {
          this.#appiumRecheck = null;
          void this.#startAppiumIfUsed();
        }, APPIUM_DEMAND_RECHECK_MS);
        this.#appiumRecheck.unref?.();
      }
      return;
    }

    await this.#serialise(async () => {
      if (!this.#appiumDeferred || !this.running) return;
      if (await appiumHubIsAnswering()) {
        this.#appiumDeferred = false;
        this.#adoptAppium();
        return;
      }
      this.#undeferAppium();
      this.#note("Something here uses mobile automation; starting the Appium hub.");
      await this.#startAppium();
    });
  }

  /** Start the hub, if `#configureAppium` decided this app runs one. */
  async #startAppium(): Promise<void> {
    const child = this.#child("appium");
    if (!child.status().enabled) return;
    try {
      await child.start();
      // Healthy on spawn rather than on a probe: the hub gates nothing, so a
      // readiness wait here would only make every start slower.
      child.markHealthy();
    } catch (err) {
      // Never fatal. This is the whole difference between a capability and a
      // dependency, and the reason Appium is not in `GATING_CHILD_IDS`.
      this.#note(`Appium did not start: ${describe(err)}. Mobile automation is unavailable until it does.`);
    }
  }

  /**
   * The embedder's `ChildSpec`. `process.execPath` plus `ELECTRON_RUN_AS_NODE`
   * is the standard Electron pattern for running a plain Node script with the
   * Electron binary rather than as an Electron app — see `embedder/src/
   * index.ts`'s own note on why nothing in that process may import `electron`.
   */
  #embedderSpec(): ChildSpec {
    return {
      id: "embedder",
      command: process.execPath,
      args: [embedderScriptPath(), "--cache-dir", app.getPath("userData")],
      // Closing its stdin is how it is asked to stop, and the parent pid is how
      // it notices this process died without closing anything.
      stdinPipe: true,
      env: {
        ...childEnv(this.#preflight, { ownBinary: true }),
        ELECTRON_RUN_AS_NODE: "1",
        TASKTROOPER_EXIT_ON_STDIN_CLOSE: "1",
        TASKTROOPER_PARENT_PID: String(process.pid),
      },
      restart: true,
    };
  }

  /**
   * Resolved by the embedder's own line the moment it is read, or with null at
   * the bound. Now that the start no longer waits on a whole preflight, this
   * is often the last thing it waits on, and a 100 ms poll was most of it.
   */
  #resolveEmbedderUrl(): Promise<string | null> {
    if (this.#embedderUrl) return Promise.resolve(this.#embedderUrl);
    return new Promise((resolve) => {
      const finish = (url: string | null): void => {
        clearTimeout(timer);
        this.#embedderWaiters.delete(finish);
        resolve(url);
      };
      const timer = setTimeout(() => finish(this.#embedderUrl), EMBEDDER_URL_WAIT_MS);
      timer.unref?.();
      this.#embedderWaiters.add(finish);
    });
  }

  /**
   * Wait for the backend to be usable: its `LISTENING` line first, then a 200
   * from `/health`.
   *
   * Two steps because they fail differently and the user has to be told which.
   * No `LISTENING` line means the process never got as far as binding a port —
   * a Postgres download, a migration, a port collision — and its own last line
   * says which. A bound port that never answers `/health` is a backend that is
   * up and stuck, which is a different problem with a different first thing to
   * look at.
   */
  async #waitForServer(
    signal: AbortSignal,
    child: SupervisedChild,
  ): Promise<{ ok: true } | { ok: false; message: string }> {
    const listening = await this.#waitForListening(signal, child);
    if (!listening.ok) return listening;

    this.#note("Waiting for the local server to finish starting…");
    const health = await waitForHealth(`${listening.url}/health`, {
      timeoutMs: HEALTH_TIMEOUT_MS,
      alive: () => child.running,
      signal,
    });
    if (health.ok) return { ok: true };
    if (health.reason === "aborted") return { ok: false, message: "Cancelled." };
    return {
      ok: false,
      message:
        health.reason === "exited"
          ? "The local server exited before it was ready. Its own last line on the Status page says why."
          : `The local server bound ${listening.url} but never answered /health within ${HEALTH_TIMEOUT_MS / 1000}s.`,
    };
  }

  /**
   * Resolved by the `LISTENING` line itself (`#onChildLog`), by the child's
   * exit, by the deadline or by an abort — whichever is first. Nothing polls:
   * the line is acted on the moment it is read.
   */
  #waitForListening(
    signal: AbortSignal,
    child: SupervisedChild,
  ): Promise<{ ok: true; url: string } | { ok: false; message: string }> {
    if (this.#serverUrl) return Promise.resolve({ ok: true, url: this.#serverUrl });
    if (signal.aborted) return Promise.resolve({ ok: false, message: "Cancelled." });
    return new Promise((resolve) => {
      let drain: NodeJS.Timeout | undefined;
      const finish = (result: { ok: true; url: string } | { ok: false; message: string }): void => {
        clearTimeout(deadline);
        clearTimeout(drain);
        this.#listeningWaiters.delete(onListening);
        child.off("exited", onExit);
        signal.removeEventListener("abort", onAbort);
        resolve(result);
      };
      const onListening = (url: string): void => finish({ ok: true, url });
      const onAbort = (): void => finish({ ok: false, message: "Cancelled." });
      const exitedNow = (): void =>
        finish(
          this.#serverUrl
            ? { ok: true, url: this.#serverUrl }
            : { ok: false, message: describeServerExit(this.#lastAgentServerLine()) },
        );
      const onExit = (): void => {
        drain = setTimeout(exitedNow, EXIT_DRAIN_MS);
      };

      const deadline = setTimeout(
        () =>
          finish({
            ok: false,
            message:
              `The local server did not open a port within ${LISTENING_TIMEOUT_MS / 1000}s. ` +
              "A first start downloads Postgres, so a slow or blocked network is the usual cause.",
          }),
        LISTENING_TIMEOUT_MS,
      );
      deadline.unref?.();
      this.#listeningWaiters.add(onListening);
      signal.addEventListener("abort", onAbort, { once: true });
      if (child.running) child.once("exited", onExit);
      else onExit();
    });
  }

  #lastAgentServerLine(): string | undefined {
    return this.#logs.last("agent-server")?.text;
  }

  #onChildLog(id: ChildId, stream: "stdout" | "stderr", text: string): void {
    // In development, also to this process's own output. A child's last line
    // before it died is usually the entire diagnosis, and it would otherwise
    // exist only inside the app's own log view, which cannot be read from a
    // terminal or a test run.
    if (!app.isPackaged) console.warn(`[${id}] ${text.trimEnd()}`);

    if (id === "agent-server") {
      const listening = parseServerListening(text);
      if (listening !== undefined) {
        this.#serverUrl = listening;
        for (const waiter of [...this.#listeningWaiters]) waiter(listening);
      }
      this.#queueLog(this.#logs.append(id, stream, listening !== undefined ? text.trim() : text, serverLineLevel(text)));
      return;
    }

    if (id === "embedder") {
      const port = parseEmbedderListening(text);
      if (port !== undefined) {
        const url = `http://127.0.0.1:${port}`;
        this.#embedderUrl = url;
        for (const waiter of [...this.#embedderWaiters]) waiter(url);
      }
      this.#queueLog(this.#logs.append(id, stream, text));
      return;
    }

    this.#queueLog(this.#logs.append(id, stream, text));
  }

  #onChildCrashed(id: ChildId): void {
    const child = this.#child(id);

    // The embedder runs for the app's whole lifetime, independent of the
    // backend, so its branch is unconditional and comes before the `running`
    // check that correctly makes the others a no-op once a session has ended.
    if (id === "embedder") {
      const delay = child.scheduleRestart(() => {
        void this.#serialise(async () => this.#startChild(id));
      });
      this.#note(`embedder ${child.status().detail ?? "exited"}; restarting in ${Math.round(delay / 1000)}s`);
      this.#emitState();
      return;
    }

    if (!this.running) return;

    // Appium crashing costs mobile automation until it is back; it is not the
    // app going away, and reporting it as `degraded` would put a warning in
    // front of everyone who never touches a device.
    if (id !== "agent-server") {
      const delay = child.scheduleRestart(() => {
        void this.#serialise(async () => {
          if (!this.running) return;
          await this.#startChild(id);
        });
      });
      this.#note(`${id} ${child.status().detail ?? "exited"}; restarting in ${Math.round(delay / 1000)}s`);
      this.#emitState();
      return;
    }

    // The API base is gone with the process that was serving it, and the next
    // one will bind a DIFFERENT port — `PORT=0`. Saying so now is what keeps
    // the window from spending the backoff calling a dead address.
    this.#serverUrl = null;
    this.#serverReady = false;
    this.emit("server", null);

    this.#setState("degraded");
    const delay = child.scheduleRestart(() => {
      void this.#serialise(async () => {
        if (!this.running) return;
        await this.#startChild(id);
      });
    });
    this.#note(`${id} ${child.status().detail ?? "exited"}; restarting in ${Math.round(delay / 1000)}s`);
  }

  /**
   * Restart the child and wait for it again. The wait matters on a restart as
   * much as on a cold start: a backend that respawns and never answers must
   * show as degraded, not as running because a pid exists.
   */
  async #startChild(id: ChildId): Promise<void> {
    const child = this.#child(id);
    if (!child.status().enabled) return;

    if (id === "embedder") await this.reapStale();
    this.#note(`Restarting ${id}…`);
    try {
      await child.start();
    } catch (err) {
      this.#note(`${id} failed to restart: ${describe(err)}`);
      return;
    }

    // Only the backend has a readiness to wait for. Appium and the embedder are
    // up when their process is up; a probe here would be a gate on a capability
    // that gates nothing.
    if (id !== "agent-server") {
      child.markHealthy();
      this.#note(`${id} is back up.`);
      return;
    }

    const abort = new AbortController();
    const ready = await this.#waitForServer(abort.signal, child);
    if (!ready.ok) {
      this.#note(`${id} did not become ready after restarting: ${ready.message}`);
      return;
    }
    child.markHealthy();
    this.#serverReady = true;
    this.#note(`${id} is back up.`);
    this.emit("server", this.#serverUrl);
    this.#recomputeState();
  }

  // --- state ---------------------------------------------------------------

  /**
   * `running` vs `degraded`. Only the GATING children count, which today is the
   * backend. Appium is a capability: a Mac whose hub is mid-restart still runs
   * every task that does not touch a device, and colouring the tray icon for it
   * would train people to ignore the one state that means their work is not
   * running.
   */
  #recomputeState(): void {
    if (!this.running) {
      this.#emitState();
      return;
    }
    const childrenOk = GATING_CHILD_IDS.map((id) => this.#child(id).status()).every(
      (s) => !s.enabled || s.state === "healthy",
    );
    this.#setState(childrenOk && this.#serverReady ? "running" : "degraded");
  }

  #setState(state: SupervisorState): void {
    if (this.#state !== state) {
      this.#state = state;
      this.#since = Date.now();
    }
    this.#emitState();
  }

  #fail(message: string): SupervisorSnapshot {
    this.#state = "failed";
    this.#since = Date.now();
    this.#detail = message;
    this.#step = undefined;
    this.#queueLog(this.#logs.append("supervisor", "stderr", message));
    this.#emitState();
    return this.snapshot();
  }

  /**
   * The narration line. It is `detail` on the snapshot rather than only a log
   * entry because a first start takes tens of seconds and silence reads as a
   * hang — the user needs a sentence, not a spinner.
   */
  #note(text: string): void {
    this.#detail = text;
    this.#queueLog(this.#logs.append("supervisor", "stdout", text));
  }

  #emitState(): void {
    this.emit("state", this.snapshot());
  }

  /**
   * Log lines are coalesced before they cross IPC. A Claude Code session
   * streaming its output through the backend emits hundreds of lines a second,
   * and one IPC message per line is a renderer that spends its frame budget on
   * postMessage.
   *
   * And they are not pushed at all while nobody listens: the ring keeps them,
   * and a view that opens later catches up with `logs(child, afterSeq)`.
   */
  #queueLog(line: LogLine): void {
    if (this.listenerCount("logs") === 0) return;
    this.#pendingLogs.push(line);
    if (this.#logFlushTimer) return;
    this.#logFlushTimer = setTimeout(() => {
      this.#logFlushTimer = null;
      const batch = this.#pendingLogs.map((pending) => this.#logs.render(pending));
      this.#pendingLogs = [];
      if (batch.length > 0 && this.listenerCount("logs") > 0) this.emit("logs", batch);
    }, LOG_FLUSH_MS);
    this.#logFlushTimer.unref?.();
  }
}

function describe(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

/**
 * The "could not start" message when the backend exited before it opened a
 * port. Includes the child's own last log line when there is one, since that
 * line is usually the whole diagnosis and is otherwise visible only in the
 * app's own log view.
 */
export function describeServerExit(lastLine?: string): string {
  return lastLine
    ? `The local server exited before it opened a port. Its own last line: "${lastLine}"`
    : "The local server exited before it opened a port. Its own last line says why.";
}
