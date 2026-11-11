import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Outlet, Route, Routes, useLocation } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@/hooks/useI18n";
import { audience, audienceDetail, study } from "@/test/audienceFixtures";

const api = vi.hoisted(() => ({
  audienceRuntime: vi.fn(),
  audiences: vi.fn(),
  audienceStudies: vi.fn(),
  audience: vi.fn(),
  createAudience: vi.fn(),
  updateAudience: vi.fn(),
  buildAudience: vi.fn(),
  cancelAudienceBuild: vi.fn(),
  deleteAudience: vi.fn(),
  planAudienceStudy: vi.fn(),
  startAudienceStudy: vi.fn(),
  listRepositories: vi.fn(),
  createSession: vi.fn(),
  audienceThread: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, ...api } };
});
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn(), info: vi.fn() } }));

import AudienceDetailPage from "./AudienceDetailPage";
import AudiencePage from "./AudiencePage";

function Chat() {
  const location = useLocation();
  return (
    <div data-testid="chat">
      {location.pathname} {String((location.state as { autoSend?: string } | null)?.autoSend ?? "")}
    </div>
  );
}

function renderAt(path: string, context: { leadAgent: unknown } = { leadAgent: null }) {
  return render(
    <I18nProvider>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route element={<Outlet context={context} />}>
            <Route path="/audience" element={<AudiencePage />} />
            <Route path="/audience/:audienceId" element={<AudienceDetailPage />} />
            <Route path="/audience/studies/:studyId" element={<div>study page</div>} />
            <Route path="/audience/studies/new" element={<div>new study page</div>} />
            <Route path="/agents/:agentId/chat/:sessionId" element={<Chat />} />
          </Route>
          <Route path="/settings/llm" element={<div>llm settings</div>} />
        </Routes>
      </MemoryRouter>
    </I18nProvider>,
  );
}

beforeEach(() => {
  for (const f of Object.values(api)) f.mockReset();
  api.audienceRuntime.mockResolvedValue({ ready: true, agent: "Product Manager", provider: "claude_code" });
  api.listRepositories.mockResolvedValue({ repositories: [{ id: "r1", name: "acme" }] });
});

describe("Audience page", () => {
  it("guides to the LLM connection when there is no agent to run on", async () => {
    api.audienceRuntime.mockResolvedValue({ ready: false });
    api.audiences.mockResolvedValue({ audiences: [] });
    api.audienceStudies.mockResolvedValue({ studies: [] });
    renderAt("/audience");
    expect(await screen.findByText("No agent is connected to run this on")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("link", { name: "Open LLM connection" }));
    expect(await screen.findByText("llm settings")).toBeInTheDocument();
  });

  it("starts with two cards, create your audience and start a study, even before any audience exists", async () => {
    api.audiences.mockResolvedValue({ audiences: [] });
    api.audienceStudies.mockResolvedValue({ studies: [] });
    renderAt("/audience");
    expect(await screen.findByText("No audience yet")).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Where do you want to start?" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Create your audience/ })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /Start a study/ })).toHaveAttribute("href", "/audience/studies/new");
  });

  it("opens the form to create an audience when there is no Product Manager to hand it to", async () => {
    api.audiences.mockResolvedValue({ audiences: [] });
    api.audienceStudies.mockResolvedValue({ studies: [] });
    renderAt("/audience");
    fireEvent.click(await screen.findByRole("button", { name: /Create your audience/ }));
    expect(await screen.findByLabelText("The product and where it stands today")).toBeInTheDocument();
  });

  it("hands the guided audience request to the Product Manager and opens the chat with it already sent", async () => {
    api.audiences.mockResolvedValue({ audiences: [] });
    api.audienceStudies.mockResolvedValue({ studies: [] });
    api.createSession.mockResolvedValue({ id: "sess1" });
    renderAt("/audience", { leadAgent: { id: "pm1", name: "Product Manager", enabled: true } });
    fireEvent.click(await screen.findByRole("button", { name: /Create your audience/ }));
    await waitFor(() => expect(api.createSession).toHaveBeenCalledWith(expect.objectContaining({ agent_id: "pm1" })));
    const chat = await screen.findByTestId("chat");
    expect(chat).toHaveTextContent("/agents/pm1/chat/sess1");
    expect(chat).toHaveTextContent("which segments and which countries");
  });

  it("lists audiences with status and counts, and studies with theirs", async () => {
    api.audiences.mockResolvedValue({ audiences: [audience(), audience({ id: "a2", name: "CTOs", status: "building", persona_count: 0, study_count: 0 })] });
    api.audienceStudies.mockResolvedValue({ studies: [study(), study({ id: "s2", title: "Pricing", status: "running", persona_done: 1 })] });
    renderAt("/audience");
    expect(await screen.findByText("Freelance designers")).toBeInTheDocument();
    expect(screen.getByText("CTOs")).toBeInTheDocument();
    expect(screen.getByText("Building")).toBeInTheDocument();
    expect(screen.getByText("2 people")).toBeInTheDocument();
    expect(screen.getByText("Would they pay?")).toBeInTheDocument();
    expect(screen.getByText("1 of 2 interviews done")).toBeInTheDocument();
    expect(screen.getAllByRole("link", { name: /Freelance designers/ }).map((l) => l.getAttribute("href"))).toContain("/audience/a1");
  });

  it("creates an audience from the brief and opens it", async () => {
    api.audiences.mockResolvedValue({ audiences: [] });
    api.audienceStudies.mockResolvedValue({ studies: [] });
    api.createAudience.mockResolvedValue(audience({ id: "new" }));
    api.audience.mockResolvedValue(audienceDetail({ id: "new" }));
    renderAt("/audience");
    fireEvent.click((await screen.findAllByRole("button", { name: "New audience" }))[0]!);
    fireEvent.change(await screen.findByLabelText("Name"), { target: { value: " Designers " } });
    expect(screen.getByRole("button", { name: "Create" })).toBeDisabled();
    fireEvent.change(screen.getByLabelText("The product and where it stands today"), { target: { value: "A kanban for agents" } });
    fireEvent.click(screen.getByRole("button", { name: "Add a segment" }));
    fireEvent.change(screen.getByLabelText("Segment"), { target: { value: "Freelancers" } });
    fireEvent.click(screen.getByRole("button", { name: "Create" }));
    await waitFor(() => expect(api.createAudience).toHaveBeenCalled());
    const sent = api.createAudience.mock.calls[0]![0];
    expect(sent.name).toBe("Designers");
    expect(sent.brief.product).toBe("A kanban for agents");
    expect(sent.brief.segments).toEqual([{ name: "Freelancers", description: "", count: 0 }]);
    expect(sent.persona_target).toBe(12);
    expect(await screen.findByRole("heading", { name: "Freelance designers" })).toBeInTheDocument();
  });
});

describe("Audience detail page", () => {
  it("shows the brief, the invented people and the studies", async () => {
    api.audience.mockResolvedValue(audienceDetail());
    renderAt("/audience/a1");
    expect(await screen.findByRole("heading", { name: "Freelance designers" })).toBeInTheDocument();
    expect(screen.getByText("A kanban board where AI agents do the work.")).toBeInTheDocument();
    expect(screen.getByText("Ayse Demir")).toBeInTheDocument();
    expect(screen.getByText("Can Yilmaz")).toBeInTheDocument();
    expect(screen.getByText(/invented from your brief/)).toBeInTheDocument();
    expect(screen.getByText("Would they pay?")).toBeInTheDocument();
  });

  it("opens a profile and a conversation with a person", async () => {
    api.audience.mockResolvedValue(audienceDetail());
    api.audienceThread.mockResolvedValue({ messages: [] });
    window.HTMLElement.prototype.scrollIntoView = vi.fn();
    renderAt("/audience/a1");
    await screen.findByText("Ayse Demir");
    fireEvent.click(screen.getAllByRole("button", { name: "Profile" })[0]!);
    expect(screen.getByText("Works alone for startups.")).toBeInTheDocument();
    fireEvent.click(screen.getAllByRole("button", { name: "Talk" })[0]!);
    expect(await screen.findByText("Talk to Ayse Demir")).toBeInTheDocument();
    expect(api.audienceThread).toHaveBeenCalledWith("p1");
  });

  it("names the number of model calls before it builds again, and only builds on confirm", async () => {
    api.audience.mockResolvedValue(audienceDetail());
    api.buildAudience.mockResolvedValue(audience({ status: "building" }));
    renderAt("/audience/a1");
    fireEvent.click(await screen.findByRole("button", { name: "Build again" }));
    expect(await screen.findByText(/replaces the 12 people with new ones, with about 2 model call/)).toBeInTheDocument();
    expect(api.buildAudience).not.toHaveBeenCalled();
    const buttons = screen.getAllByRole("button", { name: "Build again" });
    fireEvent.click(buttons[buttons.length - 1]!);
    await waitFor(() => expect(api.buildAudience).toHaveBeenCalledWith("a1"));
  });

  it("builds the first time and shows the progress notice while it builds", async () => {
    api.audience.mockResolvedValue(audienceDetail({ status: "building", personas: [], persona_count: 0 }));
    renderAt("/audience/a1");
    expect(await screen.findByText("Writing the people")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Cancel build" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Edit brief" })).toBeDisabled();
  });

  it("sends planning a study to the wizard with this audience chosen, and a planned study back to its script", async () => {
    api.audience.mockResolvedValue(audienceDetail({ studies: [study({ id: "s9", status: "draft", title: "Price test", calls_estimate: 3, persona_total: 2 })] }));
    renderAt("/audience/a1");
    expect(await screen.findByText("Planned: 2 interviews and the report, about 3 model calls.")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Continue" })).toHaveAttribute("href", "/audience/studies/s9");
    expect(api.startAudienceStudy).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Plan a study" }));
    expect(await screen.findByText("new study page")).toBeInTheDocument();
  });

  it("will not plan a study before there are people", async () => {
    api.audience.mockResolvedValue(audienceDetail({ personas: [], persona_count: 0, studies: [], status: "draft" }));
    renderAt("/audience/a1");
    expect(await screen.findByText("No people yet")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Plan a study" })).toBeDisabled();
  });

  it("says so when the audience does not exist", async () => {
    const { ApiError } = await import("@/api");
    api.audience.mockRejectedValue(new ApiError("not found", 404));
    renderAt("/audience/gone");
    expect(await screen.findByText("Audience not found")).toBeInTheDocument();
  });
});
