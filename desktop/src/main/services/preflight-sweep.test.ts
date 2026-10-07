import { afterEach, describe, expect, it, vi } from "vitest";
import type { PreflightReport } from "../../ipc/types.js";

const loginEnv = vi.hoisted(() => ({
  seeded: [] as string[][],
  fresh: Promise.resolve(["/fresh/bin"]),
}));
vi.mock("./login-env.js", () => ({
  seedLoginShellPath: (entries: string[]) => loginEnv.seeded.push(entries),
  loginShellPath: () => loginEnv.fresh,
}));

const { PREFLIGHT_REUSE_MS, PreflightSweeps, overridesKey, primeLoginShellPath } = await import("./preflight-sweep.js");

function report(n: number): PreflightReport {
  return { generatedAt: n, items: [], ready: true };
}

/** A sweep whose two halves the test settles by hand. */
function controllable() {
  const started: { force: boolean; finish: (r: PreflightReport) => void; fail: (e: Error) => void }[] = [];
  const start = vi.fn(({ force }: { force: boolean }) => {
    let finish!: (r: PreflightReport) => void;
    let fail!: (e: Error) => void;
    const complete = new Promise<PreflightReport>((resolve, reject) => {
      finish = resolve;
      fail = reject;
    });
    complete.catch(() => undefined);
    started.push({ force, finish, fail });
    return { gating: Promise.resolve(report(0)), complete };
  });
  return { start, started };
}

describe("PreflightSweeps", () => {
  afterEach(() => vi.useRealTimers());

  it("joins a sweep in flight instead of starting a second one", () => {
    const { start } = controllable();
    const sweeps = new PreflightSweeps({ start });
    const a = sweeps.sweep({ overrides: {} });
    const b = sweeps.sweep({ overrides: {} });
    expect(b).toBe(a);
    expect(start).toHaveBeenCalledTimes(1);
  });

  it("reuses a finished sweep while it is recent, and starts a new one after", async () => {
    let now = 1_000;
    const { start, started } = controllable();
    const sweeps = new PreflightSweeps({ start, now: () => now });
    const first = sweeps.sweep({ overrides: {} });
    started[0]?.finish(report(1));
    await first.complete;
    expect(first.result).toEqual(report(1));

    now += PREFLIGHT_REUSE_MS - 1;
    expect(sweeps.sweep({ overrides: {} })).toBe(first);
    now += 1;
    expect(sweeps.sweep({ overrides: {} })).not.toBe(first);
    expect(start).toHaveBeenCalledTimes(2);
  });

  it("answers from any age when the caller says so", async () => {
    let now = 0;
    const { start, started } = controllable();
    const sweeps = new PreflightSweeps({ start, now: () => now });
    const first = sweeps.sweep({ overrides: {} });
    started[0]?.finish(report(1));
    await first.complete;
    now += 24 * 60 * 60_000;
    expect(sweeps.sweep({ overrides: {}, maxAgeMs: Number.POSITIVE_INFINITY })).toBe(first);
  });

  it("always starts a new sweep when forced, and passes the force on", () => {
    const { start, started } = controllable();
    const sweeps = new PreflightSweeps({ start });
    const a = sweeps.sweep({ overrides: {} });
    const b = sweeps.sweep({ overrides: {}, force: true });
    expect(b).not.toBe(a);
    expect(started.map((s) => s.force)).toEqual([false, true]);
    expect(sweeps.latest).toBe(b);
  });

  it("does not answer for different overrides", () => {
    const { start } = controllable();
    const sweeps = new PreflightSweeps({ start });
    const a = sweeps.sweep({ overrides: {} });
    const b = sweeps.sweep({ overrides: { claudeBin: "/x/claude" } });
    expect(b).not.toBe(a);
    // An empty override is the same as none.
    expect(overridesKey({ claudeBin: "" })).toBe(overridesKey({}));
    expect(overridesKey({ gitBin: "/g", claudeBin: "/c" })).toBe(overridesKey({ claudeBin: "/c", gitBin: "/g" }));
  });

  it("never reuses a sweep that failed", async () => {
    const { start, started } = controllable();
    const sweeps = new PreflightSweeps({ start });
    const a = sweeps.sweep({ overrides: {} });
    started[0]?.fail(new Error("boom"));
    await expect(a.complete).rejects.toThrow("boom");
    expect(sweeps.sweep({ overrides: {} })).not.toBe(a);
  });

  it("tells its owner about each sweep it starts, once", () => {
    const { start } = controllable();
    const onSweep = vi.fn();
    const sweeps = new PreflightSweeps({ start, onSweep });
    sweeps.sweep({ overrides: {} });
    sweeps.sweep({ overrides: {} });
    sweeps.sweep({ overrides: {}, force: true });
    expect(onSweep).toHaveBeenCalledTimes(2);
  });
});

describe("primeLoginShellPath", () => {
  it("seeds the remembered PATH at once and records the fresh one", async () => {
    loginEnv.seeded = [];
    const recorded: string[][] = [];
    const cache = { loginPath: ["/old/bin"], setLoginPath: (e: string[]) => (recorded.push(e), true) };
    await primeLoginShellPath(cache as never);
    expect(loginEnv.seeded).toEqual([["/old/bin"]]);
    await Promise.resolve();
    expect(recorded).toEqual([["/fresh/bin"]]);
  });

  it("seeds nothing on a first launch", async () => {
    loginEnv.seeded = [];
    await primeLoginShellPath({ loginPath: undefined, setLoginPath: () => true } as never);
    expect(loginEnv.seeded).toEqual([]);
  });

  it("keeps the recorded PATH when this launch's login shell came back empty", async () => {
    loginEnv.seeded = [];
    loginEnv.fresh = Promise.resolve([]);
    const recorded: string[][] = [];
    const cache = { loginPath: ["/old/bin"], setLoginPath: (e: string[]) => (recorded.push(e), true) };
    await primeLoginShellPath(cache as never);
    await Promise.resolve();
    expect(recorded).toEqual([]);
    loginEnv.fresh = Promise.resolve(["/fresh/bin"]);
  });

  it("does not seed an empty PATH an older build recorded", async () => {
    loginEnv.seeded = [];
    await primeLoginShellPath({ loginPath: [], setLoginPath: () => true } as never);
    expect(loginEnv.seeded).toEqual([]);
  });
});
