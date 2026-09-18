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
  preflight,
} from "../services/detect.js";
import { waitForHealth } from "../services/health.js";
import { ensureWorkspace } from "../config/workspace.js";
import { SupervisedChild, type ChildSpec } from "./child.js";
import { parseEmbedderListening } from "./embedder-log.js";
import { LogStore } from "./log-buffer.js";
import { agentServerEnv, appiumArgs, childEnv } from "./env.js";
import { parseServerLine } from "./server-log.js";

/**
 * The supervisor: one start, one process that matters, and a state machine the
 * UI subscribes to.
 *
 *   preflight  → every required environment check must pass, or the start is
 *                refused and the refusal NAMES the check that failed.
 *   start      → the embedder (already running since app init), then the
 *                backend, and, when this Mac has Appium, a hub beside it.
 *   ready      → the backend prints `LISTENING http://127.0.0.1:<port>` and
 *                then answers `GET /health` with a 200. Only at that point does
 *                this app have an API base to hand the window.
 *
 * **Appium is a child and deliberately not a gate.** It starts beside the
 * backend rather than before it, nothing waits for it, and its absence or its
 * crash never becomes the supervisor's state — `GATING_CHILD_IDS` is what says
 * so. A start that failed because an optional capability was slow is the shape
 * this app exists to avoid.
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

export class Supervisor extends EventEmitter<SupervisorEvents> {
  readonly #children = new Map<ChildId, SupervisedChild>();
  readonly #logs = new LogStore();

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
  #overrides: Overrides = {};
  #preflight: PreflightReport = emptyReport();

  #startAbort: AbortController | null = null;
  #pendingLogs: LogLine[] = [];
  #logFlushTimer: NodeJS.Timeout | null = null;
  /** Serialises start/stop so two clicks cannot interleave two teardowns. */
  #transition: Promise<unknown> = Promise.resolve();

  constructor() {
    super();
    for (const id of CHILD_IDS) {
      const child = new SupervisedChild({ id, command: "", args: [], env: {}, restart: true });
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

  get preflight(): PreflightReport {
    return this.#preflight;
  }

  // --- configuration -------------------------------------------------------

  configure(opts: { secrets: LocalSecrets | null; settings: UserSettings; overrides: Overrides }): void {
    this.#secrets = opts.secrets;
    this.#settings = opts.settings;
    this.#overrides = opts.overrides;
  }

  /** Re-run the environment checks without starting anything. */
  async detect(): Promise<PreflightReport> {
    this.#preflight = await preflight({ overrides: this.#overrides });
    this.#emitState();
    return this.#preflight;
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
    const child = this.#child("embedder");
    child.update(this.#embedderSpec());
    try {
      await child.start();
      // Marked healthy on spawn, not on a probe: this child gates nothing, so
      // there is no readiness worth waiting for beyond the process existing.
      // Its model-loaded state is a fact for `POST /v1/embeddings` callers (a
      // 503 until then), not for this state machine.
      child.markHealthy();
    } catch (err) {
      this.#note(`embedder did not start: ${describe(err)}. Embeddings are unavailable until it does.`);
    }
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
      await child.stop();
      // The embedder restarts regardless of `this.running`: unlike the backend
      // and Appium it is not scoped to a start session, so a manual restart
      // from Diagnostics must work whether or not the backend is up.
      if (id !== "embedder" && !this.running) return this.snapshot();
      await this.#startChild(id);
      return this.snapshot();
    });
  }

  #serialise<T>(fn: () => Promise<T>): Promise<T> {
    const next = this.#transition.then(fn, fn);
    // Swallow on the chain itself so one rejection does not poison every later
    // transition; the caller still sees its own rejection through `next`.
    this.#transition = next.catch(() => undefined);
    return next;
  }

  async #connect(): Promise<SupervisorSnapshot> {
    if (this.running) return this.snapshot();

    const settings = this.#settings;
    const secrets = this.#secrets;
    if (!settings) return this.#fail("no settings loaded");
    if (!secrets) {
      return this.#fail(
        "This Mac has no local credentials yet, and macOS would not provide an encryption key to make them. " +
          "Unlock the login keychain and try again.",
      );
    }

    this.#blocker = undefined;
    this.#setState("preflight");

    this.#note("Checking what this Mac can do…");
    this.#preflight = await preflight({ overrides: this.#overrides });

    // REFUSED, not attempted, while a required check fails, and the refusal
    // names the check. Starting anyway would move the failure into a Claude
    // Code session minutes later, which is what this preflight exists to end.
    const blocker = firstBlocker(this.#preflight);
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

    const spec = this.#agentServerSpec(secrets, embeddingsBaseURL);
    if ("error" in spec) return this.#fail(spec.error);

    const child = this.#child("agent-server");
    child.resetCounters();
    child.setEnabled(true);
    child.update(spec.value);

    // Decided before the backend starts, because the backend is told at spawn
    // whether it has a hub to proxy to, and reads that once.
    await this.#configureAppium();

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

    // Beside the backend and not before it. Nothing below waits for the hub: a
    // mobile task that arrives in the two seconds Appium takes to bind sees a
    // 502 naming the address, which is a better answer than a start that took
    // two seconds longer for everyone who never touches a device.
    await this.#startAppium();

    const ready = await this.#waitForServer(abort.signal, () => child.running);
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
    return this.snapshot();
  }

  async #stop(final: SupervisorState): Promise<SupervisorSnapshot> {
    this.#startAbort?.abort();
    this.#startAbort = null;

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
    this.#serverUrl = null;
    this.#serverReady = false;
    this.emit("server", null);
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

  #agentServerSpec(
    secrets: LocalSecrets,
    embeddingsBaseURL: string | null,
  ): { value: ChildSpec } | { error: string } {
    const server = itemById(this.#preflight, "agent-server");
    if (!server?.path) return { error: "The server binary is missing from this copy of TaskTrooper." };
    return {
      value: {
        id: "agent-server",
        command: server.path,
        args: [],
        stdinPipe: true,
        env: agentServerEnv({
          preflight: this.#preflight,
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
   *   on the port            → this app runs it.
   *
   * In all three the backend is told the same address, because it is the same
   * hub as far as the proxy is concerned.
   */
  async #configureAppium(): Promise<void> {
    const child = this.#child("appium");
    child.resetCounters();

    const appium = itemById(this.#preflight, "appium");
    if (appium?.status !== "ok" || !appium.path) {
      child.setEnabled(false, appium?.remediation ?? "Appium is not installed, so mobile automation is unavailable.");
      return;
    }
    if (await appiumHubIsAnswering()) {
      child.setEnabled(
        false,
        `An Appium server is already running on ${APPIUM_BASE_URL}; TaskTrooper is using it rather than starting a second one.`,
      );
      return;
    }
    child.setEnabled(true);
    child.update({
      id: "appium",
      command: appium.path,
      args: appiumArgs(),
      // Appium needs no secret and is given none: a clean PATH and the Android
      // SDK root, which is everything its drivers look for.
      env: childEnv(this.#preflight),
      restart: true,
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
      env: { ...childEnv(this.#preflight), ELECTRON_RUN_AS_NODE: "1" },
      restart: true,
    };
  }

  async #resolveEmbedderUrl(): Promise<string | null> {
    if (this.#embedderUrl) return this.#embedderUrl;
    const deadline = Date.now() + EMBEDDER_URL_WAIT_MS;
    while (!this.#embedderUrl && Date.now() < deadline) {
      await new Promise((resolve) => setTimeout(resolve, 100));
    }
    return this.#embedderUrl;
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
    alive: () => boolean,
  ): Promise<{ ok: true } | { ok: false; message: string }> {
    const listening = await this.#waitForListening(signal, alive);
    if (!listening.ok) return listening;

    this.#note("Waiting for the local server to finish starting…");
    const health = await waitForHealth(`${listening.url}/health`, {
      timeoutMs: HEALTH_TIMEOUT_MS,
      alive,
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

  #waitForListening(
    signal: AbortSignal,
    alive: () => boolean,
  ): Promise<{ ok: true; url: string } | { ok: false; message: string }> {
    if (this.#serverUrl) return Promise.resolve({ ok: true, url: this.#serverUrl });
    const deadline = Date.now() + LISTENING_TIMEOUT_MS;
    return new Promise((resolve) => {
      const finish = (result: { ok: true; url: string } | { ok: false; message: string }): void => {
        clearInterval(timer);
        signal.removeEventListener("abort", onAbort);
        resolve(result);
      };
      const onAbort = (): void => finish({ ok: false, message: "Cancelled." });

      const timer = setInterval(() => {
        if (this.#serverUrl) return finish({ ok: true, url: this.#serverUrl });
        if (!alive()) {
          return finish({ ok: false, message: describeServerExit(this.#lastAgentServerLine()) });
        }
        if (Date.now() >= deadline) {
          return finish({
            ok: false,
            message:
              `The local server did not open a port within ${LISTENING_TIMEOUT_MS / 1000}s. ` +
              "A first start downloads Postgres, so a slow or blocked network is the usual cause.",
          });
        }
      }, 250);
      timer.unref?.();
      signal.addEventListener("abort", onAbort, { once: true });
    });
  }

  #lastAgentServerLine(): string | undefined {
    const lines = this.#logs.read("agent-server");
    return lines.length > 0 ? lines[lines.length - 1]?.text : undefined;
  }

  #onChildLog(id: ChildId, stream: "stdout" | "stderr", text: string): void {
    // In development, also to this process's own output. A child's last line
    // before it died is usually the entire diagnosis, and it would otherwise
    // exist only inside the app's own log view, which cannot be read from a
    // terminal or a test run.
    if (!app.isPackaged) console.warn(`[${id}] ${text.trimEnd()}`);

    if (id === "agent-server") {
      const parsed = parseServerLine(text);
      if (parsed.listening !== undefined) this.#serverUrl = parsed.listening;
      this.#queueLog(this.#logs.append(id, stream, parsed.text, parsed.level));
      return;
    }

    if (id === "embedder") {
      const port = parseEmbedderListening(text);
      if (port !== undefined) this.#embedderUrl = `http://127.0.0.1:${port}`;
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
    const ready = await this.#waitForServer(abort.signal, () => child.running);
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
   */
  #queueLog(line: LogLine): void {
    this.#pendingLogs.push(line);
    if (this.#logFlushTimer) return;
    this.#logFlushTimer = setTimeout(() => {
      this.#logFlushTimer = null;
      const batch = this.#pendingLogs;
      this.#pendingLogs = [];
      if (batch.length > 0) this.emit("logs", batch);
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
