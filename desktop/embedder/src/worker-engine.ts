import { Worker } from "node:worker_threads";
import type { CachedModelFiles } from "./engine.js";
import type { EmbeddingEngine } from "./server.js";

/**
 * The model, held in a worker thread rather than in this process's own heap.
 *
 * onnxruntime-web's wasm memory only ever grows: releasing the session frees
 * its allocations inside that memory and gives nothing back to the OS, which
 * is why an idle unload used to leave the process exactly as large as before.
 * A worker owns its own isolate, wasm memory and wasm threads, and
 * `terminate()` tears all of them down — so `release()` here is the unload
 * that actually returns the memory, and the HTTP server, its port and the
 * download all stay where they are.
 */

export interface WorkerInit {
  files: CachedModelFiles;
  wasmDir: string;
  numThreads: number;
}

export type WorkerRequest = { type: "embed"; id: number; texts: string[] };

export type WorkerReply =
  | { type: "ready" }
  | { type: "failed"; message: string }
  | { type: "result"; id: number; vectors: Float64Array[]; promptTokens: number }
  | { type: "error"; id: number; message: string };

/**
 * Threads for the wasm backend when nothing overrides it: half the cores, at
 * most four. Each wasm thread is a whole worker with its own isolate, so past
 * a few the memory they cost outgrows the speed they buy on inputs this size.
 */
export function defaultWasmThreads(cores: number): number {
  return Math.max(1, Math.min(4, Math.floor(cores / 2)));
}

interface Pending {
  resolve(result: { vectors: Float64Array[]; promptTokens: number }): void;
  reject(err: Error): void;
}

class WorkerEngine implements EmbeddingEngine {
  readonly #worker: Worker;
  readonly #pending = new Map<number, Pending>();
  #nextId = 1;
  #dead = false;

  constructor(worker: Worker) {
    this.#worker = worker;
    worker.on("message", (reply: WorkerReply) => this.#onReply(reply));
    worker.on("error", (err) => this.#fail(err instanceof Error ? err : new Error(String(err))));
    worker.on("exit", (code) => this.#fail(new Error(`the embedding worker exited with code ${code}`)));
  }

  get alive(): boolean {
    return !this.#dead;
  }

  embed(texts: string[]): Promise<{ vectors: Float64Array[]; promptTokens: number }> {
    if (this.#dead) return Promise.reject(new Error("the embedding worker is gone"));
    const id = this.#nextId++;
    return new Promise((resolve, reject) => {
      this.#pending.set(id, { resolve, reject });
      this.#worker.postMessage({ type: "embed", id, texts } satisfies WorkerRequest);
    });
  }

  async release(): Promise<void> {
    this.#fail(new Error("the embedding model was unloaded"));
    await this.#worker.terminate();
  }

  #onReply(reply: WorkerReply): void {
    if (reply.type !== "result" && reply.type !== "error") return;
    const pending = this.#pending.get(reply.id);
    if (!pending) return;
    this.#pending.delete(reply.id);
    if (reply.type === "result") pending.resolve({ vectors: reply.vectors, promptTokens: reply.promptTokens });
    else pending.reject(new Error(reply.message));
  }

  #fail(err: Error): void {
    this.#dead = true;
    for (const pending of this.#pending.values()) pending.reject(err);
    this.#pending.clear();
  }
}

/**
 * Start a worker running `script` and resolve once its model has loaded.
 * Rejects, with the worker already gone, when the load fails.
 */
export function startWorkerEngine(script: string, init: WorkerInit): Promise<EmbeddingEngine> {
  return new Promise((resolve, reject) => {
    const worker = new Worker(script, { workerData: init });
    const cleanup = (): void => {
      worker.off("message", onMessage);
      worker.off("error", onError);
      worker.off("exit", onExit);
    };
    const onMessage = (reply: WorkerReply): void => {
      if (reply.type === "ready") {
        cleanup();
        resolve(new WorkerEngine(worker));
      } else if (reply.type === "failed") {
        cleanup();
        void worker.terminate();
        reject(new Error(reply.message));
      }
    };
    const onError = (err: unknown): void => {
      cleanup();
      void worker.terminate();
      reject(err instanceof Error ? err : new Error(String(err)));
    };
    const onExit = (code: number): void => {
      cleanup();
      reject(new Error(`the embedding worker exited with code ${code} while loading`));
    };
    worker.on("message", onMessage);
    worker.on("error", onError);
    worker.on("exit", onExit);
  });
}
