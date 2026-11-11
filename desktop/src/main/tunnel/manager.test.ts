import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { RemoteAccessConfig, RemoteBinaryDetection, RemoteProvider } from "../../ipc/types.js";
import { TunnelManager } from "./manager.js";
import type { TunnelProcess } from "./process.js";
import { defaultRemoteConfig, DEFAULT_REMOTE_PORT, type RemoteAccessStoreLike, type RemoteSecrets } from "./store.js";

const TOKEN = "eyJhIjoiMDEyMzQ1Njc4OWFiY2RlZiIsInQiOiJ0b2tlbi10b2tlbi10b2tlbiIsInMiOiJzZWNyZXQifQ==";
const AUTHTOKEN = "2abcDEF1234ghi_5JkLmNoPqRsTuVwXyZ12";

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
  config = () => ({ ...this.#config });
  setConfig = (patch: Partial<RemoteAccessConfig>) => (this.#config = { ...this.#config, ...patch });
  lastPort = () => this.#port;
  setLastPort = (port: number) => void (this.#port = port);
  secrets = () => ({ ...this.#secrets });
  setSecrets = (set: RemoteSecrets, clear: readonly (keyof RemoteSecrets)[] = []) => {
    this.#secrets = { ...this.#secrets, ...set };
    for (const name of clear) delete this.#secrets[name];
  };
}

function setup(options: { installed?: boolean; server?: boolean; enablePort?: number | Error } = {}) {
  const store = new MemoryStore();
  const procs: FakeProcess[] = [];
  const enables: number[] = [];
  const disables: number[] = [];
  let installed = options.installed ?? true;
  let serverUp = options.server ?? true;
  let enablePort: number | Error = options.enablePort ?? DEFAULT_REMOTE_PORT;
  let localMode = true;
  const fetchJson = vi.fn<(url: string) => Promise<unknown>>().mockRejectedValue(new Error("down"));
  const manager = new TunnelManager({
    store,
    detect: (provider: RemoteProvider): Promise<RemoteBinaryDetection> =>
      Promise.resolve(installed ? { provider, installed: true, path: `/bin/${provider}`, version: "1.0.0" } : { provider, installed: false, detail: "missing", installCommand: "brew install x" }),
    spawn: (command, args, env) => {
      const proc = new FakeProcess(command, args, env);
      procs.push(proc);
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
    isLocalMode: () => localMode,
  });
  return {
    store,
    manager,
    procs,
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
  it("needs a token and a hostname before it runs anything", async () => {
    const t = setup();
    await t.manager.configure({ mode: "named" });
    const state = await t.manager.start();
    expect(state).toMatchObject({ tunnel: "error", problem: "needs_token" });
    expect(t.procs).toHaveLength(0);
    await t.manager.configure({ cloudflareToken: TOKEN });
    expect(t.manager.snapshot().problem).toBe("needs_hostname");
  });

  it("puts the token in the environment only, knows its URL from the hostname and connects on registration", async () => {
    const t = setup();
    await t.manager.configure({ mode: "named", hostname: "phone.example.com", cloudflareToken: TOKEN });
    await t.manager.start();
    const proc = t.last();
    expect(proc.args.join(" ")).not.toContain(TOKEN);
    expect(proc.env).toEqual({ TUNNEL_TOKEN: TOKEN });
    expect(t.manager.snapshot()).toMatchObject({ tunnel: "starting", url: "https://phone.example.com" });
    proc.say("INF Registered tunnel connection connIndex=0");
    expect(t.manager.snapshot()).toMatchObject({ tunnel: "connected", url: "https://phone.example.com" });
  });

  it("stops for good on a rejected token instead of restarting it", async () => {
    const t = setup();
    await t.manager.configure({ mode: "named", hostname: "phone.example.com", cloudflareToken: TOKEN });
    await t.manager.start();
    const proc = t.last();
    proc.say("ERR Provided Tunnel token is not valid.");
    proc.die(1);
    await vi.advanceTimersByTimeAsync(120_000);
    expect(t.procs).toHaveLength(1);
    expect(t.manager.snapshot()).toMatchObject({ tunnel: "error", problem: "auth_failed" });
  });
});

describe("ngrok", () => {
  it("requires an authtoken, runs ngrok with it in the environment and takes the URL from the log", async () => {
    const t = setup();
    await t.manager.configure({ provider: "ngrok" });
    expect(await t.manager.start()).toMatchObject({ tunnel: "error", problem: "needs_token" });

    await t.manager.configure({ ngrokAuthtoken: AUTHTOKEN });
    const proc = t.last();
    expect(proc.args).toEqual(["http", `127.0.0.1:${DEFAULT_REMOTE_PORT}`, "--log=stdout", "--log-format=json"]);
    expect(proc.env).toEqual({ NGROK_AUTHTOKEN: AUTHTOKEN });
    proc.say('{"lvl":"info","msg":"started tunnel","url":"https://odd-owl-1.ngrok-free.app"}');
    expect(t.manager.snapshot()).toMatchObject({ tunnel: "connected", url: "https://odd-owl-1.ngrok-free.app" });
  });

  it("falls back to ngrok's local API on the address the log announced", async () => {
    const t = setup();
    await t.manager.configure({ provider: "ngrok", ngrokAuthtoken: AUTHTOKEN });
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
    await t.manager.configure({ provider: "ngrok", ngrokAuthtoken: AUTHTOKEN });
    await t.manager.start();
    t.last().say('{"err":"authentication failed: ERR_NGROK_4018","lvl":"eror","msg":"command failed"}');
    t.last().die(1);
    await vi.advanceTimersByTimeAsync(60_000);
    expect(t.procs).toHaveLength(1);
    expect(t.manager.snapshot()).toMatchObject({ tunnel: "error", problem: "auth_failed" });
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
    await t.manager.configure({ provider: "ngrok", ngrokAuthtoken: AUTHTOKEN });
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
    const t = setup();
    await t.manager.configure({ mode: "named", hostname: "phone.example.com", cloudflareToken: TOKEN });
    await t.manager.start();
    t.last().say(`ERR failed to run with token ${TOKEN}`, `connecting with ${AUTHTOKEN}`);
    await vi.advanceTimersByTimeAsync(300);
    const everything = JSON.stringify([t.manager.snapshot(), t.manager.logs()]);
    expect(everything).not.toContain(TOKEN);
    expect(everything).not.toContain(AUTHTOKEN);
    expect(t.manager.snapshot().secrets).toEqual({ cloudflareToken: true, ngrokAuthtoken: false });
    await t.manager.configure({ clearSecrets: ["cloudflareToken"] });
    expect(t.manager.snapshot().secrets.cloudflareToken).toBe(false);
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
