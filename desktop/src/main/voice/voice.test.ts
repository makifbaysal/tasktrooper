import { EventEmitter } from "node:events";
import { PassThrough, Writable } from "node:stream";
import type { ChildProcessWithoutNullStreams } from "node:child_process";
import { describe, expect, it, vi } from "vitest";
import { CLOUD_EVENTS } from "../../ipc/channels.js";
import { parseHelperLine, VoiceService, type VoiceDeps } from "./voice.js";

class FakeChild extends EventEmitter {
  stdout = new PassThrough();
  stderr = new PassThrough();
  written: Buffer[] = [];
  stdinEnded = false;
  killed: string | null = null;
  stdin = new Writable({
    write: (chunk: Buffer, _enc, cb) => {
      this.written.push(chunk);
      cb();
    },
    final: (cb) => {
      this.stdinEnded = true;
      cb();
    },
  });
  kill(signal: string): boolean {
    this.killed = signal;
    return true;
  }
  say(obj: unknown): void {
    this.stdout.write(`${JSON.stringify(obj)}\n`);
  }
}

function setup(overrides: Partial<VoiceDeps> = {}) {
  const children: FakeChild[] = [];
  const events: Array<{ channel: string; payload: unknown }> = [];
  const spawnSpeech = vi.fn((_bin: string, _args: string[]) => {
    const child = new FakeChild();
    children.push(child);
    return child as unknown as ChildProcessWithoutNullStreams;
  });
  const service = new VoiceService({
    platform: "darwin",
    speechBinary: () => "/app/bin/tt-speech",
    embedderUrl: () => Promise.resolve("http://127.0.0.1:5555"),
    publish: (channel, payload) => events.push({ channel, payload }),
    spawnSpeech,
    ...overrides,
  });
  return { service, children, events, spawnSpeech };
}

const tick = () => new Promise((r) => setTimeout(r, 0));

const TINYVOCA = { id: "tinyvoca", sample_rate: 24_000, styles: ["M2", "F2"], default_style: "F2" };
const VOICES = {
  voices: [
    { id: "tr", default_engine: "ema", engines: [{ id: "ema", sample_rate: 48_000, styles: [] }] },
    { id: "en", default_engine: "tinyvoca", engines: [TINYVOCA] },
  ],
};

function voicesFetch(body: unknown, status = 200) {
  return vi.fn(async () => new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } }));
}

describe("VoiceService.capabilities", () => {
  it("listens on macOS with the helper and speaks with the engines the embedder lists, default engine and style first", async () => {
    const fetchMock = voicesFetch(VOICES);
    const { service } = setup({ fetch: fetchMock as unknown as typeof fetch });
    await expect(service.capabilities()).resolves.toEqual({
      listen: { tr: true, en: true },
      speak: { tr: true, en: true },
      speakEngines: {
        tr: [{ engine: "ema", styles: [] }],
        en: [{ engine: "tinyvoca", styles: ["F2", "M2"] }],
      },
    });
    expect((fetchMock.mock.calls[0] as unknown as [string])[0]).toBe("http://127.0.0.1:5555/v1/audio/voices");
  });

  it("speaks only what the embedder has files for", async () => {
    const listing = { voices: [{ id: "tr", default_engine: "ema", engines: [{ id: "ema", sample_rate: 48_000, styles: [] }] }] };
    const { service } = setup({ fetch: voicesFetch(listing) as unknown as typeof fetch });
    const caps = await service.capabilities();
    expect(caps.speak).toEqual({ tr: true, en: false });
    expect(caps.speakEngines).toEqual({ tr: [{ engine: "ema", styles: [] }], en: [] });
  });

  it("puts the listed default engine first and drops engines it does not know", async () => {
    const listing = {
      voices: [{ id: "en", default_engine: "tinyvoca", engines: [{ id: "kokoro", styles: ["F2"] }, { id: "ema", styles: [] }, TINYVOCA, "junk"] }],
    };
    const { service } = setup({ fetch: voicesFetch(listing) as unknown as typeof fetch });
    expect((await service.capabilities()).speakEngines.en).toEqual([
      { engine: "tinyvoca", styles: ["F2", "M2"] },
      { engine: "ema", styles: [] },
    ]);
  });

  it("speaks nothing when the embedder does not answer the list", async () => {
    for (const fetchMock of [voicesFetch({}, 404), voicesFetch({ voices: "no" }), vi.fn(async () => Promise.reject(new Error("refused")))]) {
      const { service } = setup({ fetch: fetchMock as unknown as typeof fetch, log: () => {} });
      expect((await service.capabilities()).speak).toEqual({ tr: false, en: false });
    }
  });

  it("does not listen without the helper or off macOS, and does not speak without the embedder", async () => {
    const { service } = setup({ platform: "win32", embedderUrl: () => Promise.resolve(null) });
    await expect(service.capabilities()).resolves.toEqual({
      listen: { tr: false, en: false },
      speak: { tr: false, en: false },
      speakEngines: { tr: [], en: [] },
    });
    const noHelper = setup({ speechBinary: () => null, fetch: voicesFetch(VOICES) as unknown as typeof fetch }).service;
    expect((await noHelper.capabilities()).listen.tr).toBe(false);
  });
});

describe("VoiceService listening", () => {
  it("runs the helper with the locale, streams audio in and transcripts out", async () => {
    const { service, children, events, spawnSpeech } = setup();
    const { id } = service.listenStart("tr");
    expect(spawnSpeech).toHaveBeenCalledWith("/app/bin/tt-speech", ["tr-TR"]);
    const child = children[0];

    service.listenAudio(id, new Uint8Array([1, 2, 3, 4]));
    child.say({ type: "partial", text: "merhaba" });
    service.listenStop(id);
    child.say({ type: "final", text: "Merhaba dünya" });
    await tick();

    expect(Buffer.concat(child.written)).toEqual(Buffer.from([1, 2, 3, 4]));
    expect(child.stdinEnded).toBe(true);
    const transcripts = events.filter((e) => e.channel === CLOUD_EVENTS.voiceTranscript).map((e) => e.payload);
    expect(transcripts).toEqual([
      { id, type: "partial", text: "merhaba" },
      { id, type: "final", text: "Merhaba dünya" },
    ]);

    child.emit("exit", 0, null);
    await tick();
    // The final already went out; exit does not send a second one.
    expect(events.filter((e) => e.channel === CLOUD_EVENTS.voiceTranscript)).toHaveLength(2);
  });

  it("ends the previous utterance when a new one starts, and ignores audio for unknown ids", () => {
    const { service, children } = setup();
    const first = service.listenStart("tr");
    service.listenStart("en");
    expect(children[0].killed).toBe("SIGTERM");
    service.listenAudio(first.id, new Uint8Array([9, 9]));
    expect(children[0].written).toHaveLength(0);
  });

  it("reports a helper that exits with an error code", async () => {
    const { service, children, events } = setup();
    const { id } = service.listenStart("tr");
    children[0].emit("exit", 1, null);
    await tick();
    expect(events.at(-1)?.payload).toMatchObject({ id, type: "error", code: "exit" });
  });

  it("refuses to listen where there is no recognizer", () => {
    const { service } = setup({ platform: "linux" });
    expect(() => service.listenStart("tr")).toThrow(/not available/);
  });
});

describe("VoiceService speaking", () => {
  it("streams whole float32 samples, carrying split bytes to the next chunk", async () => {
    const chunks = [new Uint8Array([0, 0, 128]), new Uint8Array([63, 0, 0, 0, 64, 1])];
    const fetchMock = vi.fn(async () =>
      new Response(
        new ReadableStream({
          start(controller) {
            for (const c of chunks) controller.enqueue(c);
            controller.close();
          },
        }),
        { status: 200, headers: { "X-Sample-Rate": "48000" } },
      ),
    );
    const { service, events } = setup({ fetch: fetchMock as unknown as typeof fetch });
    const { id } = await service.speak("tr", "Merhaba.", 1);
    await vi.waitFor(() => expect(events.some((e) => (e.payload as { done?: boolean }).done)).toBe(true));

    const [url, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toBe("http://127.0.0.1:5555/v1/audio/speech");
    expect(JSON.parse(init.body as string)).toEqual({ input: "Merhaba.", voice: "tr", speed: 1 });

    const audio = events.filter((e) => e.channel === CLOUD_EVENTS.voiceAudio).map((e) => e.payload as { pcm?: ArrayBuffer; done?: boolean; id: string });
    const pcm = audio.filter((a) => a.pcm).map((a) => new Float32Array(a.pcm!));
    expect(pcm.map((p) => Array.from(p))).toEqual([[1, 2]]);
    expect(audio.at(-1)).toMatchObject({ id, done: true });
    expect(audio.at(-1)).not.toHaveProperty("error");
  });

  it("ends with an error the person can read when the engine fails", async () => {
    const fetchMock = vi.fn(async () => new Response("nope", { status: 503 }));
    const { service, events } = setup({ fetch: fetchMock as unknown as typeof fetch, log: () => {} });
    const { id } = await service.speak("tr", "Merhaba.", 1);
    await vi.waitFor(() => expect(events).toHaveLength(1));
    expect(events[0].payload).toMatchObject({ id, done: true, error: expect.any(String) });
  });

  it("asks for English with the style, and forwards the 24 kHz rate the embedder announces", async () => {
    const fetchMock = vi.fn(async () => new Response(new Uint8Array([0, 0, 128, 63]), { status: 200, headers: { "X-Sample-Rate": "24000" } }));
    const { service, events } = setup({ fetch: fetchMock as unknown as typeof fetch });
    await service.speak("en", "Hello.", 1.25, { style: "M2" });
    await vi.waitFor(() => expect(events.some((e) => (e.payload as { done?: boolean }).done)).toBe(true));
    const [, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    expect(JSON.parse(init.body as string)).toEqual({ input: "Hello.", voice: "en", speed: 1.25, style: "M2" });
    expect(events.map((e) => (e.payload as { sampleRate: number }).sampleRate)).toEqual([24_000, 24_000]);
  });

  it("names the engine and style only when picked, for either language", async () => {
    const fetchMock = vi.fn(async () => new Response(new Uint8Array(0), { status: 200 }));
    const { service, events } = setup({ fetch: fetchMock as unknown as typeof fetch });
    await service.speak("tr", "Merhaba.", 1, { engine: "ema" });
    await service.speak("en", "Hello.", 1, { engine: "tinyvoca", style: "M2" });
    await service.speak("en", "Hello.", 1);
    await vi.waitFor(() => expect(events.filter((e) => (e.payload as { done?: boolean }).done)).toHaveLength(3));
    const bodies = fetchMock.mock.calls.map((c) => JSON.parse((c as unknown as [string, RequestInit])[1].body as string) as Record<string, unknown>);
    expect(bodies).toEqual([
      { input: "Merhaba.", voice: "tr", speed: 1, engine: "ema" },
      { input: "Hello.", voice: "en", speed: 1, engine: "tinyvoca", style: "M2" },
      { input: "Hello.", voice: "en", speed: 1 },
    ]);
  });

  it("ends a refused request with an error at the named engine's rate", async () => {
    const fetchMock = vi.fn(async () => new Response("{}", { status: 400 }));
    const { service, events } = setup({ fetch: fetchMock as unknown as typeof fetch, log: () => {} });
    await service.speak("en", "Hello.", 1, { engine: "tinyvoca", style: "nope" });
    await vi.waitFor(() => expect(events).toHaveLength(1));
    expect(events[0].payload).toMatchObject({ sampleRate: 24_000, done: true, error: expect.any(String) });
  });

  it("refuses to speak while the embedder is not running", async () => {
    const { service } = setup({ embedderUrl: () => Promise.resolve(null) });
    await expect(service.speak("en", "Hello.", 1)).rejects.toThrow(/not running/);
  });

  it("stays silent after a cancel", async () => {
    let abortSeen = false;
    const fetchMock = vi.fn(
      (_url: string, init: RequestInit) =>
        new Promise<Response>((_resolve, reject) => {
          init.signal?.addEventListener("abort", () => {
            abortSeen = true;
            reject(new DOMException("aborted", "AbortError"));
          });
        }),
    );
    const { service, events } = setup({ fetch: fetchMock as unknown as typeof fetch });
    const { id } = await service.speak("tr", "Merhaba.", 1);
    service.speakCancel(id);
    await tick();
    expect(abortSeen).toBe(true);
    expect(events).toHaveLength(0);
  });
});

describe("parseHelperLine", () => {
  it("maps each helper line, and drops what it does not know", () => {
    expect(parseHelperLine("x", '{"type":"ready","onDevice":true}')).toEqual({ id: "x", type: "ready" });
    expect(parseHelperLine("x", '{"type":"error","code":"denied","message":"off"}')).toEqual({
      id: "x",
      type: "error",
      code: "denied",
      message: "off",
    });
    expect(parseHelperLine("x", "not json")).toBeNull();
    expect(parseHelperLine("x", '{"type":"other"}')).toBeNull();
  });
});
