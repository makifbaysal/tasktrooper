import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { CloudAccount, CloudResource } from "@/api";
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

const resource: CloudResource = {
  account_id: "acc-1",
  provider: "gcp",
  ref: { kind: "cloud_run_service", id: "svc-1", name: "acme-api", region: "europe-west1" },
};

function renderDialog(onBound = vi.fn()) {
  render(
    <I18nProvider>
      <BindEnvironmentDialog
        open
        onOpenChange={() => {}}
        componentId="comp-1"
        environment="production"
        accounts={[account]}
        onBound={onBound}
      />
    </I18nProvider>,
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
});
