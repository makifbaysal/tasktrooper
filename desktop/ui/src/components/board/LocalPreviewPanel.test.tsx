import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { BoardTask, LocalPreview, TaskPreview } from "@/api";
import { LocalPreviewPanel } from "@/components/board/LocalPreviewPanel";
import { I18nProvider } from "@/hooks/useI18n";

const { getLocalPreview, startLocalPreview, getTaskPreviews } = vi.hoisted(() => ({
  getLocalPreview: vi.fn(),
  startLocalPreview: vi.fn(),
  getTaskPreviews: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, getLocalPreview, startLocalPreview, getTaskPreviews } };
});

const task = { id: "task-1" } as unknown as BoardTask;

function makePreview(overrides: Partial<LocalPreview> = {}): LocalPreview {
  return {
    repository_id: "repo-1",
    task_id: "task-1",
    branch: "task/T-64",
    command: "npm run dev",
    status: "starting",
    started_at: "2026-09-27T00:00:00Z",
    ...overrides,
  };
}

function makeHostedPreview(overrides: Partial<TaskPreview> = {}): TaskPreview {
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

function renderPanel(overrides: Partial<BoardTask> = {}, actions?: ReactNode) {
  return render(
    <I18nProvider>
      <LocalPreviewPanel task={{ ...task, ...overrides }} repositoryId="repo-1" actions={actions} />
    </I18nProvider>,
  );
}

describe("LocalPreviewPanel", () => {
  beforeEach(() => {
    getLocalPreview.mockReset();
    startLocalPreview.mockReset();
    getTaskPreviews.mockReset().mockResolvedValue({ previews: [] });
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("shows why this task's preview died and offers to run it again", async () => {
    getLocalPreview.mockResolvedValue({
      active: true,
      preview: makePreview({
        status: "failed",
        detail: "exit status 1",
        log_tail: ["> next dev", "⨯ Another next dev server is already running."],
      }),
    });
    renderPanel();

    expect(await screen.findByText("exit status 1")).toBeInTheDocument();
    expect(screen.getByText(/Another next dev server is already running/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Run locally/ })).toBeEnabled();
    expect(screen.queryByRole("button", { name: /Stop/ })).not.toBeInTheDocument();
  });

  it("opens the site once the preview it was asked to start is running", async () => {
    const open = vi.spyOn(window, "open").mockReturnValue(null);
    getLocalPreview.mockResolvedValueOnce({ active: false });
    startLocalPreview.mockResolvedValue(makePreview({ status: "running", url: "http://localhost:3000" }));
    renderPanel();

    fireEvent.click(await screen.findByRole("button", { name: /Run locally/ }));

    await waitFor(() => expect(open).toHaveBeenCalledWith("http://localhost:3000", "_blank", "noopener,noreferrer"));
    expect(open).toHaveBeenCalledTimes(1);
    expect(screen.getByRole("link", { name: /localhost:3000/ })).toHaveAttribute("href", "http://localhost:3000");
  });

  it("does not open a preview it did not start", async () => {
    const open = vi.spyOn(window, "open").mockReturnValue(null);
    getLocalPreview.mockResolvedValue({ active: true, preview: makePreview({ status: "running", url: "http://localhost:3000" }) });
    renderPanel();

    expect(await screen.findByRole("link", { name: /localhost:3000/ })).toBeInTheDocument();
    expect(open).not.toHaveBeenCalled();
  });

  it("leads with the PR's ready hosted preview and never starts a local run on its own", async () => {
    getLocalPreview.mockResolvedValue({ active: false });
    getTaskPreviews.mockResolvedValue({ previews: [makeHostedPreview()] });
    const hostedLink = <a href={makeHostedPreview().branch_url}>Open preview</a>;
    renderPanel({ pr_url: "https://github.com/acme/web/pull/7" }, hostedLink);

    const hostedEl = await screen.findByRole("link", { name: "Open preview" });
    const runLocallyEl = screen.getByRole("button", { name: /Run locally/ });
    const order = Array.from(document.body.querySelectorAll("a, button"));
    expect(order.indexOf(hostedEl)).toBeLessThan(order.indexOf(runLocallyEl));
    expect(startLocalPreview).not.toHaveBeenCalled();
  });

  it("still starts a local run normally when triggered manually despite a ready hosted preview", async () => {
    getLocalPreview.mockResolvedValueOnce({ active: false });
    getTaskPreviews.mockResolvedValue({ previews: [makeHostedPreview()] });
    startLocalPreview.mockResolvedValue(makePreview({ status: "running", url: "http://localhost:3000" }));
    const open = vi.spyOn(window, "open").mockReturnValue(null);
    renderPanel({ pr_url: "https://github.com/acme/web/pull/7" }, <a href="https://x">Open preview</a>);

    fireEvent.click(await screen.findByRole("button", { name: /Run locally/ }));

    await waitFor(() => expect(startLocalPreview).toHaveBeenCalledWith("repo-1", "task-1"));
    await waitFor(() => expect(open).toHaveBeenCalledWith("http://localhost:3000", "_blank", "noopener,noreferrer"));
  });

  it("keeps Run locally first when the PR has no ready hosted preview", async () => {
    getLocalPreview.mockResolvedValue({ active: false });
    getTaskPreviews.mockResolvedValue({ previews: [makeHostedPreview({ status: "building", url: "", branch_url: "" })] });
    renderPanel({ pr_url: "https://github.com/acme/web/pull/7" });

    const runLocallyEl = await screen.findByRole("button", { name: /Run locally/ });
    expect(runLocallyEl).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "Open preview" })).not.toBeInTheDocument();
  });
});
