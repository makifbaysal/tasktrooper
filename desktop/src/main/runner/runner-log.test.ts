import { describe, expect, it } from "vitest";
import { isAuthRejection, parseRunnerLine } from "./runner-log.js";

/**
 * These messages are pinned to what `desktop/runner/main.go` actually emits
 * (see that file's `log.Info().Msg("tunnel attached")` and friends). A wrong
 * match shows the user a tunnel that is up when it is not, which is worse
 * than `unknown` — so this suite drives real JSON lines shaped exactly like
 * zerolog's output, not a loosened stand-in.
 */
describe("parseRunnerLine", () => {
  it("moves the tunnel to attached", () => {
    const parsed = parseRunnerLine('{"level":"info","time":"2026-09-28T00:00:00Z","message":"tunnel attached"}');
    expect(parsed.tunnel).toMatchObject({ state: "attached" });
    expect(parsed.level).toBe("info");
  });

  it("moves the tunnel to detached and carries the stream count", () => {
    const parsed = parseRunnerLine(
      '{"level":"info","time":"2026-09-28T00:00:00Z","streams":7,"message":"tunnel detached"}',
    );
    expect(parsed.tunnel).toMatchObject({ state: "detached", streams: 7 });
  });

  it("moves the tunnel to reconnecting and computes retryAt from retry_in milliseconds", () => {
    const before = Date.now();
    const parsed = parseRunnerLine(
      '{"level":"warn","time":"2026-09-28T00:00:00Z","error":"dial: connection refused","retry_in":8000,"uptime":0,"message":"tunnel session ended, reconnecting"}',
    );
    expect(parsed.tunnel?.state).toBe("reconnecting");
    expect(parsed.tunnel?.detail).toBe("dial: connection refused");
    expect(parsed.tunnel?.retryAt).toBeGreaterThanOrEqual(before + 8000);
  });

  it("moves the tunnel to detached on a clean shutdown", () => {
    const parsed = parseRunnerLine('{"level":"info","time":"2026-09-28T00:00:00Z","message":"shut down cleanly"}');
    expect(parsed.tunnel).toMatchObject({ state: "detached", detail: "shut down" });
  });

  it("does not move the tunnel for an unrelated line", () => {
    const parsed = parseRunnerLine('{"level":"debug","time":"2026-09-28T00:00:00Z","message":"stream accepted"}');
    expect(parsed.tunnel).toBeUndefined();
    expect(parsed.text).toContain("stream accepted");
  });

  it("passes a non-JSON line through untouched — a Go panic trace must never be swallowed", () => {
    const line = "panic: runtime error: index out of range [3] with length 3";
    expect(parseRunnerLine(line)).toEqual({ text: line });
  });

  it("passes malformed JSON through untouched rather than throwing", () => {
    const line = '{"level":"info", not json';
    expect(parseRunnerLine(line)).toEqual({ text: line });
  });
});

describe("isAuthRejection", () => {
  it("recognises the runner's own authError text", () => {
    expect(
      isAuthRejection(
        "control plane rejected the connection (HTTP 401) — this machine's runner token is wrong or revoked; re-pair it, retrying will not help",
      ),
    ).toBe(true);
  });

  it("does not fire on an ordinary reconnect warning", () => {
    expect(isAuthRejection("tunnel session ended, reconnecting")).toBe(false);
  });
});
