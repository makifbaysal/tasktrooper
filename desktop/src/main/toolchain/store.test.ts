import { mkdtempSync, readdirSync, readFileSync, writeFileSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import { describe, expect, it } from "vitest";
import type { ToolchainJobState } from "../../ipc/types.js";
import { INTERRUPTED } from "./job.js";
import { IDLE, ToolchainStateStore, parseJobState, recoverJob } from "./store.js";

const fileIn = (): string => path.join(mkdtempSync(path.join(os.tmpdir(), "tt-toolchain-store-")), "toolchain-state.json");

const RUNNING: ToolchainJobState = {
  phase: "running",
  jobId: "j",
  startedAt: 100,
  steps: [
    { id: "homebrew", label: "Homebrew", state: "done", installKind: "interactive", finishedAt: 200 },
    { id: "node", label: "Node.js", state: "running", installKind: "auto", percent: 40, detail: "Running brew install node…", log: ["==> Pouring"] },
    { id: "appium", label: "Appium", state: "queued", installKind: "auto" },
  ],
  percent: 47,
  restartRecommended: true,
};

describe("recoverJob", () => {
  it("turns a job the app did not live to finish into a finished one, with what was left failed as interrupted", () => {
    const recovered = recoverJob(RUNNING, 999);
    expect(recovered.phase).toBe("done");
    expect(recovered.finishedAt).toBe(999);
    expect(recovered.percent).toBe(100);
    expect(recovered.restartRecommended).toBeUndefined();
    expect(recovered.steps[0]).toEqual(RUNNING.steps[0]);
    expect(recovered.steps[1]).toEqual({
      id: "node",
      label: "Node.js",
      state: "failed",
      installKind: "auto",
      error: INTERRUPTED,
      finishedAt: 999,
      log: ["==> Pouring"],
    });
    expect(recovered.steps[2]).toMatchObject({ state: "failed", error: INTERRUPTED });
  });
});

describe("parseJobState", () => {
  it("refuses anything without the job's shape", () => {
    for (const bad of [null, 3, "x", {}, { phase: "idle", steps: [] }, { phase: "done" }, { phase: "done", steps: [{ id: "rm -rf", state: "done", installKind: "auto" }] }, { phase: "done", steps: [{ id: "git", state: "exploded", installKind: "auto" }] }]) {
      expect(parseJobState(bad), JSON.stringify(bad)).toBeNull();
    }
  });

  it("keeps log lines clean and bounded even when the file was edited", () => {
    const state = parseJobState({
      phase: "done",
      steps: [{ id: "git", label: "Git", state: "done", installKind: "auto", log: [...Array.from({ length: 50 }, (_, i) => `l${i}`), "\u001b[31mred\u001b[0m"] }],
      percent: 100,
    });
    const log = state?.steps[0]?.log ?? [];
    expect(log).toHaveLength(40);
    expect(log.at(-1)).toBe("red");
  });
});

describe("ToolchainStateStore", () => {
  it("is idle with no file, and with a corrupt one", () => {
    const file = fileIn();
    expect(new ToolchainStateStore(file).load()).toEqual(IDLE);
    writeFileSync(file, "{not json");
    expect(new ToolchainStateStore(file).load()).toEqual(IDLE);
  });

  it("writes atomically, coalescing a burst into the newest state", async () => {
    const file = fileIn();
    const store = new ToolchainStateStore(file);
    store.save({ ...RUNNING, percent: 1 });
    store.save({ ...RUNNING, percent: 2 });
    store.save({ ...RUNNING, phase: "done", percent: 100 });
    await store.flush();
    expect(JSON.parse(readFileSync(file, "utf8"))).toMatchObject({ phase: "done", percent: 100 });
    expect(readdirSync(path.dirname(file))).toEqual(["toolchain-state.json"]);
    expect(store.load(5).phase).toBe("done");
  });

  it("loads a job persisted mid-run as interrupted", () => {
    const file = fileIn();
    new ToolchainStateStore(file).saveNow(RUNNING);
    const loaded = new ToolchainStateStore(file).load(777);
    expect(loaded.phase).toBe("done");
    expect(loaded.steps.map((s) => s.state)).toEqual(["done", "failed", "failed"]);
  });

  it("does not let a background write land over the one made at quit", async () => {
    const file = fileIn();
    const store = new ToolchainStateStore(file);
    store.save({ ...RUNNING, percent: 5 });
    store.saveNow({ ...RUNNING, phase: "done", percent: 100 });
    store.save({ ...RUNNING, percent: 6 });
    await store.flush();
    expect(JSON.parse(readFileSync(file, "utf8"))).toMatchObject({ phase: "done", percent: 100 });
  });
});
