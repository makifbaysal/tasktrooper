import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError, type MobileStoreApp, type StoreTestBuild } from "@/api";
import { TaskStoreBuildsPanel } from "@/components/board/TaskStoreBuildsPanel";
import { I18nProvider } from "@/hooks/useI18n";

const { listStoreTestBuilds, startStoreTestBuilds, listStoreTestGroups } = vi.hoisted(() => ({
  listStoreTestBuilds: vi.fn(),
  startStoreTestBuilds: vi.fn(),
  listStoreTestGroups: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, listStoreTestBuilds, startStoreTestBuilds, listStoreTestGroups } };
});

function app(platform: "ios" | "android"): MobileStoreApp {
  return {
    id: `app-${platform}`,
    repository_id: "repo-1",
    platform,
    identifier: "com.example.app",
    store_app_id: platform === "ios" ? "123" : undefined,
    state: "test_ready",
    created_at: "2026-10-01T00:00:00Z",
    updated_at: "2026-10-01T00:00:00Z",
  };
}

function build(overrides: Partial<StoreTestBuild> = {}): StoreTestBuild {
  return {
    id: "b1",
    repository_id: "repo-1",
    platform: "android",
    task_id: "task-1",
    task_key: "T-54",
    attempt: 2,
    sequence: 412,
    build_number: "412",
    commit_sha: "a1b2c3d4e5f6",
    status: "ready",
    install_url: "https://play.google.com/apps/test/abc",
    has_artifact: true,
    groups: null,
    trigger: "human_uat",
    created_at: "2026-10-01T00:00:00Z",
    updated_at: "2026-10-01T00:00:00Z",
    ...overrides,
  };
}

function renderPanel(apps: MobileStoreApp[]) {
  return render(
    <I18nProvider>
      <TaskStoreBuildsPanel repositoryId="repo-1" taskId="task-1" apps={apps} />
    </I18nProvider>,
  );
}

describe("TaskStoreBuildsPanel", () => {
  beforeEach(() => {
    listStoreTestBuilds.mockReset();
    startStoreTestBuilds.mockReset();
    listStoreTestGroups.mockReset().mockResolvedValue([]);
  });

  it("shows an Android build's install link and label", async () => {
    listStoreTestBuilds.mockResolvedValue([build()]);
    renderPanel([app("android")]);

    expect(await screen.findByRole("link", { name: /Install link/ })).toHaveAttribute(
      "href",
      "https://play.google.com/apps/test/abc",
    );
    expect(screen.getByText("T-54 · #2 · a1b2c3d")).toBeInTheDocument();
    expect(listStoreTestBuilds).toHaveBeenCalledWith("repo-1", { taskId: "task-1", limit: 30 });
  });

  it("offers one Build for test for every linked app when the task has none", async () => {
    listStoreTestBuilds.mockResolvedValue([]);
    startStoreTestBuilds.mockResolvedValue({ builds: [build({ status: "queued" })] });
    renderPanel([app("ios"), app("android")]);

    fireEvent.click(await screen.findByRole("button", { name: /Build for test/ }));

    await waitFor(() =>
      expect(startStoreTestBuilds).toHaveBeenCalledWith("repo-1", { task_id: "task-1", platforms: undefined }),
    );
    expect(await screen.findByText("Queued")).toBeInTheDocument();
  });

  it("renders nothing when test builds are not configured on this install", async () => {
    listStoreTestBuilds.mockRejectedValue(new ApiError("not configured", 503));
    const { container } = renderPanel([app("ios")]);
    await waitFor(() => expect(container).toBeEmptyDOMElement());
  });
});
