import "@testing-library/jest-dom/vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi, beforeEach } from "vitest";
import type { Repository, WorkspaceIndex } from "@/api";
import { CodeIndexSearchPanel } from "@/components/rag/CodeIndexSearchPanel";
import { I18nProvider } from "@/hooks/useI18n";

const { getRepositoryIndexStatus } = vi.hoisted(() => ({
  getRepositoryIndexStatus: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    api: { ...actual.api, getRepositoryIndexStatus },
  };
});

function repo(): Repository {
  return {
    id: "repo-1",
    name: "demo",
    description: "",
    root_path: "/demo",
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  };
}

describe("CodeIndexSearchPanel heading scale", () => {
  beforeEach(() => {
    getRepositoryIndexStatus.mockReset();
  });

  it("renders the section title on the heading scale", async () => {
    getRepositoryIndexStatus.mockResolvedValue({ status: "completed" } satisfies Partial<WorkspaceIndex>);
    render(
      <I18nProvider>
        <MemoryRouter>
          <CodeIndexSearchPanel repositories={[repo()]} />
        </MemoryRouter>
      </I18nProvider>,
    );

    await waitFor(() => expect(getRepositoryIndexStatus).toHaveBeenCalled());
    expect(screen.getByRole("heading", { name: "Code repository indexes" }).className).toContain(
      "text-heading",
    );
  });
});
