import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Agent } from "@/api";

const { listActivity } = vi.hoisted(() => ({
  listActivity: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    api: {
      ...actual.api,
      listActivity,
    },
  };
});

import { useAgentUnread } from "@/hooks/useAgentUnread";

const agents: Agent[] = [
  {
    id: "agent-1",
    name: "Rex",
    description: "",
    subagent_type: "general",
    system_prompt: "",
    provider_type: "",
    model: "",
    model_heavy: "",
    tool_policy: {},
    skill_ids: [],
    enabled: true,
    self_evolution_enabled: false,
    created_at: "2026-09-17T10:00:00Z",
  },
  {
    id: "agent-2",
    name: "Ada",
    description: "",
    subagent_type: "general",
    system_prompt: "",
    provider_type: "",
    model: "",
    model_heavy: "",
    tool_policy: {},
    skill_ids: [],
    enabled: true,
    self_evolution_enabled: false,
    created_at: "2026-09-17T10:00:00Z",
  },
];

function chatMessage(agentId: string, createdAt: string, overrides: Record<string, unknown> = {}) {
  return {
    id: `evt-${agentId}-${createdAt}`,
    kind: "chat_message",
    agent_id: agentId,
    session_id: `session-${agentId}`,
    summary: "hi",
    created_at: createdAt,
    ...overrides,
  };
}

beforeEach(() => {
  window.localStorage.clear();
  listActivity.mockResolvedValue({ items: [] });
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("useAgentUnread", () => {
  it("counts a chat_message newer than the last-viewed time as unread", async () => {
    listActivity.mockResolvedValue({ items: [chatMessage("agent-1", "2026-09-18T10:00:00Z")] });
    const { result } = renderHook(() => useAgentUnread(agents, null));
    await waitFor(() => expect(result.current.counts["agent-1"]).toBe(1));
  });

  it("does not count an agent unread from a task comment alone (chat-only)", async () => {
    listActivity.mockResolvedValue({
      items: [
        {
          id: "evt-1",
          kind: "board_event",
          task_id: "task-1",
          event_type: "task.commented",
          payload: { author_type: "agent", author_id: "agent-2", content: "done" },
          created_at: "2026-09-18T10:00:00Z",
        },
      ],
    });
    const { result } = renderHook(() => useAgentUnread(agents, null));
    await waitFor(() => expect(listActivity).toHaveBeenCalled());
    expect(result.current.counts["agent-2"]).toBeUndefined();
  });

  it("sums multiple unread chat messages from the same agent into one count", async () => {
    listActivity.mockResolvedValue({
      items: [
        chatMessage("agent-2", "2026-09-18T10:00:00Z"),
        chatMessage("agent-2", "2026-09-18T10:05:00Z"),
      ],
    });
    const { result } = renderHook(() => useAgentUnread(agents, null));
    await waitFor(() => expect(result.current.counts["agent-2"]).toBe(2));
  });

  it("clears an agent's count via markAgentSeen and persists it to localStorage", async () => {
    listActivity.mockResolvedValue({ items: [chatMessage("agent-2", "2026-09-18T10:00:00Z")] });
    const { result } = renderHook(() => useAgentUnread(agents, null));
    await waitFor(() => expect(result.current.counts["agent-2"]).toBe(1));

    act(() => {
      result.current.markAgentSeen("agent-2", "2026-09-18T10:00:00Z");
    });

    await waitFor(() => expect(result.current.counts["agent-2"]).toBeUndefined());
    const stored = JSON.parse(window.localStorage.getItem("tt.agentChat.lastViewedAt") ?? "{}");
    expect(stored["agent-2"]).toBe("2026-09-18T10:00:00Z");
  });

  it("excludes the active agent's own chat from the count and marks it seen", async () => {
    listActivity.mockResolvedValue({ items: [chatMessage("agent-1", "2026-09-18T10:00:00Z")] });
    const { result } = renderHook(() => useAgentUnread(agents, "agent-1"));
    await waitFor(() => expect(listActivity).toHaveBeenCalled());
    expect(result.current.counts["agent-1"]).toBeUndefined();
  });
});
