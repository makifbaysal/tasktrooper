import "@testing-library/jest-dom/vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Release, ReleaseStatus } from "@/api";
import { ReleaseDrawer } from "@/components/projects/repository/deploy/ReleaseDrawer";
import { I18nProvider } from "@/hooks/useI18n";

const { getRelease } = vi.hoisted(() => ({ getRelease: vi.fn() }));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, getRelease } };
});

function makeRelease(status: ReleaseStatus, overrides: Partial<Release> = {}): Release {
  return {
    id: "rel-1",
    repository_id: "repo-1",
    component_id: "comp-1",
    version: "abc1234",
    mode: "dispatch",
    executor: "github_actions",
    status,
    profile: { mode: "dispatch", executor: "github_actions", verify: {}, auto_rollback: true },
    checks: {},
    tasks: [{ id: "task-1", key: "T-1", title: "Fix the thing" }],
    created_at: "2024-01-01T00:00:00Z",
    updated_at: "2024-01-01T00:00:00Z",
    ...overrides,
  };
}

async function renderDrawer(release: Release) {
  getRelease.mockResolvedValue(release);
  render(
    <MemoryRouter>
      <I18nProvider>
        <ReleaseDrawer releaseId={release.id} repositoryName="acme" open onOpenChange={() => {}} />
      </I18nProvider>
    </MemoryRouter>,
  );
  await waitFor(() => expect(getRelease).toHaveBeenCalled());
  await screen.findByText(release.version);
}

describe("ReleaseDrawer", () => {
  beforeEach(() => {
    getRelease.mockReset();
  });

  it("offers Deploy only for a pending dispatch release", async () => {
    await renderDrawer(makeRelease("pending"));
    expect(screen.getByRole("button", { name: "Deploy" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Mark released" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Roll back" })).not.toBeInTheDocument();
  });

  it("offers Mark released and Roll back when awaiting a verdict", async () => {
    await renderDrawer(makeRelease("awaiting_verdict"));
    expect(screen.queryByRole("button", { name: "Deploy" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Mark released" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Roll back" })).toBeInTheDocument();
  });

  it("offers only Roll back for a released release", async () => {
    await renderDrawer(makeRelease("released"));
    expect(screen.queryByRole("button", { name: "Deploy" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Mark released" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Roll back" })).toBeInTheDocument();
  });

  it("offers no actions while deploying", async () => {
    await renderDrawer(makeRelease("deploying"));
    expect(screen.queryByRole("button", { name: "Deploy" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Mark released" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Roll back" })).not.toBeInTheDocument();
  });

  it("offers Mark released and Roll back for a failed release", async () => {
    await renderDrawer(makeRelease("failed", { failure_reason: "the deploy job errored" }));
    expect(screen.getByRole("button", { name: "Mark released" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Roll back" })).toBeInTheDocument();
    expect(screen.getByText("the deploy job errored")).toBeInTheDocument();
  });
});
