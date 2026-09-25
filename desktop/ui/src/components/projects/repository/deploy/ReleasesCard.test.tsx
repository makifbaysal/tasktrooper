import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Component, Release } from "@/api";
import { ReleasesCard } from "@/components/projects/repository/deploy/ReleasesCard";
import { I18nProvider } from "@/hooks/useI18n";

const { listReleases, getRelease, getReleaseCutPreview } = vi.hoisted(() => ({
  listReleases: vi.fn(),
  getRelease: vi.fn(),
  getReleaseCutPreview: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, listReleases, getRelease, getReleaseCutPreview } };
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

function makeComponent(overrides: Partial<Component> = {}): Component {
  return {
    id: "comp-1",
    repository_id: "repo-1",
    path: ".",
    name: {},
    role: {},
    stack: {},
    commands: [],
    docs: {},
    gates: {},
    status: "active",
    manually_added: false,
    needs_review: false,
    created_at: "2024-01-01T00:00:00Z",
    updated_at: "2024-01-01T00:00:00Z",
    ...overrides,
  };
}

function renderCard(component: Component = makeComponent()) {
  render(
    <MemoryRouter>
      <I18nProvider>
        <ReleasesCard repositoryId="repo-1" repositoryName="acme" component={component} />
      </I18nProvider>
    </MemoryRouter>,
  );
}

describe("ReleasesCard", () => {
  beforeEach(() => {
    listReleases.mockReset();
    getRelease.mockReset();
    getReleaseCutPreview.mockReset();
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

  it("pins a batch component's draft release first with a cut button when it carries tasks", async () => {
    const draft = makeRelease({ id: "rel-draft", status: "draft", tasks: [{ id: "task-1", key: "T-1" }, { id: "task-2", key: "T-2" }] });
    listReleases.mockResolvedValue({ releases: [draft, makeRelease()] });
    const component = makeComponent({ delivery: { override: { mode: "batch", executor: "github_actions", verify: {}, auto_rollback: true } } });
    renderCard(component);

    expect(await screen.findByText("Next release — 2 merged tasks")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Cut release" })).toBeInTheDocument();
  });

  it("hides the cut release button when the draft carries no tasks", async () => {
    const draft = makeRelease({ id: "rel-draft", status: "draft", tasks: [] });
    listReleases.mockResolvedValue({ releases: [draft] });
    const component = makeComponent({ delivery: { override: { mode: "batch", executor: "github_actions", verify: {}, auto_rollback: true } } });
    renderCard(component);

    expect(await screen.findByText("Next release — 0 merged tasks")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Cut release" })).not.toBeInTheDocument();
  });

  it("pins a pending batch release that never deployed with a re-cut button", async () => {
    const stuck = makeRelease({
      id: "rel-stuck",
      status: "pending",
      mode: "batch",
      version: "1.0.0",
      tasks: [{ id: "task-1", key: "T-1" }],
    });
    listReleases.mockResolvedValue({ releases: [stuck] });
    const component = makeComponent({ delivery: { override: { mode: "batch", executor: "github_actions", verify: {}, auto_rollback: true } } });
    renderCard(component);

    expect(await screen.findByText("1.0.0 never deployed")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Re-cut" })).toBeInTheDocument();
  });

  it("does not offer a re-cut once the pending release has started deploying", async () => {
    const deploying = makeRelease({
      id: "rel-deploying",
      status: "pending",
      mode: "batch",
      version: "1.0.0",
      deploy_started_at: "2024-01-01T00:00:00Z",
      tasks: [{ id: "task-1", key: "T-1" }],
    });
    listReleases.mockResolvedValue({ releases: [deploying] });
    const component = makeComponent({ delivery: { override: { mode: "batch", executor: "github_actions", verify: {}, auto_rollback: true } } });
    renderCard(component);

    await waitFor(() => expect(listReleases).toHaveBeenCalled());
    expect(screen.queryByRole("button", { name: "Re-cut" })).not.toBeInTheDocument();
  });

  it("pins both a new draft and a still-recuttable pending release as two separate rows", async () => {
    const stuck = makeRelease({
      id: "rel-stuck",
      status: "pending",
      mode: "batch",
      version: "1.0.0",
      tasks: [{ id: "task-1", key: "T-1" }],
    });
    const draft = makeRelease({ id: "rel-draft", status: "draft", tasks: [{ id: "task-2", key: "T-2" }] });
    listReleases.mockResolvedValue({ releases: [draft, stuck] });
    const component = makeComponent({ delivery: { override: { mode: "batch", executor: "github_actions", verify: {}, auto_rollback: true } } });
    renderCard(component);

    expect(await screen.findByText("1.0.0 never deployed")).toBeInTheDocument();
    expect(screen.getByText("Next release — 1 merged tasks")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Re-cut" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Cut release" })).toBeInTheDocument();
  });

  it("does not pin a draft row for a non-batch component", async () => {
    const draft = makeRelease({ id: "rel-draft", status: "draft", tasks: [{ id: "task-1", key: "T-1" }] });
    listReleases.mockResolvedValue({ releases: [draft] });
    renderCard(makeComponent({ delivery: { override: { mode: "on_merge", executor: "github_actions", verify: {}, auto_rollback: true } } }));

    await waitFor(() => expect(listReleases).toHaveBeenCalled());
    expect(screen.queryByText(/Next release/)).not.toBeInTheDocument();
  });
});
