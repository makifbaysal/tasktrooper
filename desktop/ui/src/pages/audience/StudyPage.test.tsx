import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@/hooks/useI18n";
import { studyDetail } from "@/test/audienceFixtures";

const api = vi.hoisted(() => ({
  audienceStudy: vi.fn(),
  startAudienceStudy: vi.fn(),
  cancelAudienceStudy: vi.fn(),
  deleteAudienceStudy: vi.fn(),
  askAudienceResearch: vi.fn(),
  askAudienceEveryone: vi.fn(),
  followUpAudiencePersona: vi.fn(),
  audienceThread: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, ...api } };
});
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn(), info: vi.fn() } }));

import StudyPage from "./StudyPage";

function renderStudy() {
  return render(
    <I18nProvider>
      <MemoryRouter initialEntries={["/audience/studies/s1"]}>
        <Routes>
          <Route path="/audience/studies/:studyId" element={<StudyPage />} />
          <Route path="/audience/:audienceId" element={<div>audience page</div>} />
        </Routes>
      </MemoryRouter>
    </I18nProvider>,
  );
}

beforeEach(() => {
  for (const f of Object.values(api)) f.mockReset();
  window.HTMLElement.prototype.scrollIntoView = vi.fn();
});

describe("Study page", () => {
  it("renders the finished report with the answers and follow-ups under it", async () => {
    api.audienceStudy.mockResolvedValue(studyDetail());
    renderStudy();
    expect(await screen.findByText("Interest is lukewarm and price-bound.")).toBeInTheDocument();
    expect(screen.getByText("What each person said")).toBeInTheDocument();
    expect(screen.getByText("Follow-up questions")).toBeInTheDocument();
    expect(screen.getByText("One thing missing?")).toBeInTheDocument();
    expect(screen.getByText("1 of 2 answered")).toBeInTheDocument();
  });

  it("shows progress while the people are interviewed and offers to cancel", async () => {
    api.audienceStudy.mockResolvedValue(studyDetail({ status: "running", report: undefined, persona_done: 1, persona_failed: 0, persona_total: 4, answers: [], follow_ups: [] }));
    renderStudy();
    expect(await screen.findByText("Interviewing the people")).toBeInTheDocument();
    expect(screen.getAllByText("1 of 4 interviews done").length).toBeGreaterThan(0);
    expect(screen.getByRole("progressbar")).toHaveAttribute("aria-valuenow", "25");
    expect(screen.getByRole("button", { name: "Cancel" })).toBeInTheDocument();
    expect(screen.queryByText("Follow-up questions")).not.toBeInTheDocument();
  });

  it("shows the plan of a study that has not run and starts it after confirming the calls", async () => {
    api.audienceStudy.mockResolvedValue(studyDetail({ status: "draft", report: undefined, answers: [], follow_ups: [], questions: ["What would convince you?"], calls_estimate: 3, persona_total: 2 }));
    api.startAudienceStudy.mockResolvedValue({});
    renderStudy();
    expect(await screen.findByText(/Not run yet\. Running it asks 2 people/)).toBeInTheDocument();
    expect(screen.getByText("What would convince you?")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Run" }));
    expect(await screen.findByText("Run this study?")).toBeInTheDocument();
    expect(screen.getByText(/Every one of the 2 people is asked the same questions/)).toBeInTheDocument();
    expect(api.startAudienceStudy).not.toHaveBeenCalled();
    const run = screen.getAllByRole("button", { name: "Run" });
    fireEvent.click(run[run.length - 1]!);
    await waitFor(() => expect(api.startAudienceStudy).toHaveBeenCalledWith("s1"));
  });

  it("explains a failed study and keeps what was answered", async () => {
    api.audienceStudy.mockResolvedValue(studyDetail({ status: "failed", report: undefined, error: "No one answered.", persona_done: 0 }));
    renderStudy();
    expect(await screen.findByText("The study failed")).toBeInTheDocument();
    expect(screen.getByText("No one answered.")).toBeInTheDocument();
  });

  it("opens a person's answers and a follow-up conversation tied to the study", async () => {
    api.audienceStudy.mockResolvedValue(studyDetail());
    api.audienceThread.mockResolvedValue({ messages: [] });
    renderStudy();
    const answers = (await screen.findByText("What each person said")).closest("section")!;
    fireEvent.click(within(answers).getByRole("button", { name: /Ayse Demir/ }));
    expect(within(answers).getAllByText("too pricey for me").length).toBeGreaterThan(0);
    expect(within(answers).getByText("$9: would not buy")).toBeInTheDocument();
    expect(within(answers).getByText("No answer")).toBeInTheDocument();
    fireEvent.click(within(answers).getAllByRole("button", { name: "Talk" })[0]!);
    expect(await screen.findByText("Talk to Ayse Demir")).toBeInTheDocument();
  });

  it("asks everyone only after confirming how many people it goes to", async () => {
    api.audienceStudy.mockResolvedValue(studyDetail());
    api.askAudienceEveryone.mockResolvedValue({ group_id: "g2", calls: 1 });
    renderStudy();
    const box = await screen.findByLabelText("Ask everyone");
    fireEvent.change(box, { target: { value: "What would make you buy?" } });
    fireEvent.click(screen.getAllByRole("button", { name: "Ask everyone" })[0]!);
    expect(await screen.findByText(/goes to 1 people: 1 model calls/)).toBeInTheDocument();
    expect(api.askAudienceEveryone).not.toHaveBeenCalled();
    const confirm = screen.getAllByRole("button", { name: "Ask everyone" });
    fireEvent.click(confirm[confirm.length - 1]!);
    await waitFor(() => expect(api.askAudienceEveryone).toHaveBeenCalledWith("s1", "What would make you buy?"));
  });

  it("answers a question about the findings from the report", async () => {
    api.audienceStudy.mockResolvedValue(studyDetail());
    api.askAudienceResearch.mockResolvedValue({ answer: "Ayse objected most." });
    renderStudy();
    fireEvent.change(await screen.findByLabelText("Ask about the findings"), { target: { value: "Who objected?" } });
    fireEvent.click(screen.getByRole("button", { name: "Ask" }));
    expect(await screen.findByText("Ayse objected most.")).toBeInTheDocument();
    expect(api.askAudienceResearch).toHaveBeenCalledWith("s1", "Who objected?");
  });
});
