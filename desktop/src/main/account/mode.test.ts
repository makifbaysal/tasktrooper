import { describe, expect, it } from "vitest";
import type { AccountState, RunnerPairingBundle, UserSettings } from "../../ipc/types.js";
import { ModeController, type ModeControllerDeps } from "./mode.js";

const CLOUD = "https://app.tasktrooper.ai";

function settings(initial: Partial<UserSettings> = {}): ModeControllerDeps["settings"] & { value: UserSettings } {
  const store = {
    value: {
      workspaceDir: "/Users/me/TaskTrooper",
      launchAtLogin: false,
      autoConnect: true,
      notifications: { enabled: true, analizReview: true, humanUat: true, humanNeeded: true, agentComments: true, agentChatReplies: true },
      mode: "local",
      ...initial,
    } as UserSettings,
    get: () => store.value,
    set: (patch: Partial<UserSettings>) => {
      store.value = { ...store.value, ...patch };
      return store.value;
    },
  };
  return store;
}

function harness(initial: Partial<UserSettings> = {}, overrides: Partial<ModeControllerDeps> = {}) {
  const calls: string[] = [];
  const store = settings(initial);
  let paired = false;
  const deps: ModeControllerDeps = {
    settings: store,
    rules: { allowLoopbackHttp: false },
    defaultOrigin: CLOUD,
    stopLocal: async () => {
      calls.push(`stopLocal mode=${store.value.mode}`);
    },
    startLocal: async () => {
      calls.push(`startLocal mode=${store.value.mode}`);
    },
    stopAccount: async () => {
      calls.push(`stopAccount mode=${store.value.mode}`);
      paired = false;
    },
    showAccount: (origin, route) => calls.push(`showAccount ${origin}${route} mode=${store.value.mode}`),
    showLocal: () => calls.push(`showLocal mode=${store.value.mode}`),
    clearAccountSession: async (origin) => {
      calls.push(`clear ${origin}`);
    },
    paired: () => paired,
    ...overrides,
  };
  const controller = new ModeController(deps);
  const states: AccountState[] = [];
  controller.on("state", (s) => states.push(s));
  return { controller, calls, store, states, pair: () => (paired = true) };
}

const bundle = (tm_base_url: string): RunnerPairingBundle => ({
  runner_token: "rtok",
  tm_base_url,
  tenant_id: "t",
  member_uid: "m",
  paired_at: "2026-10-09T00:00:00Z",
  label: "laptop",
});

describe("signing in", () => {
  it("stops the local backend, persists the mode, then loads the account's login page", async () => {
    const { controller, calls, store } = harness();
    const state = await controller.signIn();
    expect(calls).toEqual(["stopLocal mode=local", `showAccount ${CLOUD}/login mode=account`]);
    expect(store.value.mode).toBe("account");
    expect(store.value.accountOrigin).toBe(CLOUD);
    expect(state).toMatchObject({ mode: "account", origin: CLOUD, switching: false });
  });

  it("says it is switching while it is, so a page can show it", async () => {
    const { controller, states } = harness();
    await controller.signIn();
    expect(states.map((s) => s.switching)).toEqual([true, false]);
  });

  it("opens the origin it was given, when it is one this build accepts", async () => {
    const { controller, calls } = harness();
    await controller.signIn("https://acme.example/");
    expect(calls.at(-1)).toBe("showAccount https://acme.example/login mode=account");
    expect(controller.origin).toBe("https://acme.example");
  });

  it("refuses an origin this build will not open, before stopping anything", async () => {
    const { controller, calls, store } = harness();
    await expect(controller.signIn("http://app.tasktrooper.ai")).rejects.toThrow(/https/);
    await expect(controller.signIn("http://127.0.0.1:8080")).rejects.toThrow();
    expect(calls).toEqual([]);
    expect(store.value.mode).toBe("local");
  });

  it("is a no-op when already signed in there, and refused for another account", async () => {
    const { controller, calls } = harness({ mode: "account", accountOrigin: CLOUD });
    await expect(controller.signIn()).resolves.toMatchObject({ mode: "account" });
    await expect(controller.signIn("https://other.example")).rejects.toThrow(/Sign out first/);
    expect(calls).toEqual([]);
  });

  it("stays local, starts the backend again and reports why when stopping it fails", async () => {
    const { controller, calls, store } = harness(
      {},
      {
        stopLocal: async () => {
          throw new Error("drain failed");
        },
      },
    );
    await expect(controller.signIn()).rejects.toThrow("drain failed");
    expect(store.value.mode).toBe("local");
    expect(calls).toContain("startLocal mode=local");
    expect(controller.state().error).toBe("drain failed");
  });
});

describe("signing out", () => {
  it("takes the account's page down first, then stops the runner, persists local, clears the session and starts locally", async () => {
    const { controller, calls, store } = harness({ mode: "account", accountOrigin: CLOUD });
    const state = await controller.signOut();
    expect(calls).toEqual([
      "showLocal mode=account",
      "stopAccount mode=account",
      `clear ${CLOUD}`,
      "startLocal mode=local",
    ]);
    expect(store.value.mode).toBe("local");
    // Kept, so signing in again opens the same account.
    expect(store.value.accountOrigin).toBe(CLOUD);
    expect(state.mode).toBe("local");
  });

  it("is a no-op in local mode", async () => {
    const { controller, calls } = harness();
    await controller.signOut();
    expect(calls).toEqual([]);
  });

  it("still ends local when the session cannot be cleared, and says so", async () => {
    const { controller, store } = harness(
      { mode: "account", accountOrigin: CLOUD },
      {
        clearAccountSession: async () => {
          throw new Error("busy");
        },
      },
    );
    const state = await controller.signOut();
    expect(store.value.mode).toBe("local");
    expect(state.error).toMatch(/busy/);
  });

  it("runs a sign-in and a sign-out one after the other, never interleaved", async () => {
    const { controller, calls } = harness();
    await Promise.all([controller.signIn(), controller.signOut()]);
    expect(calls).toEqual([
      "stopLocal mode=local",
      `showAccount ${CLOUD}/login mode=account`,
      "showLocal mode=account",
      "stopAccount mode=account",
      `clear ${CLOUD}`,
      "startLocal mode=local",
    ]);
  });
});

describe("the origin", () => {
  it("does not honour a stored loopback origin in a build that refuses one", () => {
    const { controller } = harness({ accountOrigin: "http://127.0.0.1:8080" });
    expect(controller.origin).toBe(CLOUD);
  });
});

describe("checkPairing", () => {
  it("refuses any pairing while local", () => {
    const { controller } = harness();
    expect(controller.checkPairing(bundle(`${CLOUD}`))).toMatch(/not signed in/);
  });

  it("accepts a bundle for the account's own origin and refuses every other", () => {
    const { controller } = harness({ mode: "account", accountOrigin: CLOUD });
    expect(controller.checkPairing(bundle(CLOUD))).toBeNull();
    expect(controller.checkPairing(bundle(`${CLOUD}/api`))).toBeNull();
    for (const other of ["https://evil.example", "http://app.tasktrooper.ai", "https://app.tasktrooper.ai.evil.example"]) {
      expect(controller.checkPairing(bundle(other)), other).toMatch(/signed in to/);
    }
  });

  it("reports pairing in the state it publishes", () => {
    const { controller, pair } = harness({ mode: "account", accountOrigin: CLOUD });
    expect(controller.state().paired).toBe(false);
    pair();
    expect(controller.state().paired).toBe(true);
  });
});
