import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@/hooks/useI18n";

const { telemetrySettings, saveTelemetrySettings, testTelemetry } = vi.hoisted(() => ({
  telemetrySettings: vi.fn(),
  saveTelemetrySettings: vi.fn(),
  testTelemetry: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, telemetrySettings, saveTelemetrySettings, testTelemetry } };
});
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

import { TelemetryCard } from "@/components/admin/TelemetryCard";

function renderCard() {
  return render(
    <I18nProvider>
      <TelemetryCard />
    </I18nProvider>,
  );
}

beforeEach(() => {
  telemetrySettings.mockReset();
  saveTelemetrySettings.mockReset();
  testTelemetry.mockReset();
});

describe("TelemetryCard", () => {
  it("starts empty and not configured", async () => {
    telemetrySettings.mockResolvedValue({ base_url: "", configured: false });
    renderCard();
    expect(await screen.findByLabelText("API address")).toHaveValue("");
    expect(screen.getByText("Not configured")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Test connection" })).toBeDisabled();
    expect(screen.queryByRole("button", { name: "Remove key" })).not.toBeInTheDocument();
  });

  it("saves the address and key, then forgets the key it typed", async () => {
    telemetrySettings.mockResolvedValue({ base_url: "", configured: false });
    saveTelemetrySettings.mockResolvedValue({ base_url: "https://t.example.com", configured: true });
    renderCard();
    fireEvent.change(await screen.findByLabelText("API address"), { target: { value: " https://t.example.com " } });
    fireEvent.change(screen.getByLabelText("API key"), { target: { value: "tk_secret" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(saveTelemetrySettings).toHaveBeenCalledWith({ base_url: "https://t.example.com", api_key: "tk_secret" }));
    await waitFor(() => expect(screen.getByLabelText("API key")).toHaveValue(""));
    expect(screen.getByText("Configured")).toBeInTheDocument();
    expect(screen.getByLabelText("API key")).toHaveAttribute("type", "password");
  });

  it("leaves the key out of an address-only save", async () => {
    telemetrySettings.mockResolvedValue({ base_url: "https://t.example.com", configured: true });
    saveTelemetrySettings.mockResolvedValue({ base_url: "https://other.example.com", configured: true });
    renderCard();
    fireEvent.change(await screen.findByLabelText("API address"), { target: { value: "https://other.example.com" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(saveTelemetrySettings).toHaveBeenCalledWith({ base_url: "https://other.example.com" }));
  });

  it("tests the saved connection and shows the result", async () => {
    telemetrySettings.mockResolvedValue({ base_url: "https://t.example.com", configured: true });
    testTelemetry.mockResolvedValueOnce({ ok: true }).mockResolvedValueOnce({ ok: false, error: "401 unauthorized" });
    renderCard();
    fireEvent.click(await screen.findByRole("button", { name: "Test connection" }));
    expect(await screen.findByText("The telemetry server answered")).toBeInTheDocument();
    expect(screen.getByText("Connected")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Test connection" }));
    expect(await screen.findByText("401 unauthorized")).toBeInTheDocument();
    expect(screen.getByText("Not reachable")).toBeInTheDocument();
  });

  it("clears only the key", async () => {
    telemetrySettings.mockResolvedValue({ base_url: "https://t.example.com", configured: true });
    saveTelemetrySettings.mockResolvedValue({ base_url: "https://t.example.com", configured: false });
    renderCard();
    fireEvent.click(await screen.findByRole("button", { name: "Remove key" }));
    await waitFor(() => expect(saveTelemetrySettings).toHaveBeenCalledWith({ base_url: "https://t.example.com", api_key: "" }));
    expect(await screen.findByText("Not configured")).toBeInTheDocument();
  });
});

describe("TelemetryCard test before saving", () => {
  it("tests what is typed without saving it", async () => {
    telemetrySettings.mockResolvedValue({ base_url: "", configured: false });
    testTelemetry.mockResolvedValue({ ok: true });
    renderCard();
    fireEvent.change(await screen.findByLabelText("API address"), { target: { value: "https://t.example.com" } });
    expect(screen.getByRole("button", { name: "Test connection" })).toBeDisabled();
    fireEvent.change(screen.getByLabelText("API key"), { target: { value: "tk_new" } });
    fireEvent.click(screen.getByRole("button", { name: "Test connection" }));
    await waitFor(() => expect(testTelemetry).toHaveBeenCalledWith({ base_url: "https://t.example.com", api_key: "tk_new" }));
    expect(saveTelemetrySettings).not.toHaveBeenCalled();
  });
});
