import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi, beforeEach } from "vitest";
import type { Component, ComponentCheck, ComponentEnvironment, Repository, RepositoryModel } from "@/api";
import { ReviewList } from "@/components/projects/model/ReviewList";
import { I18nProvider } from "@/hooks/useI18n";

const { patchEnvironment, updateComponent, updateCheck } = vi.hoisted(() => ({
  patchEnvironment: vi.fn(),
  updateComponent: vi.fn(),
  updateCheck: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    api: { ...actual.api, patchEnvironment, updateComponent, updateCheck },
  };
});

const repository: Repository = {
  id: "repo-1",
  name: "acme-platform",
  description: "",
  root_path: "/repo",
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
};

function component(overrides: Partial<Component> = {}): Component {
  return {
    id: "c1",
    repository_id: "repo-1",
    path: ".",
    name: { detected: "web" },
    role: { detected: "frontend" },
    stack: {},
    commands: [],
    docs: {},
    gates: {},
    status: "active",
    manually_added: false,
    needs_review: false,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

function baseModel(environments: ComponentEnvironment[]): RepositoryModel {
  return {
    repository,
    shape: "single",
    components: [component()],
    checks: [],
    links: [],
    incoming_links: [],
    resources: [],
    linked_components: [],
    notes: [],
    environments,
    review: environments.map((e) => ({
      kind: "environment",
      entity_id: e.id,
      repository_id: "repo-1",
      component_id: "c1",
      confidence: "medium",
    })),
  };
}

function envWithCandidate(): ComponentEnvironment {
  return {
    id: "env-1",
    repository_id: "repo-1",
    component_id: "c1",
    environment: "production",
    provider: "vercel",
    status: "suggested",
    source: "scan",
    confidence: "medium",
    auto_confirmed: false,
    candidates: [
      {
        account_id: "acc-1",
        provider: "vercel",
        ref: { kind: "vercel_project", id: "prj_1", name: "acme-web", region: "iad1" },
        url: "https://acme-web.vercel.app",
      },
    ],
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  };
}

function envWithNoAccount(): ComponentEnvironment {
  return {
    id: "env-2",
    repository_id: "repo-1",
    component_id: "c1",
    environment: "staging",
    provider: "gcp",
    status: "suggested",
    source: "scan",
    confidence: "medium",
    auto_confirmed: false,
    candidates: [],
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  };
}

describe("ReviewList environment item", () => {
  beforeEach(() => {
    patchEnvironment.mockReset();
  });

  it("picking a candidate sends patchEnvironment with its account and resource", async () => {
    patchEnvironment.mockResolvedValue({});
    const onChanged = vi.fn();
    render(
      <I18nProvider>
        <ReviewList model={baseModel([envWithCandidate()])} onChanged={onChanged} />
      </I18nProvider>,
    );

    expect(await screen.findByText("acme-web")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("radio"));
    fireEvent.click(screen.getByRole("button", { name: "Use this" }));

    await waitFor(() =>
      expect(patchEnvironment).toHaveBeenCalledWith("env-1", {
        status: "confirmed",
        account_id: "acc-1",
        resource: { kind: "vercel_project", id: "prj_1", name: "acme-web", region: "iad1" },
      }),
    );
    await waitFor(() => expect(onChanged).toHaveBeenCalled());
  });

  it("an item with no account connected offers to connect one, which opens the dialog", async () => {
    render(
      <I18nProvider>
        <ReviewList model={baseModel([envWithNoAccount()])} onChanged={vi.fn()} />
      </I18nProvider>,
    );

    expect(
      await screen.findByText("Connect a Google Cloud account to read its deployments, logs and errors"),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Connect" }));

    expect(await screen.findByText("Connect Google Cloud")).toBeInTheDocument();
    expect(screen.getByLabelText(/service account json/i)).toBeInTheDocument();
  });
});

function checkFixture(overrides: Partial<ComponentCheck> = {}): ComponentCheck {
  return {
    id: "chk-1",
    repository_id: "repo-1",
    component_id: "c1",
    source: "ci",
    workflow: "ci.yml",
    workflow_name: "CI",
    job_key: "test",
    job_name: "Test",
    purpose: { detected: "test" },
    local_commands: {},
    gate: { detected: "required" },
    dispatchable: true,
    status: "active",
    missing: false,
    needs_review: true,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

describe("ReviewList component/check items", () => {
  beforeEach(() => {
    updateComponent.mockReset();
    updateCheck.mockReset();
  });

  it("a component item's Keep calls updateComponent with reviewed:true, Dismiss with status:dismissed", async () => {
    updateComponent.mockResolvedValue({});
    const onChanged = vi.fn();
    const newComponent = component({ id: "c-new", path: "apps/admin", role: { detected: "frontend" }, needs_review: true });
    const model: RepositoryModel = {
      ...baseModel([]),
      components: [component(), newComponent],
      review: [{ kind: "component", entity_id: "c-new", repository_id: "repo-1", component_id: "c-new", confidence: "medium" }],
    };
    render(
      <I18nProvider>
        <ReviewList model={model} onChanged={onChanged} />
      </I18nProvider>,
    );

    expect(await screen.findByText("New component apps/admin detected as Frontend — keep it?")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Keep" }));
    await waitFor(() => expect(updateComponent).toHaveBeenCalledWith("c-new", { reviewed: true }));
    await waitFor(() => expect(onChanged).toHaveBeenCalled());

    fireEvent.click(screen.getByRole("button", { name: "Ignore" }));
    await waitFor(() => expect(updateComponent).toHaveBeenCalledWith("c-new", { status: "dismissed" }));
  });

  it("a check item's OK calls updateCheck with reviewed:true, Make informative with gate:info+reviewed:true", async () => {
    updateCheck.mockResolvedValue({});
    const onChanged = vi.fn();
    const check = checkFixture();
    const model: RepositoryModel = {
      ...baseModel([]),
      checks: [check],
      review: [{ kind: "check", entity_id: "chk-1", repository_id: "repo-1", component_id: "c1", confidence: "medium" }],
    };
    render(
      <I18nProvider>
        <ReviewList model={model} onChanged={onChanged} />
      </I18nProvider>,
    );

    expect(await screen.findByText("CI now requires CI › Test before hand-off for web")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "OK" }));
    await waitFor(() => expect(updateCheck).toHaveBeenCalledWith("chk-1", { reviewed: true }));
    await waitFor(() => expect(onChanged).toHaveBeenCalled());

    fireEvent.click(screen.getByRole("button", { name: "Make informative" }));
    await waitFor(() => expect(updateCheck).toHaveBeenCalledWith("chk-1", { gate: "info", reviewed: true }));
  });
});
