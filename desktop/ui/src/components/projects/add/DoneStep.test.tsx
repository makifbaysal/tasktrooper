import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const { navigate } = vi.hoisted(() => ({ navigate: vi.fn() }));

vi.mock("react-router-dom", async () => {
  const actual = await vi.importActual<typeof import("react-router-dom")>("react-router-dom");
  return { ...actual, useNavigate: () => navigate };
});

import { DoneStep } from "@/components/projects/add/DoneStep";
import type { DoneStats, PendingRepo } from "@/components/projects/add/flow-types";
import { I18nProvider } from "@/hooks/useI18n";

const stats: DoneStats = { components: 4, checks: 9, requiredChecks: 5, links: 14, reviewAnswered: 4 };

function repo(overrides: Partial<PendingRepo> = {}): PendingRepo {
  return {
    localId: overrides.repositoryId ?? "l1",
    label: "platform",
    recipe: { method: "folder", rootPath: "/repos/platform" },
    status: "ready",
    repositoryId: "repo-1",
    ...overrides,
  };
}

function renderStep(repos: PendingRepo[]) {
  render(
    <I18nProvider>
      <DoneStep repos={repos} projectId="proj-1" projectName="Acme Shop" stats={stats} />
    </I18nProvider>,
  );
}

describe("DoneStep navigation", () => {
  beforeEach(() => {
    navigate.mockReset();
  });

  it("opens the single repository directly, with the project id carried along", () => {
    renderStep([repo()]);
    fireEvent.click(screen.getByRole("button", { name: "Open repository" }));
    expect(navigate).toHaveBeenCalledWith("/repositories/repo-1?project=proj-1");
  });

  it("opens the project instead when several repositories were added", () => {
    renderStep([repo({ localId: "l1", repositoryId: "repo-1", label: "platform" }), repo({ localId: "l2", repositoryId: "repo-2", label: "infra" })]);
    fireEvent.click(screen.getByRole("button", { name: "Open repository" }));
    expect(navigate).toHaveBeenCalledWith("/projects/proj-1");
  });

  it("always opens the project from the Open project button", () => {
    renderStep([repo()]);
    fireEvent.click(screen.getByRole("button", { name: "Open project" }));
    expect(navigate).toHaveBeenCalledWith("/projects/proj-1");
  });
});
