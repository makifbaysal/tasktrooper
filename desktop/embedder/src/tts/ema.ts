import { randomInt } from "node:crypto";
import { readFileSync } from "node:fs";
import path from "node:path";
import { setImmediate as nextTurn } from "node:timers/promises";
import { pathToFileURL } from "node:url";
import type * as OrtModule from "onnxruntime-web";
import { SerialQueue } from "../limits.js";
import { chunk } from "./chunker.js";
import { Frontend } from "./frontend.js";
import { WasmNormalizer } from "./normalizer.js";
import { CONTEXT, FIRST_WINDOW, gaussianNoise, piece, pieceSeed, RATE, timeline, WINDOW, windows } from "./plan.js";

// A literal require, not an import, for the reason engine.ts gives.
// eslint-disable-next-line @typescript-eslint/no-require-imports -- see engine.ts.
const ort = require("onnxruntime-web") as typeof OrtModule;
const { env, InferenceSession, Tensor } = ort;

/**
 * EMA Lightning, the Turkish voice: text in, 48 kHz mono float32 out, one
 * decoded window at a time.
 *
 * Every piece of a text goes through the three exported graphs in turn: the
 * text stage (letter features and durations), the sound stage (the aligner and
 * the four sampling steps, which make all of the piece's latents at once) and
 * the decoder, run per window with CONTEXT frames of latents either side so
 * the seams do not click. The first window of a text is one second, so the
 * first audio is ready after one short decode rather than four seconds' worth.
 *
 * Runs only inside the speech worker (`tts/worker.ts`), for the same reason
 * engine.ts runs only inside its own: wasm memory never shrinks, and ending
 * the worker is the unload.
 */

export interface VoiceOptions {
  numThreads: number;
}

export interface SpeakOptions {
  speed: number;
  /** Checked between ONNX runs; generation stops at the next one. */
  signal?: AbortSignal;
}

interface VoiceConfig {
  vocab: string[];
  times: number[];
  latent_dim: number;
  hop: number;
  rate: number;
}

export class Voice {
  readonly #text: OrtModule.InferenceSession;
  readonly #sound: OrtModule.InferenceSession;
  readonly #decoder: OrtModule.InferenceSession;
  readonly #config: VoiceConfig;
  readonly #stoi: Map<string, number>;
  readonly #frontend: Frontend;
  // One ONNX run at a time across every stream: concurrent runs on the wasm
  // backend only fight over the same threads. Streams interleave per run.
  readonly #queue = new SerialQueue();

  constructor(
    sessions: { text: OrtModule.InferenceSession; sound: OrtModule.InferenceSession; decoder: OrtModule.InferenceSession },
    config: VoiceConfig,
    frontend: Frontend,
  ) {
    this.#text = sessions.text;
    this.#sound = sessions.sound;
    this.#decoder = sessions.decoder;
    this.#config = config;
    this.#stoi = new Map(config.vocab.map((ch, i) => [ch, i]));
    this.#frontend = frontend;
  }

  /**
   * Audio chunks as they are made: the first window of the first piece, then
   * each later window, and after each piece but the last, its pause as zeros.
   * Returning early (or aborting) drops the rest of the work.
   */
  async *stream(text: string, options: SpeakOptions): AsyncGenerator<Float32Array> {
    const { speed, signal } = options;
    const pieces = chunk(this.#frontend.run(text), speed);
    const seed = randomInt(2 ** 31);
    const { hop, latent_dim: dim, times } = this.#config;
    for (const [index, { text: spoken, pause }] of pieces.entries()) {
      const p = piece(spoken, this.#stoi);
      const L = p.ids.length;
      const textOut = await this.#run(this.#text, { ids: new Tensor("int64", p.ids, [1, L]) }, signal);
      if (!textOut) return;
      const h = output(textOut, "h");
      const plan = timeline(output(textOut, "dur").data as Float32Array, p.cw, p.words, speed);
      const T = plan.frames;

      const soundOut = await this.#run(
        this.#sound,
        {
          h,
          dur: new Tensor("float32", plan.dur, [1, L]),
          cw: new Tensor("int64", p.cw, [1, L]),
          wstart: new Tensor("int64", p.wstart, [1, L]),
          fw: new Tensor("int64", plan.fw, [1, T]),
          fp: new Tensor("float32", plan.fp, [1, T]),
          noise: new Tensor("float32", gaussianNoise(pieceSeed(seed, index), times.length * T * dim), [1, times.length, T, dim]),
        },
        signal,
      );
      if (!soundOut) return;
      const latents = output(soundOut, "latents").data as Float32Array;

      for (const [s, e] of windows(T, index === 0 ? FIRST_WINDOW : WINDOW)) {
        const a = Math.max(0, s - CONTEXT);
        const b = Math.min(T, e + CONTEXT);
        const z = new Tensor("float32", latents.slice(a * dim, b * dim), [1, b - a, dim]);
        const decoded = await this.#run(this.#decoder, { z }, signal);
        if (!decoded) return;
        yield (output(decoded, "audio").data as Float32Array).slice((s - a) * hop, (e - a) * hop);
      }
      if (pause > 0) yield new Float32Array(Math.round(pause * RATE));
    }
  }

  async release(): Promise<void> {
    await Promise.all([this.#text.release(), this.#sound.release(), this.#decoder.release()]);
  }

  /**
   * A run on this wasm build is synchronous under its promise, and the awaits
   * between runs are only microtasks, so without a real turn of the event loop
   * here the worker would not see a `cancel` message until the whole text had
   * been spoken. Null, and no run, once `signal` has aborted.
   */
  #run(
    session: OrtModule.InferenceSession,
    feeds: Record<string, OrtModule.Tensor>,
    signal: AbortSignal | undefined,
  ): Promise<OrtModule.InferenceSession.OnnxValueMapType | null> {
    return this.#queue.run(async () => {
      await nextTurn();
      return signal?.aborted ? null : session.run(feeds);
    });
  }
}

function output(results: OrtModule.InferenceSession.OnnxValueMapType, name: string): OrtModule.Tensor {
  const value = results[name];
  if (!value) throw new Error(`the voice model did not return ${name}`);
  return value;
}

/**
 * Load the three sessions and the normalizer from `voiceDir` (the files of
 * `files.ts`), with the wasm runtime pointed at `wasmDir` and spinning off,
 * exactly as engine.ts's loadEngine does and for the same reasons.
 */
export async function loadVoice(voiceDir: string, wasmDir: string, options: VoiceOptions): Promise<Voice> {
  const wasmUrl = pathToFileURL(wasmDir).href;
  env.wasm.wasmPaths = wasmUrl.endsWith("/") ? wasmUrl : `${wasmUrl}/`;
  env.wasm.numThreads = options.numThreads;

  const config = JSON.parse(readFileSync(path.join(voiceDir, "ema.json"), "utf8")) as VoiceConfig;
  if (config.rate !== RATE) throw new Error(`the voice model is ${config.rate} Hz; this engine speaks ${RATE} Hz`);
  const normalizer = new WasmNormalizer(readFileSync(path.join(voiceDir, "normalizer.wasm")));

  const sessionOptions: OrtModule.InferenceSession.SessionOptions = {
    executionProviders: ["wasm"],
    extra: { session: { intra_op: { allow_spinning: "0" }, inter_op: { allow_spinning: "0" } } },
  };
  // One after another: the wasm backend initialises on the first create, and
  // a second create racing it fails with "multiple calls to initWasm()".
  const text = await InferenceSession.create(path.join(voiceDir, "ema_text.onnx"), sessionOptions);
  const sound = await InferenceSession.create(path.join(voiceDir, "ema_sound.onnx"), sessionOptions);
  const decoder = await InferenceSession.create(path.join(voiceDir, "ema_decoder.onnx"), sessionOptions);
  return new Voice({ text, sound, decoder }, config, new Frontend(config.vocab, normalizer));
}
