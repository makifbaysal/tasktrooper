import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError, type AlertToken, type CreatedAlertToken } from "@/api";
import { ALERT_DOCS_URL } from "@/components/operations/alerts/alertConnections";
import { I18nProvider } from "@/hooks/useI18n";
import { writeCache } from "@/lib/uiCache";
import { opsOverview } from "@/test/opsConsoleFixtures";

const mocks = vi.hoisted(() => ({
  getProjectsOverview: vi.fn(),
  listAlertTokens: vi.fn(),
  createAlertToken: vi.fn(),
  deleteAlertToken: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, ...mocks } };
});

import { OpsShell } from "@/components/operations/shell/OpsShell";
import OpsAlertConnectionsPage from "@/pages/ops/OpsAlertConnectionsPage";

if (!Element.prototype.scrollIntoView) {
  Element.prototype.scrollIntoView = () => {};
}

const SECRET = "tt_alert_s3cr3t-T0ken_value";
const BASE = "http://127.0.0.1:61234";

const SHOP_TOKENS: AlertToken[] = [
  { id: "tok-grafana", label: "Grafana prod", default_env: "prod", created_at: "2026-10-10T10:00:00Z", last_used_at: "2026-10-15T09:00:00Z" },
  { id: "tok-old", label: "Old alerts", default_env: "stage", created_at: "2026-10-01T10:00:00Z", last_used_at: null },
];

const CREATED: CreatedAlertToken = {
  id: "tok-new",
  label: "Alertmanager",
  default_env: "preprod",
  created_at: "2026-10-16T10:00:00Z",
  token: SECRET,
  ingest_path: `/v1/alerts/ingest/${SECRET}`,
};

function renderPage() {
  return render(
    <MemoryRouter initialEntries={["/operations/alert-connections"]}>
      <I18nProvider>
        <Routes>
          <Route path="/operations" element={<OpsShell />}>
            <Route path="alert-connections" element={<OpsAlertConnectionsPage />} />
          </Route>
        </Routes>
      </I18nProvider>
    </MemoryRouter>,
  );
}

function withTokens(byRepo: Record<string, AlertToken[] | null>) {
  mocks.listAlertTokens.mockImplementation(async (id: string) => ({ tokens: id in byRepo ? byRepo[id] : [] }));
}

function storedAnywhere(text: string): boolean {
  for (const store of [sessionStorage, localStorage]) {
    for (let i = 0; i < store.length; i++) {
      const key = store.key(i);
      if (key && (key.includes(text) || (store.getItem(key) ?? "").includes(text))) return true;
    }
  }
  return false;
}

const writeText = vi.fn();

beforeEach(() => {
  sessionStorage.clear();
  localStorage.clear();
  writeCache("operations.entities.overview", undefined);
  writeCache("operations.alertConnections.tokens", undefined);
  mocks.getProjectsOverview.mockReset().mockResolvedValue(opsOverview);
  mocks.listAlertTokens.mockReset();
  mocks.createAlertToken.mockReset();
  mocks.deleteAlertToken.mockReset().mockResolvedValue(undefined);
  writeText.mockReset().mockResolvedValue(undefined);
  Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });
});

afterEach(() => {
  delete window.__tasktrooperDesktop;
});

describe("OpsAlertConnectionsPage", () => {
  it("explains alert connections when there are none, with a link to the docs", async () => {
    withTokens({ "r-shop": null });
    renderPage();

    expect(await screen.findByText("No alert connections yet")).toBeInTheDocument();
    expect(screen.getByText(/An alert connection is a private URL for one alerting tool/)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /How alert connections work/ })).toHaveAttribute("href", ALERT_DOCS_URL);
    expect(screen.getByRole("button", { name: "New connection" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { level: 1, name: "Alert connections" })).toBeInTheDocument();
    expect(screen.getAllByRole("link", { name: "Alert connections" })[0]).toHaveAttribute("aria-current", "page");
    expect(screen.queryByLabelText("Environment")).not.toBeInTheDocument();
    expect(mocks.listAlertTokens).toHaveBeenCalledTimes(4);
  });

  it("lists every repository's connections, with Never for one that has not been used", async () => {
    withTokens({ "r-shop": SHOP_TOKENS });
    renderPage();

    const table = await screen.findByRole("table", { name: "Alert connections" });
    const shop = within(table).getByRole("rowgroup", { name: "shop" });
    const rows = within(shop).getAllByRole("row");
    expect(rows).toHaveLength(3);
    expect(within(rows[1]).getByText("Grafana prod")).toBeInTheDocument();
    expect(within(rows[1]).getByText("Production")).toBeInTheDocument();
    expect(within(rows[1]).queryByText("Never")).not.toBeInTheDocument();
    expect(within(rows[2]).getByText("Old alerts")).toBeInTheDocument();
    expect(within(rows[2]).getByText("Staging")).toBeInTheDocument();
    expect(within(rows[2]).getByText("stage")).toBeInTheDocument();
    expect(within(rows[2]).getByText("Never")).toBeInTheDocument();

    const tools = within(table).getByRole("rowgroup", { name: "tools" });
    expect(within(tools).getByText("No connections yet.")).toBeInTheDocument();
    const names = within(table)
      .getAllByRole("rowgroup")
      .map((g) => g.getAttribute("aria-label"))
      .filter(Boolean);
    expect(names).toEqual(["docs", "shop", "site", "tools"]);
  });

  it("creates a connection, reveals its URL once, and forgets it on close", async () => {
    window.__tasktrooperDesktop = { apiBase: BASE } as Window["__tasktrooperDesktop"];
    withTokens({ "r-shop": SHOP_TOKENS });
    mocks.createAlertToken.mockResolvedValue(CREATED);
    renderPage();

    fireEvent.click(await screen.findByRole("button", { name: "New connection for shop" }));
    let dialog = await screen.findByRole("dialog", { name: "New alert connection" });
    fireEvent.click(within(dialog).getByRole("button", { name: "Create connection" }));
    expect(await within(dialog).findByText("Give it a label.")).toBeInTheDocument();
    expect(mocks.createAlertToken).not.toHaveBeenCalled();

    fireEvent.change(within(dialog).getByLabelText("Label"), { target: { value: "  Alertmanager " } });
    fireEvent.click(within(dialog).getByLabelText("Default environment"));
    fireEvent.click(await screen.findByRole("option", { name: /Pre-production/ }));
    fireEvent.click(within(dialog).getByRole("button", { name: "Create connection" }));

    dialog = await screen.findByRole("dialog", { name: "Connection created" });
    expect(mocks.createAlertToken).toHaveBeenCalledWith("r-shop", { label: "Alertmanager", default_env: "preprod" });
    const url = `${BASE}/v1/alerts/ingest/${SECRET}`;
    expect(within(dialog).getByLabelText("Ingest URL")).toHaveTextContent(url);
    expect(within(dialog).getByText("Copy the URL now: it is shown only this once")).toBeInTheDocument();
    expect(within(dialog).getByText("Reachable only from this computer")).toBeInTheDocument();
    expect(within(dialog).getByText(/The server listens on 127\.0\.0\.1:61234/)).toBeInTheDocument();
    expect(within(dialog).getByText(/picks a new port each time it starts/)).toBeInTheDocument();

    const snippet = (tool: string) => within(dialog).getByRole("tabpanel", { name: tool });
    expect(snippet("Alertmanager")).toHaveTextContent(`url: "${url}?source=alertmanager"`);
    fireEvent.click(within(dialog).getByRole("tab", { name: "Grafana" }));
    expect(snippet("Grafana")).toHaveTextContent("type: webhook");
    fireEvent.click(within(dialog).getByRole("tab", { name: "Sentry" }));
    expect(snippet("Sentry")).toHaveTextContent(`${url}?source=sentry`);
    fireEvent.click(within(dialog).getByRole("tab", { name: "Cloud Monitoring" }));
    expect(snippet("Cloud Monitoring")).toHaveTextContent("webhook_tokenauth");
    fireEvent.click(within(dialog).getByRole("tab", { name: "curl" }));
    expect(snippet("curl")).toHaveTextContent('"env": "preprod"');
    expect(within(snippet("curl")).getByRole("button", { name: "Copy the curl snippet" })).toBeInTheDocument();

    fireEvent.click(within(dialog).getByRole("button", { name: "Copy the URL" }));
    await waitFor(() => expect(writeText).toHaveBeenCalledWith(url));

    const shopBehind = screen.getByRole("rowgroup", { name: "shop", hidden: true });
    expect(within(shopBehind).getByText("Alertmanager")).toBeInTheDocument();
    expect(storedAnywhere(SECRET)).toBe(false);

    fireEvent.click(within(dialog).getByRole("button", { name: "Done" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    const shop = screen.getByRole("rowgroup", { name: "shop" });
    expect(within(shop).getByText("Alertmanager")).toBeInTheDocument();
    expect(within(shop).getAllByText("Never")).toHaveLength(2);
    expect(screen.queryByText(new RegExp(SECRET))).not.toBeInTheDocument();
    expect(storedAnywhere(SECRET)).toBe(false);
    expect(window.location.href).not.toContain(SECRET);

    fireEvent.click(screen.getByRole("button", { name: "New connection for shop" }));
    dialog = await screen.findByRole("dialog", { name: "New alert connection" });
    expect(within(dialog).getByLabelText("Label")).toHaveValue("");
    expect(screen.queryByText(new RegExp(SECRET))).not.toBeInTheDocument();
  });

  it("keeps the form open with the server's reason when creating fails", async () => {
    withTokens({ "r-shop": SHOP_TOKENS });
    mocks.createAlertToken.mockRejectedValue(new ApiError("invalid alert token request: unknown default_env", 400));
    renderPage();

    fireEvent.click(await screen.findByRole("button", { name: "New connection for tools" }));
    const dialog = await screen.findByRole("dialog", { name: "New alert connection" });
    fireEvent.change(within(dialog).getByLabelText("Label"), { target: { value: "Sentry" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Create connection" }));

    expect(await within(dialog).findByText("Could not create the connection")).toBeInTheDocument();
    expect(within(dialog).getByText(/unknown default_env/)).toBeInTheDocument();
    expect(mocks.createAlertToken).toHaveBeenCalledWith("r-tools", { label: "Sentry", default_env: "prod" });
  });

  it("deletes a connection only after the confirm", async () => {
    withTokens({ "r-shop": SHOP_TOKENS });
    renderPage();

    const shop = await screen.findByRole("rowgroup", { name: "shop" });
    fireEvent.click(within(shop).getByRole("button", { name: "Delete Old alerts" }));
    let confirm = await screen.findByRole("dialog", { name: "Delete Old alerts?" });
    expect(within(confirm).getByText(/opens no more issues on shop/)).toBeInTheDocument();
    fireEvent.click(within(confirm).getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(mocks.deleteAlertToken).not.toHaveBeenCalled();

    fireEvent.click(within(shop).getByRole("button", { name: "Delete Old alerts" }));
    confirm = await screen.findByRole("dialog", { name: "Delete Old alerts?" });
    fireEvent.click(within(confirm).getByRole("button", { name: "Delete" }));

    await waitFor(() => expect(mocks.deleteAlertToken).toHaveBeenCalledWith("r-shop", "tok-old"));
    await waitFor(() => expect(within(shop).queryByText("Old alerts")).not.toBeInTheDocument());
    expect(within(shop).getByText("Grafana prod")).toBeInTheDocument();
  });
});
