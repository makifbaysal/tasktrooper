import type { Server } from "node:http";
import { request } from "node:http";
import type { AddressInfo } from "node:net";
import { afterEach, describe, expect, it, vi } from "vitest";
import { createEmbeddingsServer, MAX_SPEECH_CHARS, SPEECH_SAMPLE_RATES } from "./server.js";
import type { SpeechEngine } from "./tts/speech-worker.js";

/** `POST /v1/audio/speech` against a fake voice: what reaches the client, and when generation stops. */

function fakeVoice(chunks: Float32Array[], options: { failAfter?: number; gate?: Promise<void> } = {}) {
  const seen: { text: string; lang: string; speed: number; signal: AbortSignal; style?: string }[] = [];
  const engine = {
    stream: vi.fn(async function* (text: string, opts: { lang: string; speed: number; signal: AbortSignal; style?: string }) {
      seen.push({ text, ...opts });
      for (const [i, chunk] of chunks.entries()) {
        if (opts.signal.aborted) return;
        if (options.failAfter === i) throw new Error("decoder blew up");
        if (i > 0 && options.gate) await options.gate;
        yield chunk;
      }
    }),
    release: vi.fn(async () => undefined),
    alive: true,
  } satisfies SpeechEngine;
  return { engine, seen };
}

const open: Server[] = [];
afterEach(async () => {
  for (const s of open.splice(0)) {
    s.closeAllConnections();
    await new Promise((resolve) => s.close(resolve));
  }
});

const TINYVOCA = { sampleRate: 24_000, styles: ["F2", "M2"], defaultStyle: "F2" };

async function start(voice?: () => Promise<SpeechEngine>, english?: () => Promise<SpeechEngine>) {
  const made = createEmbeddingsServer({ log: () => undefined, idleUnloadMs: 0 });
  if (voice) made.enableSpeech("ema", voice, { sampleRate: SPEECH_SAMPLE_RATES.ema, styles: [] });
  if (english) made.enableSpeech("tinyvoca", english, TINYVOCA);
  await new Promise<void>((resolve) => made.server.listen(0, "127.0.0.1", resolve));
  open.push(made.server);
  return { ...made, port: (made.server.address() as AddressInfo).port };
}

async function speak(port: number, body: unknown) {
  const res = await fetch(`http://127.0.0.1:${port}/v1/audio/speech`, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: typeof body === "string" ? body : JSON.stringify(body),
  });
  return { res, bytes: Buffer.from(await res.arrayBuffer()) };
}

describe("POST /v1/audio/speech", () => {
  it("streams the engine's chunks as little-endian float32 at 48 kHz", async () => {
    const { engine, seen } = fakeVoice([Float32Array.from([0.5, -0.25]), Float32Array.from([1])]);
    const s = await start(async () => engine);
    const { res, bytes } = await speak(s.port, { input: "Merhaba!", voice: "tr", speed: 1.5 });
    expect(res.status).toBe(200);
    expect(res.headers.get("content-type")).toBe("application/octet-stream");
    expect(res.headers.get("x-sample-rate")).toBe("48000");
    expect(Array.from(new Float32Array(bytes.buffer, bytes.byteOffset, bytes.byteLength / 4))).toEqual([0.5, -0.25, 1]);
    expect(bytes.readFloatLE(0)).toBe(0.5);
    expect(seen[0]).toMatchObject({ text: "Merhaba!", lang: "tr", speed: 1.5 });
  });

  it("defaults the voice to tr and the speed to 1", async () => {
    const { engine, seen } = fakeVoice([new Float32Array(1)]);
    const s = await start(async () => engine);
    expect((await speak(s.port, { input: "Selam" })).res.status).toBe(200);
    expect(seen[0].speed).toBe(1);
  });

  it.each([
    ["not JSON", "{", "bad_request"],
    ["no input", {}, "bad_request"],
    ["blank input", { input: "  \n" }, "bad_request"],
    ["input not a string", { input: ["a"] }, "bad_request"],
    ["speed too high", { input: "a", speed: 2.5 }, "bad_request"],
    ["speed too low", { input: "a", speed: 0.25 }, "bad_request"],
    ["speed not a number", { input: "a", speed: "fast" }, "bad_request"],
    ["unsupported voice", { input: "a", voice: "fr" }, "unsupported_voice"],
    ["a style for EMA, the Turkish default", { input: "a", style: "F2" }, "unsupported_style"],
    ["a style for EMA by name", { input: "a", engine: "ema", style: "F1" }, "unsupported_style"],
    ["an unknown tiny-voca voice", { input: "a", voice: "en", style: "F9" }, "unsupported_style"],
    ["a style that is not a string", { input: "a", voice: "en", style: 3 }, "unsupported_style"],
    ["an unknown engine", { input: "a", engine: "espeak" }, "unsupported_engine"],
    ["an engine that is not a string", { input: "a", engine: 1 }, "unsupported_engine"],
    ["a removed engine", { input: "a", voice: "en", engine: "kokoro" }, "unsupported_engine"],
    ["tiny-voca for Turkish", { input: "a", voice: "tr", engine: "tinyvoca" }, "unsupported_engine"],
    ["EMA for English", { input: "a", voice: "en", engine: "ema" }, "unsupported_engine"],
  ])("answers 400 for %s without loading the voice", async (_label, body, code) => {
    const load = vi.fn(async () => fakeVoice([]).engine);
    const s = await start(load, load);
    const { res, bytes } = await speak(s.port, body);
    expect(res.status).toBe(400);
    expect((JSON.parse(bytes.toString()) as { error: { code: string } }).error.code).toBe(code);
    expect(load).not.toHaveBeenCalled();
  });

  it("routes English to tiny-voca at 24 kHz, with the default style unless one is named", async () => {
    const turkish = fakeVoice([Float32Array.from([1])]);
    const english = fakeVoice([Float32Array.from([0.5])]);
    const loadTurkish = vi.fn(async () => turkish.engine);
    const s = await start(loadTurkish, async () => english.engine);
    const { res, bytes } = await speak(s.port, { input: "Hello!", voice: "en", speed: 1.25 });
    expect(res.status).toBe(200);
    expect(res.headers.get("x-sample-rate")).toBe("24000");
    expect(bytes.readFloatLE(0)).toBe(0.5);
    expect(english.seen[0]).toMatchObject({ text: "Hello!", lang: "en", speed: 1.25, style: "F2" });
    expect(loadTurkish).not.toHaveBeenCalled();

    expect((await speak(s.port, { input: "Hi", voice: "en", style: "M2" })).res.status).toBe(200);
    expect(english.seen[1].style).toBe("M2");

    const tr = await speak(s.port, { input: "Merhaba" });
    expect(tr.res.headers.get("x-sample-rate")).toBe("48000");
    expect(turkish.seen[0].style).toBeUndefined();
  });

  it("answers 503 for a named engine whose files are missing, even with the default there", async () => {
    const s = await start(async () => fakeVoice([]).engine);
    const { res, bytes } = await speak(s.port, { input: "Hello", voice: "en", engine: "tinyvoca" });
    expect(res.status).toBe(503);
    expect(bytes.toString()).toContain("voice_unavailable");
  });

  it("answers 503 voice_unavailable for a language whose files are missing", async () => {
    const s = await start(async () => fakeVoice([]).engine);
    const { res, bytes } = await speak(s.port, { input: "Hello", voice: "en", style: "F2" });
    expect(res.status).toBe(503);
    expect(bytes.toString()).toContain("voice_unavailable");
  });

  it("lists, per language, the engines this copy has on GET /v1/audio/voices, the default first", async () => {
    const all = await start(async () => fakeVoice([]).engine, async () => fakeVoice([]).engine);
    expect(await (await fetch(`http://127.0.0.1:${all.port}/v1/audio/voices`)).json()).toEqual({
      voices: [
        { id: "tr", default_engine: "ema", engines: [{ id: "ema", sample_rate: 48_000, styles: [] }] },
        { id: "en", default_engine: "tinyvoca", engines: [{ id: "tinyvoca", sample_rate: 24_000, styles: ["F2", "M2"], default_style: "F2" }] },
      ],
    });
    const onlyEnglish = await start(undefined, async () => fakeVoice([]).engine);
    expect(await (await fetch(`http://127.0.0.1:${onlyEnglish.port}/v1/audio/voices`)).json()).toEqual({
      voices: [{ id: "en", default_engine: "tinyvoca", engines: [{ id: "tinyvoca", sample_rate: 24_000, styles: ["F2", "M2"], default_style: "F2" }] }],
    });
    const none = await start();
    expect(await (await fetch(`http://127.0.0.1:${none.port}/audio/voices`)).json()).toEqual({ voices: [] });
  });

  it("answers 413 for input past the cap", async () => {
    const s = await start(async () => fakeVoice([]).engine);
    expect((await speak(s.port, { input: "a".repeat(MAX_SPEECH_CHARS + 1) })).res.status).toBe(413);
    expect((await speak(s.port, { input: "a".repeat(200_000) })).res.status).toBe(413);
  });

  it("answers 503 voice_unavailable when the voice files are not installed", async () => {
    const s = await start();
    const { res, bytes } = await speak(s.port, { input: "Merhaba" });
    expect(res.status).toBe(503);
    expect(bytes.toString()).toContain("voice_unavailable");
  });

  it("answers 503 with retry-after when the voice fails to load", async () => {
    const s = await start(async () => {
      throw new Error("bad onnx");
    });
    const { res } = await speak(s.port, { input: "Merhaba" });
    expect(res.status).toBe(503);
    expect(res.headers.get("retry-after")).toBeTruthy();
  });

  it("answers 500 when the engine fails before the first chunk", async () => {
    const { engine } = fakeVoice([new Float32Array(1)], { failAfter: 0 });
    const s = await start(async () => engine);
    expect((await speak(s.port, { input: "Merhaba" })).res.status).toBe(500);
  });

  it("cuts the stream off rather than ending it cleanly when the engine fails midway", async () => {
    const { engine } = fakeVoice([new Float32Array(4), new Float32Array(4)], { failAfter: 1 });
    const s = await start(async () => engine);
    await expect(speak(s.port, { input: "Merhaba" })).rejects.toThrow();
  });

  it("stops generating when the client hangs up", async () => {
    let release!: () => void;
    const gate = new Promise<void>((resolve) => {
      release = resolve;
    });
    const { engine, seen } = fakeVoice([new Float32Array(8), new Float32Array(8), new Float32Array(8)], { gate });
    const s = await start(async () => engine);

    await new Promise<void>((resolve, reject) => {
      const req = request(
        { host: "127.0.0.1", port: s.port, path: "/v1/audio/speech", method: "POST", headers: { "content-type": "application/json" } },
        (res) => {
          res.once("data", () => {
            req.destroy();
            resolve();
          });
        },
      );
      req.on("error", () => undefined);
      req.end(JSON.stringify({ input: "Merhaba" }));
      setTimeout(() => reject(new Error("no first chunk")), 2000);
    });
    await vi.waitFor(() => expect(seen[0]?.signal.aborted).toBe(true));
    release();
  });

  it("still answers embeddings alongside it", async () => {
    const s = await start(async () => fakeVoice([]).engine);
    const res = await fetch(`http://127.0.0.1:${s.port}/v1/embeddings`, { method: "POST", body: JSON.stringify({ input: "x" }) });
    await res.arrayBuffer();
    expect(res.status).toBe(503);
  });
});
