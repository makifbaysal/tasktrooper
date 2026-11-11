import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { DesktopRemoteAccessState } from "@/lib/desktop-bridge";
import { I18nProvider } from "@/hooks/useI18n";
import { RemoteAccessSettingsPage } from "@/pages/RemoteAccessSettingsPage";

const api = vi.hoisted(() => ({
  listRemoteDevices: vi.fn(),
  remoteAccessStatus: vi.fn(),
  createRemotePairing: vi.fn(),
  cancelRemotePairing: vi.fn(),
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
    secrets: { ngrokAuthtoken: false, ngrokApiKey: false },
    cloudflare: { loggedIn: false, phase: "idle", suggestedSubdomain: "tasktrooper-abc123" },
    ngrok: { verified: false, phase: "idle", domains: [] },
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
    refreshAccount: vi.fn(() => Promise.resolve(current)),
    cloudflareLogin: vi.fn(() => {
      current = { ...current, cloudflare: { ...current.cloudflare, phase: "waiting", loginUrl: "https://dash.cloudflare.com/argotunnel?aud=x" } };
      return Promise.resolve(current);
    }),
    cloudflareLoginCancel: vi.fn(() => Promise.resolve(current)),
    cloudflareLogout: vi.fn(() => {
      current = { ...current, cloudflare: { ...current.cloudflare, loggedIn: false } };
      return Promise.resolve(current);
    }),
    ngrokConnect: vi.fn(() => Promise.resolve(current)),
    ngrokDisconnect: vi.fn(() => Promise.resolve(current)),
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
  api.remoteAccessStatus.mockResolvedValue({ enabled: true, port: 47820, server_name: "Akif's MacBook", device_count: 0, pending_pairings: 0 });
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

  it("shows the security note, the tunnel form and a stopped tunnel with the pair button waiting for it", async () => {
    stubShell(state());
    renderPage();
    expect(await screen.findByText("Cloudflare Tunnel")).toBeTruthy();
    expect(screen.getByText(/Read this before you pair a phone/i)).toBeTruthy();
    expect(screen.getByTestId("tunnel-status").textContent).toBe("Stopped");
    expect(screen.getByText(/cloudflared 2025.10.1 found/i)).toBeTruthy();
    expect((screen.getByRole("button", { name: "Pair phone" }) as HTMLButtonElement).disabled).toBe(true);
    expect(screen.getByText("Start the tunnel to pair a phone.")).toBeTruthy();
  });

  it("has no always-visible pairing card and no list of paired phones", async () => {
    stubShell(state({ tunnel: "connected", wanted: true, url: "https://calm-fox-12.trycloudflare.com" }));
    api.listRemoteDevices.mockResolvedValue({ devices: [{ id: "d1", name: "Akif's iPhone", role: "operator", created_at: new Date().toISOString() }] });
    renderPage();
    await screen.findByText("Paired: Akif's iPhone");
    expect(screen.queryByText("Paired phones")).toBeNull();
    expect(screen.queryByText("Pair a phone")).toBeNull();
    expect(screen.queryByTestId("pairing-code")).toBeNull();
    expect(screen.queryAllByTestId("device-row")).toHaveLength(0);
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

  describe("a connected tunnel", () => {
    const connected = () =>
      state({
        tunnel: "connected",
        wanted: true,
        url: "https://calm-fox-12.trycloudflare.com",
        config: { provider: "cloudflare", mode: "quick", hostname: "", autoStart: true, serverName: "My Mac" },
        logTail: ["INF Registered tunnel connection"],
      });

    it("collapses to the status, the address, the phone line and three actions", async () => {
      stubShell(connected());
      renderPage();
      expect(await screen.findByTestId("tunnel-summary")).toBeTruthy();
      expect(screen.getByTestId("tunnel-status").textContent).toBe("Connected");
      expect(screen.getByTestId("tunnel-url").textContent).toBe("https://calm-fox-12.trycloudflare.com");
      expect(screen.getByRole("button", { name: "Copy" })).toBeTruthy();
      expect(screen.getByText("Not paired")).toBeTruthy();
      expect(screen.getByRole("button", { name: "Stop tunnel" })).toBeTruthy();
      expect(screen.getByRole("button", { name: "Edit" })).toBeTruthy();
      // Everything about setting it up is out of the way.
      for (const label of ["Provider", "Address", "Name shown on the phone"]) expect(screen.queryByLabelText(label)).toBeNull();
      expect(screen.queryByRole("button", { name: "Save settings" })).toBeNull();
      expect(screen.queryByTestId("tunnel-binary")).toBeNull();
      expect(screen.queryByTestId("tunnel-logs")).toBeNull();
      expect(screen.queryByLabelText("Start the tunnel when TaskTrooper launches")).toBeNull();
    });

    it("stops the tunnel from the summary", async () => {
      const { host } = stubShell(connected());
      renderPage();
      fireEvent.click(await screen.findByRole("button", { name: "Stop tunnel" }));
      await waitFor(() => expect(host.stop).toHaveBeenCalledOnce());
      // A stopped tunnel is the first-setup form again.
      expect(await screen.findByLabelText("Provider")).toBeTruthy();
      expect(screen.queryByTestId("tunnel-summary")).toBeNull();
    });

    it("Edit opens the whole form with the fixed settings locked, the output and the start-up switch usable, and Done closes it", async () => {
      const { host } = stubShell(connected());
      renderPage();
      fireEvent.click(await screen.findByRole("button", { name: "Edit" }));
      expect(await screen.findByText(/Stop it to change the provider/)).toBeTruthy();
      expect(screen.getByLabelText("Provider").hasAttribute("disabled") || screen.getByLabelText("Provider").getAttribute("data-disabled") !== null).toBe(true);
      expect((screen.getByLabelText("Name shown on the phone") as HTMLInputElement).disabled).toBe(true);
      expect(screen.getByTestId("tunnel-logs").textContent).toContain("Registered tunnel connection");
      expect(screen.getByTestId("tunnel-binary")).toBeTruthy();
      fireEvent.click(screen.getByRole("switch", { name: /Start the tunnel when TaskTrooper launches/ }));
      await waitFor(() => expect(host.configure).toHaveBeenCalledWith({ autoStart: false }));

      fireEvent.click(screen.getByRole("button", { name: "Done" }));
      expect(await screen.findByTestId("tunnel-summary")).toBeTruthy();
    });

    it("stays compact while the connection drops and comes back, and falls back to the form on a real problem", async () => {
      const { push } = stubShell(connected());
      renderPage();
      await screen.findByTestId("tunnel-summary");
      push({ ...connected(), tunnel: "reconnecting", restarts: 2, error: "cloudflared exited (code 1). Restarting in 1 s.", problem: "exited" });
      expect(await screen.findByText("Reconnecting")).toBeTruthy();
      expect(screen.getByTestId("tunnel-summary")).toBeTruthy();
      expect(screen.getByText(/cloudflared exited/)).toBeTruthy();
      push({ ...connected(), tunnel: "error", error: "The tunnel stopped for good.", problem: "auth_failed" });
      await waitFor(() => expect(screen.queryByTestId("tunnel-summary")).toBeNull());
      expect(screen.getByText("The tunnel stopped for good.")).toBeTruthy();
      expect(screen.getByLabelText("Provider")).toBeTruthy();
    });
  });

  describe("Cloudflare, own domain", () => {
    const named = { provider: "cloudflare", mode: "named", hostname: "", autoStart: false, serverName: "" } as const;

    it("asks for a login instead of a token and a hostname, and starts the login through the bridge", async () => {
      const { host } = stubShell(state({ config: named }));
      renderPage();
      expect(await screen.findByRole("button", { name: "Log in to Cloudflare" })).toBeTruthy();
      expect(screen.queryByLabelText("Tunnel token")).toBeNull();
      expect(screen.queryByLabelText("Public hostname")).toBeNull();
      expect(screen.queryByLabelText("Subdomain")).toBeNull();
      fireEvent.click(screen.getByRole("button", { name: "Log in to Cloudflare" }));
      await waitFor(() => expect(host.cloudflareLogin).toHaveBeenCalledOnce());
      expect(await screen.findByText("Waiting for the browser…")).toBeTruthy();
      expect(screen.getByTestId("cloudflare-login-url").getAttribute("href")).toBe("https://dash.cloudflare.com/argotunnel?aud=x");
      fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
      await waitFor(() => expect(host.cloudflareLoginCancel).toHaveBeenCalledOnce());
    });

    it("keeps the unsaved 'My own domain' choice when the login finishes and the domain arrives", async () => {
      const quick = { provider: "cloudflare", mode: "quick", hostname: "", autoStart: false, serverName: "" } as const;
      // Radix Select needs these from a real browser.
      Element.prototype.scrollIntoView = vi.fn();
      Element.prototype.hasPointerCapture = vi.fn(() => false);
      Element.prototype.releasePointerCapture = vi.fn();
      const { push } = stubShell(state({ config: quick }));
      renderPage();
      const trigger = await screen.findByRole("combobox", { name: "Address" });
      fireEvent.keyDown(trigger, { key: "ArrowDown" });
      fireEvent.click(await screen.findByRole("option", { name: "My own domain" }));
      expect(await screen.findByRole("button", { name: "Log in to Cloudflare" })).toBeTruthy();

      // The browser login ends: main pushes the new state, config still says "quick".
      push(state({ config: quick, cloudflare: { loggedIn: true, domain: "example.com", phase: "idle", suggestedSubdomain: "tasktrooper-abc123" } }));
      expect(await screen.findByText("Logged in to Cloudflare")).toBeTruthy();
      expect(screen.getByRole("combobox", { name: "Address" }).textContent).toContain("My own domain");
      expect((screen.getByLabelText("Subdomain") as HTMLInputElement).value).toBe("tasktrooper-abc123");
    });

    it("shows a failed login with the reason", async () => {
      stubShell(state({ config: named, cloudflare: { loggedIn: false, phase: "failed", error: "The Cloudflare login did not finish. Try again.", suggestedSubdomain: "tasktrooper-abc123" } }));
      renderPage();
      expect(await screen.findByText("The Cloudflare login did not finish. Try again.")).toBeTruthy();
      expect(screen.getByRole("button", { name: "Log in to Cloudflare" })).toBeTruthy();
    });

    it("shows the logged-in domain and only asks for a subdomain, prefilled; Start saves the full address first", async () => {
      const { host } = stubShell(state({ config: named, cloudflare: { loggedIn: true, domain: "example.com", phase: "idle", suggestedSubdomain: "tasktrooper-abc123" } }));
      renderPage();
      expect(await screen.findByText("Logged in to Cloudflare")).toBeTruthy();
      expect(screen.getByText("Domain: example.com")).toBeTruthy();
      const input = screen.getByLabelText("Subdomain") as HTMLInputElement;
      expect(input.value).toBe("tasktrooper-abc123");
      expect(screen.getByText("Phone address: https://tasktrooper-abc123.example.com")).toBeTruthy();

      fireEvent.change(input, { target: { value: "Phone" } });
      fireEvent.click(screen.getByRole("button", { name: "Start tunnel" }));
      await waitFor(() => expect(host.start).toHaveBeenCalledOnce());
      expect(host.configure).toHaveBeenCalledWith({ provider: "cloudflare", mode: "named", hostname: "phone.example.com", serverName: "" });
      expect(host.configure.mock.invocationCallOrder[0]).toBeLessThan(host.start.mock.invocationCallOrder[0] as number);
    });

    it("keeps a saved subdomain and refuses one that is not a single label", async () => {
      const { host } = stubShell(state({ config: { ...named, hostname: "phone.example.com" }, cloudflare: { loggedIn: true, domain: "example.com", phase: "idle", suggestedSubdomain: "tasktrooper-abc123" } }));
      renderPage();
      const input = (await screen.findByLabelText("Subdomain")) as HTMLInputElement;
      expect(input.value).toBe("phone");
      fireEvent.change(input, { target: { value: "a.b" } });
      expect(screen.getByText(/one word of letters/i)).toBeTruthy();
      expect((screen.getByRole("button", { name: "Save settings" }) as HTMLButtonElement).disabled).toBe(true);
      expect((screen.getByRole("button", { name: "Start tunnel" }) as HTMLButtonElement).disabled).toBe(true);
      expect(host.start).not.toHaveBeenCalled();
    });

    it("asks for the whole address when Cloudflare would not say the domain", async () => {
      stubShell(state({ config: named, cloudflare: { loggedIn: true, domainError: "Could not read the domain from Cloudflare (HTTP 403).", phase: "idle", suggestedSubdomain: "tasktrooper-abc123" } }));
      renderPage();
      expect(await screen.findByLabelText("Full address")).toBeTruthy();
      expect(screen.getByText(/HTTP 403/)).toBeTruthy();
      expect(screen.queryByLabelText("Subdomain")).toBeNull();
    });

    it("logs out after a confirmation, and not while the tunnel runs", async () => {
      const { host, push } = stubShell(state({ config: named, cloudflare: { loggedIn: true, domain: "example.com", phase: "idle", suggestedSubdomain: "tasktrooper-abc123" } }));
      renderPage();
      fireEvent.click(await screen.findByRole("button", { name: "Log out" }));
      expect(await screen.findByText("Log out of Cloudflare?")).toBeTruthy();
      const buttons = screen.getAllByRole("button", { name: "Log out" });
      fireEvent.click(buttons[buttons.length - 1] as HTMLElement);
      await waitFor(() => expect(host.cloudflareLogout).toHaveBeenCalledOnce());

      push(state({ config: named, tunnel: "connected", wanted: true, url: "https://x.example.com", cloudflare: { loggedIn: true, domain: "example.com", phase: "idle", suggestedSubdomain: "tasktrooper-abc123" } }));
      fireEvent.click(await screen.findByRole("button", { name: "Edit" }));
      await waitFor(() => expect((screen.getByRole("button", { name: "Log out" }) as HTMLButtonElement).disabled).toBe(true));
    });
  });

  describe("ngrok", () => {
    const ngrok = { provider: "ngrok", mode: "quick", hostname: "", autoStart: false, serverName: "" } as const;
    const AUTHTOKEN = "2abcDEF1234ghi_5JkLmNoPqRsTuVwXyZ12";

    it("takes the authtoken once through the connect button, hides it, and links to where to get it", async () => {
      const { host } = stubShell(state({ config: ngrok }));
      renderPage();
      const input = (await screen.findByLabelText("ngrok authtoken")) as HTMLInputElement;
      expect(input.type).toBe("password");
      expect((screen.getByRole("button", { name: "Connect" }) as HTMLButtonElement).disabled).toBe(true);
      expect(screen.getByRole("link", { name: /dashboard\.ngrok\.com/ }).getAttribute("href")).toBe("https://dashboard.ngrok.com/get-started/your-authtoken");
      fireEvent.change(input, { target: { value: AUTHTOKEN } });
      fireEvent.click(screen.getByRole("button", { name: "Connect" }));
      await waitFor(() => expect(host.ngrokConnect).toHaveBeenCalledWith({ authtoken: AUTHTOKEN }));
      await waitFor(() => expect((screen.getByLabelText("ngrok authtoken") as HTMLInputElement).value).toBe(""));
      expect(host.configure).not.toHaveBeenCalled();
    });

    it("shows a spinner while ngrok checks the token, and ngrok's refusal", async () => {
      stubShell(state({ config: ngrok, ngrok: { verified: false, phase: "checking", domains: [] } }));
      const { unmount } = renderPage();
      expect(await screen.findByText("Checking with ngrok…")).toBeTruthy();
      unmount();
      stubShell(state({ config: ngrok, ngrok: { verified: false, phase: "idle", domains: [], error: "ngrok does not accept this authtoken. Copy it again from the ngrok dashboard." } }));
      renderPage();
      expect(await screen.findByText(/does not accept this authtoken/)).toBeTruthy();
    });

    it("offers no domain choice for the free address once connected, and can disconnect", async () => {
      const { host } = stubShell(state({ config: ngrok, secrets: { ngrokAuthtoken: true, ngrokApiKey: false }, ngrok: { verified: true, phase: "idle", domains: [] } }));
      renderPage();
      expect(await screen.findByText("ngrok account connected")).toBeTruthy();
      expect(screen.queryByLabelText("Domain")).toBeNull();
      fireEvent.click(screen.getByRole("button", { name: "Disconnect" }));
      await waitFor(() => expect(host.ngrokDisconnect).toHaveBeenCalledOnce());
    });

    it("lists the account's domains with an API key, and keeps the key out of the page afterwards", async () => {
      const { host } = stubShell(
        state({
          config: { ...ngrok, mode: "named", hostname: "mine.ngrok.app" },
          secrets: { ngrokAuthtoken: true, ngrokApiKey: false },
          ngrok: { verified: true, phase: "idle", domains: ["mine.ngrok.app", "other.ngrok.app"] },
        }),
      );
      renderPage();
      expect(await screen.findByText("2 domains found")).toBeTruthy();
      expect(screen.getByRole("combobox", { name: "Domain" }).textContent).toContain("mine.ngrok.app");
      const key = screen.getByLabelText("ngrok API key (optional)") as HTMLInputElement;
      expect(key.type).toBe("password");
      fireEvent.change(key, { target: { value: "2xYz_apiKey123" } });
      fireEvent.click(screen.getByRole("button", { name: "List my domains" }));
      await waitFor(() => expect(host.ngrokConnect).toHaveBeenCalledWith({ apiKey: "2xYz_apiKey123" }));
      await waitFor(() => expect((screen.getByLabelText("ngrok API key (optional)") as HTMLInputElement).value).toBe(""));
    });
  });

  describe("pairing", () => {
    const connected = () => state({ tunnel: "connected", wanted: true, url: "https://calm-fox-12.trycloudflare.com" });
    const pairing = (ms = 5 * 60_000) => ({
      id: "p1",
      code: "K7QD2M9WXH4T",
      code_display: "K7QD-2M9W-XH4T",
      uri: URI,
      expires_at: new Date(Date.now() + ms).toISOString(),
    });

    it("opens a modal with the QR and the same details as text, for the live tunnel address", async () => {
      const shell = stubShell(connected());
      api.createRemotePairing.mockResolvedValue(pairing());
      renderPage();
      expect((await screen.findByRole("button", { name: "Pair phone" }) as HTMLButtonElement).disabled).toBe(false);
      expect(api.createRemotePairing).not.toHaveBeenCalled();
      fireEvent.click(screen.getByRole("button", { name: "Pair phone" }));
      await waitFor(() => expect(api.createRemotePairing).toHaveBeenCalledOnce());
      expect(api.createRemotePairing.mock.calls[0]?.[0]).toMatchObject({ role: "operator", expires_in_days: 30, public_url: "https://calm-fox-12.trycloudflare.com" });

      expect((await screen.findByTestId("pairing-code")).textContent).toBe("K7QD-2M9W-XH4T");
      expect(screen.getByTestId("pairing-address").textContent).toBe("https://calm-fox-12.trycloudflare.com");
      expect(await screen.findByText("Akif's MacBook")).toBeTruthy();
      expect(screen.getByTestId("pairing-expiry").textContent).toMatch(/Expires in 5 min/);
      expect(screen.getByRole("img", { name: "Scan with the TaskTrooper app" })).toBeTruthy();
      expect(screen.getByText(/Waiting for the phone/)).toBeTruthy();
      expect(screen.getByRole("button", { name: "Copy: Pairing code" })).toBeTruthy();
      expect(screen.getByRole("button", { name: "Copy: Address" })).toBeTruthy();
      expect(screen.queryByRole("button", { name: "New code" })).toBeNull();

      shell.push(state({ tunnel: "stopped", wanted: false }));
      await waitFor(() => expect(screen.queryByTestId("pairing-code")).toBeNull());
      expect(api.cancelRemotePairing).toHaveBeenCalledWith("p1");
    });

    it("offers a new code once the code has expired", async () => {
      stubShell(connected());
      api.createRemotePairing.mockResolvedValueOnce(pairing(1_200)).mockResolvedValueOnce({ ...pairing(), id: "p2", code_display: "AAAA-BBBB-CCCC" });
      renderPage();
      fireEvent.click(await screen.findByRole("button", { name: "Pair phone" }));
      expect(await screen.findByText("This code has expired.", {}, { timeout: 4_000 })).toBeTruthy();
      expect(screen.queryByTestId("pairing-code")).toBeNull();
      fireEvent.click(screen.getByRole("button", { name: "New code" }));
      expect((await screen.findByTestId("pairing-code")).textContent).toBe("AAAA-BBBB-CCCC");
      expect(api.createRemotePairing).toHaveBeenCalledTimes(2);
    }, 8_000);

    it("cancels an unused code when the modal is closed", async () => {
      stubShell(connected());
      api.createRemotePairing.mockResolvedValue(pairing());
      renderPage();
      fireEvent.click(await screen.findByRole("button", { name: "Pair phone" }));
      await screen.findByTestId("pairing-code");
      fireEvent.click(screen.getByRole("button", { name: "Done" }));
      await waitFor(() => expect(api.cancelRemotePairing).toHaveBeenCalledWith("p1"));
    });

    it("recognises the phone that paired", async () => {
      stubShell(connected());
      api.createRemotePairing.mockResolvedValue(pairing());
      renderPage();
      fireEvent.click(await screen.findByRole("button", { name: "Pair phone" }));
      await screen.findByTestId("pairing-code");
      api.listRemoteDevices.mockResolvedValue({
        devices: [{ id: "d1", name: "Akif's iPhone", role: "operator", created_at: new Date().toISOString() }],
      });
      expect(await screen.findByText("Akif's iPhone is connected.", {}, { timeout: 4_000 })).toBeTruthy();
      expect(api.cancelRemotePairing).not.toHaveBeenCalled();
    }, 8_000);

    it("shows the one connected phone as a status line and disconnects it after a confirmation", async () => {
      stubShell(connected());
      api.listRemoteDevices.mockResolvedValue({
        devices: [
          { id: "d1", name: "Akif's iPhone", platform: "ios", role: "viewer", created_at: new Date().toISOString(), expires_at: null },
          { id: "old", name: "Old phone", role: "operator", created_at: new Date().toISOString(), revoked_at: new Date().toISOString() },
        ],
      });
      renderPage();
      expect(await screen.findByText("Paired: Akif's iPhone")).toBeTruthy();
      expect(screen.queryByText(/Old phone/)).toBeNull();
      // A paired phone replaces the Pair phone button with Stop pairing.
      expect(screen.queryByRole("button", { name: "Pair phone" })).toBeNull();
      fireEvent.click(screen.getByRole("button", { name: "Stop pairing" }));
      expect(await screen.findByText("Disconnect Akif's iPhone?")).toBeTruthy();
      const buttons = screen.getAllByRole("button", { name: "Stop pairing" });
      fireEvent.click(buttons[buttons.length - 1] as HTMLElement);
      await waitFor(() => expect(api.revokeRemoteDevice).toHaveBeenCalledWith("d1"));
      await waitFor(() => expect(screen.getByText("Not paired")).toBeTruthy());
      expect(screen.getByRole("button", { name: "Pair phone" })).toBeTruthy();
    });

    it("shows 'Not paired' with the Pair phone button when no phone is connected", async () => {
      stubShell(connected());
      renderPage();
      await screen.findByRole("button", { name: "Pair phone" });
      expect(screen.getByText("Not paired")).toBeTruthy();
      expect(screen.queryByRole("button", { name: "Stop pairing" })).toBeNull();
    });
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
