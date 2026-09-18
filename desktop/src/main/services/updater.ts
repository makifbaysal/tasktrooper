import { existsSync, readFileSync } from "node:fs";
import path from "node:path";
import type { UpdateStatus } from "../../ipc/types.js";

/**
 * Auto-update.
 *
 * This app ships the web UI it renders and the backend it supervises, so a
 * change to either reaches a machine only by replacing the `.app`. Without this
 * file that is a `.dmg` installed by hand, every time.
 *
 * ## What actually installs the update
 *
 * On macOS, electron-updater's `MacUpdater` is a thin driver over **Squirrel.Mac**,
 * which is the thing that swaps the bundle. That matters for three reasons and
 * each one shaped the design here:
 *
 *  1. **Squirrel validates the code signature of what it is about to install
 *     against the running app's own designated requirement.** That is the
 *     security of this feature — not the transport. It is why the update feed
 *     can be a plain public HTTPS bucket: a hostile feed can serve any bytes it
 *     likes and Squirrel refuses to install them, because they are not signed by
 *     our Developer ID. Nothing in this file turns that off, and there is no
 *     code path here that installs anything Squirrel has not accepted.
 *  2. **`autoInstallOnAppQuit` works differently here than the name suggests,
 *     and the difference was measured rather than read.** `MacUpdater` extends
 *     `AppUpdater`, not `BaseUpdater`, so electron-updater registers no
 *     `app.on("quit")` hook at all. What the flag actually does on macOS is hand
 *     the downloaded zip to Squirrel immediately, which validates its signature,
 *     unpacks it, and leaves **ShipIt** — a separate process — waiting for this
 *     one to exit. ShipIt then swaps the bundle **and does not relaunch**.
 *
 *     So an update genuinely does apply on quit, silently, with no surprise
 *     relaunch. That is the behaviour this app wants and it is why the flag is
 *     on. It also means the swap happens strictly after this process exits,
 *     which in every quit path here is strictly after `supervisor.drain()` has
 *     resolved — see `quit.ts`. Children are down before a byte moves.
 *  3. **The explicit press is the other ending, and it is the one that
 *     relaunches.** `quitAndInstall()` asks ShipIt to bring the app back after
 *     installing, which is what "Restart to update" promises. Nothing else in
 *     this app calls it, so nothing else can restart the app under someone.
 *
 * ## Why nothing here talks to Electron
 *
 * Everything Electron-shaped is passed in: the backend (`electron-updater`'s
 * `autoUpdater`) and the two paths in `resolveFeed`. That is what makes the
 * state machine testable without a packaged app, which is the half of this
 * feature that can be tested at all.
 */

/** Set to anything non-empty to route electron-updater's own log to stderr. */
export const FEED_DEBUG_ENV = "TASKTROOPER_UPDATE_DEBUG";

/**
 * Long enough that the first check is not competing with detection, the tray,
 * the web app's first paint and the backend's own start.
 */
export const FIRST_CHECK_DELAY_MS = 20_000;

/**
 * Six hours. This app is left running for days, so an interval is what makes
 * updates arrive at all; making it shorter buys nothing a person would notice
 * and costs a request against the feed per copy of the app.
 */
export const CHECK_INTERVAL_MS = 6 * 60 * 60 * 1000;

const BUNDLED_CONFIG_FILE = "app-update.yml";

/**
 * The one repository this app accepts a GitHub feed for: the releases the
 * `release` workflow publishes. Anything else in a bundled config was
 * inferred from somebody's git remote rather than chosen — see `resolveFeed`.
 */
const GITHUB_OWNER = "makifbaysal";
const GITHUB_REPO = "tasktrooper";

// --- the feed ---------------------------------------------------------------

export interface FeedPaths {
  packaged: boolean;
  /** `process.resourcesPath` — where electron-builder puts `app-update.yml`. */
  resourcesPath: string;
}

/**
 * Where update metadata is fetched from.
 *
 * `none` is a first-class, expected state, not a failure: a dev run has no
 * feed, and so does a `npm run package` build, which is deliberately still the
 * ad-hoc build it always was. Both say so plainly rather than checking a URL
 * that was never configured and reporting a network error.
 */
export type Feed = { kind: "bundled"; provider: "generic" | "github"; url: string } | { kind: "none"; reason: string };

/**
 * https, or http on the loopback interface and nowhere else.
 *
 * The same rule `shell.openExternal` follows, for the same reason: plain http
 * to a remote host is a feed anybody on the path can rewrite. Squirrel would
 * still refuse to install the result, but a download that can never succeed is
 * not worth starting.
 */
export function feedUrlIsUsable(raw: string): boolean {
  let url: URL;
  try {
    url = new URL(raw);
  } catch {
    return false;
  }
  if (url.protocol === "https:") return url.hostname.length > 0;
  if (url.protocol !== "http:") return false;
  return url.hostname === "127.0.0.1" || url.hostname === "localhost" || url.hostname === "[::1]" || url.hostname === "::1";
}

/** One top-level scalar out of an electron-builder update config. */
function field(file: string, name: string): string | undefined {
  try {
    const match = new RegExp(`^${name}:\\s*(.+?)\\s*$`, "m").exec(readFileSync(file, "utf8"));
    return match?.[1]?.replace(/^["']|["']$/g, "");
  } catch {
    return undefined;
  }
}

/**
 * Decide where this launch fetches update metadata from.
 *
 * One source: the `app-update.yml` a packaging run deliberately put in the
 * bundle, and only the two shapes this project publishes — a `generic` URL, or
 * the `github` releases of this repository. No environment override, and
 * nothing this repository points at by default — a build with no feed is
 * honestly unsupported rather than quietly checking a URL nobody configured.
 */
export function resolveFeed(paths: FeedPaths): Feed {
  if (!paths.packaged) {
    return {
      kind: "none",
      reason: "Updates only apply to an installed app — this one is running from a development build.",
    };
  }

  const bundled = path.join(paths.resourcesPath, BUNDLED_CONFIG_FILE);
  if (existsSync(bundled)) {
    /**
     * Two providers, and reading this file at all is load-bearing rather than
     * defensive tidiness.
     *
     * electron-builder does not leave `app-update.yml` out when no publish
     * configuration is set — it **guesses one from the git remote** and writes
     * a `provider: github` config nobody chose. A build that trusted that file
     * would check a repository it was never meant to check, forever.
     *
     * `publish: null` in electron-builder.yml stops the guess at the source.
     * This refuses it at the other end: a generic feed must carry a URL that
     * could be trusted, and a GitHub feed must name this repository, which the
     * release workflow states explicitly and a guess cannot.
     */
    const provider = field(bundled, "provider");

    if (provider === "github") {
      // The release workflow passes owner and repo on the command line, so a
      // config naming anything else is one electron-builder guessed. The guess
      // is what this check exists for; the identity is the whole of it.
      const owner = field(bundled, "owner");
      const repo = field(bundled, "repo");
      if (owner !== GITHUB_OWNER || repo !== GITHUB_REPO) {
        return {
          kind: "none",
          reason:
            "This build carries a GitHub update feed for a repository this app does not publish from, " +
            "so it was inferred from a git remote rather than chosen, and it is ignored.",
        };
      }
      return { kind: "bundled", provider: "github", url: `https://github.com/${owner}/${repo}/releases` };
    }

    if (provider !== "generic") {
      return {
        kind: "none",
        reason: `This build's update feed uses the '${provider ?? "unknown"}' provider, which this app does not read.`,
      };
    }

    const url = field(bundled, "url");
    if (!url || !feedUrlIsUsable(url)) {
      return {
        kind: "none",
        reason: "This build's update feed is not a usable URL, so it will not be fetched.",
      };
    }
    return { kind: "bundled", provider: "generic", url };
  }

  return {
    kind: "none",
    reason:
      "This build carries no update feed, so it cannot update itself. Packaging with a " +
      "`publish` configuration is what adds one.",
  };
}

// --- the backend ------------------------------------------------------------

export interface UpdaterLogger {
  info(message: string): void;
  warn(message: string): void;
  error(message: string): void;
  debug?(message: string): void;
}

export interface UpdaterEvents {
  "checking-for-update": () => void;
  "update-available": (info: { version?: string }) => void;
  "update-not-available": (info: { version?: string }) => void;
  "download-progress": (progress: { percent?: number }) => void;
  "update-downloaded": (info: { version?: string }) => void;
  error: (err: unknown) => void;
}

/**
 * The slice of electron-updater's `AppUpdater` this service drives.
 *
 * Narrow on purpose. Everything omitted is something this app has decided not
 * to do: `setFeedURL` (the config file is the one mechanism), `autoDownload =
 * false` (a background download is the point), `allowPrerelease`, and the
 * Windows and Linux installers.
 */
export interface UpdaterBackend {
  autoDownload: boolean;
  autoInstallOnAppQuit: boolean;
  allowDowngrade: boolean;
  logger: UpdaterLogger | null;
  on<K extends keyof UpdaterEvents>(event: K, listener: UpdaterEvents[K]): unknown;
  checkForUpdates(): Promise<unknown>;
  quitAndInstall(): void;
}

// --- the service ------------------------------------------------------------

export interface UpdateServiceDeps {
  backend: UpdaterBackend;
  feed: Feed;
  /** Called on every status change, so the title bar and the tray both follow. */
  onStatus: (status: UpdateStatus) => void;
  /** Injected so a test does not depend on the wall clock. */
  now?: () => number;
  debug?: boolean;
  /**
   * One line per failed check, always — unlike the console logger, not gated
   * behind `debug`. The title bar has one word for a failure ("update check
   * failed"); this is the file a person can actually open to see what that
   * word was standing in for. Injected rather than written here directly, for
   * the same reason nothing else in this file talks to Electron: main/index.ts
   * is the one place that knows `app.getPath("logs")`.
   */
  logLine?: (line: string) => void;
}

/** One sentence, not a stack. The title bar has one line to say this in. */
function reason(err: unknown): string {
  const raw = err instanceof Error ? err.message : String(err);
  const first = raw.split("\n", 1)[0]?.trim() ?? "";
  if (!first) return "The update check failed.";
  return first.length > 200 ? `${first.slice(0, 197)}…` : first;
}

/**
 * A GitHub feed answering "not found" is the expected state while this
 * repository is private, not a fault a user can do anything about.
 *
 * electron-updater's GitHub provider reads the public releases feed with no
 * credentials, which is what makes it work for everyone once the repository is
 * public — and what makes it 404 for everyone until then. Drawing a warning
 * triangle over that would tell every early user their app is broken, so it is
 * logged at debug level and the status stays quiet.
 */
export function feedNotFound(feed: Feed, err: unknown): boolean {
  if (feed.kind !== "bundled" || feed.provider !== "github") return false;
  const message = err instanceof Error ? err.message : String(err);
  return /\b404\b/.test(message) || /not found/i.test(message) || /ensure a production release exists/i.test(message);
}

/**
 * Squirrel.Mac's own message when it cannot read a code signature off the
 * RUNNING app, before the feed is ever reached — the state an ad-hoc signed
 * build is in, which is release.yml's fallback whenever CSC_LINK is not set
 * (see its "Ad-hoc signed … so not notarized" summary line). No signature to
 * compare against means this build can never verify an update, on any feed,
 * so the fix is re-signing the build, not something wrong with this launch.
 *
 * Deliberately narrow to "for running application": Squirrel raises a
 * different, genuine error when a DOWNLOADED update's signature fails to
 * validate ("Code signature at URL … did not pass validation"), and that one
 * must keep surfacing as an error — it means a real update was rejected.
 */
export function signatureUnsupported(err: unknown): boolean {
  const message = err instanceof Error ? err.message : String(err);
  return /could not get code signature for running application/i.test(message);
}

export class UpdateService {
  readonly #backend: UpdaterBackend;
  readonly #feed: Feed;
  readonly #onStatus: (status: UpdateStatus) => void;
  readonly #now: () => number;
  readonly #debug: boolean;
  readonly #logLine: (line: string) => void;

  #status: UpdateStatus;
  #started = false;
  #firstCheck: ReturnType<typeof setTimeout> | null = null;
  #interval: ReturnType<typeof setInterval> | null = null;

  constructor(deps: UpdateServiceDeps) {
    this.#backend = deps.backend;
    this.#feed = deps.feed;
    this.#onStatus = deps.onStatus;
    this.#now = deps.now ?? (() => Date.now());
    this.#debug = deps.debug ?? false;
    this.#logLine = deps.logLine ?? (() => {});
    this.#status =
      this.#feed.kind === "none"
        ? { phase: "unsupported", detail: this.#feed.reason }
        : { phase: "idle", ...(feedUrl(this.#feed) ? { feed: feedUrl(this.#feed) } : {}) };
  }

  get status(): UpdateStatus {
    return this.#status;
  }

  /** True once a downloaded update has been staged and can be applied. */
  get ready(): boolean {
    return this.#status.phase === "ready";
  }

  /**
   * Wire the backend and start the schedule.
   *
   * Nothing is wired when there is no feed: an app that cannot update should
   * not be holding listeners and a six-hour timer for the possibility.
   */
  start(): void {
    if (this.#started) return;
    this.#started = true;
    if (this.#feed.kind === "none") return;

    const backend = this.#backend;
    backend.autoDownload = true;
    // On macOS: hand the download to Squirrel as soon as it lands, so the
    // signature check happens while the user is still working, and leave ShipIt
    // waiting to swap the bundle when this process exits — without relaunching.
    // That is what makes "applies on quit" true here. See the header.
    backend.autoInstallOnAppQuit = true;
    // A feed that has gone backwards is a mistake at the publishing end, and
    // silently installing an older build over a newer one is the worst
    // available response to it.
    backend.allowDowngrade = false;
    backend.logger = this.#logger();

    backend.on("checking-for-update", () => {
      this.#set({ phase: "checking" });
    });
    backend.on("update-available", (info) => {
      this.#set({
        phase: "available",
        version: info.version,
        percent: 0,
        detail: info.version ? `Downloading ${info.version}…` : "Downloading an update…",
      });
    });
    backend.on("update-not-available", () => {
      this.#set({ phase: "current", checkedAt: this.#now(), version: undefined, percent: undefined, detail: undefined });
    });
    backend.on("download-progress", (progress) => {
      // Ignored once staged: a late progress line must not un-ready a button
      // the user is about to press.
      if (this.#status.phase !== "available") return;
      this.#set({ percent: Math.max(0, Math.min(100, Math.round(progress.percent ?? 0))) });
    });
    backend.on("update-downloaded", (info) => {
      this.#set({
        phase: "ready",
        version: info.version,
        percent: 100,
        checkedAt: this.#now(),
        detail: info.version ? `TaskTrooper ${info.version} is ready to install.` : "An update is ready to install.",
      });
    });
    backend.on("error", (err) => {
      this.#failed(err);
    });

    this.#firstCheck = setTimeout(() => void this.check(), FIRST_CHECK_DELAY_MS);
    this.#firstCheck.unref?.();
    this.#interval = setInterval(() => void this.check(), CHECK_INTERVAL_MS);
    this.#interval.unref?.();
  }

  stop(): void {
    if (this.#firstCheck) clearTimeout(this.#firstCheck);
    if (this.#interval) clearInterval(this.#interval);
    this.#firstCheck = null;
    this.#interval = null;
  }

  /**
   * Ask the feed. Resolves with the status rather than throwing, because both
   * callers — the timer and the "check now" menu item — want to render the
   * failure, not handle it.
   */
  async check(): Promise<UpdateStatus> {
    if (this.#feed.kind === "none") return this.#status;
    // Already staged. Another check would find the same version and, on a feed
    // that had since moved on, would start a second download over a finished
    // one.
    if (this.ready) return this.#status;
    try {
      await this.#backend.checkForUpdates();
    } catch (err) {
      this.#failed(err);
    }
    return this.#status;
  }

  /**
   * Apply the staged update **and relaunch**.
   *
   * The difference between this and simply quitting is only the relaunch:
   * ShipIt installs on exit either way. This is what "Restart to update"
   * presses, and it is the only call in this app that brings the app back.
   *
   * Call it only after the supervisor's drain has resolved. `quit.ts` owns that
   * ordering and asserts it.
   */
  install(): boolean {
    if (!this.ready) return false;
    this.#backend.quitAndInstall();
    return true;
  }

  /** Every failure lands here, so the one that must stay silent stays silent. */
  #failed(err: unknown): void {
    const detail = reason(err);
    if (feedNotFound(this.#feed, err)) {
      this.#logDebug(`feed has no releases yet: ${detail}`);
      this.#logLine(`feed has no releases yet: ${detail}`);
      this.#set({ phase: "current", checkedAt: this.#now(), version: undefined, percent: undefined, detail: undefined });
      return;
    }
    if (signatureUnsupported(err)) {
      this.#logLine(`this build cannot verify itself for updates (unsigned or ad-hoc signed): ${detail}`);
      this.#set({
        phase: "unsupported",
        checkedAt: this.#now(),
        version: undefined,
        percent: undefined,
        detail: "This build is not signed for automatic updates.",
      });
      return;
    }
    this.#logLine(`update check failed: ${detail}`);
    this.#set({ phase: "error", checkedAt: this.#now(), detail });
  }

  #set(patch: Partial<UpdateStatus>): void {
    this.#status = { ...this.#status, ...patch };
    this.#onStatus(this.#status);
  }

  /**
   * electron-updater logs the feed URL, the versions and the cache paths.
   * None of that is a secret, and none of it is useful noise on a user's Mac
   * either, so info and debug are dropped unless they were asked for.
   */
  #logger(): UpdaterLogger {
    return {
      info: (message) => this.#logDebug(message),
      debug: (message) => this.#logDebug(message),
      warn: (message) => console.warn(`[updater] ${message}`),
      error: (message) => console.error(`[updater] ${message}`),
    };
  }

  #logDebug(message: string): void {
    if (this.#debug) console.warn(`[updater] ${message}`);
  }
}

function feedUrl(feed: Feed): string | undefined {
  return feed.kind === "none" ? undefined : feed.url;
}
