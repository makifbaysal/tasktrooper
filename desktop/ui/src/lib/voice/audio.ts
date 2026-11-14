/**
 * The microphone and the speaker for a voice conversation.
 *
 * The mic runs through Chromium's echo cancellation, so the agent's own voice
 * coming out of the speakers is (mostly) not heard back as the person talking —
 * that is what lets the person interrupt it. Audio leaves here as 16 kHz mono
 * s16le, about a tenth of a second per chunk, with a loudness level for the
 * turn-taking logic and the level meter.
 */

export const STT_RATE = 16_000;
const CHUNK_SAMPLES = 1_600;

const WORKLET = `
class TtDownsample extends AudioWorkletProcessor {
  constructor(options) {
    super();
    this.ratio = sampleRate / ${STT_RATE};
    this.pos = 0;
    this.buf = new Int16Array(${CHUNK_SAMPLES});
    this.n = 0;
    this.sumSq = 0;
  }
  process(inputs) {
    const ch = inputs[0] && inputs[0][0];
    if (!ch) return true;
    while (this.pos < ch.length) {
      const i = Math.floor(this.pos);
      const s = Math.max(-1, Math.min(1, ch[i]));
      this.buf[this.n++] = s < 0 ? s * 0x8000 : s * 0x7fff;
      this.sumSq += s * s;
      if (this.n === this.buf.length) {
        const rms = Math.sqrt(this.sumSq / this.n);
        this.port.postMessage({ pcm: this.buf.buffer, rms }, [this.buf.buffer]);
        this.buf = new Int16Array(${CHUNK_SAMPLES});
        this.n = 0;
        this.sumSq = 0;
      }
      this.pos += this.ratio;
    }
    this.pos -= ch.length;
    return true;
  }
}
registerProcessor("tt-downsample", TtDownsample);
`;

export interface MicChunk {
  pcm: ArrayBuffer;
  /** Root mean square of the chunk, 0..1. */
  rms: number;
}

export interface Mic {
  stop(): void;
}

export async function openMic(onChunk: (chunk: MicChunk) => void): Promise<Mic> {
  const stream = await navigator.mediaDevices.getUserMedia({
    audio: { echoCancellation: true, noiseSuppression: true, autoGainControl: true, channelCount: 1 },
  });
  const ctx = new AudioContext();
  const url = URL.createObjectURL(new Blob([WORKLET], { type: "application/javascript" }));
  try {
    await ctx.audioWorklet.addModule(url);
  } finally {
    URL.revokeObjectURL(url);
  }
  const source = ctx.createMediaStreamSource(stream);
  const node = new AudioWorkletNode(ctx, "tt-downsample");
  node.port.onmessage = (e: MessageEvent<MicChunk>) => onChunk(e.data);
  source.connect(node);
  return {
    stop() {
      node.port.onmessage = null;
      source.disconnect();
      node.disconnect();
      for (const track of stream.getTracks()) track.stop();
      void ctx.close();
    },
  };
}

/**
 * Plays float32 PCM chunks back to back, as they arrive, with no gap between
 * them. `stop()` silences everything queued at once (the person interrupted).
 */
export class PcmPlayer {
  #ctx: AudioContext | null = null;
  #nextAt = 0;
  #sources = new Set<AudioBufferSourceNode>();

  play(pcm: ArrayBuffer, sampleRate: number): void {
    const samples = new Float32Array(pcm);
    if (samples.length === 0) return;
    const ctx = (this.#ctx ??= new AudioContext());
    const buffer = ctx.createBuffer(1, samples.length, sampleRate);
    buffer.copyToChannel(samples, 0);
    const node = ctx.createBufferSource();
    node.buffer = buffer;
    node.connect(ctx.destination);
    // A small lead on the first chunk keeps the start from clipping.
    const at = Math.max(this.#nextAt, ctx.currentTime + 0.05);
    node.start(at);
    this.#nextAt = at + buffer.duration;
    this.#sources.add(node);
    node.onended = () => this.#sources.delete(node);
  }

  /** Seconds of audio still queued to play. */
  remaining(): number {
    if (!this.#ctx) return 0;
    return Math.max(0, this.#nextAt - this.#ctx.currentTime);
  }

  stop(): void {
    for (const node of this.#sources) {
      try {
        node.stop();
      } catch {
        // Already ended.
      }
    }
    this.#sources.clear();
    this.#nextAt = 0;
  }

  close(): void {
    this.stop();
    void this.#ctx?.close();
    this.#ctx = null;
  }
}
