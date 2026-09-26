import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { CloudAccount, CloudResource, ComponentEnvironment } from "@/api";
import { BindEnvironmentDialog } from "@/components/projects/repository/deploy/BindEnvironmentDialog";
import { I18nProvider } from "@/hooks/useI18n";

const { listCloudResources, bindEnvironment } = vi.hoisted(() => ({
  listCloudResources: vi.fn(),
  bindEnvironment: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, listCloudResources, bindEnvironment } };
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

const vercelAccount: CloudAccount = {
  id: "acc-2",
  provider: "vercel",
  label: "acme-vercel",
  meta: {},
  status: "ok",
  created_at: "2024-01-01T00:00:00Z",
  updated_at: "2024-01-01T00:00:00Z",
};

const vercelAccount2: CloudAccount = {
  id: "acc-3",
  provider: "vercel",
  label: "acme-vercel-2",
  meta: {},
  status: "ok",
  created_at: "2024-01-01T00:00:00Z",
  updated_at: "2024-01-01T00:00:00Z",
};

const resource: CloudResource = {
  account_id: "acc-1",
  provider: "gcp",
  ref: { kind: "cloud_run_service", id: "svc-1", name: "acme-api", region: "europe-west1" },
};

const vercelResource: CloudResource = {
  account_id: "acc-2",
  provider: "vercel",
  ref: { kind: "vercel_project", id: "prj_1", name: "pishio-web" },
};

function renderDialog(
  options: {
    onBound?: (env: unknown) => void;
    accounts?: CloudAccount[];
    provider?: "vercel" | "gcp" | "aws";
    existing?: ComponentEnvironment;
  } = {},
) {
  const onBound = options.onBound ?? vi.fn();
  render(
    <MemoryRouter>
      <I18nProvider>
        <BindEnvironmentDialog
          open
          onOpenChange={() => {}}
          componentId="comp-1"
          environment="production"
          accounts={options.accounts ?? [account]}
          provider={options.provider}
          existing={options.existing}
          onBound={onBound}
        />
      </I18nProvider>
    </MemoryRouter>,
  );
  return { onBound };
}

describe("BindEnvironmentDialog", () => {
  beforeEach(() => {
    listCloudResources.mockReset().mockResolvedValue({ resources: [resource] });
    bindEnvironment.mockReset().mockResolvedValue({ id: "env-1" });
  });

  it("lists the chosen account's resources and submits bindEnvironment with {account_id, resource}", async () => {
    const { onBound } = renderDialog();

    fireEvent.click(screen.getByText("acme-prod"));
    await waitFor(() => expect(listCloudResources).toHaveBeenCalledWith("acc-1", { refresh: false }));
    await screen.findByText("acme-api");

    fireEvent.click(screen.getByRole("radio"));
    fireEvent.click(screen.getByRole("button", { name: "Connect" }));

    await waitFor(() =>
      expect(bindEnvironment).toHaveBeenCalledWith("comp-1", "production", { account_id: "acc-1", resource: resource.ref }),
    );
    await waitFor(() => expect(onBound).toHaveBeenCalledWith({ id: "env-1" }));
  });

  it("refreshes the resource list past the server's cache", async () => {
    renderDialog();
    fireEvent.click(screen.getByText("acme-prod"));
    await screen.findByText("acme-api");

    fireEvent.click(screen.getByRole("button", { name: /Refresh/i }));
    await waitFor(() => expect(listCloudResources).toHaveBeenLastCalledWith("acc-1", { refresh: true }));
  });

  it("submits a custom URL binding with {url, health_url}", async () => {
    const { onBound } = renderDialog();

    fireEvent.click(screen.getByText("Custom URL"));
    fireEvent.change(screen.getByLabelText("URL"), { target: { value: "https://custom.example.com" } });
    fireEvent.change(screen.getByLabelText(/Health check URL/i), {
      target: { value: "https://custom.example.com/health" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Connect" }));

    await waitFor(() =>
      expect(bindEnvironment).toHaveBeenCalledWith("comp-1", "production", {
        url: "https://custom.example.com",
        health_url: "https://custom.example.com/health",
      }),
    );
    await waitFor(() => expect(onBound).toHaveBeenCalled());
  });

  it("with a provider set, offers only that provider's accounts", async () => {
    renderDialog({ accounts: [account, vercelAccount, vercelAccount2], provider: "vercel" });
    expect(screen.queryByText("acme-prod")).not.toBeInTheDocument();
    expect(screen.getByText("acme-vercel")).toBeInTheDocument();
    expect(screen.getByText("acme-vercel-2")).toBeInTheDocument();
  });

  it("with a provider set and exactly one matching account, skips straight to its resources", async () => {
    listCloudResources.mockResolvedValue({ resources: [vercelResource] });
    renderDialog({ accounts: [account, vercelAccount], provider: "vercel" });

    await waitFor(() => expect(listCloudResources).toHaveBeenCalledWith("acc-2", { refresh: false }));
    await screen.findByText("pishio-web");
    expect(screen.queryByText("acme-vercel")).not.toBeInTheDocument();
  });

  it("with a provider set, does not seed from an existing row on another provider", async () => {
    const gcpProd = {
      id: "env-9",
      component_id: "comp-1",
      environment: "production",
      provider: "gcp",
      account_id: "acc-1",
      resource: resource.ref,
      status: "confirmed",
    } as unknown as ComponentEnvironment;
    renderDialog({ accounts: [account, vercelAccount, vercelAccount2], provider: "vercel", existing: gcpProd });

    expect(screen.getByText("acme-vercel")).toBeInTheDocument();
    expect(listCloudResources).not.toHaveBeenCalledWith("acc-1", expect.anything());
  });

  it("with a provider set and no matching account, points to integrations instead of a custom URL", () => {
    renderDialog({ accounts: [account], provider: "vercel" });

    expect(screen.getByText("No Vercel account connected yet.")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Settings › Integrations" })).toHaveAttribute(
      "href",
      "/settings/integrations",
    );
    expect(screen.queryByText("Custom URL")).not.toBeInTheDocument();
  });
});
