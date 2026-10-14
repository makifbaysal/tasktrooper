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
const IDLE = { phase: "idle", steps: [], percent: 0 };

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
function wire(mode: "local" | "account" = "account", consent: boolean | Promise<boolean> = true, plan: string[] | null = null) {
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
      keysMcpList: () => (calls.push("mcp-list"), { servers: [], runnerRestarting: false }),
      keysMcpSet: (_request: unknown) => (calls.push("mcp-set"), { servers: [], runnerRestarting: false }),
      keysMcpRemove: (name: string) => (calls.push(`mcp-remove ${name}`), { servers: [], runnerRestarting: false }),
      accountMode: () => mode,
      analyticsState: () => (calls.push("analytics-get"), { available: true, enabled: true, forcedOff: false }),
      setAnalytics: (on: boolean) => (calls.push(`analytics-set ${on}`), { available: true, enabled: on, forcedOff: false }),
      toolchainReport: (force: boolean) => (calls.push(`toolchain-report ${force}`), Promise.resolve({ items: [] })),
      toolchainPlan: (ids: string[]) => (calls.push(`toolchain-plan ${ids.join(",")}`), Promise.resolve(plan ?? ["homebrew", ...ids])),
      toolchainState: () => (calls.push("toolchain-state"), IDLE),
      toolchainInstall: (ids: string[]) => (calls.push(`toolchain-install ${ids.join(",")}`), Promise.resolve(IDLE)),
      toolchainEnqueue: (ids: string[]) => (calls.push(`toolchain-enqueue ${ids.join(",")}`), IDLE),
      confirmToolchainInstall: (ids: string[]) => (calls.push(`toolchain-confirm ${ids.join(",")}`), Promise.resolve(consent)),
      toolchainCancel: () => (calls.push("toolchain-cancel"), IDLE),
      toolchainDismiss: () => (calls.push("toolchain-dismiss"), IDLE),
      toolchainOpenPage: (id: string) => (calls.push(`toolchain-open ${id}`), Promise.resolve()),
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

describe("cloud:analytics:*", () => {
  it.each(["local", "account"] as const)("answers the web app's page in %s mode and validates the payload", async (mode) => {
    const { calls, invoke } = wire(mode);
    const page = from(accountView, `${ACCOUNT}/settings`);
    await expect(invoke(CLOUD_CHANNELS.analyticsGet, page)).resolves.toMatchObject({ enabled: true });
    await expect(invoke(CLOUD_CHANNELS.analyticsSet, page, { on: false })).resolves.toMatchObject({ enabled: false });
    await expect(invoke(CLOUD_CHANNELS.analyticsSet, page, { on: "no" })).rejects.toThrow(/invalid request/);
    expect(calls).toEqual(["analytics-get", "analytics-set false"]);
  });

  it("refuses any other sender", async () => {
    const { calls, invoke } = wire("account");
    await expect(invoke(CLOUD_CHANNELS.analyticsGet, from(chrome, "file:///x.html"))).rejects.toThrow(/refused/);
    await expect(invoke(CLOUD_CHANNELS.analyticsSet, from(chrome, "file:///x.html"), { on: false })).rejects.toThrow(/refused/);
    await expect(invoke(CLOUD_CHANNELS.analyticsSet, from(contents(99), ACCOUNT), { on: false })).rejects.toThrow(/refused/);
    expect(calls).toEqual([]);
  });
});

describe("shell:keys:mcp-*", () => {
  const server = { name: "notes", command: "/bin/notes", env: { NOTES_TOKEN: SECRET } };

  it("answers the key window's own page, without a secret in the reply", async () => {
    const { calls, invoke } = wire();
    const page = from(keysWindow, KEYS_PAGE);
    const replies = [
      await invoke(KEYS_CHANNELS.mcpList, page),
      await invoke(KEYS_CHANNELS.mcpSet, page, server),
      await invoke(KEYS_CHANNELS.mcpRemove, page, { name: "notes" }),
    ];
    expect(calls).toEqual(["mcp-list", "mcp-set", "mcp-remove notes"]);
    expect(JSON.stringify(replies)).not.toContain(SECRET);
  });

  it("refuses the account's page, the chrome and a stranger on every MCP channel, before reading the payload", async () => {
    const { calls, invoke } = wire();
    const senders = [
      from(accountView, `${ACCOUNT}/settings`),
      from(chrome, "file:///app/dist/renderer/index.html"),
      from(contents(99), KEYS_PAGE),
      from(keysWindow, "https://evil.example/keys.html"),
    ];
    for (const page of senders) {
      for (const [channel, payload] of [
        [KEYS_CHANNELS.mcpList, undefined],
        [KEYS_CHANNELS.mcpSet, server],
        [KEYS_CHANNELS.mcpRemove, { name: "notes" }],
      ] as const) {
        await expect(invoke(channel, page, payload), channel).rejects.toThrow(/refused/);
      }
    }
    expect(calls).toEqual([]);
  });

  it("refuses a malformed server without quoting its secret", async () => {
    const { calls, invoke } = wire();
    const bad = `${SECRET}\nX-Injected: 1`;
    const refusal = await invoke(KEYS_CHANNELS.mcpSet, from(keysWindow, KEYS_PAGE), {
      name: "wiki",
      url: "https://wiki.example/mcp",
      headers: { Authorization: bad },
    }).then(
      () => "",
      (err: unknown) => (err instanceof Error ? err.message : String(err)),
    );
    expect(refusal).toMatch(/invalid request: keys.mcp-set.headers/);
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

describe("cloud:toolchain:*", () => {
  const TOOLCHAIN = [
    [CLOUD_CHANNELS.toolchainReport, { force: true }],
    [CLOUD_CHANNELS.toolchainPlan, { ids: ["node"] }],
    [CLOUD_CHANNELS.toolchainState, undefined],
    [CLOUD_CHANNELS.toolchainInstall, { ids: ["node"] }],
    [CLOUD_CHANNELS.toolchainCancel, undefined],
    [CLOUD_CHANNELS.toolchainDismiss, undefined],
    [CLOUD_CHANNELS.toolchainOpenPage, { id: "xcode" }],
  ] as const;

  it("registers all seven channels", () => {
    wire();
    for (const [channel] of TOOLCHAIN) expect(ipc.handlers.has(channel), channel).toBe(true);
  });

  it("answers the local app's page in local mode, installing without a dialog", async () => {
    const { calls, invoke } = wire("local");
    const page = from(accountView, ACCOUNT);
    for (const [channel, payload] of TOOLCHAIN) await invoke(channel, page, payload);
    expect(calls).toEqual([
      "toolchain-report true",
      "toolchain-plan node",
      "toolchain-state",
      "toolchain-install node",
      "toolchain-cancel",
      "toolchain-dismiss",
      "toolchain-open xcode",
    ]);
  });

  it("asks the person natively before the account's page installs anything, and enqueues exactly what they saw", async () => {
    const { calls, invoke } = wire("account", true);
    await invoke(CLOUD_CHANNELS.toolchainInstall, from(accountView, `${ACCOUNT}/home`), { ids: ["node"] });
    expect(calls).toEqual(["toolchain-plan node", "toolchain-confirm homebrew,node", "toolchain-enqueue homebrew,node"]);
  });

  it("shows no dialog when there is nothing left to install", async () => {
    const { calls, invoke } = wire("account", true, []);
    await invoke(CLOUD_CHANNELS.toolchainInstall, from(accountView, `${ACCOUNT}/home`), { ids: ["node"] });
    expect(calls).toEqual(["toolchain-plan node", "toolchain-state"]);
  });

  it("answers \"busy\" to a second install while the first one's dialog is open", async () => {
    let answer: (yes: boolean) => void = () => undefined;
    const pending = new Promise<boolean>((resolve) => {
      answer = resolve;
    });
    const { calls, invoke } = wire("account", pending);
    const page = from(accountView, `${ACCOUNT}/home`);
    const first = invoke(CLOUD_CHANNELS.toolchainInstall, page, { ids: ["node"] });
    await new Promise((r) => setTimeout(r, 0));
    await expect(invoke(CLOUD_CHANNELS.toolchainInstall, page, { ids: ["go"] })).rejects.toThrow(/^busy$/);
    answer(true);
    await first;
    expect(calls).toEqual(["toolchain-plan node", "toolchain-confirm homebrew,node", "toolchain-enqueue homebrew,node"]);
    await invoke(CLOUD_CHANNELS.toolchainInstall, page, { ids: ["go"] });
    expect(calls.at(-1)).toBe("toolchain-enqueue homebrew,go");
  });

  it("rejects with \"declined\" when the person cancels the dialog, and installs nothing", async () => {
    const { calls, invoke } = wire("account", false);
    await expect(invoke(CLOUD_CHANNELS.toolchainInstall, from(accountView, `${ACCOUNT}/home`), { ids: ["node"] })).rejects.toThrow(
      /^declined$/,
    );
    expect(calls).toEqual(["toolchain-plan node", "toolchain-confirm homebrew,node"]);
  });

  it("lets the account's page read, plan, cancel and open pages without a dialog", async () => {
    const { calls, invoke } = wire("account", false);
    const page = from(accountView, `${ACCOUNT}/settings`);
    for (const [channel, payload] of TOOLCHAIN.filter(([c]) => c !== CLOUD_CHANNELS.toolchainInstall)) {
      await invoke(channel, page, payload);
    }
    expect(calls.some((c) => c.startsWith("toolchain-confirm"))).toBe(false);
    expect(calls).toHaveLength(6);
  });

  it("refuses the chrome, a stranger and a page that left its origin, before reading the payload", async () => {
    const { calls, invoke } = wire("account");
    for (const event of [from(chrome, "file:///app/dist/renderer/index.html"), from(contents(99), ACCOUNT), from(accountView, "https://evil.example/")]) {
      for (const [channel, payload] of TOOLCHAIN) {
        await expect(invoke(channel, event, payload), channel).rejects.toThrow(/refused/);
      }
    }
    expect(calls).toEqual([]);
  });

  it("takes ids only: a command, a URL or an unknown id is an invalid request", async () => {
    const { calls, invoke } = wire("local");
    const page = from(accountView, ACCOUNT);
    for (const payload of [{ ids: ["brew install wget"] }, { ids: "node" }, { ids: ["https://evil.example/x.sh"] }]) {
      await expect(invoke(CLOUD_CHANNELS.toolchainInstall, page, payload)).rejects.toThrow(/invalid request/);
    }
    await expect(invoke(CLOUD_CHANNELS.toolchainOpenPage, page, { id: "https://evil.example" })).rejects.toThrow(/invalid request/);
    await expect(invoke(CLOUD_CHANNELS.toolchainReport, page, { force: "yes" })).rejects.toThrow(/invalid request/);
    expect(calls).toEqual([]);
  });
});
