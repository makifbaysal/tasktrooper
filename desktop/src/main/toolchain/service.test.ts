import { mkdtempSync, readFileSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import { describe, expect, it, vi } from "vitest";
import { TOOLCHAIN_IDS, type ToolchainId, type ToolchainJobState } from "../../ipc/types.js";
import type { Detection, Installer, ProbeKit, ToolDefinition } from "./types.js";

vi.mock("electron", () => ({ app: { getAppPath: () => "/app", getPath: () => "/tmp/tt-service", isPackaged: false } }));

const { ToolchainDetector } = await import("./detect.js");
const { ToolchainJobRunner } = await import("./job.js");
const { ToolchainStateStore } = await import("./store.js");
const { ToolchainService, isOpenablePage } = await import("./service.js");

/**
 * The report and the service over a small fake table: four tools apply to
 * this "Mac", the rest do not. Detection counts its sweeps, so joining and
 * reusing them is observable.
 */

const APPLY: ToolchainId[] = ["homebrew", "node", "xcode", "appium"];

async function until(check: () => boolean): Promise<void> {
  for (let i = 0; i < 100 && !check(); i += 1) await new Promise((r) => setTimeout(r, 5));
  if (!check()) throw new Error("timed out waiting");
}

function setup() {
  const installed = new Set<ToolchainId>();
  let sweeps = 0;
  let gate: Promise<void> = Promise.resolve();
  const detectFor = (id: ToolchainId) => async (): Promise<Detection> => {
    if (id === "homebrew") sweeps += 1;
    await gate;
    return installed.has(id)
      ? { status: "installed", version: "9.9", path: `/opt/tools/${id}/bin/${id}` }
      : { status: "missing", detail: `no ${id}` };
  };
  const definitions = Object.fromEntries(
    TOOLCHAIN_IDS.map((id): [ToolchainId, ToolDefinition] => [
      id,
      {
        id,
        label: id[0]?.toUpperCase() + id.slice(1),
        docsUrl: `https://docs.example/${id}`,
        affectsRuntime: id === "node",
        ...(id === "node" ? { runtime: "bin-dir" as const } : {}),
        recipes: APPLY.includes(id) ? { darwin: { install: { kind: "brew", formula: id } } } : {},
        detect: detectFor(id),
      },
    ]),
  ) as Record<ToolchainId, ToolDefinition>;
  const installers: Partial<Record<ToolchainId, Installer>> = {
    homebrew: { installKind: "interactive", method: "Terminal", command: "install brew", dependsOn: [], run: async () => void installed.add("homebrew") },
    node: { installKind: "auto", method: "Homebrew", command: "brew install node", dependsOn: ["homebrew"], run: async () => void installed.add("node") },
    xcode: { installKind: "manual", method: "App Store", url: "https://apps.apple.com/app/xcode/id497799835", dependsOn: [], run: () => Promise.reject(new Error("manual")) },
    appium: { installKind: "auto", method: "npm", command: "npm install -g appium", dependsOn: ["node"], run: async () => void installed.add("appium") },
  };
  const installerFor = (id: ToolchainId): Installer | null => installers[id] ?? null;
  const detector = new ToolchainDetector({ definitions, platform: "darwin", kit: () => ({}) as ProbeKit, installerFor });
  const runner = new ToolchainJobRunner({
    installerFor,
    label: (id) => definitions[id].label,
    affectsRuntime: (id) => definitions[id].affectsRuntime,
    detect: (id) => detector.detect(id),
    machine: { workDir: "/tmp", host: () => ({ platform: "darwin", arch: "arm64", home: "/h", env: {}, which: () => null }), exec: () => Promise.resolve(0), download: () => Promise.resolve() },
  });
  const file = path.join(mkdtempSync(path.join(os.tmpdir(), "tt-service-")), "toolchain-state.json");
  const store = new ToolchainStateStore(file);
  const published: ToolchainJobState[] = [];
  const finished: ToolchainJobState[] = [];
  const service = new ToolchainService(
    {
      definitions,
      detector,
      runner,
      store,
      installerFor,
      workDir: path.join(path.dirname(file), "toolchain"),
      installed: () => ({ pathDirs: ["/static/tools"] }),
    },
    { publish: (s) => published.push(s), onJobFinished: (s) => finished.push(s) },
  );
  return {
    service,
    installed,
    runner,
    store,
    file,
    published,
    finished,
    sweeps: () => sweeps,
    hold: () => {
      let release = (): void => undefined;
      gate = new Promise<void>((r) => {
        release = r;
      });
      return () => {
        release();
        gate = Promise.resolve();
      };
    },
  };
}

describe("the toolchain report", () => {
  it("lists only what applies here, in TOOLCHAIN_IDS order, with how each would be installed", async () => {
    const { service } = setup();
    const report = await service.report();
    expect(report.platform).toBe("darwin");
    expect(report.items.map((i) => i.id)).toEqual(APPLY);
    expect(report.items[1]).toEqual({
      id: "node",
      label: "Node",
      status: "missing",
      detail: "no node",
      installKind: "auto",
      method: "Homebrew",
      command: "brew install node",
      url: "https://docs.example/node",
      dependsOn: ["homebrew"],
    });
    expect(report.items[2]).toMatchObject({ installKind: "manual", url: "https://apps.apple.com/app/xcode/id497799835" });
  });

  it("joins a sweep in flight, reuses a fresh one, and starts over when forced or after an install", async () => {
    const t = setup();
    const release = t.hold();
    const a = t.service.report();
    const b = t.service.report();
    release();
    expect(await a).toBe(await b);
    expect(t.sweeps()).toBe(1);
    await t.service.report();
    expect(t.sweeps()).toBe(1);
    await t.service.report(true);
    expect(t.sweeps()).toBe(2);
    await t.service.install(["homebrew"]);
    await t.runner.idle();
    const after = await t.service.report();
    expect(after.items[0]?.status).toBe("installed");
  });
});

describe("ToolchainService", () => {
  it("plans with dependencies, without manual or installed tools", async () => {
    const t = setup();
    expect(await t.service.plan(["appium", "xcode"])).toEqual(["homebrew", "node", "appium"]);
    t.installed.add("homebrew");
    await t.service.report(true);
    expect(await t.service.plan(["appium"])).toEqual(["node", "appium"]);
    expect(await t.service.plan([])).toEqual([]);
  });

  it("starts the planned job at once, pushes its state, writes it down, and announces its end", async () => {
    const t = setup();
    const started = await t.service.install(["appium"]);
    expect(started.phase).toBe("running");
    expect(started.steps.map((s) => s.id)).toEqual(["homebrew", "node", "appium"]);
    expect(t.published[0]?.phase).toBe("running");
    const sweepsBefore = t.sweeps();
    await t.runner.idle();
    await until(() => t.finished.length === 1);
    expect(t.sweeps()).toBeGreaterThan(sweepsBefore);
    expect(t.service.runtime()).toEqual({ pathDirs: ["/opt/tools/node/bin", "/static/tools"] });
    expect(t.published.at(-1)).toMatchObject({ phase: "done", percent: 100, restartRecommended: true });
    await t.store.flush();
    expect(JSON.parse(readFileSync(t.file, "utf8"))).toMatchObject({ phase: "done" });
    expect(t.service.dismiss().phase).toBe("idle");
  });

  it("leaves out of a plan what the running job will already install", async () => {
    const t = setup();
    await t.service.install(["homebrew"]);
    const release = t.hold();
    await new Promise((r) => setTimeout(r, 0));
    expect(await t.service.plan(["node"])).toEqual(["node"]);
    release();
  });

  it("enqueues a confirmed plan exactly, adding no dependency nobody saw", () => {
    const t = setup();
    expect(t.service.enqueue(["appium"]).steps.map((s) => s.id)).toEqual(["appium"]);
  });

  it("describes a plan for the consent dialog by label and channel", () => {
    const { service } = setup();
    expect(service.describe(["homebrew", "node"])).toEqual([
      { id: "homebrew", label: "Homebrew", method: "Terminal" },
      { id: "node", label: "Node", method: "Homebrew" },
    ]);
  });

  it("opens only pages from its own table, and none for a tool that does not apply here", () => {
    const { service } = setup();
    expect(service.pageUrl("xcode")).toBe("https://apps.apple.com/app/xcode/id497799835");
    expect(service.pageUrl("node")).toBe("https://docs.example/node");
    expect(service.pageUrl("unity-hub")).toBeNull();
    expect(isOpenablePage("macappstore://apps.apple.com/app/id497799835")).toBe(true);
    for (const bad of ["javascript:alert(1)", "file:///etc/passwd", "http://example.test", "not a url"]) {
      expect(isOpenablePage(bad), bad).toBe(false);
    }
  });

  it("writes an interrupted job synchronously on quit", async () => {
    const t = setup();
    await t.service.install(["node"]);
    const release = t.hold();
    await new Promise((r) => setTimeout(r, 0));
    expect(t.runner.snapshot().steps[0]?.state).toBe("running");
    t.service.shutdown();
    release();
    const written = JSON.parse(readFileSync(t.file, "utf8")) as ToolchainJobState;
    expect(written.phase).toBe("done");
    expect(written.steps.every((s) => s.state === "failed")).toBe(true);
  });
});
