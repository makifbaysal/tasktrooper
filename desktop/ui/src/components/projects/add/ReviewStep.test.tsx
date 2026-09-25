import "@testing-library/jest-dom/vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Component, Repository, RepositoryModel } from "@/api";

const { getRepositoryModel } = vi.hoisted(() => ({
  getRepositoryModel: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    api: { ...actual.api, getRepositoryModel },
  };
});

import { ReviewStep } from "@/components/projects/add/ReviewStep";
import type { PendingRepo } from "@/components/projects/add/flow-types";
import { I18nProvider } from "@/hooks/useI18n";

function makeRepository(id: string, name: string): Repository {
  return {
    id,
    name,
    description: "",
    root_path: `/repos/${name}`,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  };
}

function makeComponent(id: string, repositoryId: string): Component {
  return {
    id,
    repository_id: repositoryId,
    path: "services/worker",
    name: {},
    role: { detected: "worker", confidence: "medium" },
    stack: {},
    commands: [],
    docs: {},
    gates: {},
    status: "active",
    manually_added: false,
    needs_review: false,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  };
}

function makeModel(repo: Repository, opts: { withReview: boolean }): RepositoryModel {
  const component = makeComponent("c1", repo.id);
  return {
    repository: repo,
    shape: "single",
    components: [component],
    checks: [],
    links: [],
    incoming_links: [],
    resources: [],
    linked_components: [],
    environments: [],
    review: opts.withReview
      ? [{ kind: "role", entity_id: component.id, repository_id: repo.id, component_id: component.id, confidence: "medium" }]
      : [],
  };
}

function makeRepo(repositoryId: string, label: string): PendingRepo {
  return {
    localId: repositoryId,
    label,
    recipe: { method: "folder", rootPath: `/repos/${label}` },
    status: "ready",
    repositoryId,
  };
}

describe("ReviewStep", () => {
  beforeEach(() => {
    getRepositoryModel.mockReset();
  });

  it("shows the ReviewList only for the repo that has review items, and hides the success notice", async () => {
    const repoA = makeRepository("repo-1", "platform");
    const repoB = makeRepository("repo-2", "docs");
    getRepositoryModel.mockImplementation((id: string) =>
      Promise.resolve(id === "repo-1" ? makeModel(repoA, { withReview: true }) : makeModel(repoB, { withReview: false })),
    );

    render(
      <I18nProvider>
        <ReviewStep repos={[makeRepo("repo-1", "platform"), makeRepo("repo-2", "docs")]} onFinish={vi.fn()} />
      </I18nProvider>,
    );

    await waitFor(() => expect(getRepositoryModel).toHaveBeenCalledTimes(2));

    const headings = await screen.findAllByText("Needs your answer");
    expect(headings).toHaveLength(1);
    expect(screen.queryByText("Nothing to ask — everything was detected with confidence")).not.toBeInTheDocument();
  });

  it("shows the success notice once every repo has zero review items", async () => {
    const repoA = makeRepository("repo-1", "platform");
    const repoB = makeRepository("repo-2", "docs");
    getRepositoryModel.mockImplementation((id: string) =>
      Promise.resolve(id === "repo-1" ? makeModel(repoA, { withReview: false }) : makeModel(repoB, { withReview: false })),
    );

    render(
      <I18nProvider>
        <ReviewStep repos={[makeRepo("repo-1", "platform"), makeRepo("repo-2", "docs")]} onFinish={vi.fn()} />
      </I18nProvider>,
    );

    await waitFor(() =>
      expect(screen.getByText("Nothing to ask — everything was detected with confidence")).toBeInTheDocument(),
    );
    expect(screen.queryByText("Needs your answer")).not.toBeInTheDocument();
  });
});
