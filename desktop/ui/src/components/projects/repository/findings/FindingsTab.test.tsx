import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Component, ComponentFinding, RepositoryModel } from "@/api";
import { FindingsTab } from "@/components/projects/repository/findings/FindingsTab";
import { I18nProvider } from "@/hooks/useI18n";

if (!Element.prototype.scrollIntoView) {
  Element.prototype.scrollIntoView = () => {};
}

const { updateFinding, createFindingTask } = vi.hoisted(() => ({
  updateFinding: vi.fn(),
  createFindingTask: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, updateFinding, createFindingTask } };
});

const base = {
  repository_id: "repo-1",
  name: { detected: "x" },
  role: { detected: "backend" as const },
  stack: { detected: {} },
  commands: [],
  docs: {},
  gates: {},
  status: "active" as const,
  manually_added: false,
  needs_review: false,
  created_at: "2024-01-01T00:00:00Z",
  updated_at: "2024-01-01T00:00:00Z",
};
const api: Component = { ...base, id: "comp-api", path: "services/api" };
const web: Component = { ...base, id: "comp-web", path: "apps/web", role: { detected: "frontend" } };

function finding(over: Partial<ComponentFinding>): ComponentFinding {
  return {
    id: "f",
    repository_id: "repo-1",
    component_id: "comp-api",
    rule: "empty_catch_block",
    category: "error_handling",
    severity: "medium",
    occurrences: 3,
    evidence: [{ path: "services/api/main.go", line: 12, note: "if err != nil {" }],
    status: "open",
    created_at: "2024-01-01T00:00:00Z",
    updated_at: "2024-01-01T00:00:00Z",
    ...over,
  };
}

const findings: ComponentFinding[] = [
  finding({ id: "f-secret", rule: "hardcoded_secret", category: "security", severity: "high", occurrences: 1 }),
  finding({ id: "f-catch" }),
  finding({ id: "f-console", component_id: "comp-web", rule: "console_logs_in_production", category: "logging" }),
  finding({ id: "f-fp", rule: "print_instead_of_logger", category: "logging", severity: "low", status: "false_positive", status_reason: "a CLI" }),
  finding({ id: "f-env", component_id: undefined, rule: "committed_env_file", category: "security", severity: "high" }),
];

const model: RepositoryModel = {
  repository: { id: "repo-1", name: "acme", description: "", root_path: "/repo", created_at: "", updated_at: "" },
  shape: "monorepo",
  components: [api, web],
  checks: [],
  links: [],
  incoming_links: [],
  resources: [],
  linked_components: [],
  environments: [],
  review: [],
  latest_scan: { id: "s", repository_id: "repo-1", trigger: "import", status: "succeeded", events: [], review_count: 0, started_at: "" },
  findings,
};

function renderTab(selectedComponentId: string | null = null, onReload = vi.fn()) {
  return render(
    <MemoryRouter>
      <I18nProvider>
        <FindingsTab model={model} repositoryId="repo-1" selectedComponentId={selectedComponentId} onSelectComponent={vi.fn()} onReload={onReload} />
      </I18nProvider>
    </MemoryRouter>,
  );
}

describe("FindingsTab", () => {
  beforeEach(() => {
    updateFinding.mockReset().mockResolvedValue(findings[0]);
    createFindingTask.mockReset().mockResolvedValue({ task: { id: "t1", key: "TC-1", title: "x" }, findings: [] });
  });

  it("groups open findings under every component and the repository, worst first", () => {
    renderTab();
    expect(screen.getByText("Secret committed in code")).toBeInTheDocument();
    expect(screen.getByText("Empty error handler")).toBeInTheDocument();
    expect(screen.getByText("Debug console output in production")).toBeInTheDocument();
    expect(screen.getByText("Repository-wide")).toBeInTheDocument();
    expect(screen.queryByText("Prints instead of logging")).not.toBeInTheDocument();
    const titles = screen.getAllByText(/Secret committed in code|Empty error handler/).map((n) => n.textContent);
    expect(titles).toEqual(["Secret committed in code", "Empty error handler"]);
  });

  it("scopes to the selected component", () => {
    renderTab("comp-web");
    expect(screen.getByText("Debug console output in production")).toBeInTheDocument();
    expect(screen.queryByText("Empty error handler")).not.toBeInTheDocument();
    expect(screen.queryByText("Repository-wide")).not.toBeInTheDocument();
  });

  it("shows a false positive with its reason and reopens it", async () => {
    const onReload = vi.fn();
    renderTab(null, onReload);
    fireEvent.click(screen.getByRole("tab", { name: /False positive/ }));
    expect(screen.getByText("Prints instead of logging")).toBeInTheDocument();
    expect(screen.getByText("Reason: a CLI")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /Reopen/ }));
    await waitFor(() => expect(updateFinding).toHaveBeenCalledWith("f-fp", { status: "open" }));
    expect(onReload).toHaveBeenCalled();
  });

  it("marks a finding as a false positive with a reason", async () => {
    renderTab("comp-api");
    const card = screen.getByText("Empty error handler").closest("div.space-y-2") as HTMLElement;
    fireEvent.click(within(card).getByRole("button", { name: "False positive" }));
    fireEvent.change(await screen.findByLabelText("Reason (optional)"), { target: { value: "  retried by the caller " } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() =>
      expect(updateFinding).toHaveBeenCalledWith("f-catch", { status: "false_positive", reason: "retried by the caller" }),
    );
  });

  it("creates one task for the selected findings", async () => {
    renderTab("comp-api");
    fireEvent.click(screen.getByRole("button", { name: "Select all" }));
    fireEvent.click(screen.getByRole("button", { name: "Create task for 2" }));
    const title = await screen.findByLabelText("Title");
    expect((title as HTMLInputElement).value).toBe("Fix 2 code health findings in services/api");
    fireEvent.click(screen.getByRole("button", { name: "Create task" }));
    await waitFor(() =>
      expect(createFindingTask).toHaveBeenCalledWith("repo-1", {
        finding_ids: ["f-secret", "f-catch"],
        title: "Fix 2 code health findings in services/api",
        notes: undefined,
      }),
    );
  });
});
