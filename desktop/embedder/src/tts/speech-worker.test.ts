import { existsSync, mkdtempSync, writeFileSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import { describe, expect, it } from "vitest";
import { TINYVOCA_MODEL, TINYVOCA_STYLES } from "./files.js";
import { startSpeechWorker, type SpeechEngine } from "./speech-worker.js";

/**
 * A real worker thread speaking the protocol, as in worker-engine.test.ts, so
 * the streaming, the cancel and the thread's death are what is tested.
 */
function fakeWorker(body: string): string {
  const dir = mkdtempSync(path.join(os.tmpdir(), "tt-speech-worker-"));
  const file = path.join(dir, "worker.cjs");
  writeFileSync(file, `const { parentPort } = require("node:worker_threads");\n${body}\n`);
  return file;
}

const init = { engine: "ema" as const, voiceDir: "/v", wasmDir: "/w", numThreads: 1 };

// Three chunks of [speed, i] for "say"; for "long", one every 20 ms until
// cancelled; "crash" kills the thread.
const FAKE = `
parentPort.postMessage({ type: "ready" });
const timers = new Map();
parentPort.on("message", (req) => {
  if (req.type === "cancel") {
    clearInterval(timers.get(req.id));
    return;
  }
  if (req.text === "crash") process.exit(3);
  if (req.text === "fail") return parentPort.postMessage({ type: "error", id: req.id, message: "no voice" });
  if (req.text === "long") {
    timers.set(req.id, setInterval(() => {
      const pcm = Float32Array.from([1]);
      parentPort.postMessage({ type: "chunk", id: req.id, pcm }, [pcm.buffer]);
    }, 20));
    return;
  }
  for (let i = 0; i < 3; i++) {
    const pcm = Float32Array.from([req.speed, i, req.style === "bm_george" ? 1 : 0, req.lang === "en" ? 1 : 0]);
    parentPort.postMessage({ type: "chunk", id: req.id, pcm }, [pcm.buffer]);
  }
  parentPort.postMessage({ type: "done", id: req.id });
});`;

async function collect(engine: SpeechEngine, text: string, signal = new AbortController().signal): Promise<number[][]> {
  const out: number[][] = [];
  for await (const pcm of engine.stream(text, { lang: "tr", speed: 1.5, signal })) out.push(Array.from(pcm));
  return out;
}

describe("startSpeechWorker", () => {
  it("streams each chunk in order and ends on done", async () => {
    const engine = await startSpeechWorker(fakeWorker(FAKE), init);
    expect(await collect(engine, "say")).toEqual([
      [1.5, 0, 0, 0],
      [1.5, 1, 0, 0],
      [1.5, 2, 0, 0],
    ]);
    await engine.release();
    expect(engine.alive).toBe(false);
  });

  it("passes the language and the style on to the worker", async () => {
    const engine = await startSpeechWorker(fakeWorker(FAKE), { ...init, engine: "tinyvoca" });
    const got: number[][] = [];
    for await (const pcm of engine.stream("say", { lang: "en", speed: 1, style: "bm_george", signal: new AbortController().signal })) got.push(Array.from(pcm));
    expect(got.map((c) => c[2])).toEqual([1, 1, 1]);
    expect(got.map((c) => c[3])).toEqual([1, 1, 1]);
    await engine.release();
  });

  it("passes a worker error on", async () => {
    const engine = await startSpeechWorker(fakeWorker(FAKE), init);
    await expect(collect(engine, "fail")).rejects.toThrow(/no voice/);
    await engine.release();
  });

  it("ends the stream and tells the worker to stop when aborted", async () => {
    const engine = await startSpeechWorker(fakeWorker(FAKE), init);
    const controller = new AbortController();
    const got: Float32Array[] = [];
    for await (const pcm of engine.stream("long", { lang: "tr", speed: 1, signal: controller.signal })) {
      got.push(pcm);
      if (got.length === 2) controller.abort();
    }
    expect(got).toHaveLength(2);
    await engine.release();
  });

  it("fails a stream in flight when the worker dies", async () => {
    const engine = await startSpeechWorker(fakeWorker(FAKE), init);
    await expect(collect(engine, "crash")).rejects.toThrow(/exited with code 3/);
    expect(engine.alive).toBe(false);
  });

  it("rejects when the worker fails to load", async () => {
    await expect(startSpeechWorker(fakeWorker(`parentPort.postMessage({ type: "failed", message: "no files" });`), init)).rejects.toThrow(/no files/);
  });
});

/**
 * The real voice, through the bundled worker exactly as the app starts it.
 * Needs `npm run build:embedder` first; opt in with TASKTROOPER_VOICE_SMOKE=1.
 */
const dist = path.resolve(import.meta.dirname, "../../../dist/embedder");
describe.skipIf(process.env.TASKTROOPER_VOICE_SMOKE !== "1" || !existsSync(path.join(dist, "voice", "ema_sound.onnx")))("the real voice", () => {
  it("speaks a Turkish sentence: audible, a sane length, first chunk one second", { timeout: 60_000 }, async () => {
    const engine = await startSpeechWorker(path.join(dist, "tts-worker.cjs"), { engine: "ema", voiceDir: path.join(dist, "voice"), wasmDir: dist, numThreads: 2 });
    try {
      const chunks: Float32Array[] = [];
      for await (const pcm of engine.stream("Merhaba! Bugün hava çok güzel.", { lang: "tr", speed: 1, signal: new AbortController().signal })) chunks.push(pcm);
      expect(chunks[0].length).toBe(25 * 1920);
      const all = chunks.flatMap((c) => Array.from(c));
      const seconds = all.length / 48_000;
      expect(seconds).toBeGreaterThan(1);
      expect(seconds).toBeLessThan(6);
      const rms = Math.sqrt(all.reduce((a, b) => a + b * b, 0) / all.length);
      expect(rms).toBeGreaterThan(0.01);
      expect(all.every(Number.isFinite)).toBe(true);
    } finally {
      await engine.release();
    }
  });
});

describe.skipIf(process.env.TASKTROOPER_VOICE_SMOKE !== "1" || !existsSync(path.join(dist, "voice", TINYVOCA_MODEL)))("the real English voice", () => {
  it("speaks English in both voices: audible, a sane length, one chunk per sentence", { timeout: 120_000 }, async () => {
    const engine = await startSpeechWorker(path.join(dist, "tts-worker.cjs"), { engine: "tinyvoca", voiceDir: path.join(dist, "voice"), wasmDir: dist, numThreads: 1 });
    try {
      for (const style of TINYVOCA_STYLES) {
        const chunks: Float32Array[] = [];
        for await (const pcm of engine.stream("Hello! The build is done.", { lang: "en", speed: 1, style, signal: new AbortController().signal })) chunks.push(pcm);
        expect(chunks).toHaveLength(2);
        const all = chunks.flatMap((c) => Array.from(c));
        const seconds = all.length / 24_000;
        expect(seconds).toBeGreaterThan(1);
        expect(seconds).toBeLessThan(5);
        const rms = Math.sqrt(all.reduce((a, b) => a + b * b, 0) / all.length);
        expect(rms).toBeGreaterThan(0.01);
        expect(all.every((x) => Number.isFinite(x) && Math.abs(x) <= 0.99)).toBe(true);
      }
    } finally {
      await engine.release();
    }
  });
});
