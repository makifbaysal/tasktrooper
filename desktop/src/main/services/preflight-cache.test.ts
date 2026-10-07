import { chmodSync, mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import { describe, expect, it } from "vitest";
import { PreflightCache, VERSION_FRESH_MS } from "./preflight-cache.js";

function dir(): string {
  return mkdtempSync(path.join(os.tmpdir(), "tt-preflight-cache-"));
}

function binary(where: string, body = "#!/bin/sh\necho 1.0\n"): string {
  const file = path.join(where, "tool");
  writeFileSync(file, body);
  chmodSync(file, 0o755);
  return file;
}

describe("PreflightCache", () => {
  it("survives a restart: a new instance over the same file has the same answers", async () => {
    const d = dir();
    const file = path.join(d, "preflight-cache.json");
    const bin = binary(d);
    const cache = new PreflightCache(file);
    expect(cache.setLoginPath(["/a", "/b"])).toBe(true);
    expect(cache.setLoginPath(["/a", "/b"])).toBe(false);
    cache.setVersion(bin, { code: 0, out: "tool 1.0" });
    await cache.flush();

    const later = new PreflightCache(file);
    expect(later.loginPath).toEqual(["/a", "/b"]);
    expect(later.version(bin)).toEqual({ code: 0, out: "tool 1.0", fresh: true });
  });

  it("misses once the binary is a different file", () => {
    const d = dir();
    const bin = binary(d);
    const cache = new PreflightCache(path.join(d, "c.json"));
    cache.setVersion(bin, { code: 0, out: "tool 1.0" });
    writeFileSync(bin, "#!/bin/sh\necho 2.0 with a longer body\n");
    expect(cache.version(bin)).toBeUndefined();
  });

  it("follows a symlink to what it points at, so moving the link is a miss", async () => {
    const d = dir();
    const v1 = path.join(d, "v1");
    const v2 = path.join(d, "v2");
    writeFileSync(v1, "one");
    writeFileSync(v2, "one");
    const link = path.join(d, "link");
    const { symlinkSync, unlinkSync } = await import("node:fs");
    symlinkSync(v1, link);
    const cache = new PreflightCache(path.join(d, "c.json"));
    cache.setVersion(link, { code: 0, out: "1" });
    expect(cache.version(link)?.out).toBe("1");
    unlinkSync(link);
    symlinkSync(v2, link);
    expect(cache.version(link)).toBeUndefined();
  });

  it("marks an old answer stale rather than dropping it", () => {
    const d = dir();
    const bin = binary(d);
    let now = 0;
    const cache = new PreflightCache(path.join(d, "c.json"), () => now);
    cache.setVersion(bin, { code: 0, out: "tool 1.0" });
    now = VERSION_FRESH_MS;
    expect(cache.version(bin)).toEqual({ code: 0, out: "tool 1.0", fresh: false });
  });

  it("keeps an answer to re-check as never fresh, but still answers with it", () => {
    const d = dir();
    const bin = binary(d);
    const cache = new PreflightCache(path.join(d, "c.json"));
    cache.setVersion(bin, { code: 1, out: "env: node: No such file or directory" }, { recheck: true });
    expect(cache.version(bin)).toEqual({ code: 1, out: "env: node: No such file or directory", fresh: false });
  });

  it("forgets an answer for good, across a restart", async () => {
    const d = dir();
    const file = path.join(d, "preflight-cache.json");
    const bin = binary(d);
    const cache = new PreflightCache(file);
    cache.setVersion(bin, { code: 0, out: "tool 1.0" });
    cache.forgetVersion(bin);
    expect(cache.version(bin)).toBeUndefined();
    await cache.flush();
    expect(new PreflightCache(file).version(bin)).toBeUndefined();
  });

  it("treats a corrupt file as an empty cache, and overwrites it", async () => {
    const d = dir();
    const file = path.join(d, "c.json");
    writeFileSync(file, "{not json");
    const cache = new PreflightCache(file);
    expect(cache.loginPath).toBeUndefined();
    cache.setLoginPath(["/x"]);
    await cache.flush();
    expect(JSON.parse(readFileSync(file, "utf8")).loginPath).toEqual(["/x"]);
  });
});
