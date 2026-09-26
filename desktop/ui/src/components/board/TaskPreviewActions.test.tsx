import "@testing-library/jest-dom/vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { BoardTask, TaskPreview } from "@/api";
import { LocalPreviewPanel } from "@/components/board/LocalPreviewPanel";
import { TaskPreviewActions } from "@/components/board/TaskPreviewActions";
import { I18nProvider } from "@/hooks/useI18n";

const { getTaskPreviews, getLocalPreview } = vi.hoisted(() => ({
  getTaskPreviews: vi.fn(),
  getLocalPreview: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, getTaskPreviews, getLocalPreview } };
});

function makePreview(overrides: Partial<TaskPreview> = {}): TaskPreview {
  return {
    component_id: "comp-1",
    component_name: "web-app",
    environment_id: "env-prev",
    provider: "vercel",
    status: "ready",
    url: "https://web-app-abc123-acme.vercel.app",
    branch_url: "https://web-app-git-feat-x-acme.vercel.app",
    pr_number: 7,
    commit_sha: "abcdef1234567",
    created_at: "2024-01-01T00:00:00Z",
    inspect_url: "https://vercel.com/acme/web-app/dpl_1",
    protected: false,
    bypass_configured: false,
    ...overrides,
  };
}

function renderActions() {
  return render(
    <I18nProvider>
      <TaskPreviewActions repositoryId="repo-1" taskId="task-1" />
    </I18nProvider>,
  );
}

describe("TaskPreviewActions", () => {
  beforeEach(() => {
    getTaskPreviews.mockReset();
    getLocalPreview.mockReset().mockResolvedValue({ active: false });
  });

  it("links a ready preview to its branch address", async () => {
    getTaskPreviews.mockResolvedValue({ previews: [makePreview()] });
    renderActions();

    const link = await screen.findByRole("link", { name: /Open preview/ });
    expect(link).toHaveAttribute("href", "https://web-app-git-feat-x-acme.vercel.app");
    expect(link).toHaveAttribute("target", "_blank");
  });

  it("falls back to the commit URL when there is no branch alias", async () => {
    getTaskPreviews.mockResolvedValue({ previews: [makePreview({ branch_url: "" })] });
    renderActions();

    expect(await screen.findByRole("link", { name: /Open preview/ })).toHaveAttribute(
      "href",
      "https://web-app-abc123-acme.vercel.app",
    );
  });

  it("shows a disabled building state while the preview is being built", async () => {
    getTaskPreviews.mockResolvedValue({ previews: [makePreview({ status: "building" })] });
    renderActions();

    expect(await screen.findByRole("button", { name: /Preview building/ })).toBeDisabled();
    expect(screen.queryByRole("link")).not.toBeInTheDocument();
  });

  it("says when the preview build failed", async () => {
    getTaskPreviews.mockResolvedValue({ previews: [makePreview({ status: "error" })] });
    renderActions();

    expect(await screen.findByText("Preview build failed")).toBeInTheDocument();
  });

  it("renders nothing when there is no preview yet", async () => {
    getTaskPreviews.mockResolvedValue({ previews: [makePreview({ status: "none", url: "", branch_url: "" })] });
    const { container } = renderActions();

    await waitFor(() => expect(getTaskPreviews).toHaveBeenCalled());
    expect(container).toBeEmptyDOMElement();
  });

  it("names each component when there are several previews", async () => {
    getTaskPreviews.mockResolvedValue({
      previews: [
        makePreview(),
        makePreview({ component_id: "comp-2", component_name: "docs", branch_url: "https://docs-git-feat-x-acme.vercel.app" }),
      ],
    });
    renderActions();

    expect(await screen.findByRole("link", { name: /Open web-app preview/ })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /Open docs preview/ })).toHaveAttribute(
      "href",
      "https://docs-git-feat-x-acme.vercel.app",
    );
  });

  it("sits next to Run locally in the local preview panel", async () => {
    getTaskPreviews.mockResolvedValue({ previews: [makePreview()] });
    const task = { id: "task-1", pr_url: "https://github.com/acme/web/pull/7" } as unknown as BoardTask;
    render(
      <I18nProvider>
        <LocalPreviewPanel task={task} repositoryId="repo-1" actions={<TaskPreviewActions repositoryId="repo-1" taskId="task-1" />} />
      </I18nProvider>,
    );

    expect(await screen.findByRole("link", { name: /Open preview/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Run locally/ })).toBeInTheDocument();
  });
});
