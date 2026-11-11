import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@/hooks/useI18n";

const { telemetryStatus } = vi.hoisted(() => ({ telemetryStatus: vi.fn() }));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, telemetryStatus } };
});

import { TelemetryGate } from "@/components/operations/telemetry/TelemetryGate";

function renderGate() {
  return render(
    <MemoryRouter initialEntries={["/operations/telemetry/services"]}>
      <I18nProvider>
        <Routes>
          <Route path="/operations/telemetry" element={<TelemetryGate />}>
            <Route path="services" element={<p>services screen</p>} />
          </Route>
          <Route path="/settings/telemetry" element={<p>telemetry settings</p>} />
        </Routes>
      </I18nProvider>
    </MemoryRouter>,
  );
}

beforeEach(() => {
  telemetryStatus.mockReset();
});

describe("TelemetryGate", () => {
  it("leads to the settings section when nothing is configured", async () => {
    telemetryStatus.mockResolvedValue({ configured: false, reachable: false });
    renderGate();
    expect(await screen.findByText("Connect TaskTrooper Telemetry")).toBeInTheDocument();
    expect(screen.queryByText("services screen")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("link", { name: "Open telemetry settings" }));
    expect(await screen.findByText("telemetry settings")).toBeInTheDocument();
  });

  it("says so when the saved server does not answer, and checks again on retry", async () => {
    telemetryStatus.mockResolvedValueOnce({ configured: true, reachable: false }).mockResolvedValueOnce({ configured: true, reachable: true });
    renderGate();
    expect(await screen.findByText("Telemetry is not answering")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Open telemetry settings" })).toHaveAttribute("href", "/settings/telemetry");
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(await screen.findByText("services screen")).toBeInTheDocument();
    expect(telemetryStatus).toHaveBeenCalledTimes(2);
  });

  it("shows the screens when the product is reachable", async () => {
    telemetryStatus.mockResolvedValue({ configured: true, reachable: true });
    renderGate();
    expect(await screen.findByText("services screen")).toBeInTheDocument();
  });

  it("reports a failed status check with its message", async () => {
    telemetryStatus.mockRejectedValue(new Error("server down"));
    renderGate();
    expect(await screen.findByText("Could not check the telemetry connection")).toBeInTheDocument();
    await waitFor(() => expect(screen.getByText("server down")).toBeInTheDocument());
  });
});
