import "@testing-library/jest-dom/vitest";
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import type { CloudAccount, Component, ComponentEnvironment } from "@/api";
import { EnvironmentsCard } from "@/components/projects/repository/deploy/EnvironmentsCard";
import { I18nProvider } from "@/hooks/useI18n";

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    api: { ...actual.api, deleteEnvironment: vi.fn(), patchEnvironment: vi.fn(), listCloudResources: vi.fn() },
  };
});

const component: Component = {
  id: "comp-1",
  repository_id: "repo-1",
  path: "services/api",
  name: { detected: "api" },
  role: { detected: "backend" },
  stack: { detected: {} },
  commands: [],
  docs: {},
  gates: {},
  status: "active",
  manually_added: false,
  needs_review: false,
  created_at: "2024-01-01T00:00:00Z",
  updated_at: "2024-01-01T00:00:00Z",
};

const account: CloudAccount = {
  id: "acc-1",
  provider: "gcp",
  label: "acme-prod",
  meta: {},
  status: "ok",
  created_at: "2024-01-01T00:00:00Z",
  updated_at: "2024-01-01T00:00:00Z",
};

function makeEnv(overrides: Partial<ComponentEnvironment>): ComponentEnvironment {
  return {
    id: "env-1",
    repository_id: "repo-1",
    component_id: "comp-1",
    environment: "production",
    status: "confirmed",
    source: "user",
    confidence: "exact",
    auto_confirmed: false,
    created_at: "2024-01-01T00:00:00Z",
    updated_at: "2024-01-01T00:00:00Z",
    ...overrides,
  };
}

function renderCard(environments: ComponentEnvironment[], accounts: CloudAccount[] = [account]) {
  return render(
    <MemoryRouter>
      <I18nProvider>
        <EnvironmentsCard
          component={component}
          environments={environments}
          accounts={accounts}
          accountsLoading={false}
          selectedEnvId={null}
          onSelectEnv={() => {}}
          onChanged={() => {}}
          onAccountsChanged={() => {}}
        />
      </I18nProvider>
    </MemoryRouter>,
  );
}

describe("EnvironmentsCard", () => {
  it("renders a bound cloud row with its resource, url and health", () => {
    renderCard([
      makeEnv({
        environment: "production",
        provider: "gcp",
        account_id: "acc-1",
        resource: { kind: "cloud_run_service", id: "svc-1", name: "acme-api", region: "europe-west1" },
        url: "https://api.acme.com",
        health: { status: "healthy", error_count_24h: 2, checked_at: "2024-01-01T00:00:00Z" },
      }),
    ]);
    expect(screen.getByText("acme-api · europe-west1")).toBeInTheDocument();
    expect(screen.getByText("https://api.acme.com")).toBeInTheDocument();
    expect(screen.getByText("2")).toBeInTheDocument();
  });

  it("renders a custom (no-provider) bound row by its URL", () => {
    renderCard([
      makeEnv({
        environment: "staging",
        url: "https://staging.example.com",
        health: { status: "unknown", error_count_24h: 0, checked_at: "2024-01-01T00:00:00Z" },
      }),
    ]);
    expect(screen.getAllByText("https://staging.example.com").length).toBeGreaterThan(0);
  });

  it("renders a suggested row with the candidate picker instead of Connect/Change", () => {
    renderCard([
      makeEnv({
        environment: "preview",
        status: "suggested",
        provider: "vercel",
        candidates: [
          { account_id: "acc-2", provider: "vercel", ref: { kind: "vercel_project", id: "prj_1", name: "web-app" } },
        ],
      }),
    ]);
    expect(screen.getByText("web-app")).toBeInTheDocument();
    expect(screen.getAllByText("needs your answer").length).toBeGreaterThan(0);
  });

  it("shows a Connect action for an unbound environment", () => {
    renderCard([]);
    expect(screen.getAllByText("Connect").length).toBeGreaterThan(0);
  });

  it("shows an empty state when no cloud accounts are connected at all", () => {
    renderCard([], []);
    expect(screen.getByText("No cloud accounts connected")).toBeInTheDocument();
    expect(screen.getByText("Connect a cloud account")).toBeInTheDocument();
  });
});
