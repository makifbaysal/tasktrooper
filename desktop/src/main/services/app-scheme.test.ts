import { describe, expect, it, vi } from "vitest";

/**
 * `originOf` exists because of a bug that reached a user, and both halves of
 * that bug are asserted here.
 *
 * The product is served from `app://tasktrooper`, and `new URL(...).origin` is
 * the literal string `"null"` for any scheme the URL standard does not treat as
 * special. So:
 *
 *   - every `origin === APP_ORIGIN` check failed, which is what refused the
 *     page's own "Continue with Google" with "this request did not come from
 *     the TaskTrooper app running in this window", and
 *   - every `originA === originB` check PASSED for any two custom-scheme URLs,
 *     because both sides were `"null"`. That is the dangerous half: it made
 *     `evil://anything` same-origin with the product in the navigation guard.
 *
 * The first failure is loud and the second is silent, which is why the second
 * gets the longer test.
 */
vi.mock("electron", () => ({
  app: { getAppPath: () => "/app", isPackaged: false },
  net: { fetch: () => Promise.resolve(new Response("")) },
  protocol: { registerSchemesAsPrivileged: () => {}, handle: () => {} },
}));

const { APP_ORIGIN, WebRootFiles, catalogRoot, originOf } = await import("./app-scheme.js");

describe("originOf", () => {
  it("gives the app scheme a comparable origin, which URL.origin does not", () => {
    // The comparison isTrustedCloudSender makes, on the URL the window actually
    // holds once the home route has loaded.
    expect(originOf("app://tasktrooper/board")).toBe(APP_ORIGIN);
    expect(new URL("app://tasktrooper/board").origin).toBe("null");
  });

  it("does not make two different custom schemes equal", () => {
    // The silent half: each of these pairs compared EQUAL through URL.origin,
    // because both sides were the string "null".
    expect(originOf("evil://anything")).not.toBe(originOf("app://tasktrooper"));
    expect(originOf("app://impostor")).not.toBe(originOf("app://tasktrooper"));
    expect(originOf("file:///etc/passwd")).toBeNull();
  });

  it("still agrees with URL.origin for http(s), port included", () => {
    for (const url of ["https://example.com/board", "http://127.0.0.1:5173/", "https://x.dev:8443/a"]) {
      expect(originOf(url)).toBe(new URL(url).origin);
    }
  });

  it("is null rather than throwing for something that is not a URL", () => {
    expect(originOf("")).toBeNull();
    expect(originOf("not a url")).toBeNull();
  });
});

describe("catalogRoot", () => {
  it("resolves to the monorepo catalog next to the app in dev", () => {
    expect(catalogRoot()).toBe("/catalog");
  });
});

describe("WebRootFiles", () => {
  it("knows the built files, sees one added later, and never calls a directory a file", async () => {
    const { mkdtempSync, mkdirSync, writeFileSync } = await import("node:fs");
    const os = await import("node:os");
    const path = await import("node:path");
    const root = mkdtempSync(path.join(os.tmpdir(), "tt-webroot-"));
    mkdirSync(path.join(root, "assets"));
    writeFileSync(path.join(root, "index.html"), "<html>");
    writeFileSync(path.join(root, "assets", "app-abc123.js"), "x");

    const files = new WebRootFiles(root);
    expect(await files.has(path.join(root, "index.html"))).toBe(true);
    expect(await files.has(path.join(root, "assets", "app-abc123.js"))).toBe(true);
    expect(await files.has(path.join(root, "settings", "llm"))).toBe(false);
    expect(await files.has(path.join(root, "assets"))).toBe(false);

    // A dev rebuild while the app runs.
    writeFileSync(path.join(root, "assets", "app-def456.js"), "y");
    expect(await files.has(path.join(root, "assets", "app-def456.js"))).toBe(true);
  });
});
