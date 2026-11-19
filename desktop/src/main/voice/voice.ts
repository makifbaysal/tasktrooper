import { spawn, type ChildProcessWithoutNullStreams } from "node:child_process";
import { randomUUID } from "node:crypto";
import { createInterface } from "node:readline";
import { CLOUD_EVENTS } from "../../ipc/channels.js";
import {
  VOICE_ENGINES,
  type VoiceAudioEvent,
  type VoiceCapabilities,
  type VoiceChoice,
  type VoiceEngine,
  type VoiceEngineOption,
  type VoiceLanguage,
  type VoiceTranscriptEvent,
} from "../../ipc/types.js";

/**
 * Talking to an agent: what the person says becomes text (`listen*`), and what
 * the agent answers becomes sound (`speak*`). Both stay on this computer.
 *
 * Listening is macOS's own recognizer, on-device where the Mac has the model,
 * through the `tt-speech` helper (`desktop/speech/`): one process per
 * utterance, 16 kHz mono s16le PCM in on stdin, JSON lines out. The page owns
 * the microphone (so Chromium's echo cancellation is between the speaker and
 * what we hear) and streams PCM here.
 *
 * Speaking is the embedder's `POST /v1/audio/speech`, with engines bundled
 * with the app (EMA Lightning, Turkish, 48 kHz, one voice; tiny-voca,
 * English, 24 kHz, two voices): raw float32
 * PCM at the rate the embedder announces, passed to the page chunk by chunk
 * as it is made. Which engines this copy has per language, and their styles,
 * come from the embedder's `GET /v1/audio/voices`; a language without one is
 * read by the page with the OS voice.
 */

export const SPEECH_LOCALES: Record<VoiceLanguage, string> = { tr: "tr-TR", en: "en-US" };

/** An utterance nobody ends is ended for them: the recognizer and the mic stream are not left running. */
export const MAX_UTTERANCE_MS = 5 * 60_000;

/** A text longer than the embedder accepts; the page sends one sentence or so at a time. */
export const MAX_SPEAK_CHARS = 4000;

/** Each engine's rate, for an error event sent before the embedder named one. */
const SAMPLE_RATES: Record<VoiceEngine, number> = { ema: 48_000, tinyvoca: 24_000 };
/** The engine the embedder picks for a request that names none, when it has it. */
const DEFAULT_ENGINES: Record<VoiceLanguage, VoiceEngine> = { tr: "ema", en: "tinyvoca" };

/** The voices list is a loopback call to a process this app runs; past this, the voices count as missing. */
const VOICES_TIMEOUT_MS = 3_000;

interface ListedVoice {
  id: string;
  default_engine?: unknown;
  engines?: unknown;
}

export interface VoiceDeps {
  platform: NodeJS.Platform;
  /** The tt-speech binary, or null when this build or OS has none. */
  speechBinary(): string | null;
  embedderUrl(): Promise<string | null>;
  publish(channel: string, payload: unknown): void;
  fetch?: typeof fetch;
  spawnSpeech?: (binary: string, args: string[]) => ChildProcessWithoutNullStreams;
  log?: (line: string) => void;
}

interface Utterance {
  child: ChildProcessWithoutNullStreams;
  timer: NodeJS.Timeout;
  ended: boolean;
}

export class VoiceService {
  readonly #deps: VoiceDeps;
  readonly #utterances = new Map<string, Utterance>();
  readonly #speeches = new Map<string, AbortController>();

  constructor(deps: VoiceDeps) {
    this.#deps = deps;
  }

  async capabilities(): Promise<VoiceCapabilities> {
    const canListen = this.#deps.platform === "darwin" && this.#deps.speechBinary() !== null;
    const voices = await this.#voices();
    const tr = engineOptions(voices.find((v) => v.id === "tr"));
    const en = engineOptions(voices.find((v) => v.id === "en"));
    return {
      listen: { tr: canListen, en: canListen },
      speak: { tr: tr.length > 0, en: en.length > 0 },
      speakEngines: { tr, en },
    };
  }

  /** What the embedder can speak; none while it is not running or does not answer. */
  async #voices(): Promise<ListedVoice[]> {
    const base = await this.#deps.embedderUrl();
    if (!base) return [];
    try {
      const doFetch = this.#deps.fetch ?? fetch;
      const res = await doFetch(`${base}/v1/audio/voices`, { signal: AbortSignal.timeout(VOICES_TIMEOUT_MS) });
      if (!res.ok) return [];
      const body = (await res.json()) as { voices?: unknown };
      if (!Array.isArray(body.voices)) return [];
      return body.voices.filter((v): v is ListedVoice => typeof v === "object" && v !== null && typeof (v as ListedVoice).id === "string");
    } catch (err) {
      this.#deps.log?.(`[voice] listing voices failed: ${err instanceof Error ? err.message : String(err)}`);
      return [];
    }
  }

  /** One utterance at a time: a new one ends whatever was still listening. */
  listenStart(lang: VoiceLanguage): { id: string } {
    const binary = this.#deps.platform === "darwin" ? this.#deps.speechBinary() : null;
    if (!binary) throw new Error("Speech recognition is not available on this computer.");
    for (const id of [...this.#utterances.keys()]) this.listenCancel(id);

    const id = randomUUID();
    const spawnSpeech = this.#deps.spawnSpeech ?? ((bin, args) => spawn(bin, args, { shell: false, stdio: "pipe" }));
    const child = spawnSpeech(binary, [SPEECH_LOCALES[lang]]);
    const utterance: Utterance = {
      child,
      ended: false,
      timer: setTimeout(() => this.listenStop(id), MAX_UTTERANCE_MS),
    };
    utterance.timer.unref?.();
    this.#utterances.set(id, utterance);

    let finished = false;
    const finish = (event: VoiceTranscriptEvent): void => {
      if (finished) return;
      finished = true;
      this.#publishTranscript(event);
    };
    createInterface({ input: child.stdout }).on("line", (line) => {
      const event = parseHelperLine(id, line);
      if (!event) return;
      if (event.type === "partial" || event.type === "ready") this.#publishTranscript(event);
      else finish(event);
    });
    child.stdin.on("error", () => {
      // The helper exited first (an error it already reported); writes after
      // that have nowhere to go.
    });
    child.on("error", (err) => {
      finish({ id, type: "error", code: "spawn", message: err.message });
      this.#drop(id);
    });
    child.on("exit", (code, signal) => {
      if (code !== 0 && !signal) {
        finish({ id, type: "error", code: "exit", message: `speech recognition stopped (${code ?? "?"})` });
      } else {
        finish({ id, type: "final", text: "" });
      }
      this.#drop(id);
    });
    return { id };
  }

  listenAudio(id: string, pcm: Uint8Array): void {
    const utterance = this.#utterances.get(id);
    if (!utterance || utterance.ended || utterance.child.stdin.destroyed) return;
    utterance.child.stdin.write(pcm);
  }

  /** The person is done talking: the recognizer gets end-of-audio and answers with its final text. */
  listenStop(id: string): void {
    const utterance = this.#utterances.get(id);
    if (!utterance || utterance.ended) return;
    utterance.ended = true;
    utterance.child.stdin.end();
  }

  listenCancel(id: string): void {
    const utterance = this.#utterances.get(id);
    if (!utterance) return;
    this.#drop(id);
    utterance.child.kill("SIGTERM");
  }

  /**
   * Starts speaking `text`; the audio arrives as `voice:audio` events carrying
   * the returned id. `voice` picks the engine and its speaker, each left to
   * the embedder's default when absent; one the embedder refuses (a style for
   * EMA, an engine that does not read `lang`) ends in an error event.
   */
  async speak(lang: VoiceLanguage, text: string, speed: number, voice: VoiceChoice = {}): Promise<{ id: string }> {
    const base = await this.#deps.embedderUrl();
    if (!base) throw new Error("The voice engine is not running.");
    const id = randomUUID();
    const controller = new AbortController();
    this.#speeches.set(id, controller);
    void this.#stream(id, base, { lang, text, speed, engine: voice.engine, style: voice.style }, controller.signal);
    return { id };
  }

  speakCancel(id: string): void {
    this.#speeches.get(id)?.abort();
    this.#speeches.delete(id);
  }

  /** Quit: no helper or request outlives the app. */
  stopAll(): void {
    for (const id of [...this.#utterances.keys()]) this.listenCancel(id);
    for (const id of [...this.#speeches.keys()]) this.speakCancel(id);
  }

  async #stream(
    id: string,
    base: string,
    request: { lang: VoiceLanguage; text: string; speed: number; engine: VoiceEngine | undefined; style: string | undefined },
    signal: AbortSignal,
  ): Promise<void> {
    const { lang, text, speed, engine, style } = request;
    const fallbackRate = SAMPLE_RATES[engine ?? DEFAULT_ENGINES[lang]];
    const doFetch = this.#deps.fetch ?? fetch;
    try {
      const res = await doFetch(`${base}/v1/audio/speech`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ input: text, voice: lang, speed, ...(engine ? { engine } : {}), ...(style ? { style } : {}) }),
        signal,
      });
      if (!res.ok || !res.body) throw new Error(`voice engine answered ${res.status}`);
      const sampleRate = Number(res.headers.get("x-sample-rate")) || fallbackRate;
      const reader = res.body.getReader();
      // A network chunk can split a float; carry the odd bytes to the next one.
      let carry = new Uint8Array(0);
      for (;;) {
        const { done, value } = await reader.read();
        if (done) break;
        const bytes = concat(carry, value);
        const whole = bytes.length - (bytes.length % 4);
        carry = bytes.slice(whole);
        if (whole > 0) this.#publishAudio({ id, sampleRate, pcm: bytes.slice(0, whole).buffer });
      }
      this.#publishAudio({ id, sampleRate, done: true });
    } catch (err) {
      if (!signal.aborted) {
        this.#deps.log?.(`[voice] speak failed: ${err instanceof Error ? err.message : String(err)}`);
        this.#publishAudio({ id, sampleRate: fallbackRate, done: true, error: "The voice engine could not read this." });
      }
    } finally {
      this.#speeches.delete(id);
    }
  }

  #drop(id: string): void {
    const utterance = this.#utterances.get(id);
    if (!utterance) return;
    clearTimeout(utterance.timer);
    this.#utterances.delete(id);
  }

  #publishTranscript(event: VoiceTranscriptEvent): void {
    this.#deps.publish(CLOUD_EVENTS.voiceTranscript, event);
  }

  #publishAudio(event: VoiceAudioEvent): void {
    this.#deps.publish(CLOUD_EVENTS.voiceAudio, event);
  }
}

/** A language's engines from the embedder's listing: known ids only, the default first, each one's default style first. */
function engineOptions(voice: ListedVoice | undefined): VoiceEngineOption[] {
  if (!voice || !Array.isArray(voice.engines)) return [];
  const options: VoiceEngineOption[] = [];
  for (const raw of voice.engines as unknown[]) {
    if (typeof raw !== "object" || raw === null) continue;
    const { id, styles, default_style: defaultStyle } = raw as { id?: unknown; styles?: unknown; default_style?: unknown };
    if (typeof id !== "string" || !(VOICE_ENGINES as readonly string[]).includes(id) || options.some((o) => o.engine === id)) continue;
    const listed = Array.isArray(styles) ? styles.filter((s): s is string => typeof s === "string") : [];
    const first = typeof defaultStyle === "string" && listed.includes(defaultStyle) ? defaultStyle : null;
    options.push({ engine: id as VoiceEngine, styles: first ? [first, ...listed.filter((s) => s !== first)] : listed });
  }
  const preferred = options.findIndex((o) => o.engine === voice.default_engine);
  if (preferred > 0) options.unshift(...options.splice(preferred, 1));
  return options;
}

export function parseHelperLine(id: string, line: string): VoiceTranscriptEvent | null {
  let raw: unknown;
  try {
    raw = JSON.parse(line);
  } catch {
    return null;
  }
  if (!raw || typeof raw !== "object") return null;
  const obj = raw as Record<string, unknown>;
  const text = typeof obj.text === "string" ? obj.text : "";
  switch (obj.type) {
    case "ready":
      return { id, type: "ready" };
    case "partial":
      return { id, type: "partial", text };
    case "final":
      return { id, type: "final", text };
    case "error":
      return {
        id,
        type: "error",
        code: typeof obj.code === "string" ? obj.code : "recognition",
        message: typeof obj.message === "string" ? obj.message : "speech recognition failed",
      };
    default:
      return null;
  }
}

function concat(a: Uint8Array, b: Uint8Array): Uint8Array {
  if (a.length === 0) return b;
  const out = new Uint8Array(a.length + b.length);
  out.set(a);
  out.set(b, a.length);
  return out;
}
