import type { Server } from "node:http";
import type { AddressInfo } from "node:net";
import { afterEach, describe, expect, it, vi } from "vitest";
import { createEmbeddingsServer, HEADERS_TIMEOUT_MS, KEEP_ALIVE_TIMEOUT_MS } from "./server.js";

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
      embed: vi.fn<(text: string) => Promise<Float64Array>>(async () => new Float64Array([1, 0])),
      tokenCount: vi.fn(() => 1),
      release: vi.fn(async () => undefined),
    };
  }

  async function post(port: number): Promise<{ status: number; body: { error?: { code: string } } }> {
    const res = await fetch(`http://127.0.0.1:${port}/v1/embeddings`, {
      method: "POST",
      body: JSON.stringify({ input: "hi" }),
    });
    return { status: res.status, body: (await res.json()) as { error?: { code: string } } };
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
        new Promise<Float64Array>((resolve) => {
          finish = () => resolve(new Float64Array([1]));
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
});
