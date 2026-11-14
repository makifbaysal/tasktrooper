import { readFileSync } from "node:fs";
import path from "node:path";
import { setImmediate as nextTurn } from "node:timers/promises";
import { pathToFileURL } from "node:url";
import { gunzipSync } from "node:zlib";
import type * as OrtModule from "onnxruntime-web";
import { SerialQueue } from "../limits.js";
import { EnglishG2P, type LexiconData } from "./english.js";
import { KOKORO_DEFAULT_STYLE, KOKORO_STYLES, KOKORO_CONFIG, KOKORO_MODEL, kokoroStyleFile, misakiLexiconFile } from "./files.js";
import { KOKORO_RATE, passes, STYLE_DIM, STYLE_ROWS, styleRow, tokenIds } from "./kokoro-plan.js";

// A literal require, not an import, for the reason engine.ts gives.
// eslint-disable-next-line @typescript-eslint/no-require-imports -- see engine.ts.
const ort = require("onnxruntime-web") as typeof OrtModule;
const { env, InferenceSession, Tensor } = ort;

/**
 * Kokoro-82M, the English voice: text in, 24 kHz mono float32 out, one pass
 * (a sentence, or a clause of a long one) at a time.
 *
 * One graph does everything: phoneme ids, a 256-value style for the voice and
 * the pass's length, and the speed go in; the pass's audio comes out. The
 * phonemes come from `english.ts`, with the American dictionaries for an
 * American voice and the British ones for a British voice (`b…` styles).
 *
 * Runs only inside the speech worker (`tts/worker.ts`), as ema.ts does.
 */

export interface KokoroOptions {
  numThreads: number;
}

export interface KokoroSpeakOptions {
  speed: number;
  /** One of KOKORO_STYLES; KOKORO_DEFAULT_STYLE when left out. */
  style?: string;
  /** Checked between ONNX runs; generation stops at the next one. */
  signal?: AbortSignal;
}

interface KokoroConfig {
  vocab: Record<string, number>;
  rate: number;
}

export class KokoroVoice {
  readonly #session: OrtModule.InferenceSession;
  readonly #vocab: Map<string, number>;
  readonly #voiceDir: string;
  readonly #g2p = new Map<"us" | "gb", EnglishG2P>();
  readonly #styles = new Map<string, Float32Array>();
  readonly #queue = new SerialQueue();

  constructor(session: OrtModule.InferenceSession, config: KokoroConfig, voiceDir: string) {
    this.#session = session;
    this.#vocab = new Map(Object.entries(config.vocab));
    this.#voiceDir = voiceDir;
  }

  async *stream(text: string, options: KokoroSpeakOptions): AsyncGenerator<Float32Array> {
    const { speed, signal } = options;
    const style = options.style ?? KOKORO_DEFAULT_STYLE;
    if (!KOKORO_STYLES.includes(style)) throw new Error(`no English voice style named ${style}`);
    const pack = this.#style(style);
    const phonemes = this.#lexicon(style.startsWith("b") ? "gb" : "us").phonemize(text);
    for (const pass of passes(phonemes)) {
      const ids = tokenIds(pass, this.#vocab);
      const n = ids.length - 2;
      if (n <= 0) continue;
      const out = await this.#run(
        {
          input_ids: new Tensor("int64", ids, [1, ids.length]),
          style: new Tensor("float32", styleRow(pack, n).slice(), [1, STYLE_DIM]),
          speed: new Tensor("float32", Float32Array.of(speed), [1]),
        },
        signal,
      );
      if (!out) return;
      const audio = out.waveform;
      if (!audio) throw new Error("the voice model did not return waveform");
      yield (audio.data as Float32Array).slice();
    }
  }

  async release(): Promise<void> {
    await this.#session.release();
  }

  /** Loaded on first use: a British style never costs the American speaker the British dictionaries, or the other way round. */
  #lexicon(accent: "us" | "gb"): EnglishG2P {
    let g2p = this.#g2p.get(accent);
    if (!g2p) {
      const raw = gunzipSync(readFileSync(path.join(this.#voiceDir, misakiLexiconFile(accent)))).toString("utf8");
      g2p = new EnglishG2P(JSON.parse(raw) as LexiconData, accent === "gb");
      this.#g2p.set(accent, g2p);
    }
    return g2p;
  }

  #style(name: string): Float32Array {
    let pack = this.#styles.get(name);
    if (!pack) {
      const bytes = readFileSync(path.join(this.#voiceDir, kokoroStyleFile(name)));
      if (bytes.byteLength !== STYLE_ROWS * STYLE_DIM * 4) throw new Error(`${kokoroStyleFile(name)} is not a Kokoro voice file`);
      pack = new Float32Array(bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength));
      this.#styles.set(name, pack);
    }
    return pack;
  }

  /** As ema.ts's #run: a real turn of the event loop before each run, so a cancel is seen between passes. */
  #run(feeds: Record<string, OrtModule.Tensor>, signal: AbortSignal | undefined): Promise<OrtModule.InferenceSession.OnnxValueMapType | null> {
    return this.#queue.run(async () => {
      await nextTurn();
      return signal?.aborted ? null : this.#session.run(feeds);
    });
  }
}

/** Load the model from `voiceDir`, with the wasm runtime set up exactly as ema.ts's loadVoice does. */
export async function loadKokoro(voiceDir: string, wasmDir: string, options: KokoroOptions): Promise<KokoroVoice> {
  const wasmUrl = pathToFileURL(wasmDir).href;
  env.wasm.wasmPaths = wasmUrl.endsWith("/") ? wasmUrl : `${wasmUrl}/`;
  env.wasm.numThreads = options.numThreads;

  const config = JSON.parse(readFileSync(path.join(voiceDir, KOKORO_CONFIG), "utf8")) as KokoroConfig;
  if (config.rate !== KOKORO_RATE) throw new Error(`the English voice model is ${config.rate} Hz; this engine speaks ${KOKORO_RATE} Hz`);
  const session = await InferenceSession.create(path.join(voiceDir, KOKORO_MODEL), {
    executionProviders: ["wasm"],
    extra: { session: { intra_op: { allow_spinning: "0" }, inter_op: { allow_spinning: "0" } } },
  });
  return new KokoroVoice(session, config, voiceDir);
}
