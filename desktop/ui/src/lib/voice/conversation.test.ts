import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { DesktopVoiceAudioEvent, DesktopVoiceHost, DesktopVoiceTranscriptEvent } from "@/lib/desktop-bridge";
import type { MicChunk } from "./audio";
import { END_OF_TURN_MS, VoiceConversation, type VoiceSnapshot } from "./conversation";

function fakeHost() {
  let transcriptCb: ((e: DesktopVoiceTranscriptEvent) => void) | null = null;
  let audioCb: ((e: DesktopVoiceAudioEvent) => void) | null = null;
  let n = 0;
  const host = {
    capabilities: vi.fn(),
    listenStart: vi.fn(async () => ({ id: `u${++n}` })),
    listenAudio: vi.fn(async () => {}),
    listenStop: vi.fn(async () => {}),
    listenCancel: vi.fn(async () => {}),
    onTranscript: vi.fn((cb: (e: DesktopVoiceTranscriptEvent) => void) => {
      transcriptCb = cb;
      return () => (transcriptCb = null);
    }),
    speak: vi.fn(async () => ({ id: `s${++n}` })),
    speakCancel: vi.fn(async () => {}),
    onAudio: vi.fn((cb: (e: DesktopVoiceAudioEvent) => void) => {
      audioCb = cb;
      return () => (audioCb = null);
    }),
  } satisfies DesktopVoiceHost;
  return {
    host,
    transcript: (e: DesktopVoiceTranscriptEvent) => transcriptCb?.(e),
    audio: (e: DesktopVoiceAudioEvent) => audioCb?.(e),
  };
}

const flush = () => vi.advanceTimersByTimeAsync(0);

describe("VoiceConversation", () => {
  let now = 0;
  let micCb: ((c: MicChunk) => void) | null = null;
  let snapshots: VoiceSnapshot[] = [];
  const pcm = () => new Int16Array(1600).buffer;
  const mic = (rms: number, ms = 100) => {
    now += ms;
    micCb?.({ pcm: pcm(), rms });
  };

  beforeEach(() => {
    vi.useFakeTimers();
    now = 0;
    snapshots = [];
  });
  afterEach(() => vi.useRealTimers());

  function setup(bargeIn = true) {
    const fake = fakeHost();
    const onUtterance = vi.fn();
    const onInterrupt = vi.fn();
    const conv = new VoiceConversation({
      host: fake.host,
      lang: "tr",
      bundledVoice: true,
      bargeIn,
      onChange: (s) => snapshots.push(s),
      onUtterance,
      onInterrupt,
      now: () => now,
      openMic: async (cb) => {
        micCb = cb;
        return { stop: () => (micCb = null) };
      },
    });
    return { ...fake, conv, onUtterance, onInterrupt };
  }

  async function toThinking(t: ReturnType<typeof setup>) {
    await t.conv.start();
    mic(0.2);
    t.transcript({ id: "u1", type: "partial", text: "merhaba" });
    mic(0.001, END_OF_TURN_MS + 50);
    expect(t.host.listenStop).toHaveBeenCalledWith("u1");
    t.transcript({ id: "u1", type: "final", text: "Merhaba agent" });
  }

  it("listens, ends the utterance after a pause and sends what was said", async () => {
    const t = setup();
    await t.conv.start();
    expect(t.host.listenStart).toHaveBeenCalledWith("tr");
    expect(t.conv.snapshot.state).toBe("listening");

    mic(0.2);
    expect(t.host.listenAudio).toHaveBeenCalledWith("u1", expect.any(ArrayBuffer));
    t.transcript({ id: "u1", type: "partial", text: "merhaba" });
    expect(t.conv.snapshot.transcript).toBe("merhaba");

    // A short pause is not the end of the turn.
    mic(0.001, 400);
    expect(t.host.listenStop).not.toHaveBeenCalled();
    mic(0.001, END_OF_TURN_MS);
    expect(t.host.listenStop).toHaveBeenCalledWith("u1");

    t.transcript({ id: "u1", type: "final", text: "Merhaba agent" });
    expect(t.onUtterance).toHaveBeenCalledWith("Merhaba agent");
    expect(t.conv.snapshot.state).toBe("thinking");
  });

  it("does not end a turn on silence when nothing was said", async () => {
    const t = setup();
    await t.conv.start();
    mic(0.001, 5_000);
    expect(t.host.listenStop).not.toHaveBeenCalled();
  });

  it("reads the reply sentence by sentence, then listens again", async () => {
    const t = setup();
    await toThinking(t);

    t.conv.replyProgress("Tamam");
    expect(t.conv.snapshot.state).toBe("thinking");
    // A finished sentence is read while the rest of its line is still coming.
    t.conv.replyProgress("Tamam. Hemen ");
    expect(t.conv.snapshot.state).toBe("speaking");
    t.conv.replyProgress("Tamam. Hemen bakıyorum.\n");
    await flush();
    expect(t.host.speak).toHaveBeenCalledWith("tr", "Tamam.", undefined, undefined);

    t.audio({ id: "s2", sampleRate: 48000, done: true });
    await flush();
    expect(t.host.speak).toHaveBeenLastCalledWith("tr", "Hemen bakıyorum.", undefined, undefined);

    t.conv.replyDone("Tamam. Hemen bakıyorum.\n");
    t.audio({ id: "s3", sampleRate: 48000, done: true });
    await vi.advanceTimersByTimeAsync(500);
    expect(t.conv.snapshot.state).toBe("listening");
    expect(t.host.listenStart).toHaveBeenCalledTimes(2);
  });

  it("drops what is not said yet of a stretch that turned out to be thinking", async () => {
    const t = setup();
    await toThinking(t);
    t.conv.replyProgress("Bir bakayım. Board'u açıyorum. Sonra");
    await flush();
    expect(t.host.speak).toHaveBeenCalledTimes(1);
    t.conv.replyStretchClosed();
    t.audio({ id: "s2", sampleRate: 48000, done: true });
    await flush();
    expect(t.host.speak).toHaveBeenCalledTimes(1);
    t.conv.replyProgress("Üç iş var.\n");
    await flush();
    expect(t.host.speak).toHaveBeenLastCalledWith("tr", "Üç iş var.", undefined, undefined);
  });

  it("goes back to listening when the turn fails", async () => {
    const t = setup();
    await toThinking(t);
    t.conv.replyDone(null);
    await vi.advanceTimersByTimeAsync(500);
    expect(t.conv.snapshot.state).toBe("listening");
  });

  it("never stops for loudness alone (a fan, a door), nor for misheard echo", async () => {
    const t = setup();
    await toThinking(t);
    t.conv.replyProgress("Look at the board for the team.\n");
    await flush();
    mic(0.001);
    await flush();
    const bargeId = (await t.host.listenStart.mock.results.at(-1)!.value).id;
    for (let i = 0; i < 10; i++) mic(0.4);
    t.transcript({ id: bargeId, type: "partial", text: "it" });
    t.transcript({ id: bargeId, type: "partial", text: "look at the bored for a teams" });
    expect(t.onInterrupt).not.toHaveBeenCalled();
    expect(t.conv.snapshot.state).toBe("speaking");
  });

  it("stops for two new words while someone is loud over the bundled voice", async () => {
    const t = setup();
    await toThinking(t);
    t.conv.replyProgress("Uzun bir cevap veriyorum.\n");
    await flush();
    mic(0.001);
    await flush();
    const bargeId = (await t.host.listenStart.mock.results.at(-1)!.value).id;
    t.transcript({ id: bargeId, type: "partial", text: "dur bekle" });
    expect(t.onInterrupt).not.toHaveBeenCalled();
    mic(0.3);
    mic(0.3);
    mic(0.3);
    t.transcript({ id: bargeId, type: "partial", text: "dur bekle" });
    expect(t.onInterrupt).toHaveBeenCalled();
    expect(t.host.speakCancel).toHaveBeenCalled();
    expect(t.conv.snapshot.state).toBe("listening");
  });

  it("interrupts on words the agent did not say, and keeps listening to them", async () => {
    const t = setup();
    await toThinking(t);
    t.conv.replyProgress("Uzun bir cevap veriyorum.\n");
    await flush();
    mic(0.001);
    await flush();
    const bargeId = (await t.host.listenStart.mock.results.at(-1)!.value).id;
    mic(0.001);
    expect(t.host.listenAudio).toHaveBeenLastCalledWith(bargeId, expect.any(ArrayBuffer));

    // Its own voice coming back through the mic is not an interruption.
    t.transcript({ id: bargeId, type: "partial", text: "uzun bir cevap" });
    expect(t.onInterrupt).not.toHaveBeenCalled();

    t.transcript({ id: bargeId, type: "partial", text: "uzun bir cevap dur bekle" });
    expect(t.onInterrupt).not.toHaveBeenCalled();
    t.transcript({ id: bargeId, type: "partial", text: "uzun bir cevap dur bekle lütfen" });
    expect(t.onInterrupt).toHaveBeenCalled();
    expect(t.conv.snapshot).toMatchObject({ state: "listening", transcript: "dur bekle lütfen" });
    expect(t.host.listenCancel).not.toHaveBeenCalledWith(bargeId);

    mic(0.001, END_OF_TURN_MS + 50);
    expect(t.host.listenStop).toHaveBeenCalledWith(bargeId);
    t.transcript({ id: bargeId, type: "final", text: "Uzun bir cevap dur bekle şunu yap" });
    expect(t.onUtterance).toHaveBeenLastCalledWith("dur bekle şunu yap");
  });

  it("does not interrupt when barge-in is off", async () => {
    const t = setup(false);
    await toThinking(t);
    t.conv.replyProgress("Cevap.\n");
    await flush();
    for (let i = 0; i < 10; i++) mic(0.5);
    expect(t.onInterrupt).not.toHaveBeenCalled();
  });

  it("reports a refused microphone and stays off", async () => {
    const fake = fakeHost();
    const conv = new VoiceConversation({
      host: fake.host,
      lang: "tr",
      bundledVoice: true,
      onChange: (s) => snapshots.push(s),
      onUtterance: vi.fn(),
      onInterrupt: vi.fn(),
      openMic: async () => {
        throw new DOMException("no", "NotAllowedError");
      },
    });
    await conv.start();
    expect(conv.snapshot).toMatchObject({ state: "off", error: "microphone-denied" });
    expect(fake.host.listenStart).not.toHaveBeenCalled();
  });
});
