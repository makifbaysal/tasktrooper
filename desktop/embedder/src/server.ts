import { createServer, type IncomingMessage, type Server, type ServerResponse } from "node:http";

export interface EmbeddingEngine {
  embed(text: string): Promise<Float64Array>;
  tokenCount(text: string): number;
  release(): Promise<void>;
}

/**
 * An OpenAI-compatible embeddings server on loopback.
 *
 * Plain `http.Server`, no framework. Three routes, and their shapes are
 * OpenAI's because that is the contract the backend speaks: it is configured
 * with this server's address as `EMBEDDINGS_BASE_URL` and talks to it with an
 * ordinary OpenAI-compatible client.
 *
 *   POST /v1/embeddings  the one that does the work.
 *   POST /embeddings     the same handler at the un-versioned path, because a
 *                        base URL with and without `/v1` are both things a
 *                        client is configured with, and a 404 here reads as
 *                        "the model is broken".
 *   GET  /v1/models      so a client that lists models before using one — which
 *                        several OpenAI-compatible clients do at setup — finds
 *                        the model this server actually serves.
 *
 * Answers `503` with a small JSON error body until the model is downloaded and
 * the ONNX session is loaded — never hangs, never crashes the request, because
 * a caller mid-index must see a clean "not ready yet" rather than a timeout
 * indistinguishable from a hang.
 */

// The model this engine is: one set of weights, downloaded by download.ts.
// Kept as a local literal because this bundle is built by its own esbuild
// invocation (scripts/build-embedder.mjs) and everything it needs lives under
// embedder/.
const DEFAULT_MODEL = "nomic-embed-text-v1.5";

/** A JSON body larger than this is refused before it is parsed. Generous for a batch of strings, not unbounded. */
const MAX_BODY_BYTES = 64 * 1024 * 1024;

/**
 * How long an idle keep-alive connection stays open. Node's default is five
 * seconds, shorter than the Go client keeps its idle connections, so the client
 * wrote requests onto sockets this server had just closed and an index failed
 * with "connection reset by peer". This outlives the client's 30 seconds.
 */
export const KEEP_ALIVE_TIMEOUT_MS = 65_000;
/** Must exceed the keep-alive timeout, or Node closes the socket first anyway. */
export const HEADERS_TIMEOUT_MS = 66_000;

interface EmbeddingsRequestBody {
  input?: unknown;
  model?: unknown;
  encoding_format?: unknown;
}

/** Release the model after this long without an embedding request. */
export const DEFAULT_IDLE_UNLOAD_MS = 10 * 60_000;

export interface EmbeddingsServerOptions {
  /** 0 disables unloading. */
  idleUnloadMs?: number;
  /** Brings the model back after an idle unload; never called before the first `setEngine`. */
  load?: () => Promise<EmbeddingEngine>;
  log?: (message: string) => void;
}

export interface EmbedderServer {
  server: Server;
  /** Flip from "not ready" to "ready" once the model has loaded. */
  setEngine(engine: EmbeddingEngine): void;
}

export function createEmbeddingsServer(options: EmbeddingsServerOptions = {}): EmbedderServer {
  const idleUnloadMs = options.idleUnloadMs ?? DEFAULT_IDLE_UNLOAD_MS;
  const log = options.log ?? ((message: string) => process.stderr.write(`[embedder] ${message}\n`));

  let engine: EmbeddingEngine | null = null;
  let everLoaded = false;
  let reloading: Promise<EmbeddingEngine> | null = null;
  let inFlight = 0;
  let idleTimer: NodeJS.Timeout | null = null;

  const disarm = (): void => {
    if (idleTimer) clearTimeout(idleTimer);
    idleTimer = null;
  };

  const arm = (): void => {
    disarm();
    if (idleUnloadMs <= 0 || !engine) return;
    idleTimer = setTimeout(unload, idleUnloadMs);
    idleTimer.unref();
  };

  function unload(): void {
    idleTimer = null;
    // A request that began after the timer was armed has not re-armed it yet.
    if (inFlight > 0 || !engine) return;
    const released = engine;
    engine = null;
    log(`idle for ${Math.round(idleUnloadMs / 1000)}s; unloading the embedding model.`);
    released.release().catch((err) => log(`releasing the embedding model failed: ${describe(err)}`));
  }

  const install = (next: EmbeddingEngine): void => {
    engine = next;
    everLoaded = true;
    arm();
  };

  const acquire = async (): Promise<EmbeddingEngine | null> => {
    if (engine) return engine;
    if (!everLoaded || !options.load) return null;
    if (!reloading) {
      log("reloading the embedding model for a new request.");
      reloading = options
        .load()
        .then((loaded) => {
          install(loaded);
          return loaded;
        })
        .finally(() => {
          reloading = null;
        });
    }
    try {
      return await reloading;
    } catch (err) {
      log(`reloading the embedding model failed: ${describe(err)}`);
      return null;
    }
  };

  const server = createServer((req, res) => {
    handle(req, res, { acquire, log, begin: () => { inFlight++; disarm(); }, end: () => { inFlight--; arm(); } }).catch((err) => {
      // A handler that throws after headers were already sent cannot be
      // answered again; this is the last-resort log for that case.
      log(`request failed: ${describe(err)}`);
      if (!res.headersSent) sendJson(res, 500, errorBody("internal_error", "The embedder failed to answer this request."));
      else res.end();
    });
  });

  server.keepAliveTimeout = KEEP_ALIVE_TIMEOUT_MS;
  server.headersTimeout = HEADERS_TIMEOUT_MS;
  return { server, setEngine: install };
}

interface Lifecycle {
  log: (message: string) => void;
  acquire(): Promise<EmbeddingEngine | null>;
  begin(): void;
  end(): void;
}

const EMBEDDINGS_PATHS = new Set(["/embeddings", "/v1/embeddings"]);
const MODELS_PATHS = new Set(["/models", "/v1/models"]);

async function handle(req: IncomingMessage, res: ServerResponse, life: Lifecycle): Promise<void> {
  const route = (req.url ?? "").split("?")[0] ?? "";

  // Listed whether or not the weights have finished downloading: the question
  // "what does this server serve" has an answer before the answer is loadable,
  // and a client that lists models at setup must not be told "none".
  if (req.method === "GET" && MODELS_PATHS.has(route)) {
    sendJson(res, 200, {
      object: "list",
      data: [{ id: DEFAULT_MODEL, object: "model", owned_by: "tasktrooper" }],
    });
    return;
  }

  if (req.method !== "POST" || !EMBEDDINGS_PATHS.has(route)) {
    sendJson(
      res,
      404,
      errorBody("not_found", "This server answers POST /v1/embeddings and GET /v1/models."),
    );
    return;
  }

  life.begin();
  try {
    await embeddings(req, res, life);
  } finally {
    life.end();
  }
}

async function embeddings(req: IncomingMessage, res: ServerResponse, life: Lifecycle): Promise<void> {
  const engine = await life.acquire();
  if (!engine) {
    sendJson(res, 503, errorBody("not_ready", "The embedding model is still downloading or loading on this Mac."));
    return;
  }

  let body: EmbeddingsRequestBody;
  try {
    body = await readJsonBody(req);
  } catch (err) {
    life.log(`rejected a request body: ${describe(err)}`);
    sendJson(res, 400, errorBody("bad_request", "Request body is not valid JSON."));
    return;
  }

  const inputs = normalizeInput(body.input);
  if (!inputs) {
    sendJson(res, 400, errorBody("bad_request", "input must be a non-empty string or array of strings."));
    return;
  }

  const model = typeof body.model === "string" && body.model !== "" ? body.model : DEFAULT_MODEL;

  try {
    const data: { object: "embedding"; embedding: number[]; index: number }[] = [];
    let promptTokens = 0;
    for (let i = 0; i < inputs.length; i++) {
      const text = inputs[i];
      const vector = await engine.embed(text);
      promptTokens += engine.tokenCount(text);
      data.push({ object: "embedding", embedding: Array.from(vector), index: i });
    }
    sendJson(res, 200, {
      object: "list",
      data,
      model,
      usage: { prompt_tokens: promptTokens, total_tokens: promptTokens },
    });
  } catch (err) {
    // Details stay in the log: an error's text can carry internals (paths,
    // stack frames) that do not belong in a response.
    life.log(`inference failed: ${describe(err)}`);
    sendJson(res, 500, errorBody("inference_failed", "Computing the embedding failed."));
  }
}

/** OpenAI accepts a bare string or an array of strings. Anything else, or an empty array, is refused. */
function normalizeInput(input: unknown): string[] | null {
  if (typeof input === "string" && input !== "") return [input];
  if (Array.isArray(input) && input.length > 0 && input.every((v): v is string => typeof v === "string")) {
    return input;
  }
  return null;
}

function readJsonBody(req: IncomingMessage): Promise<EmbeddingsRequestBody> {
  return new Promise((resolve, reject) => {
    const chunks: Buffer[] = [];
    let total = 0;
    req.on("data", (chunk: Buffer) => {
      total += chunk.length;
      if (total > MAX_BODY_BYTES) {
        reject(new Error(`request body exceeds ${MAX_BODY_BYTES} bytes`));
        req.destroy();
        return;
      }
      chunks.push(chunk);
    });
    req.on("end", () => {
      const raw = Buffer.concat(chunks).toString("utf8");
      if (raw === "") {
        resolve({});
        return;
      }
      try {
        const parsed: unknown = JSON.parse(raw);
        if (typeof parsed !== "object" || parsed === null) {
          reject(new Error("request body must be a JSON object"));
          return;
        }
        resolve(parsed as EmbeddingsRequestBody);
      } catch (err) {
        reject(new Error(`request body is not valid JSON: ${describe(err)}`));
      }
    });
    req.on("error", reject);
  });
}

function errorBody(code: string, message: string): { error: { code: string; message: string } } {
  return { error: { code, message } };
}

function sendJson(res: ServerResponse, status: number, body: unknown): void {
  const payload = Buffer.from(JSON.stringify(body), "utf8");
  res.writeHead(status, { "content-type": "application/json", "content-length": String(payload.length) });
  res.end(payload);
}

function describe(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}
