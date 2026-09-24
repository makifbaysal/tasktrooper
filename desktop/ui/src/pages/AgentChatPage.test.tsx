import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { describe, expect, it, vi, beforeEach } from "vitest";
import type { Agent } from "@/api";
import { AgentChatPage } from "@/pages/AgentChatPage";
import { I18nProvider } from "@/hooks/useI18n";

const {
  listAgentSessions,
  getAgent,
  getSession,
  sessionActivity,
  activeRuns,
  listFiles,
  listAgents,
  listInitiativeProjects,
  listRepositories,
} = vi.hoisted(() => ({
  listAgentSessions: vi.fn(),
  getAgent: vi.fn(),
  getSession: vi.fn(),
  sessionActivity: vi.fn(),
  activeRuns: vi.fn(),
  listFiles: vi.fn(),
  listAgents: vi.fn(),
  listInitiativeProjects: vi.fn(),
  listRepositories: vi.fn(),
}));

const { sendSessionMessageWithRecovery } = vi.hoisted(() => ({
  sendSessionMessageWithRecovery: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    api: {
      ...actual.api,
      listAgentSessions,
      getAgent,
      getSession,
      sessionActivity,
      activeRuns,
      listFiles,
      listAgents,
      listInitiativeProjects,
      listRepositories,
    },
  };
});

vi.mock("@/lib/sessionSend", async () => {
  const actual = await vi.importActual<typeof import("@/lib/sessionSend")>("@/lib/sessionSend");
  return {
    ...actual,
    sendSessionMessageWithRecovery,
  };
});

const agent: Agent = {
  id: "agent-1",
  name: "Rex",
  description: "Reviews pull requests",
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
};

function renderChatPage(initialEntry = "/agents/agent-1/chat") {
  return render(
    <I18nProvider>
      <MemoryRouter initialEntries={[initialEntry]}>
        <Routes>
          <Route path="agents/:agentId/chat" element={<AgentChatPage />} />
          <Route path="agents/:agentId/chat/:sessionId" element={<AgentChatPage />} />
        </Routes>
      </MemoryRouter>
    </I18nProvider>,
  );
}

describe("AgentChatPage header shortcuts", () => {
  beforeEach(() => {
    listAgentSessions.mockReset().mockResolvedValue({ sessions: [] });
    getAgent.mockReset().mockResolvedValue(agent);
    listFiles.mockReset().mockResolvedValue({ files: [] });
    listAgents.mockReset().mockResolvedValue({ agents: [] });
    listInitiativeProjects.mockReset().mockResolvedValue({ projects: [] });
    listRepositories.mockReset().mockResolvedValue({ repositories: [] });
  });

  it("renders a Memory shortcut next to the Performance shortcut, linking to that agent's memory view", async () => {
    renderChatPage();

    await waitFor(() => expect(getAgent).toHaveBeenCalledWith("agent-1"));

    const performanceLink = await screen.findByRole("link", { name: "Performance" });
    const memoryLink = screen.getByRole("link", { name: "Memory" });

    expect(memoryLink).toHaveAttribute("href", "/agents/agent-1/memory");
    expect(performanceLink).toHaveAttribute("href", "/agents/agent-1/performance");
  });

  it("does not render the agent's description under its name", async () => {
    renderChatPage();

    await waitFor(() => expect(getAgent).toHaveBeenCalledWith("agent-1"));
    await screen.findByRole("heading", { name: "Rex" });

    expect(screen.queryByText("Reviews pull requests")).not.toBeInTheDocument();
    expect(screen.queryByText("Talk directly with this agent")).not.toBeInTheDocument();
  });
});

describe("AgentChatPage sidebar title refresh", () => {
  const untitledSession = {
    id: "session-1",
    title: "Rex ile sohbet",
    model: "",
    agent_id: "agent-1",
    created_at: "2026-09-23T10:00:00Z",
    updated_at: "2026-09-23T10:00:00Z",
  };
  const autoTitledSession = { ...untitledSession, title: "Postgres migration nasıl geri alınır" };

  beforeEach(() => {
    Element.prototype.scrollIntoView = vi.fn();
    getAgent.mockReset().mockResolvedValue(agent);
    listFiles.mockReset().mockResolvedValue({ files: [] });
    listAgents.mockReset().mockResolvedValue({ agents: [] });
    listInitiativeProjects.mockReset().mockResolvedValue({ projects: [] });
    listRepositories.mockReset().mockResolvedValue({ repositories: [] });
    getSession.mockReset().mockResolvedValue({
      session: untitledSession,
      messages: [],
      actions: [],
    });
    sessionActivity.mockReset().mockResolvedValue({ runs: [] });
    activeRuns.mockReset().mockResolvedValue({ runs: [] });
    listAgentSessions
      .mockReset()
      .mockResolvedValueOnce({ sessions: [untitledSession] })
      .mockResolvedValue({ sessions: [autoTitledSession] });
    sendSessionMessageWithRecovery.mockReset().mockResolvedValue({
      status: "response",
      response: { message: { role: "assistant", content: "Şöyle geri alabilirsin..." } },
      messages: [
        { id: "m1", role: "user", content: "Postgres migration nasıl geri alınır?", created_at: "2026-09-23T10:01:00Z" },
        { id: "m2", role: "assistant", content: "Şöyle geri alabilirsin...", created_at: "2026-09-23T10:01:05Z" },
      ],
      actions: [],
    });
  });

  it("shows the backend's auto-generated title without reloading once the first turn answers", async () => {
    renderChatPage("/agents/agent-1/chat/session-1");

    await screen.findByText("Rex ile sohbet");
    expect(listAgentSessions).toHaveBeenCalledTimes(1);

    const textarea = await screen.findByPlaceholderText(
      "Type your message... (Enter to send, Shift+Enter for a new line)",
    );
    fireEvent.change(textarea, { target: { value: "Postgres migration nasıl geri alınır?" } });
    fireEvent.keyDown(textarea, { key: "Enter" });

    await waitFor(() => expect(sendSessionMessageWithRecovery).toHaveBeenCalled());
    await waitFor(() => expect(listAgentSessions).toHaveBeenCalledTimes(2));
    await screen.findByText("Postgres migration nasıl geri alınır");
    expect(screen.queryByText("Rex ile sohbet")).not.toBeInTheDocument();
  });

  it("gives two separate sessions of the same agent distinct titles once the active one answers", async () => {
    // A sibling session already auto-titled from its own, different first
    // message — proves the refresh brings back every session's own title, not
    // just a copy of the one just answered.
    const siblingSession = { ...untitledSession, id: "session-2", title: "Prod deploy nasıl geri alınır" };
    listAgentSessions
      .mockReset()
      .mockResolvedValueOnce({ sessions: [untitledSession, siblingSession] })
      .mockResolvedValue({ sessions: [autoTitledSession, siblingSession] });

    renderChatPage("/agents/agent-1/chat/session-1");
    await screen.findByText("Prod deploy nasıl geri alınır");
    expect(screen.getAllByText("Rex ile sohbet")).toHaveLength(1);

    const textarea = await screen.findByPlaceholderText(
      "Type your message... (Enter to send, Shift+Enter for a new line)",
    );
    fireEvent.change(textarea, { target: { value: "Postgres migration nasıl geri alınır?" } });
    fireEvent.keyDown(textarea, { key: "Enter" });

    await screen.findByText("Postgres migration nasıl geri alınır");
    expect(screen.getByText("Prod deploy nasıl geri alınır")).toBeInTheDocument();
    expect(screen.queryByText("Rex ile sohbet")).not.toBeInTheDocument();
  });
});
