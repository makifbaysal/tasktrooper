import "@testing-library/jest-dom/vitest";
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it } from "vitest";
import type { RepositorySummary } from "@/api";
import { RepositoryOverviewRow } from "@/components/projects/hub/RepositoryOverviewRow";
import { I18nProvider } from "@/hooks/useI18n";

function repository(overrides: Partial<RepositorySummary> = {}): RepositorySummary {
  return {
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
    ...overrides,
  };
}

function renderRow(repo: RepositorySummary) {
  return render(
    <MemoryRouter>
      <I18nProvider>
        <RepositoryOverviewRow repository={repo} projectId="proj-1" />
      </I18nProvider>
    </MemoryRouter>,
  );
}

describe("RepositoryOverviewRow environment chips", () => {
  it("renders nothing when there are no confirmed environments", () => {
    renderRow(repository({ environments: [{ id: "env-1", component_id: "c1", environment: "production", status: "suggested", error_count_24h: 0 }] }));
    expect(screen.queryByText("Prod")).not.toBeInTheDocument();
  });

  it("shows a confirmed environment's short label, health and error count", () => {
    renderRow(
      repository({
        environments: [
          {
            id: "env-1",
            component_id: "c1",
            environment: "production",
            provider: "vercel",
            status: "confirmed",
            health: "degraded",
            error_count_24h: 3,
          },
        ],
      }),
    );

    expect(screen.getByText("Prod")).toBeInTheDocument();
    const badge = screen.getByText("3");
    expect(badge).toHaveAttribute("title", "3 errors in the last 24 h");
  });

  it("hides the error badge when there were no errors", () => {
    renderRow(
      repository({
        environments: [
          {
            id: "env-1",
            component_id: "c1",
            environment: "staging",
            provider: "gcp",
            status: "confirmed",
            health: "healthy",
            error_count_24h: 0,
          },
        ],
      }),
    );

    expect(screen.getByText("Stg")).toBeInTheDocument();
    expect(screen.queryByText("0")).not.toBeInTheDocument();
  });
});
