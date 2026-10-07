import os from "node:os";
import path from "node:path";
import { downloadAllWithRetry } from "./download.js";
import { createEmbeddingsServer, DEFAULT_IDLE_UNLOAD_MS } from "./server.js";
import { armParentWatchdog } from "./watchdog.js";
import { defaultWasmThreads, startWorkerEngine } from "./worker-engine.js";

/**
 * Entrypoint: parse `--cache-dir`, bind a loopback port the OS assigns, print
 * it, and only then start downloading the model — in that order, because the
 * Electron supervisor that spawned this process is waiting on the printed
 * port, not on the model.
 *
 * The download is eager, so the first request never waits on the network; the
 * LOAD is lazy, and happens in a worker thread (`worker-engine.ts`) the first
 * time a request needs it. A machine that never indexes anything never pays
 * the ~650 MB a loaded model costs, and an idle unload terminates that worker,
 * which is the only thing that gives wasm memory back.
 *
 * Spawned as `process.execPath <this file> --cache-dir <userData>` with
 * `ELECTRON_RUN_AS_NODE=1` (see `main/supervisor/supervisor.ts`), which is
 * why nothing here may import `electron` — under that env var the Electron
 * binary behaves like a plain Node binary and no `app`/`BrowserWindow`
 * module exists to import.
 */

function parseCacheDir(argv: string[]): string {
  const flagIndex = argv.indexOf("--cache-dir");
  const value = flagIndex === -1 ? undefined : argv[flagIndex + 1];
  if (!value) {
    process.stderr.write("embedder: --cache-dir <path> is required\n");
    process.exit(1);
  }
  return value;
}

const log = {
  info(msg: string): void {
    process.stdout.write(`[embedder] ${msg}\n`);
  },
  warn(msg: string): void {
    process.stderr.write(`[embedder] ${msg}\n`);
  },
};

function idleUnloadMs(raw: string | undefined): number {
  if (raw === undefined || raw.trim() === "") return DEFAULT_IDLE_UNLOAD_MS;
  const value = Number(raw);
  return Number.isInteger(value) && value >= 0 ? value : DEFAULT_IDLE_UNLOAD_MS;
}

function wasmThreads(raw: string | undefined): number {
  const value = Number(raw);
  if (raw !== undefined && raw.trim() !== "" && Number.isInteger(value) && value >= 1 && value <= 16) return value;
  return defaultWasmThreads(os.availableParallelism());
}

function main(): void {
  const cacheDir = parseCacheDir(process.argv.slice(2));

  // Staged alongside this bundled file by scripts/build-embedder.mjs — see
  // engine.ts's loadEngine() for why this is pointed at explicitly rather
  // than left to onnxruntime-web's relative auto-resolution. The worker's own
  // bundle is staged in the same directory.
  const wasmDir = __dirname;
  const workerScript = path.join(__dirname, "worker.cjs");
  const numThreads = wasmThreads(process.env.EMBEDDER_THREADS);

  const { server, enableLoading } = createEmbeddingsServer({
    idleUnloadMs: idleUnloadMs(process.env.EMBEDDER_IDLE_UNLOAD_MS),
  });

  let shuttingDown = false;
  const shutdown = (): void => {
    if (shuttingDown) return;
    shuttingDown = true;
    setTimeout(() => process.exit(0), 2_000).unref();
    server.close(() => process.exit(0));
    server.closeAllConnections();
  };
  armParentWatchdog({
    env: process.env,
    stdin: process.stdin,
    ppid: () => process.ppid,
    kill: (pid, signal) => process.kill(pid, signal),
    setInterval,
    shutdown,
  });

  server.on("error", (err) => {
    process.stderr.write(`[embedder] fatal: HTTP server error: ${err instanceof Error ? err.message : String(err)}\n`);
    process.exit(1);
  });

  server.listen(0, "127.0.0.1", () => {
    const address = server.address();
    const port = address && typeof address === "object" ? address.port : 0;
    // The FIRST thing this process writes to stdout, and a fixed string on
    // purpose: this is how main/supervisor/supervisor.ts learns the port,
    // the same technique runner-log.ts uses to parse the Go runner's own
    // fixed status strings out of its stdout.
    process.stdout.write(`EMBEDDER_LISTENING ${port}\n`);

    // Kicked off only after the server is already listening, so a slow or
    // offline download never delays the port this process exists to hand
    // back. Retries indefinitely in the background; the server keeps
    // answering 503 the whole time.
    downloadAllWithRetry(path.join(cacheDir, "embedding-model"), log)
      .then((files) => {
        enableLoading(() => startWorkerEngine(workerScript, { files, wasmDir, numThreads }));
        log.info(`model is on disk; it loads on the first request (${numThreads} wasm threads).`);
      })
      .catch((err) => {
        // downloadAllWithRetry never rejects — this is unreachable in
        // practice, and logged rather than silently swallowed if that changes.
        log.warn(`unexpected: model download stopped retrying: ${err instanceof Error ? err.message : String(err)}`);
      });
  });
}

main();
