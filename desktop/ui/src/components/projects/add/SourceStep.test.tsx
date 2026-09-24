import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";

const { listInitiativeProjects, listRepositories, githubStatus } = vi.hoisted(() => ({
  listInitiativeProjects: vi.fn(),
  listRepositories: vi.fn(),
  githubStatus: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    api: {
      ...actual.api,
      listInitiativeProjects,
      listRepositories,
      githubStatus,
    },
  };
});

import { SourceStep } from "@/components/projects/add/SourceStep";
import { I18nProvider } from "@/hooks/useI18n";

function renderStep(onScan = vi.fn()) {
  render(
    <I18nProvider>
      <MemoryRouter>
        <SourceStep onScan={onScan} />
      </MemoryRouter>
    </I18nProvider>,
  );
  return onScan;
}

describe("SourceStep validation", () => {
  beforeEach(() => {
    listInitiativeProjects.mockReset().mockResolvedValue({ projects: [{ id: "p1", name: "Acme Shop" }] });
    listRepositories.mockReset().mockResolvedValue({ repositories: [] });
    githubStatus.mockReset().mockResolvedValue({ connected: false });
  });

  it("keeps Scan disabled with no project chosen, even once a source is filled in", async () => {
    renderStep();
    await waitFor(() => expect(listInitiativeProjects).toHaveBeenCalled());

    fireEvent.change(screen.getByPlaceholderText("/path/to/project"), { target: { value: "/Users/me/code/acme" } });

    const scanButton = screen.getByRole("button", { name: /Scan/i });
    expect(scanButton).toBeDisabled();
  });

  it("keeps Scan disabled for a new project until it has a name, given a source is already picked", async () => {
    renderStep();
    await waitFor(() => expect(listInitiativeProjects).toHaveBeenCalled());

    fireEvent.click(screen.getByRole("tab", { name: "New project" }));
    fireEvent.change(screen.getByPlaceholderText("/path/to/project"), { target: { value: "/Users/me/code/acme" } });

    const scanButton = screen.getByRole("button", { name: /Scan/i });
    expect(scanButton).toBeDisabled();

    fireEvent.change(screen.getByPlaceholderText("e.g. Acme Shop"), { target: { value: "Acme Shop" } });
    expect(scanButton).toBeEnabled();
  });

  it("counts every filled-in source in the Scan button's label", async () => {
    renderStep();
    await waitFor(() => expect(listInitiativeProjects).toHaveBeenCalled());

    expect(screen.getByRole("button", { name: "Scan (0)" })).toBeInTheDocument();
    fireEvent.change(screen.getByPlaceholderText("/path/to/project"), { target: { value: "/Users/me/code/acme" } });
    expect(screen.getByRole("button", { name: "Scan (1)" })).toBeInTheDocument();
    fireEvent.change(screen.getByPlaceholderText("my-app"), { target: { value: "acme-worker" } });
    expect(screen.getByRole("button", { name: "Scan (2)" })).toBeInTheDocument();
  });
});
