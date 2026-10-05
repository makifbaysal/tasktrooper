import { describe, expect, it } from "vitest";
import { tStatic } from "@/hooks/useI18n";
import type { FeedItem, FeedLane, FeedTool } from "@/lib/activityFeed";
import {
  currentLabel,
  eventLabel,
  formatElapsed,
  formatToolDuration,
  humanizeToolName,
  laneTitle,
  statusLabel,
  toolGroupLabel,
  toolLabel,
} from "@/lib/activityFeedLabels";

const t = tStatic;

function tool(over: Partial<FeedTool>): FeedTool {
  return { id: "1", name: "read_file", kind: "read", isError: false, status: "completed", startedAt: "", ...over };
}

describe("activityFeedLabels (en)", () => {
  it("labels a tool in its done and running forms", () => {
    expect(toolLabel(t, tool({ target: "run.go" }))).toBe("run.go read");
    expect(toolLabel(t, tool({ target: "run.go", status: "running" }))).toBe("Reading run.go…");
  });

  it("falls back to a humanized name without a target", () => {
    expect(toolLabel(t, tool({ name: "mcp__srv__list_open_issues", kind: "other" }))).toBe("List open issues");
    expect(toolLabel(t, tool({ kind: "read", name: "read_file" }))).toBe("Read file");
    expect(humanizeToolName("mcp__a__do_thing")).toBe("Do thing");
  });

  it("labels groups by count", () => {
    const tools = [tool({ id: "a" }), tool({ id: "b" })];
    const item: Extract<FeedItem, { kind: "tools" }> = { kind: "tools", id: "g", toolKind: "read", tools, status: "completed", at: "" };
    expect(toolGroupLabel(t, item)).toBe("2 files read");
    expect(toolGroupLabel(t, { ...item, tools: [tools[0]], toolKind: "read" })).toBe("Read file");
  });

  it("labels the current state", () => {
    expect(currentLabel(t, { kind: "thinking", since: "" })).toBe("Thinking…");
    expect(currentLabel(t, { kind: "tool", tool: tool({ target: "x.go", status: "running" }), since: "" })).toBe("Reading x.go…");
    expect(currentLabel(t, null)).toBe("Thinking…");
  });

  it("labels events, dropping an unresolved parameter", () => {
    const base = { kind: "event", id: "e", tone: "info", status: "completed", at: "" } as const;
    expect(eventLabel(t, { ...base, stepType: "planner_complete", payload: { task_count: 3 } })).toBe("Plan ready (3 tasks)");
    expect(eventLabel(t, { ...base, stepType: "planner_complete", payload: {} })).toBe("Plan ready");
    expect(eventLabel(t, { ...base, stepType: "verification_complete", payload: { passed: false } })).toBe("Verification failed");
    expect(eventLabel(t, { ...base, stepType: "mystery_step", payload: {} })).toBe("mystery_step");
  });

  it("titles lanes with fallbacks and labels run statuses", () => {
    const lane: FeedLane = { id: "task:T-1@r2", source: "subtask", title: "", status: "running", items: [], toolCount: 0, startedAt: "", current: null };
    expect(laneTitle(t, lane)).toBe("T-1");
    expect(laneTitle(t, { ...lane, id: "sub:1", source: "subagent" })).toBe("Sub-agent");
    expect(statusLabel(t, "running")).toBe("Running");
    expect(statusLabel(t, "weird")).toBe("weird");
  });

  it("formats durations", () => {
    expect(formatElapsed(65_000)).toBe("1:05");
    expect(formatElapsed(3_725_000)).toBe("1:02:05");
    expect(formatToolDuration(400)).toBe("<1s");
    expect(formatToolDuration(4_000)).toBe("4s");
    expect(formatToolDuration(75_000)).toBe("1m 15s");
  });
});
