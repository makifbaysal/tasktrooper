import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Release } from "@/api";
import { ReleasesCard } from "@/components/projects/repository/deploy/ReleasesCard";
import { I18nProvider } from "@/hooks/useI18n";

const { listReleases, getRelease } = vi.hoisted(() => ({
  listReleases: vi.fn(),
  getRelease: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, listReleases, getRelease } };
});

function makeRelease(overrides: Partial<Release> = {}): Release {
  return {
    id: "rel-1",
    repository_id: "repo-1",
    component_id: "comp-1",
    version: "abc1234",
    mode: "on_merge",
    executor: "github_actions",
    status: "released",
    profile: { mode: "on_merge", executor: "github_actions", verify: {}, auto_rollback: true },
    checks: {},
    tasks: [{ id: "task-1", key: "T-1" }],
    created_at: "2024-01-01T00:00:00Z",
    updated_at: "2024-01-01T00:00:00Z",
    ...overrides,
  };
}

function renderCard() {
  render(
    <MemoryRouter>
      <I18nProvider>
        <ReleasesCard repositoryId="repo-1" repositoryName="acme" componentId="comp-1" />
      </I18nProvider>
    </MemoryRouter>,
  );
}

describe("ReleasesCard", () => {
  beforeEach(() => {
    listReleases.mockReset();
    getRelease.mockReset();
  });

  it("shows an empty state when the component has no releases", async () => {
    listReleases.mockResolvedValue({ releases: [] });
    renderCard();
    expect(await screen.findByText("No releases yet")).toBeInTheDocument();
  });

  it("lists releases with their version, status and task count", async () => {
    listReleases.mockResolvedValue({ releases: [makeRelease()] });
    renderCard();
    expect(await screen.findByText("abc1234")).toBeInTheDocument();
    expect(screen.getByText("Released")).toBeInTheDocument();
    expect(screen.getByText("1 tasks")).toBeInTheDocument();
  });

  it("opens the release drawer when a row is clicked", async () => {
    listReleases.mockResolvedValue({ releases: [makeRelease()] });
    getRelease.mockResolvedValue(makeRelease());
    renderCard();
    fireEvent.click(await screen.findByText("abc1234"));
    await waitFor(() => expect(getRelease).toHaveBeenCalledWith("rel-1"));
  });

  it("shows the deploy run link when the release carries one", async () => {
    listReleases.mockResolvedValue({
      releases: [makeRelease({ deploy: { task_id: "task-1", repository_id: "repo-1", env: "production", merge_commit_sha: "abc", state: "success", signal: "actions_run", run_url: "https://github.com/x/y/actions/runs/1", auto_rollback: true, checked_at: "2024-01-01T00:00:00Z" } })],
    });
    renderCard();
    const link = await screen.findByRole("link", { name: /Deploy run/ });
    expect(link).toHaveAttribute("href", "https://github.com/x/y/actions/runs/1");
  });
});
