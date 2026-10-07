import { mkdtempSync, writeFileSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import { describe, expect, it } from "vitest";
import { defaultWasmThreads, startWorkerEngine } from "./worker-engine.js";

/**
 * A real worker thread speaking the protocol, so what is tested is the thread
 * lifecycle — terminate on release, a crash failing what was in flight —
 * rather than a stubbed `Worker`.
 */
function fakeWorker(body: string): string {
  const dir = mkdtempSync(path.join(os.tmpdir(), "tt-embed-worker-"));
  const file = path.join(dir, "worker.cjs");
  writeFileSync(file, `const { parentPort, workerData } = require("node:worker_threads");\n${body}\n`);
  return file;
}

const files = { modelPath: "m", tokenizerJsonPath: "t", tokenizerConfigPath: "c" };
const init = { files, wasmDir: "/w", numThreads: 1 };

const ECHO = `
parentPort.postMessage({ type: "ready" });
parentPort.on("message", (req) => {
  if (req.texts[0] === "crash") process.exit(3);
  const vectors = req.texts.map((t) => Float64Array.from([t.length]));
  parentPort.postMessage({ type: "result", id: req.id, vectors, promptTokens: req.texts.length * 10 }, vectors.map((v) => v.buffer));
});`;

describe("startWorkerEngine", () => {
  it("answers through the worker and terminates it on release", async () => {
    const engine = await startWorkerEngine(fakeWorker(ECHO), init);
    const result = await engine.embed(["ab", "abcd"]);
    expect(result.vectors.map((v) => v[0])).toEqual([2, 4]);
    expect(result.promptTokens).toBe(20);
    expect(engine.alive).toBe(true);

    await engine.release();
    expect(engine.alive).toBe(false);
    await expect(engine.embed(["x"])).rejects.toThrow(/gone/);
  });

  it("fails what was in flight and reports itself dead when the worker exits", async () => {
    const engine = await startWorkerEngine(fakeWorker(ECHO), init);
    await expect(engine.embed(["crash"])).rejects.toThrow(/exited with code 3/);
    expect(engine.alive).toBe(false);
  });

  it("rejects, with the worker gone, when the model does not load", async () => {
    await expect(
      startWorkerEngine(fakeWorker(`parentPort.postMessage({ type: "failed", message: "no wasm" });`), init),
    ).rejects.toThrow("no wasm");
  });

  it("rejects when the worker dies before it is ready", async () => {
    await expect(startWorkerEngine(fakeWorker("process.exit(2);"), init)).rejects.toThrow(/code 2/);
  });
});

describe("defaultWasmThreads", () => {
  it("uses half the cores, between one and four", () => {
    expect(defaultWasmThreads(1)).toBe(1);
    expect(defaultWasmThreads(4)).toBe(2);
    expect(defaultWasmThreads(12)).toBe(4);
  });
});
