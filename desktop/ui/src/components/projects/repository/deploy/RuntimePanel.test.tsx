import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { CloudAccount, ComponentEnvironment, EnvironmentRuntime } from "@/api";
import { RuntimePanel } from "@/components/projects/repository/deploy/RuntimePanel";
import { I18nProvider } from "@/hooks/useI18n";

const { getEnvironmentOverview, getEnvironmentErrors } = vi.hoisted(() => ({
  getEnvironmentOverview: vi.fn(),
  getEnvironmentErrors: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, getEnvironmentOverview, getEnvironmentErrors } };
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

const env: ComponentEnvironment = {
  id: "env-1",
  repository_id: "repo-1",
  component_id: "comp-1",
  environment: "production",
  provider: "gcp",
  account_id: "acc-1",
  status: "confirmed",
  source: "user",
  confidence: "exact",
  auto_confirmed: false,
  created_at: "2024-01-01T00:00:00Z",
  updated_at: "2024-01-01T00:00:00Z",
};

function renderPanel() {
  return render(
    <MemoryRouter>
      <I18nProvider>
        <RuntimePanel env={env} accounts={[account]} onAccountsChanged={() => {}} />
      </I18nProvider>
    </MemoryRouter>,
  );
}

describe("RuntimePanel", () => {
  beforeEach(() => {
    getEnvironmentErrors.mockReset().mockResolvedValue({ errors: [] });
  });

  it("shows resource status and the latest deployment when the overview is available", async () => {
    const overview: EnvironmentRuntime = {
      environment: env,
      detail: {
        account_id: "acc-1",
        provider: "gcp",
        ref: { kind: "cloud_run_service", id: "svc-1", name: "acme-api" },
        status: "healthy",
        revision: "00042-xoz",
      },
      deployments: [],
      errors: [],
    };
    getEnvironmentOverview.mockReset().mockResolvedValue(overview);
    renderPanel();

    await screen.findByText("00042-xoz");
    expect(screen.getByText("Healthy")).toBeInTheDocument();
  });

  it("an unavailable auth issue shows Reconnect, which opens the account dialog", async () => {
    const overview: EnvironmentRuntime = {
      environment: env,
      deployments: [],
      errors: [],
      unavailable: "The stored credential's token has expired — reconnect this account.",
    };
    getEnvironmentOverview.mockReset().mockResolvedValue(overview);
    renderPanel();

    await screen.findByText(overview.unavailable!);
    fireEvent.click(screen.getByRole("button", { name: "Reconnect" }));

    await waitFor(() => expect(screen.getByText("Replace the Google Cloud credential")).toBeInTheDocument());
  });
});
