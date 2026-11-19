import { parentPort, workerData } from "node:worker_threads";
import { loadVoice } from "./ema.js";
import type { SpeechLanguage, SpeechWorkerInit, SpeechWorkerReply, SpeechWorkerRequest } from "./speech-worker.js";
import { loadTinyVoca } from "./tinyvoca.js";

/**
 * The worker side of `speech-worker.ts`: load the engine it was started for, then stream speech
 * for each request until the main thread terminates this thread.
 */

if (!parentPort) throw new Error("tts/worker.ts must run as a worker thread");
const port = parentPort;

const init = workerData as SpeechWorkerInit;
const options = { numThreads: init.numThreads };
const ready = init.engine === "ema" ? loadVoice(init.voiceDir, init.wasmDir, options) : loadTinyVoca(init.voiceDir);
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
  const { id, text, lang, speed, style } = request;
  void speak(id, text, { lang, speed, style, signal: controller.signal }).finally(() => running.delete(id));
});

async function speak(id: number, text: string, options: { lang: SpeechLanguage; speed: number; style?: string; signal: AbortSignal }): Promise<void> {
  try {
    const voice = await ready;
    for await (const pcm of voice.stream(text, options)) {
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
