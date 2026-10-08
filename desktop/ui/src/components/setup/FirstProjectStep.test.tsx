import "@testing-library/jest-dom/vitest";
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import type { InitiativeProject } from "@/api";
import { FirstProjectStep } from "@/components/setup/FirstProjectStep";
import { I18nProvider } from "@/hooks/useI18n";

const listInitiativeProjects = vi.fn<() => Promise<{ projects: InitiativeProject[] }>>();

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, listInitiativeProjects: () => listInitiativeProjects() } };
});

vi.mock("@/hooks/useSetup", () => ({
  useSetup: () => ({ steps: { project: { id: "project", state: "todo", actionable: true } }, refresh: vi.fn() }),
}));

function renderStep() {
  return render(
    <I18nProvider>
      <MemoryRouter>
        <FirstProjectStep />
      </MemoryRouter>
    </I18nProvider>,
  );
}

describe("FirstProjectStep", () => {
  it("asks for a project first and offers no import without one", async () => {
    listInitiativeProjects.mockResolvedValue({ projects: [] });
    renderStep();

    expect(await screen.findByRole("button", { name: "Create a project" })).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "Add repository" })).toBeNull();
  });

  it("offers the import inside the project once it exists", async () => {
    listInitiativeProjects.mockResolvedValue({ projects: [{ id: "proj-1", name: "Harbor" } as InitiativeProject] });
    renderStep();

    expect(await screen.findByRole("link", { name: "Add repository" })).toHaveAttribute(
      "href",
      "/projects/new?project=proj-1",
    );
    expect(screen.getByText("Harbor")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Create a project" })).toBeNull();
  });
});
