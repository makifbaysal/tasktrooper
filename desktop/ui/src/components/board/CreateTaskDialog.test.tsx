import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { BoardColumn, Repository } from "@/api";
import { CreateTaskDialog } from "@/components/board/CreateTaskDialog";
import { I18nProvider } from "@/hooks/useI18n";

// jsdom has no layout, so Radix Select's scroll-into-view-on-open crashes without it.
if (!Element.prototype.scrollIntoView) {
  Element.prototype.scrollIntoView = () => {};
}

const { createRepositoryTask, listTaskTypes, searchIssues } = vi.hoisted(() => ({
  createRepositoryTask: vi.fn(),
  listTaskTypes: vi.fn(),
  searchIssues: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, createRepositoryTask, listTaskTypes, searchIssues } };
});

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

const columns: BoardColumn[] = [
  { id: "backlog", slug: "backlog", label: "Backlog", position: 0, is_backlog: true },
  { id: "todo", slug: "todo", label: "Todo", position: 1, is_backlog: false },
];

function renderDialog() {
  return render(
    <I18nProvider>
      <CreateTaskDialog
        open
        onOpenChange={() => {}}
        repositories={[repository()]}
        initiativeProjects={[]}
        columns={columns}
        agents={[]}
        memberAgentIds={[]}
        defaultColumn="todo"
        onCreated={() => {}}
      />
    </I18nProvider>,
  );
}

describe("CreateTaskDialog", () => {
  beforeEach(() => {
    createRepositoryTask.mockReset();
    listTaskTypes.mockReset().mockResolvedValue({ task_types: [] });
    searchIssues.mockReset().mockResolvedValue({ issues: [] });
  });

  it("keeps only one modal open at a time when switching to Import from issue, and preserves the draft on return", async () => {
    renderDialog();

    fireEvent.change(await screen.findByLabelText("Title"), { target: { value: "Gift cards" } });
    fireEvent.click(screen.getByRole("button", { name: /Import from an issue/ }));

    const importDialog = await screen.findByRole("dialog", { name: "Import from an issue" });
    // Two Radix Dialog.Contents mounted together fight over aria-hide/inert
    // and swallow pointer input meant for the inner one (the exact bug
    // HumanUatDecision's decline panel avoids the same way). Testing
    // Library's role queries silently skip aria-hidden nodes, which would
    // hide a still-mounted outer dialog from `queryByRole` even when it is
    // the live DOM node breaking clicks — so this asserts on the raw DOM
    // instead of the accessibility tree.
    expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(1);
    expect(document.getElementById("create-task-title")).toBeNull();

    fireEvent.click(within(importDialog).getByRole("button", { name: "Close" }));

    expect(await screen.findByRole("button", { name: "Create" })).toBeInTheDocument();
    expect(screen.getByLabelText("Title")).toHaveValue("Gift cards");
    expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(1);
  });

  it("still creates a task normally when the import dialog is never opened", async () => {
    createRepositoryTask.mockResolvedValue({ id: "task-9" });
    renderDialog();

    fireEvent.change(await screen.findByLabelText("Title"), { target: { value: "Gift cards" } });
    fireEvent.click(screen.getByRole("button", { name: "Create" }));

    await waitFor(() =>
      expect(createRepositoryTask).toHaveBeenCalledWith(
        "repo-1",
        expect.objectContaining({ title: "Gift cards" }),
      ),
    );
  });
});
