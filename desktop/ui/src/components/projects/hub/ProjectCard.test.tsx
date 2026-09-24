import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import type { ProjectOverview } from "@/api";
import { ProjectCard } from "@/components/projects/hub/ProjectCard";
import { I18nProvider } from "@/hooks/useI18n";

function baseProject(overrides: Partial<ProjectOverview> = {}): ProjectOverview {
  return {
    id: "proj-1",
    name: "Acme Shop",
    description: "Orders and checkout",
    type: "single_repo",
    review_count: 0,
    cross_projects: [],
    cross_links: 0,
    shared_resources: [],
    repositories: [
      {
        id: "repo-1",
        name: "acme-platform",
        description: "",
        shape: "single",
        components: [
          { id: "c1", path: ".", name: "api", role: "backend", stack_summary: "Go 1.23", checks: 1, required_checks: 1 },
        ],
        review_count: 0,
        environments: [],
        updated_at: "2026-01-01T00:00:00Z",
      },
    ],
    ...overrides,
  };
}

function renderCard(project: ProjectOverview, onEdit = vi.fn(), onDelete = vi.fn()) {
  render(
    <I18nProvider>
      <MemoryRouter>
        <ProjectCard project={project} repositories={project.repositories} onEdit={onEdit} onDelete={onDelete} />
      </MemoryRouter>
    </I18nProvider>,
  );
  return { onEdit, onDelete };
}

describe("ProjectCard", () => {
  it("renders the project name, type badge and its repositories", () => {
    renderCard(baseProject());
    expect(screen.getByText("Acme Shop")).toBeInTheDocument();
    expect(screen.getByText("Single repo")).toBeInTheDocument();
    expect(screen.getByText("acme-platform")).toBeInTheDocument();
  });

  it("shows an empty state with an add-repository link when the project has no repositories", () => {
    renderCard(baseProject({ repositories: [], type: "empty" }));
    expect(screen.getByText("No repositories yet")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Add repository" })).toHaveAttribute("href", "/projects/new?project=proj-1");
  });

  it("shows no cross-project footer when cross_projects and shared_resources are both empty", () => {
    renderCard(baseProject());
    expect(screen.queryByText("Links to")).not.toBeInTheDocument();
  });

  it("shows the cross-project footer when cross_projects is non-empty", () => {
    renderCard(baseProject({ cross_projects: [{ id: "proj-2", name: "Acme Billing" }], cross_links: 1 }));
    expect(screen.getByText("Links to")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Acme Billing" })).toHaveAttribute("href", "/projects/proj-2");
  });

  it("calls onEdit / onDelete from the header icon buttons", () => {
    const { onEdit, onDelete } = renderCard(baseProject());
    fireEvent.click(screen.getByTitle("Edit project"));
    fireEvent.click(screen.getByTitle("Delete project"));
    expect(onEdit).toHaveBeenCalledTimes(1);
    expect(onDelete).toHaveBeenCalledTimes(1);
  });
});
