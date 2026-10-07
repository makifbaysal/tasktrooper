import { describe, expect, it } from "vitest";
import type { SessionMessage } from "@/api";
import { keepMessages, mergeServerMessages, sessionRunKey } from "@/lib/chat";

const msg = (id: string, content: string, role: SessionMessage["role"] = "assistant"): SessionMessage => ({
  id,
  role,
  content,
  created_at: "2026-09-17T10:00:00Z",
});

describe("mergeServerMessages", () => {
  it("returns the current array when the server transcript holds the same ids and content", () => {
    const current = [msg("m1", "hi", "user"), msg("m2", "hello")];
    const server = [msg("m1", "hi", "user"), msg("m2", "hello")];
    expect(mergeServerMessages(server, current)).toBe(current);
  });

  it("takes the server's transcript when a message was added or its content changed", () => {
    const current = [msg("m1", "hi", "user"), msg("m2", "hel")];
    const grown = [msg("m1", "hi", "user"), msg("m2", "hello")];
    expect(mergeServerMessages(grown, current)).toBe(grown);
    const added = [...grown, msg("m3", "more")];
    expect(mergeServerMessages(added, grown)).toBe(added);
  });

  it("keeps an optimistic bubble until the server has the same message", () => {
    const optimistic = msg("temp-1", "do it", "user");
    const current = [msg("m1", "hi", "user"), optimistic];
    const without = [msg("m1", "hi", "user")];
    expect(mergeServerMessages(without, current)).toBe(current);

    const confirmed = [msg("m1", "hi", "user"), msg("m9", "do it", "user")];
    expect(mergeServerMessages(confirmed, current)).toBe(confirmed);
  });
});

describe("keepMessages", () => {
  it("keeps the previous transcript only when ids and content all match", () => {
    const prev = [msg("m1", "a")];
    expect(keepMessages(prev, [msg("m1", "a")])).toBe(prev);
    const edited = [msg("m1", "b")];
    expect(keepMessages(prev, edited)).toBe(edited);
  });

  it("takes the new transcript when only a message's attachments changed", () => {
    const attachment = {
      id: "a1",
      filename: "shot.png",
      content_type: "image/png",
      size_bytes: 1,
      sha256: "x",
      created_by_type: "user",
      created_at: "2026-09-17T10:00:00Z",
    };
    const prev = [msg("m1", "see this", "user")];
    const withFile = [{ ...msg("m1", "see this", "user"), attachments: [attachment] }];
    expect(keepMessages(prev, withFile)).toBe(withFile);
    expect(keepMessages(withFile, [{ ...msg("m1", "see this", "user"), attachments: [{ ...attachment }] }])).toBe(withFile);
  });
});

describe("sessionRunKey", () => {
  it("changes when a run's status or completion changes", () => {
    const run = { id: "r1", request_id: "q", status: "running", started_at: "2026-09-17T10:00:00Z" };
    const done = { ...run, status: "completed", completed_at: "2026-09-17T10:05:00Z" };
    expect(sessionRunKey(run)).toBe(sessionRunKey({ ...run }));
    expect(sessionRunKey(run)).not.toBe(sessionRunKey(done));
  });
});
