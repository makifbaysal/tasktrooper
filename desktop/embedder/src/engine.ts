import { readFileSync } from "node:fs";
import { pathToFileURL } from "node:url";
import { Tokenizer } from "@huggingface/tokenizers";
import { SerialQueue, truncateEncoding } from "./limits.js";
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
 */

export const EMBEDDING_DIM = 768;

export interface CachedModelFiles {
  modelPath: string;
  tokenizerJsonPath: string;
  tokenizerConfigPath: string;
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

  /** Full pipeline: text -> tokenize -> run -> mean-pool -> L2-normalize -> a 768-dim unit vector. */
  async embed(text: string): Promise<Float64Array> {
    return this.#queue.run(async () => {
      const { lastHiddenState, attentionMask, seqLen, hidden } = await this.#run(text);
      const pooled = meanPool(lastHiddenState, attentionMask, seqLen, hidden);
      return l2Normalize(pooled);
    });
  }

  /**
   * The tokenizer's own count for `text`, for the response's `usage` field.
   * Separate from `embed()` — which must keep exactly the signature the
   * server expects — so a caller that only wants a token count is not made
   * to pay for a forward pass through the model to get one.
   */
  tokenCount(text: string): number {
    return this.#tokenizer.encode(text, { return_token_type_ids: true }).ids.length;
  }

  async #run(
    text: string,
  ): Promise<{ lastHiddenState: Float32Array; attentionMask: number[]; seqLen: number; hidden: number }> {
    // Capped before inference: see MAX_SEQUENCE_TOKENS.
    const encoded = truncateEncoding(this.#tokenizer.encode(text, { return_token_type_ids: true }));
    const seqLen = encoded.ids.length;

    const inputIds = BigInt64Array.from(encoded.ids.map((id) => BigInt(id)));
    const attentionMask = BigInt64Array.from(encoded.attention_mask.map((v) => BigInt(v)));
    const tokenTypeIds = BigInt64Array.from(encoded.token_type_ids.map((v) => BigInt(v)));

    const feeds = {
      input_ids: new Tensor("int64", inputIds, [1, seqLen]),
      attention_mask: new Tensor("int64", attentionMask, [1, seqLen]),
      token_type_ids: new Tensor("int64", tokenTypeIds, [1, seqLen]),
    };

    const results = await this.#session.run(feeds);
    const lastHiddenState = results.last_hidden_state;
    if (!lastHiddenState) throw new Error("the model did not return last_hidden_state");
    const hidden = lastHiddenState.dims[lastHiddenState.dims.length - 1];
    if (typeof hidden !== "number") throw new Error("last_hidden_state has no hidden dimension");

    return {
      lastHiddenState: lastHiddenState.data as Float32Array,
      attentionMask: encoded.attention_mask,
      seqLen,
      hidden,
    };
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
 */
export async function loadEngine(files: CachedModelFiles, wasmDir: string): Promise<Engine> {
  const wasmUrl = pathToFileURL(wasmDir).href;
  env.wasm.wasmPaths = wasmUrl.endsWith("/") ? wasmUrl : `${wasmUrl}/`;

  const tokenizerJson = JSON.parse(readFileSync(files.tokenizerJsonPath, "utf8")) as object;
  const tokenizerConfig = JSON.parse(readFileSync(files.tokenizerConfigPath, "utf8")) as object;
  const tokenizer = new Tokenizer(tokenizerJson, tokenizerConfig);

  const session = await InferenceSession.create(files.modelPath, { executionProviders: ["wasm"] });
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
