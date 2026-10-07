import { parentPort, workerData } from "node:worker_threads";
import { loadEngine } from "./engine.js";
import type { WorkerInit, WorkerReply, WorkerRequest } from "./worker-engine.js";

/**
 * The worker side of `worker-engine.ts`: load the model, then answer embed
 * requests until the main thread terminates this thread — which is the
 * unload, and the reason the model lives here at all.
 */

const port = parentPort;
if (!port) throw new Error("worker.ts must run as a worker thread");

const init = workerData as WorkerInit;
const ready = loadEngine(init.files, init.wasmDir, { numThreads: init.numThreads });

ready.then(
  () => port.postMessage({ type: "ready" } satisfies WorkerReply),
  (err: unknown) => port.postMessage({ type: "failed", message: describe(err) } satisfies WorkerReply),
);

port.on("message", (request: WorkerRequest) => {
  if (request.type !== "embed") return;
  ready
    .then((engine) => engine.embed(request.texts))
    .then(
      ({ vectors, promptTokens }) => {
        const reply: WorkerReply = { type: "result", id: request.id, vectors, promptTokens };
        port.postMessage(
          reply,
          vectors.map((v) => v.buffer as ArrayBuffer),
        );
      },
      (err: unknown) => port.postMessage({ type: "error", id: request.id, message: describe(err) } satisfies WorkerReply),
    );
});

function describe(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}
