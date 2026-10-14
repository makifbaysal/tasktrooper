import { mkdtempSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import { describe, expect, it } from "vitest";
import type { ToolchainId, ToolchainJobState } from "../../ipc/types.js";
import { INTERRUPTED, LINGERING, ToolchainJobRunner, type StepMachine } from "./job.js";
import { ToolchainStateStore } from "./store.js";
import { CancelledError, InstallError, type HostFacts, type Installer, type StepContext } from "./types.js";

/**
 * The job's state machine, driven by fake installers: each one is a function
 * of its step context, and "installed" is a set the fake detection reads — so
 * every rule (decided by detection, dependencies, cancel, append) is asserted
 * without touching the machine.
 */

interface Deferred {
  promise: Promise<void>;
  resolve: () => void;
}

function deferred(): Deferred {
  let resolve = (): void => undefined;
  const promise = new Promise<void>((r) => {
    resolve = r;
  });
  return { promise, resolve };
}

const HOST: HostFacts = { platform: "darwin", arch: "arm64", home: "/home/me", env: {}, which: () => null };

function harness(opts: { installed?: ToolchainId[]; affectsRuntime?: ToolchainId[]; initial?: ToolchainJobState } = {}) {
  const installed = new Set<ToolchainId>(opts.installed ?? []);
  const installers = new Map<ToolchainId, Installer>();
  const machine: StepMachine = {
    workDir: "/tmp/unused",
    host: () => HOST,
    exec: () => Promise.resolve(0),
    download: () => Promise.resolve(),
  };
  const runner = new ToolchainJobRunner(
    {
      installerFor: (id) => installers.get(id) ?? null,
      label: (id) => id.toUpperCase(),
      affectsRuntime: (id) => (opts.affectsRuntime ?? ["node"]).includes(id),
      detect: (id) =>
        Promise.resolve(installed.has(id) ? { status: "installed", version: "1.2.3" } : { status: "missing", detail: `${id} is not here` }),
      machine,
      now: () => 1_000,
      newJobId: () => "job-1",
      sleep: () => Promise.resolve(),
      stopGraceMs: 20,
    },
    opts.initial,
  );
  const phases: boolean[] = [];
  runner.onChange((phaseChanged) => phases.push(phaseChanged));
  const finished: ToolchainJobState[] = [];
  runner.onFinished((state) => finished.push(state));

  const fake = (run: (ctx: StepContext) => Promise<void>, over: Partial<Installer> = {}): Installer => ({
    installKind: "auto",
    method: "Fake",
    command: "fake install",
    dependsOn: [],
    run,
    ...over,
  });
  /** An installer that works: afterwards, detection finds it. */
  const works = (id: ToolchainId, over: Partial<Installer> = {}): void => {
    installers.set(
      id,
      fake(async () => {
        installed.add(id);
      }, over),
    );
  };
  const steps = (): Record<string, string> => Object.fromEntries(runner.snapshot().steps.map((s) => [s.id, s.state]));
  return { runner, installed, installers, fake, works, steps, phases, finished };
}

describe("ToolchainJobRunner", () => {
  it("runs a plan in order, decides each step by detection, and finishes once", async () => {
    const h = harness();
    const order: ToolchainId[] = [];
    for (const id of ["git", "node"] as const) {
      h.installers.set(
        id,
        h.fake(async () => {
          order.push(id);
          h.installed.add(id);
        }),
      );
    }
    const started = h.runner.enqueue(["git", "node"]);
    expect(started.phase).toBe("running");
    expect(started.jobId).toBe("job-1");
    await h.runner.idle();

    const state = h.runner.snapshot();
    expect(order).toEqual(["git", "node"]);
    expect(state.phase).toBe("done");
    expect(state.percent).toBe(100);
    expect(state.steps.map((s) => [s.id, s.state, s.detail])).toEqual([
      ["git", "done", "Installed 1.2.3."],
      ["node", "done", "Installed 1.2.3."],
    ]);
    expect(h.finished).toHaveLength(1);
    expect(h.phases.filter(Boolean)).toHaveLength(2);
  });

  it("recommends a restart only when something the runtime reads was installed", async () => {
    const h = harness({ affectsRuntime: ["node"] });
    h.works("python");
    h.runner.enqueue(["python"]);
    await h.runner.idle();
    expect(h.runner.snapshot().restartRecommended).toBeUndefined();

    h.runner.dismiss();
    h.works("node");
    h.runner.enqueue(["node"]);
    await h.runner.idle();
    expect(h.runner.snapshot().restartRecommended).toBe(true);
  });

  it("skips what is already installed, and that counts for what depends on it", async () => {
    const h = harness({ installed: ["homebrew"] });
    h.works("homebrew");
    h.works("git", { dependsOn: ["homebrew"] });
    h.runner.enqueue(["homebrew", "git"]);
    await h.runner.idle();
    const [brew, git] = h.runner.snapshot().steps;
    expect(brew).toMatchObject({ state: "skipped", error: "Already installed." });
    expect(git?.state).toBe("done");
    expect(h.runner.snapshot().restartRecommended).toBeUndefined();
  });

  it("fails a step with the installer's own sentence, and skips what needed it, naming it", async () => {
    const h = harness();
    h.installers.set("homebrew", h.fake(() => Promise.reject(new InstallError("brew exited with code 1: no network"))));
    h.works("node", { dependsOn: ["homebrew"] });
    h.works("appium", { dependsOn: ["node"] });
    h.works("python");
    h.runner.enqueue(["homebrew", "node", "appium", "python"]);
    await h.runner.idle();
    const s = h.runner.snapshot().steps;
    expect(s[0]).toMatchObject({ id: "homebrew", state: "failed", error: "brew exited with code 1: no network" });
    expect(s[1]).toMatchObject({ id: "node", state: "skipped", error: "Skipped: HOMEBREW did not install." });
    expect(s[2]).toMatchObject({ id: "appium", state: "skipped", error: "Skipped: NODE did not install." });
    expect(s[3]?.state).toBe("done");
  });

  it("fails an install that exited cleanly but left nothing to find", async () => {
    const h = harness();
    h.installers.set("go", h.fake(() => Promise.resolve()));
    h.runner.enqueue(["go"]);
    await h.runner.idle();
    expect(h.runner.snapshot().steps[0]).toMatchObject({
      state: "failed",
      error: "Installed, but not found afterwards: go is not here",
    });
  });

  it("trusts detection over an exit code: a failed run that did install is done", async () => {
    const h = harness();
    h.installers.set(
      "git",
      h.fake(async () => {
        h.installed.add("git");
        throw new InstallError("winget exited with code 3010");
      }),
    );
    h.runner.enqueue(["git"]);
    await h.runner.idle();
    expect(h.runner.snapshot().steps[0]?.state).toBe("done");
  });

  it("skips a step whose dependency is neither installed nor in the job, saying where to get it", async () => {
    const h = harness();
    h.installers.set("node", { installKind: "manual", method: "Download", url: "https://nodejs.org/en/download", dependsOn: [], run: () => Promise.reject(new Error("never")) });
    h.works("appium", { dependsOn: ["node"] });
    h.runner.enqueue(["appium"]);
    await h.runner.idle();
    expect(h.runner.snapshot().steps[0]).toMatchObject({
      state: "skipped",
      error: "Skipped: it needs NODE, which is not installed. Install it from https://nodejs.org/en/download",
    });
  });

  it("reports progress within a step and across the job", async () => {
    const h = harness();
    const gate = deferred();
    h.installers.set(
      "android-sdk",
      h.fake(async (ctx) => {
        ctx.progress({ detail: "Downloading…", percent: 50 });
        await gate.promise;
        h.installed.add("android-sdk");
      }),
    );
    h.works("appium");
    h.runner.enqueue(["android-sdk", "appium"]);
    await new Promise((r) => setTimeout(r, 0));
    const mid = h.runner.snapshot();
    expect(mid.steps[0]).toMatchObject({ state: "running", percent: 50, detail: "Downloading…" });
    expect(mid.percent).toBe(25);
    gate.resolve();
    await h.runner.idle();
    expect(h.runner.snapshot().percent).toBe(100);
    expect(h.runner.snapshot().steps[0]?.percent).toBeUndefined();
  });

  it("shows an interactive step as waiting while the person finishes it", async () => {
    const h = harness();
    const gate = deferred();
    h.installers.set(
      "xcode-clt",
      h.fake(
        async (ctx) => {
          ctx.progress({ waiting: true, detail: "Finish the installer macOS opened." });
          await gate.promise;
          h.installed.add("xcode-clt");
        },
        { installKind: "interactive" },
      ),
    );
    h.runner.enqueue(["xcode-clt"]);
    await new Promise((r) => setTimeout(r, 0));
    expect(h.runner.snapshot().steps[0]).toMatchObject({ state: "waiting", installKind: "interactive" });
    gate.resolve();
    await h.runner.idle();
    expect(h.runner.snapshot().steps[0]?.state).toBe("done");
  });

  it("keeps the last 40 output lines, without terminal escapes", async () => {
    const h = harness();
    h.installers.set(
      "git",
      h.fake(async (ctx) => {
        for (let i = 1; i <= 45; i += 1) ctx.log(`\u001b[32mline ${i}\u001b[0m`);
        ctx.log("x".repeat(400));
        h.installed.add("git");
      }),
    );
    h.runner.enqueue(["git"]);
    await h.runner.idle();
    const log = h.runner.snapshot().steps[0]?.log ?? [];
    expect(log).toHaveLength(40);
    expect(log[0]).toBe("line 7");
    expect(log.at(-2)).toBe("line 45");
    expect(log.at(-1)?.length).toBe(300);
  });

  it("cancels: the running step is aborted, the queued ones never start", async () => {
    const h = harness();
    let aborted = false;
    h.installers.set(
      "android-sdk",
      h.fake(
        (ctx) =>
          new Promise<void>((_resolve, reject) => {
            ctx.signal.addEventListener("abort", () => {
              aborted = true;
              reject(new CancelledError());
            });
          }),
      ),
    );
    let started = false;
    h.installers.set(
      "appium",
      h.fake(async () => {
        started = true;
      }),
    );
    h.runner.enqueue(["android-sdk", "appium"]);
    await new Promise((r) => setTimeout(r, 0));
    const after = h.runner.cancel();
    expect(after.steps.map((s) => s.state)).toEqual(["cancelled", "cancelled"]);
    await h.runner.idle();
    expect(aborted).toBe(true);
    expect(started).toBe(false);
    expect(h.runner.snapshot()).toMatchObject({ phase: "done", percent: 100 });
    expect(h.finished).toHaveLength(1);
  });

  it("appends to a running job without repeating what it already has", async () => {
    const h = harness();
    const gate = deferred();
    h.installers.set(
      "git",
      h.fake(async () => {
        await gate.promise;
        h.installed.add("git");
      }),
    );
    h.works("node");
    h.works("go");
    h.runner.enqueue(["git"]);
    const appended = h.runner.enqueue(["git", "node", "go"]);
    expect(appended.jobId).toBe("job-1");
    expect(appended.steps.map((s) => s.id)).toEqual(["git", "node", "go"]);
    gate.resolve();
    await h.runner.idle();
    expect(h.steps()).toEqual({ git: "done", node: "done", go: "done" });
    expect(h.finished).toHaveLength(1);
  });

  it("starts what was asked for during a cancel as the next job", async () => {
    const h = harness();
    h.installers.set(
      "git",
      h.fake((ctx) => new Promise<void>((_r, reject) => ctx.signal.addEventListener("abort", () => reject(new CancelledError())))),
    );
    h.works("node");
    h.runner.enqueue(["git"]);
    await new Promise((r) => setTimeout(r, 0));
    h.runner.cancel();
    h.runner.enqueue(["node"]);
    await h.runner.idle();
    await h.runner.idle();
    expect(h.steps()).toEqual({ node: "done" });
    expect(h.finished).toHaveLength(2);
  });

  it("stops waiting on an installer it cannot kill (one running as administrator), and finishes the job", async () => {
    const h = harness();
    h.installers.set("git", h.fake(() => new Promise<void>(() => undefined)));
    h.works("node");
    h.runner.enqueue(["git", "node"]);
    await new Promise((r) => setTimeout(r, 0));
    h.runner.cancel();
    await h.runner.idle();
    const state = h.runner.snapshot();
    expect(state.phase).toBe("done");
    expect(state.steps[0]).toMatchObject({ state: "cancelled", detail: LINGERING });
    expect(state.steps[1]?.state).toBe("cancelled");
    expect(h.finished).toHaveLength(1);
  });

  it("lets a step that ended without installing be tried again in the same job, as a new step at the end", async () => {
    const h = harness();
    const gate = deferred();
    let attempts = 0;
    h.installers.set(
      "homebrew",
      h.fake(async () => {
        attempts += 1;
        if (attempts === 1) throw new InstallError("network down");
        h.installed.add("homebrew");
      }),
    );
    h.works("node", { dependsOn: ["homebrew"] });
    h.installers.set(
      "go",
      h.fake(async () => {
        await gate.promise;
        h.installed.add("go");
      }),
    );
    h.runner.enqueue(["homebrew", "node", "go"]);
    await new Promise((r) => setTimeout(r, 0));
    expect(h.steps()).toEqual({ homebrew: "failed", node: "skipped", go: "running" });

    const again = h.runner.enqueue(["homebrew", "node", "go"]);
    expect(again.steps.map((s) => [s.id, s.state])).toEqual([
      ["go", "running"],
      ["homebrew", "queued"],
      ["node", "queued"],
    ]);
    gate.resolve();
    await h.runner.idle();
    expect(h.runner.snapshot().steps.map((s) => [s.id, s.state])).toEqual([
      ["go", "done"],
      ["homebrew", "done"],
      ["node", "done"],
    ]);
    expect(h.finished).toHaveLength(1);
  });

  it("lists which ids the running job will still get to", async () => {
    const h = harness();
    const gate = deferred();
    h.installers.set(
      "git",
      h.fake(async () => {
        await gate.promise;
        h.installed.add("git");
      }),
    );
    h.works("node");
    h.runner.enqueue(["git", "node"]);
    await new Promise((r) => setTimeout(r, 0));
    expect(h.runner.pendingIds()).toEqual(["git", "node"]);
    gate.resolve();
    await h.runner.idle();
    expect(h.runner.pendingIds()).toEqual([]);
  });

  it("dismisses a finished job back to idle, and not a running one", async () => {
    const h = harness();
    const gate = deferred();
    h.installers.set(
      "git",
      h.fake(async () => {
        await gate.promise;
        h.installed.add("git");
      }),
    );
    h.runner.enqueue(["git"]);
    expect(h.runner.dismiss().phase).toBe("running");
    gate.resolve();
    await h.runner.idle();
    expect(h.runner.dismiss()).toEqual({ phase: "idle", steps: [], percent: 0 });
  });

  it("does nothing for an empty plan", () => {
    const h = harness();
    expect(h.runner.enqueue([])).toEqual({ phase: "idle", steps: [], percent: 0 });
    expect(h.phases).toEqual([]);
  });

  it("on quit, kills the running step and fails what was unfinished as interrupted — and that survives a relaunch", async () => {
    const h = harness();
    let aborted = false;
    h.installers.set(
      "android-sdk",
      h.fake(
        (ctx) =>
          new Promise<void>((_r, reject) =>
            ctx.signal.addEventListener("abort", () => {
              aborted = true;
              reject(new CancelledError());
            }),
          ),
      ),
    );
    h.works("git");
    h.installed.add("homebrew");
    h.works("homebrew");
    h.runner.enqueue(["homebrew", "android-sdk", "git"]);
    await new Promise((r) => setTimeout(r, 0));
    const before = h.phases.length;
    const final = h.runner.shutdown();
    expect(aborted).toBe(true);
    expect(final.phase).toBe("done");
    expect(final.steps.map((s) => [s.state, s.error])).toEqual([
      ["skipped", "Already installed."],
      ["failed", INTERRUPTED],
      ["failed", INTERRUPTED],
    ]);
    await h.runner.idle();
    expect(h.phases.length).toBe(before);
    expect(h.finished).toHaveLength(0);

    const file = path.join(mkdtempSync(path.join(os.tmpdir(), "tt-job-")), "toolchain-state.json");
    const store = new ToolchainStateStore(file);
    store.saveNow(final);
    const relaunched = harness({ initial: store.load(2_000) });
    expect(relaunched.runner.snapshot()).toMatchObject({ phase: "done", jobId: "job-1", percent: 100 });
    expect(relaunched.runner.snapshot().steps.map((s) => s.state)).toEqual(["skipped", "failed", "failed"]);
  });
});
