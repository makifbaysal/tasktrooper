import { parentPort, workerData } from "node:worker_threads";
import { loadVoice } from "./ema.js";
import type { SpeechWorkerInit, SpeechWorkerReply, SpeechWorkerRequest } from "./speech-worker.js";

/**
 * The worker side of `speech-worker.ts`: load the voice, then stream speech
 * for each request until the main thread terminates this thread.
 */

if (!parentPort) throw new Error("tts/worker.ts must run as a worker thread");
const port = parentPort;

const init = workerData as SpeechWorkerInit;
const ready = loadVoice(init.voiceDir, init.wasmDir, { numThreads: init.numThreads });
const running = new Map<number, AbortController>();

ready.then(
  () => port.postMessage({ type: "ready" } satisfies SpeechWorkerReply),
  (err: unknown) => port.postMessage({ type: "failed", message: describe(err) } satisfies SpeechWorkerReply),
);

port.on("message", (request: SpeechWorkerRequest) => {
  if (request.type === "cancel") {
    running.get(request.id)?.abort();
    return;
  }
  if (request.type !== "speak") return;
  const controller = new AbortController();
  running.set(request.id, controller);
  void speak(request.id, request.text, request.speed, controller.signal).finally(() => running.delete(request.id));
});

async function speak(id: number, text: string, speed: number, signal: AbortSignal): Promise<void> {
  try {
    const voice = await ready;
    for await (const pcm of voice.stream(text, { speed, signal })) {
      port.postMessage({ type: "chunk", id, pcm } satisfies SpeechWorkerReply, [pcm.buffer as ArrayBuffer]);
    }
    port.postMessage({ type: "done", id } satisfies SpeechWorkerReply);
  } catch (err) {
    port.postMessage({ type: "error", id, message: describe(err) } satisfies SpeechWorkerReply);
  }
}

function describe(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}
