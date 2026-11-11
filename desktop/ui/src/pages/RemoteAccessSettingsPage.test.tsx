import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { DesktopRemoteAccessState } from "@/lib/desktop-bridge";
import { I18nProvider } from "@/hooks/useI18n";
import { RemoteAccessSettingsPage } from "@/pages/RemoteAccessSettingsPage";

const api = vi.hoisted(() => ({
  listRemoteDevices: vi.fn(),
  createRemotePairing: vi.fn(),
  cancelRemotePairing: vi.fn(),
  renameRemoteDevice: vi.fn(),
  revokeRemoteDevice: vi.fn(),
  remoteAccessAudit: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, ...api } };
});

const URI =
  "tasktrooper://pair?v=1&u=https%3A%2F%2Fcalm-fox-12.trycloudflare.com&c=K7QD2M9WXH4T&n=Akif%27s%20MacBook";

function state(patch: Partial<DesktopRemoteAccessState> = {}): DesktopRemoteAccessState {
  return {
    tunnel: "stopped",
    wanted: false,
    config: { provider: "cloudflare", mode: "quick", hostname: "", autoStart: false, serverName: "" },
    secrets: { cloudflareToken: false, ngrokAuthtoken: false },
    since: 1,
    restarts: 0,
    logTail: [],
    suggestedPort: 47820,
    detection: { provider: "cloudflare", installed: true, path: "/opt/homebrew/bin/cloudflared", version: "2025.10.1" },
    ...patch,
  };
}

function stubShell(initial: DesktopRemoteAccessState) {
  let current = initial;
  const listeners = new Set<(s: DesktopRemoteAccessState) => void>();
  const host = {
    state: vi.fn(() => Promise.resolve(current)),
    subscribe: vi.fn((cb: (s: DesktopRemoteAccessState) => void) => {
      listeners.add(cb);
      return () => listeners.delete(cb);
    }),
    configure: vi.fn((req: Record<string, unknown>) => {
      current = { ...current, config: { ...current.config, ...req } as never };
      return Promise.resolve(current);
    }),
    start: vi.fn(() => {
      current = { ...current, wanted: true, tunnel: "starting" };
      return Promise.resolve(current);
    }),
    stop: vi.fn(() => {
      current = { ...current, wanted: false, tunnel: "stopped" };
      return Promise.resolve(current);
    }),
    detect: vi.fn(),
    openInstallPage: vi.fn(() => Promise.resolve()),
    logs: vi.fn(() => Promise.resolve([])),
  };
  window.__tasktrooperDesktop = { remoteAccess: host } as never;
  return {
    host,
    push: (s: DesktopRemoteAccessState) => {
      current = s;
      act(() => listeners.forEach((cb) => cb(s)));
    },
  };
}

function renderPage() {
  return render(
    <I18nProvider>
      <RemoteAccessSettingsPage />
    </I18nProvider>,
  );
}

beforeEach(() => {
  api.listRemoteDevices.mockResolvedValue({ devices: [] });
  api.remoteAccessAudit.mockResolvedValue({ entries: [] });
  api.revokeRemoteDevice.mockResolvedValue(undefined);
  api.cancelRemotePairing.mockResolvedValue(undefined);
});

afterEach(() => {
  delete window.__tasktrooperDesktop;
  vi.clearAllMocks();
});

describe("RemoteAccessSettingsPage", () => {
  it("says it lives in the desktop app when there is no bridge", () => {
    renderPage();
    expect(screen.getByText(/set up in the TaskTrooper desktop app/i)).toBeTruthy();
  });

  it("shows the security note, the tunnel form and a stopped tunnel", async () => {
    stubShell(state());
    renderPage();
    expect(await screen.findByText("Cloudflare Tunnel")).toBeTruthy();
    expect(screen.getByText(/Read this before you pair a phone/i)).toBeTruthy();
    expect(screen.getByTestId("tunnel-status").textContent).toBe("Stopped");
    expect(screen.getByText(/cloudflared 2025.10.1 found/i)).toBeTruthy();
    expect(screen.getByText(/Start the tunnel first/i)).toBeTruthy();
  });

  it("starts and stops the tunnel through the bridge", async () => {
    const { host } = stubShell(state());
    renderPage();
    fireEvent.click(await screen.findByRole("button", { name: "Start tunnel" }));
    await waitFor(() => expect(host.start).toHaveBeenCalledOnce());
    fireEvent.click(await screen.findByRole("button", { name: "Stop tunnel" }));
    await waitFor(() => expect(host.stop).toHaveBeenCalledOnce());
  });

  it("tells a person who lacks the program how to install it, and never installs it itself", async () => {
    const { host } = stubShell(
      state({
        tunnel: "binary_missing",
        detection: { provider: "cloudflare", installed: false, installCommand: "brew install cloudflared", detail: "cloudflared was not found on this computer." },
      }),
    );
    renderPage();
    expect(await screen.findByText("brew install cloudflared")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: /Open download page/i }));
    expect(host.openInstallPage).toHaveBeenCalledWith("cloudflare");
  });

  it("sends a typed token to the bridge once and never shows a stored one", async () => {
    const { host } = stubShell(state({ config: { provider: "ngrok", mode: "quick", hostname: "", autoStart: false, serverName: "" } }));
    renderPage();
    const input = (await screen.findByLabelText("ngrok authtoken")) as HTMLInputElement;
    expect(input.type).toBe("password");
    fireEvent.change(input, { target: { value: "2abcDEF1234ghi_5JkLmNoPqRsTuVwXyZ12" } });
    fireEvent.click(screen.getByRole("button", { name: "Save settings" }));
    await waitFor(() => expect(host.configure).toHaveBeenCalled());
    expect(host.configure.mock.calls[0]?.[0]).toMatchObject({ provider: "ngrok", ngrokAuthtoken: "2abcDEF1234ghi_5JkLmNoPqRsTuVwXyZ12" });
    await waitFor(() => expect((screen.getByLabelText("ngrok authtoken") as HTMLInputElement).value).toBe(""));
  });

  it("asks for a hostname in own-domain mode and shows the Cloudflare service to point at", async () => {
    stubShell(state({ config: { provider: "cloudflare", mode: "named", hostname: "", autoStart: false, serverName: "" }, remotePort: 47820 }));
    renderPage();
    expect(await screen.findByLabelText("Public hostname")).toBeTruthy();
    expect(screen.getByText(/http:\/\/127\.0\.0\.1:47820/)).toBeTruthy();
    expect(screen.getByLabelText("Tunnel token")).toBeTruthy();
  });

  it("creates a pairing for the live tunnel address and shows the QR and the typed code", async () => {
    const shell = stubShell(state({ tunnel: "connected", wanted: true, url: "https://calm-fox-12.trycloudflare.com" }));
    api.createRemotePairing.mockResolvedValue({
      id: "p1",
      code: "K7QD2M9WXH4T",
      code_display: "K7QD-2M9W-XH4T",
      uri: URI,
      expires_at: new Date(Date.now() + 5 * 60_000).toISOString(),
    });
    renderPage();
    fireEvent.click(await screen.findByRole("button", { name: "Show pairing code" }));
    await waitFor(() => expect(api.createRemotePairing).toHaveBeenCalled());
    expect(api.createRemotePairing.mock.calls[0]?.[0]).toMatchObject({
      role: "operator",
      expires_in_days: 30,
      public_url: "https://calm-fox-12.trycloudflare.com",
    });
    expect((await screen.findByTestId("pairing-code")).textContent).toBe("K7QD-2M9W-XH4T");
    expect(screen.getByRole("img", { name: "Scan with the TaskTrooper app" })).toBeTruthy();
    expect(screen.getByText(/Waiting for the phone/)).toBeTruthy();

    shell.push(state({ tunnel: "stopped", wanted: false }));
    await waitFor(() => expect(screen.queryByTestId("pairing-code")).toBeNull());
  });

  it("recognises the phone that paired", async () => {
    stubShell(state({ tunnel: "connected", wanted: true, url: "https://calm-fox-12.trycloudflare.com" }));
    api.createRemotePairing.mockResolvedValue({
      id: "p1",
      code: "K7QD2M9WXH4T",
      code_display: "K7QD-2M9W-XH4T",
      uri: URI,
      expires_at: new Date(Date.now() + 5 * 60_000).toISOString(),
    });
    renderPage();
    fireEvent.click(await screen.findByRole("button", { name: "Show pairing code" }));
    await screen.findByTestId("pairing-code");
    api.listRemoteDevices.mockResolvedValue({
      devices: [{ id: "d1", name: "Akif's iPhone", role: "operator", created_at: new Date().toISOString() }],
    });
    expect(await screen.findByText("Akif's iPhone is paired.", {}, { timeout: 4_000 })).toBeTruthy();
  }, 8_000);

  it("lists paired phones and revokes one after confirmation", async () => {
    stubShell(state());
    api.listRemoteDevices.mockResolvedValue({
      devices: [
        {
          id: "d1",
          name: "Akif's iPhone",
          platform: "ios",
          role: "viewer",
          created_at: new Date().toISOString(),
          last_seen_at: new Date(Date.now() - 5 * 60_000).toISOString(),
          expires_at: null,
        },
      ],
    });
    renderPage();
    expect(await screen.findByText("Akif's iPhone")).toBeTruthy();
    expect(screen.getByText("View only")).toBeTruthy();
    expect(screen.getByText(/Last seen 5 min ago/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Revoke" }));
    const dialogButtons = await screen.findAllByRole("button", { name: "Revoke" });
    fireEvent.click(dialogButtons[dialogButtons.length - 1] as HTMLElement);
    await waitFor(() => expect(api.revokeRemoteDevice).toHaveBeenCalledWith("d1"));
  });

  it("renders the audit log without request bodies", async () => {
    stubShell(state());
    api.remoteAccessAudit.mockResolvedValue({
      entries: [
        { id: 1, at: new Date().toISOString(), device_name: "Akif's iPhone", method: "POST", route: "/v1/sessions/:id/messages", status: 202, kind: "request" },
        { id: 2, at: new Date().toISOString(), kind: "auth_failed", status: 401 },
      ],
    });
    renderPage();
    expect(await screen.findByText("POST /v1/sessions/:id/messages")).toBeTruthy();
    expect(screen.getByText("Refused: bad credentials")).toBeTruthy();
    expect(screen.getAllByTestId("audit-row")).toHaveLength(2);
  });
});
