import type { Server } from "node:http";
import type { AddressInfo } from "node:net";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  createEmbeddingsServer,
  HEADERS_TIMEOUT_MS,
  KEEP_ALIVE_TIMEOUT_MS,
  LOAD_RETRY_BASE_MS,
  LOAD_RETRY_MAX_MS,
} from "./server.js";

describe("createEmbeddingsServer", () => {
  it("keeps idle connections open longer than the Go client does", () => {
    const { server } = createEmbeddingsServer();
    expect(server.keepAliveTimeout).toBe(KEEP_ALIVE_TIMEOUT_MS);
    expect(server.headersTimeout).toBe(HEADERS_TIMEOUT_MS);
    expect(server.headersTimeout).toBeGreaterThan(server.keepAliveTimeout);
    expect(KEEP_ALIVE_TIMEOUT_MS).toBeGreaterThan(30_000);
  });
});

describe("idle unload", () => {
  function fakeEngine() {
    return {
      embed: vi.fn<(texts: string[]) => Promise<{ vectors: Float64Array[]; promptTokens: number }>>(async (texts) => ({
        vectors: texts.map(() => new Float64Array([1, 0])),
        promptTokens: texts.length,
      })),
      release: vi.fn(async () => undefined),
      alive: true,
    };
  }

  interface Answer {
    status: number;
    body: { error?: { code: string }; data?: { index: number }[]; usage?: { prompt_tokens: number } };
  }

  async function post(port: number, input: string | string[] = "hi"): Promise<Answer> {
    const res = await fetch(`http://127.0.0.1:${port}/v1/embeddings`, {
      method: "POST",
      body: JSON.stringify({ input }),
    });
    return { status: res.status, body: (await res.json()) as Answer["body"] };
  }

  async function start(options: Parameters<typeof createEmbeddingsServer>[0]) {
    const made = createEmbeddingsServer({ log: () => undefined, ...options });
    await new Promise<void>((resolve) => made.server.listen(0, "127.0.0.1", resolve));
    const port = (made.server.address() as AddressInfo).port;
    return { ...made, port };
  }

  const open: Server[] = [];
  afterEach(async () => {
    vi.useRealTimers();
    for (const s of open.splice(0)) {
      s.closeAllConnections();
      await new Promise((resolve) => s.close(resolve));
    }
  });

  it("answers 503 not_ready before the first load", async () => {
    const load = vi.fn();
    const s = await start({ idleUnloadMs: 10, load });
    open.push(s.server);
    const res = await post(s.port);
    expect(res.status).toBe(503);
    expect(res.body.error?.code).toBe("not_ready");
    expect(load).not.toHaveBeenCalled();
  });

  it("releases the engine when idle and reloads once on the next request", async () => {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
    const first = fakeEngine();
    const second = fakeEngine();
    const load = vi.fn(async () => second);
    const s = await start({ idleUnloadMs: 1000, load });
    open.push(s.server);
    s.setEngine(first);

    await vi.advanceTimersByTimeAsync(1000);
    expect(first.release).toHaveBeenCalledTimes(1);

    const res = await post(s.port);
    expect(res.status).toBe(200);
    expect(load).toHaveBeenCalledTimes(1);
    expect(second.embed).toHaveBeenCalled();
  });

  it("shares one reload between concurrent requests", async () => {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
    const next = fakeEngine();
    let finish!: () => void;
    const load = vi.fn(
      () =>
        new Promise<ReturnType<typeof fakeEngine>>((resolve) => {
          finish = () => resolve(next);
        }),
    );
    const s = await start({ idleUnloadMs: 1000, load });
    open.push(s.server);
    s.setEngine(fakeEngine());
    await vi.advanceTimersByTimeAsync(1000);

    const both = Promise.all([post(s.port), post(s.port)]);
    await vi.waitFor(() => expect(load).toHaveBeenCalledTimes(1));
    await new Promise((resolve) => setImmediate(resolve));
    finish();
    const results = await both;
    expect(results.map((r) => r.status)).toEqual([200, 200]);
    expect(load).toHaveBeenCalledTimes(1);
  });

  it("does not unload while a request is in flight", async () => {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
    const engine = fakeEngine();
    let finish!: () => void;
    engine.embed.mockImplementation(
      () =>
        new Promise((resolve) => {
          finish = () => resolve({ vectors: [new Float64Array([1])], promptTokens: 1 });
        }),
    );
    const s = await start({ idleUnloadMs: 1000, load: vi.fn() });
    open.push(s.server);
    s.setEngine(engine);

    const pending = post(s.port);
    await vi.waitFor(() => expect(engine.embed).toHaveBeenCalled());
    await vi.advanceTimersByTimeAsync(5000);
    expect(engine.release).not.toHaveBeenCalled();
    finish();
    expect((await pending).status).toBe(200);

    await vi.advanceTimersByTimeAsync(1000);
    expect(engine.release).toHaveBeenCalledTimes(1);
  });

  it("never unloads when disabled", async () => {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
    const engine = fakeEngine();
    const s = await start({ idleUnloadMs: 0 });
    open.push(s.server);
    s.setEngine(engine);
    await vi.advanceTimersByTimeAsync(60 * 60_000);
    expect(engine.release).not.toHaveBeenCalled();
  });

  it("hands every input to the engine in one call and reports its token count", async () => {
    const engine = fakeEngine();
    engine.embed.mockResolvedValue({ vectors: [new Float64Array([1]), new Float64Array([2]), new Float64Array([3])], promptTokens: 42 });
    const s = await start({ idleUnloadMs: 0 });
    open.push(s.server);
    s.setEngine(engine);

    const res = await post(s.port, ["a", "b", "c"]);
    expect(res.status).toBe(200);
    expect(engine.embed).toHaveBeenCalledTimes(1);
    expect(engine.embed).toHaveBeenCalledWith(["a", "b", "c"]);
    expect(res.body.data?.map((d) => d.index)).toEqual([0, 1, 2]);
    expect(res.body.usage?.prompt_tokens).toBe(42);
  });
});

/**
 * The model is downloaded eagerly and LOADED lazily: a machine that never
 * indexes anything never pays for a loaded model, and the first request after
 * the download waits for the load instead of being refused.
 */
describe("lazy load", () => {
  const open: Server[] = [];
  afterEach(async () => {
    vi.useRealTimers();
    for (const s of open.splice(0)) {
      s.closeAllConnections();
      await new Promise((resolve) => s.close(resolve));
    }
  });

  function engine(alive = true) {
    return {
      embed: vi.fn(async (texts: string[]) => ({ vectors: texts.map(() => new Float64Array([1])), promptTokens: 1 })),
      release: vi.fn(async () => undefined),
      alive,
    };
  }

  async function start(options: Parameters<typeof createEmbeddingsServer>[0]) {
    const made = createEmbeddingsServer({ log: () => undefined, ...options });
    await new Promise<void>((resolve) => made.server.listen(0, "127.0.0.1", resolve));
    open.push(made.server);
    return { ...made, port: (made.server.address() as AddressInfo).port };
  }

  async function status(port: number): Promise<number> {
    const res = await fetch(`http://127.0.0.1:${port}/v1/embeddings`, { method: "POST", body: JSON.stringify({ input: "x" }) });
    await res.arrayBuffer();
    return res.status;
  }

  it("does not load anything until loading is enabled and a request arrives", async () => {
    const load = vi.fn(async () => engine());
    const s = await start({ idleUnloadMs: 0 });
    expect(await status(s.port)).toBe(503);

    s.enableLoading(load);
    expect(load).not.toHaveBeenCalled();

    expect(await status(s.port)).toBe(200);
    expect(await status(s.port)).toBe(200);
    expect(load).toHaveBeenCalledTimes(1);
  });

  it("unloads after the idle period and loads again for the next request", async () => {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
    const first = engine();
    const second = engine();
    const load = vi.fn().mockResolvedValueOnce(first).mockResolvedValueOnce(second);
    const s = await start({ idleUnloadMs: 1000 });
    s.enableLoading(load);

    expect(await status(s.port)).toBe(200);
    await vi.advanceTimersByTimeAsync(1000);
    expect(first.release).toHaveBeenCalledTimes(1);

    expect(await status(s.port)).toBe(200);
    expect(load).toHaveBeenCalledTimes(2);
    expect(second.embed).toHaveBeenCalled();
  });

  it("replaces an engine whose worker died instead of answering with it", async () => {
    const dead = engine(false);
    const fresh = engine();
    const load = vi.fn(async () => fresh);
    const s = await start({ idleUnloadMs: 0, load });
    s.setEngine(dead);

    expect(await status(s.port)).toBe(200);
    expect(dead.embed).not.toHaveBeenCalled();
    expect(fresh.embed).toHaveBeenCalledTimes(1);
  });

  it("answers 503 rather than hanging when the load fails, and tries again once the backoff has passed", async () => {
    vi.useFakeTimers({ toFake: ["Date"] });
    const load = vi.fn().mockRejectedValueOnce(new Error("bad wasm")).mockResolvedValueOnce(engine());
    const s = await start({ idleUnloadMs: 0 });
    s.enableLoading(load);
    expect(await status(s.port)).toBe(503);
    vi.setSystemTime(Date.now() + LOAD_RETRY_BASE_MS);
    expect(await status(s.port)).toBe(200);
    expect(load).toHaveBeenCalledTimes(2);
  });

  async function answer(port: number): Promise<{ status: number; retryAfter: string | null }> {
    const res = await fetch(`http://127.0.0.1:${port}/v1/embeddings`, { method: "POST", body: JSON.stringify({ input: "x" }) });
    await res.arrayBuffer();
    return { status: res.status, retryAfter: res.headers.get("retry-after") };
  }

  it("does not load again while backing off after a failed load, and says when to retry", async () => {
    vi.useFakeTimers({ toFake: ["Date"] });
    const load = vi.fn().mockRejectedValue(new Error("bad wasm"));
    const s = await start({ idleUnloadMs: 0 });
    s.enableLoading(load);

    expect(await answer(s.port)).toEqual({ status: 503, retryAfter: String(LOAD_RETRY_BASE_MS / 1000) });
    expect(await answer(s.port)).toEqual({ status: 503, retryAfter: String(LOAD_RETRY_BASE_MS / 1000) });
    expect(load).toHaveBeenCalledTimes(1);

    vi.setSystemTime(Date.now() + LOAD_RETRY_BASE_MS);
    expect(await answer(s.port)).toEqual({ status: 503, retryAfter: String((2 * LOAD_RETRY_BASE_MS) / 1000) });
    expect(load).toHaveBeenCalledTimes(2);

    vi.setSystemTime(Date.now() + LOAD_RETRY_BASE_MS);
    expect(await answer(s.port)).toEqual({ status: 503, retryAfter: String(LOAD_RETRY_BASE_MS / 1000) });
    expect(load).toHaveBeenCalledTimes(2);
    vi.setSystemTime(Date.now() + LOAD_RETRY_BASE_MS);
    expect((await answer(s.port)).status).toBe(503);
    expect(load).toHaveBeenCalledTimes(3);
  });

  it("caps the backoff and resets it after a successful load", async () => {
    vi.useFakeTimers({ toFake: ["Date"] });
    let failing = true;
    const loaded = engine();
    const load = vi.fn(async () => {
      if (failing) throw new Error("bad wasm");
      return loaded;
    });
    const s = await start({ idleUnloadMs: 0 });
    s.enableLoading(load);

    for (let i = 0; i < 10; i++) {
      expect(await answer(s.port)).toEqual({ status: 503, retryAfter: expect.any(String) });
      vi.setSystemTime(Date.now() + LOAD_RETRY_MAX_MS);
    }
    expect(load).toHaveBeenCalledTimes(10);

    failing = false;
    expect((await answer(s.port)).status).toBe(200);

    failing = true;
    loaded.alive = false;
    expect(await answer(s.port)).toEqual({ status: 503, retryAfter: String(LOAD_RETRY_BASE_MS / 1000) });
    expect(load).toHaveBeenCalledTimes(12);
  });

  it("refuses a malformed request without loading the model", async () => {
    const load = vi.fn(async () => engine());
    const s = await start({ idleUnloadMs: 0 });
    s.enableLoading(load);
    const res = await fetch(`http://127.0.0.1:${s.port}/v1/embeddings`, { method: "POST", body: JSON.stringify({ input: [] }) });
    await res.arrayBuffer();
    expect(res.status).toBe(400);
    const garbage = await fetch(`http://127.0.0.1:${s.port}/v1/embeddings`, { method: "POST", body: "{" });
    await garbage.arrayBuffer();
    expect(garbage.status).toBe(400);
    expect(load).not.toHaveBeenCalled();
  });
});
