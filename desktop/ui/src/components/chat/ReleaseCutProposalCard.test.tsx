import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Release, SessionAction } from "@/api";
import { SessionActionCard } from "@/components/chat/SessionActionCard";
import { I18nProvider } from "@/hooks/useI18n";

const { getRelease, getRepository, dismissCutProposal, getReleaseCutPreview, cutRelease } = vi.hoisted(() => ({
  getRelease: vi.fn(),
  getRepository: vi.fn(),
  dismissCutProposal: vi.fn(),
  getReleaseCutPreview: vi.fn(),
  cutRelease: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, getRelease, getRepository, dismissCutProposal, getReleaseCutPreview, cutRelease } };
});

function makeRelease(overrides: Partial<Release> = {}): Release {
  return {
    id: "rel-1",
    repository_id: "repo-1",
    component_id: "comp-1",
    version: "",
    mode: "batch",
    executor: "github_actions",
    status: "draft",
    profile: { mode: "batch", executor: "github_actions", tag_pattern: "v{version}", verify: {}, auto_rollback: true },
    checks: {},
    tasks: [
      { id: "task-1", key: "T-1" },
      { id: "task-2", key: "T-2" },
    ],
    created_at: "2024-01-01T00:00:00Z",
    updated_at: "2024-01-01T00:00:00Z",
    cut_proposal: { proposed_at: "2024-01-02T00:00:00Z", by: "agent", version: "1.4.0", notes: "Two fixes" },
    ...overrides,
  };
}

const action: SessionAction = {
  id: "act-1",
  session_id: "sess-1",
  tool_name: "propose_release_cut",
  verb: "proposed",
  entity_kind: "release",
  entity_id: "rel-1",
  title: "1.4.0",
  repository_id: "repo-1",
  is_error: false,
  created_at: "2024-01-02T00:00:00Z",
};

function renderCard() {
  render(
    <I18nProvider>
      <SessionActionCard action={action} />
    </I18nProvider>,
  );
}

describe("ReleaseCutProposalCard", () => {
  beforeEach(() => {
    for (const fn of [getRelease, getRepository, dismissCutProposal, getReleaseCutPreview, cutRelease]) fn.mockReset();
    getRepository.mockResolvedValue({ id: "repo-1", name: "acme" });
    getReleaseCutPreview.mockResolvedValue({ suggested_version: "1.4.0", commit_sha: "abc1234", notes: "Two fixes", tasks: [] });
  });

  it("asks the person to approve, and cuts only after the repository name is typed", async () => {
    getRelease.mockResolvedValue(makeRelease());
    cutRelease.mockResolvedValue(makeRelease({ status: "pending", version: "1.4.0", cut_proposal: null }));
    renderCard();

    expect(await screen.findByText(/Nothing ships until you review and cut it/)).toBeInTheDocument();
    expect(screen.getByText("1.4.0")).toBeInTheDocument();
    await waitFor(() => expect(screen.getByRole("button", { name: "Review and cut" })).toBeEnabled());
    expect(cutRelease).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Review and cut" }));
    await screen.findByDisplayValue("Two fixes");
    const cut = screen.getByRole("button", { name: "Cut release" });
    expect(cut).toBeDisabled();
    fireEvent.change(screen.getByPlaceholderText("acme"), { target: { value: "acme" } });
    fireEvent.click(cut);

    await waitFor(() => expect(cutRelease).toHaveBeenCalledWith("rel-1", "acme", "1.4.0", "Two fixes"));
    expect(await screen.findByText("Cut as 1.4.0 — Pending.")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Review and cut" })).not.toBeInTheDocument();
  });

  it("dismisses the proposal and keeps the draft", async () => {
    getRelease.mockResolvedValue(makeRelease());
    dismissCutProposal.mockResolvedValue(makeRelease({ cut_proposal: null }));
    renderCard();

    fireEvent.click(await screen.findByRole("button", { name: "Dismiss" }));
    await waitFor(() => expect(dismissCutProposal).toHaveBeenCalledWith("rel-1"));
    expect(await screen.findByText("This proposal was dismissed. The draft is still open.")).toBeInTheDocument();
  });

  it("offers no approval for a release that was already cut", async () => {
    getRelease.mockResolvedValue(makeRelease({ status: "released", version: "1.4.0", cut_proposal: null }));
    renderCard();

    expect(await screen.findByText("Cut as 1.4.0 — Released.")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Review and cut" })).not.toBeInTheDocument();
  });

  it("says so when the release is gone", async () => {
    getRelease.mockRejectedValue(new Error("not found"));
    renderCard();

    expect(await screen.findByText("This release no longer exists.")).toBeInTheDocument();
  });
});
