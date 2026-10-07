import { describe, expect, it } from "vitest";
import { parseServerLine, parseServerListening, renderServerLine, serverLineLevel } from "./server-log.js";

/**
 * The one line the whole boot sequence hangs on. The backend binds an
 * OS-assigned port, so this string is the only way this process learns where
 * the UI's API calls should go — and a wrong answer here is a window that opens
 * on a base URL nothing is listening on.
 */
describe("parseServerListening", () => {
  it("reads the port the backend bound", () => {
    expect(parseServerListening("LISTENING http://127.0.0.1:52341")).toBe("http://127.0.0.1:52341");
    expect(parseServerListening("  LISTENING http://127.0.0.1:8085/  \n")).toBe("http://127.0.0.1:8085");
    expect(parseServerListening("LISTENING http://localhost:8085")).toBe("http://127.0.0.1:8085");
  });

  it("ignores every other line the backend writes", () => {
    for (const line of [
      "",
      "LISTENING",
      "LISTENING ",
      "listening http://127.0.0.1:8085",
      "now LISTENING http://127.0.0.1:8085",
      '{"level":"info","message":"http server started"}',
      "LISTENING not-a-url",
    ]) {
      expect(parseServerListening(line), JSON.stringify(line)).toBeUndefined();
    }
  });

  /**
   * Loopback only. A base URL off this machine would be where the UI sends the
   * bearer token this process generated.
   */
  it("refuses an address that is not on this machine, and a scheme that is not http", () => {
    expect(parseServerListening("LISTENING http://10.0.0.4:8085")).toBeUndefined();
    expect(parseServerListening("LISTENING http://evil.example:8085")).toBeUndefined();
    expect(parseServerListening("LISTENING file:///etc/passwd")).toBeUndefined();
    // No port means no address the supervisor could poll.
    expect(parseServerListening("LISTENING http://127.0.0.1")).toBeUndefined();
  });
});

describe("parseServerLine", () => {
  it("flattens a zerolog line into something a log view can show", () => {
    const parsed = parseServerLine('{"level":"warn","time":"t","message":"migrating","step":4}');
    expect(parsed.level).toBe("warn");
    expect(parsed.text).toBe("migrating step=4");
    expect(parsed.listening).toBeUndefined();
  });

  it("passes a panic trace through untouched, because that line is the diagnosis", () => {
    const parsed = parseServerLine("panic: runtime error: invalid memory address");
    expect(parsed.text).toBe("panic: runtime error: invalid memory address");
    expect(parsed.level).toBeUndefined();
  });

  it("carries the bound address on the one line that has one", () => {
    expect(parseServerLine("LISTENING http://127.0.0.1:52341").listening).toBe("http://127.0.0.1:52341");
  });
});

describe("serverLineLevel and renderServerLine", () => {
  it("lifts the level without a parse, and renders only when asked", () => {
    const raw = '{"level":"warn","time":"t","message":"migrating","step":4}';
    expect(serverLineLevel(raw)).toBe("warn");
    expect(renderServerLine(raw)).toBe("migrating step=4");
  });

  it("finds no level on a line that is not zerolog, and leaves its text alone", () => {
    expect(serverLineLevel("panic: runtime error")).toBeUndefined();
    expect(renderServerLine("panic: runtime error")).toBe("panic: runtime error");
    expect(serverLineLevel("LISTENING http://127.0.0.1:1")).toBeUndefined();
  });

  it("agrees with the full parse on every zerolog level", () => {
    for (const level of ["trace", "debug", "info", "warn", "error", "fatal", "panic"]) {
      const raw = `{"level":"${level}","message":"m"}`;
      expect(serverLineLevel(raw)).toBe(parseServerLine(raw).level);
    }
  });
});
