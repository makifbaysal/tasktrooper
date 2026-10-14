import { createHash } from "node:crypto";
import { existsSync, mkdtempSync, readdirSync, readFileSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import { describe, expect, it } from "vitest";
import { fetchFile, type FetchLike } from "./fetch-file.js";
import { CancelledError, InstallError } from "./types.js";

const BODY = Buffer.from("#!/bin/sh\necho installed\n".repeat(200));
const SHA256 = createHash("sha256").update(BODY).digest("hex");
const SHA1 = createHash("sha1").update(BODY).digest("hex");

function serve(body: Buffer, init: { url?: string; status?: number } = {}): FetchLike & { asked: string[] } {
  const asked: string[] = [];
  const fn = (url: string): Promise<Response> => {
    asked.push(url);
    const res = new Response(new Uint8Array(body), {
      status: init.status ?? 200,
      headers: { "content-length": String(body.length) },
    });
    if (init.url !== undefined) Object.defineProperty(res, "url", { value: init.url });
    return Promise.resolve(res);
  };
  return Object.assign(fn, { asked });
}

const dir = (): string => mkdtempSync(path.join(os.tmpdir(), "tt-fetch-"));

describe("fetchFile", () => {
  it("writes the whole file, reporting percent, once its digests match", async () => {
    const dest = path.join(dir(), "install.sh");
    const percents: number[] = [];
    await fetchFile("https://example.test/install.sh", dest, {
      signal: new AbortController().signal,
      sha256: SHA256,
      sha1: SHA1.toUpperCase(),
      onPercent: (p) => percents.push(p),
      fetch: serve(BODY),
    });
    expect(readFileSync(dest)).toEqual(BODY);
    expect(percents.at(-1)).toBe(100);
    expect(readdirSync(path.dirname(dest))).toEqual(["install.sh"]);
  });

  it("deletes a file that does not match its pinned digest", async () => {
    const dest = path.join(dir(), "tool");
    await expect(
      fetchFile("https://example.test/tool", dest, { signal: new AbortController().signal, sha256: "0".repeat(64), fetch: serve(BODY) }),
    ).rejects.toThrow(/did not match its published checksum/);
    expect(readdirSync(path.dirname(dest))).toEqual([]);
  });

  it("refuses plain http, and a redirect off https", async () => {
    const fetch = serve(BODY);
    await expect(fetchFile("http://example.test/x", path.join(dir(), "x"), { signal: new AbortController().signal, fetch })).rejects.toBeInstanceOf(InstallError);
    expect(fetch.asked).toEqual([]);
    const dest = path.join(dir(), "y");
    await expect(
      fetchFile("https://example.test/y", dest, { signal: new AbortController().signal, fetch: serve(BODY, { url: "http://evil.test/y" }) }),
    ).rejects.toThrow(/redirected off https/);
    expect(existsSync(dest)).toBe(false);
  });

  it("gives up on a connection that goes quiet, saying so", async () => {
    const dest = path.join(dir(), "slow.zip");
    const stalled: FetchLike = (_url, init) =>
      Promise.resolve(
        new Response(
          new ReadableStream<Uint8Array>({
            start(controller) {
              controller.enqueue(new Uint8Array([1, 2, 3]));
              init.signal.addEventListener("abort", () => controller.error(new Error("aborted")));
            },
          }),
        ),
      );
    await expect(fetchFile("https://example.test/slow.zip", dest, { signal: new AbortController().signal, fetch: stalled, idleMs: 30 })).rejects.toThrow(
      /^The download of slow\.zip stalled: nothing arrived for \d+ seconds\.$/,
    );
    expect(readdirSync(path.dirname(dest))).toEqual([]);
  });

  it("says which download failed on an HTTP error, and is cancelled by its signal", async () => {
    await expect(
      fetchFile("https://example.test/gone.zip", path.join(dir(), "z"), { signal: new AbortController().signal, fetch: serve(BODY, { status: 404 }) }),
    ).rejects.toThrow("Could not download gone.zip: HTTP 404.");
    const controller = new AbortController();
    controller.abort();
    await expect(
      fetchFile("https://example.test/a", path.join(dir(), "a"), {
        signal: controller.signal,
        fetch: () => Promise.reject(new Error("aborted")),
      }),
    ).rejects.toBeInstanceOf(CancelledError);
  });
});
