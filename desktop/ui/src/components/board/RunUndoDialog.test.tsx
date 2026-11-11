import "@testing-library/jest-dom/vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { RunUndoPreview } from "@/api";
import { RunUndoDialog } from "@/components/board/RunUndoDialog";
import { I18nProvider } from "@/hooks/useI18n";

const { getRunUndoPreview, undoRun, revertRun } = vi.hoisted(() => ({
  getRunUndoPreview: vi.fn(),
  undoRun: vi.fn(),
  revertRun: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, getRunUndoPreview, undoRun, revertRun } };
});

function preview(over: Partial<RunUndoPreview> = {}): RunUndoPreview {
  return {
    run_id: "r1",
    supported: true,
    mode: "reset",
    branch: "tt-7-export",
    base_sha: "0123456789abcdef",
    commits: [{ sha: "aaaaaaaaaaaa", subject: "TT-7: change app" }],
    files: [{ path: "app.txt", status: "M" }],
    uncommitted: [],
    pushed: false,
    remote_ahead: false,
    backup_ref: "tasktrooper/undo-backup/r1",
    non_undoable: [{ seq: 4, tool: "http_request", kind: "http_request", target: "api.example.com" }],
    move_to: "todo",
    revert_available: false,
    ...over,
  };
}

function renderDialog(onDone = vi.fn()) {
  render(
    <I18nProvider>
      <RunUndoDialog open onOpenChange={() => {}} repositoryId="repo" taskId="task" runId="r1" onDone={onDone} />
    </I18nProvider>,
  );
  return onDone;
}

describe("RunUndoDialog", () => {
  beforeEach(() => {
    getRunUndoPreview.mockReset().mockResolvedValue({ preview: preview() });
    undoRun.mockReset().mockResolvedValue({
      result: { run_id: "r1", mode: "reset", backup_ref: "tasktrooper/undo-backup/r1", push_pending: false, force_pushed: false, cleaned_paths: [], left_untracked: ["x.log"], moved_to: "todo", stale_criteria: 2, non_undoable: [] },
    });
    revertRun.mockReset().mockResolvedValue({ result: { run_id: "r1", branch: "tasktrooper/revert-tt-7", pr_url: "https://github.com/o/r/pull/9", merge_sha: "abc" } });
  });
  afterEach(() => cleanup());

  it("lists what would be reset, the backup and what cannot be undone, and sends nothing until confirmed", async () => {
    renderDialog();
    expect(await screen.findByText(/goes back to commit 0123456789/)).toBeInTheDocument();
    expect(screen.getByText("TT-7: change app")).toBeInTheDocument();
    expect(screen.getByText(/tasktrooper\/undo-backup\/r1/)).toBeInTheDocument();
    expect(screen.getByText("What this cannot undo")).toBeInTheDocument();
    expect(screen.getByText(/HTTP request that can change something/)).toBeInTheDocument();
    expect(undoRun).not.toHaveBeenCalled();
  });

  it("confirms with the move-back choice and shows the backup afterwards", async () => {
    const onDone = renderDialog();
    fireEvent.click(await screen.findByRole("button", { name: "Undo this run" }));
    await waitFor(() => expect(undoRun).toHaveBeenCalledWith("repo", "task", "r1", { force_push: false, keep_column: false }));
    expect(await screen.findByText("The run was undone")).toBeInTheDocument();
    expect(screen.getByText(/saved on the branch tasktrooper\/undo-backup\/r1/)).toBeInTheDocument();
    expect(screen.getByText("x.log")).toBeInTheDocument();
    expect(onDone).toHaveBeenCalled();
  });

  it("offers a force-push only for a pushed branch, and never when the remote is ahead", async () => {
    getRunUndoPreview.mockResolvedValue({ preview: preview({ pushed: true }) });
    renderDialog();
    fireEvent.click(await screen.findByLabelText(/force-push the reset branch/));
    fireEvent.click(screen.getByRole("button", { name: "Undo this run" }));
    await waitFor(() => expect(undoRun).toHaveBeenCalledWith("repo", "task", "r1", { force_push: true, keep_column: false }));
  });

  it("hides the force-push when the remote holds other commits", async () => {
    getRunUndoPreview.mockResolvedValue({ preview: preview({ pushed: true, remote_ahead: true }) });
    renderDialog();
    expect(await screen.findByText(/not force-pushed/)).toBeInTheDocument();
    expect(screen.queryByLabelText(/force-push the reset branch/)).not.toBeInTheDocument();
  });

  it("gates the confirm button: a blocked run offers none", async () => {
    getRunUndoPreview.mockResolvedValue({ preview: preview({ supported: false, reason: "remote_not_supported", commits: [], files: [] }) });
    renderDialog();
    expect(await screen.findByText(/another computer/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Undo this run" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Open the revert" })).not.toBeInTheDocument();
  });

  it("opens a revert instead of rewriting history when the pull request is merged", async () => {
    getRunUndoPreview.mockResolvedValue({
      preview: preview({ supported: false, reason: "merged", mode: "revert", revert_available: true, pull_request: { url: "u", merged: true, merge_sha: "fedcba9876543210" } }),
    });
    renderDialog();
    expect(screen.queryByRole("button", { name: "Undo this run" })).not.toBeInTheDocument();
    fireEvent.click(await screen.findByRole("button", { name: "Open the revert" }));
    await waitFor(() => expect(revertRun).toHaveBeenCalledWith("repo", "task", "r1"));
    expect(await screen.findByText(/pull\/9/)).toBeInTheDocument();
    expect(undoRun).not.toHaveBeenCalled();
  });

  it("shows a server refusal and stays open", async () => {
    undoRun.mockRejectedValue(new Error("this run cannot be undone (live_run)"));
    renderDialog();
    fireEvent.click(await screen.findByRole("button", { name: "Undo this run" }));
    expect(await screen.findByText(/live_run/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Undo this run" })).toBeInTheDocument();
  });
});
