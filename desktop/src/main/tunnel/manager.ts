import { EventEmitter } from "node:events";
import path from "node:path";
import type {
  RemoteAccessConfig,
  RemoteAccessState,
  RemoteCloudflareAccount,
  RemoteNgrokAccount,
  RemoteBinaryDetection,
  RemoteProvider,
  RemoteTunnelProblem,
  RemoteTunnelState,
} from "../../ipc/types.js";
import type { RemoteAccessConfigureRequest, RemoteAccessNgrokConnectRequest } from "../../ipc/channels.js";
import {
  cloudflareConfig,
  cloudflareConfigPath,
  originCertPath,
  parseLoginLine,
  parseOriginCert,
  parseZoneName,
  provisionTunnel,
  type OriginCert,
} from "./cloudflare.js";
import { listNgrokDomains, probeNgrok } from "./ngrok.js";
import { PROVIDER_FACTS } from "./detect.js";
import { buildLaunch, parseLine, parseNgrokTunnels, redact, type TunnelEvent } from "./parse.js";
import { runToEnd, type TunnelFiles, type TunnelProcess, type TunnelSpawner } from "./process.js";
import type { RemoteListenerClient, ServerEndpoint } from "./server-client.js";
import { DEFAULT_REMOTE_PORT, secretsView, type RemoteAccessStoreLike } from "./store.js";

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
 *  - The ngrok authtoken reaches the child only through its environment and
 *    never appears in state, logs or errors (`redact`). Cloudflare has no
 *    secret of its own: the person logs in (`cloudflared tunnel login`) and the
 *    certificate cloudflared keeps is what creates and routes our named tunnel.
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
  /** GET json: ngrok's local inspection API, ngrok's and Cloudflare's REST APIs (with `headers`). */
  fetchJson(url: string, headers?: Record<string, string>): Promise<unknown>;
  files: TunnelFiles;
  /** The person's home: where `cloudflared tunnel login` keeps the certificate. */
  homeDir: string;
  /** The app's data directory: where the named tunnel's config file is written. */
  dataDir: string;
  env?: NodeJS.ProcessEnv;
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
const LOGIN_TIMEOUT_MS = 10 * 60_000;
const CLOUDFLARE_STEP_TIMEOUT_MS = 90_000;

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

  // Cloudflare login (the certificate on disk) and the browser round trip.
  #cert: OriginCert | undefined;
  #domain: string | undefined;
  #domainError: string | undefined;
  #loginProc: TunnelProcess | null = null;
  #loginGen = 0;
  #loginTimer: NodeJS.Timeout | null = null;
  #cfPhase: RemoteCloudflareAccount["phase"] = "idle";
  #cfLoginUrl: string | undefined;
  #cfError: string | undefined;

  // ngrok: the test tunnel that proves an authtoken, and what came of it.
  #ngrokPhase: RemoteNgrokAccount["phase"] = "idle";
  #ngrokError: string | undefined;
  #ngrokApiKeyError: string | undefined;

  constructor(deps: TunnelManagerDeps) {
    super();
    this.#deps = deps;
    this.#now = deps.now ?? Date.now;
    this.#since = this.#now();
    this.#readCert();
  }

  // --- public ---------------------------------------------------------------

  snapshot(): RemoteAccessState {
    const secrets = this.#deps.store.secrets();
    return {
      tunnel: this.#state,
      wanted: this.#wanted,
      config: this.#deps.store.config(),
      secrets: secretsView(secrets),
      cloudflare: this.#cloudflareView(),
      ngrok: this.#ngrokView(secrets.ngrokAuthtoken !== undefined),
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
      const login = this.#loginProc;
      this.#loginGen += 1;
      this.#loginProc = null;
      if (this.#loginTimer) clearTimeout(this.#loginTimer);
      this.#loginTimer = null;
      await login?.kill();
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
      const after = this.#deps.store.setConfig(request);
      const launchChanged =
        before.provider !== after.provider ||
        before.mode !== after.mode ||
        before.hostname !== after.hostname ||
        before.serverName !== after.serverName;
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

  // --- accounts: Cloudflare login, ngrok authtoken ---------------------------

  /** Re-read the Cloudflare certificate and, once per login, ask Cloudflare for its domain. */
  async refreshAccount(): Promise<RemoteAccessState> {
    this.#assertLocal();
    this.#readCert();
    await this.#lookUpDomain();
    this.#publish();
    return this.snapshot();
  }

  /**
   * `cloudflared tunnel login`. It opens the browser itself and waits there; the
   * answer comes back at once and the end of the login arrives as a state push.
   */
  async cloudflareLogin(): Promise<RemoteAccessState> {
    this.#assertLocal();
    if (this.#loginProc) return this.snapshot();
    this.#readCert();
    if (this.#cert) {
      await this.#lookUpDomain();
      this.#publish();
      return this.snapshot();
    }
    const detection = await this.#deps.detect("cloudflare");
    if (this.#deps.store.config().provider === "cloudflare") this.#detection = detection;
    if (!detection.installed || !detection.path) {
      this.#cfPhase = "failed";
      this.#cfError = `${detection.detail ?? "cloudflared was not found on this computer."} Install it, then log in.`;
      this.#publish();
      return this.snapshot();
    }
    // A leftover file that is not a login would make cloudflared refuse ("existing certificate").
    const certFile = this.#certFile();
    if (this.#deps.files.exists(certFile)) this.#deps.files.remove(certFile);

    const gen = ++this.#loginGen;
    this.#cfPhase = "waiting";
    this.#cfLoginUrl = undefined;
    this.#cfError = undefined;
    let lastError: string | undefined;
    const proc = this.#deps.spawn(detection.path, ["tunnel", "login"], {});
    this.#loginProc = proc;
    proc.onLine((line) => {
      if (gen !== this.#loginGen) return;
      const event = parseLoginLine(line);
      if (event?.kind === "url") this.#cfLoginUrl = event.url;
      else if (event?.kind === "error") lastError = redact(event.message);
      else return;
      this.#publish();
    });
    proc.onExit(() => {
      if (gen !== this.#loginGen) return;
      void (async () => {
        this.#loginProc = null;
        if (this.#loginTimer) clearTimeout(this.#loginTimer);
        this.#loginTimer = null;
        this.#readCert();
        if (this.#cert) {
          this.#cfPhase = "idle";
          this.#cfLoginUrl = undefined;
          this.#cfError = undefined;
          await this.#lookUpDomain();
        } else {
          this.#cfPhase = "failed";
          this.#cfError = lastError ?? "The Cloudflare login did not finish. Try again.";
        }
        if (gen === this.#loginGen) this.#publish();
      })();
    });
    this.#loginTimer = setTimeout(() => {
      this.#loginTimer = null;
      if (gen !== this.#loginGen) return;
      this.#loginGen += 1;
      this.#loginProc = null;
      void proc.kill();
      this.#cfPhase = "failed";
      this.#cfLoginUrl = undefined;
      this.#cfError = "Nobody finished the Cloudflare login in the browser. Try again.";
      this.#publish();
    }, LOGIN_TIMEOUT_MS);
    this.#loginTimer.unref?.();
    this.#publish();
    return this.snapshot();
  }

  async cloudflareLoginCancel(): Promise<RemoteAccessState> {
    this.#assertLocal();
    const proc = this.#loginProc;
    this.#loginGen += 1;
    this.#loginProc = null;
    if (this.#loginTimer) clearTimeout(this.#loginTimer);
    this.#loginTimer = null;
    await proc?.kill();
    this.#cfPhase = "idle";
    this.#cfLoginUrl = undefined;
    this.#cfError = undefined;
    this.#readCert();
    this.#publish();
    return this.snapshot();
  }

  /**
   * Forget the login: the certificate is deleted. The tunnel made earlier stays in
   * the person's Cloudflare account and is reused if they log in to the same domain again.
   */
  cloudflareLogout(): Promise<RemoteAccessState> {
    return this.#serial(async () => {
      this.#assertLocal();
      const config = this.#deps.store.config();
      if (this.#wanted && config.provider === "cloudflare" && config.mode === "named") {
        throw new Error("Stop the tunnel before logging out of Cloudflare.");
      }
      await this.cloudflareLoginCancel();
      this.#deps.files.remove(this.#certFile());
      this.#cert = undefined;
      this.#domain = undefined;
      this.#domainError = undefined;
      this.#cfError = undefined;
      this.#publish();
      return this.snapshot();
    });
  }

  /**
   * Prove the authtoken with a throw-away tunnel, then store it. With an API key
   * the account's domains are listed too (ngrok's REST API does not take an
   * authtoken). With neither, the domains are listed again from the stored key.
   */
  ngrokConnect(request: RemoteAccessNgrokConnectRequest): Promise<RemoteAccessState> {
    return this.#serial(async () => {
      this.#assertLocal();
      const config = this.#deps.store.config();
      if (this.#wanted && config.provider === "ngrok") throw new Error("Stop the tunnel before changing the ngrok account.");
      this.#ngrokError = undefined;
      this.#ngrokApiKeyError = undefined;

      if (request.authtoken !== undefined) {
        const detection = await this.#deps.detect("ngrok");
        if (config.provider === "ngrok") this.#detection = detection;
        if (!detection.installed || !detection.path) {
          this.#ngrokError = `${detection.detail ?? "ngrok was not found on this computer."} Install it, then connect.`;
          this.#publish();
          return this.snapshot();
        }
        this.#ngrokPhase = "checking";
        this.#publish();
        const probe = await probeNgrok(this.#deps.spawn, detection.path, request.authtoken);
        this.#ngrokPhase = "idle";
        if (!probe.ok) {
          this.#ngrokError = probe.message;
          this.#publish();
          return this.snapshot();
        }
        this.#deps.store.setSecrets({ ngrokAuthtoken: request.authtoken });
        this.#deps.store.setNgrokAccount({ verified: true, domains: this.#deps.store.ngrokAccount().domains });
      }

      const apiKey = request.apiKey ?? this.#deps.store.secrets().ngrokApiKey;
      if (apiKey !== undefined && (request.apiKey !== undefined || request.authtoken === undefined)) {
        try {
          const domains = await listNgrokDomains(this.#deps.fetchJson, apiKey);
          if (request.apiKey !== undefined) this.#deps.store.setSecrets({ ngrokApiKey: request.apiKey });
          const account = this.#deps.store.ngrokAccount();
          this.#deps.store.setNgrokAccount({ verified: account.verified, domains });
        } catch (err) {
          this.#ngrokApiKeyError = redact(errorText(err), [apiKey]);
        }
      } else if (request.authtoken === undefined) {
        this.#ngrokError = "Paste your ngrok authtoken first.";
      }
      this.#publish();
      return this.snapshot();
    });
  }

  ngrokDisconnect(): Promise<RemoteAccessState> {
    return this.#serial(async () => {
      this.#assertLocal();
      if (this.#wanted && this.#deps.store.config().provider === "ngrok") throw new Error("Stop the tunnel before changing the ngrok account.");
      this.#deps.store.setSecrets({}, ["ngrokAuthtoken", "ngrokApiKey"]);
      this.#deps.store.setNgrokAccount(undefined);
      this.#ngrokError = undefined;
      this.#ngrokApiKeyError = undefined;
      this.#publish();
      return this.snapshot();
    });
  }

  // --- the machinery --------------------------------------------------------

  #certFile(): string {
    return originCertPath(this.#deps.homeDir, this.#deps.env ?? process.env);
  }

  /** The login is whatever certificate is on disk. A file that is not one counts as no login. */
  #readCert(): void {
    const pem = this.#deps.files.read(this.#certFile());
    const cert = (pem === null ? null : parseOriginCert(pem)) ?? undefined;
    if (cert?.zoneId !== this.#cert?.zoneId) {
      this.#domain = undefined;
      this.#domainError = undefined;
    }
    this.#cert = cert;
  }

  /** The certificate names a zone by id only; Cloudflare's API gives its domain. Asked once per login. */
  async #lookUpDomain(): Promise<void> {
    const cert = this.#cert;
    if (!cert || this.#domain !== undefined) return;
    try {
      const body = await this.#deps.fetchJson(`https://api.cloudflare.com/client/v4/zones/${encodeURIComponent(cert.zoneId)}`, {
        Authorization: `Bearer ${cert.apiToken}`,
        Accept: "application/json",
      });
      const name = parseZoneName(body);
      if (this.#cert?.zoneId !== cert.zoneId) return;
      if (name) {
        this.#domain = name;
        this.#domainError = undefined;
      } else this.#domainError = "Cloudflare did not say which domain this login is for.";
    } catch (err) {
      if (this.#cert?.zoneId === cert.zoneId) this.#domainError = redact(`Could not read the domain from Cloudflare (${errorText(err)}).`, [cert.apiToken]);
    }
  }

  #cloudflareView(): RemoteCloudflareAccount {
    return {
      loggedIn: this.#cert !== undefined,
      ...(this.#domain !== undefined ? { domain: this.#domain } : {}),
      ...(this.#cert && this.#domain === undefined && this.#domainError !== undefined ? { domainError: this.#domainError } : {}),
      phase: this.#cfPhase,
      ...(this.#cfLoginUrl !== undefined ? { loginUrl: this.#cfLoginUrl } : {}),
      ...(this.#cfError !== undefined ? { error: this.#cfError } : {}),
      suggestedSubdomain: `tasktrooper-${this.#deps.store.seed()}`,
    };
  }

  #ngrokView(hasToken: boolean): RemoteNgrokAccount {
    const account = this.#deps.store.ngrokAccount();
    return {
      verified: hasToken && account.verified,
      phase: this.#ngrokPhase,
      domains: account.domains,
      ...(this.#ngrokError !== undefined ? { error: this.#ngrokError } : {}),
      ...(this.#ngrokApiKeyError !== undefined ? { apiKeyError: this.#ngrokApiKeyError } : {}),
    };
  }

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

    const secret = config.provider === "ngrok" ? secrets.ngrokAuthtoken : undefined;
    this.#readCert();
    if (config.provider === "cloudflare" && config.mode === "named") await this.#lookUpDomain();
    if (gen !== this.#gen) return;
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

    let cloudflare: { configFile: string; tunnelId: string } | undefined;
    if (config.provider === "cloudflare" && config.mode === "named") {
      const made = await this.#prepareCloudflare(config.hostname, detection.path, port);
      if (gen !== this.#gen) return;
      if (!made.ok) {
        this.#fail(made.problem, made.message);
        return;
      }
      cloudflare = made.value;
    }

    const launch = buildLaunch({
      provider: config.provider,
      mode: config.mode,
      port,
      hostname: config.hostname,
      secret: secret ?? "",
      ...(cloudflare ? { cloudflare } : {}),
    });
    if (config.provider === "cloudflare" && config.mode === "named") this.#url = `https://${config.hostname}`;

    const proc = this.#deps.spawn(detection.path, launch.args, launch.env);
    this.#proc = proc;
    this.#startedAt = this.#now();
    const secretList = [secret ?? "", this.#cert?.apiToken ?? ""];
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
      if (!this.#cert) return { problem: "needs_login", message: "Log in to Cloudflare first." };
      if (config.hostname === "") return { problem: "needs_hostname", message: "Choose a subdomain for the phone address." };
      if (this.#domain !== undefined && !config.hostname.endsWith(`.${this.#domain}`)) {
        return { problem: "needs_hostname", message: `The address must end with .${this.#domain}, the domain you logged in with.` };
      }
    }
    if (config.provider === "ngrok") {
      if (!secret) return { problem: "needs_token", message: "Connect your ngrok account first (authtoken)." };
      if (config.mode === "named" && config.hostname === "") {
        return { problem: "needs_hostname", message: "Choose one of your ngrok domains." };
      }
    }
    return null;
  }

  /** Create (or reuse) our tunnel, route the hostname to it and write its config. */
  async #prepareCloudflare(
    hostname: string,
    binary: string,
    port: number,
  ): Promise<{ ok: true; value: { configFile: string; tunnelId: string } } | { ok: false; problem: RemoteTunnelProblem; message: string }> {
    const cert = this.#cert;
    if (!cert) return { ok: false, problem: "needs_login", message: "Log in to Cloudflare first." };
    this.#pushLog("Setting up your Cloudflare tunnel…");
    const result = await provisionTunnel({
      run: (command, args, timeoutMs = CLOUDFLARE_STEP_TIMEOUT_MS) => runToEnd(this.#deps.spawn, command, args, {}, timeoutMs),
      binary,
      files: this.#deps.files,
      certDir: path.dirname(this.#certFile()),
      hostname,
      zoneId: cert.zoneId,
      seed: this.#deps.store.seed(),
      record: this.#deps.store.cloudflareTunnel(),
    });
    if (!result.ok) return result;
    this.#deps.store.setCloudflareTunnel(result.record);
    const configFile = cloudflareConfigPath(this.#deps.dataDir);
    this.#deps.files.write(
      configFile,
      cloudflareConfig({ tunnelId: result.record.tunnelId, credentialsFile: result.record.credentialsFile, hostname, port }),
    );
    return { ok: true, value: { configFile, tunnelId: result.record.tunnelId } };
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
