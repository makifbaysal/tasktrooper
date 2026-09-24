import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const { importGitHubRepository, createInitiativeProject, openRepository, createRepository } = vi.hoisted(() => ({
  importGitHubRepository: vi.fn(),
  createInitiativeProject: vi.fn(),
  openRepository: vi.fn(),
  createRepository: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    api: {
      ...actual.api,
      importGitHubRepository,
      createInitiativeProject,
      openRepository,
      createRepository,
    },
  };
});

import { useAddRepositoryFlow } from "@/components/projects/add/useAddRepositoryFlow";

describe("useAddRepositoryFlow", () => {
  beforeEach(() => {
    importGitHubRepository.mockReset();
    createInitiativeProject.mockReset();
    openRepository.mockReset();
    createRepository.mockReset();
  });

  it("imports every selected GitHub repo once, in order, with project_ids and no description", async () => {
    importGitHubRepository.mockResolvedValueOnce({ id: "repo-1" }).mockResolvedValueOnce({ id: "repo-2" });

    const { result } = renderHook(() => useAddRepositoryFlow());

    await act(async () => {
      await result.current.startScan(
        { mode: "existing", projectId: "proj-1", projectName: "Acme" },
        {
          github: {
            owner: "acme",
            repos: [
              { name: "platform", cloneUrl: "https://example.com/acme/platform.git" },
              { name: "infra" },
            ],
          },
          folderPath: "",
          empty: null,
        },
      );
    });

    await waitFor(() => expect(importGitHubRepository).toHaveBeenCalledTimes(2));

    expect(importGitHubRepository).toHaveBeenNthCalledWith(1, {
      owner: "acme",
      name: "platform",
      clone_url: "https://example.com/acme/platform.git",
      project_ids: ["proj-1"],
    });
    expect(importGitHubRepository).toHaveBeenNthCalledWith(2, {
      owner: "acme",
      name: "infra",
      clone_url: undefined,
      project_ids: ["proj-1"],
    });
    for (const call of importGitHubRepository.mock.calls) {
      expect(call[0]).not.toHaveProperty("description");
    }
    expect(createInitiativeProject).not.toHaveBeenCalled();

    expect(result.current.state.step).toBe(1);
    expect(result.current.state.repos.map((r) => r.label)).toEqual(["platform", "infra"]);
    await waitFor(() => expect(result.current.state.repos.every((r) => r.status === "ready")).toBe(true));
    expect(result.current.state.repos.map((r) => r.repositoryId)).toEqual(["repo-1", "repo-2"]);
  });

  it("creates a new project first, then imports into it", async () => {
    createInitiativeProject.mockResolvedValue({ id: "new-proj", name: "Acme Shop" });
    openRepository.mockResolvedValue({ id: "repo-folder" });

    const { result } = renderHook(() => useAddRepositoryFlow());

    await act(async () => {
      await result.current.startScan(
        { mode: "new", name: "Acme Shop" },
        { github: null, folderPath: "/Users/me/code/acme", empty: null },
      );
    });

    expect(createInitiativeProject).toHaveBeenCalledWith({ name: "Acme Shop" });
    await waitFor(() => expect(openRepository).toHaveBeenCalledWith("/Users/me/code/acme", undefined, ["new-proj"]));
    expect(result.current.state.projectId).toBe("new-proj");
    expect(result.current.state.projectName).toBe("Acme Shop");
  });

  it("retries only the failed repo, reusing its original recipe", async () => {
    createRepository.mockRejectedValueOnce(new Error("boom")).mockResolvedValueOnce({ id: "repo-empty" });

    const { result } = renderHook(() => useAddRepositoryFlow());

    await act(async () => {
      await result.current.startScan(
        { mode: "existing", projectId: "proj-1", projectName: "Acme" },
        { github: null, folderPath: "", empty: { name: "my-app", owner: "acme" } },
      );
    });

    await waitFor(() => expect(result.current.state.repos[0].status).toBe("import_failed"));
    const localId = result.current.state.repos[0].localId;

    act(() => {
      result.current.retryImport(localId);
    });

    await waitFor(() => expect(result.current.state.repos[0].status).toBe("ready"));
    expect(createRepository).toHaveBeenCalledTimes(2);
    expect(createRepository).toHaveBeenNthCalledWith(2, "my-app", "", undefined, ["proj-1"], "acme");
  });
});
