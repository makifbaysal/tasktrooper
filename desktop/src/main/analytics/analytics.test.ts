import { existsSync, mkdtempSync, readFileSync, rmSync, statSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { Analytics, FLUSH_DELAY_MS, HEARTBEAT_MS, type AnalyticsDeps } from "./analytics.js";
import { telemetryForcedOff } from "./config.js";

const CONFIG = { measurementId: "G-TEST123", apiSecret: "s3cr3t-VALUE_xyz" };

let dir: string;
let clock: number;
let pref: boolean | undefined;
let sent: { url: string; body: { client_id: string; events: { name: string; params: Record<string, unknown> }[] } }[];
let logs: string[];

function make(patch: Partial<AnalyticsDeps> = {}): Analytics {
  return new Analytics({
    config: CONFIG,
    dir,
    env: {},
    appVersion: "1.2.3",
    platform: "darwin",
    arch: "arm64",
    mode: () => "local",
    preference: () => pref,
    storePreference: (on) => {
      pref = on;
    },
    post: (url, body) => {
      sent.push({ url, body: JSON.parse(body) });
      return Promise.resolve();
    },
    log: (line) => logs.push(line),
    now: () => clock,
    ...patch,
  });
}

const idFile = () => path.join(dir, "analytics-id");

beforeEach(() => {
  vi.useFakeTimers();
  dir = mkdtempSync(path.join(tmpdir(), "tt-analytics-"));
  clock = 1_800_000_000_000;
  pref = undefined;
  sent = [];
  logs = [];
});

afterEach(() => {
  vi.useRealTimers();
  rmSync(dir, { recursive: true, force: true });
});

describe("Analytics", () => {
  it("is disabled, silent and leaves no id file when the build has no configuration", async () => {
    const a = make({ config: null });
    a.start(true);
    a.focus();
    await vi.advanceTimersByTimeAsync(HEARTBEAT_MS * 2);
    expect(a.state()).toEqual({ available: false, enabled: false, forcedOff: false });
    expect(sent).toEqual([]);
    expect(existsSync(idFile())).toBe(false);
  });

  it("is disabled when the stored choice is off", async () => {
    pref = false;
    const a = make();
    a.start(true);
    await vi.advanceTimersByTimeAsync(HEARTBEAT_MS);
    expect(a.state().enabled).toBe(false);
    expect(sent).toEqual([]);
    expect(existsSync(idFile())).toBe(false);
  });

  it.each([{ TASKTROOPER_TELEMETRY: "0" }, { DO_NOT_TRACK: "1" }])("is forced off by %j", async (env) => {
    const a = make({ env });
    a.start(true);
    await vi.advanceTimersByTimeAsync(HEARTBEAT_MS);
    expect(a.state()).toEqual({ available: true, enabled: false, forcedOff: true });
    expect(sent).toEqual([]);
    expect(existsSync(idFile())).toBe(false);
  });

  it("reads the environment overrides", () => {
    expect(telemetryForcedOff({})).toBe(false);
    expect(telemetryForcedOff({ TASKTROOPER_TELEMETRY: "1", DO_NOT_TRACK: "0" })).toBe(false);
    expect(telemetryForcedOff({ TASKTROOPER_TELEMETRY: "0" })).toBe(true);
    expect(telemetryForcedOff({ DO_NOT_TRACK: "1" })).toBe(true);
  });

  it("sends app_open and a first heartbeat with the expected params and nothing identifying", async () => {
    const a = make();
    a.start(true);
    await vi.advanceTimersByTimeAsync(FLUSH_DELAY_MS);

    expect(sent).toHaveLength(1);
    const [{ url, body }] = sent;
    expect(url).toMatch(/^https:\/\/www\.google-analytics\.com\/mp\/collect\?measurement_id=G-TEST123&api_secret=/);
    expect(Object.keys(body).sort()).toEqual(["client_id", "events", "non_personalized_ads"]);
    expect(body.client_id).toMatch(/^[0-9a-f-]{36}$/);
    expect(body.events.map((e) => e.name)).toEqual(["app_open", "app_active"]);
    for (const event of body.events) {
      expect(event.params).toMatchObject({ app_version: "1.2.3", platform: "darwin", arch: "arm64", mode: "local" });
      expect(event.params.session_id).toEqual(expect.any(String));
      expect(Object.keys(event.params).sort()).not.toEqual(expect.arrayContaining(["email", "path", "origin"]));
    }
    expect(body.events[0]?.params.engagement_time_msec).toBeUndefined();
    expect(body.events[1]?.params.engagement_time_msec).toBeGreaterThan(0);
    const text = JSON.stringify(body);
    expect(text).not.toContain(dir);
    expect(text).not.toContain(CONFIG.apiSecret);
  });

  it("reports the mode it is in", async () => {
    const a = make({ mode: () => "temporary_local" });
    a.start(false);
    await vi.advanceTimersByTimeAsync(FLUSH_DELAY_MS);
    expect(sent[0]?.body.events[0]?.params.mode).toBe("temporary_local");
  });

  it("sends no heartbeat while no window is focused", async () => {
    const a = make();
    a.start(false);
    await vi.advanceTimersByTimeAsync(HEARTBEAT_MS * 3);
    expect(sent.flatMap((s) => s.body.events.map((e) => e.name))).toEqual(["app_open"]);
  });

  it("throttles the heartbeat to one per 30 minutes however often focus changes", async () => {
    const a = make();
    a.start(true);
    await vi.advanceTimersByTimeAsync(FLUSH_DELAY_MS);
    const heartbeats = () => sent.flatMap((s) => s.body.events).filter((e) => e.name === "app_active").length;
    expect(heartbeats()).toBe(1);

    for (let i = 0; i < 5; i += 1) {
      clock += 60_000;
      a.blur();
      a.focus();
    }
    await vi.advanceTimersByTimeAsync(FLUSH_DELAY_MS);
    expect(heartbeats()).toBe(1);

    clock += HEARTBEAT_MS;
    a.blur();
    a.focus();
    await vi.advanceTimersByTimeAsync(FLUSH_DELAY_MS);
    expect(heartbeats()).toBe(2);
    const last = sent.at(-1)?.body.events.at(-1);
    expect(last?.params.engagement_time_msec).toBeGreaterThanOrEqual(HEARTBEAT_MS);
  });

  it("creates the id file only when enabled, mode 0600, and keeps the id across launches", async () => {
    make({ config: null }).start(true);
    expect(existsSync(idFile())).toBe(false);

    const first = make();
    first.start(false);
    expect(statSync(idFile()).mode & 0o777).toBe(0o600);
    const id = readFileSync(idFile(), "utf8").trim();
    await vi.advanceTimersByTimeAsync(FLUSH_DELAY_MS);
    expect(sent[0]?.body.client_id).toBe(id);

    const second = make();
    second.start(false);
    await vi.advanceTimersByTimeAsync(FLUSH_DELAY_MS);
    expect(sent[1]?.body.client_id).toBe(id);
  });

  it("stops at once on opt-out: queued events dropped, id file deleted, nothing more sent", async () => {
    const a = make();
    a.start(true);
    expect(existsSync(idFile())).toBe(true);

    expect(a.set(false)).toEqual({ available: true, enabled: false, forcedOff: false });
    expect(pref).toBe(false);
    expect(existsSync(idFile())).toBe(false);

    await vi.advanceTimersByTimeAsync(HEARTBEAT_MS * 2);
    a.focus();
    expect(sent).toEqual([]);
    expect(existsSync(idFile())).toBe(false);
  });

  it("resumes with a fresh id after opting back in", async () => {
    const a = make();
    a.start(true);
    const before = readFileSync(idFile(), "utf8");
    a.set(false);
    a.set(true);
    expect(existsSync(idFile())).toBe(true);
    expect(readFileSync(idFile(), "utf8")).not.toBe(before);
    await vi.advanceTimersByTimeAsync(FLUSH_DELAY_MS);
    expect(sent).toHaveLength(1);
  });

  it("never lets the secret reach a logged error", async () => {
    const encoded = encodeURIComponent(CONFIG.apiSecret);
    const a = make({
      post: (url) => Promise.reject(new Error(`fetch failed for ${url} (api_secret=${CONFIG.apiSecret}, ${encoded})`)),
    });
    a.start(true);
    await vi.advanceTimersByTimeAsync(FLUSH_DELAY_MS);

    expect(logs).toHaveLength(1);
    expect(logs[0]).not.toContain(CONFIG.apiSecret);
    expect(logs[0]).not.toContain(encoded);
    expect(logs[0]).toContain("[redacted]");
  });

  it("fails silently on a network error and keeps running", async () => {
    let calls = 0;
    const a = make({
      post: () => {
        calls += 1;
        return Promise.reject(new Error("offline"));
      },
    });
    a.start(true);
    await vi.advanceTimersByTimeAsync(FLUSH_DELAY_MS);
    clock += HEARTBEAT_MS;
    await vi.advanceTimersByTimeAsync(HEARTBEAT_MS);
    expect(calls).toBeGreaterThan(1);
    expect(logs).toHaveLength(1);
  });
});
