import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { UpdateStatus } from "../../ipc/types.js";
import {
  CHECK_INTERVAL_MS,
  FIRST_CHECK_DELAY_MS,
  UpdateService,
  feedNotFound,
  feedUrlIsUsable,
  resolveFeed,
  signatureUnsupported,
  type Feed,
  type UpdaterBackend,
  type UpdaterEvents,
  type UpdaterLogger,
} from "./updater.js";

/**
 * The update state machine, without a packaged app.
 *
 * What can be tested here and what cannot is worth being explicit about,
 * because the interesting half of this feature lives in Squirrel.Mac and
 * Squirrel.Mac needs a real signed bundle. So:
 *
 *   tested here — which feed a launch resolves to and why, that an unusable
 *   URL is refused rather than fetched, that the phases move in the order a
 *   title bar renders, that a failed check surfaces as an error instead of
 *   silence, that nothing installs unless something is staged, and that the
 *   schedule is a delayed first check plus an interval.
 *
 *   not tested here — that Squirrel accepts a correctly signed update and
 *   refuses a wrong one. That is exercised against a real packaged app driven
 *   at a local feed; see README, "Verifying the updater against a local feed".
 */

/** A stand-in for electron-updater's `autoUpdater`, driveable event by event. */
class FakeBackend implements UpdaterBackend {
  autoDownload = false;
  autoInstallOnAppQuit = false;
  allowDowngrade = true;
  logger: UpdaterLogger | null = null;

  checks = 0;
  installs = 0;
  /** Set to make `checkForUpdates` reject, the way a dead feed does. */
  checkError: Error | null = null;

  readonly #listeners = new Map<string, ((...args: never[]) => void)[]>();

  on<K extends keyof UpdaterEvents>(event: K, listener: UpdaterEvents[K]): this {
    const list = this.#listeners.get(event) ?? [];
    list.push(listener as (...args: never[]) => void);
    this.#listeners.set(event, list);
    return this;
  }

  emit<K extends keyof UpdaterEvents>(event: K, ...args: Parameters<UpdaterEvents[K]>): void {
    for (const listener of this.#listeners.get(event) ?? []) {
      (listener as (...a: unknown[]) => void)(...args);
    }
  }

  checkForUpdates(): Promise<unknown> {
    this.checks += 1;
    if (this.checkError) return Promise.reject(this.checkError);
    return Promise.resolve(null);
  }

  quitAndInstall(): void {
    this.installs += 1;
  }
}

let dir: string;

beforeEach(() => {
  dir = mkdtempSync(path.join(tmpdir(), "tt-updater-"));
});

afterEach(() => {
  rmSync(dir, { recursive: true, force: true });
});

function paths(overrides: Partial<{ packaged: boolean; resourcesPath: string }> = {}) {
  return { packaged: true, resourcesPath: path.join(dir, "Resources"), ...overrides };
}

function bundledConfig(body: string): void {
  const resources = path.join(dir, "Resources");
  mkdirSync(resources, { recursive: true });
  writeFileSync(path.join(resources, "app-update.yml"), body, "utf8");
}

describe("feedUrlIsUsable", () => {
  it("accepts https anywhere", () => {
    expect(feedUrlIsUsable("https://storage.googleapis.com/some-bucket/mac/")).toBe(true);
  });

  it("accepts http only on the loopback interface", () => {
    expect(feedUrlIsUsable("http://127.0.0.1:8080/")).toBe(true);
    expect(feedUrlIsUsable("http://localhost:8080/")).toBe(true);
    // The whole point of the rule: a feed anyone on the path can rewrite.
    expect(feedUrlIsUsable("http://updates.example.com/")).toBe(false);
  });

  it("refuses anything that is not a URL, and anything that is not http(s)", () => {
    expect(feedUrlIsUsable("")).toBe(false);
    expect(feedUrlIsUsable("storage.googleapis.com/bucket")).toBe(false);
    expect(feedUrlIsUsable("file:///tmp/feed/")).toBe(false);
    expect(feedUrlIsUsable("gs://bucket/mac/")).toBe(false);
  });
});

describe("resolveFeed", () => {
  it("has no feed in a development run", () => {
    const feed = resolveFeed(paths({ packaged: false }));
    expect(feed.kind).toBe("none");
    expect(feed.kind === "none" && feed.reason).toMatch(/development build/);
  });

  it("has no feed when the build was packaged without one", () => {
    const feed = resolveFeed(paths());
    expect(feed.kind).toBe("none");
    expect(feed.kind === "none" && feed.reason).toMatch(/no update feed/);
  });

  it("uses the generic feed the build was packaged with", () => {
    bundledConfig("provider: generic\nurl: https://example.com/mac/\nupdaterCacheDirName: tasktrooper-desktop-updater\n");
    expect(resolveFeed(paths())).toEqual({ kind: "bundled", provider: "generic", url: "https://example.com/mac/" });
  });

  /** What the release workflow packages: `-c.publish.provider=github` and this repository. */
  it("uses the GitHub releases of this repository", () => {
    bundledConfig("provider: github\nowner: makifbaysal\nrepo: tasktrooper\nupdaterCacheDirName: tasktrooper-desktop-updater\n");
    expect(resolveFeed(paths())).toEqual({
      kind: "bundled",
      provider: "github",
      url: "https://github.com/makifbaysal/tasktrooper/releases",
    });
  });

  /**
   * electron-builder infers a `provider: github` config from the git remote
   * when no publish configuration is set. Nobody chose it, so a GitHub feed is
   * followed only when it names the repository this app publishes from — this
   * is the check at the reading end, and `publish: null` is the one at the
   * writing end.
   */
  it("refuses a GitHub feed for some other repository", () => {
    bundledConfig("provider: github\nowner: someone\nrepo: desktop\n");
    const feed = resolveFeed(paths());
    expect(feed.kind).toBe("none");
    expect(feed.kind === "none" && feed.reason).toMatch(/inferred/);
  });

  it("refuses a provider it does not read", () => {
    bundledConfig("provider: s3\nbucket: somebody-else\n");
    const feed = resolveFeed(paths());
    expect(feed.kind).toBe("none");
    expect(feed.kind === "none" && feed.reason).toMatch(/'s3'/);
  });

  /** Plain http to a remote host is a feed anybody on the path can rewrite. */
  it("refuses a bundled feed whose URL could never be trusted", () => {
    bundledConfig("provider: generic\nurl: http://updates.example.com/\n");
    const feed = resolveFeed(paths());
    expect(feed.kind).toBe("none");
    expect(feed.kind === "none" && feed.reason).toMatch(/not a usable URL/);
  });
});

/**
 * A private repository answers the unauthenticated releases feed with 404, and
 * that is the state every early install is in. It must not reach the title bar.
 */
describe("feedNotFound", () => {
  const github: Feed = {
    kind: "bundled",
    provider: "github",
    url: "https://github.com/makifbaysal/tasktrooper/releases",
  };

  it("recognises the answers a private or release-less repository gives", () => {
    expect(feedNotFound(github, new Error("HttpError: 404 Not Found"))).toBe(true);
    expect(
      feedNotFound(github, new Error("Unable to find latest version on GitHub, please ensure a production release exists")),
    ).toBe(true);
  });

  it("stays out of the way of every other failure, and of every other feed", () => {
    expect(feedNotFound(github, new Error("net::ERR_INTERNET_DISCONNECTED"))).toBe(false);
    expect(feedNotFound({ kind: "bundled", provider: "generic", url: "https://example.com/mac/" }, new Error("404"))).toBe(false);
    expect(feedNotFound({ kind: "none", reason: "no feed" }, new Error("404"))).toBe(false);
  });
});

describe("signatureUnsupported", () => {
  it("recognises Squirrel refusing to read the running app's own signature", () => {
    expect(signatureUnsupported(new Error("Could not get code signature for running application"))).toBe(true);
  });

  it("stays out of the way of a downloaded update's signature actually failing", () => {
    expect(signatureUnsupported(new Error("Code signature at URL file:///… did not pass validation"))).toBe(false);
  });

  it("stays out of the way of every other failure", () => {
    expect(signatureUnsupported(new Error("net::ERR_INTERNET_DISCONNECTED"))).toBe(false);
  });
});

describe("UpdateService", () => {
  function service(feedKind: "ok" | "github" | "none" = "ok") {
    const backend = new FakeBackend();
    const seen: UpdateStatus[] = [];
    const lines: string[] = [];
    const feed: Feed =
      feedKind === "ok"
        ? { kind: "bundled", provider: "generic", url: "https://example.com/mac/" }
        : feedKind === "github"
          ? { kind: "bundled", provider: "github", url: "https://github.com/makifbaysal/tasktrooper/releases" }
          : { kind: "none", reason: "no feed here" };
    const svc = new UpdateService({
      backend,
      feed,
      onStatus: (status) => seen.push(status),
      now: () => 1_700_000_000_000,
      logLine: (line) => lines.push(line),
    });
    return { backend, svc, seen, lines };
  }

  it("is unsupported, silent and inert without a feed", async () => {
    const { backend, svc, seen } = service("none");
    svc.start();
    expect(svc.status).toEqual({ phase: "unsupported", detail: "no feed here" });
    // No listeners, no timers, no requests: an app that cannot update should
    // not be holding any of the three.
    expect(backend.logger).toBeNull();
    await svc.check();
    expect(backend.checks).toBe(0);
    expect(seen).toEqual([]);
    expect(svc.install()).toBe(false);
  });

  it("configures the backend to download in the background and stage eagerly", () => {
    const { backend, svc } = service();
    svc.start();
    expect(backend.autoDownload).toBe(true);
    // On macOS this hands the download to Squirrel as soon as it lands — which
    // is where the signature is checked — and leaves ShipIt to swap the bundle
    // when this process exits, without relaunching. It is not a quit hook;
    // MacUpdater has none. See services/updater.ts.
    expect(backend.autoInstallOnAppQuit).toBe(true);
    expect(backend.allowDowngrade).toBe(false);
  });

  it("walks idle → checking → available → ready as the download progresses", async () => {
    const { backend, svc } = service();
    svc.start();
    expect(svc.status.phase).toBe("idle");

    backend.emit("checking-for-update");
    expect(svc.status.phase).toBe("checking");

    backend.emit("update-available", { version: "0.2.0" });
    expect(svc.status).toMatchObject({ phase: "available", version: "0.2.0", percent: 0 });

    backend.emit("download-progress", { percent: 41.6 });
    expect(svc.status.percent).toBe(42);

    backend.emit("update-downloaded", { version: "0.2.0" });
    expect(svc.status).toMatchObject({ phase: "ready", version: "0.2.0", percent: 100 });
    expect(svc.ready).toBe(true);
    expect(svc.status.detail).toContain("0.2.0");

    // A late progress line must not un-ready a button somebody is reaching for.
    backend.emit("download-progress", { percent: 99 });
    expect(svc.status.phase).toBe("ready");
    // And a staged update is not re-checked; a second download over a finished
    // one is the failure that would produce.
    await svc.check();
    expect(backend.checks).toBe(0);
  });

  it("reports being up to date without holding on to a stale version", () => {
    const { backend, svc } = service();
    svc.start();
    backend.emit("update-available", { version: "0.2.0" });
    backend.emit("update-not-available", { version: "0.1.0" });
    expect(svc.status).toMatchObject({ phase: "current", checkedAt: 1_700_000_000_000 });
    expect(svc.status.version).toBeUndefined();
  });

  it("surfaces a failed check as one sentence rather than a stack", async () => {
    const { backend, svc, lines } = service();
    svc.start();
    backend.checkError = new Error("net::ERR_NAME_NOT_RESOLVED\n    at Object.<anonymous> (/x/y.js:1:1)");
    await svc.check();
    expect(svc.status.phase).toBe("error");
    expect(svc.status.detail).toBe("net::ERR_NAME_NOT_RESOLVED");
    expect(svc.status.checkedAt).toBe(1_700_000_000_000);
    // The tooltip is easy to miss; the reason must also reach whatever this
    // launch's logLine is wired to (main/index.ts: a file under
    // `app.getPath("logs")`) so it is not the only place to look.
    expect(lines).toEqual(["update check failed: net::ERR_NAME_NOT_RESOLVED"]);
  });

  /**
   * An ad-hoc signed build (release.yml's fallback when CSC_LINK is not set)
   * can never verify an update, on any feed. That is expected for this kind
   * of build, not a fault the user can act on — the same treatment a dev run
   * or an unpublished `npm run package` build already gets for carrying no
   * feed at all, so it must not draw the same warning triangle a transient
   * network failure does.
   */
  it("stays quiet, not an error, when the running app cannot be signature-checked", async () => {
    const { backend, svc, lines } = service();
    svc.start();
    backend.checkError = new Error("Could not get code signature for running application");
    await svc.check();
    expect(svc.status.phase).toBe("unsupported");
    expect(svc.status.detail).toBe("This build is not signed for automatic updates.");
    expect(lines).toHaveLength(1);
    expect(lines[0]).toContain("Could not get code signature");
  });

  it("surfaces an error the backend emits, including one that arrives after staging", () => {
    const { backend, svc } = service();
    svc.start();
    backend.emit("update-downloaded", { version: "0.2.0" });
    expect(svc.ready).toBe(true);
    // Squirrel rejecting the signature of a staged build arrives this way, and
    // it must take the restart button away rather than leave it offering a
    // restart that cannot install anything.
    backend.emit("error", new Error("Code signature at URL … did not pass validation"));
    expect(svc.status.phase).toBe("error");
    expect(svc.ready).toBe(false);
    expect(svc.install()).toBe(false);
  });

  /**
   * The repository is private until the project is opened up, so this is the
   * only answer the feed gives at first. "update check failed" over it would be
   * every user's first impression of the app.
   */
  it("says nothing when the GitHub feed has nothing to offer yet", async () => {
    const { backend, svc } = service("github");
    svc.start();
    backend.checkError = new Error("HttpError: 404 Not Found\n    method: GET url: https://github.com/…/releases.atom");
    await svc.check();
    expect(svc.status.phase).toBe("current");
    expect(svc.status.detail).toBeUndefined();

    // Any other failure on the same feed still surfaces.
    backend.checkError = new Error("net::ERR_INTERNET_DISCONNECTED");
    await svc.check();
    expect(svc.status.phase).toBe("error");
  });

  it("installs only what has been staged", () => {
    const { backend, svc } = service();
    svc.start();
    expect(svc.install()).toBe(false);
    expect(backend.installs).toBe(0);

    backend.emit("update-downloaded", { version: "0.2.0" });
    expect(svc.install()).toBe(true);
    expect(backend.installs).toBe(1);
  });

  it("checks once shortly after launch and then on an interval, and stops when told to", () => {
    vi.useFakeTimers();
    try {
      const { backend, svc } = service();
      svc.start();
      expect(backend.checks).toBe(0);

      vi.advanceTimersByTime(FIRST_CHECK_DELAY_MS);
      expect(backend.checks).toBe(1);

      vi.advanceTimersByTime(CHECK_INTERVAL_MS);
      expect(backend.checks).toBe(2);

      svc.stop();
      vi.advanceTimersByTime(CHECK_INTERVAL_MS * 3);
      expect(backend.checks).toBe(2);
    } finally {
      vi.useRealTimers();
    }
  });

  it("only starts once, so one event is one status push", () => {
    const { backend, svc, seen } = service();
    svc.start();
    svc.start();
    backend.emit("checking-for-update");
    // A second start would register a second set of listeners, and every
    // status change would then be pushed to the title bar twice.
    expect(seen).toHaveLength(1);
  });
});
