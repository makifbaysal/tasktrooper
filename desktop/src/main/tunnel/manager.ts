import { EventEmitter } from "node:events";
import type {
  RemoteAccessConfig,
  RemoteAccessState,
  RemoteBinaryDetection,
  RemoteProvider,
  RemoteTunnelProblem,
  RemoteTunnelState,
} from "../../ipc/types.js";
import type { RemoteAccessConfigureRequest } from "../../ipc/channels.js";
import { PROVIDER_FACTS } from "./detect.js";
import { buildLaunch, parseLine, parseNgrokTunnels, redact, type TunnelEvent } from "./parse.js";
import type { TunnelProcess, TunnelSpawner } from "./process.js";
import type { RemoteListenerClient, ServerEndpoint } from "./server-client.js";
import { DEFAULT_REMOTE_PORT, secretsView, type RemoteAccessStoreLike, type RemoteSecrets } from "./store.js";

/**
 * The tunnel for the phone app: one cloudflared or ngrok process pointed at the
 * server's remote listener, kept alive while the person wants it.
 *
 * What this class decides:
 *  - Nothing starts without the server: the listener the tunnel targets is
 *    opened by the server (`/v1/remote-access/enable`), and a server restart
 *    brings the listener back on the same preferred port, so the running tunnel
 *    survives it (only a CHANGED port restarts the tunnel).
 *  - A crash is restarted on a capped backoff, because a tunnel that quietly
 *    stays down is a phone that quietly stops working. A refusal that waiting
 *    cannot fix (a bad token, an ngrok account error) stops and says so.
 *  - Secrets reach the child only through its environment and never appear in
 *    state, logs or errors (`redact`).
 *  - Stopping the tunnel also closes the server's remote listener, so nothing
 *    listens for the phone while no one asked for it.
 */

export interface TunnelManagerDeps {
  store: RemoteAccessStoreLike;
  detect(provider: RemoteProvider): Promise<RemoteBinaryDetection>;
  spawn: TunnelSpawner;
  listener: RemoteListenerClient;
  /** The local server's address and bearer, or null while it is down. */
  server(): ServerEndpoint | null;
  /** GET json from ngrok's local inspection API. */
  fetchJson(url: string): Promise<unknown>;
  now?(): number;
  /** Not available in account mode. */
  isLocalMode?(): boolean;
}

const MIN_BACKOFF_MS = 1_000;
const MAX_BACKOFF_MS = 30_000;
const STABLE_AFTER_MS = 60_000;
const LOG_CAPACITY = 200;
const LOG_TAIL = 40;
const NGROK_POLL_MS = 1_000;
const NGROK_POLL_TRIES = 30;
const DEFAULT_NGROK_API = "127.0.0.1:4040";

interface Events {
  state: [RemoteAccessState];
}

export class TunnelManager extends EventEmitter<Events> {
  readonly #deps: TunnelManagerDeps;
  readonly #now: () => number;

  #wanted = false;
  #state: RemoteTunnelState = "stopped";
  #problem: RemoteTunnelProblem | undefined;
  #error: string | undefined;
  #url: string | undefined;
  #port: number | undefined;
  #since: number;
  #restarts = 0;
  #detection: RemoteBinaryDetection | undefined;
  #logs: string[] = [];

  #proc: TunnelProcess | null = null;
  #gen = 0;
  #registered = false;
  #fatal = false;
  #lastError: string | undefined;
  #startedAt = 0;
  #backoff = MIN_BACKOFF_MS;
  #restartTimer: NodeJS.Timeout | null = null;
  #pollTimer: NodeJS.Timeout | null = null;
  #emitTimer: NodeJS.Timeout | null = null;
  #ngrokApi = DEFAULT_NGROK_API;
  #queue: Promise<unknown> = Promise.resolve();

  constructor(deps: TunnelManagerDeps) {
    super();
    this.#deps = deps;
    this.#now = deps.now ?? Date.now;
    this.#since = this.#now();
  }

  // --- public ---------------------------------------------------------------

  snapshot(): RemoteAccessState {
    const secrets = this.#deps.store.secrets();
    return {
      tunnel: this.#state,
      wanted: this.#wanted,
      config: this.#deps.store.config(),
      secrets: secretsView(secrets),
      ...(this.#url !== undefined ? { url: this.#url } : {}),
      ...(this.#port !== undefined ? { remotePort: this.#port } : {}),
      ...(this.#problem !== undefined ? { problem: this.#problem } : {}),
      ...(this.#error !== undefined ? { error: this.#error } : {}),
      since: this.#since,
      restarts: this.#restarts,
      ...(this.#detection ? { detection: this.#detection } : {}),
      logTail: this.#logs.slice(-LOG_TAIL),
      suggestedPort: this.#deps.store.lastPort() || DEFAULT_REMOTE_PORT,
    };
  }

  logs(): string[] {
    return [...this.#logs];
  }

  /** Launch: honour the auto-start switch. The server may not be up yet; `onServer` continues. */
  async init(): Promise<void> {
    if (this.#wanted || (this.#deps.isLocalMode && !this.#deps.isLocalMode())) return;
    if (!this.#deps.store.config().autoStart) return;
    await this.start();
  }

  start(): Promise<RemoteAccessState> {
    return this.#serial(async () => {
      this.#assertLocal();
      this.#wanted = true;
      this.#backoff = MIN_BACKOFF_MS;
      await this.#launch();
      return this.snapshot();
    });
  }

  stop(): Promise<RemoteAccessState> {
    return this.#serial(async () => {
      await this.#halt(true);
      return this.snapshot();
    });
  }

  /** The app is quitting or leaving local mode: stop without touching the server. */
  shutdown(): Promise<void> {
    return this.#serial(async () => {
      this.#wanted = false;
      this.#gen += 1;
      this.#clearTimers();
      const proc = this.#proc;
      this.#proc = null;
      await proc?.kill();
      this.#set("stopped");
    });
  }

  /** The server (re)started, or went away (null). */
  onServer(): Promise<void> {
    return this.#serial(async () => {
      if (!this.#wanted) return;
      if (this.#deps.isLocalMode && !this.#deps.isLocalMode()) return;
      const server = this.#deps.server();
      if (!server) return;
      const needsLaunch = !this.#proc || this.#state === "error" || this.#state === "binary_missing" || this.#problem === "server_unavailable";
      if (needsLaunch && !this.#fatal) {
        await this.#launch();
        return;
      }
      if (!this.#proc) return;
      const port = await this.#enable(server).catch((err: unknown) => {
        this.#fail("enable_failed", errorText(err));
        return undefined;
      });
      if (port !== undefined && port !== this.#port) await this.#launch();
    });
  }

  configure(request: RemoteAccessConfigureRequest): Promise<RemoteAccessState> {
    return this.#serial(async () => {
      this.#assertLocal();
      const before = this.#deps.store.config();
      const secretsBefore = this.#deps.store.secrets();
      const { cloudflareToken, ngrokAuthtoken, clearSecrets, ...patch } = request;
      const secretSet: RemoteSecrets = {
        ...(cloudflareToken !== undefined ? { cloudflareToken } : {}),
        ...(ngrokAuthtoken !== undefined ? { ngrokAuthtoken } : {}),
      };
      if (Object.keys(secretSet).length > 0 || (clearSecrets?.length ?? 0) > 0) this.#deps.store.setSecrets(secretSet, clearSecrets ?? []);
      const after = this.#deps.store.setConfig(patch);
      const secretsAfter = this.#deps.store.secrets();
      const launchChanged =
        before.provider !== after.provider ||
        before.mode !== after.mode ||
        before.hostname !== after.hostname ||
        before.serverName !== after.serverName ||
        secretsBefore.cloudflareToken !== secretsAfter.cloudflareToken ||
        secretsBefore.ngrokAuthtoken !== secretsAfter.ngrokAuthtoken;
      if (this.#wanted && launchChanged) {
        this.#backoff = MIN_BACKOFF_MS;
        await this.#launch();
      } else {
        if (before.provider !== after.provider) this.#detection = undefined;
        this.#publish();
      }
      return this.snapshot();
    });
  }

  async detect(provider: RemoteProvider): Promise<RemoteBinaryDetection> {
    const detection = await this.#deps.detect(provider);
    if (provider === this.#deps.store.config().provider) {
      this.#detection = detection;
      if (this.#state === "binary_missing" && detection.installed) this.#state = "stopped";
      this.#publish();
    }
    return detection;
  }

  // --- the machinery --------------------------------------------------------

  #serial<T>(fn: () => Promise<T>): Promise<T> {
    const run = this.#queue.then(fn, fn);
    this.#queue = run.catch(() => undefined);
    return run;
  }

  #assertLocal(): void {
    if (this.#deps.isLocalMode && !this.#deps.isLocalMode()) {
      throw new Error("Remote access is available while TaskTrooper runs locally.");
    }
  }

  async #halt(closeListener: boolean): Promise<void> {
    this.#wanted = false;
    this.#gen += 1;
    this.#clearTimers();
    const proc = this.#proc;
    this.#proc = null;
    await proc?.kill();
    this.#url = undefined;
    this.#port = undefined;
    this.#problem = undefined;
    this.#error = undefined;
    this.#restarts = 0;
    this.#fatal = false;
    this.#set("stopped");
    if (closeListener) {
      const server = this.#deps.server();
      if (server) await this.#deps.listener.disable(server).catch(() => undefined);
    }
  }

  #clearTimers(): void {
    for (const timer of [this.#restartTimer, this.#pollTimer]) if (timer) clearTimeout(timer);
    this.#restartTimer = null;
    this.#pollTimer = null;
  }

  async #enable(server: ServerEndpoint): Promise<number> {
    const config = this.#deps.store.config();
    const port = await this.#deps.listener.enable(server, {
      preferredPort: this.#deps.store.lastPort(),
      serverName: config.serverName,
    });
    this.#deps.store.setLastPort(port);
    return port;
  }

  /** Start (or restart) the program. Always called inside `#serial`. */
  async #launch(): Promise<void> {
    const gen = ++this.#gen;
    this.#clearTimers();
    const previous = this.#proc;
    this.#proc = null;
    await previous?.kill();

    this.#url = undefined;
    this.#registered = false;
    this.#fatal = false;
    this.#lastError = undefined;
    this.#problem = undefined;
    this.#error = undefined;
    this.#ngrokApi = DEFAULT_NGROK_API;
    this.#set("starting");

    const config = this.#deps.store.config();
    const secrets = this.#deps.store.secrets();
    const detection = await this.#deps.detect(config.provider);
    if (gen !== this.#gen) return;
    this.#detection = detection;
    if (!detection.installed || !detection.path) {
      this.#set("binary_missing");
      return;
    }

    const secret = config.provider === "cloudflare" ? secrets.cloudflareToken : secrets.ngrokAuthtoken;
    const problem = this.#configProblem(config, secret);
    if (problem) {
      this.#fail(problem.problem, problem.message);
      return;
    }

    const server = this.#deps.server();
    if (!server) {
      this.#fail("server_unavailable", "Waiting for the local server to start.", "starting");
      return;
    }
    let port: number;
    try {
      port = await this.#enable(server);
    } catch (err) {
      if (gen !== this.#gen) return;
      this.#fail("enable_failed", `Could not open the remote access port on the local server: ${errorText(err)}`);
      return;
    }
    if (gen !== this.#gen) return;
    this.#port = port;

    const launch = buildLaunch({
      provider: config.provider,
      mode: config.mode,
      port,
      hostname: config.hostname,
      secret: secret ?? "",
    });
    if (config.provider === "cloudflare" && config.mode === "named") this.#url = `https://${config.hostname}`;

    const proc = this.#deps.spawn(detection.path, launch.args, launch.env);
    this.#proc = proc;
    this.#startedAt = this.#now();
    const secretList = [secret ?? ""];
    proc.onLine((line) => {
      if (gen !== this.#gen) return;
      this.#pushLog(redact(line, secretList));
      const event = parseLine(config.provider, line);
      if (event) this.#apply(event, config.provider, secretList);
    });
    proc.onExit((code, signal) => {
      if (gen !== this.#gen) return;
      void this.#serial(async () => {
        if (gen !== this.#gen) return;
        this.#proc = null;
        this.#onExit(code, signal);
      });
    });
    if (config.provider === "ngrok") this.#pollNgrok(gen, port, 0);
    this.#publish();
  }

  #configProblem(config: RemoteAccessConfig, secret: string | undefined): { problem: RemoteTunnelProblem; message: string } | null {
    if (config.provider === "cloudflare" && config.mode === "named") {
      if (!secret) return { problem: "needs_token", message: "Paste the tunnel token from your Cloudflare dashboard." };
      if (config.hostname === "") return { problem: "needs_hostname", message: "Enter the public hostname you routed to this tunnel in Cloudflare." };
    }
    if (config.provider === "ngrok") {
      if (!secret) return { problem: "needs_token", message: "Paste your ngrok authtoken." };
      if (config.mode === "named" && config.hostname === "") {
        return { problem: "needs_hostname", message: "Enter your ngrok domain." };
      }
    }
    return null;
  }

  #apply(event: TunnelEvent, provider: RemoteProvider, secrets: readonly string[]): void {
    switch (event.kind) {
      case "url":
        this.#url = event.url;
        if (provider === "ngrok") this.#registered = true;
        break;
      case "connected":
        this.#registered = true;
        break;
      case "reconnecting":
        if (this.#state === "connected") this.#set("reconnecting");
        return;
      case "api_addr":
        this.#ngrokApi = event.addr;
        return;
      case "fatal":
        this.#fatal = true;
        this.#fail(event.problem, redact(event.message, secrets));
        void this.#proc?.kill();
        return;
      case "error":
        this.#lastError = redact(event.message, secrets);
        return;
    }
    if (this.#registered && this.#url !== undefined) {
      this.#error = undefined;
      this.#problem = undefined;
      this.#set("connected");
    } else this.#publish();
  }

  #onExit(code: number | null, signal: string | null): void {
    if (this.#fatal || !this.#wanted) return;
    const ranFor = this.#now() - this.#startedAt;
    if (ranFor >= STABLE_AFTER_MS) this.#backoff = MIN_BACKOFF_MS;
    const wait = this.#backoff;
    this.#backoff = Math.min(this.#backoff * 2, MAX_BACKOFF_MS);
    const how = signal ? `was stopped (${signal})` : `exited (code ${code ?? "?"})`;
    this.#problem = "exited";
    this.#error = `${PROVIDER_FACTS[this.#deps.store.config().provider].binary} ${how}${this.#lastError ? `: ${this.#lastError}` : ""}. Restarting in ${Math.ceil(wait / 1000)} s.`;
    this.#restarts += 1;
    this.#registered = false;
    this.#set("reconnecting");
    this.#restartTimer = setTimeout(() => {
      this.#restartTimer = null;
      if (!this.#wanted) return;
      void this.#serial(async () => {
        if (this.#wanted && !this.#proc) await this.#launch();
      });
    }, wait);
    this.#restartTimer.unref?.();
  }

  #pollNgrok(gen: number, port: number, attempt: number): void {
    this.#pollTimer = setTimeout(() => {
      this.#pollTimer = null;
      if (gen !== this.#gen || this.#url !== undefined || attempt >= NGROK_POLL_TRIES) return;
      this.#deps
        .fetchJson(`http://${this.#ngrokApi}/api/tunnels`)
        .then((body) => {
          if (gen !== this.#gen) return;
          const url = parseNgrokTunnels(body, port);
          if (url) this.#apply({ kind: "url", url }, "ngrok", []);
          else this.#pollNgrok(gen, port, attempt + 1);
        })
        .catch(() => {
          if (gen === this.#gen) this.#pollNgrok(gen, port, attempt + 1);
        });
    }, NGROK_POLL_MS);
    this.#pollTimer.unref?.();
  }

  #fail(problem: RemoteTunnelProblem, message: string, state: RemoteTunnelState = "error"): void {
    this.#problem = problem;
    this.#error = message;
    this.#set(state);
  }

  #pushLog(line: string): void {
    if (line.trim() === "") return;
    this.#logs.push(line.length > 400 ? `${line.slice(0, 399)}…` : line);
    if (this.#logs.length > LOG_CAPACITY) this.#logs.splice(0, this.#logs.length - LOG_CAPACITY);
    this.#publishSoon();
  }

  #set(state: RemoteTunnelState): void {
    if (state !== this.#state) this.#since = this.#now();
    this.#state = state;
    this.#publish();
  }

  #publish(): void {
    if (this.#emitTimer) {
      clearTimeout(this.#emitTimer);
      this.#emitTimer = null;
    }
    this.emit("state", this.snapshot());
  }

  /** Log lines arrive in bursts; the page gets one push per burst. */
  #publishSoon(): void {
    if (this.#emitTimer) return;
    this.#emitTimer = setTimeout(() => {
      this.#emitTimer = null;
      this.emit("state", this.snapshot());
    }, 250);
    this.#emitTimer.unref?.();
  }
}

function errorText(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}
