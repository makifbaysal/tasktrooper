import "@testing-library/jest-dom/vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const { getLatestRepositoryScan, getRepositoryModel } = vi.hoisted(() => ({
  getLatestRepositoryScan: vi.fn(),
  getRepositoryModel: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    api: { ...actual.api, getLatestRepositoryScan, getRepositoryModel },
  };
});

import { ScanStep } from "@/components/projects/add/ScanStep";
import type { PendingRepo } from "@/components/projects/add/flow-types";
import { I18nProvider } from "@/hooks/useI18n";

function makeRepo(overrides: Partial<PendingRepo> = {}): PendingRepo {
  return {
    localId: "l1",
    label: "platform",
    recipe: { method: "github", owner: "acme", name: "platform" },
    status: "ready",
    repositoryId: "repo-1",
    ...overrides,
  };
}

describe("ScanStep", () => {
  beforeEach(() => {
    getLatestRepositoryScan.mockReset();
    getRepositoryModel.mockReset().mockResolvedValue({
      repository: { id: "repo-1", name: "platform", description: "", root_path: "/repos/platform", created_at: "", updated_at: "" },
      shape: "single",
      components: [],
      checks: [],
      links: [],
      incoming_links: [],
      resources: [],
      linked_components: [],
      environments: [],
      review: [],
    });
  });

  it("enables Continue once the polled scan comes back succeeded", async () => {
    getLatestRepositoryScan.mockResolvedValue({
      scan: {
        id: "scan-1",
        repository_id: "repo-1",
        trigger: "import",
        status: "succeeded",
        events: [],
        review_count: 0,
        started_at: "2026-01-01T00:00:00Z",
        finished_at: "2026-01-01T00:00:05Z",
      },
    });

    render(
      <I18nProvider>
        <ScanStep repos={[makeRepo()]} onRetry={vi.fn()} onContinue={vi.fn()} />
      </I18nProvider>,
    );

    const continueButton = screen.getByRole("button", { name: /Continue to review/i });
    expect(continueButton).toBeDisabled();
    await waitFor(() => expect(continueButton).toBeEnabled());
  });

  it("keeps Continue disabled while a repo is still importing", () => {
    render(
      <I18nProvider>
        <ScanStep repos={[makeRepo({ status: "importing", repositoryId: undefined })]} onRetry={vi.fn()} onContinue={vi.fn()} />
      </I18nProvider>,
    );
    expect(screen.getByRole("button", { name: /Continue to review/i })).toBeDisabled();
  });

  it("treats a failed import as settled, not blocking Continue", async () => {
    render(
      <I18nProvider>
        <ScanStep
          repos={[makeRepo({ status: "import_failed", repositoryId: undefined, error: "clone failed" })]}
          onRetry={vi.fn()}
          onContinue={vi.fn()}
        />
      </I18nProvider>,
    );
    await waitFor(() => expect(screen.getByRole("button", { name: /Continue to review/i })).toBeEnabled());
  });
});
