import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Agent } from "@/api";
import { I18nProvider } from "@/hooks/useI18n";
import { LeadQuickAsk } from "@/components/layout/LeadQuickAsk";

const { createSession, listAgents, listInitiativeProjects, listRepositories, uploadAttachment } = vi.hoisted(() => ({
  createSession: vi.fn(),
  listAgents: vi.fn(),
  listInitiativeProjects: vi.fn(),
  listRepositories: vi.fn(),
  uploadAttachment: vi.fn(),
}));

vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  return {
    ...actual,
    api: {
      ...actual.api,
      createSession,
      listAgents,
      listInitiativeProjects,
      listRepositories,
      uploadAttachment,
    },
  };
});

const agent = { id: "pm", name: "Pia", catalog_slug: "product-manager", enabled: true } as Agent;

function renderQuickAsk() {
  return render(
    <I18nProvider>
      <MemoryRouter initialEntries={["/board"]}>
        <Routes>
          <Route path="/board" element={<LeadQuickAsk agent={agent} />} />
          <Route path="/agents/:id/chat/:sessionId" element={<p>lead chat</p>} />
        </Routes>
      </MemoryRouter>
    </I18nProvider>,
  );
}

function makeFile(name: string, type: string) {
  return new File(["x"], name, { type });
}

describe("LeadQuickAsk", () => {
  beforeEach(() => {
    listAgents.mockReset().mockResolvedValue({ agents: [] });
    listInitiativeProjects.mockReset().mockResolvedValue({ projects: [] });
    listRepositories.mockReset().mockResolvedValue({ repositories: [] });
    createSession.mockReset();
    uploadAttachment.mockReset();
  });

  it("opens a mention menu listing repositories and agents on @", async () => {
    listRepositories.mockResolvedValue({ repositories: [{ id: "r-1", name: "acme-web", description: "" }] });
    renderQuickAsk();
    const textarea = await screen.findByRole("textbox");
    fireEvent.change(textarea, { target: { value: "@acm", selectionStart: 4 } });
    expect(await screen.findByText("acme-web")).toBeInTheDocument();
  });

  it("pastes a file onto the box and sends it along with the typed message", async () => {
    const meta = { id: "att-1", filename: "shot.png", content_type: "image/png", size_bytes: 10 };
    uploadAttachment.mockResolvedValue(meta);
    createSession.mockResolvedValue({ id: "s-1" });
    renderQuickAsk();
    const textarea = await screen.findByRole("textbox");

    const file = makeFile("shot.png", "image/png");
    fireEvent.paste(textarea, { clipboardData: { files: [file] } });
    await waitFor(() => expect(screen.getByTitle("shot.png")).toBeInTheDocument());

    fireEvent.change(textarea, { target: { value: "look at this" } });
    fireEvent.keyDown(textarea, { key: "Enter" });

    await screen.findByText("lead chat");
    expect(createSession).toHaveBeenCalled();
  });

  it("adds a file via the attach button and sends it with the message", async () => {
    const meta = { id: "att-2", filename: "spec.pdf", content_type: "application/pdf", size_bytes: 20 };
    uploadAttachment.mockResolvedValue(meta);
    createSession.mockResolvedValue({ id: "s-2" });
    renderQuickAsk();
    await screen.findByRole("textbox");

    const file = makeFile("spec.pdf", "application/pdf");
    const fileInput = document.querySelector('input[type="file"]') as HTMLInputElement;
    fireEvent.change(fileInput, { target: { files: [file] } });
    await waitFor(() => expect(screen.getByTitle("spec.pdf")).toBeInTheDocument());

    fireEvent.change(screen.getByRole("textbox"), { target: { value: "see attached" } });
    fireEvent.keyDown(screen.getByRole("textbox"), { key: "Enter" });

    await screen.findByText("lead chat");
    expect(createSession).toHaveBeenCalled();
  });
});
