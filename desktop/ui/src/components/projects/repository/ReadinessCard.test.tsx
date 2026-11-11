import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ReadinessStep, RepositoryReadiness } from "@/api";
import { ReadinessCard } from "@/components/projects/repository/ReadinessCard";
import { I18nProvider } from "@/hooks/useI18n";

const { getRepositoryReadiness, runReadinessStep, runReadinessAll, setReadinessStepSkipped, setReadinessDismissed, toast } = vi.hoisted(() => ({
  getRepositoryReadiness: vi.fn(),
  runReadinessStep: vi.fn(),
  runReadinessAll: vi.fn(),
  setReadinessStepSkipped: vi.fn(),
  setReadinessDismissed: vi.fn(),
  toast: { success: vi.fn(), error: vi.fn() },
}));

vi.mock("sonner", () => ({ toast }));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    api: {
      ...actual.api,
      getRepositoryReadiness,
      runReadinessStep,
      runReadinessAll,
      setReadinessStepSkipped,
      setReadinessDismissed,
    },
  };
});

function step(over: Partial<ReadinessStep> & Pick<ReadinessStep, "id">): ReadinessStep {
  return { status: "todo", action: "task", depends_on: ["scan"], ...over };
}

function checklist(steps: ReadinessStep[], over: Partial<RepositoryReadiness> = {}): RepositoryReadiness {
  const done = steps.filter((s) => s.status === "done").length;
  const skipped = steps.filter((s) => s.status === "skipped").length;
  const planned = steps.filter((s) => s.status === "todo" && (s.action === "task" || s.action === "run"));
  return {
    repository_id: "repo-1",
    steps,
    done,
    skipped,
    total: steps.length,
    complete: done + skipped === steps.length,
    dismissed: false,
    run_all: {
      tasks: planned.filter((s) => s.action === "task").length,
      runs: planned.filter((s) => s.action === "run").length,
      steps: planned.map((s) => s.id),
    },
    ...over,
  };
}

const fresh = checklist([
  step({ id: "scan", status: "done", action: "run", depends_on: [], facts: { components: 2 } }),
  step({ id: "index", action: "run", depends_on: [], reason: "not_indexed" }),
  step({ id: "guide", reason: "missing", facts: { missing: ["readme", "agent_guide"] } }),
  step({ id: "tests", reason: "gaps", facts: { count: 1, components: ["api"] } }),
  step({ id: "delivery", action: "navigate", reason: "gaps", facts: { components: ["api"] } }),
  step({ id: "observability", action: "navigate", status: "blocked", reason: "needs_delivery", depends_on: ["delivery"] }),
]);

function Where() {
  const loc = useLocation();
  return <div data-testid="where">{loc.pathname + loc.search}</div>;
}

function renderCard(props: { expandOnArrival?: boolean; onOpenTab?: (t: "deploy" | "findings" | "design") => void } = {}) {
  return render(
    <I18nProvider>
      <MemoryRouter initialEntries={["/repositories/repo-1"]}>
        <Routes>
          <Route
            path="/repositories/:id"
            element={<ReadinessCard repositoryId="repo-1" onOpenTab={props.onOpenTab ?? (() => {})} expandOnArrival={props.expandOnArrival} />}
          />
          <Route path="*" element={<Where />} />
        </Routes>
      </MemoryRouter>
    </I18nProvider>,
  );
}

describe("ReadinessCard", () => {
  beforeEach(() => {
    window.localStorage.clear();
    getRepositoryReadiness.mockReset().mockResolvedValue(fresh);
    runReadinessStep.mockReset().mockResolvedValue({ step: "guide", task: { id: "t1", key: "TT-5", title: "x", column: "todo" }, started: false });
    runReadinessAll.mockReset().mockResolvedValue({ results: [] });
    setReadinessStepSkipped.mockReset();
    setReadinessDismissed.mockReset();
    toast.success.mockReset();
    toast.error.mockReset();
  });

  it("lists the steps in order with progress, why, and what it found", async () => {
    renderCard({ expandOnArrival: true });

    expect(await screen.findByText("Get this project ready")).toBeInTheDocument();
    expect(screen.getByText("1 of 6 done")).toBeInTheDocument();
    const items = screen.getAllByRole("listitem");
    expect(items.map((li) => li.getAttribute("data-status"))).toEqual(["done", "todo", "todo", "todo", "todo", "blocked"]);
    expect(within(items[2]).getByText(/Add a README and an agent guide/)).toBeInTheDocument();
    expect(within(items[2]).getByText("Missing: README, agent guide.")).toBeInTheDocument();
    expect(within(items[3]).getByText("No test command for: api.")).toBeInTheDocument();
    expect(within(items[5]).getByText("Waiting for: Choose how changes ship.")).toBeInTheDocument();
    expect(within(items[5]).queryByRole("button", { name: "Open" })).not.toBeInTheDocument();
  });

  it("opens a task for one step on its own click and refreshes", async () => {
    renderCard({ expandOnArrival: true });
    const guide = (await screen.findAllByRole("listitem"))[2];

    fireEvent.click(within(guide).getByRole("button", { name: "Do it" }));

    await waitFor(() => expect(runReadinessStep).toHaveBeenCalledWith("repo-1", "guide", true));
    await waitFor(() => expect(toast.success).toHaveBeenCalledWith("Task TT-5 opened on the board."));
    expect(getRepositoryReadiness.mock.calls.length).toBeGreaterThan(1);
  });

  it("shows the task a step opened and links to it", async () => {
    getRepositoryReadiness.mockResolvedValue(
      checklist([
        step({ id: "scan", status: "done", action: "run", depends_on: [] }),
        step({ id: "tests", status: "running", task: { id: "t9", key: "TT-9", title: "Add automated tests", column: "in_progress" } }),
      ]),
    );
    renderCard({ expandOnArrival: true });

    expect(await screen.findByText("Task TT-9 is in progress.")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "View TT-9" }));
    expect(await screen.findByTestId("where")).toHaveTextContent("/board?task=t9");
  });

  it("states the cost before Run all and sends the count it showed", async () => {
    renderCard({ expandOnArrival: true });
    await screen.findByText("Get this project ready");

    fireEvent.click(screen.getByRole("button", { name: "Run all in order" }));

    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText(/2 tasks will be opened on the board/)).toBeInTheDocument();
    expect(within(dialog).getByText(/1 will start right away/)).toBeInTheDocument();
    expect(runReadinessAll).not.toHaveBeenCalled();

    fireEvent.click(within(dialog).getByRole("button", { name: "Run all" }));
    await waitFor(() => expect(runReadinessAll).toHaveBeenCalledWith("repo-1", 2));
  });

  it("offers no Run all when nothing can be started for you", async () => {
    getRepositoryReadiness.mockResolvedValue(
      checklist([
        step({ id: "scan", status: "done", action: "run", depends_on: [] }),
        step({ id: "delivery", action: "navigate", reason: "gaps", facts: { components: ["api"] } }),
      ]),
    );
    renderCard({ expandOnArrival: true });
    await screen.findByText("Get this project ready");
    expect(screen.queryByRole("button", { name: "Run all in order" })).not.toBeInTheDocument();
  });

  it("sends a navigate step to the tab that asks for what it needs", async () => {
    const onOpenTab = vi.fn();
    renderCard({ expandOnArrival: true, onOpenTab });
    const delivery = (await screen.findAllByRole("listitem"))[4];

    fireEvent.click(within(delivery).getByRole("button", { name: "Open" }));

    expect(onOpenTab).toHaveBeenCalledWith("deploy");
    expect(runReadinessStep).not.toHaveBeenCalled();
  });

  it("skips a step and takes the server's recount", async () => {
    setReadinessStepSkipped.mockResolvedValue(
      checklist(fresh.steps.map((s) => (s.id === "guide" ? { ...s, status: "skipped" as const } : s))),
    );
    renderCard({ expandOnArrival: true });
    const guide = (await screen.findAllByRole("listitem"))[2];

    fireEvent.click(within(guide).getByRole("button", { name: "Skip" }));

    await waitFor(() => expect(setReadinessStepSkipped).toHaveBeenCalledWith("repo-1", "guide", true));
    expect(await screen.findByText(/1 skipped/)).toBeInTheDocument();
    expect(within(screen.getAllByRole("listitem")[2]).getByRole("button", { name: "Undo skip" })).toBeInTheDocument();
  });

  it("hides the card to a one-line row that brings it back", async () => {
    setReadinessDismissed.mockImplementation(async (_id: string, dismissed: boolean) => ({ ...fresh, dismissed }));
    renderCard({ expandOnArrival: true });
    await screen.findByText("Get this project ready");

    fireEvent.click(screen.getByRole("button", { name: "Hide" }));

    expect(await screen.findByRole("button", { name: "Show checklist" })).toBeInTheDocument();
    expect(screen.queryByRole("listitem")).not.toBeInTheDocument();
    expect(screen.getByText(/1 of 6 done/)).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Show checklist" }));
    expect(await screen.findAllByRole("listitem")).toHaveLength(6);
  });

  it("says when everything is done and still lets the person hide it", async () => {
    getRepositoryReadiness.mockResolvedValue(
      checklist([step({ id: "scan", status: "done", action: "run", depends_on: [] }), step({ id: "guide", status: "skipped" })]),
    );
    renderCard();
    expect(await screen.findByText("This project is ready.")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Run all in order" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Hide" })).toBeInTheDocument();
  });

  it("keeps a folded card folded unless the page was reached right after an import", async () => {
    window.localStorage.setItem("tt.readiness.folded.repo-1", "1");
    const { unmount } = renderCard();
    await screen.findByText("Get this project ready");
    expect(screen.queryByRole("listitem")).not.toBeInTheDocument();
    unmount();

    renderCard({ expandOnArrival: true });
    expect(await screen.findAllByRole("listitem")).toHaveLength(6);
  });

  it("renders nothing when the repository has no checklist", async () => {
    getRepositoryReadiness.mockResolvedValue(checklist([]));
    const { container } = renderCard();
    await waitFor(() => expect(getRepositoryReadiness).toHaveBeenCalled());
    await waitFor(() => expect(container.querySelector('[data-testid="readiness-card"]')).toBeNull());
  });

  it("reports a refused step and reloads the checklist", async () => {
    runReadinessStep.mockRejectedValue(new Error("the checklist changed since it was shown"));
    renderCard({ expandOnArrival: true });
    const guide = (await screen.findAllByRole("listitem"))[2];

    fireEvent.click(within(guide).getByRole("button", { name: "Do it" }));

    await waitFor(() => expect(toast.error).toHaveBeenCalledWith("the checklist changed since it was shown"));
  });
});
