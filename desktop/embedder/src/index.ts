import path from "node:path";
import { downloadAllWithRetry, MAX_BACKOFF_MS, MIN_BACKOFF_MS } from "./download.js";
import { loadEngine } from "./engine.js";
import { createEmbeddingsServer, DEFAULT_IDLE_UNLOAD_MS } from "./server.js";
import { armParentWatchdog } from "./watchdog.js";

/**
 * Entrypoint: parse `--cache-dir`, bind a loopback port the OS assigns, print
 * it, and only then start downloading and loading the model — in that order,
 * because the Electron supervisor that spawned this process is waiting on
 * the printed port, not on the model.
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

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

/**
 * Download and load the model, retrying the whole sequence forever on
 * failure. `downloadAllWithRetry` already retries its own network failures
 * internally and only ever returns on success, so the backoff here exists
 * for the one failure it cannot retry past on its own: the ONNX session
 * failing to construct from files that hashed correctly.
 */
async function loadModelForever(cacheDir: string, wasmDir: string) {
  let backoff = MIN_BACKOFF_MS;
  for (;;) {
    try {
      const files = await downloadAllWithRetry(path.join(cacheDir, "embedding-model"), log);
      return await loadEngine(files, wasmDir);
    } catch (err) {
      const delay = backoff;
      backoff = Math.min(backoff * 2, MAX_BACKOFF_MS);
      log.warn(`model load failed: ${err instanceof Error ? err.message : String(err)}; retrying in ${Math.round(delay / 1000)}s`);
      await sleep(delay);
    }
  }
}

function idleUnloadMs(raw: string | undefined): number {
  if (raw === undefined || raw.trim() === "") return DEFAULT_IDLE_UNLOAD_MS;
  const value = Number(raw);
  return Number.isInteger(value) && value >= 0 ? value : DEFAULT_IDLE_UNLOAD_MS;
}

function main(): void {
  const cacheDir = parseCacheDir(process.argv.slice(2));

  // Staged alongside this bundled file by scripts/build-embedder.mjs — see
  // engine.ts's loadEngine() for why this is pointed at explicitly rather
  // than left to onnxruntime-web's relative auto-resolution.
  const wasmDir = __dirname;

  const { server, setEngine } = createEmbeddingsServer({
    idleUnloadMs: idleUnloadMs(process.env.EMBEDDER_IDLE_UNLOAD_MS),
    load: () => loadModelForever(cacheDir, wasmDir),
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
    loadModelForever(cacheDir, wasmDir)
      .then((engine) => {
        setEngine(engine);
        log.info("model loaded; serving real embeddings.");
      })
      .catch((err) => {
        // loadModelForever never rejects — this is unreachable in practice,
        // and logged rather than silently swallowed if that ever changes.
        log.warn(`unexpected: model loading stopped retrying: ${err instanceof Error ? err.message : String(err)}`);
      });
  });
}

main();
