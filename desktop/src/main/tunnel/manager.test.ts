import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { RemoteAccessConfig, RemoteBinaryDetection, RemoteProvider } from "../../ipc/types.js";
import type { CloudflareTunnelRecord } from "./cloudflare.js";
import { TunnelManager } from "./manager.js";
import { HttpStatusError, type TunnelFiles, type TunnelProcess } from "./process.js";
import { defaultRemoteConfig, DEFAULT_REMOTE_PORT, type NgrokAccountRecord, type RemoteAccessStoreLike, type RemoteSecrets } from "./store.js";

const AUTHTOKEN = "2abcDEF1234ghi_5JkLmNoPqRsTuVwXyZ12";
const CF_API_TOKEN = "cf-origin-api-token-SECRET-0123456789";
const TUNNEL_ID = "0b8a3c1e-5d2f-4c7a-9e11-3f6d8a2b4c90";
const HOME = "/home/u";
const CERT = `${HOME}/.cloudflared/cert.pem`;
const CREDENTIALS = `${HOME}/.cloudflared/${TUNNEL_ID}.json`;

/** What `cloudflared tunnel login` leaves behind: three PEM blocks, the last one base64 JSON. */
function certPem(zoneID = "zone-1", apiToken = CF_API_TOKEN): string {
  const json = Buffer.from(JSON.stringify({ zoneID, accountID: "acct-1", apiToken })).toString("base64");
  return [
    "-----BEGIN PRIVATE KEY-----\nAAAA\n-----END PRIVATE KEY-----",
    "-----BEGIN CERTIFICATE-----\nBBBB\n-----END CERTIFICATE-----",
    `-----BEGIN ARGO TUNNEL TOKEN-----\n${json}\n-----END ARGO TUNNEL TOKEN-----`,
    "",
  ].join("\n");
}

class FakeProcess implements TunnelProcess {
  lines: ((line: string) => void)[] = [];
  exits: ((code: number | null, signal: string | null) => void)[] = [];
  killed = false;
  constructor(
    readonly command: string,
    readonly args: string[],
    readonly env: Record<string, string>,
  ) {}
  onLine(cb: (line: string) => void): void {
    this.lines.push(cb);
  }
  onExit(cb: (code: number | null, signal: string | null) => void): void {
    this.exits.push(cb);
  }
  kill(): Promise<void> {
    this.killed = true;
    return Promise.resolve();
  }
  say(...lines: string[]): void {
    for (const line of lines) for (const cb of this.lines) cb(line);
  }
  die(code: number | null = 1): void {
    for (const cb of this.exits) cb(code, null);
  }
}

class MemoryStore implements RemoteAccessStoreLike {
  #config: RemoteAccessConfig = defaultRemoteConfig();
  #secrets: RemoteSecrets = {};
  #port = DEFAULT_REMOTE_PORT;
  #tunnel: CloudflareTunnelRecord | undefined;
  #ngrok: NgrokAccountRecord = { verified: false, domains: [] };
  config = () => ({ ...this.#config });
  setConfig = (patch: Partial<RemoteAccessConfig>) => (this.#config = { ...this.#config, ...patch });
  lastPort = () => this.#port;
  setLastPort = (port: number) => void (this.#port = port);
  secrets = () => ({ ...this.#secrets });
  setSecrets = (set: RemoteSecrets, clear: readonly (keyof RemoteSecrets)[] = []) => {
    this.#secrets = { ...this.#secrets, ...set };
    for (const name of clear) delete this.#secrets[name];
  };
  seed = () => "abc123";
  cloudflareTunnel = () => this.#tunnel;
  setCloudflareTunnel = (record: CloudflareTunnelRecord | undefined) => void (this.#tunnel = record);
  ngrokAccount = () => ({ ...this.#ngrok });
  setNgrokAccount = (record: NgrokAccountRecord | undefined) => void (this.#ngrok = record ?? { verified: false, domains: [] });
}

/** A scripted short-lived command (`cloudflared tunnel create`, an ngrok test tunnel…). */
interface Script {
  match(args: string[]): boolean;
  lines: string[];
  /** Exit after speaking; false keeps it running until the test ends it. */
  exit?: number | false;
  run?(): void;
}

function setup(options: { installed?: boolean; server?: boolean; enablePort?: number | Error; loggedIn?: boolean } = {}) {
  const store = new MemoryStore();
  const procs: FakeProcess[] = [];
  const runs: { command: string; args: string[]; proc: FakeProcess }[] = [];
  const enables: number[] = [];
  const disables: number[] = [];
  const disk = new Map<string, string>();
  if (options.loggedIn) disk.set(CERT, certPem());
  const files: TunnelFiles = {
    read: (file) => disk.get(file) ?? null,
    write: (file, text) => void disk.set(file, text),
    remove: (file) => void disk.delete(file),
    exists: (file) => disk.has(file),
  };
  let installed = options.installed ?? true;
  let serverUp = options.server ?? true;
  let enablePort: number | Error = options.enablePort ?? DEFAULT_REMOTE_PORT;
  let localMode = true;
  const net = { zone: "example.com" as string | Error, domains: [] as string[] | Error };
  const fetchJson = vi.fn<(url: string, headers?: Record<string, string>) => Promise<unknown>>().mockImplementation((url) => {
    if (url.startsWith("https://api.cloudflare.com/")) {
      return net.zone instanceof Error ? Promise.reject(net.zone) : Promise.resolve({ result: { name: net.zone } });
    }
    if (url.startsWith("https://api.ngrok.com/reserved_domains")) {
      return net.domains instanceof Error ? Promise.reject(net.domains) : Promise.resolve({ reserved_domains: net.domains.map((domain) => ({ domain })) });
    }
    return Promise.reject(new Error("down"));
  });
  const scripts: Script[] = [
    {
      match: (a) => a[0] === "tunnel" && a[1] === "create",
      lines: [`Tunnel credentials written to ${CREDENTIALS}. cloudflared chose this file based on where your origin certificate was found.`, `Created tunnel tasktrooper-abc123 with id ${TUNNEL_ID}`],
      run: () => void disk.set(CREDENTIALS, "{}"),
    },
    { match: (a) => a[0] === "tunnel" && a[1] === "route", lines: ["Added CNAME phone.example.com which will route to this tunnel tunnelID=" + TUNNEL_ID] },
    { match: (a) => a[0] === "http" && a[1] === "127.0.0.1:9", lines: ['{"lvl":"info","msg":"started tunnel","url":"https://probe.ngrok-free.app"}'], exit: false },
  ];
  const manager = new TunnelManager({
    store,
    detect: (provider: RemoteProvider): Promise<RemoteBinaryDetection> =>
      Promise.resolve(installed ? { provider, installed: true, path: `/bin/${provider}`, version: "1.0.0" } : { provider, installed: false, detail: "missing", installCommand: "brew install x" }),
    spawn: (command, args, env) => {
      const proc = new FakeProcess(command, args, env);
      const script = args[0] === "tunnel" && args[1] === "login" ? undefined : scripts.find((candidate) => candidate.match(args));
      if (script) {
        runs.push({ command, args, proc });
        queueMicrotask(() => {
          script.run?.();
          proc.say(...script.lines);
          if (script.exit !== false) proc.die(script.exit ?? 0);
        });
      } else if (args[0] === "tunnel" && (args[1] === "create" || args[1] === "route")) {
        runs.push({ command, args, proc });
      } else procs.push(proc);
      return proc;
    },
    listener: {
      enable: (_server, request) => {
        enables.push(request.preferredPort);
        return enablePort instanceof Error ? Promise.reject(enablePort) : Promise.resolve(enablePort);
      },
      disable: () => {
        disables.push(1);
        return Promise.resolve();
      },
    },
    server: () => (serverUp ? { base: "http://127.0.0.1:5555", token: "api-token" } : null),
    fetchJson,
    files,
    homeDir: HOME,
    dataDir: "/data",
    env: {},
    isLocalMode: () => localMode,
  });
  return {
    store,
    manager,
    procs,
    runs,
    scripts,
    disk,
    net,
    enables,
    disables,
    fetchJson,
    set: {
      installed: (v: boolean) => (installed = v),
      server: (v: boolean) => (serverUp = v),
      enablePort: (v: number | Error) => (enablePort = v),
      localMode: (v: boolean) => (localMode = v),
    },
    last: () => procs[procs.length - 1] as FakeProcess,
    commands: () => runs.map((r) => r.args.join(" ")),
  };
}

beforeEach(() => {
  vi.useFakeTimers();
});
afterEach(() => {
  vi.useRealTimers();
});

describe("a quick Cloudflare tunnel", () => {
  it("opens the server's listener, runs cloudflared at that port and reports the URL once registered", async () => {
    const t = setup({ enablePort: 47999 });
    await t.manager.start();
    expect(t.enables).toEqual([DEFAULT_REMOTE_PORT]);
    expect(t.last().args).toEqual(["tunnel", "--no-autoupdate", "--url", "http://127.0.0.1:47999"]);
    expect(t.store.lastPort()).toBe(47999);
    expect(t.manager.snapshot()).toMatchObject({ tunnel: "starting", wanted: true, remotePort: 47999 });

    t.last().say("INF |  https://calm-fox-12.trycloudflare.com  |");
    expect(t.manager.snapshot()).toMatchObject({ tunnel: "starting", url: "https://calm-fox-12.trycloudflare.com" });
    t.last().say("INF Registered tunnel connection connIndex=0");
    expect(t.manager.snapshot()).toMatchObject({ tunnel: "connected", url: "https://calm-fox-12.trycloudflare.com" });
  });

  it("goes to reconnecting when the connection drops and back to connected", async () => {
    const t = setup();
    await t.manager.start();
    t.last().say("INF |  https://a-b.trycloudflare.com  |", "INF Registered tunnel connection connIndex=0");
    t.last().say("WRN Retrying connection in up to 4s connIndex=0");
    expect(t.manager.snapshot().tunnel).toBe("reconnecting");
    t.last().say("INF Registered tunnel connection connIndex=0");
    expect(t.manager.snapshot().tunnel).toBe("connected");
  });

  it("stop kills the process, closes the listener and forgets the URL", async () => {
    const t = setup();
    await t.manager.start();
    t.last().say("INF |  https://a-b.trycloudflare.com  |", "INF Registered tunnel connection connIndex=0");
    const proc = t.last();
    const state = await t.manager.stop();
    expect(proc.killed).toBe(true);
    expect(t.disables).toHaveLength(1);
    expect(state).toMatchObject({ tunnel: "stopped", wanted: false });
    expect(state.url).toBeUndefined();
  });
});

describe("a named Cloudflare tunnel", () => {
  it("needs a Cloudflare login, then a subdomain, before it runs anything", async () => {
    const t = setup();
    await t.manager.configure({ mode: "named" });
    expect(await t.manager.start()).toMatchObject({ tunnel: "error", problem: "needs_login", cloudflare: { loggedIn: false } });
    expect(t.procs).toHaveLength(0);
    expect(t.runs).toHaveLength(0);

    t.disk.set(CERT, certPem());
    await t.manager.stop();
    expect(await t.manager.start()).toMatchObject({ tunnel: "error", problem: "needs_hostname", cloudflare: { loggedIn: true, domain: "example.com" } });
    expect(t.runs).toHaveLength(0);
  });

  it("refuses a hostname outside the domain the person logged in with", async () => {
    const t = setup({ loggedIn: true });
    await t.manager.configure({ mode: "named", hostname: "phone.other.org" });
    const state = await t.manager.start();
    expect(state).toMatchObject({ tunnel: "error", problem: "needs_hostname" });
    expect(state.error).toContain(".example.com");
    expect(t.runs).toHaveLength(0);
  });

  it("creates the tunnel, routes the hostname, writes the ingress and runs it by config file", async () => {
    const t = setup({ loggedIn: true, enablePort: 47999 });
    await t.manager.configure({ mode: "named", hostname: "phone.example.com" });
    await t.manager.start();
    expect(t.commands()).toEqual([`tunnel create tasktrooper-abc123`, `tunnel route dns ${TUNNEL_ID} phone.example.com`]);

    const config = t.disk.get("/data/cloudflared/config.yml") ?? "";
    expect(config).toContain(`tunnel: ${TUNNEL_ID}`);
    expect(config).toContain(`credentials-file: "${CREDENTIALS}"`);
    expect(config).toContain("- hostname: phone.example.com");
    expect(config).toContain("service: http://127.0.0.1:47999");
    expect(config).toContain("service: http_status:404");

    const proc = t.last();
    expect(proc.args).toEqual(["tunnel", "--no-autoupdate", "--config", "/data/cloudflared/config.yml", "run", TUNNEL_ID]);
    expect(proc.env).toEqual({});
    expect(t.manager.snapshot()).toMatchObject({ tunnel: "starting", url: "https://phone.example.com" });
    proc.say("INF Registered tunnel connection connIndex=0");
    expect(t.manager.snapshot()).toMatchObject({ tunnel: "connected", url: "https://phone.example.com" });
    expect(t.store.cloudflareTunnel()).toMatchObject({ tunnelId: TUNNEL_ID, zoneId: "zone-1", routedHostname: "phone.example.com" });
  });

  it("reuses the tunnel and the DNS route on the next start, and only routes a new subdomain", async () => {
    const t = setup({ loggedIn: true });
    await t.manager.configure({ mode: "named", hostname: "phone.example.com" });
    await t.manager.start();
    await t.manager.stop();
    await t.manager.start();
    expect(t.commands()).toHaveLength(2);

    t.scripts.unshift({ match: (a) => a[1] === "route", lines: [`Added CNAME second.example.com which will route to this tunnel tunnelID=${TUNNEL_ID}`] });
    await t.manager.configure({ hostname: "second.example.com" });
    await t.manager.start();
    expect(t.commands().slice(2)).toEqual([`tunnel route dns ${TUNNEL_ID} second.example.com`]);
  });

  it("makes a new tunnel when the login is for another zone", async () => {
    const t = setup({ loggedIn: true });
    await t.manager.configure({ mode: "named", hostname: "phone.example.com" });
    await t.manager.start();
    await t.manager.stop();
    t.disk.set(CERT, certPem("zone-2"));
    t.disk.delete(CREDENTIALS);
    await t.manager.start();
    expect(t.commands().filter((c) => c.startsWith("tunnel create"))).toHaveLength(2);
  });

  it("does not take over a DNS name that is already in use", async () => {
    const t = setup({ loggedIn: true });
    t.scripts.unshift({
      match: (a) => a[1] === "route",
      lines: ["Failed to add route: code: 1003, reason: Failed to create record phone.example.com with err An A, AAAA, or CNAME record with that host already exists."],
      exit: 1,
    });
    await t.manager.configure({ mode: "named", hostname: "phone.example.com" });
    const state = await t.manager.start();
    expect(state).toMatchObject({ tunnel: "error", problem: "needs_hostname" });
    expect(state.error).toContain("already has a DNS record");
    expect(t.procs).toHaveLength(0);
  });

  it("stops when cloudflared routed some other name than the one asked for", async () => {
    const t = setup({ loggedIn: true });
    t.scripts.unshift({ match: (a) => a[1] === "route", lines: ["Added CNAME phone.example.com.other.org which will route to this tunnel"] });
    await t.manager.configure({ mode: "named", hostname: "phone.example.com" });
    const state = await t.manager.start();
    expect(state).toMatchObject({ tunnel: "error", problem: "needs_hostname" });
    expect(t.procs).toHaveLength(0);
  });

  it("explains a failure to create the tunnel instead of starting", async () => {
    const t = setup({ loggedIn: true });
    t.scripts.unshift({ match: (a) => a[1] === "create", lines: ["Tunnel create failed: Unauthorized: token expired"], exit: 1 });
    await t.manager.configure({ mode: "named", hostname: "phone.example.com" });
    const state = await t.manager.start();
    expect(state).toMatchObject({ tunnel: "error", problem: "tunnel_setup_failed" });
    expect(state.error).toContain("Could not create the tunnel");
  });

  it("still starts when Cloudflare would not tell the domain, and shows why", async () => {
    const t = setup({ loggedIn: true });
    t.net.zone = new HttpStatusError(403);
    await t.manager.configure({ mode: "named", hostname: "phone.example.com" });
    await t.manager.start();
    expect(t.manager.snapshot().cloudflare).toMatchObject({ loggedIn: true, domainError: expect.stringContaining("HTTP 403") });
    expect(t.manager.snapshot().cloudflare.domain).toBeUndefined();
    expect(t.last().args).toContain("run");
  });

  it("stops for good when Cloudflare no longer accepts the tunnel", async () => {
    const t = setup({ loggedIn: true });
    await t.manager.configure({ mode: "named", hostname: "phone.example.com" });
    await t.manager.start();
    const proc = t.last();
    proc.say("ERR Unauthorized: Invalid tunnel secret");
    proc.die(1);
    await vi.advanceTimersByTimeAsync(120_000);
    expect(t.procs).toHaveLength(1);
    expect(t.manager.snapshot()).toMatchObject({ tunnel: "error", problem: "auth_failed" });
  });
});

describe("the Cloudflare login", () => {
  const LOGIN = [
    "Please open the following URL and log in with your Cloudflare account:",
    "",
    "https://dash.cloudflare.com/argotunnel?aud=&callback=https%3A%2F%2Flogin.cloudflareaccess.org%2Fabc",
    "Leave cloudflared running to download the cert automatically.",
  ];

  it("runs `cloudflared tunnel login`, shows the link, and reads the domain once the certificate is there", async () => {
    const t = setup();
    const state = await t.manager.cloudflareLogin();
    expect(state.cloudflare).toMatchObject({ phase: "waiting", loggedIn: false });
    const login = t.last();
    expect(login.command).toBe("/bin/cloudflare");
    expect(login.args).toEqual(["tunnel", "login"]);

    login.say(...LOGIN);
    expect(t.manager.snapshot().cloudflare.loginUrl).toMatch(/^https:\/\/dash\.cloudflare\.com\/argotunnel\?aud=/);

    t.disk.set(CERT, certPem());
    login.say("You have successfully logged in.");
    login.die(0);
    await vi.advanceTimersByTimeAsync(0);
    expect(t.manager.snapshot().cloudflare).toMatchObject({ phase: "idle", loggedIn: true, domain: "example.com", suggestedSubdomain: "tasktrooper-abc123" });
    expect(t.manager.snapshot().cloudflare.loginUrl).toBeUndefined();
    expect(JSON.stringify(t.manager.snapshot())).not.toContain(CF_API_TOKEN);
  });

  it("asks Cloudflare for the zone's name with the certificate's token, as a bearer", async () => {
    const t = setup({ loggedIn: true });
    await t.manager.refreshAccount();
    expect(t.fetchJson).toHaveBeenCalledWith("https://api.cloudflare.com/client/v4/zones/zone-1", expect.objectContaining({ Authorization: `Bearer ${CF_API_TOKEN}` }));
    await t.manager.refreshAccount();
    expect(t.fetchJson).toHaveBeenCalledTimes(1);
  });

  it("reports a login that ended without a certificate, with what cloudflared said", async () => {
    const t = setup();
    await t.manager.cloudflareLogin();
    t.last().say("ERR failed to fetch resource: context deadline exceeded");
    t.last().die(1);
    await vi.advanceTimersByTimeAsync(0);
    expect(t.manager.snapshot().cloudflare).toMatchObject({ phase: "failed", loggedIn: false, error: expect.stringContaining("deadline exceeded") });
  });

  it("gives up when nobody finishes the login in the browser", async () => {
    const t = setup();
    await t.manager.cloudflareLogin();
    await vi.advanceTimersByTimeAsync(11 * 60_000);
    expect(t.last().killed).toBe(true);
    expect(t.manager.snapshot().cloudflare).toMatchObject({ phase: "failed", error: expect.stringContaining("Try again") });
  });

  it("can be cancelled", async () => {
    const t = setup();
    await t.manager.cloudflareLogin();
    const state = await t.manager.cloudflareLoginCancel();
    expect(t.last().killed).toBe(true);
    expect(state.cloudflare).toMatchObject({ phase: "idle", loggedIn: false });
  });

  it("needs cloudflared installed", async () => {
    const t = setup({ installed: false });
    const state = await t.manager.cloudflareLogin();
    expect(state.cloudflare).toMatchObject({ phase: "failed" });
    expect(state.cloudflare.error).toContain("Install it");
    expect(t.procs).toHaveLength(0);
  });

  it("does not start a second login while one is open, nor when already logged in", async () => {
    const t = setup();
    await t.manager.cloudflareLogin();
    await t.manager.cloudflareLogin();
    expect(t.procs).toHaveLength(1);
    const loggedIn = setup({ loggedIn: true });
    expect((await loggedIn.manager.cloudflareLogin()).cloudflare.loggedIn).toBe(true);
    expect(loggedIn.procs).toHaveLength(0);
  });

  it("logs out by deleting the certificate, but not while the tunnel runs on it", async () => {
    const t = setup({ loggedIn: true });
    await t.manager.configure({ mode: "named", hostname: "phone.example.com" });
    await t.manager.start();
    await expect(t.manager.cloudflareLogout()).rejects.toThrow(/Stop the tunnel/);
    expect(t.disk.has(CERT)).toBe(true);

    await t.manager.stop();
    const state = await t.manager.cloudflareLogout();
    expect(t.disk.has(CERT)).toBe(false);
    expect(state.cloudflare).toMatchObject({ loggedIn: false });
    expect(state.cloudflare.domain).toBeUndefined();
  });

  it("treats a certificate file that is not a login as no login, and clears it before logging in again", async () => {
    const t = setup();
    t.disk.set(CERT, "garbage");
    await t.manager.refreshAccount();
    expect(t.manager.snapshot().cloudflare.loggedIn).toBe(false);
    await t.manager.cloudflareLogin();
    expect(t.disk.has(CERT)).toBe(false);
    expect(t.last().args).toEqual(["tunnel", "login"]);
  });
});

describe("ngrok", () => {
  it("requires an authtoken, runs ngrok with it in the environment and takes the URL from the log", async () => {
    const t = setup();
    await t.manager.configure({ provider: "ngrok" });
    expect(await t.manager.start()).toMatchObject({ tunnel: "error", problem: "needs_token" });

    t.store.setSecrets({ ngrokAuthtoken: AUTHTOKEN });
    await t.manager.configure({ serverName: "My Mac" });
    const proc = t.last();
    expect(proc.args).toEqual(["http", `127.0.0.1:${DEFAULT_REMOTE_PORT}`, "--log=stdout", "--log-format=json"]);
    expect(proc.env).toEqual({ NGROK_AUTHTOKEN: AUTHTOKEN });
    proc.say('{"lvl":"info","msg":"started tunnel","url":"https://odd-owl-1.ngrok-free.app"}');
    expect(t.manager.snapshot()).toMatchObject({ tunnel: "connected", url: "https://odd-owl-1.ngrok-free.app" });
  });

  it("serves a chosen domain of the account with --url", async () => {
    const t = setup();
    t.store.setSecrets({ ngrokAuthtoken: AUTHTOKEN });
    await t.manager.configure({ provider: "ngrok", mode: "named", hostname: "mine.ngrok.app" });
    await t.manager.start();
    expect(t.last().args).toContain("--url=https://mine.ngrok.app");
  });

  it("falls back to ngrok's local API on the address the log announced", async () => {
    const t = setup();
    t.store.setSecrets({ ngrokAuthtoken: AUTHTOKEN });
    await t.manager.configure({ provider: "ngrok" });
    await t.manager.start();
    t.last().say('{"lvl":"info","msg":"starting web service","addr":"127.0.0.1:4041"}');
    t.fetchJson.mockResolvedValue({
      tunnels: [{ public_url: "https://api-found.ngrok-free.app", config: { addr: `http://127.0.0.1:${DEFAULT_REMOTE_PORT}` } }],
    });
    await vi.advanceTimersByTimeAsync(1_100);
    expect(t.fetchJson).toHaveBeenCalledWith("http://127.0.0.1:4041/api/tunnels");
    expect(t.manager.snapshot()).toMatchObject({ tunnel: "connected", url: "https://api-found.ngrok-free.app" });
  });

  it("reports ERR_NGROK_4018 as an authtoken problem, without restarting", async () => {
    const t = setup();
    t.store.setSecrets({ ngrokAuthtoken: AUTHTOKEN });
    await t.manager.configure({ provider: "ngrok" });
    await t.manager.start();
    t.last().say('{"err":"authentication failed: ERR_NGROK_4018","lvl":"eror","msg":"command failed"}');
    t.last().die(1);
    await vi.advanceTimersByTimeAsync(60_000);
    expect(t.procs).toHaveLength(1);
    expect(t.manager.snapshot()).toMatchObject({ tunnel: "error", problem: "auth_failed" });
  });
});

describe("connecting an ngrok account", () => {
  it("proves the authtoken with a test tunnel before storing it, and stops the test tunnel", async () => {
    const t = setup();
    await t.manager.configure({ provider: "ngrok" });
    const state = await t.manager.ngrokConnect({ authtoken: AUTHTOKEN });
    expect(t.runs[0]?.args).toEqual(["http", "127.0.0.1:9", "--log=stdout", "--log-format=json"]);
    expect(t.runs[0]?.proc.env).toEqual({ NGROK_AUTHTOKEN: AUTHTOKEN });
    expect(t.runs[0]?.proc.killed).toBe(true);
    expect(t.store.secrets().ngrokAuthtoken).toBe(AUTHTOKEN);
    expect(state.ngrok).toMatchObject({ verified: true, phase: "idle" });
    expect(state.secrets).toEqual({ ngrokAuthtoken: true, ngrokApiKey: false });
    expect(JSON.stringify(state)).not.toContain(AUTHTOKEN);
  });

  it("does not store an authtoken ngrok refuses, and says why", async () => {
    const t = setup();
    t.scripts.unshift({ match: (a) => a[0] === "http", lines: ['{"err":"authentication failed: ERR_NGROK_105","lvl":"eror","msg":"command failed"}'], exit: 1 });
    const state = await t.manager.ngrokConnect({ authtoken: AUTHTOKEN });
    expect(t.store.secrets()).toEqual({});
    expect(state.ngrok).toMatchObject({ verified: false, phase: "idle", error: expect.stringContaining("does not accept this authtoken") });
  });

  it("gives up on a test tunnel that never answers", async () => {
    const t = setup();
    t.scripts.unshift({ match: (a) => a[0] === "http", lines: [], exit: false });
    const pending = t.manager.ngrokConnect({ authtoken: AUTHTOKEN });
    await vi.advanceTimersByTimeAsync(30_000);
    const state = await pending;
    expect(state.ngrok.error).toContain("did not answer in time");
    expect(t.store.secrets()).toEqual({});
  });

  it("needs ngrok installed", async () => {
    const t = setup({ installed: false });
    const state = await t.manager.ngrokConnect({ authtoken: AUTHTOKEN });
    expect(state.ngrok.error).toContain("Install it");
    expect(t.runs).toHaveLength(0);
  });

  it("lists the account's domains with an API key, keeping the key only when ngrok accepts it", async () => {
    const t = setup();
    await t.manager.ngrokConnect({ authtoken: AUTHTOKEN });
    t.net.domains = new HttpStatusError(401);
    let state = await t.manager.ngrokConnect({ apiKey: "bad-key" });
    expect(state.ngrok.apiKeyError).toContain("did not accept that API key");
    expect(t.store.secrets().ngrokApiKey).toBeUndefined();

    t.net.domains = ["b.ngrok.app", "a.ngrok-free.app"];
    state = await t.manager.ngrokConnect({ apiKey: "good-key" });
    expect(state.ngrok).toMatchObject({ verified: true, domains: ["a.ngrok-free.app", "b.ngrok.app"] });
    expect(state.ngrok.apiKeyError).toBeUndefined();
    expect(t.fetchJson).toHaveBeenCalledWith(expect.stringContaining("https://api.ngrok.com/reserved_domains"), expect.objectContaining({ Authorization: "Bearer good-key", "Ngrok-Version": "2" }));
    expect(state.secrets.ngrokApiKey).toBe(true);
    expect(JSON.stringify(state)).not.toContain("good-key");

    t.net.domains = ["c.ngrok.app"];
    state = await t.manager.ngrokConnect({});
    expect(state.ngrok.domains).toEqual(["c.ngrok.app"]);
  });

  it("refuses to change the account while the ngrok tunnel is running, and can disconnect when stopped", async () => {
    const t = setup();
    await t.manager.ngrokConnect({ authtoken: AUTHTOKEN, apiKey: "good-key" });
    await t.manager.configure({ provider: "ngrok" });
    await t.manager.start();
    await expect(t.manager.ngrokConnect({ apiKey: "x" })).rejects.toThrow(/Stop the tunnel/);
    await expect(t.manager.ngrokDisconnect()).rejects.toThrow(/Stop the tunnel/);
    await t.manager.stop();
    const state = await t.manager.ngrokDisconnect();
    expect(state.secrets).toEqual({ ngrokAuthtoken: false, ngrokApiKey: false });
    expect(state.ngrok).toMatchObject({ verified: false, domains: [] });
  });
});

describe("supervision", () => {
  it("restarts a crashed tunnel with a doubling, capped backoff", async () => {
    const t = setup();
    await t.manager.start();
    const waits: number[] = [];
    for (let i = 0; i < 7; i += 1) {
      const before = t.procs.length;
      t.last().die(1);
      await vi.advanceTimersByTimeAsync(0);
      expect(t.manager.snapshot().tunnel).toBe("reconnecting");
      let waited = 0;
      while (t.procs.length === before && waited < 60_000) {
        await vi.advanceTimersByTimeAsync(500);
        waited += 500;
      }
      waits.push(waited);
    }
    expect(waits[0]).toBeLessThanOrEqual(1_000);
    expect(waits[1]).toBeLessThanOrEqual(2_000);
    expect(waits[1]).toBeGreaterThan(waits[0] as number);
    expect(waits[2]).toBeLessThanOrEqual(4_000);
    expect(Math.max(...waits)).toBeLessThanOrEqual(30_000);
    expect(t.manager.snapshot().restarts).toBe(7);
  });

  it("does not restart after Stop", async () => {
    const t = setup();
    await t.manager.start();
    t.last().die(1);
    await vi.advanceTimersByTimeAsync(0);
    await t.manager.stop();
    await vi.advanceTimersByTimeAsync(120_000);
    expect(t.procs).toHaveLength(1);
    expect(t.manager.snapshot().tunnel).toBe("stopped");
  });

  it("reports a missing program without spawning anything, and recovers after it is installed", async () => {
    const t = setup({ installed: false });
    const state = await t.manager.start();
    expect(state).toMatchObject({ tunnel: "binary_missing", detection: { installed: false, installCommand: "brew install x" } });
    expect(t.procs).toHaveLength(0);
    t.set.installed(true);
    await t.manager.start();
    expect(t.procs).toHaveLength(1);
  });

  it("waits for the server, then starts when it appears", async () => {
    const t = setup({ server: false });
    await t.manager.start();
    expect(t.manager.snapshot()).toMatchObject({ tunnel: "starting", problem: "server_unavailable" });
    expect(t.procs).toHaveLength(0);
    t.set.server(true);
    await t.manager.onServer();
    expect(t.procs).toHaveLength(1);
  });

  it("keeps a running tunnel across a server restart on the same port, and restarts it when the port moved", async () => {
    const t = setup();
    await t.manager.start();
    await t.manager.onServer();
    expect(t.procs).toHaveLength(1);
    expect(t.enables).toHaveLength(2);

    t.set.enablePort(48123);
    await t.manager.onServer();
    expect(t.procs).toHaveLength(2);
    expect(t.last().args).toContain("http://127.0.0.1:48123");
    expect(t.procs[0]?.killed).toBe(true);
  });

  it("surfaces a server that refuses to open the listener", async () => {
    const t = setup({ enablePort: new Error("the local server answered 404 to /v1/remote-access/enable") });
    const state = await t.manager.start();
    expect(state).toMatchObject({ tunnel: "error", problem: "enable_failed" });
    expect(t.procs).toHaveLength(0);
  });

  it("restarts when the account or provider settings change while running, not when only auto-start does", async () => {
    const t = setup();
    await t.manager.start();
    await t.manager.configure({ autoStart: true });
    expect(t.procs).toHaveLength(1);
    t.store.setSecrets({ ngrokAuthtoken: AUTHTOKEN });
    await t.manager.configure({ provider: "ngrok" });
    expect(t.procs).toHaveLength(2);
    expect(t.last().command).toBe("/bin/ngrok");
  });
});

describe("auto-start", () => {
  it("starts at launch only when switched on and local", async () => {
    const off = setup();
    await off.manager.init();
    expect(off.procs).toHaveLength(0);

    const on = setup();
    await on.manager.configure({ autoStart: true });
    await on.manager.init();
    expect(on.procs).toHaveLength(1);

    const account = setup();
    await account.manager.configure({ autoStart: true });
    account.set.localMode(false);
    await account.manager.init();
    expect(account.procs).toHaveLength(0);
    await expect(account.manager.start()).rejects.toThrow(/locally/);
  });

  it("shutdown stops the process without asking the server to close the listener", async () => {
    const t = setup();
    await t.manager.start();
    await t.manager.shutdown();
    expect(t.last().killed).toBe(true);
    expect(t.disables).toHaveLength(0);
    expect(t.manager.snapshot().tunnel).toBe("stopped");
  });
});

describe("secrets", () => {
  it("never shows a secret in state or logs, only whether one is stored", async () => {
    const t = setup({ loggedIn: true });
    t.store.setSecrets({ ngrokAuthtoken: AUTHTOKEN });
    await t.manager.configure({ mode: "named", hostname: "phone.example.com" });
    await t.manager.start();
    t.last().say(`ERR failed to run with ${CF_API_TOKEN}`, `connecting with ${AUTHTOKEN}`);
    await vi.advanceTimersByTimeAsync(300);
    const everything = JSON.stringify([t.manager.snapshot(), t.manager.logs()]);
    expect(everything).not.toContain(CF_API_TOKEN);
    expect(everything).not.toContain(AUTHTOKEN);
    expect(t.manager.snapshot().secrets).toEqual({ ngrokAuthtoken: true, ngrokApiKey: false });
  });

  it("pushes state changes to subscribers", async () => {
    const t = setup();
    const seen: string[] = [];
    t.manager.on("state", (s) => seen.push(s.tunnel));
    await t.manager.start();
    t.last().say("INF |  https://a-b.trycloudflare.com  |", "INF Registered tunnel connection connIndex=0");
    expect(seen).toContain("starting");
    expect(seen[seen.length - 1]).toBe("connected");
  });
});
