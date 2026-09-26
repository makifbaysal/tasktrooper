import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { CloudAccount, ComponentEnvironment, EnvironmentRuntime } from "@/api";
import { RuntimePanel } from "@/components/projects/repository/deploy/RuntimePanel";
import { I18nProvider } from "@/hooks/useI18n";

const { getEnvironmentOverview, getEnvironmentErrors, getEnvironmentLogs } = vi.hoisted(() => ({
  getEnvironmentOverview: vi.fn(),
  getEnvironmentErrors: vi.fn(),
  getEnvironmentLogs: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, getEnvironmentOverview, getEnvironmentErrors, getEnvironmentLogs } };
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

const previewEnv: ComponentEnvironment = {
  ...env,
  id: "env-prev",
  environment: "preview",
  provider: "vercel",
  per_branch: true,
};

function renderPanel(target: ComponentEnvironment = env) {
  return render(
    <MemoryRouter>
      <I18nProvider>
        <RuntimePanel env={target} accounts={[account]} onAccountsChanged={() => {}} />
      </I18nProvider>
    </MemoryRouter>,
  );
}

describe("RuntimePanel", () => {
  beforeEach(() => {
    getEnvironmentErrors.mockReset().mockResolvedValue({ errors: [] });
    getEnvironmentLogs.mockReset().mockResolvedValue({ entries: [] });
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

  it("unavailable_code cloud_auth shows Reconnect, which opens the account dialog", async () => {
    const overview: EnvironmentRuntime = {
      environment: env,
      deployments: [],
      errors: [],
      unavailable: "The stored credential's token has expired — reconnect this account.",
      unavailable_code: "cloud_auth",
    };
    getEnvironmentOverview.mockReset().mockResolvedValue(overview);
    renderPanel();

    await screen.findByText(overview.unavailable!);
    fireEvent.click(screen.getByRole("button", { name: "Reconnect" }));

    await waitFor(() => expect(screen.getByText("Replace the Google Cloud credential")).toBeInTheDocument());
  });

  it("unavailable_code not_connected shows Connect instead of Reconnect", async () => {
    const overview: EnvironmentRuntime = {
      environment: env,
      deployments: [],
      errors: [],
      unavailable: "No cloud account is connected for this environment.",
      unavailable_code: "not_connected",
    };
    getEnvironmentOverview.mockReset().mockResolvedValue(overview);
    renderPanel();

    await screen.findByText(overview.unavailable!);
    expect(screen.getByRole("button", { name: "Connect" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Reconnect" })).not.toBeInTheDocument();
  });

  it("a protected per-branch env shows its protection mode and warns when no automation bypass exists", async () => {
    const overview: EnvironmentRuntime = {
      environment: previewEnv,
      deployments: [],
      errors: [],
      errors_supported: false,
      preview_access: { protected: true, mode: "vercel_authentication", bypass_configured: false },
    };
    getEnvironmentOverview.mockReset().mockResolvedValue(overview);
    renderPanel(previewEnv);

    await screen.findByText("Protected (Vercel Authentication)");
    expect(screen.getByText("Agents can't open these previews yet")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /How to bypass protection for automation/ })).toHaveAttribute(
      "href",
      "https://vercel.com/docs/deployment-protection/methods-to-bypass-deployment-protection/protection-bypass-automation",
    );
  });

  it("a protected per-branch env with a bypass configured shows no warning", async () => {
    const overview: EnvironmentRuntime = {
      environment: previewEnv,
      deployments: [],
      errors: [],
      preview_access: { protected: true, mode: "vercel_authentication", bypass_configured: true },
    };
    getEnvironmentOverview.mockReset().mockResolvedValue(overview);
    renderPanel(previewEnv);

    await screen.findByText("Protected (Vercel Authentication)");
    expect(screen.queryByText("Agents can't open these previews yet")).not.toBeInTheDocument();
  });

  it("an unprotected per-branch env reads as public", async () => {
    const overview: EnvironmentRuntime = {
      environment: previewEnv,
      deployments: [],
      errors: [],
      preview_access: { protected: false, mode: "none", bypass_configured: false },
    };
    getEnvironmentOverview.mockReset().mockResolvedValue(overview);
    renderPanel(previewEnv);

    await screen.findByText("Public");
    expect(screen.queryByText("Agents can't open these previews yet")).not.toBeInTheDocument();
  });

  it("hides the Errors tab and never asks for errors when errors_supported is false", async () => {
    const overview: EnvironmentRuntime = {
      environment: previewEnv,
      deployments: [],
      errors: [],
      errors_supported: false,
      preview_access: { protected: false, mode: "none", bypass_configured: false },
    };
    getEnvironmentOverview.mockReset().mockResolvedValue(overview);
    renderPanel(previewEnv);

    await screen.findByText("Public");
    expect(screen.queryByRole("tab", { name: "Errors" })).not.toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "Logs" })).toHaveAttribute("aria-selected", "true");
    expect(getEnvironmentErrors).not.toHaveBeenCalled();
  });

  it("keeps the Errors tab when errors_supported is absent", async () => {
    const overview: EnvironmentRuntime = { environment: env, deployments: [], errors: [] };
    getEnvironmentOverview.mockReset().mockResolvedValue(overview);
    renderPanel();

    await waitFor(() => expect(getEnvironmentErrors).toHaveBeenCalled());
    expect(screen.getByRole("tab", { name: "Errors" })).toHaveAttribute("aria-selected", "true");
  });
});
