import type { Server } from "node:http";
import { request } from "node:http";
import type { AddressInfo } from "node:net";
import { afterEach, describe, expect, it, vi } from "vitest";
import { createEmbeddingsServer, MAX_SPEECH_CHARS, SPEECH_SAMPLE_RATE } from "./server.js";
import type { SpeechEngine } from "./tts/speech-worker.js";

/** `POST /v1/audio/speech` against a fake voice: what reaches the client, and when generation stops. */

function fakeVoice(chunks: Float32Array[], options: { failAfter?: number; gate?: Promise<void> } = {}) {
  const seen: { text: string; speed: number; signal: AbortSignal; style?: string }[] = [];
  const engine = {
    stream: vi.fn(async function* (text: string, opts: { speed: number; signal: AbortSignal; style?: string }) {
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

const ENGLISH = { sampleRate: 24_000, styles: ["af_heart", "bm_george"], defaultStyle: "af_heart" };

async function start(voice?: () => Promise<SpeechEngine>, english?: () => Promise<SpeechEngine>) {
  const made = createEmbeddingsServer({ log: () => undefined, idleUnloadMs: 0 });
  if (voice) made.enableSpeech("tr", voice, { sampleRate: SPEECH_SAMPLE_RATE, styles: [] });
  if (english) made.enableSpeech("en", english, ENGLISH);
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
    expect(res.headers.get("x-sample-rate")).toBe(String(SPEECH_SAMPLE_RATE));
    expect(Array.from(new Float32Array(bytes.buffer, bytes.byteOffset, bytes.byteLength / 4))).toEqual([0.5, -0.25, 1]);
    expect(bytes.readFloatLE(0)).toBe(0.5);
    expect(seen[0]).toMatchObject({ text: "Merhaba!", speed: 1.5 });
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
    ["a style for the Turkish voice", { input: "a", style: "af_heart" }, "unsupported_style"],
    ["an unknown English style", { input: "a", voice: "en", style: "af_nobody" }, "unsupported_style"],
    ["a style that is not a string", { input: "a", voice: "en", style: 3 }, "unsupported_style"],
  ])("answers 400 for %s without loading the voice", async (_label, body, code) => {
    const load = vi.fn(async () => fakeVoice([]).engine);
    const s = await start(load, load);
    const { res, bytes } = await speak(s.port, body);
    expect(res.status).toBe(400);
    expect((JSON.parse(bytes.toString()) as { error: { code: string } }).error.code).toBe(code);
    expect(load).not.toHaveBeenCalled();
  });

  it("routes English to its own voice at 24 kHz, with the default style unless one is named", async () => {
    const turkish = fakeVoice([Float32Array.from([1])]);
    const english = fakeVoice([Float32Array.from([0.5])]);
    const loadTurkish = vi.fn(async () => turkish.engine);
    const s = await start(loadTurkish, async () => english.engine);
    const { res, bytes } = await speak(s.port, { input: "Hello!", voice: "en", speed: 1.25 });
    expect(res.status).toBe(200);
    expect(res.headers.get("x-sample-rate")).toBe("24000");
    expect(bytes.readFloatLE(0)).toBe(0.5);
    expect(english.seen[0]).toMatchObject({ text: "Hello!", speed: 1.25, style: "af_heart" });
    expect(loadTurkish).not.toHaveBeenCalled();

    expect((await speak(s.port, { input: "Hi", voice: "en", style: "bm_george" })).res.status).toBe(200);
    expect(english.seen[1].style).toBe("bm_george");

    const tr = await speak(s.port, { input: "Merhaba" });
    expect(tr.res.headers.get("x-sample-rate")).toBe("48000");
    expect(turkish.seen[0].style).toBeUndefined();
  });

  it("answers 503 voice_unavailable for a language whose files are missing", async () => {
    const s = await start(async () => fakeVoice([]).engine);
    const { res, bytes } = await speak(s.port, { input: "Hello", voice: "en", style: "af_heart" });
    expect(res.status).toBe(503);
    expect(bytes.toString()).toContain("voice_unavailable");
  });

  it("lists the voices this copy has on GET /v1/audio/voices", async () => {
    const both = await start(async () => fakeVoice([]).engine, async () => fakeVoice([]).engine);
    const listed = await (await fetch(`http://127.0.0.1:${both.port}/v1/audio/voices`)).json();
    expect(listed).toEqual({
      voices: [
        { id: "tr", sample_rate: 48_000, styles: [] },
        { id: "en", sample_rate: 24_000, styles: ["af_heart", "bm_george"], default_style: "af_heart" },
      ],
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
