import { describe, expect, it } from "vitest";
import type { ClarificationRequest, SessionMessage } from "@/api";
import { clarificationAnswers } from "@/lib/clarification";

const clarification = { questions: [] } as unknown as ClarificationRequest;

const msg = (id: string, role: SessionMessage["role"], content: string, asks = false): SessionMessage => ({
  id,
  role,
  content,
  created_at: "2026-09-17T10:00:00Z",
  ...(asks ? { clarification } : {}),
});

describe("clarificationAnswers", () => {
  it("answers each clarification with the first user message after it", () => {
    const answers = clarificationAnswers([
      msg("q1", "assistant", "which db?", true),
      msg("a1", "assistant", "still thinking"),
      msg("u1", "user", "postgres"),
      msg("u2", "user", "and redis"),
      msg("q2", "assistant", "which region?", true),
      msg("u3", "user", "eu"),
    ]);
    expect(answers.get("q1")).toBe("postgres");
    expect(answers.get("q2")).toBe("eu");
  });

  it("leaves a clarification nobody has replied to yet out of the map", () => {
    const answers = clarificationAnswers([
      msg("u0", "user", "start"),
      msg("q1", "assistant", "which db?", true),
      msg("a1", "assistant", "waiting"),
    ]);
    expect(answers.has("q1")).toBe(false);
  });
});
