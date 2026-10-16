import { randomUUID } from "node:crypto";
import { mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import path from "node:path";
import type { AccountMode, AnalyticsState } from "../../ipc/types.js";
import { ANALYTICS_DEFAULT_ON, telemetryForcedOff, type AnalyticsConfig } from "./config.js";

export const COLLECT_URL = "https://www.google-analytics.com/mp/collect";
export const HEARTBEAT_MS = 30 * 60 * 1000;
export const FLUSH_DELAY_MS = 2000;
const MAX_BATCH = 25;
// A page that loops on track() must not grow main's memory without bound; past
// this the newest events are dropped until a flush drains the queue.
const MAX_QUEUE = 200;
const ID_FILE = "analytics-id";
const USER_FILE = "analytics-user";
const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
const HASHED_USER = /^[0-9a-f]{64}$/;

/** What every event says about where it came from, so GA can split desktop from web and iOS. */
export const APP_PLATFORM = "desktop";

/** GA's own names for these operating systems, so desktop rows line up with browser traffic. */
export const GA_OS_NAMES: Partial<Record<NodeJS.Platform, string>> = {
  darwin: "Macintosh",
  win32: "Windows",
  linux: "Linux",
};

export type AnalyticsMode = AccountMode | "temporary_local";

export interface AnalyticsEvent {
  name: string;
  params: Record<string, string | number>;
}

/**
 * Measurement Protocol's `device` block: what fills GA's own operating system,
 * device category and language dimensions, which a request with no browser
 * behind it otherwise leaves "(not set)".
 */
export interface AnalyticsDevice {
  category: "desktop";
  operating_system: string;
  operating_system_version: string;
  language: string;
  screen_resolution?: string;
}

export interface AnalyticsDeps {
  config: AnalyticsConfig | null;
  dir: string;
  env: NodeJS.ProcessEnv;
  appVersion: string;
  platform: string;
  arch: string;
  mode: () => AnalyticsMode;
  device: () => AnalyticsDevice;
  preference: () => boolean | undefined;
  storePreference: (on: boolean) => void;
  post: (url: string, body: string) => Promise<unknown>;
  log?: (line: string) => void;
  now?: () => number;
}

export class Analytics {
  readonly #deps: AnalyticsDeps;
  readonly #now: () => number;
  #queue: AnalyticsEvent[] = [];
  #flushTimer: ReturnType<typeof setTimeout> | null = null;
  #beat: ReturnType<typeof setInterval> | null = null;
  #lastHeartbeat = Number.NEGATIVE_INFINITY;
  #focusedSince: number | null = null;
  #engagedMs = 0;
  #clientId: string | null = null;
  #userId: string | null = null;
  #started = false;
  #warned = false;
  readonly #sessionId: string;

  constructor(deps: AnalyticsDeps) {
    this.#deps = deps;
    this.#now = deps.now ?? Date.now;
    this.#sessionId = String(Math.floor(this.#now() / 1000));
  }

  state(): AnalyticsState {
    const available = this.#deps.config !== null;
    const forcedOff = available && telemetryForcedOff(this.#deps.env);
    return { available, forcedOff, enabled: available && !forcedOff && this.#preferred() };
  }

  get enabled(): boolean {
    return this.state().enabled;
  }

  start(focused: boolean): void {
    if (this.#started) return;
    this.#started = true;
    if (!this.enabled) return;
    this.#begin(focused);
  }

  set(on: boolean): AnalyticsState {
    this.#deps.storePreference(on);
    if (!this.#started) return this.state();
    if (!on || !this.enabled) this.#forget();
    else if (this.#clientId === null) this.#begin(this.#focusedSince !== null);
    return this.state();
  }

  focus(): void {
    if (this.#focusedSince !== null) return;
    this.#focusedSince = this.#now();
    if (this.#started && this.enabled) this.#heartbeat();
  }

  blur(): void {
    this.#bank();
    this.#focusedSince = null;
  }

  /**
   * One product event or screen view from the page. Dropped while the count is
   * off; the name and params were checked by `validateAnalyticsTrack` before
   * they got here, and the base params are laid over them so a page cannot
   * claim another version, platform or session.
   */
  track(name: string, params: Record<string, string | number>): void {
    if (!this.#started || !this.enabled || this.#clientId === null) return;
    if (this.#queue.length >= MAX_QUEUE) return;
    this.#enqueue(name, { engagement_time_msec: 1, ...params });
  }

  /**
   * The signed-in member's hashed id (or null on sign-out), so the same person
   * on the desktop, the web and iOS is one user in GA. Kept on disk next to the
   * install id so the next launch's app_open carries it too, and sent only
   * while the app is signed in to an account.
   */
  identify(userId: string | null): void {
    const next = userId !== null && HASHED_USER.test(userId) ? userId : null;
    if (next === this.#userId) return;
    this.#userId = next;
    if (!this.enabled) return;
    const file = path.join(this.#deps.dir, USER_FILE);
    try {
      if (next === null) rmSync(file, { force: true });
      else {
        mkdirSync(this.#deps.dir, { recursive: true });
        writeFileSync(file, `${next}\n`, { mode: 0o600 });
      }
    } catch (err) {
      this.#warn(err);
    }
  }

  stop(): Promise<void> {
    if (this.#beat) clearInterval(this.#beat);
    this.#beat = null;
    this.#started = false;
    return this.flush();
  }

  flush(): Promise<void> {
    if (this.#flushTimer) {
      clearTimeout(this.#flushTimer);
      this.#flushTimer = null;
    }
    const config = this.#deps.config;
    const clientId = this.#clientId;
    if (!config || !clientId || this.#queue.length === 0) return Promise.resolve();
    const events = this.#queue.splice(0, MAX_BATCH);
    const url = `${COLLECT_URL}?measurement_id=${encodeURIComponent(config.measurementId)}&api_secret=${encodeURIComponent(config.apiSecret)}`;
    const userId = this.#deps.mode() === "local" ? null : this.#userId;
    const body = JSON.stringify({
      client_id: clientId,
      ...(userId ? { user_id: userId } : {}),
      non_personalized_ads: true,
      user_properties: { app_platform: { value: APP_PLATFORM } },
      ...this.#device(),
      events: events.map((e) => ({ name: e.name, params: e.params })),
    });
    return this.#deps.post(url, body).then(
      () => {
        if (this.#queue.length > 0) this.#schedule();
      },
      (err: unknown) => this.#warn(err),
    );
  }

  #preferred(): boolean {
    return this.#deps.preference() ?? ANALYTICS_DEFAULT_ON;
  }

  #begin(focused: boolean): void {
    this.#clientId = this.#ensureId();
    if (this.#clientId === null) return;
    this.#userId ??= this.#storedUser();
    if (focused && this.#focusedSince === null) this.#focusedSince = this.#now();
    this.#enqueue("app_open", {});
    this.#beat ??= setInterval(() => {
      if (this.#focusedSince !== null) this.#heartbeat();
    }, HEARTBEAT_MS);
    this.#beat.unref?.();
    if (this.#focusedSince !== null) this.#heartbeat();
  }

  #forget(): void {
    if (this.#beat) clearInterval(this.#beat);
    this.#beat = null;
    if (this.#flushTimer) clearTimeout(this.#flushTimer);
    this.#flushTimer = null;
    this.#queue = [];
    this.#clientId = null;
    this.#userId = null;
    this.#lastHeartbeat = Number.NEGATIVE_INFINITY;
    this.#engagedMs = 0;
    try {
      rmSync(path.join(this.#deps.dir, ID_FILE), { force: true });
      rmSync(path.join(this.#deps.dir, USER_FILE), { force: true });
    } catch {
      return;
    }
  }

  #bank(): void {
    if (this.#focusedSince === null) return;
    const now = this.#now();
    this.#engagedMs += Math.max(0, now - this.#focusedSince);
    this.#focusedSince = now;
  }

  #heartbeat(): void {
    const now = this.#now();
    if (now - this.#lastHeartbeat < HEARTBEAT_MS) return;
    this.#bank();
    this.#lastHeartbeat = now;
    const engagement = Math.max(100, this.#engagedMs);
    this.#engagedMs = 0;
    this.#enqueue("app_active", { engagement_time_msec: engagement });
  }

  #enqueue(name: string, extra: Record<string, string | number>): void {
    this.#queue.push({
      name,
      params: {
        ...extra,
        session_id: this.#sessionId,
        app_platform: APP_PLATFORM,
        app_version: this.#deps.appVersion,
        platform: this.#deps.platform,
        arch: this.#deps.arch,
        mode: this.#deps.mode(),
      },
    });
    this.#schedule();
  }

  #schedule(): void {
    if (this.#flushTimer) return;
    this.#flushTimer = setTimeout(() => void this.flush(), FLUSH_DELAY_MS);
    this.#flushTimer.unref?.();
  }

  #storedUser(): string | null {
    try {
      const existing = readFileSync(path.join(this.#deps.dir, USER_FILE), "utf8").trim();
      return HASHED_USER.test(existing) ? existing : null;
    } catch {
      return null;
    }
  }

  #device(): { device?: AnalyticsDevice } {
    try {
      return { device: this.#deps.device() };
    } catch {
      return {};
    }
  }

  #ensureId(): string | null {
    const file = path.join(this.#deps.dir, ID_FILE);
    try {
      const existing = readFileSync(file, "utf8").trim();
      if (UUID.test(existing)) return existing;
    } catch {
      // first launch, or the file was removed: a new id is made below
    }
    try {
      const id = randomUUID();
      mkdirSync(this.#deps.dir, { recursive: true });
      writeFileSync(file, `${id}\n`, { mode: 0o600 });
      return id;
    } catch (err) {
      this.#warn(err);
      return null;
    }
  }

  #warn(err: unknown): void {
    if (this.#warned || !this.#deps.log) return;
    this.#warned = true;
    const config = this.#deps.config;
    let text = err instanceof Error ? `${err.name}: ${err.message}` : String(err);
    if (config) {
      for (const secret of [config.apiSecret, encodeURIComponent(config.apiSecret)]) {
        text = text.split(secret).join("[redacted]");
      }
    }
    text = text.replace(/api_secret=[^&\s"']*/gi, "api_secret=[redacted]");
    this.#deps.log(`[analytics] not sent: ${text}`);
  }
}
