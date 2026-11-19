import { readFileSync } from "node:fs";
import path from "node:path";
import { setImmediate as nextTurn } from "node:timers/promises";
import { TINYVOCA_DEFAULT_STYLE, TINYVOCA_MODEL, TINYVOCA_STYLES } from "./files.js";
import { fromGGUF, type Phonemizer } from "./tinyvoca/dizione/engine.js";
import { limited, utterancesOf } from "./tinyvoca/text.js";
import { readGGUF, TinyVoca } from "./tinyvoca/tiny-voca.js";

/**
 * tiny-voca, the English voice: text in, 24 kHz mono float32 out, one
 * sentence (or a clause of a long one) at a time.
 *
 * Plain JavaScript, no ONNX Runtime: the model and its phonemizer (dizione)
 * are upstream's own scripts in `tinyvoca/`, and both read the one GGUF file.
 * A sentence is made synchronously, so a cancel is seen between sentences.
 *
 * Runs only inside the speech worker (`tts/worker.ts`), as ema.ts does.
 */

export const TINYVOCA_RATE = 24_000;

/** Upstream's own headroom (`js/say.mjs`): the ids of a sentence leave room for its start and end marks. */
const TOKEN_HEADROOM = 12;

export interface TinyVocaSpeakOptions {
  speed: number;
  /** One of TINYVOCA_STYLES; TINYVOCA_DEFAULT_STYLE when left out. */
  style?: string;
  signal?: AbortSignal;
}

export class TinyVocaVoice {
  readonly #model: TinyVoca;
  readonly #phonemizer: Phonemizer;

  constructor(model: TinyVoca, phonemizer: Phonemizer) {
    this.#model = model;
    this.#phonemizer = phonemizer;
  }

  async *stream(text: string, options: TinyVocaSpeakOptions): AsyncGenerator<Float32Array> {
    const { speed, signal } = options;
    const style = options.style ?? TINYVOCA_DEFAULT_STYLE;
    if (!TINYVOCA_STYLES.includes(style)) throw new Error(`no English voice named ${style}`);
    const utterances = utterancesOf(text, this.#phonemizer, this.#model.config.max_tokens - TOKEN_HEADROOM);
    for (const [seed, { ids, stress }] of utterances.entries()) {
      // A real turn of the event loop, so the worker sees a `cancel` between sentences.
      await nextTurn();
      if (signal?.aborted) return;
      yield limited(this.#model.synthesize(ids, stress, { voice: style, speed, seed }).audio);
    }
  }

  async release(): Promise<void> {}
}

export async function loadTinyVoca(voiceDir: string): Promise<TinyVocaVoice> {
  const bytes = readFileSync(path.join(voiceDir, TINYVOCA_MODEL));
  const file = readGGUF(bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength) as ArrayBuffer);
  const model = new TinyVoca(file);
  if (model.config.sample_rate !== TINYVOCA_RATE) {
    throw new Error(`the English voice model is ${model.config.sample_rate} Hz; this engine speaks ${TINYVOCA_RATE} Hz`);
  }
  for (const style of TINYVOCA_STYLES) {
    if (!model.voices.includes(style)) throw new Error(`the English voice model has no voice named ${style}`);
  }
  return new TinyVocaVoice(model, await fromGGUF(file));
}
