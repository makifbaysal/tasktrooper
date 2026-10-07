import { describe, expect, it } from "vitest";
import type { LogLine } from "../../ipc/types.js";
import { LogRing, LogStore, MAX_LINE_LENGTH } from "./log-buffer.js";
import { renderServerLine } from "./server-log.js";

function line(seq: number, text = `line ${seq}`): LogLine {
  return { seq, child: "agent-server", at: 0, stream: "stdout", text };
}

describe("LogRing", () => {
  it("keeps the newest lines in order once it wraps, and counts what it dropped", () => {
    const ring = new LogRing(3);
    for (let seq = 1; seq <= 7; seq++) ring.push(line(seq));
    expect(ring.read().map((l) => l.seq)).toEqual([5, 6, 7]);
    expect(ring.size).toBe(3);
    expect(ring.dropped).toBe(4);
    expect(ring.last()?.seq).toBe(7);
  });

  it("reads past a sequence number and honours the limit, across the wrap point", () => {
    const ring = new LogRing(5);
    for (let seq = 1; seq <= 8; seq++) ring.push(line(seq * 10));
    expect(ring.read(50).map((l) => l.seq)).toEqual([60, 70, 80]);
    expect(ring.read(55).map((l) => l.seq)).toEqual([60, 70, 80]);
    expect(ring.read(0, 2).map((l) => l.seq)).toEqual([70, 80]);
    expect(ring.read(80)).toEqual([]);
    expect(ring.read(10).map((l) => l.seq)).toEqual([40, 50, 60, 70, 80]);
  });

  it("truncates a pathological line and starts empty again after clear", () => {
    const ring = new LogRing(2, 5);
    expect(ring.push(line(1, "abcdefghij")).text).toMatch(/^abcde… \[10 chars, truncated\]$/);
    ring.clear();
    expect(ring.read()).toEqual([]);
    expect(ring.last()).toBeUndefined();
    ring.push(line(2));
    expect(ring.read().map((l) => l.seq)).toEqual([2]);
  });

  it("matches the old splice-based ring on a long mixed workload", () => {
    const ring = new LogRing(100);
    const reference: LogLine[] = [];
    for (let seq = 1; seq <= 1234; seq++) {
      ring.push(line(seq));
      reference.push(line(seq));
      if (reference.length > 100) reference.splice(0, reference.length - 100);
    }
    expect(ring.read().map((l) => l.seq)).toEqual(reference.map((l) => l.seq));
    expect(ring.read(1200, 10).map((l) => l.seq)).toEqual(reference.filter((l) => l.seq > 1200).slice(-10).map((l) => l.seq));
  });
});

describe("LogStore", () => {
  it("stores the raw line and renders it only when it is read", () => {
    let renders = 0;
    const store = new LogStore(10, {
      "agent-server": (text) => {
        renders += 1;
        return text.toUpperCase();
      },
    });
    store.append("agent-server", "stderr", "raw json", "info");
    store.append("supervisor", "stdout", "narration");
    expect(renders).toBe(0);

    expect(store.read("agent-server").map((l) => l.text)).toEqual(["RAW JSON"]);
    expect(store.read().map((l) => l.text)).toEqual(["RAW JSON", "narration"]);
    expect(store.last("agent-server")?.text).toBe("RAW JSON");
    expect(store.read("agent-server")[0]?.level).toBe("info");
  });

  it("renders a line too long to store before cutting it, so a long JSON line is not shown as broken JSON", () => {
    let renders = 0;
    const store = new LogStore(10, {
      "agent-server": (text) => {
        renders += 1;
        return renderServerLine(text);
      },
    });
    const huge = JSON.stringify({ level: "info", message: "indexed", files: "x".repeat(MAX_LINE_LENGTH * 2) });
    store.append("agent-server", "stderr", huge, "info");
    expect(renders).toBe(1);

    const [shown] = store.read("agent-server");
    expect(renders).toBe(1);
    expect(shown?.text.startsWith("indexed files=xxx")).toBe(true);
    expect(shown?.text).toMatch(/… \[\d+ chars, truncated\]$/);
    expect(shown?.text.length).toBeLessThan(MAX_LINE_LENGTH + 40);
    expect(store.last("agent-server")?.text).toBe(shown?.text);
  });

  it("merges rings in sequence order and keeps only the newest when limited", () => {
    const store = new LogStore(10);
    store.append("agent-server", "stdout", "a");
    store.append("supervisor", "stdout", "b");
    store.append("agent-server", "stdout", "c");
    store.append("embedder", "stdout", "d");
    expect(store.read().map((l) => l.text)).toEqual(["a", "b", "c", "d"]);
    expect(store.read(undefined, 0, 2).map((l) => l.text)).toEqual(["c", "d"]);
    expect(store.read(undefined, 2).map((l) => l.text)).toEqual(["c", "d"]);
  });
});
