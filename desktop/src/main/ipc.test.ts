import { describe, expect, it, vi } from "vitest";
import type { KeySetRequest } from "../ipc/channels.js";
import type { ProviderKeysState } from "../ipc/types.js";

type Handler = (event: unknown, payload?: unknown) => Promise<unknown>;

const ipc = vi.hoisted(() => ({ handlers: new Map<string, Handler>() }));

vi.mock("electron", () => ({
  ipcMain: {
    handle: (channel: string, fn: Handler) => ipc.handlers.set(channel, fn),
    on: () => undefined,
  },
  app: { getAppPath: () => "/app", isPackaged: false },
  net: { fetch: () => Promise.resolve(new Response("")) },
  protocol: { registerSchemesAsPrivileged: () => {}, handle: () => {} },
}));

const { registerIpc } = await import("./ipc.js");
const { isOwnPage, isTrustedFrame } = await import("./sender-guard.js");
const { CLOUD_CHANNELS, KEYS_CHANNELS } = await import("../ipc/channels.js");

const ACCOUNT = "https://app.tasktrooper.ai";
const KEYS_PAGE = "file:///app/dist/renderer/keys.html";
const SECRET = "sk-ant-NEVER-CROSSES-BACK-42";

function contents(processId: number) {
  return { isDestroyed: () => false, mainFrame: { processId, routingId: 1 } };
}

const keysWindow = contents(1);
const accountView = contents(2);
const chrome = contents(3);

function from(sender: ReturnType<typeof contents>, url: string) {
  return { sender, senderFrame: { processId: sender.mainFrame.processId, routingId: 1, url } };
}

/**
 * The real guards, over the three WebContents a running app has: the key
 * window, the web app's view (here on the account's origin), and the chrome.
 */
function wire() {
  ipc.handlers.clear();
  const calls: string[] = [];
  const state = (): ProviderKeysState => ({
    providers: [{ id: "anthropic", type: "anthropic", hasKey: true }],
    runnerRestarting: false,
  });
  const services = new Proxy(
    {
      keysList: () => (calls.push("list"), state()),
      keysSet: (_request: KeySetRequest) => (calls.push("set"), state()),
      keysRemove: (id: string) => (calls.push(`remove ${id}`), state()),
      openKeys: (prefill?: unknown) => void calls.push(prefill ? `openKeys ${JSON.stringify(prefill)}` : "openKeys"),
      keysPrefill: () => (calls.push("prefill"), null),
      accountMode: () => "account",
    },
    {
      get: (target, key: string) =>
        key in target ? target[key as keyof typeof target] : () => { throw new Error(`unexpected ${key}`); },
    },
  );
  registerIpc(services as never, {
    isTrustedCloudSender: (event) => isTrustedFrame(event as never, accountView, ACCOUNT),
    isCloudWebContents: (event) => event.sender === accountView,
    isShellSender: (event) => event.sender === chrome,
    isKeysSender: (event) => isOwnPage(event as never, keysWindow, KEYS_PAGE),
  });
  const invoke = (channel: string, event: unknown, payload?: unknown): Promise<unknown> => {
    const handler = ipc.handlers.get(channel);
    if (!handler) throw new Error(`no handler for ${channel}`);
    return handler(event, payload);
  };
  return { calls, invoke };
}

describe("shell:keys:*", () => {
  it("answers the key window's own page", async () => {
    const { calls, invoke } = wire();
    const reply = await invoke(KEYS_CHANNELS.set, from(keysWindow, KEYS_PAGE), { type: "anthropic", api_key: SECRET });
    expect(calls).toEqual(["set"]);
    expect(JSON.stringify(reply)).not.toContain(SECRET);
  });

  it("refuses the account origin's page — a trusted cloud sender — on every keys channel, before reading the payload", async () => {
    const { calls, invoke } = wire();
    const page = from(accountView, `${ACCOUNT}/settings`);
    for (const [channel, payload] of [
      [KEYS_CHANNELS.list, undefined],
      [KEYS_CHANNELS.set, { type: "anthropic", api_key: SECRET }],
      [KEYS_CHANNELS.remove, { id: "anthropic" }],
    ] as const) {
      await expect(invoke(channel, page, payload), channel).rejects.toThrow(/refused/);
    }
    expect(calls).toEqual([]);
  });

  it("refuses the chrome, an iframe of the key window, an unknown sender, and a key window that left its page", async () => {
    const { calls, invoke } = wire();
    const senders = [
      from(chrome, "file:///app/dist/renderer/index.html"),
      { sender: keysWindow, senderFrame: { processId: 1, routingId: 7, url: KEYS_PAGE } },
      from(contents(99), KEYS_PAGE),
      from(keysWindow, "https://evil.example/keys.html"),
    ];
    for (const event of senders) {
      await expect(invoke(KEYS_CHANNELS.list, event)).rejects.toThrow(/refused/);
    }
    expect(calls).toEqual([]);
  });

  it("refuses a malformed key without quoting it back", async () => {
    const { calls, invoke } = wire();
    const bad = `${SECRET}\nX-Injected: 1`;
    const refusal = await invoke(KEYS_CHANNELS.set, from(keysWindow, KEYS_PAGE), { type: "anthropic", api_key: bad }).then(
      () => "",
      (err: unknown) => (err instanceof Error ? err.message : String(err)),
    );
    expect(refusal).toMatch(/invalid request: keys.set.api_key/);
    expect(refusal).not.toContain(SECRET);
    expect(calls).toEqual([]);
  });
});

describe("cloud:account:open-keys", () => {
  it("lets the account's page open the window, and gives it nothing back", async () => {
    const { calls, invoke } = wire();
    await expect(invoke(CLOUD_CHANNELS.accountOpenKeys, from(accountView, `${ACCOUNT}/settings`))).resolves.toBeUndefined();
    expect(calls).toEqual(["openKeys"]);
  });

  it("hands the window a validated prefill and refuses one that carries a key or a bad address", async () => {
    const { calls, invoke } = wire();
    const page = from(accountView, `${ACCOUNT}/settings`);
    const prefill = { id: "openai", type: "openai" };
    await expect(invoke(CLOUD_CHANNELS.accountOpenKeys, page, prefill)).resolves.toBeUndefined();
    expect(calls).toEqual([`openKeys ${JSON.stringify(prefill)}`]);

    const refusals = [
      { ...prefill, api_key: SECRET },
      { id: "8f0e7c0a-1b2c-4d5e-9f00-0123456789ab", type: "openai_compatible", base_url: "http://evil.example/v1" },
      { id: "not-a-uuid", type: "openai_compatible", base_url: "https://x.example/v1" },
    ];
    for (const bad of refusals) {
      const message = await invoke(CLOUD_CHANNELS.accountOpenKeys, page, bad).then(
        () => "",
        (err: Error) => err.message,
      );
      expect(message, JSON.stringify(bad)).not.toBe("");
      expect(message).not.toContain(SECRET);
    }
    expect(calls).toHaveLength(1);
  });

  it("answers the prefill only to the key window's own page, and never with a key", async () => {
    const { calls, invoke } = wire();
    await expect(invoke(KEYS_CHANNELS.prefill, from(accountView, `${ACCOUNT}/settings`))).rejects.toThrow(/refused/);
    expect(calls).toEqual([]);
    const reply = await invoke(KEYS_CHANNELS.prefill, from(keysWindow, KEYS_PAGE));
    expect(JSON.stringify(reply)).not.toContain(SECRET);
    expect(calls).toEqual(["prefill"]);
  });

  it("is the only keys channel a cloud page can reach", () => {
    wire();
    const cloudKeyChannels = [...ipc.handlers.keys()].filter((c) => c.startsWith("cloud:") && c.includes("key"));
    expect(cloudKeyChannels).toEqual([CLOUD_CHANNELS.accountOpenKeys]);
  });
});
