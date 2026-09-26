import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { TaskPreview } from "@/api";
import { TaskPreviewsSection } from "@/components/board/TaskPreviewsSection";
import { I18nProvider } from "@/hooks/useI18n";

const { getTaskPreviews } = vi.hoisted(() => ({ getTaskPreviews: vi.fn() }));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, getTaskPreviews } };
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

function renderSection() {
  return render(
    <I18nProvider>
      <TaskPreviewsSection repositoryId="repo-1" taskId="task-1" />
    </I18nProvider>,
  );
}

describe("TaskPreviewsSection", () => {
  beforeEach(() => {
    getTaskPreviews.mockReset();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("shows a ready preview with an Open link to its branch address and its PR", async () => {
    getTaskPreviews.mockResolvedValue({ previews: [makePreview()] });
    renderSection();

    await screen.findByText("web-app");
    expect(getTaskPreviews).toHaveBeenCalledWith("repo-1", "task-1");
    expect(screen.getByText("Ready")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /Open/ })).toHaveAttribute(
      "href",
      "https://web-app-git-feat-x-acme.vercel.app",
    );
    expect(screen.getByText("PR #7")).toBeInTheDocument();
    expect(screen.queryByText("Protected")).not.toBeInTheDocument();
  });

  it("falls back to the commit address when there is no branch address", async () => {
    getTaskPreviews.mockResolvedValue({ previews: [makePreview({ branch_url: "" })] });
    renderSection();

    await screen.findByText("web-app");
    expect(screen.getByRole("link", { name: /Open/ })).toHaveAttribute(
      "href",
      "https://web-app-abc123-acme.vercel.app",
    );
  });

  it("a component with no deployment for the branch yet reads 'Not yet' with nothing to open", async () => {
    getTaskPreviews.mockResolvedValue({
      previews: [makePreview({ status: "none", url: "", branch_url: "", pr_number: 0 })],
    });
    renderSection();

    await screen.findByText("Not yet");
    expect(screen.queryByRole("link", { name: /Open/ })).not.toBeInTheDocument();
    expect(screen.queryByText(/^PR #/)).not.toBeInTheDocument();
  });

  it("marks a protected preview and explains the missing bypass secret", async () => {
    getTaskPreviews.mockResolvedValue({ previews: [makePreview({ protected: true, bypass_configured: false })] });
    renderSection();

    const badge = await screen.findByText("Protected");
    expect(badge).toHaveAttribute("title", expect.stringContaining("Protection Bypass for Automation"));
  });

  it("renders nothing when no component has a per-branch preview environment", async () => {
    getTaskPreviews.mockResolvedValue({ previews: [] });
    const { container } = renderSection();

    await vi.waitFor(() => expect(getTaskPreviews).toHaveBeenCalled());
    expect(container).toBeEmptyDOMElement();
  });

  it("polls every 15s while a preview is building and stops once it is ready", async () => {
    vi.useFakeTimers();
    getTaskPreviews
      .mockResolvedValueOnce({ previews: [makePreview({ status: "building" })] })
      .mockResolvedValueOnce({ previews: [makePreview({ status: "ready" })] });
    renderSection();

    await vi.waitFor(() => expect(screen.getByText("Preparing")).toBeInTheDocument());
    expect(getTaskPreviews).toHaveBeenCalledTimes(1);

    await vi.advanceTimersByTimeAsync(15_000);
    expect(getTaskPreviews).toHaveBeenCalledTimes(2);
    await vi.waitFor(() => expect(screen.getByText("Ready")).toBeInTheDocument());

    await vi.advanceTimersByTimeAsync(45_000);
    expect(getTaskPreviews).toHaveBeenCalledTimes(2);
  });

  it("stops polling when unmounted", async () => {
    vi.useFakeTimers();
    getTaskPreviews.mockResolvedValue({ previews: [makePreview({ status: "queued" })] });
    const { unmount } = renderSection();

    await vi.waitFor(() => expect(screen.getByText("Preparing")).toBeInTheDocument());
    unmount();
    await vi.advanceTimersByTimeAsync(60_000);
    expect(getTaskPreviews).toHaveBeenCalledTimes(1);
  });

  it("the refresh button reloads the previews", async () => {
    getTaskPreviews.mockResolvedValue({ previews: [makePreview()] });
    renderSection();

    await screen.findByText("web-app");
    fireEvent.click(screen.getByRole("button", { name: "Refresh previews" }));
    await vi.waitFor(() => expect(getTaskPreviews).toHaveBeenCalledTimes(2));
  });
});
