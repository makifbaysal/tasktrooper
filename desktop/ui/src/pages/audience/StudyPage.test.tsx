import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@/hooks/useI18n";
import { studyDetail } from "@/test/audienceFixtures";

const api = vi.hoisted(() => ({
  audienceStudy: vi.fn(),
  startAudienceStudy: vi.fn(),
  updateAudienceStudy: vi.fn(),
  generateAudienceStudyScript: vi.fn(),
  approveAudienceStudyScript: vi.fn(),
  generateAudienceStudyReport: vi.fn(),
  retryAudienceStudy: vi.fn(),
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
    expect(screen.getByRole("button", { name: "Export as HTML" })).toBeInTheDocument();
  });

  it("shows the interviews as they run, with who has answered and who is still being asked", async () => {
    api.audienceStudy.mockResolvedValue(
      studyDetail({
        status: "running",
        report: undefined,
        persona_done: 1,
        persona_total: 3,
        script: { sections: [{ title: "Today", questions: ["How do you invoice?"] }] },
        script_state: "awaiting_approval",
        answers: [
          {
            id: "x1",
            persona_id: "p1",
            persona: { name: "Şule İpek", segment: "Freelancers", role: "UX designer", ordinal: 1 },
            status: "ok",
            answers: { intent_score: 6, verdict: "maybe", would_pay_now: false, reasoning: "depends on price", answers: [{ question: "How do you invoice?", answer: "In a spreadsheet." }] },
          },
        ],
        pending: [
          { persona_id: "p2", ordinal: 2, name: "Can Yılmaz", segment: "Agency leads", role: "Agency lead" },
          { persona_id: "p3", ordinal: 3, name: "Ayşe Yıldız", segment: "Freelancers", role: "Developer" },
        ],
        follow_ups: [],
      }),
    );
    renderStudy();
    expect(await screen.findByText("Interviews running - 1/3 interviews complete")).toBeInTheDocument();
    expect(screen.getByText("1/3 completed")).toBeInTheDocument();
    const cards = screen.getAllByTestId("interview-card");
    expect(cards).toHaveLength(3);
    expect(within(cards[0]!).getByText("Şule İpek")).toBeInTheDocument();
    expect(within(cards[0]!).getByText("In a spreadsheet.")).toBeInTheDocument();
    expect(within(cards[0]!).getByText("Done")).toBeInTheDocument();
    expect(within(cards[1]!).getByText("Can Yılmaz")).toBeInTheDocument();
    expect(within(cards[1]!).getByText("In progress")).toBeInTheDocument();
    expect(within(cards[1]!).getByText("Preparing the interview, this will take a few moments...")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Generate report/ })).toBeDisabled();
    expect(screen.getAllByRole("progressbar").some((p) => p.getAttribute("aria-valuenow") === "33")).toBe(true);
    expect(screen.getByRole("button", { name: "Cancel" })).toBeInTheDocument();
    expect(screen.queryByText("Follow-up questions")).not.toBeInTheDocument();
  });

  it("filters the interview cards by segment", async () => {
    api.audienceStudy.mockResolvedValue(
      studyDetail({
        status: "running",
        report: undefined,
        persona_total: 2,
        answers: [],
        pending: [
          { persona_id: "p2", ordinal: 1, name: "Can Yılmaz", segment: "Agency leads" },
          { persona_id: "p3", ordinal: 2, name: "Ayşe Yıldız", segment: "Freelancers" },
        ],
        follow_ups: [],
      }),
    );
    renderStudy();
    await screen.findAllByTestId("interview-card");
    fireEvent.click(screen.getByRole("button", { name: "Freelancers" }));
    const cards = screen.getAllByTestId("interview-card");
    expect(cards).toHaveLength(1);
    expect(within(cards[0]!).getByText("Ayşe Yıldız")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Freelancers" })).toHaveAttribute("aria-pressed", "true");
    fireEvent.click(screen.getByRole("button", { name: "All roles" }));
    expect(screen.getAllByTestId("interview-card")).toHaveLength(2);
  });

  it("holds the report until it is asked for, and writes it from the completed interviews", async () => {
    const ok = (id: string, name: string) => ({ id, persona_id: id, persona: { name, segment: "Freelancers", ordinal: 1 }, status: "ok" as const, answers: { intent_score: 5, verdict: "maybe" as const, would_pay_now: false } });
    api.audienceStudy.mockResolvedValue(
      studyDetail({
        status: "interviewed",
        report: undefined,
        persona_total: 3,
        persona_done: 2,
        persona_failed: 1,
        answers: [ok("a", "Ayse"), ok("b", "Can"), { id: "c", persona_id: "c", persona: { name: "Deniz", segment: "Freelancers", ordinal: 3 }, status: "failed", error: "no answer", answers: { intent_score: 0, verdict: "no", would_pay_now: false } }],
        follow_ups: [],
      }),
    );
    api.generateAudienceStudyReport.mockResolvedValue({});
    api.retryAudienceStudy.mockResolvedValue({});
    renderStudy();
    expect(await screen.findByText("1 interviews failed. 2 of 3 are complete.")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Retry 1 failed" }));
    await waitFor(() => expect(api.retryAudienceStudy).toHaveBeenCalledWith("s1"));
    expect(api.generateAudienceStudyReport).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: /Generate with the 2 completed/ }));
    await waitFor(() => expect(api.generateAudienceStudyReport).toHaveBeenCalledWith("s1"));
  });

  it("enables Generate report once every interview is complete", async () => {
    api.audienceStudy.mockResolvedValue(
      studyDetail({
        status: "interviewed",
        report: undefined,
        persona_total: 1,
        persona_done: 1,
        answers: [{ id: "a", persona_id: "a", persona: { name: "Ayse", ordinal: 1 }, status: "ok", answers: { intent_score: 5, verdict: "maybe", would_pay_now: false } }],
        follow_ups: [],
      }),
    );
    renderStudy();
    expect(await screen.findByText("All 1 interviews are complete.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Generate report/ })).toBeEnabled();
    expect(screen.queryByRole("button", { name: /Retry/ })).not.toBeInTheDocument();
  });

  describe("the script step of a study that has not run", () => {
    const script = { sections: [{ title: "Today", questions: ["How do you invoice?", "What went wrong?"] }, { title: "Decision", questions: ["What would make you switch?"] }] };
    const draft = (over = {}) =>
      studyDetail({ status: "draft", report: undefined, answers: [], follow_ups: [], pending: [], script, script_state: "awaiting_approval", script_version: 2, calls_estimate: 4, persona_total: 2, ...over });

    it("shows the written script, what is always asked, and executes with the call count beside the button", async () => {
      api.audienceStudy.mockResolvedValue(draft());
      api.startAudienceStudy.mockResolvedValue({});
      renderStudy();
      expect(await screen.findByDisplayValue("How do you invoice?")).toBeInTheDocument();
      expect(screen.getByText("Always asked")).toBeInTheDocument();
      expect(screen.getByDisplayValue("Decision")).toBeInTheDocument();
      expect(screen.getByText(/3 questions\. Executing asks every person and writes the report: 4 model calls in all\./)).toBeInTheDocument();
      expect(api.startAudienceStudy).not.toHaveBeenCalled();
      expect(screen.getByText(/Nothing runs until you approve this script/)).toBeInTheDocument();
      fireEvent.click(screen.getByRole("button", { name: /Approve script & run interviews/ }));
      await waitFor(() => expect(api.startAudienceStudy).toHaveBeenCalledWith("s1", { manualReport: true }));
      expect(api.approveAudienceStudyScript).toHaveBeenCalledWith("s1", 2);
      expect(api.approveAudienceStudyScript.mock.invocationCallOrder[0]!).toBeLessThan(api.startAudienceStudy.mock.invocationCallOrder[0]!);
      expect(api.updateAudienceStudy).not.toHaveBeenCalled();
    });

    it("keeps an edit: it is saved before the study runs", async () => {
      api.audienceStudy.mockResolvedValue(draft());
      api.updateAudienceStudy.mockImplementation(async (_id: string, data: { script: unknown }) => ({ script: data.script, script_version: 3 }));
      api.approveAudienceStudyScript.mockResolvedValue({});
      api.startAudienceStudy.mockResolvedValue({});
      renderStudy();
      fireEvent.change(await screen.findByDisplayValue("What went wrong?"), { target: { value: " What is your budget? " } });
      fireEvent.click(screen.getAllByRole("button", { name: "Remove question" })[0]!);
      fireEvent.click(screen.getByRole("button", { name: /Approve script & run interviews/ }));
      await waitFor(() => expect(api.startAudienceStudy).toHaveBeenCalled());
      const saved = api.updateAudienceStudy.mock.calls[0]![1].script;
      expect(saved.sections[0].questions).toEqual(["What is your budget?"]);
      expect(api.approveAudienceStudyScript).toHaveBeenCalledWith("s1", 3);
      expect(api.updateAudienceStudy.mock.invocationCallOrder[0]!).toBeLessThan(api.approveAudienceStudyScript.mock.invocationCallOrder[0]!);
      expect(api.approveAudienceStudyScript.mock.invocationCallOrder[0]!).toBeLessThan(api.startAudienceStudy.mock.invocationCallOrder[0]!);
    });

    it("saves an edit on its own with the Save button", async () => {
      api.audienceStudy.mockResolvedValue(draft());
      api.updateAudienceStudy.mockImplementation(async (_id: string, data: { script: unknown }) => ({ script: data.script }));
      renderStudy();
      fireEvent.change(await screen.findByDisplayValue("Decision"), { target: { value: "Switching" } });
      fireEvent.click(screen.getByRole("button", { name: "Save" }));
      await waitFor(() => expect(api.updateAudienceStudy).toHaveBeenCalled());
      expect(api.updateAudienceStudy.mock.calls[0]![1].script.sections[1].title).toBe("Switching");
      expect(api.startAudienceStudy).not.toHaveBeenCalled();
    });

    it("runs a script that was already approved without asking again, and shows who approved it", async () => {
      api.audienceStudy.mockResolvedValue(draft({ script_state: "approved", script_approved_version: 2, script_approved_by: "akif", script_approved_at: "2026-11-11T10:00:00Z" }));
      api.startAudienceStudy.mockResolvedValue({});
      renderStudy();
      expect(await screen.findByText(/Approved by akif on/)).toBeInTheDocument();
      fireEvent.click(screen.getByRole("button", { name: "Run interviews" }));
      await waitFor(() => expect(api.startAudienceStudy).toHaveBeenCalledWith("s1", { manualReport: true }));
      expect(api.approveAudienceStudyScript).not.toHaveBeenCalled();
    });

    it("asks for a new approval once an approved script is edited", async () => {
      api.audienceStudy.mockResolvedValue(draft({ script_state: "approved", script_approved_version: 2 }));
      renderStudy();
      fireEvent.change(await screen.findByDisplayValue("Decision"), { target: { value: "Switching" } });
      expect(screen.getByRole("button", { name: /Approve script & run interviews/ })).toBeEnabled();
      expect(screen.queryByRole("button", { name: "Run interviews" })).not.toBeInTheDocument();
      expect(screen.getByText(/Nothing runs until you approve this script/)).toBeInTheDocument();
    });

    it("does not start the interviews when the approval is refused because the script moved on", async () => {
      api.audienceStudy.mockResolvedValue(draft());
      const { ApiError } = await import("@/api");
      api.approveAudienceStudyScript.mockRejectedValue(new ApiError("the interview script changed since it was shown", 409));
      renderStudy();
      fireEvent.click(await screen.findByRole("button", { name: /Approve script & run interviews/ }));
      await waitFor(() => expect(api.approveAudienceStudyScript).toHaveBeenCalled());
      expect(api.startAudienceStudy).not.toHaveBeenCalled();
    });

    it("cannot execute while the script is being written, and says so", async () => {
      api.audienceStudy.mockResolvedValue(draft({ script: undefined, script_state: "generating" }));
      renderStudy();
      expect(await screen.findByText("Crafting your interview script")).toBeInTheDocument();
      expect(screen.getByText("Crafting interview script... Generating questions from your research goal")).toBeInTheDocument();
      expect(screen.getByRole("button", { name: /Approve script & run interviews/ })).toBeDisabled();
    });

    it("offers to write it again, or take the person's own questions, when it failed", async () => {
      api.audienceStudy.mockResolvedValue(draft({ script: undefined, script_state: "failed", error: "model did not answer" }));
      api.generateAudienceStudyScript.mockResolvedValue({});
      renderStudy();
      expect(await screen.findByText("The script could not be written")).toBeInTheDocument();
      expect(screen.getByText("model did not answer")).toBeInTheDocument();
      expect(screen.getByRole("button", { name: /Approve script & run interviews/ })).toBeDisabled();
      fireEvent.click(screen.getByRole("button", { name: "Regenerate" }));
      await waitFor(() => expect(api.generateAudienceStudyScript).toHaveBeenCalledWith("s1"));
    });

    it("reads an uploaded script file into questions and saves it", async () => {
      api.audienceStudy.mockResolvedValue(draft());
      api.updateAudienceStudy.mockImplementation(async (_id: string, data: { script: unknown }) => ({ script: data.script }));
      renderStudy();
      const input = await screen.findByTestId("script-upload");
      const file = new File(["x"], "script.md", { type: "text/markdown" });
      Object.defineProperty(file, "text", { value: async () => "# Mine\n- Nasıl yapıyorsun?\n- Kim karar veriyor?\n" });
      fireEvent.change(input, { target: { files: [file] } });
      await waitFor(() => expect(api.updateAudienceStudy).toHaveBeenCalled());
      expect(api.updateAudienceStudy.mock.calls[0]![1].script).toEqual({ sections: [{ title: "Mine", questions: ["Nasıl yapıyorsun?", "Kim karar veriyor?"] }] });
      expect(await screen.findByDisplayValue("Nasıl yapıyorsun?")).toBeInTheDocument();
    });
  });

  it("opens an earlier step of a finished study read-only from the stepper", async () => {
    api.audienceStudy.mockResolvedValue(studyDetail({ script: { sections: [{ title: "Today", questions: ["How do you invoice?"] }] }, script_state: "awaiting_approval" }));
    renderStudy();
    await screen.findByText("Interest is lukewarm and price-bound.");
    fireEvent.click(screen.getByRole("button", { name: "Script" }));
    expect(await screen.findByDisplayValue("How do you invoice?")).toBeDisabled();
    expect(screen.queryByRole("button", { name: /Approve script & run interviews/ })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Context" }));
    expect(await screen.findByText("User interviews")).toBeInTheDocument();
    expect(screen.getByRole("navigation", { name: "Study steps" })).toBeInTheDocument();
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
