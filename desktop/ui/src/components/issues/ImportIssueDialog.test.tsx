import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError, type BoardTask, type ExternalIssue, type IssueImport, type Repository } from "@/api";
import { ImportIssueDialog } from "@/components/issues/ImportIssueDialog";
import { I18nProvider } from "@/hooks/useI18n";

// jsdom has no layout, so Radix Select's scroll-into-view-on-open crashes without it.
if (!Element.prototype.scrollIntoView) {
  Element.prototype.scrollIntoView = () => {};
}

const { searchIssues, importIssue, listJiraProjects } = vi.hoisted(() => ({
  searchIssues: vi.fn(),
  importIssue: vi.fn(),
  listJiraProjects: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, searchIssues, importIssue, listJiraProjects } };
});

// No Toaster in the tree: asserting on the calls is the only way to see what a
// toast would have said.
const { toastSuccess, toastError } = vi.hoisted(() => ({ toastSuccess: vi.fn(), toastError: vi.fn() }));
vi.mock("sonner", () => ({ toast: { success: toastSuccess, error: toastError } }));

function repository(overrides: Partial<Repository> = {}): Repository {
  return {
    id: "repo-1",
    name: "web",
    description: "",
    root_path: "/repos/web",
    remote_url: "https://github.com/acme/web.git",
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

function issue(overrides: Partial<ExternalIssue> = {}): ExternalIssue {
  return {
    provider: "github",
    key: "acme/web#12",
    title: "Login button does nothing",
    url: "https://github.com/acme/web/issues/12",
    state: "open",
    labels: ["bug"],
    updated_at: "2026-02-01T00:00:00Z",
    imported_task: null,
    ...overrides,
  };
}

function task(overrides: Partial<BoardTask> = {}): BoardTask {
  return {
    id: "task-1",
    repository_id: "repo-1",
    key: "TT-7",
    task_number: 7,
    title: "Login button does nothing",
    task_type: "task",
    description: "",
    technical_description: "",
    column: "todo",
    position: 0,
    priority: "medium",
    created_by: "u1",
    created_at: "2026-02-01T00:00:00Z",
    updated_at: "2026-02-01T00:00:00Z",
    ...overrides,
  };
}

function importRecord(overrides: Partial<IssueImport> = {}): IssueImport {
  return {
    id: "import-1",
    provider: "github",
    key: "acme/web#12",
    repository_id: "repo-1",
    url: "https://github.com/acme/web/issues/12",
    title: "Login button does nothing",
    intake_task_id: "task-1",
    conversion_status: "",
    conversion_session_id: null,
    closed_at: null,
    ...overrides,
  };
}

function renderDialog(props: Partial<React.ComponentProps<typeof ImportIssueDialog>> = {}) {
  const onImported = vi.fn();
  const onOpenChange = vi.fn();
  const result = render(
    <I18nProvider>
      <MemoryRouter>
        <ImportIssueDialog
          open
          onOpenChange={onOpenChange}
          repositories={[repository()]}
          onImported={onImported}
          {...props}
        />
      </MemoryRouter>
    </I18nProvider>,
  );
  return { ...result, onImported, onOpenChange };
}

describe("ImportIssueDialog", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    listJiraProjects.mockResolvedValue({ projects: [] });
    searchIssues.mockResolvedValue({ issues: [issue()] });
    importIssue.mockResolvedValue({
      task: task(),
      link: {
        provider: "github",
        key: "acme/web#12",
        url: "https://github.com/acme/web/issues/12",
        title: "Login button does nothing",
        created_at: "2026-02-01T00:00:00Z",
        closed_at: null,
      },
      import: importRecord(),
    });
  });

  it("lists the repository's open GitHub issues", async () => {
    renderDialog();

    expect(await screen.findByText("acme/web#12")).toBeInTheDocument();
    expect(screen.getByText("Login button does nothing")).toBeInTheDocument();
    expect(searchIssues).toHaveBeenCalledWith({ provider: "github", repositoryId: "repo-1", q: undefined });
  });

  it("only offers repositories with a GitHub remote", async () => {
    renderDialog({
      repositories: [repository({ id: "repo-2", name: "infra", remote_url: "https://gitlab.com/acme/infra.git" })],
    });

    expect(await screen.findByText("Add a repository with a GitHub remote first — issues are imported into a repository.")).toBeInTheDocument();
    expect(searchIssues).not.toHaveBeenCalled();
  });

  it("shows the task key instead of Import for an issue that is already a task", async () => {
    searchIssues.mockResolvedValue({
      issues: [issue({ imported_task: { id: "task-9", key: "TT-9", repository_id: "repo-1" } })],
    });
    renderDialog();

    expect(await screen.findByText("Imported as TT-9")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Import" })).not.toBeInTheDocument();
  });

  it("imports the issue into the selected repository and hands back the task", async () => {
    const { onImported, onOpenChange } = renderDialog();

    fireEvent.click(await screen.findByRole("button", { name: "Import" }));

    await waitFor(() =>
      expect(importIssue).toHaveBeenCalledWith("github", "acme/web#12", "repo-1"),
    );
    expect(onImported).toHaveBeenCalledWith(task());
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it("passes the default repository to the import", async () => {
    const repos = [repository({ id: "repo-1" }), repository({ id: "repo-2", name: "api" })];
    renderDialog({ repositories: repos, defaultRepositoryId: "repo-2" });

    fireEvent.click(await screen.findByRole("button", { name: "Import" }));

    await waitFor(() => expect(importIssue).toHaveBeenCalledWith("github", "acme/web#12", "repo-2"));
  });

  it("says a not-connected source needs the integrations page, not a retry", async () => {
    searchIssues.mockRejectedValue(
      new ApiError("github is not connected", 400, "issue_source_not_configured"),
    );
    renderDialog();

    expect(
      await screen.findByText("GitHub is not connected. Connect it in Settings → Integrations."),
    ).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Retry" })).not.toBeInTheDocument();
  });

  it("offers a retry for a search that failed some other way", async () => {
    searchIssues.mockRejectedValue(new ApiError("github is down", 502, "issue_source_error"));
    renderDialog();

    expect(await screen.findByText("github is down")).toBeInTheDocument();
    const retry = screen.getByRole("button", { name: "Retry" });
    fireEvent.click(retry);
    await waitFor(() => expect(searchIssues).toHaveBeenCalledTimes(2));
  });

  it("reports an issue that is already imported without closing the dialog", async () => {
    importIssue.mockRejectedValue(
      new ApiError("already imported", 409, "issue_already_imported"),
    );
    const { onImported, onOpenChange } = renderDialog();

    fireEvent.click(await screen.findByRole("button", { name: "Import" }));

    await waitFor(() => expect(toastError).toHaveBeenCalledWith("That issue is already a task"));
    expect(onImported).not.toHaveBeenCalled();
    expect(onOpenChange).not.toHaveBeenCalled();
  });

  it("searches Jira once a project is picked", async () => {
    listJiraProjects.mockResolvedValue({ projects: [{ key: "ACME", name: "Acme" }] });
    renderDialog();

    fireEvent.click(await screen.findByRole("tab", { name: "Jira" }));
    fireEvent.click(await screen.findByLabelText("Select a Jira project"));
    fireEvent.click(await screen.findByRole("option", { name: "ACME · Acme" }));

    await waitFor(() =>
      expect(searchIssues).toHaveBeenCalledWith({ provider: "jira", project: "ACME", q: undefined }),
    );
  });

  it("names the task it opened when the issue is imported as it is", async () => {
    renderDialog();

    fireEvent.click(await screen.findByRole("button", { name: "Import" }));

    await waitFor(() => expect(toastSuccess).toHaveBeenCalledWith("Imported as TT-7"));
  });

  it("says the product manager is turning the issue into tasks when a conversion is queued", async () => {
    importIssue.mockResolvedValue({
      task: task(),
      link: {
        provider: "github",
        key: "acme/web#12",
        url: "https://github.com/acme/web/issues/12",
        title: "Login button does nothing",
        created_at: "2026-02-01T00:00:00Z",
        closed_at: null,
      },
      import: importRecord({ conversion_status: "pending" }),
    });
    renderDialog();

    fireEvent.click(await screen.findByRole("button", { name: "Import" }));

    await waitFor(() =>
      expect(toastSuccess).toHaveBeenCalledWith(
        "Imported. The product manager is turning acme/web#12 into tasks on the board.",
      ),
    );
  });
});
