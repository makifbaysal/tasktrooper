import { once } from "node:events";
import { createServer, type IncomingMessage, type Server, type ServerResponse } from "node:http";
import type { SpeechEngine } from "./tts/speech-worker.js";

export interface EmbeddingEngine {
  /** Every input in one call, so the engine can batch them; `promptTokens` comes from the same tokenization. */
  embed(texts: string[]): Promise<{ vectors: Float64Array[]; promptTokens: number }>;
  release(): Promise<void>;
  /** False once the engine can no longer answer (its worker died), so the next request loads a new one. */
  readonly alive?: boolean;
}

/**
 * An OpenAI-compatible embeddings server on loopback, which also speaks.
 *
 * Plain `http.Server`, no framework. The embedding routes' shapes are
 * OpenAI's because that is the contract the backend speaks: it is configured
 * with this server's address as `EMBEDDINGS_BASE_URL` and talks to it with an
 * ordinary OpenAI-compatible client.
 *
 *   POST /v1/embeddings    the one that does the work.
 *   POST /embeddings       the same handler at the un-versioned path, because a
 *                          base URL with and without `/v1` are both things a
 *                          client is configured with, and a 404 here reads as
 *                          "the model is broken".
 *   GET  /v1/models        so a client that lists models before using one — which
 *                          several OpenAI-compatible clients do at setup — finds
 *                          the model this server actually serves.
 *   POST /v1/audio/speech  the bundled Turkish voice (and at /audio/speech, for
 *                          the same reason). OpenAI's path and request shape,
 *                          but the answer is raw PCM streamed as it is made,
 *                          not an encoded file: see speech().
 *
 * Answers `503` with a small JSON error body until the model is downloaded —
 * never hangs, never crashes the request, because a caller mid-index must see
 * a clean "not ready yet" rather than a timeout indistinguishable from a hang.
 * Once it is on disk the model is loaded by the first request that needs it
 * (that request waits for the load), and unloaded again after
 * `idleUnloadMs` without one.
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

interface SpeechRequestBody {
  input?: unknown;
  voice?: unknown;
  speed?: unknown;
}

/** The voice's sample rate; every chunk the engine yields is at this rate. */
export const SPEECH_SAMPLE_RATE = 48_000;
/** The one voice there is. */
export const SPEECH_VOICES: readonly string[] = ["tr"];
/** A spoken reply, not a document: longer text is refused before any of it is read aloud. */
export const MAX_SPEECH_CHARS = 4000;
/** Room for MAX_SPEECH_CHARS with every one of them escaped in JSON. */
const MAX_SPEECH_BODY_BYTES = 64 * 1024;
export const MIN_SPEECH_SPEED = 0.5;
export const MAX_SPEECH_SPEED = 2;

/** Release the model after this long without an embedding request. */
export const DEFAULT_IDLE_UNLOAD_MS = 10 * 60_000;

/**
 * After a failed load, requests are refused for this long (doubling per
 * consecutive failure, up to the cap) instead of each paying for another load
 * of a model that just failed to load.
 */
export const LOAD_RETRY_BASE_MS = 5_000;
export const LOAD_RETRY_MAX_MS = 5 * 60_000;

interface Releasable {
  release(): Promise<void>;
  readonly alive?: boolean;
}

type Acquired<E> = { engine: E } | { engine: null; retryAfterMs?: number };

export interface EmbeddingsServerOptions {
  /** 0 disables unloading. Applies to the embedding model and the voice alike. */
  idleUnloadMs?: number;
  /**
   * Brings the model back after an idle unload. Never called before the first
   * `setEngine`; `enableLoading` is the lazy alternative that needs no first
   * engine at all.
   */
  load?: () => Promise<EmbeddingEngine>;
  log?: (message: string) => void;
}

export interface EmbedderServer {
  server: Server;
  /** Flip from "not ready" to "ready" with an engine that is already loaded. */
  setEngine(engine: EmbeddingEngine): void;
  /**
   * Flip from "not ready" to "loadable": the model is on disk, and the first
   * request that needs it calls `load` (or the `load` option) and waits for it.
   */
  enableLoading(load?: () => Promise<EmbeddingEngine>): void;
  /**
   * The voice files are on disk: `/v1/audio/speech` loads the voice with
   * `load` on its first request. Until this is called that route answers 503.
   */
  enableSpeech(load: () => Promise<SpeechEngine>): void;
}

/**
 * One lazily loaded model: loaded by the first request that needs it (which
 * waits), shared by every request while loaded, released after `idleUnloadMs`
 * with no request in flight, and backed off from after a failed load. The
 * embedding model and the voice each have one, so either can idle out while
 * the other is busy.
 */
class LazyModel<E extends Releasable> {
  #engine: E | null = null;
  #loader: (() => Promise<E>) | null = null;
  #everLoaded = false;
  #loading: Promise<E> | null = null;
  #loadFailures = 0;
  #retryAt = 0;
  #inFlight = 0;
  #idleTimer: NodeJS.Timeout | null = null;

  constructor(
    readonly label: string,
    readonly idleUnloadMs: number,
    readonly log: (message: string) => void,
  ) {}

  get loadable(): boolean {
    return this.#loader !== null || this.#engine !== null;
  }

  setLoader(load: (() => Promise<E>) | null, keepExisting = false): void {
    this.#loader = keepExisting ? (this.#loader ?? load) : load;
  }

  install(next: E): void {
    this.#engine = next;
    this.#everLoaded = true;
    this.#loadFailures = 0;
    this.#retryAt = 0;
    this.#arm();
  }

  begin(): void {
    this.#inFlight++;
    this.#disarm();
  }

  end(): void {
    this.#inFlight--;
    this.#arm();
  }

  async acquire(): Promise<Acquired<E>> {
    if (this.#engine && this.#engine.alive !== false) return { engine: this.#engine };
    if (this.#engine) {
      this.log(`${this.label} stopped answering; loading it again.`);
      this.#engine = null;
      this.#disarm();
    }
    const loader = this.#loader;
    if (!loader) return { engine: null };
    if (!this.#loading) {
      const wait = this.#retryAt - Date.now();
      if (wait > 0) return { engine: null, retryAfterMs: wait };
      this.log(this.#everLoaded ? `reloading ${this.label} for a new request.` : `loading ${this.label} for its first request.`);
      this.#loading = loader()
        .then(
          (loaded) => {
            this.install(loaded);
            return loaded;
          },
          (err: unknown) => {
            this.#loadFailures++;
            const backoff = Math.min(LOAD_RETRY_BASE_MS * 2 ** (this.#loadFailures - 1), LOAD_RETRY_MAX_MS);
            this.#retryAt = Date.now() + backoff;
            this.log(`loading ${this.label} failed: ${describe(err)}; not trying again for ${Math.round(backoff / 1000)}s.`);
            throw err;
          },
        )
        .finally(() => {
          this.#loading = null;
        });
    }
    try {
      return { engine: await this.#loading };
    } catch {
      return { engine: null, retryAfterMs: Math.max(0, this.#retryAt - Date.now()) };
    }
  }

  #disarm(): void {
    if (this.#idleTimer) clearTimeout(this.#idleTimer);
    this.#idleTimer = null;
  }

  #arm(): void {
    this.#disarm();
    if (this.idleUnloadMs <= 0 || !this.#engine) return;
    this.#idleTimer = setTimeout(() => this.#unload(), this.idleUnloadMs);
    this.#idleTimer.unref();
  }

  #unload(): void {
    this.#idleTimer = null;
    // A request that began after the timer was armed has not re-armed it yet.
    if (this.#inFlight > 0 || !this.#engine) return;
    const released = this.#engine;
    this.#engine = null;
    this.log(`idle for ${Math.round(this.idleUnloadMs / 1000)}s; unloading ${this.label}.`);
    released.release().catch((err) => this.log(`releasing ${this.label} failed: ${describe(err)}`));
  }
}

export function createEmbeddingsServer(options: EmbeddingsServerOptions = {}): EmbedderServer {
  const idleUnloadMs = options.idleUnloadMs ?? DEFAULT_IDLE_UNLOAD_MS;
  const log = options.log ?? ((message: string) => process.stderr.write(`[embedder] ${message}\n`));
  const embedding = new LazyModel<EmbeddingEngine>("the embedding model", idleUnloadMs, log);
  const speech = new LazyModel<SpeechEngine>("the voice", idleUnloadMs, log);

  const server = createServer((req, res) => {
    handle(req, res, { log, embedding, speech }).catch((err) => {
      // A handler that throws after headers were already sent cannot be
      // answered again; this is the last-resort log for that case.
      log(`request failed: ${describe(err)}`);
      if (!res.headersSent) sendJson(res, 500, errorBody("internal_error", "The embedder failed to answer this request."));
      else res.end();
    });
  });

  server.keepAliveTimeout = KEEP_ALIVE_TIMEOUT_MS;
  server.headersTimeout = HEADERS_TIMEOUT_MS;
  return {
    server,
    setEngine: (next) => {
      embedding.setLoader(options.load ?? null, true);
      embedding.install(next);
    },
    enableLoading: (load) => {
      embedding.setLoader(load ?? options.load ?? null);
    },
    enableSpeech: (load) => {
      speech.setLoader(load);
    },
  };
}

interface Routes {
  log: (message: string) => void;
  embedding: LazyModel<EmbeddingEngine>;
  speech: LazyModel<SpeechEngine>;
}

const EMBEDDINGS_PATHS = new Set(["/embeddings", "/v1/embeddings"]);
const MODELS_PATHS = new Set(["/models", "/v1/models"]);
const SPEECH_PATHS = new Set(["/audio/speech", "/v1/audio/speech"]);

async function handle(req: IncomingMessage, res: ServerResponse, routes: Routes): Promise<void> {
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

  const model = req.method !== "POST" ? null : EMBEDDINGS_PATHS.has(route) ? routes.embedding : SPEECH_PATHS.has(route) ? routes.speech : null;
  if (!model) {
    sendJson(
      res,
      404,
      errorBody("not_found", "This server answers POST /v1/embeddings, POST /v1/audio/speech and GET /v1/models."),
    );
    return;
  }

  model.begin();
  try {
    if (model === routes.speech) await speech(req, res, routes.speech);
    else await embeddings(req, res, routes.embedding);
  } finally {
    model.end();
  }
}

async function embeddings(req: IncomingMessage, res: ServerResponse, life: LazyModel<EmbeddingEngine>): Promise<void> {
  // The body is checked before acquire(): a malformed request must not cost a
  // model load.
  let body: EmbeddingsRequestBody;
  try {
    body = await readJsonBody<EmbeddingsRequestBody>(req);
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

  const acquired = await life.acquire();
  if (!acquired.engine) {
    const headers: Record<string, string> =
      acquired.retryAfterMs === undefined ? {} : { "retry-after": String(Math.max(1, Math.ceil(acquired.retryAfterMs / 1000))) };
    sendJson(res, 503, errorBody("not_ready", "The embedding model is still downloading or loading on this machine."), headers);
    return;
  }

  try {
    const { vectors, promptTokens } = await acquired.engine.embed(inputs);
    const data = vectors.map((vector, index) => ({ object: "embedding" as const, embedding: Array.from(vector), index }));
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

/**
 * Speak `input` and stream it back as it is made: 200 with
 * `application/octet-stream`, `X-Sample-Rate: 48000`, and a chunked body of
 * mono float32 little-endian PCM (every platform this ships on is little
 * endian, so the engine's own buffers go out as they are). The first chunk is
 * one second of audio, so playback can start long before the text is done.
 *
 * Status is decided before the first byte: a load or inference failure ahead
 * of it is a JSON error like every other route's. A failure after it can only
 * cut the response off, which the client sees as a broken stream rather than
 * a short clip. A client that hangs up stops the generation.
 */
async function speech(req: IncomingMessage, res: ServerResponse, life: LazyModel<SpeechEngine>): Promise<void> {
  // Refused from the header when it says so, because past the limit the body
  // reader drops the connection and the 413 would never arrive.
  if (Number(req.headers["content-length"] ?? 0) > MAX_SPEECH_BODY_BYTES) {
    sendJson(res, 413, errorBody("too_large", `input must be at most ${MAX_SPEECH_CHARS} characters.`), { connection: "close" });
    return;
  }
  let body: SpeechRequestBody;
  try {
    body = await readJsonBody<SpeechRequestBody>(req, MAX_SPEECH_BODY_BYTES);
  } catch (err) {
    life.log(`rejected a speech request body: ${describe(err)}`);
    if (err instanceof BodyTooLargeError) sendJson(res, 413, errorBody("too_large", `input must be at most ${MAX_SPEECH_CHARS} characters.`));
    else sendJson(res, 400, errorBody("bad_request", "Request body is not valid JSON."));
    return;
  }

  const { input, voice = "tr", speed = 1 } = body;
  if (typeof input !== "string" || input.trim() === "") {
    sendJson(res, 400, errorBody("bad_request", "input must be a non-empty string."));
    return;
  }
  if (input.length > MAX_SPEECH_CHARS) {
    sendJson(res, 413, errorBody("too_large", `input must be at most ${MAX_SPEECH_CHARS} characters.`));
    return;
  }
  if (typeof voice !== "string" || !SPEECH_VOICES.includes(voice)) {
    sendJson(res, 400, errorBody("unsupported_voice", `voice must be one of: ${SPEECH_VOICES.join(", ")}.`));
    return;
  }
  if (typeof speed !== "number" || !Number.isFinite(speed) || speed < MIN_SPEECH_SPEED || speed > MAX_SPEECH_SPEED) {
    sendJson(res, 400, errorBody("bad_request", `speed must be a number from ${MIN_SPEECH_SPEED} to ${MAX_SPEECH_SPEED}.`));
    return;
  }

  if (!life.loadable) {
    sendJson(res, 503, errorBody("voice_unavailable", "The voice files are not installed with this copy of the app."));
    return;
  }
  const acquired = await life.acquire();
  if (!acquired.engine) {
    const headers: Record<string, string> =
      acquired.retryAfterMs === undefined ? {} : { "retry-after": String(Math.max(1, Math.ceil(acquired.retryAfterMs / 1000))) };
    sendJson(res, 503, errorBody("not_ready", "The voice is still loading on this machine."), headers);
    return;
  }

  const abort = new AbortController();
  const onClose = (): void => {
    if (!res.writableFinished) abort.abort();
  };
  res.on("close", onClose);
  try {
    for await (const pcm of acquired.engine.stream(input, { speed, signal: abort.signal })) {
      if (abort.signal.aborted) break;
      if (!res.headersSent) startAudio(res);
      if (!res.write(Buffer.from(pcm.buffer, pcm.byteOffset, pcm.byteLength))) {
        // A slow reader leaves the audio queued in the engine's stream, not
        // in socket buffers. Generation is not paused: at about ten times
        // real time, MAX_SPEECH_CHARS is at most ~40 MB of PCM.
        await Promise.race([once(res, "drain"), once(abort.signal, "abort")]);
      }
    }
    if (abort.signal.aborted) return;
    if (!res.headersSent) startAudio(res);
    res.end();
  } catch (err) {
    life.log(`speech failed: ${describe(err)}`);
    if (!res.headersSent) sendJson(res, 500, errorBody("inference_failed", "Speaking the text failed."));
    else res.destroy();
  } finally {
    res.off("close", onClose);
    abort.abort();
  }
}

function startAudio(res: ServerResponse): void {
  res.writeHead(200, {
    "content-type": "application/octet-stream",
    "x-sample-rate": String(SPEECH_SAMPLE_RATE),
    "cache-control": "no-store",
  });
}

/** OpenAI accepts a bare string or an array of strings. Anything else, or an empty array, is refused. */
function normalizeInput(input: unknown): string[] | null {
  if (typeof input === "string" && input !== "") return [input];
  if (Array.isArray(input) && input.length > 0 && input.every((v): v is string => typeof v === "string")) {
    return input;
  }
  return null;
}

class BodyTooLargeError extends Error {}

function readJsonBody<T extends object>(req: IncomingMessage, maxBytes: number = MAX_BODY_BYTES): Promise<T> {
  return new Promise((resolve, reject) => {
    const chunks: Buffer[] = [];
    let total = 0;
    req.on("data", (chunk: Buffer) => {
      total += chunk.length;
      if (total > maxBytes) {
        reject(new BodyTooLargeError(`request body exceeds ${maxBytes} bytes`));
        req.destroy();
        return;
      }
      chunks.push(chunk);
    });
    req.on("end", () => {
      const raw = Buffer.concat(chunks).toString("utf8");
      if (raw === "") {
        resolve({} as T);
        return;
      }
      try {
        const parsed: unknown = JSON.parse(raw);
        if (typeof parsed !== "object" || parsed === null) {
          reject(new Error("request body must be a JSON object"));
          return;
        }
        resolve(parsed as T);
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

function sendJson(res: ServerResponse, status: number, body: unknown, headers: Record<string, string> = {}): void {
  const payload = Buffer.from(JSON.stringify(body), "utf8");
  res.writeHead(status, { ...headers, "content-type": "application/json", "content-length": String(payload.length) });
  res.end(payload);
}

function describe(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}
