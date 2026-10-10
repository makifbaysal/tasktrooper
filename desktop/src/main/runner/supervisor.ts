import { EventEmitter } from "node:events";
import type {
  Blocker,
  LogLine,
  Overrides,
  PreflightReport,
  RunnerPairingBundle,
  RunnerPairingSummary,
  SupervisorSnapshot,
  SupervisorState,
  TunnelStatus,
  UserSettings,
} from "../../ipc/types.js";
import { ensureWorkspace } from "../config/workspace.js";
import { asPairingBundle, PairingStore, pairingSummary } from "../config/pairing.js";
import type { McpServerConfig } from "../config/mcp-servers.js";
import { usableProviderIds, type ProviderStore } from "../config/providers.js";
import { accountPreflight, emptyReport, executorDataDir, firstBlocker, itemById, postgresCacheDir, runnerDataDir } from "../services/detect.js";
import { LogStore } from "../supervisor/log-buffer.js";
import { RunnerChild } from "./child.js";
import { childEnv, embeddingsMessage, preflightMessage, runnerConfig } from "./env.js";
import { isAuthRejection, parseRunnerLine } from "./runner-log.js";

/**
 * Account mode's supervisor: one Connect, one child — the runner — and the
 * same state-machine shape the local `Supervisor` uses, so `main/index.ts` and
 * the bridge can treat the two modes almost identically.
 *
 * What is deliberately NOT here, unlike the local supervisor: no agent-server,
 * no embedded Postgres, no Appium hub, and the embedder is the local
 * supervisor's (it runs in both modes) — only its address comes here, through
 * `RunnerSupervisorOptions.embeddings`. The runner starts its own
 * hub when an Appium call needs one (`appium_bin`) and its own executor
 * (`executor_bin`). There is no local backend in account mode — the web app
 * talks to its own origin — so this supervises exactly the process that makes
 * THIS COMPUTER useful to the account: the tunnel.
 */

export interface RunnerSupervisorEvents {
  state: [snapshot: SupervisorSnapshot];
  logs: [lines: LogLine[]];
}

export interface RunnerSupervisorOptions {
  /**
   * The local embedder's address, or null when it has none (yet). Asked at
   * every spawn; a later move reaches a running runner through
   * `setEmbeddingsBaseURL`.
   */
  embeddings?: () => Promise<string | null>;
  /** The member's own MCP servers, read at every spawn like the providers. */
  mcpServers?: { read(): McpServerConfig[] };
}

/** How long the runner gets to attach before Connect gives up. */
const ATTACH_TIMEOUT_MS = 45_000;

/** Log lines are flushed to the renderer in batches. */
const LOG_FLUSH_MS = 120;

export class RunnerSupervisor extends EventEmitter<RunnerSupervisorEvents> {
  readonly #pairing: PairingStore;
  readonly #providers: ProviderStore;
  readonly #embeddings: () => Promise<string | null>;
  readonly #mcpServers: { read(): McpServerConfig[] };
  readonly #child = new RunnerChild();
  readonly #logs = new LogStore();

  #state: SupervisorState = "idle";
  #detail: string | undefined;
  #blocker: Blocker | undefined;
  #since = Date.now();
  #tunnel: TunnelStatus = { state: "unknown", changedAt: Date.now() };

  #settings: UserSettings | null = null;
  #overrides: Overrides = {};
  #preflight: PreflightReport = emptyReport();

  #startAbort: AbortController | null = null;
  #pendingLogs: LogLine[] = [];
  #logFlushTimer: NodeJS.Timeout | null = null;
  /** Serialises every transition so two clicks cannot interleave two teardowns. */
  #transition: Promise<unknown> = Promise.resolve();

  constructor(pairing: PairingStore, providers: ProviderStore, options: RunnerSupervisorOptions = {}) {
    super();
    this.#pairing = pairing;
    this.#providers = providers;
    this.#embeddings = options.embeddings ?? (() => Promise.resolve(null));
    this.#mcpServers = options.mcpServers ?? { read: () => [] };
    this.#child.on("log", (stream, text) => this.#onChildLog(stream, text));
    this.#child.on("state", () => this.#emitState());
    this.#child.on("crashed", () => this.#onChildCrashed());
  }

  // --- observation -----------------------------------------------------------

  snapshot(): SupervisorSnapshot {
    return {
      state: this.#state,
      ...(this.#detail !== undefined ? { detail: this.#detail } : {}),
      ...(this.#blocker !== undefined ? { blocker: this.#blocker } : {}),
      since: this.#since,
      children: [this.#child.status()],
      tunnel: this.#tunnel,
    };
  }

  logs(child?: "runner" | "supervisor", afterSeq = 0, limit?: number): LogLine[] {
    return this.#logs.read(child, afterSeq, limit);
  }

  clearLogs(): void {
    this.#logs.clear();
  }

  get running(): boolean {
    return this.#state === "starting" || this.#state === "running" || this.#state === "degraded";
  }

  get preflight(): PreflightReport {
    return this.#preflight;
  }

  get paired(): boolean {
    return this.#pairing.read() !== null;
  }

  /** The stored pairing, without its token — what the UI is allowed to see. */
  pairingInfo(): RunnerPairingSummary | null {
    const bundle = this.#pairing.read();
    return bundle ? pairingSummary(bundle) : null;
  }

  // --- configuration -----------------------------------------------------------

  configure(settings: UserSettings, overrides: Overrides = {}): void {
    this.#settings = settings;
    this.#overrides = overrides;
  }

  async detect(): Promise<PreflightReport> {
    this.#preflight = await this.#sweep();
    this.#child.send(preflightMessage(this.#preflight));
    return this.#preflight;
  }

  /**
   * The embedder moved (a restart binds a new port): tell a running runner,
   * on its control channel. A runner that is not running learns the address
   * at its next spawn instead.
   */
  setEmbeddingsBaseURL(url: string): void {
    this.#child.send(embeddingsMessage(url));
  }

  /** The account preflight, told which providers hold a key — by id, never the key. */
  #sweep(): Promise<PreflightReport> {
    return accountPreflight({ overrides: this.#overrides, providerIds: usableProviderIds(this.#providers.read()) });
  }

  // --- pairing -----------------------------------------------------------------

  /**
   * Validate, store, and connect — pairing this computer is the same act as
   * starting to use it, so there is no separate "paired but not connected"
   * step for a person to get stuck on.
   */
  pair(bundle: RunnerPairingBundle): Promise<SupervisorSnapshot> {
    return this.#serialise(async () => {
      const clean = asPairingBundle(bundle);
      if (!clean) return this.#fail("The pairing response from the server was not usable. Try again.");
      if (this.running) await this.#stop("stopped");
      this.#pairing.store(clean);
      return this.#connect();
    });
  }

  /** Stop the runner and forget the pairing. Irreversible without pairing again. */
  unpair(): Promise<SupervisorSnapshot> {
    return this.#serialise(async () => {
      const result = await this.#stop("stopped");
      this.#pairing.forget();
      return result;
    });
  }

  // --- lifecycle -----------------------------------------------------------

  connect(): Promise<SupervisorSnapshot> {
    return this.#serialise(() => this.#connect());
  }

  disconnect(): Promise<SupervisorSnapshot> {
    return this.#serialise(() => this.#stop("stopped"));
  }

  restart(): Promise<SupervisorSnapshot> {
    return this.#serialise(async () => {
      await this.#stop("stopped");
      return this.#connect();
    });
  }

  /** The quit path. */
  drain(): Promise<SupervisorSnapshot> {
    return this.#serialise(() => this.#stop("stopped"));
  }

  #serialise<T>(fn: () => Promise<T>): Promise<T> {
    const next = this.#transition.then(fn, fn);
    this.#transition = next.catch(() => undefined);
    return next;
  }

  async #connect(): Promise<SupervisorSnapshot> {
    if (this.running) return this.snapshot();

    const settings = this.#settings;
    if (!settings) return this.#fail("no settings loaded");
    const bundle = this.#pairing.read();
    if (!bundle) return this.#fail("This computer is not paired yet.");

    this.#blocker = undefined;
    this.#setState("preflight");
    this.#note("Checking what this computer can do…");
    this.#preflight = await this.#sweep();

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

    const runnerBin = itemById(this.#preflight, "runner");
    if (!runnerBin?.path) return this.#fail("The runner binary is missing from this copy of TaskTrooper.");

    this.#child.resetCounters();
    this.#tunnel = { state: "unknown", changedAt: Date.now() };

    const abort = new AbortController();
    this.#startAbort = abort;
    this.#setState("starting");
    this.#note("Connecting to TaskTrooper…");
    const embeddings = await this.#embeddings();

    try {
      await this.#child.start({
        command: runnerBin.path,
        args: [],
        env: childEnv(this.#preflight),
        stdin: this.#config(bundle, settings, embeddings),
      });
    } catch (err) {
      await this.#stop("failed");
      return this.#fail(`The tunnel failed to start: ${describe(err)}`);
    }
    // The config line carries tool paths, not the report: the runner answers
    // preflight.report only from this control line, so without it a fresh
    // connect said "not reported yet" until something re-detected.
    this.#child.send(preflightMessage(this.#preflight));

    const attached = await this.#waitForTunnel(abort.signal, () => this.#child.running);
    if (!attached.ok) {
      if (abort.signal.aborted) {
        await this.#stop("stopped");
        return this.snapshot();
      }
      await this.#stop("failed");
      return this.#fail(attached.message);
    }

    this.#child.markHealthy();
    this.#startAbort = null;
    this.#setState("running");
    this.#note("Attached. Tasks assigned to you now run on this computer.");
    return this.snapshot();
  }

  async #stop(final: SupervisorState): Promise<SupervisorSnapshot> {
    this.#startAbort?.abort();
    this.#startAbort = null;

    if (this.#state !== "idle" && this.#state !== "stopped") {
      this.#setState("stopping");
      this.#note("Stopping…");
    }

    if (this.#child.state !== "idle") await this.#child.stop();

    this.#tunnel = { state: "detached", changedAt: Date.now(), detail: "stopped" };
    if (final !== "failed") {
      this.#setState(final);
      this.#note("Stopped.");
    }
    return this.snapshot();
  }

  // --- wiring --------------------------------------------------------------

  /**
   * The stdin document, built at each spawn: the providers are read from
   * their store here, so a key changed since the last start is the one sent.
   */
  #config(bundle: RunnerPairingBundle, settings: UserSettings, embeddings: string | null): string {
    return runnerConfig({
      bundle,
      settings,
      preflight: this.#preflight,
      providers: this.#providers.read(),
      mcpServers: this.#mcpServers.read(),
      executorDataDir: executorDataDir(),
      executorPostgresCacheDir: postgresCacheDir(),
      runnerDataDir: runnerDataDir(),
      ...(embeddings !== null ? { embeddingsBaseURL: embeddings } : {}),
    });
  }

  #waitForTunnel(signal: AbortSignal, alive: () => boolean): Promise<{ ok: true } | { ok: false; message: string }> {
    const deadline = Date.now() + ATTACH_TIMEOUT_MS;
    return new Promise((resolve) => {
      const finish = (result: { ok: true } | { ok: false; message: string }): void => {
        clearInterval(timer);
        signal.removeEventListener("abort", onAbort);
        resolve(result);
      };
      const onAbort = (): void => finish({ ok: false, message: "Cancelled." });

      const timer = setInterval(() => {
        if (this.#tunnel.state === "attached") return finish({ ok: true });
        if (this.#tunnel.state === "auth-failed") {
          return finish({
            ok: false,
            message: this.#tunnel.detail ?? "The control plane rejected this computer. Pair it again — retrying will not help.",
          });
        }
        if (!alive()) {
          return finish({ ok: false, message: "The tunnel process exited before it attached. Its own last line says why." });
        }
        if (Date.now() >= deadline) {
          return finish({ ok: false, message: `The tunnel did not attach within ${ATTACH_TIMEOUT_MS / 1000}s. Check this computer's network.` });
        }
      }, 250);
      timer.unref?.();
      signal.addEventListener("abort", onAbort, { once: true });
    });
  }

  #onChildLog(stream: "stdout" | "stderr", text: string): void {
    const parsed = parseRunnerLine(text);
    if (parsed.tunnel) this.#setTunnel(parsed.tunnel);
    if (isAuthRejection(text)) {
      this.#setTunnel({
        state: "auth-failed",
        changedAt: Date.now(),
        detail: "The control plane rejected this computer's runner token. Pair it again — retrying will not help.",
      });
    }
    this.#queueLog(this.#logs.append("runner", stream, parsed.text, parsed.level));
  }

  #onChildCrashed(): void {
    if (!this.running) return;

    if (this.#tunnel.state !== "auth-failed") {
      this.#setTunnel({ state: "detached", changedAt: Date.now(), detail: "the tunnel process exited" });
    }

    if (this.#tunnel.state === "auth-failed") {
      this.#note("The tunnel was rejected; not retrying. Pair this computer again.");
      this.#setState("degraded");
      return;
    }

    this.#setState("degraded");
    const delay = this.#child.scheduleRestart(() => {
      void this.#serialise(async () => {
        if (!this.running) return;
        await this.#restartAfterCrash();
      });
    });
    this.#note(`runner ${this.#child.status().detail ?? "exited"}; restarting in ${Math.round(delay / 1000)}s`);
  }

  async #restartAfterCrash(): Promise<void> {
    const bundle = this.#pairing.read();
    const settings = this.#settings;
    const runnerBin = itemById(this.#preflight, "runner");
    if (!bundle || !settings || !runnerBin?.path) return;

    this.#note("Restarting…");
    const embeddings = await this.#embeddings();
    try {
      await this.#child.start({
        command: runnerBin.path,
        args: [],
        env: childEnv(this.#preflight),
        stdin: this.#config(bundle, settings, embeddings),
      });
    } catch (err) {
      this.#note(`failed to restart: ${describe(err)}`);
      return;
    }
    this.#child.send(preflightMessage(this.#preflight));

    const abort = new AbortController();
    const attached = await this.#waitForTunnel(abort.signal, () => this.#child.running);
    if (!attached.ok) {
      this.#note(`did not attach after restarting: ${attached.message}`);
      return;
    }
    this.#child.markHealthy();
    this.#note("Back up.");
    this.#recomputeState();
  }

  // --- state ---------------------------------------------------------------

  #setTunnel(next: TunnelStatus): void {
    this.#tunnel = next;
    this.#recomputeState();
  }

  #recomputeState(): void {
    if (!this.running) {
      this.#emitState();
      return;
    }
    this.#setState(this.#child.status().state === "healthy" && this.#tunnel.state === "attached" ? "running" : "degraded");
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
    this.#queueLog(this.#logs.append("supervisor", "stderr", message));
    this.#emitState();
    return this.snapshot();
  }

  #note(text: string): void {
    this.#detail = text;
    this.#queueLog(this.#logs.append("supervisor", "stdout", text));
  }

  #emitState(): void {
    this.emit("state", this.snapshot());
  }

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
