import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError, type Agent, type MCPServerListResponse, type MCPServerView } from "@/api";
import { I18nProvider } from "@/hooks/useI18n";
import { MCPServersPage } from "@/pages/MCPServersPage";

if (!Element.prototype.scrollIntoView) {
  Element.prototype.scrollIntoView = () => {};
}

const { api, toast } = vi.hoisted(() => ({
  api: {
    listMCPServers: vi.fn(),
    listAgents: vi.fn(),
    createMCPServer: vi.fn(),
    updateMCPServer: vi.fn(),
    deleteMCPServer: vi.fn(),
    startMCPOAuth: vi.fn(),
    disconnectMCPOAuth: vi.fn(),
  },
  toast: { success: vi.fn(), error: vi.fn(), info: vi.fn() },
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, ...api } };
});

vi.mock("sonner", () => ({ toast }));

function server(overrides: Partial<MCPServerView>): MCPServerView {
  return {
    id: "server",
    enabled: true,
    transport: "http",
    url: "https://mcp.example.com/mcp",
    access: "all",
    created_at: "2026-10-01T00:00:00Z",
    connected: true,
    tool_count: 0,
    status: "connected",
    auth: "none",
    ...overrides,
  };
}

function agent(name: string, servers: string[]): Agent {
  return { id: name, name, tool_policy: { allow_mcp_servers: servers } } as Agent;
}

function listing(servers: MCPServerView[]): MCPServerListResponse {
  return { servers, count: servers.length, oauth_redirect_uri: "http://127.0.0.1:43123/oauth/mcp/callback" };
}

function renderPage() {
  return render(
    <I18nProvider>
      <MCPServersPage />
    </I18nProvider>,
  );
}

function rowOf(id: string): HTMLElement {
  const cell = screen.getByText(id, { selector: "div.font-medium" });
  return cell.closest("tr") as HTMLElement;
}

describe("MCPServersPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    api.listAgents.mockResolvedValue({ agents: [agent("UI Designer", ["figma"]), agent("Backend", [])] });
  });

  afterEach(() => {
    delete window.__tasktrooperDesktop;
  });

  it("shows who each server is available to and which agents list it", async () => {
    api.listMCPServers.mockResolvedValue(
      listing([
        server({ id: "figma", access: "listed" }),
        server({ id: "linear", access: "listed" }),
        server({ id: "browser", transport: "stdio", access: "all" }),
      ]),
    );
    renderPage();

    await screen.findByText("Listed by UI Designer");
    expect(within(rowOf("figma")).getByText("Listed agents")).toBeInTheDocument();
    expect(within(rowOf("linear")).getByText("No agent lists it yet")).toBeInTheDocument();
    expect(within(rowOf("browser")).getByText("All agents")).toBeInTheDocument();
  });

  it("adds a new server for listed agents only unless told otherwise, and saves the choice", async () => {
    api.listMCPServers.mockResolvedValue(listing([]));
    api.createMCPServer.mockResolvedValue(server({ id: "filesystem" }));
    renderPage();

    fireEvent.click(await screen.findByRole("button", { name: "Add First Server" }));
    const access = await screen.findByRole("combobox", { name: "Available to" });
    expect(access).toHaveTextContent("Only agents that list it");
    expect(screen.getByText(/No agent gets these tools until/)).toBeInTheDocument();

    fireEvent.click(access);
    fireEvent.click(await screen.findByRole("option", { name: "All agents" }));
    expect(screen.getByText(/Every agent gets these tools/)).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Add" }));
    await waitFor(() => expect(api.createMCPServer).toHaveBeenCalledTimes(1));
    expect(api.createMCPServer.mock.calls[0][0]).toMatchObject({ id: "filesystem", access: "all" });
  });

  it("connects an HTTP server that needs sign-in in the system browser and waits for it", async () => {
    const openExternal = vi.fn().mockResolvedValue(true);
    window.__tasktrooperDesktop = { runner: { openExternal } as never };
    api.listMCPServers.mockResolvedValue(
      listing([server({ id: "figma", access: "listed", status: "error", connected: false, auth: "oauth_needed" })]),
    );
    api.startMCPOAuth.mockResolvedValue({
      authorization_url: "https://auth.example.com/authorize?state=abc",
      expires_at: "2026-10-08T12:10:00Z",
    });
    renderPage();

    expect(await screen.findByText("Sign-in needed")).toBeInTheDocument();
    api.listMCPServers
      .mockResolvedValueOnce(
        listing([server({ id: "figma", access: "listed", status: "error", connected: false, auth: "oauth_needed" })]),
      )
      .mockResolvedValue(listing([server({ id: "figma", access: "listed", auth: "oauth_connected", tool_count: 3 })]));
    fireEvent.click(screen.getByRole("button", { name: "Connect" }));

    await waitFor(() => expect(openExternal).toHaveBeenCalledWith("https://auth.example.com/authorize?state=abc"));
    expect(api.startMCPOAuth).toHaveBeenCalledWith("figma", undefined);
    expect(await screen.findByRole("link", { name: "Open the sign-in page" })).toHaveAttribute(
      "href",
      "https://auth.example.com/authorize?state=abc",
    );

    expect(await screen.findByText("Signed in", {}, { timeout: 5000 })).toBeInTheDocument();
    expect(toast.success).toHaveBeenCalledWith("Signed in to figma");
    expect(screen.queryByText("Waiting for sign-in…")).not.toBeInTheDocument();
  }, 10000);

  it("asks for a client id when the authorization server cannot register one", async () => {
    window.__tasktrooperDesktop = { runner: { openExternal: vi.fn().mockResolvedValue(true) } as never };
    api.listMCPServers.mockResolvedValue(
      listing([server({ id: "github", status: "error", connected: false, auth: "oauth_needed" })]),
    );
    api.startMCPOAuth
      .mockRejectedValueOnce(new ApiError("enter a client id", 400, "oauth_client_required"))
      .mockResolvedValueOnce({ authorization_url: "https://github.example/login", expires_at: "" });
    renderPage();

    fireEvent.click(await screen.findByRole("button", { name: "Connect" }));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText("http://127.0.0.1:43123/oauth/mcp/callback")).toBeInTheDocument();

    fireEvent.change(within(dialog).getByLabelText("Client ID"), { target: { value: " my-client " } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Continue to sign-in" }));

    await waitFor(() => expect(api.startMCPOAuth).toHaveBeenCalledTimes(2));
    expect(api.startMCPOAuth.mock.calls[1]).toEqual(["github", { client_id: "my-client", client_secret: undefined }]);
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  });

  it("disconnects a signed-in server", async () => {
    api.listMCPServers.mockResolvedValue(listing([server({ id: "figma", auth: "oauth_connected" })]));
    api.disconnectMCPOAuth.mockResolvedValue(undefined);
    renderPage();

    fireEvent.click(await screen.findByRole("button", { name: "Disconnect" }));
    await waitFor(() => expect(api.disconnectMCPOAuth).toHaveBeenCalledWith("figma"));
    expect(toast.success).toHaveBeenCalledWith("Signed out of figma");
  });
});
