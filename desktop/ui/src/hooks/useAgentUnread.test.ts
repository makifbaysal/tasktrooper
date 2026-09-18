import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Agent } from "@/api";

const { listSessions, listActivity } = vi.hoisted(() => ({
  listSessions: vi.fn(),
  listActivity: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    api: {
      ...actual.api,
      listSessions,
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

beforeEach(() => {
  window.localStorage.clear();
  listSessions.mockResolvedValue({ sessions: [] });
  listActivity.mockResolvedValue({ items: [] });
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("useAgentUnread", () => {
  it("still reports an agent unread from a session update (regression)", async () => {
    listSessions.mockResolvedValue({
      sessions: [{ id: "s1", agent_id: "agent-1", title: "t", model: "m", created_at: "", updated_at: "2026-09-18T10:00:00Z" }],
    });
    const { result } = renderHook(() => useAgentUnread(agents, null));
    await waitFor(() => expect(result.current.unread.has("agent-1")).toBe(true));
  });

  it("reports an agent unread from an agent's task comment, with no session at all", async () => {
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
    await waitFor(() => expect(result.current.unread.has("agent-2")).toBe(true));
  });

  it("clears an agent via markAgentSeen and persists it to localStorage", async () => {
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
    await waitFor(() => expect(result.current.unread.has("agent-2")).toBe(true));

    act(() => {
      result.current.markAgentSeen("agent-2", "2026-09-18T10:00:00Z");
    });

    await waitFor(() => expect(result.current.unread.has("agent-2")).toBe(false));
    const stored = JSON.parse(window.localStorage.getItem("tt.agentChat.lastViewedAt") ?? "{}");
    expect(stored["agent-2"]).toBe("2026-09-18T10:00:00Z");
  });

  it("still excludes the active agent's chat (regression)", async () => {
    listSessions.mockResolvedValue({
      sessions: [{ id: "s1", agent_id: "agent-1", title: "t", model: "m", created_at: "", updated_at: "2026-09-18T10:00:00Z" }],
    });
    const { result } = renderHook(() => useAgentUnread(agents, "agent-1"));
    await waitFor(() => expect(listSessions).toHaveBeenCalled());
    expect(result.current.unread.has("agent-1")).toBe(false);
  });
});
