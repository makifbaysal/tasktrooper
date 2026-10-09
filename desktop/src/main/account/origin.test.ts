import { describe, expect, it } from "vitest";
import {
  ACCOUNT_ORIGIN_ENV,
  DEFAULT_ACCOUNT_ORIGIN,
  accountPartition,
  defaultAccountOrigin,
  normalizeAccountOrigin,
} from "./origin.js";

describe("normalizeAccountOrigin", () => {
  const packaged = { allowLoopbackHttp: false };
  const dev = { allowLoopbackHttp: true };

  it("keeps an https origin, port included, and drops a bare trailing slash", () => {
    expect(normalizeAccountOrigin("https://app.tasktrooper.ai/", packaged)).toBe("https://app.tasktrooper.ai");
    expect(normalizeAccountOrigin("https://acme.example:8443", packaged)).toBe("https://acme.example:8443");
  });

  it("refuses plain http anywhere but loopback, and loopback only in development", () => {
    expect(normalizeAccountOrigin("http://app.tasktrooper.ai", dev)).toBeNull();
    expect(normalizeAccountOrigin("http://127.0.0.1:8080", packaged)).toBeNull();
    expect(normalizeAccountOrigin("http://127.0.0.1:8080", dev)).toBe("http://127.0.0.1:8080");
    expect(normalizeAccountOrigin("http://localhost:5173", dev)).toBe("http://localhost:5173");
  });

  it("refuses what is not an origin: a path, a query, credentials, another scheme", () => {
    for (const raw of [
      "https://app.tasktrooper.ai/login",
      "https://app.tasktrooper.ai/?x=1",
      "https://user:pw@app.tasktrooper.ai",
      "file:///etc/passwd",
      "javascript:alert(1)",
      "app://tasktrooper",
      "not a url",
    ]) {
      expect(normalizeAccountOrigin(raw, dev), raw).toBeNull();
    }
  });
});

describe("defaultAccountOrigin", () => {
  it("is the TaskTrooper cloud in a packaged build, whatever the environment says", () => {
    expect(defaultAccountOrigin({ packaged: true, env: { [ACCOUNT_ORIGIN_ENV]: "http://127.0.0.1:8080" } })).toBe(
      DEFAULT_ACCOUNT_ORIGIN,
    );
  });

  it("follows the development override when it is an acceptable origin", () => {
    expect(defaultAccountOrigin({ packaged: false, env: { [ACCOUNT_ORIGIN_ENV]: "http://127.0.0.1:8080" } })).toBe(
      "http://127.0.0.1:8080",
    );
    expect(defaultAccountOrigin({ packaged: false, env: { [ACCOUNT_ORIGIN_ENV]: "http://evil.example" } })).toBe(
      DEFAULT_ACCOUNT_ORIGIN,
    );
    expect(defaultAccountOrigin({ packaged: false, env: {} })).toBe(DEFAULT_ACCOUNT_ORIGIN);
  });
});

describe("accountPartition", () => {
  it("is persistent and per origin", () => {
    expect(accountPartition("https://app.tasktrooper.ai")).toBe("persist:account:https://app.tasktrooper.ai");
    expect(accountPartition("https://a.example")).not.toBe(accountPartition("https://b.example"));
  });
});
