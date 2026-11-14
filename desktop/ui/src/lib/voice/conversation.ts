import type {
  DesktopVoiceAudioEvent,
  DesktopVoiceChoice,
  DesktopVoiceHost,
  DesktopVoiceLanguage,
  DesktopVoiceTranscriptEvent,
} from "@/lib/desktop-bridge";
import { openMic, PcmPlayer, type Mic, type MicChunk } from "./audio";
import { osVoiceLang, pickOsVoice } from "./osVoice";
import { SentenceFeed } from "./speechText";

/**
 * One voice conversation with an agent, as a small state machine:
 *
 *   listening → (the person stops talking) → thinking → speaking → listening
 *
 * - listening: the mic streams to the recognizer; the live transcript is shown.
 *   A pause after speech ends the utterance and sends what was said.
 * - thinking: the agent works; nothing is heard.
 * - speaking: each complete sentence of the reply is read as soon as it is
 *   written. Meanwhile a recognizer listens too: words that are not the
 *   agent's own (its echo) mean the person is talking over it (barge-in), so
 *   the voice and the run stop and that same utterance becomes the person's
 *   turn. With the bundled voice, which the page plays and so echo
 *   cancellation removes, sustained loud input interrupts as well.
 *
 * Everything the page needs comes out through `onChange`; the page feeds the
 * reply in through `replyProgress` / `replyDone`.
 */

export type VoiceState = "starting" | "listening" | "thinking" | "speaking" | "off";

export interface VoiceSnapshot {
  state: VoiceState;
  /** What the person is saying right now (listening), or just said. */
  transcript: string;
  /** Mic loudness 0..1, for the meter. */
  level: number;
  error: string | null;
}

export interface VoiceConversationOptions {
  host: DesktopVoiceHost;
  lang: DesktopVoiceLanguage;
  /** A bundled voice exists for `lang`; otherwise the OS voice reads it. */
  bundledVoice: boolean;
  speed?: number;
  /** The bundled model and voice to read with; absent = the language's default. */
  voice?: DesktopVoiceChoice;
  bargeIn?: boolean;
  onChange(snapshot: VoiceSnapshot): void;
  /** The person finished an utterance: send it to the agent as a spoken turn. */
  onUtterance(text: string): void;
  /** The person talked over the reply: stop the run if it is still going. */
  onInterrupt(): void;
  openMic?: typeof openMic;
  now?: () => number;
}

/** Silence after speech that ends an utterance. */
export const END_OF_TURN_MS = 1_100;
/** Of the last BARGE_WINDOW mic chunks (0.1 s each), this many loud ones mean someone is talking. */
export const BARGE_LOUD_CHUNKS = 3;
export const BARGE_WINDOW = 5;
/**
 * Words heard while speaking that the agent did not say, before it counts as
 * an interruption: BARGE_WORDS on their own, BARGE_WORDS_LOUD while the mic is
 * also loud (bundled voice only — the OS voice's own loudness reaches the mic).
 * Loudness alone never interrupts: a fan or a door is loud too.
 */
export const BARGE_WORDS = 3;
export const BARGE_WORDS_LOUD = 2;
/** Chunks (0.1 s each) kept so the start of an interruption is not lost. */
const PREROLL_CHUNKS = 8;

export class VoiceConversation {
  readonly #o: VoiceConversationOptions;
  readonly #now: () => number;
  #snap: VoiceSnapshot = {
    state: "off",
    transcript: "",
    level: 0,
    error: null,
  };
  #mic: Mic | null = null;
  #unsubs: Array<() => void> = [];

  // listening
  #utteranceId: string | null = null;
  #stopping = false;
  #heardSpeech = false;
  #lastLoudAt = 0;
  #noiseFloor = 0.01;
  #preroll: ArrayBuffer[] = [];

  // speaking
  readonly #feed = new SentenceFeed();
  readonly #player = new PcmPlayer();
  #queue: string[] = [];
  #replyFinished = false;
  #speakingId: string | null = null;
  #synthBusy = false;
  #drainTimer: ReturnType<typeof setTimeout> | null = null;
  #loudHistory: boolean[] = [];
  // The recognizer that listens while the agent speaks, for barge-in.
  #bargeId: string | null = null;
  #bargeStarting = false;
  // Words of the reply read so far: what the recognizer hears of them is echo.
  #echoWords = new Set<string>();
  // An utterance taken over from barge-in starts with echo; it is trimmed off.
  #trimEcho = false;
  // Audio for an id whose speak() answer has not arrived yet.
  #early = new Map<string, DesktopVoiceAudioEvent[]>();

  constructor(options: VoiceConversationOptions) {
    this.#o = options;
    this.#now = options.now ?? (() => Date.now());
  }

  get snapshot(): VoiceSnapshot {
    return this.#snap;
  }

  async start(): Promise<void> {
    if (this.#snap.state !== "off") return;
    this.#set({ state: "starting", transcript: "", error: null });
    // Chromium fills the voice list lazily; asking now has it ready for the first reply.
    if (!this.#o.bundledVoice && typeof speechSynthesis !== "undefined") speechSynthesis.getVoices();
    this.#unsubs.push(this.#o.host.onTranscript((e) => this.#onTranscript(e)));
    this.#unsubs.push(this.#o.host.onAudio((e) => this.#onAudio(e)));
    try {
      this.#mic = await (this.#o.openMic ?? openMic)((chunk) => this.#onMic(chunk));
    } catch (err) {
      this.stop();
      this.#set({ error: micError(err) });
      return;
    }
    // stop() may have run while the mic was opening.
    if ((this.#snap.state as VoiceState) !== "starting") return;
    await this.#listen();
  }

  stop(): void {
    this.#mic?.stop();
    this.#mic = null;
    for (const off of this.#unsubs) off();
    this.#unsubs = [];
    if (this.#utteranceId) void this.#o.host.listenCancel(this.#utteranceId);
    this.#utteranceId = null;
    this.#cancelBarge();
    this.#silence();
    this.#player.close();
    this.#set({ state: "off", level: 0 });
  }

  /** The reply so far, as it streams. */
  replyProgress(markdown: string): void {
    if (this.#snap.state !== "thinking" && this.#snap.state !== "speaking") return;
    this.#enqueue(this.#feed.push(markdown));
  }

  /**
   * The stretch being written turned out to be thinking (a tool call followed
   * it): what of it is not said yet is dropped, and the next stretch is read
   * from its start.
   */
  replyStretchClosed(): void {
    if (this.#snap.state !== "thinking" && this.#snap.state !== "speaking") return;
    this.#queue = [];
    this.#feed.reset();
  }

  /** The turn ended: the final reply, or null when it failed or was stopped. */
  replyDone(markdown: string | null): void {
    if (this.#snap.state !== "thinking" && this.#snap.state !== "speaking") return;
    if (markdown) this.#enqueue(this.#feed.finish(markdown));
    this.#replyFinished = true;
    this.#maybeBackToListening();
  }

  // --- listening -------------------------------------------------------------

  #resetTurn(): void {
    this.#feed.reset();
    this.#queue = [];
    this.#replyFinished = false;
    this.#heardSpeech = false;
    this.#stopping = false;
    this.#loudHistory = [];
  }

  async #listen(): Promise<void> {
    this.#cancelBarge();
    this.#resetTurn();
    this.#trimEcho = false;
    this.#set({ state: "listening", transcript: "" });
    try {
      const { id } = await this.#o.host.listenStart(this.#o.lang);
      if (this.#snap.state !== "listening") {
        void this.#o.host.listenCancel(id);
        return;
      }
      this.#utteranceId = id;
      // What was said while the recognizer started (or just before a barge-in).
      for (const pcm of this.#preroll) void this.#o.host.listenAudio(id, pcm);
      this.#preroll = [];
    } catch (err) {
      this.stop();
      this.#set({ error: err instanceof Error ? err.message : String(err) });
    }
  }

  #onMic({ pcm, rms }: MicChunk): void {
    const now = this.#now();
    const quiet = rms < Math.max(0.015, this.#noiseFloor * 3);
    if (quiet) this.#noiseFloor = this.#noiseFloor * 0.95 + rms * 0.05;
    this.#set({ level: Math.min(1, rms * 8) });

    switch (this.#snap.state) {
      case "listening": {
        if (!this.#utteranceId) {
          this.#keepPreroll(pcm);
          return;
        }
        if (this.#stopping) return;
        void this.#o.host.listenAudio(this.#utteranceId, pcm);
        if (!quiet) {
          this.#heardSpeech = true;
          this.#lastLoudAt = now;
        } else if (this.#heardSpeech && this.#snap.transcript.trim() && now - this.#lastLoudAt > END_OF_TURN_MS) {
          this.#stopping = true;
          void this.#o.host.listenStop(this.#utteranceId);
        }
        return;
      }
      case "speaking": {
        this.#keepPreroll(pcm);
        if (!this.#o.bargeIn) return;
        if (this.#bargeId) void this.#o.host.listenAudio(this.#bargeId, pcm);
        else this.#startBarge();
        this.#loudHistory.push(rms > Math.max(0.05, this.#noiseFloor * 6));
        if (this.#loudHistory.length > BARGE_WINDOW) this.#loudHistory.shift();
        return;
      }
      default:
        this.#keepPreroll(pcm);
    }
  }

  #keepPreroll(pcm: ArrayBuffer): void {
    this.#preroll.push(pcm);
    if (this.#preroll.length > PREROLL_CHUNKS) this.#preroll.shift();
  }

  #onTranscript(e: DesktopVoiceTranscriptEvent): void {
    if (e.id === this.#bargeId) {
      this.#onBargeTranscript(e);
      return;
    }
    if (e.id !== this.#utteranceId) return;
    if (e.type === "partial") {
      this.#set({
        transcript: this.#trimEcho ? this.#withoutEcho(e.text) : e.text,
      });
      return;
    }
    if (e.type === "ready") return;
    this.#utteranceId = null;
    if (e.type === "error") {
      this.stop();
      this.#set({ error: e.message });
      return;
    }
    const heard = e.text ? (this.#trimEcho ? this.#withoutEcho(e.text) : e.text) : this.#snap.transcript;
    // An interruption that turns out to be only echo and a stray word is not a turn.
    if (this.#trimEcho && this.#novelWords(heard) < BARGE_WORDS_LOUD) {
      void this.#listen();
      return;
    }
    this.#send(heard.trim());
  }

  #send(text: string): void {
    if (!text) {
      void this.#listen();
      return;
    }
    this.#trimEcho = false;
    this.#echoWords = new Set();
    this.#set({ state: "thinking", transcript: text });
    this.#o.onUtterance(text);
  }

  // --- barge-in --------------------------------------------------------------

  #startBarge(): void {
    if (this.#bargeId || this.#bargeStarting) return;
    this.#bargeStarting = true;
    this.#o.host.listenStart(this.#o.lang).then(
      ({ id }) => {
        this.#bargeStarting = false;
        if (this.#snap.state !== "speaking") {
          void this.#o.host.listenCancel(id);
          return;
        }
        this.#bargeId = id;
      },
      () => {
        // Barge-in by words is a nicety; the conversation goes on without it.
        this.#bargeStarting = false;
      },
    );
  }

  #cancelBarge(): void {
    if (this.#bargeId) void this.#o.host.listenCancel(this.#bargeId);
    this.#bargeId = null;
  }

  #onBargeTranscript(e: DesktopVoiceTranscriptEvent): void {
    if (e.type === "ready") return;
    if (e.type === "partial") {
      if (this.#snap.state === "speaking" && this.#isTalkingOver(e.text)) this.#interrupt(e.text);
      return;
    }
    // The recognizer ended on its own (its silence limit, an error) while the
    // agent was still talking.
    this.#bargeId = null;
    if (this.#snap.state !== "speaking") return;
    const text = e.type === "final" ? e.text : "";
    if (this.#isTalkingOver(text)) {
      this.#silence();
      this.#o.onInterrupt();
      this.#resetTurn();
      this.#send(this.#withoutEcho(text).trim());
    }
  }

  /**
   * The person talked over the reply: the voice and the run stop, and the
   * barge-in recognizer, which already has their first words, becomes the
   * utterance being listened to.
   */
  #interrupt(heard = ""): void {
    this.#silence();
    this.#o.onInterrupt();
    const id = this.#bargeId;
    if (!id) {
      void this.#listen();
      return;
    }
    this.#bargeId = null;
    this.#resetTurn();
    this.#utteranceId = id;
    this.#trimEcho = true;
    this.#heardSpeech = true;
    this.#lastLoudAt = this.#now();
    this.#preroll = [];
    this.#set({
      state: "listening",
      transcript: heard ? this.#withoutEcho(heard) : "",
    });
  }

  #words(text: string): string[] {
    return text
      .toLocaleLowerCase(this.#o.lang)
      .split(/[^\p{L}\p{N}']+/u)
      .filter(Boolean);
  }

  #isTalkingOver(text: string): boolean {
    const novel = this.#novelWords(text);
    if (novel >= BARGE_WORDS) return true;
    const loud = this.#o.bundledVoice && this.#loudHistory.filter(Boolean).length >= BARGE_LOUD_CHUNKS;
    return loud && novel >= BARGE_WORDS_LOUD;
  }

  /**
   * Words the agent did not just say. The recognizer hears the agent's own
   * voice slightly wrong ("bored" for "board"), so a near match is echo too,
   * and very short words ("it", "a") are too easily made out of noise to count.
   */
  #novelWords(text: string): number {
    return this.#words(text).filter((w) => w.length >= 3 && !this.#isEcho(w)).length;
  }

  #isEcho(word: string): boolean {
    if (this.#echoWords.has(word)) return true;
    for (const said of this.#echoWords) {
      if (Math.abs(said.length - word.length) > 2) continue;
      if (said.length >= 4 && word.length >= 4 && said.slice(0, 4) === word.slice(0, 4)) return true;
      if (editDistance(said, word) <= (word.length >= 6 ? 2 : 1)) return true;
    }
    return false;
  }

  /** The heard text without the agent's own words it starts with. */
  #withoutEcho(text: string): string {
    const parts = text.trim().split(/\s+/);
    let i = 0;
    while (i < parts.length && this.#words(parts[i]).every((w) => w.length < 3 || this.#isEcho(w))) i++;
    return parts.slice(i).join(" ");
  }

  // --- speaking --------------------------------------------------------------

  #enqueue(sentences: string[]): void {
    if (sentences.length === 0) return;
    this.#queue.push(...sentences);
    if (this.#snap.state === "thinking") this.#set({ state: "speaking" });
    this.#speakNext();
  }

  #speakNext(): void {
    if (this.#speakingId || this.#synthBusy) return;
    const text = this.#queue.shift();
    if (text === undefined) {
      this.#maybeBackToListening();
      return;
    }
    for (const w of this.#words(text)) this.#echoWords.add(w);
    if (this.#o.bundledVoice) {
      this.#speakingId = "pending";
      this.#o.host.speak(this.#o.lang, text, this.#o.speed, this.#o.voice).then(
        ({ id }) => {
          const early = this.#early.get(id) ?? [];
          this.#early.clear();
          if (this.#speakingId !== "pending") {
            void this.#o.host.speakCancel(id);
            return;
          }
          this.#speakingId = id;
          for (const e of early) this.#onAudio(e);
        },
        () => {
          this.#speakingId = null;
          this.#speakNext();
        },
      );
      return;
    }
    if (typeof speechSynthesis === "undefined") return;
    const utterance = new SpeechSynthesisUtterance(text);
    utterance.lang = osVoiceLang(this.#o.lang);
    const voice = pickOsVoice(speechSynthesis.getVoices(), this.#o.lang);
    if (voice) utterance.voice = voice;
    utterance.rate = this.#o.speed ?? 1;
    this.#synthBusy = true;
    const next = () => {
      this.#synthBusy = false;
      this.#speakNext();
    };
    utterance.onend = next;
    utterance.onerror = next;
    speechSynthesis.speak(utterance);
  }

  #onAudio(e: DesktopVoiceAudioEvent): void {
    if (this.#speakingId === "pending") {
      const list = this.#early.get(e.id) ?? [];
      list.push(e);
      this.#early.set(e.id, list);
      return;
    }
    if (e.id !== this.#speakingId) return;
    if (e.pcm) this.#player.play(e.pcm, e.sampleRate);
    if (e.done) {
      // Generation is done; playback may still be going. The next sentence is
      // made now so it is ready when this one ends.
      this.#speakingId = null;
      this.#speakNext();
    }
  }

  #maybeBackToListening(): void {
    if (this.#snap.state !== "speaking" && this.#snap.state !== "thinking") return;
    if (!this.#replyFinished || this.#queue.length > 0 || this.#speakingId || this.#synthBusy) return;
    if (this.#drainTimer) clearTimeout(this.#drainTimer);
    const left = this.#player.remaining();
    this.#drainTimer = setTimeout(
      () => {
        this.#drainTimer = null;
        if (this.#player.remaining() > 0.05) {
          this.#maybeBackToListening();
          return;
        }
        if (this.#snap.state === "speaking" || this.#snap.state === "thinking") void this.#listen();
      },
      left * 1000 + 150,
    );
  }

  #silence(): void {
    if (this.#drainTimer) clearTimeout(this.#drainTimer);
    this.#drainTimer = null;
    this.#queue = [];
    if (this.#speakingId && this.#speakingId !== "pending") void this.#o.host.speakCancel(this.#speakingId);
    this.#speakingId = null;
    this.#player.stop();
    if (this.#synthBusy && typeof speechSynthesis !== "undefined") speechSynthesis.cancel();
    this.#synthBusy = false;
  }

  #set(patch: Partial<VoiceSnapshot>): void {
    const next = { ...this.#snap, ...patch };
    if (
      next.state === this.#snap.state &&
      next.transcript === this.#snap.transcript &&
      next.error === this.#snap.error &&
      Math.abs(next.level - this.#snap.level) < 0.02
    ) {
      return;
    }
    this.#snap = next;
    this.#o.onChange(next);
  }
}

function micError(err: unknown): string {
  if (err instanceof DOMException && (err.name === "NotAllowedError" || err.name === "SecurityError")) {
    return "microphone-denied";
  }
  if (err instanceof DOMException && err.name === "NotFoundError") return "microphone-missing";
  return err instanceof Error ? err.message : String(err);
}

function editDistance(a: string, b: string): number {
  let prev = Array.from({ length: b.length + 1 }, (_, j) => j);
  for (let i = 1; i <= a.length; i++) {
    const row = [i];
    for (let j = 1; j <= b.length; j++) {
      row[j] = Math.min(prev[j] + 1, row[j - 1] + 1, prev[j - 1] + (a[i - 1] === b[j - 1] ? 0 : 1));
    }
    prev = row;
  }
  return prev[b.length];
}
