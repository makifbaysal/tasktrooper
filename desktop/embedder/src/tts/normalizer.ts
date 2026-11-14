import type { TextNormalizer } from "./frontend.js";

/**
 * normalizer-tr, the Rust crate EMA reads numbers, dates, money and
 * abbreviations through, compiled to a wasm module with no imports and a
 * four-function C ABI (`desktop/voice-models/normalizer-wasm`): the caller
 * copies UTF-8 in with `alloc`, `normalize` returns the output's byte length
 * (or -1), and `out_ptr` says where it is until the next call.
 *
 * Synchronous on purpose: the module is compiled once, and a call is a plain
 * function call into it with nothing to wait for.
 */

interface Exports {
  memory: { buffer: ArrayBuffer };
  alloc(len: number): number;
  dealloc(ptr: number, len: number): void;
  normalize(ptr: number, len: number): number;
  out_ptr(): number;
}

// The global exists in every Node this runs on; this project's lib and
// @types/node just do not declare it, and this is all of it that is used.
interface WasmApi {
  Module: new (bytes: Uint8Array) => object;
  Instance: new (module: object, imports: object) => { exports: unknown };
}
const wasm = (globalThis as unknown as { WebAssembly: WasmApi }).WebAssembly;

const encoder = new TextEncoder();
const decoder = new TextDecoder("utf-8", { fatal: false });

export class WasmNormalizer implements TextNormalizer {
  readonly #module: object;
  #exports: Exports;

  constructor(bytes: Uint8Array) {
    this.#module = new wasm.Module(bytes);
    this.#exports = this.#instantiate();
  }

  /** The spoken form, or null when the normalizer refused the text. */
  normalize(text: string): string | null {
    const input = encoder.encode(text);
    try {
      return this.#call(input);
    } catch {
      // The crate is built with panic = "abort", so a panic is a wasm trap
      // that can leave its heap and its lazily built tables half-written. A
      // fresh instance of the same compiled module is cheap and trusted.
      this.#exports = this.#instantiate();
      return null;
    }
  }

  #call(input: Uint8Array): string | null {
    const e = this.#exports;
    const ptr = e.alloc(input.length);
    // A view taken before alloc can be detached by a memory.grow inside it.
    new Uint8Array(e.memory.buffer, ptr, input.length).set(input);
    let n: number;
    try {
      n = e.normalize(ptr, input.length);
    } finally {
      e.dealloc(ptr, input.length);
    }
    if (n < 0) return null;
    return decoder.decode(new Uint8Array(e.memory.buffer, e.out_ptr(), n));
  }

  #instantiate(): Exports {
    return new wasm.Instance(this.#module, {}).exports as Exports;
  }
}
