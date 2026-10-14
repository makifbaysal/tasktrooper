import { describe, expect, it } from "vitest";
import { LOG_LINE_CHARS, LogRing, cleanLine } from "./log-ring.js";
import { ThrottledPublisher, type PublisherTimers } from "./publisher.js";
import { expandWindowsVars, parseRegPath, refreshedPath } from "./winpath.js";
import { OutputLines } from "./process.js";

/** The small pure pieces: the log tail, the push throttle, the Windows PATH refresh, line splitting. */

describe("log lines", () => {
  it("strips colours, cursor movement and OSC titles, and bounds the length", () => {
    expect(cleanLine("\u001b[1;32m==>\u001b[0m Pouring node")).toBe("==> Pouring node");
    expect(cleanLine("\u001b]0;title\u0007done\u001b[2K")).toBe("done");
    expect(cleanLine("a\bb\u0007c   ")).toBe("abc");
    const long = cleanLine("y".repeat(1000));
    expect(long).toHaveLength(LOG_LINE_CHARS);
    expect(long.endsWith("…")).toBe(true);
  });

  it("keeps the newest 40 and drops blank ones", () => {
    const ring = new LogRing();
    for (let i = 0; i < 50; i += 1) ring.push(i % 10 === 0 ? "   " : `line ${i}`);
    expect(ring.size).toBe(40);
    expect(ring.lines.at(-1)).toBe("line 49");
  });
});

describe("OutputLines", () => {
  it("splits on \\n, \\r\\n and a progress bar's bare \\r, across chunks", () => {
    const lines = new OutputLines();
    expect(lines.push("one\r\ntw")).toEqual(["one"]);
    expect(lines.push("o\n[==  ] 10%\r[====] 20%\r")).toEqual(["two", "[==  ] 10%", "[====] 20%"]);
    expect(lines.push("tail")).toEqual([]);
    expect(lines.flush()).toEqual(["tail"]);
  });
});

function fakeTimers() {
  let now = 0;
  const pending: { at: number; fn: () => void; id: number }[] = [];
  let next = 1;
  const timers: PublisherTimers = {
    now: () => now,
    setTimeout: (fn, ms) => {
      const id = next++;
      pending.push({ at: now + ms, fn, id });
      return id;
    },
    clearTimeout: (id) => {
      const i = pending.findIndex((p) => p.id === id);
      if (i >= 0) pending.splice(i, 1);
    },
  };
  const advance = (ms: number): void => {
    now += ms;
    for (const p of pending.filter((p) => p.at <= now)) {
      pending.splice(pending.indexOf(p), 1);
      p.fn();
    }
  };
  return { timers, advance };
}

describe("ThrottledPublisher", () => {
  it("pushes at most once per interval, always ending on the latest value", () => {
    const { timers, advance } = fakeTimers();
    let value = 0;
    const seen: number[] = [];
    const publisher = new ThrottledPublisher(() => value, [(v) => seen.push(v)], 250, timers);
    for (value = 1; value <= 10; value += 1) publisher.notify();
    value = 10;
    expect(seen).toEqual([1]);
    advance(249);
    expect(seen).toEqual([1]);
    advance(1);
    expect(seen).toEqual([1, 10]);
    for (let t = 0; t < 1000; t += 50) {
      value += 1;
      publisher.notify();
      advance(50);
    }
    expect(seen.length).toBeLessThanOrEqual(2 + 1000 / 250 + 1);
  });

  it("pushes a phase change at once and drops the pending trailing push it replaces", () => {
    const { timers, advance } = fakeTimers();
    let value = "a";
    const seen: string[] = [];
    const publisher = new ThrottledPublisher(() => value, [(v) => seen.push(v)], 250, timers);
    publisher.notify();
    value = "b";
    publisher.notify();
    value = "done";
    publisher.notify(true);
    advance(1000);
    expect(seen).toEqual(["a", "done"]);
  });

  it("keeps pushing to the other sinks when one throws", () => {
    const { timers } = fakeTimers();
    const seen: number[] = [];
    const publisher = new ThrottledPublisher(
      () => 7,
      [
        () => {
          throw new Error("frame disposed");
        },
        (v) => seen.push(v),
      ],
      250,
      timers,
    );
    publisher.notify(true);
    expect(seen).toEqual([7]);
  });
});

describe("Windows PATH refresh", () => {
  const REG = [
    "",
    "HKEY_LOCAL_MACHINE\\SYSTEM\\CurrentControlSet\\Control\\Session Manager\\Environment",
    "    Path    REG_EXPAND_SZ    %SystemRoot%\\system32;%SystemRoot%;C:\\Program Files\\Git\\cmd;C:\\Program Files\\Go\\bin",
    "",
  ].join("\r\n");

  it("reads the Path value out of reg query", () => {
    expect(parseRegPath(REG)).toBe("%SystemRoot%\\system32;%SystemRoot%;C:\\Program Files\\Git\\cmd;C:\\Program Files\\Go\\bin");
    expect(parseRegPath("ERROR: The system was unable to find the specified registry key or value.")).toBeNull();
  });

  it("expands variables case-insensitively and leaves unknown ones", () => {
    expect(expandWindowsVars("%systemroot%\\x;%NOPE%\\y", { SystemRoot: "C:\\Windows" })).toBe("C:\\Windows\\x;%NOPE%\\y");
  });

  it("keeps this process's order and appends only what the registry added", () => {
    const current = "C:\\Windows\\system32;C:\\Users\\me\\AppData\\Local\\Microsoft\\WindowsApps";
    const merged = refreshedPath(current, parseRegPath(REG), "%USERPROFILE%\\.local\\bin", {
      SystemRoot: "C:\\Windows",
      USERPROFILE: "C:\\Users\\me",
    });
    expect(merged.split(";")).toEqual([
      "C:\\Windows\\system32",
      "C:\\Users\\me\\AppData\\Local\\Microsoft\\WindowsApps",
      "C:\\Windows",
      "C:\\Program Files\\Git\\cmd",
      "C:\\Program Files\\Go\\bin",
      "C:\\Users\\me\\.local\\bin",
    ]);
  });
});
