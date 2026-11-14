import { randomInt } from "node:crypto";
import { readFileSync } from "node:fs";
import path from "node:path";
import { setImmediate as nextTurn } from "node:timers/promises";
import { pathToFileURL } from "node:url";
import type * as OrtModule from "onnxruntime-web";
import { SerialQueue } from "../limits.js";
import {
  SUPERTONIC_CONFIG,
  SUPERTONIC_DEFAULT_STYLE,
  SUPERTONIC_GRAPHS,
  SUPERTONIC_INDEXER,
  SUPERTONIC_STYLES,
  supertonicGraphFile,
  supertonicStyleFile,
  type SupertonicGraph,
} from "./files.js";
import { blocks } from "./frontend.js";
import { WasmNormalizer } from "./normalizer.js";
import { gaussianNoise, pieceSeed } from "./plan.js";
import type { SpeechLanguage } from "./speech-worker.js";
import {
  BASE_SPEED,
  englishSpoken,
  latentShape,
  passes,
  prepareText,
  speechBounds,
  SUPERTONIC_RATE,
  SUPERTONIC_STEPS,
  textIds,
  turkishOrdinals,
  type SupertonicConfig,
} from "./supertonic-plan.js";

// A literal require, not an import, for the reason engine.ts gives.
// eslint-disable-next-line @typescript-eslint/no-require-imports -- see engine.ts.
const ort = require("onnxruntime-web") as typeof OrtModule;
const { env, InferenceSession, Tensor } = ort;

/**
 * Supertonic 3, the voice for both languages: text in, 44.1 kHz mono float32
 * out, one pass (a sentence, or a clause of a long one) at a time.
 *
 * Each pass goes through the four graphs as the official runtime runs them:
 * the duration predictor (how long the pass is), the text encoder, the vector
 * estimator SUPERTONIC_STEPS times from Gaussian noise, and the vocoder. A
 * voice style is two tensors, one for the duration predictor and one for the
 * rest. Turkish text is read through normalizer-tr first (numbers, money,
 * abbreviations), English text has its amounts of money and percentages spelled out.
 *
 * Runs only inside the speech worker (`tts/worker.ts`), as ema.ts does.
 */

export interface SupertonicOptions {
  numThreads: number;
}

export interface SupertonicSpeakOptions {
  lang: SpeechLanguage;
  speed: number;
  /** One of SUPERTONIC_STYLES; SUPERTONIC_DEFAULT_STYLE when left out. */
  style?: string;
  /** Checked between ONNX runs; generation stops at the next one. */
  signal?: AbortSignal;
}

interface Style {
  ttl: OrtModule.Tensor;
  dp: OrtModule.Tensor;
}

interface StyleFile {
  style_ttl: { dims: number[]; data: unknown[] };
  style_dp: { dims: number[]; data: unknown[] };
}

const TTL_DIMS = [1, 50, 256];
const DP_DIMS = [1, 8, 16];

export class SupertonicVoice {
  readonly #sessions: Record<SupertonicGraph, OrtModule.InferenceSession>;
  readonly #config: SupertonicConfig;
  readonly #indexer: Int32Array;
  readonly #normalizer: WasmNormalizer;
  readonly #voiceDir: string;
  readonly #styles = new Map<string, Style>();
  readonly #queue = new SerialQueue();

  constructor(
    sessions: Record<SupertonicGraph, OrtModule.InferenceSession>,
    config: SupertonicConfig,
    indexer: Int32Array,
    normalizer: WasmNormalizer,
    voiceDir: string,
  ) {
    this.#sessions = sessions;
    this.#config = config;
    this.#indexer = indexer;
    this.#normalizer = normalizer;
    this.#voiceDir = voiceDir;
  }

  async *stream(text: string, options: SupertonicSpeakOptions): AsyncGenerator<Float32Array> {
    const { lang, speed, signal } = options;
    const style = this.#style(options.style ?? SUPERTONIC_DEFAULT_STYLE);
    const spoken = lang === "tr" ? this.#turkish(text) : englishSpoken(text);
    const seed = randomInt(2 ** 31);
    const { dp, ttl } = style;
    for (const [index, pass] of passes(spoken).entries()) {
      const ids = textIds(prepareText(pass.text), lang, this.#indexer);
      if (!ids) continue;
      const L = ids.length;
      const text_ids = new Tensor("int64", ids, [1, L]);
      const text_mask = new Tensor("float32", new Float32Array(L).fill(1), [1, 1, L]);

      const durations = await this.#run("duration_predictor", { text_ids, style_dp: dp, text_mask }, signal);
      if (!durations) return;
      const seconds = (output(durations, "duration").data as Float32Array)[0] / (speed * BASE_SPEED);
      const encoded = await this.#run("text_encoder", { text_ids, style_ttl: ttl, text_mask }, signal);
      if (!encoded) return;
      const text_emb = output(encoded, "text_emb");

      const { samples, frames, channels } = latentShape(seconds, this.#config);
      let latent = gaussianNoise(pieceSeed(seed, index), channels * frames);
      const latent_mask = new Tensor("float32", new Float32Array(frames).fill(1), [1, 1, frames]);
      const total_step = new Tensor("float32", Float32Array.of(SUPERTONIC_STEPS), [1]);
      for (let step = 0; step < SUPERTONIC_STEPS; step++) {
        const denoised = await this.#run(
          "vector_estimator",
          {
            noisy_latent: new Tensor("float32", latent, [1, channels, frames]),
            text_emb,
            style_ttl: ttl,
            text_mask,
            latent_mask,
            total_step,
            current_step: new Tensor("float32", Float32Array.of(step), [1]),
          },
          signal,
        );
        if (!denoised) return;
        latent = output(denoised, "denoised_latent").data as Float32Array;
      }
      const vocoded = await this.#run("vocoder", { latent: new Tensor("float32", latent, [1, channels, frames]) }, signal);
      if (!vocoded) return;
      const wav = (output(vocoded, "wav_tts").data as Float32Array).subarray(0, samples);
      const [start, end] = speechBounds(wav, SUPERTONIC_RATE);
      if (end > start) yield wav.slice(start, end);
      if (pass.pause > 0) yield new Float32Array(Math.round(pass.pause * SUPERTONIC_RATE));
    }
  }

  async release(): Promise<void> {
    await Promise.all(SUPERTONIC_GRAPHS.map((graph) => this.#sessions[graph].release()));
  }

  /** A block the normalizer refuses keeps its words, as EMA's frontend does. */
  #turkish(text: string): string {
    return blocks(turkishOrdinals(text))
      .map((block) => {
        try {
          return this.#normalizer.normalize(block) ?? block;
        } catch {
          return block;
        }
      })
      .join(" ");
  }

  #style(name: string): Style {
    if (!SUPERTONIC_STYLES.includes(name)) throw new Error(`no Supertonic voice style named ${name}`);
    let style = this.#styles.get(name);
    if (!style) {
      const file = JSON.parse(readFileSync(path.join(this.#voiceDir, supertonicStyleFile(name)), "utf8")) as StyleFile;
      style = { ttl: styleTensor(file.style_ttl, TTL_DIMS, name), dp: styleTensor(file.style_dp, DP_DIMS, name) };
      this.#styles.set(name, style);
    }
    return style;
  }

  /** As ema.ts's #run: a real turn of the event loop before each run, so a cancel is seen between runs. */
  #run(
    graph: SupertonicGraph,
    feeds: Record<string, OrtModule.Tensor>,
    signal: AbortSignal | undefined,
  ): Promise<OrtModule.InferenceSession.OnnxValueMapType | null> {
    return this.#queue.run(async () => {
      await nextTurn();
      return signal?.aborted ? null : this.#sessions[graph].run(feeds);
    });
  }
}

function styleTensor(raw: { dims: number[]; data: unknown[] }, dims: number[], name: string): OrtModule.Tensor {
  const data = Float32Array.from(raw.data.flat(Infinity) as number[]);
  if (raw.dims.join() !== dims.join() || data.length !== dims.reduce((a, b) => a * b, 1)) {
    throw new Error(`${supertonicStyleFile(name)} is not a Supertonic voice style`);
  }
  return new Tensor("float32", data, dims);
}

function output(results: OrtModule.InferenceSession.OnnxValueMapType, name: string): OrtModule.Tensor {
  const value = results[name];
  if (!value) throw new Error(`the voice model did not return ${name}`);
  return value;
}

/** Load the four graphs, the index and the normalizer from `voiceDir`, with the wasm runtime set up as ema.ts's loadVoice does. */
export async function loadSupertonic(voiceDir: string, wasmDir: string, options: SupertonicOptions): Promise<SupertonicVoice> {
  const wasmUrl = pathToFileURL(wasmDir).href;
  env.wasm.wasmPaths = wasmUrl.endsWith("/") ? wasmUrl : `${wasmUrl}/`;
  env.wasm.numThreads = options.numThreads;

  const config = JSON.parse(readFileSync(path.join(voiceDir, SUPERTONIC_CONFIG), "utf8")) as SupertonicConfig;
  if (config.ae.sample_rate !== SUPERTONIC_RATE) {
    throw new Error(`the Supertonic model is ${config.ae.sample_rate} Hz; this engine speaks ${SUPERTONIC_RATE} Hz`);
  }
  const indexer = Int32Array.from(JSON.parse(readFileSync(path.join(voiceDir, SUPERTONIC_INDEXER), "utf8")) as number[]);
  const normalizer = new WasmNormalizer(readFileSync(path.join(voiceDir, "normalizer.wasm")));

  const sessionOptions: OrtModule.InferenceSession.SessionOptions = {
    executionProviders: ["wasm"],
    extra: { session: { intra_op: { allow_spinning: "0" }, inter_op: { allow_spinning: "0" } } },
  };
  const sessions = {} as Record<SupertonicGraph, OrtModule.InferenceSession>;
  // One after another, for the reason ema.ts's loadVoice gives.
  for (const graph of SUPERTONIC_GRAPHS) {
    sessions[graph] = await InferenceSession.create(path.join(voiceDir, supertonicGraphFile(graph)), sessionOptions);
  }
  return new SupertonicVoice(sessions, config, indexer, normalizer, voiceDir);
}
