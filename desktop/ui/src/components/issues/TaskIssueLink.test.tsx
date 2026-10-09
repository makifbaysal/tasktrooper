import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError, type IssueImport, type IssueLink } from "@/api";
import { TaskIssueLink } from "@/components/issues/TaskIssueLink";
import { I18nProvider } from "@/hooks/useI18n";

const { getTaskIssueLink, convertIssueImport, getSession } = vi.hoisted(() => ({
  getTaskIssueLink: vi.fn(),
  convertIssueImport: vi.fn(),
  getSession: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, getTaskIssueLink, convertIssueImport, getSession } };
});

const { toastError, toastSuccess } = vi.hoisted(() => ({ toastError: vi.fn(), toastSuccess: vi.fn() }));
vi.mock("sonner", () => ({ toast: { success: toastSuccess, error: toastError } }));

const LINK: IssueLink = {
  provider: "github",
  key: "acme/web#12",
  url: "https://github.com/acme/web/issues/12",
  title: "Login button does nothing",
  created_at: "2026-02-01T00:00:00Z",
  closed_at: null,
};

function importRecord(overrides: Partial<IssueImport> = {}): IssueImport {
  return {
    id: "import-1",
    provider: "github",
    key: "acme/web#12",
    repository_id: "repo-1",
    url: "https://github.com/acme/web/issues/12",
    title: "Login button does nothing",
    intake_task_id: null,
    conversion_status: "converted",
    conversion_session_id: "session-1",
    closed_at: null,
    ...overrides,
  };
}

function renderLink() {
  return render(
    <I18nProvider>
      <MemoryRouter initialEntries={["/board"]}>
        <Routes>
          <Route path="/board" element={<TaskIssueLink repositoryId="repo-1" taskId="task-1" />} />
          <Route path="/agents/:agentId/chat/:sessionId" element={<p>chat page</p>} />
        </Routes>
      </MemoryRouter>
    </I18nProvider>,
  );
}

describe("TaskIssueLink", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("shows the provider and the issue key, linked to the issue", async () => {
    getTaskIssueLink.mockResolvedValue({ link: LINK, import: null });
    renderLink();

    const link = await screen.findByRole("link", { name: "GitHub acme/web#12" });
    expect(link).toHaveAttribute("href", "https://github.com/acme/web/issues/12");
    expect(getTaskIssueLink).toHaveBeenCalledWith("repo-1", "task-1");
  });

  it("marks an issue TaskTrooper already closed", async () => {
    getTaskIssueLink.mockResolvedValue({ link: { ...LINK, closed_at: "2026-03-01T00:00:00Z" }, import: null });
    renderLink();

    expect(await screen.findByText("Closed")).toBeInTheDocument();
  });

  it("renders nothing for a task that came from no issue", async () => {
    getTaskIssueLink.mockResolvedValue({ link: null, import: null });
    const { container } = renderLink();

    await waitFor(() => expect(getTaskIssueLink).toHaveBeenCalled());
    expect(screen.queryByRole("link")).not.toBeInTheDocument();
    expect(container).toBeEmptyDOMElement();
  });

  it("renders nothing when the link cannot be read", async () => {
    getTaskIssueLink.mockRejectedValue(new Error("boom"));
    const { container } = renderLink();

    await waitFor(() => expect(getTaskIssueLink).toHaveBeenCalled());
    expect(container).toBeEmptyDOMElement();
  });

  it("opens the product manager's conversion chat for a task the conversion produced", async () => {
    getTaskIssueLink.mockResolvedValue({ link: LINK, import: importRecord() });
    getSession.mockResolvedValue({
      session: { id: "session-1", agent_id: "agent-pm", title: "", model: "", created_at: "", updated_at: "" },
      messages: [],
    });
    renderLink();

    expect(await screen.findByText("Turned into tasks")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Turn into tasks with the product manager" })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Open the product manager's chat" }));

    expect(await screen.findByText("chat page")).toBeInTheDocument();
    expect(getSession).toHaveBeenCalledWith("session-1");
  });

  it("offers the conversion again for an imported task the product manager could not convert", async () => {
    getTaskIssueLink.mockResolvedValue({
      link: LINK,
      import: importRecord({
        intake_task_id: "task-1",
        conversion_status: "failed",
        conversion_session_id: null,
        conversion_error: "the product manager opened no task",
      }),
    });
    convertIssueImport.mockResolvedValue({
      import: importRecord({ intake_task_id: "task-1", conversion_status: "pending", conversion_session_id: null }),
    });
    renderLink();

    expect(await screen.findByText("the product manager opened no task")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Turn into tasks with the product manager" }));

    await waitFor(() => expect(convertIssueImport).toHaveBeenCalledWith("import-1"));
    expect(toastSuccess).toHaveBeenCalledWith("The product manager will turn this issue into tasks.");
    expect(await screen.findByText("Being turned into tasks")).toBeInTheDocument();
  });

  it("explains a conversion nobody can run", async () => {
    getTaskIssueLink.mockResolvedValue({
      link: LINK,
      import: importRecord({ intake_task_id: "task-1", conversion_status: "skipped", conversion_session_id: null }),
    });
    convertIssueImport.mockRejectedValue(new ApiError("no pm", 409, "issue_conversion_unavailable"));
    renderLink();

    fireEvent.click(await screen.findByRole("button", { name: "Turn into tasks with the product manager" }));

    await waitFor(() =>
      expect(toastError).toHaveBeenCalledWith(
        "Nobody can turn this issue into tasks right now. Check that an enabled agent holds the product manager role.",
      ),
    );
    expect(screen.getByRole("link", { name: "GitHub acme/web#12" })).toBeInTheDocument();
  });
});
