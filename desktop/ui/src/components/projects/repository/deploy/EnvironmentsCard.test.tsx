import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import type { CloudAccount, ComponentDelivery, ComponentEnvironment } from "@/api";
import { EnvironmentsCard } from "@/components/projects/repository/deploy/EnvironmentsCard";
import { I18nProvider } from "@/hooks/useI18n";

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    api: { ...actual.api, deleteEnvironment: vi.fn(), patchEnvironment: vi.fn(), listCloudResources: vi.fn() },
  };
});

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

function renderCard(
  environments: ComponentEnvironment[],
  accounts: CloudAccount[] = [account],
  options: { delivery?: ComponentDelivery | null; onRequestBind?: (env: ComponentEnvironment["environment"], existing?: ComponentEnvironment) => void } = {},
) {
  const onRequestBind = options.onRequestBind ?? vi.fn();
  return {
    onRequestBind,
    ...render(
      <MemoryRouter>
        <I18nProvider>
          <EnvironmentsCard
            environments={environments}
            accounts={accounts}
            accountsLoading={false}
            selectedEnvId={null}
            onSelectEnv={() => {}}
            delivery={options.delivery ?? null}
            onRequestBind={onRequestBind}
            onChanged={() => {}}
            onAccountsChanged={() => {}}
          />
        </I18nProvider>
      </MemoryRouter>,
    ),
  };
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

  it("asks the caller to open the bind dialog instead of owning it", () => {
    const { onRequestBind } = renderCard([]);
    fireEvent.click(screen.getAllByText("Connect")[0]);
    expect(onRequestBind).toHaveBeenCalledWith("production", undefined);
  });

  it("shows a deploy badge on the production row when the delivery profile deploys there", () => {
    const delivery: ComponentDelivery = { mode: "on_merge", executor: "vercel", verify: {}, auto_rollback: true };
    renderCard(
      [
        makeEnv({
          environment: "production",
          provider: "vercel",
          resource: { kind: "vercel_project", id: "prj_1", name: "pishio-web" },
        }),
      ],
      [account],
      { delivery },
    );
    expect(screen.getByText("Deploy: On merge · Vercel")).toBeInTheDocument();
  });

  it("does not show a deploy badge when delivery mode is none", () => {
    const delivery: ComponentDelivery = { mode: "none", verify: {}, auto_rollback: true };
    renderCard([makeEnv({ environment: "production", provider: "vercel" })], [account], { delivery });
    expect(screen.queryByText(/^Deploy:/)).not.toBeInTheDocument();
  });

  it("warns more strongly before disconnecting production when a delivery profile deploys there", () => {
    const delivery: ComponentDelivery = { mode: "on_merge", executor: "vercel", verify: {}, auto_rollback: true };
    renderCard([makeEnv({ environment: "production" })], [account], { delivery });
    fireEvent.click(screen.getByText("Disconnect"));
    expect(
      screen.getByText("The delivery profile deploys here — after disconnecting, deploys can't be verified or rolled back."),
    ).toBeInTheDocument();
  });
});
