import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Outlet, Route, Routes, useLocation } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@/hooks/useI18n";
import { audience, study } from "@/test/audienceFixtures";

const api = vi.hoisted(() => ({
  audiences: vi.fn(),
  planAudienceStudy: vi.fn(),
  createSession: vi.fn(),
  createAudience: vi.fn(),
  listRepositories: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, ...api } };
});
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn(), info: vi.fn() } }));

import NewStudyPage from "./NewStudyPage";

function Chat() {
  const location = useLocation();
  return (
    <div data-testid="chat">
      {location.pathname} {String((location.state as { autoSend?: string } | null)?.autoSend ?? "")}
    </div>
  );
}

function renderAt(path = "/audience/studies/new", leadAgent: unknown = null) {
  return render(
    <I18nProvider>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route element={<Outlet context={{ leadAgent }} />}>
            <Route path="/audience/studies/new" element={<NewStudyPage />} />
            <Route path="/audience/studies/:studyId" element={<div>script step of study</div>} />
            <Route path="/audience" element={<div>audience home</div>} />
            <Route path="/audience/:audienceId" element={<div>audience detail</div>} />
            <Route path="/agents/:agentId/chat/:sessionId" element={<Chat />} />
          </Route>
        </Routes>
      </MemoryRouter>
    </I18nProvider>,
  );
}

const ready = audience({ id: "a1", name: "Freelance designers", persona_count: 4 });

beforeEach(() => {
  for (const f of Object.values(api)) f.mockReset();
  api.audiences.mockResolvedValue({ audiences: [ready, audience({ id: "a2", name: "Still building", status: "building", persona_count: 0 })] });
  api.listRepositories.mockResolvedValue({ repositories: [] });
});

describe("the first two steps of a study", () => {
  it("shows the prompt box and the four study types, and needs a type before moving on", async () => {
    renderAt();
    expect(await screen.findByRole("heading", { name: "What do you want to find out?" })).toBeInTheDocument();
    expect(screen.getByPlaceholderText("Ask anything, or pick any study type below")).toBeInTheDocument();
    const kinds = screen.getAllByRole("radio");
    expect(kinds.map((k) => k.textContent)).toEqual([
      expect.stringContaining("User interviews"),
      expect.stringContaining("Landing page test"),
      expect.stringContaining("Message testing"),
      expect.stringContaining("A/B test"),
    ]);
    expect(screen.getByText("Interview your exact audience, from first idea to live funnel")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Next/ })).toBeDisabled();
    expect(screen.getByRole("navigation", { name: "Study steps" })).toBeInTheDocument();
    expect(screen.getByText("Context").closest("li")).toHaveAttribute("aria-current", "step");
  });

  it("plans a user-interview study on a ready audience only, with the prices and goal, then opens it", async () => {
    api.planAudienceStudy.mockResolvedValue(study({ id: "new1", status: "draft" }));
    renderAt();
    fireEvent.change(await screen.findByPlaceholderText("Ask anything, or pick any study type below"), { target: { value: "Would they pay?" } });
    fireEvent.click(screen.getByRole("radio", { name: /User interviews/ }));
    fireEvent.change(screen.getByLabelText("Prices to test (optional)"), { target: { value: "19, 9" } });
    fireEvent.click(screen.getByRole("button", { name: /Next/ }));

    expect(await screen.findByText("Freelance designers")).toBeInTheDocument();
    expect(screen.queryByText("Still building")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Write the script/ })).toBeDisabled();
    fireEvent.click(screen.getByRole("radio", { name: /Freelance designers/ }));
    expect(screen.getByText("4 people, the report and the script: 6 model calls in all.")).toBeInTheDocument();
    expect(api.planAudienceStudy).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: /Write the script/ }));
    await waitFor(() => expect(api.planAudienceStudy).toHaveBeenCalledWith("a1", { kind: "user_interviews", goal: "Would they pay?", price_points: [9, 19], currency: "USD" }));
    expect(await screen.findByText("script step of study")).toBeInTheDocument();
  });

  it("asks for a page and an intent in a landing page test and sends them", async () => {
    api.planAudienceStudy.mockResolvedValue(study({ id: "new2" }));
    renderAt("/audience/studies/new?audience=a1");
    fireEvent.click(await screen.findByRole("radio", { name: /Landing page test/ }));
    const next = screen.getByRole("button", { name: /Next/ });
    expect(next).toBeDisabled();
    expect(screen.getByText("Enter the page's address.")).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("The page: Page address"), { target: { value: "acme.test" } });
    expect(screen.getByText("The address must start with http:// or https://.")).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("The page: Page address"), { target: { value: "https://acme.test/" } });
    fireEvent.click(screen.getByRole("button", { name: "Buy" }));
    expect(screen.getByLabelText("What does the visitor come to do?")).toHaveValue("Buy");
    fireEvent.click(next);
    expect(screen.getByRole("radio", { name: /Freelance designers/ })).toHaveAttribute("aria-checked", "true");
    fireEvent.click(await screen.findByRole("button", { name: /Write the script/ }));
    await waitFor(() => expect(api.planAudienceStudy).toHaveBeenCalledWith("a1", { kind: "landing_page_test", goal: "", inputs: { intent: "Buy", pages: [{ url: "https://acme.test/" }] } }));
  });

  it("takes pasted text instead of an address, and two pages for an A/B test", async () => {
    renderAt();
    fireEvent.click(await screen.findByRole("radio", { name: /A\/B test/ }));
    expect(screen.getByText("Enter the address of page A.")).toBeInTheDocument();
    const modes = screen.getAllByRole("button", { name: "Paste the text" });
    fireEvent.click(modes[0]!);
    fireEvent.change(screen.getByLabelText("Page A: Paste the text"), { target: { value: "Invoices that chase themselves" } });
    expect(screen.getByText("Enter the address of page B.")).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Page B: Page address"), { target: { value: "https://b.test" } });
    expect(screen.getByRole("button", { name: /Next/ })).toBeEnabled();
  });

  it("needs two different lines for message testing and at most five", async () => {
    renderAt();
    fireEvent.click(await screen.findByRole("radio", { name: /Message testing/ }));
    expect(screen.getByRole("button", { name: /Next/ })).toBeDisabled();
    fireEvent.change(screen.getByLabelText("Line 1"), { target: { value: "Boards for agents" } });
    fireEvent.change(screen.getByLabelText("Line 2"), { target: { value: "boards for agents" } });
    expect(screen.getByText("Add at least two different lines.")).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Line 2"), { target: { value: "Ship faster" } });
    expect(screen.getByRole("button", { name: /Next/ })).toBeEnabled();
    for (let i = 0; i < 3; i++) fireEvent.click(screen.getByRole("button", { name: "Add a line" }));
    expect(screen.queryByRole("button", { name: "Add a line" })).not.toBeInTheDocument();
    expect(screen.getByLabelText("Line 5")).toBeInTheDocument();
  });

  it("says why a study could not be planned and keeps the person on the step", async () => {
    api.planAudienceStudy.mockRejectedValue(new Error("page 1: that URL could not be read; paste the page text instead"));
    renderAt("/audience/studies/new?audience=a1");
    fireEvent.click(await screen.findByRole("radio", { name: /Landing page test/ }));
    fireEvent.change(screen.getByLabelText("The page: Page address"), { target: { value: "https://nope.test" } });
    fireEvent.click(screen.getByRole("button", { name: /Next/ }));
    fireEvent.click(await screen.findByRole("button", { name: /Write the script/ }));
    expect(await screen.findByText("The study could not be planned")).toBeInTheDocument();
    expect(screen.getByText(/that URL could not be read/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Write the script/ })).toBeEnabled();
  });

  it("leaves through Exit without planning anything", async () => {
    renderAt();
    fireEvent.click(await screen.findByRole("link", { name: /Exit/ }));
    expect(await screen.findByText("audience home")).toBeInTheDocument();
    expect(api.planAudienceStudy).not.toHaveBeenCalled();
  });

  it("hands free text to the Product Manager with the question already sent", async () => {
    api.createSession.mockResolvedValue({ id: "sess9" });
    renderAt("/audience/studies/new", { id: "pm1", name: "Product Manager", enabled: true });
    const box = await screen.findByPlaceholderText("Ask anything, or pick any study type below");
    expect(screen.getByRole("button", { name: "Ask the Product Manager" })).toBeDisabled();
    fireEvent.change(box, { target: { value: "Would a freelancer pay 19 USD?" } });
    fireEvent.click(screen.getByRole("button", { name: "Ask the Product Manager" }));
    const chat = await screen.findByTestId("chat");
    expect(chat).toHaveTextContent("/agents/pm1/chat/sess9");
    expect(chat).toHaveTextContent("Would a freelancer pay 19 USD?");
  });

  it("offers to create an audience when none is ready, through the Product Manager", async () => {
    api.audiences.mockResolvedValue({ audiences: [] });
    api.createSession.mockResolvedValue({ id: "sess3" });
    renderAt("/audience/studies/new", { id: "pm1", name: "Product Manager", enabled: true });
    fireEvent.click(await screen.findByRole("radio", { name: /User interviews/ }));
    fireEvent.click(screen.getByRole("button", { name: /Next/ }));
    expect(await screen.findByText("No ready audience yet")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Create a new audience" }));
    expect(await screen.findByTestId("chat")).toHaveTextContent("/agents/pm1/chat/sess3");
  });
});
