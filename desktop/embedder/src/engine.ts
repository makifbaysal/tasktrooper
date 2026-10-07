import { readFileSync } from "node:fs";
import { pathToFileURL } from "node:url";
import { Tokenizer } from "@huggingface/tokenizers";
import { SerialQueue, truncateEncoding, type Encoding } from "./limits.js";
import type * as OrtModule from "onnxruntime-web";

/**
 * A plain `require()` call, not an ESM `import`, and it has to be: this
 * package's `exports` map picks a DIFFERENT file depending on which one is
 * used — `import` resolves to `dist/ort.node.min.mjs`, which calls
 * `createRequire(import.meta.url)`, and esbuild's CJS output has no real
 * `import.meta.url` to give it. `require` resolves to `dist/ort.node.min.js`,
 * the plain CJS build the go/no-go prototype ran directly under Node and
 * confirmed depends on nothing but `onnxruntime-common`. Written as a literal
 * `require(...)` call (not a renamed alias) so esbuild's bundler still
 * recognises and inlines it, rather than leaving it as a runtime lookup that
 * would need `node_modules/onnxruntime-web` shipped beside this file.
 */
// eslint-disable-next-line @typescript-eslint/no-require-imports -- see comment above.
const ort = require("onnxruntime-web") as typeof OrtModule;
const { env, InferenceSession, Tensor } = ort;

/**
 * Tokenize -> run -> mean-pool -> L2-normalize, the way the go/no-go
 * prototype proved out (see `engine.js` in the prototype directory this was
 * adapted from). `onnxruntime-web`'s WASM execution provider is used
 * directly, never through `@huggingface/transformers`'s `pipeline()` — that
 * defaults to `onnxruntime-node` under Node.js, which is the exact native
 * dependency this whole approach exists to avoid.
 *
 * Whatever text is handed to `embed()` is embedded exactly as received. No
 * `search_query:`/`search_document:` task prefix is added here: prefixing,
 * if any, is a decision made upstream of this repository, the same contract
 * the LM Studio proxy this replaces always had.
 *
 * This module only ever runs inside the worker (`worker.ts`): the wasm heap
 * it grows never shrinks, and terminating that worker is the only way to give
 * the memory back.
 */

export const EMBEDDING_DIM = 768;

export interface CachedModelFiles {
  modelPath: string;
  tokenizerJsonPath: string;
  tokenizerConfigPath: string;
}

export interface EmbedResult {
  /** One 768-dim unit vector per input, in input order. */
  vectors: Float64Array[];
  /** The tokenizer's count over every input, before truncation — the response's `usage`. */
  promptTokens: number;
}

export interface EngineOptions {
  /** Threads for the wasm backend's intra-op pool. */
  numThreads: number;
}

export class Engine {
  // Typed through the `import type` namespace, not the destructured value
  // above: a value binding does not carry the merged `interface
  // InferenceSession` declaration the real module export has, so only
  // `OrtModule.InferenceSession` is usable as a type here.
  readonly #session: OrtModule.InferenceSession;
  readonly #tokenizer: Tokenizer;
  // One inference at a time; see SerialQueue.
  readonly #queue = new SerialQueue();

  constructor(session: OrtModule.InferenceSession, tokenizer: Tokenizer) {
    this.#session = session;
    this.#tokenizer = tokenizer;
  }

  /**
   * Tokenize each input once — for both the run and the count — and run them
   * one at a time.
   *
   * Never batched into one `session.run`, and that is measured rather than
   * assumed: this quantized export does not isolate padded positions (twenty
   * pad tokens moved a real token's output to cosine 0.954), most likely
   * because its dynamic activation quantization takes its range over the whole
   * tensor. A vector that depends on what it was batched with would make the
   * same chunk embed differently from one index run to the next.
   */
  async embed(texts: readonly string[]): Promise<EmbedResult> {
    let promptTokens = 0;
    const vectors: Float64Array[] = [];
    for (const text of texts) {
      const full = this.#tokenizer.encode(text, { return_token_type_ids: true });
      promptTokens += full.ids.length;
      // Capped before inference: see MAX_SEQUENCE_TOKENS. Each input takes
      // its own turn, so a large request does not hold a small concurrent one
      // behind all of its inputs.
      vectors.push(await this.#queue.run(() => this.#run(truncateEncoding(full))));
    }
    return { vectors, promptTokens };
  }

  /** Frees the ONNX session's native memory. The engine must not be used afterwards. */
  release(): Promise<void> {
    return this.#session.release();
  }

  async #run(encoded: Encoding): Promise<Float64Array> {
    const seqLen = encoded.ids.length;
    const shape = [1, seqLen];
    const results = await this.#session.run({
      input_ids: new Tensor("int64", BigInt64Array.from(encoded.ids.map((id) => BigInt(id))), shape),
      attention_mask: new Tensor("int64", BigInt64Array.from(encoded.attention_mask.map((v) => BigInt(v))), shape),
      token_type_ids: new Tensor("int64", BigInt64Array.from(encoded.token_type_ids.map((v) => BigInt(v))), shape),
    });
    const lastHiddenState = results.last_hidden_state;
    if (!lastHiddenState) throw new Error("the model did not return last_hidden_state");
    const hidden = lastHiddenState.dims[lastHiddenState.dims.length - 1];
    if (typeof hidden !== "number") throw new Error("last_hidden_state has no hidden dimension");
    return l2Normalize(meanPool(lastHiddenState.data as Float32Array, encoded.attention_mask, seqLen, hidden));
  }
}

/**
 * Load the tokenizer and the ONNX session from files already downloaded and
 * hash-verified by `download.ts`, and point the WASM runtime at `wasmDir` —
 * the directory `build-embedder.mjs` stages `onnxruntime-web`'s `.wasm`/
 * `.mjs` runtime files into alongside the bundle. Set explicitly rather than
 * left to relative auto-resolution: a packaged app's `extraResources` stages
 * `dist/embedder` as a flat directory that does not mirror `node_modules`,
 * and the prototype's packaged-layout simulation proved auto-resolution
 * fails there.
 *
 * Spinning is off: an idle intra-op pool otherwise busy-waits between runs,
 * which is CPU taken from the user's own work for nothing.
 */
export async function loadEngine(files: CachedModelFiles, wasmDir: string, options: EngineOptions): Promise<Engine> {
  const wasmUrl = pathToFileURL(wasmDir).href;
  env.wasm.wasmPaths = wasmUrl.endsWith("/") ? wasmUrl : `${wasmUrl}/`;
  env.wasm.numThreads = options.numThreads;

  const tokenizerJson = JSON.parse(readFileSync(files.tokenizerJsonPath, "utf8")) as object;
  const tokenizerConfig = JSON.parse(readFileSync(files.tokenizerConfigPath, "utf8")) as object;
  const tokenizer = new Tokenizer(tokenizerJson, tokenizerConfig);

  const session = await InferenceSession.create(files.modelPath, {
    executionProviders: ["wasm"],
    extra: { session: { intra_op: { allow_spinning: "0" }, inter_op: { allow_spinning: "0" } } },
  });
  return new Engine(session, tokenizer);
}

/** Mean-pool token embeddings over the sequence dimension, weighted by the attention mask. */
function meanPool(lastHiddenState: Float32Array, attentionMask: number[], seqLen: number, hidden: number): Float64Array {
  const pooled = new Float64Array(hidden);
  let maskSum = 0;
  for (let t = 0; t < seqLen; t++) {
    const m = attentionMask[t];
    maskSum += m;
    if (m === 0) continue;
    const base = t * hidden;
    for (let h = 0; h < hidden; h++) pooled[h] += lastHiddenState[base + h] * m;
  }
  const denom = Math.max(maskSum, 1e-9);
  for (let h = 0; h < hidden; h++) pooled[h] /= denom;
  return pooled;
}

/** L2-normalize a vector in place, and return it. */
function l2Normalize(vec: Float64Array): Float64Array {
  let sumSq = 0;
  for (let i = 0; i < vec.length; i++) sumSq += vec[i] * vec[i];
  const norm = Math.sqrt(sumSq);
  if (norm > 0) for (let i = 0; i < vec.length; i++) vec[i] /= norm;
  return vec;
}
