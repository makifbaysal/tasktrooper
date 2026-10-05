import { afterEach, describe, expect, it, vi } from "vitest";
import type { AttachmentMeta } from "@/api";
import { loadQueue, mergeQueued, saveQueue, type QueuedMessage } from "@/lib/chatQueue";

const att = (id: string): AttachmentMeta => ({
  id,
  filename: `${id}.png`,
  content_type: "image/png",
  size_bytes: 1,
  sha256: "x",
  created_by_type: "user",
  created_at: "2026-10-01T00:00:00Z",
});

const item = (id: string, over: Partial<QueuedMessage> = {}): QueuedMessage => ({
  id,
  content: id,
  mentions: [],
  attachments: [],
  fileIds: [],
  createdAt: "2026-10-01T00:00:00Z",
  ...over,
});

afterEach(() => {
  vi.restoreAllMocks();
  localStorage.clear();
});

describe("mergeQueued", () => {
  it("joins content with a blank line and dedupes mentions, attachments and file ids", () => {
    const repo = { kind: "repository" as const, id: "r1", name: "web" };
    const merged = mergeQueued([
      item("a", { mentions: [repo], attachments: [att("1")], fileIds: ["f1"] }),
      item("b", { mentions: [repo], attachments: [att("1"), att("2")], fileIds: ["f1", "f2"] }),
    ]);
    expect(merged.content).toBe("a\n\nb");
    expect(merged.mentions).toEqual([repo]);
    expect(merged.attachments.map((a) => a.id)).toEqual(["1", "2"]);
    expect(merged.fileIds).toEqual(["f1", "f2"]);
  });
});

describe("queue storage", () => {
  it("round-trips per session and removes the key when emptied", () => {
    saveQueue("s1", [item("a")]);
    expect(loadQueue("s1")).toEqual([item("a")]);
    expect(loadQueue("s2")).toEqual([]);
    saveQueue("s1", []);
    expect(localStorage.getItem("tt.chat.queue.s1")).toBeNull();
  });

  it("treats malformed storage as an empty queue", () => {
    localStorage.setItem("tt.chat.queue.s1", "{not json");
    expect(loadQueue("s1")).toEqual([]);
    localStorage.setItem("tt.chat.queue.s1", JSON.stringify({ a: 1 }));
    expect(loadQueue("s1")).toEqual([]);
    localStorage.setItem("tt.chat.queue.s1", JSON.stringify([{ id: 1 }, item("ok")]));
    expect(loadQueue("s1")).toEqual([item("ok")]);
  });

  it("survives storage that throws", () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new Error("denied");
    });
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("denied");
    });
    vi.spyOn(Storage.prototype, "removeItem").mockImplementation(() => {
      throw new Error("denied");
    });
    expect(loadQueue("s1")).toEqual([]);
    expect(() => saveQueue("s1", [item("a")])).not.toThrow();
    expect(() => saveQueue("s1", [])).not.toThrow();
  });
});
