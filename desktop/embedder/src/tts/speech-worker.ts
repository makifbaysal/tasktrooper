import { Worker } from "node:worker_threads";

/**
 * A voice engine, held in a worker thread of its own: separate from the
 * embedding model's and from the other engines, so each loads, idles out and
 * unloads on its own schedule, and `terminate()` hands that engine's wasm
 * memory back without touching the others.
 * See worker-engine.ts for why a worker at all.
 *
 * A request streams: the worker posts each chunk as it is made (its buffer
 * transferred, not copied), then `done` or `error`. A `cancel` stops the
 * worker at its next ONNX run.
 */

/** The languages the bundled voices speak. */
export type SpeechLanguage = "tr" | "en";

/** Which model a worker holds: EMA Lightning (Turkish) or tiny-voca (English). */
export type SpeechEngineId = "ema" | "tinyvoca";

export interface SpeechWorkerInit {
  engine: SpeechEngineId;
  voiceDir: string;
  wasmDir: string;
  numThreads: number;
}

export type SpeechWorkerRequest =
  | { type: "speak"; id: number; text: string; lang: SpeechLanguage; speed: number; style?: string }
  | { type: "cancel"; id: number };

export type SpeechWorkerReply =
  | { type: "ready" }
  | { type: "failed"; message: string }
  | { type: "chunk"; id: number; pcm: Float32Array }
  | { type: "done"; id: number }
  | { type: "error"; id: number; message: string };

export interface SpeechOptions {
  lang: SpeechLanguage;
  speed: number;
  signal: AbortSignal;
  /** The engine's voice; EMA has one, so none is named. */
  style?: string;
}

export interface SpeechEngine {
  /** Mono float32 chunks at the voice's rate, in order. Ends early, without an error, when `signal` aborts. */
  stream(text: string, options: SpeechOptions): AsyncIterable<Float32Array>;
  release(): Promise<void>;
  /** False once the engine can no longer answer (its worker died), so the next request loads a new one. */
  readonly alive?: boolean;
}

interface Stream {
  push(reply: SpeechWorkerReply): void;
  fail(err: Error): void;
}

class WorkerSpeechEngine implements SpeechEngine {
  readonly #worker: Worker;
  readonly #streams = new Map<number, Stream>();
  #nextId = 1;
  #dead = false;

  constructor(worker: Worker) {
    this.#worker = worker;
    worker.on("message", (reply: SpeechWorkerReply) => {
      if ("id" in reply) this.#streams.get(reply.id)?.push(reply);
    });
    worker.on("error", (err) => this.#fail(err instanceof Error ? err : new Error(String(err))));
    worker.on("exit", (code) => this.#fail(new Error(`the speech worker exited with code ${code}`)));
  }

  get alive(): boolean {
    return !this.#dead;
  }

  async *stream(text: string, options: SpeechOptions): AsyncGenerator<Float32Array> {
    if (this.#dead) throw new Error("the speech worker is gone");
    const { signal } = options;
    if (signal.aborted) return;
    const id = this.#nextId++;
    const queue: Float32Array[] = [];
    const state: { finished: boolean; failure: Error | null } = { finished: false, failure: null };
    let wake: (() => void) | null = null;
    const notify = (): void => {
      wake?.();
      wake = null;
    };
    this.#streams.set(id, {
      push: (reply) => {
        if (reply.type === "chunk") queue.push(reply.pcm);
        else if (reply.type === "done") state.finished = true;
        else if (reply.type === "error") state.failure = new Error(reply.message);
        notify();
      },
      fail: (err) => {
        state.failure = err;
        notify();
      },
    });
    const onAbort = (): void => notify();
    signal.addEventListener("abort", onAbort);
    this.#worker.postMessage({ type: "speak", id, text, lang: options.lang, speed: options.speed, style: options.style } satisfies SpeechWorkerRequest);
    try {
      for (;;) {
        const next = queue.shift();
        if (next) {
          yield next;
          continue;
        }
        if (state.failure) throw state.failure;
        if (state.finished || signal.aborted) return;
        await new Promise<void>((resolve) => {
          wake = resolve;
        });
      }
    } finally {
      signal.removeEventListener("abort", onAbort);
      this.#streams.delete(id);
      // Also reached when the consumer stops early: the worker must not keep
      // generating audio nobody will read.
      if (!state.finished && !state.failure && !this.#dead) this.#worker.postMessage({ type: "cancel", id } satisfies SpeechWorkerRequest);
    }
  }

  async release(): Promise<void> {
    this.#fail(new Error("the voice was unloaded"));
    await this.#worker.terminate();
  }

  #fail(err: Error): void {
    this.#dead = true;
    for (const stream of this.#streams.values()) stream.fail(err);
    this.#streams.clear();
  }
}

/**
 * Start a worker running `script` and resolve once its engine has loaded.
 * Rejects, with the worker already gone, when the load fails.
 */
export function startSpeechWorker(script: string, init: SpeechWorkerInit): Promise<SpeechEngine> {
  return new Promise((resolve, reject) => {
    const worker = new Worker(script, { workerData: init });
    const cleanup = (): void => {
      worker.off("message", onMessage);
      worker.off("error", onError);
      worker.off("exit", onExit);
    };
    const onMessage = (reply: SpeechWorkerReply): void => {
      if (reply.type === "ready") {
        cleanup();
        resolve(new WorkerSpeechEngine(worker));
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
      reject(new Error(`the speech worker exited with code ${code} while loading`));
    };
    worker.on("message", onMessage);
    worker.on("error", onError);
    worker.on("exit", onExit);
  });
}
