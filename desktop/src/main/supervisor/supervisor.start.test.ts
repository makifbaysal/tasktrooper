import { EventEmitter } from "node:events";
import { mkdtempSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { LogLine, PreflightItem, PreflightReport } from "../../ipc/types.js";

/**
 * The start path, driven end to end with fake children: which sweep a start
 * waits for, what it spawns on, when it restarts, and what it pushes. The
 * processes are fakes; the supervisor, its sweep sharing and its environment
 * building are the real ones.
 */

const h = vi.hoisted(() => ({
  userData: "",
  runs: [] as { force: boolean; finish: (r: PreflightReport) => void }[],
  gating: null as PreflightReport | null,
  reap: null as Promise<void> | null,
  reaps: 0,
  onStart: (_child: unknown): void => undefined,
}));

vi.mock("electron", () => ({
  app: { getAppPath: () => "/app", getPath: () => h.userData, isPackaged: false },
}));

vi.mock("./child.js", async () => {
  const { EventEmitter: Emitter } = await import("node:events");
  class FakeChild extends Emitter {
    static all: FakeChild[] = [];
    spec: { id: string; env: NodeJS.ProcessEnv };
    enabled = true;
    running = false;
    state = "idle";
    starts = 0;
    constructor(spec: { id: string; env: NodeJS.ProcessEnv }) {
      super();
      this.spec = spec;
      FakeChild.all.push(this);
    }
    get id(): string {
      return this.spec.id;
    }
    update(spec: { id: string; env: NodeJS.ProcessEnv }): void {
      this.spec = spec;
    }
    setEnabled(enabled: boolean): void {
      this.enabled = enabled;
    }
    resetCounters(): void {}
    resetBackoff(): void {}
    status() {
      return { id: this.spec.id, state: this.state, enabled: this.enabled, restarts: 0, lastExitCode: null, lastExitSignal: null };
    }
    async start(): Promise<void> {
      this.starts += 1;
      this.running = true;
      this.state = "waiting-health";
      h.onStart(this);
    }
    markHealthy(): void {
      this.state = "healthy";
    }
    async stop(): Promise<void> {
      this.running = false;
      this.state = "stopped";
      this.emit("exited", 0, null);
    }
    scheduleRestart(): number {
      return 0;
    }
  }
  return { SupervisedChild: FakeChild };
});

vi.mock("../services/detect.js", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../services/detect.js")>()),
  startPreflight: ({ force }: { force: boolean }) => {
    let finish!: (r: PreflightReport) => void;
    const complete = new Promise<PreflightReport>((resolve) => {
      finish = resolve;
    });
    h.runs.push({ force, finish });
    return { gating: Promise.resolve(h.gating as PreflightReport), complete };
  },
}));

vi.mock("../services/health.js", () => ({ waitForHealth: async () => ({ ok: true }) }));
vi.mock("../config/workspace.js", () => ({ ensureWorkspace: () => undefined }));
vi.mock("./reaper.js", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./reaper.js")>()),
  reapWithin: () => {
    h.reaps += 1;
    return h.reap ?? Promise.resolve();
  },
}));

const { Supervisor } = await import("./supervisor.js");
const { SupervisedChild } = await import("./child.js");

interface Fake extends EventEmitter {
  spec: { id: string; env: NodeJS.ProcessEnv };
  starts: number;
  running: boolean;
}
const children = (): Fake[] => (SupervisedChild as unknown as { all: Fake[] }).all;
const child = (id: string): Fake => {
  const found = children().filter((c) => c.spec.id === id || (c as unknown as { id: string }).id === id).at(-1);
  if (!found) throw new Error(`no ${id}`);
  return found;
};

function ok(id: PreflightItem["id"], p: string, extra: Partial<PreflightItem> = {}): PreflightItem {
  return { id, label: id, required: id === "agent-server" || id === "git", status: "ok", path: p, ...extra };
}

function report(items: PreflightItem[]): PreflightReport {
  return { generatedAt: 1, items, ready: items.every((i) => !i.required || i.status === "ok") };
}

const base = (): PreflightItem[] => [ok("agent-server", "/bin/agent-server"), ok("git", "/usr/bin/git")];
const withClaude = (status: PreflightItem["status"] = "ok"): PreflightReport =>
  report([...base(), ok("claude", "/opt/homebrew/bin/claude", { status })]);

function configured(): InstanceType<typeof Supervisor> {
  const s = new Supervisor();
  s.configure({
    secrets: { api_token: "t", mcp_secrets_key: "k" } as never,
    settings: { workspaceDir: "/w", launchAtLogin: false, autoConnect: true, notifications: {} } as never,
    overrides: {},
  });
  return s;
}

const tick = (ms = 0): Promise<void> => new Promise((resolve) => setTimeout(resolve, ms));

beforeEach(() => {
  // A dev run echoes every child line to the console; that is not under test.
  vi.spyOn(console, "warn").mockImplementation(() => undefined);
  h.userData = mkdtempSync(path.join(os.tmpdir(), "tt-supervisor-"));
  h.runs = [];
  h.gating = withClaude();
  h.reap = null;
  h.reaps = 0;
  children().length = 0;
  h.onStart = (c) => {
    const fake = c as Fake;
    if (fake.spec.id === "embedder") setTimeout(() => fake.emit("log", "stdout", "EMBEDDER_LISTENING 7777"));
    if (fake.spec.id === "agent-server") setTimeout(() => fake.emit("log", "stdout", "LISTENING http://127.0.0.1:5555"));
  };
});

afterEach(() => vi.useRealTimers());

describe("Supervisor start", () => {
  it("shares one sweep between the launch's detection and its start", async () => {
    const s = configured();
    await s.startEmbedder();
    const detected = s.detect();
    const started = s.connect();
    await started;
    expect(h.runs).toHaveLength(1);

    h.runs[0]?.finish(withClaude());
    await detected;
    expect(h.runs).toHaveLength(1);
    // Recent, so answered again without a new sweep; forced, so not.
    await s.detect();
    expect(h.runs).toHaveLength(1);
    void s.detect({ force: true });
    expect(h.runs).toHaveLength(2);
    expect(h.runs[1]?.force).toBe(true);
  });

  it("spawns the backend on the gating half, without waiting for the rest of the sweep", async () => {
    const s = configured();
    await s.startEmbedder();
    const snapshot = await s.connect();
    expect(snapshot.state).toBe("running");
    expect(snapshot.apiBase).toBe("http://127.0.0.1:5555");
    expect(child("agent-server").starts).toBe(1);
    expect(child("agent-server").spec.env.CLAUDE_CODE_BIN).toBe("/opt/homebrew/bin/claude");
    // The complete half has not even finished.
    expect(s.preflight.items).toEqual([]);
  });

  it("restarts the backend once when the complete half changes its environment", async () => {
    const s = configured();
    await s.startEmbedder();
    await s.connect();

    h.runs[0]?.finish(withClaude("unusable"));
    await vi.waitFor(() => expect(child("agent-server").starts).toBe(2));
    await vi.waitFor(() => expect(s.snapshot().state).toBe("running"));
    expect(child("agent-server").spec.env.CLAUDE_CODE_BIN).toBeUndefined();
    // On the report that asked for it, not a new sweep that could ask again.
    expect(h.runs).toHaveLength(1);
  });

  it("leaves the backend alone when the complete half only adds what it never reads", async () => {
    const s = configured();
    await s.startEmbedder();
    await s.connect();

    h.runs[0]?.finish(
      report([
        ...withClaude().items.map((i) => (i.id === "claude" ? { ...i, version: "2.1.0" } : i)),
        { id: "claude-account", label: "Claude account", required: false, status: "missing" },
      ]),
    );
    await tick(20);
    expect(child("agent-server").starts).toBe(1);
    expect(s.preflight.items.map((i) => i.id)).toContain("claude-account");
  });

  it("refuses the start on a blocker in the gating half, naming it", async () => {
    h.gating = report([ok("agent-server", "/bin/agent-server"), { ...ok("git", ""), status: "missing", path: undefined }]);
    const s = configured();
    await s.startEmbedder();
    const snapshot = await s.connect();
    expect(snapshot.state).toBe("failed");
    expect(snapshot.blocker?.id).toBe("git");
    expect(children().some((c) => c.spec.id === "agent-server" && c.starts > 0)).toBe(false);
  });

  it("runs the sweep while the stale-child reaper is still going, and spawns only after both", async () => {
    let reaped!: () => void;
    h.reap = new Promise<void>((resolve) => {
      reaped = resolve;
    });
    const s = configured();
    void s.reapStale();
    const started = s.connect();
    await tick();
    expect(h.runs).toHaveLength(1);
    expect(children().find((c) => c.spec.id === "agent-server")?.starts ?? 0).toBe(0);

    reaped();
    // The embedder waits for the reaper too; it is started here so the
    // backend is handed its URL.
    await s.startEmbedder();
    await started;
    expect(child("agent-server").starts).toBe(1);
  });

  it("says why when the backend exits before its LISTENING line, quoting its last words", async () => {
    h.onStart = (c) => {
      const fake = c as Fake;
      if (fake.spec.id === "embedder") setTimeout(() => fake.emit("log", "stdout", "EMBEDDER_LISTENING 7777"));
      if (fake.spec.id === "agent-server") {
        setTimeout(() => {
          fake.running = false;
          fake.emit("exited", 1, null);
          // The pipe delivers the explanation just after the exit.
          fake.emit("log", "stderr", '{"level":"error","message":"embedded postgres failed to start"}');
        });
      }
    };
    const s = configured();
    await s.startEmbedder();
    const snapshot = await s.connect();
    expect(snapshot.state).toBe("failed");
    expect(snapshot.detail).toContain("embedded postgres failed to start");
  });

  it("pushes log lines only while somebody listens, and keeps them for a later read", async () => {
    const s = configured();
    await s.startEmbedder();
    await s.connect();
    child("agent-server").emit("log", "stderr", '{"level":"warn","message":"before","n":1}');
    await tick(150);

    const batches: LogLine[][] = [];
    s.on("logs", (lines) => batches.push(lines));
    child("agent-server").emit("log", "stderr", '{"level":"info","message":"after","n":2}');
    await vi.waitFor(() => expect(batches.flat().length).toBeGreaterThan(0));

    expect(batches.flat().map((l) => l.text)).toEqual(["after n=2"]);
    const stored = s.logs("agent-server").map((l) => [l.text, l.level]);
    expect(stored).toContainEqual(["before n=1", "warn"]);
    expect(stored).toContainEqual(["after n=2", "info"]);
  });
});

describe("Supervisor start, waiting on the embedder", () => {
  it("hands the backend the embedder's URL as soon as the embedder prints it", async () => {
    h.onStart = (c) => {
      const fake = c as Fake;
      if (fake.spec.id === "embedder") setTimeout(() => fake.emit("log", "stdout", "EMBEDDER_LISTENING 7777"), 30);
      if (fake.spec.id === "agent-server") setTimeout(() => fake.emit("log", "stdout", "LISTENING http://127.0.0.1:5555"));
    };
    const s = configured();
    void s.startEmbedder();
    const t0 = Date.now();
    await s.connect();
    expect(child("agent-server").spec.env.EMBEDDINGS_BASE_URL).toBe("http://127.0.0.1:7777");
    expect(Date.now() - t0).toBeLessThan(1_000);
  });
});

/**
 * The reaper is not a boot-only sweep any more: an orphan the bounded boot
 * sweep gave up on is looked for again before every embedder spawn and on
 * wake (`main/index.ts` calls `reapStale()` on powerMonitor "resume").
 */
describe("Supervisor reaping", () => {
  it("runs a fresh sweep on every call once the last one finished, and joins one in flight", async () => {
    let finish!: () => void;
    h.reap = new Promise<void>((resolve) => {
      finish = resolve;
    });
    const s = configured();
    const first = s.reapStale();
    const joined = s.reapStale();
    expect(h.reaps).toBe(1);
    finish();
    await Promise.all([first, joined]);

    h.reap = null;
    await s.reapStale();
    expect(h.reaps).toBe(2);
  });

  it("sweeps before every embedder spawn, a restart included", async () => {
    const reapsAtSpawn: number[] = [];
    h.onStart = (c) => {
      if ((c as Fake).spec.id === "embedder") reapsAtSpawn.push(h.reaps);
    };
    const s = configured();
    await s.startEmbedder();
    await s.restartChild("embedder");
    expect(reapsAtSpawn).toEqual([1, 2]);
  });

  it("does not spawn the embedder until the sweep it started is done", async () => {
    let finish!: () => void;
    h.reap = new Promise<void>((resolve) => {
      finish = resolve;
    });
    const s = configured();
    const started = s.startEmbedder();
    await tick();
    expect(children().find((c) => c.spec.id === "embedder")?.starts ?? 0).toBe(0);
    finish();
    await started;
    expect(child("embedder").starts).toBe(1);
  });
});


/**
 * The hub is the backend's now: it starts one the first time a mobile tool
 * needs it and stops it once idle. All this app does is say where appium is.
 */
describe("Supervisor and Appium", () => {
  beforeEach(() => {
    h.gating = report([...withClaude().items, ok("appium", "/opt/homebrew/bin/appium")]);
  });

  it("runs no hub of its own and hands the backend the appium that does", async () => {
    const order: string[] = [];
    const defaults = h.onStart;
    h.onStart = (c) => {
      order.push((c as Fake).spec.id);
      defaults(c);
    };
    const s = configured();
    await s.startEmbedder();
    const snapshot = await s.connect();
    await tick(20);

    expect(snapshot.state).toBe("running");
    expect(order).toEqual(["embedder", "agent-server"]);
    expect(snapshot.children.map((c) => c.id)).toEqual(["embedder", "agent-server"]);
    expect(children().some((c) => c.spec.id === "appium")).toBe(false);
    expect(child("agent-server").spec.env.APPIUM_BIN).toBe("/opt/homebrew/bin/appium");
    expect(child("agent-server").spec.env.MOBILE_APPIUM_HUB_URL).toBe("http://127.0.0.1:4723");
  });

  it("stops the backend, which stops its hub, and leaves the embedder running", async () => {
    const s = configured();
    await s.startEmbedder();
    await s.connect();
    await s.disconnect();

    expect(child("agent-server").running).toBe(false);
    expect(child("embedder").running).toBe(true);
  });
});
