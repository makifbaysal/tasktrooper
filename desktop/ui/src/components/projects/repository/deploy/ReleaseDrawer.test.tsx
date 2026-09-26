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

  it("shows the cut date, notes and local run for a batch release", async () => {
    await renderDrawer(
      makeRelease("verifying", {
        mode: "batch",
        executor: "local",
        cut_at: "2024-02-01T00:00:00Z",
        notes: "## 1.0.0\n### Features\n- T-1 Fix the thing",
        local_run: {
          argv: ["./scripts/publish.sh", "1.0.0"],
          exit_code: 0,
          log_path: "/data/releases/rel-1.log",
          tail: "publishing…\ndone",
          started_at: "2024-02-01T00:05:00Z",
        },
      }),
    );
    expect(screen.getByText(/### Features/)).toBeInTheDocument();
    expect(screen.getByText("./scripts/publish.sh 1.0.0")).toBeInTheDocument();
    expect(screen.getByText("/data/releases/rel-1.log")).toBeInTheDocument();
    expect(screen.getByText(/publishing…/)).toBeInTheDocument();
  });

  it("shows a smoke check's name as its label, with method+path as secondary text", async () => {
    await renderDrawer(
      makeRelease("verifying", {
        checks: {
          smoke: [
            {
              check: { name: "Health", method: "GET", path: "/health", expect_status: 200 },
              url: "https://example.com/health",
              at: "2024-02-01T00:05:00Z",
              status: 200,
              ok: true,
              latency_ms: 42,
            },
          ],
        },
      }),
    );
    expect(screen.getByText("Health")).toBeInTheDocument();
    expect(screen.getByText("GET /health")).toBeInTheDocument();
  });

  it("shows a store builds table for a batch store release", async () => {
    await renderDrawer(
      makeRelease("verifying", {
        mode: "batch",
        executor: "store",
        store_builds: [
          { platform: "ios", engine: "native", baseline_build: "41", build: "42" },
          { platform: "android", engine: "native", baseline_build: "41", error: "start failed" },
        ],
      }),
    );
    expect(screen.getByText("ios")).toBeInTheDocument();
    expect(screen.getByText("42")).toBeInTheDocument();
    expect(screen.getByText("start failed")).toBeInTheDocument();
  });
});
