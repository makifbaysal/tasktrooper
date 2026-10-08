import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { AttachmentMeta, TaskDesign, TaskDocument } from "@/api";
import { TaskDesignSection } from "@/components/board/TaskDesignSection";
import { I18nProvider } from "@/hooks/useI18n";
import { ThemeProvider } from "@/hooks/useTheme";

const { getTaskDesign, listTaskAttachments, fetchAttachmentBlob } = vi.hoisted(() => ({
  getTaskDesign: vi.fn(),
  listTaskAttachments: vi.fn(),
  fetchAttachmentBlob: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, getTaskDesign, listTaskAttachments, fetchAttachmentBlob } };
});

function doc(over: Partial<TaskDocument>): TaskDocument {
  return {
    id: "doc",
    task_id: "design-task",
    title: "",
    content: "",
    format: "markdown",
    position: 0,
    created_by_type: "agent",
    created_by_id: "designer",
    created_at: "2026-10-01T00:00:00Z",
    updated_at: "2026-10-01T00:00:00Z",
    ...over,
  };
}

const screenshot: AttachmentMeta = {
  id: "att-1",
  filename: "export-dialog.png",
  content_type: "image/png",
  size_bytes: 2048,
  sha256: "x",
  created_by_type: "agent",
  created_at: "2026-10-01T00:00:00Z",
};

const design: TaskDesign = {
  references: [
    {
      task_id: "design-task",
      key: "D-12",
      title: "Export dialog",
      task_type: "design",
      column: "done",
      repository_id: "repo-web",
      documents: [
        doc({ id: "mock-b", title: "design: export dialog · B", format: "html", content: "<p>Variant B</p>" }),
        doc({ id: "handoff", title: "handoff: export dialog", content: "## States\n\nEmpty, loading and error." }),
        doc({ id: "mock-a", title: "design: export dialog · A", format: "html", content: "<p>Variant A</p>" }),
      ],
    },
  ],
  design_system: { project_id: "proj-1", project_name: "TaskTrooper", base_version: 3, layer_version: 1 },
};

function renderSection() {
  return render(
    <ThemeProvider>
      <I18nProvider>
        <MemoryRouter>
          <TaskDesignSection repositoryId="repo-1" taskId="task-1" repositoryName="web" active />
        </MemoryRouter>
      </I18nProvider>
    </ThemeProvider>,
  );
}

describe("TaskDesignSection", () => {
  beforeEach(() => {
    getTaskDesign.mockReset().mockResolvedValue(design);
    listTaskAttachments.mockReset().mockResolvedValue({
      attachments: [screenshot, { ...screenshot, id: "att-2", filename: "notes.pdf", content_type: "application/pdf" }],
    });
    fetchAttachmentBlob.mockReset().mockResolvedValue(new Blob(["png"], { type: "image/png" }));
    Object.defineProperty(URL, "createObjectURL", { value: vi.fn(() => "blob:screenshot"), configurable: true });
    Object.defineProperty(URL, "revokeObjectURL", { value: vi.fn(), configurable: true });
  });

  it("shows the design system, the hand-off spec, the screens and the screenshots", async () => {
    renderSection();

    const dsLink = await screen.findByRole("link", { name: "TaskTrooper v3 · web layer v1" });
    expect(dsLink).toHaveAttribute("href", "/repositories/repo-1?tab=design");
    expect(getTaskDesign).toHaveBeenCalledWith("repo-1", "task-1");

    expect(screen.getByRole("link", { name: /D-12\s*Export dialog/ })).toHaveAttribute(
      "href",
      "/repositories/repo-web/tasks/design-task/analysis",
    );

    const handoff = screen.getByRole("button", { name: "export dialog" });
    expect(handoff).toHaveAttribute("aria-expanded", "true");
    expect(screen.getByRole("heading", { name: "States" })).toBeInTheDocument();

    const openButtons = screen.getAllByRole("button", { name: /^Open export dialog · / });
    expect(openButtons.map((b) => b.getAttribute("aria-label"))).toEqual([
      "Open export dialog · A",
      "Open export dialog · B",
    ]);

    await waitFor(() => expect(listTaskAttachments).toHaveBeenCalledWith("repo-web", "design-task"));
    expect(await screen.findByAltText("export-dialog.png")).toHaveAttribute("src", "blob:screenshot");
    expect(fetchAttachmentBlob).toHaveBeenCalledWith("att-1");
    expect(screen.queryByText("notes.pdf")).not.toBeInTheDocument();
  });

  it("opens a mockup read-only in the sandboxed frame", async () => {
    renderSection();

    fireEvent.click(await screen.findByRole("button", { name: "Open export dialog · B" }));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText("export dialog · B")).toBeInTheDocument();
    const frame = await waitFor(() => {
      const iframe = dialog.querySelector("iframe");
      if (!iframe) throw new Error("no frame yet");
      return iframe;
    });
    expect(frame.getAttribute("sandbox")).toBe("allow-scripts");
    expect(frame.getAttribute("srcdoc")).toContain("<p>Variant B</p>");
    expect(within(dialog).queryByText("Variant B")).not.toBeInTheDocument();
  });

  it("warns when the base project is ambiguous", async () => {
    getTaskDesign.mockResolvedValue({ references: [], design_system: { layer_version: 2, ambiguous: true } });
    renderSection();

    expect(await screen.findByRole("link", { name: "web layer v2" })).toBeInTheDocument();
    expect(screen.getByText("Base project not chosen")).toBeInTheDocument();
    expect(listTaskAttachments).not.toHaveBeenCalled();
  });

  it("renders nothing without a design or a design system", async () => {
    getTaskDesign.mockResolvedValue({ references: null });
    const { container } = renderSection();

    await waitFor(() => expect(getTaskDesign).toHaveBeenCalled());
    await Promise.resolve();
    expect(container).toBeEmptyDOMElement();
  });
});
