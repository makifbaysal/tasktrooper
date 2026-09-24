import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi, beforeEach } from "vitest";
import { ApiError, type CloudAccount } from "@/api";
import { CloudAccountDialog } from "@/components/admin/CloudAccountDialog";
import { I18nProvider } from "@/hooks/useI18n";

const { createCloudAccount } = vi.hoisted(() => ({
  createCloudAccount: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    api: { ...actual.api, createCloudAccount },
  };
});

function account(overrides: Partial<CloudAccount> = {}): CloudAccount {
  return {
    id: "acc-1",
    provider: "vercel",
    label: "Production Vercel",
    meta: {},
    status: "ok",
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

function renderDialog(provider: CloudAccount["provider"], onSaved = vi.fn()) {
  render(
    <I18nProvider>
      <CloudAccountDialog open onOpenChange={vi.fn()} provider={provider} onSaved={onSaved} />
    </I18nProvider>,
  );
  return onSaved;
}

describe("CloudAccountDialog", () => {
  beforeEach(() => {
    Element.prototype.scrollIntoView = vi.fn();
    createCloudAccount.mockReset();
  });

  it("sends the vercel token and team id as fields", async () => {
    createCloudAccount.mockResolvedValue(account());
    const onSaved = renderDialog("vercel");

    fireEvent.change(await screen.findByLabelText(/access token/i), { target: { value: "tok_abc" } });
    fireEvent.change(screen.getByLabelText(/team id/i), { target: { value: "team_123" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(createCloudAccount).toHaveBeenCalled());
    expect(createCloudAccount).toHaveBeenCalledWith({
      provider: "vercel",
      label: undefined,
      fields: { token: "tok_abc", team_id: "team_123" },
    });
    await waitFor(() => expect(onSaved).toHaveBeenCalledWith(account()));
  });

  it("sends the gcp service account json as fields", async () => {
    createCloudAccount.mockResolvedValue(account({ provider: "gcp" }));
    renderDialog("gcp");

    fireEvent.change(await screen.findByLabelText(/service account json/i), {
      target: { value: '{"type":"service_account"}' },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(createCloudAccount).toHaveBeenCalled());
    expect(createCloudAccount).toHaveBeenCalledWith({
      provider: "gcp",
      label: undefined,
      fields: { service_account_json: '{"type":"service_account"}' },
    });
  });

  it("sends the aws keys and region as fields, session token omitted when blank", async () => {
    createCloudAccount.mockResolvedValue(account({ provider: "aws" }));
    renderDialog("aws");

    fireEvent.change(await screen.findByLabelText(/access key id/i), { target: { value: "AKIA123" } });
    fireEvent.change(screen.getByLabelText(/secret access key/i), { target: { value: "secret" } });
    fireEvent.click(screen.getByLabelText(/^region$/i));
    fireEvent.click(await screen.findByRole("option", { name: "us-east-1" }));
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(createCloudAccount).toHaveBeenCalled());
    expect(createCloudAccount).toHaveBeenCalledWith({
      provider: "aws",
      label: undefined,
      fields: { access_key_id: "AKIA123", secret_access_key: "secret", region: "us-east-1" },
    });
  });

  it("shows the cloud_auth error inline instead of a toast, without closing", async () => {
    createCloudAccount.mockRejectedValue(new ApiError("Vercel rejected this token", 400, "cloud_auth"));
    const onSaved = renderDialog("vercel");

    fireEvent.change(await screen.findByLabelText(/access token/i), { target: { value: "bad-token" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    expect(await screen.findByText("Vercel rejected this token")).toBeInTheDocument();
    expect(onSaved).not.toHaveBeenCalled();
  });
});
