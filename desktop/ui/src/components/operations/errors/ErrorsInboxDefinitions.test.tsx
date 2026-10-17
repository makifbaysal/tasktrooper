import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ErrorDefinition, OpsErrorGroup, OpsErrors } from "@/api";
import { I18nProvider } from "@/hooks/useI18n";
import { writeCache } from "@/lib/uiCache";
import { opsOverview } from "@/test/opsConsoleFixtures";

if (!Element.prototype.scrollIntoView) {
  Element.prototype.scrollIntoView = () => {};
}

const toast = vi.hoisted(() => ({ success: vi.fn(), info: vi.fn(), error: vi.fn() }));
vi.mock("sonner", () => ({ toast }));

const apiMock = vi.hoisted(() => ({
  getProjectsOverview: vi.fn(),
  listIncidents: vi.fn(),
  listAllReleases: vi.fn(),
  listOpsErrors: vi.fn(),
  createErrorTask: vi.fn(),
  createErrorDefinition: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, ...apiMock } };
});

import { OpsShell } from "@/components/operations/shell/OpsShell";
import OpsErrorsInboxPage from "@/pages/ops/OpsErrorsInboxPage";

const ago = (ms: number) => new Date(Date.now() - ms).toISOString();

function group(overrides: Partial<OpsErrorGroup>): OpsErrorGroup {
  return {
    component_id: "c-api",
    repository_id: "r-shop",
    environment_id: "e-api",
    provider: "gcp",
    fingerprint: "fp",
    message: "boom",
    count: 1,
    first_seen: ago(30 * 60_000),
    last_seen: ago(5 * 60_000),
    new: false,
    ...overrides,
  };
}

const defined = group({
  fingerprint: "fp-stripe",
  message: "context deadline exceeded calling stripe",
  count: 40,
  definition: { id: "d-1", name: "Stripe timeouts", category: "dependency", severity: "high", action: "alert" },
});
const undefinedGroup = group({ fingerprint: "fp-order", message: "order 4521 not found\n  at orders.go:42", count: 3 });
const ignoredGroup = group({
  fingerprint: "fp-cart",
  message: "CartNotFound: cart 7",
  definition: { id: "d-2", name: "Cart not found", category: "user_error", severity: "low", action: "ignore" },
});

const answer: OpsErrors = { groups: [defined, undefinedGroup], unavailable: [], truncated: false, ignored_count: 4 };

function Where() {
  const { pathname, search } = useLocation();
  return <p data-testid="where">{`${pathname}${search}`}</p>;
}

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <I18nProvider>
        <Routes>
          <Route path="/operations" element={<OpsShell />}>
            <Route path="errors" element={<OpsErrorsInboxPage />} />
            <Route path="errors/definitions" element={<p>definitions page</p>} />
          </Route>
        </Routes>
        <Where />
      </I18nProvider>
    </MemoryRouter>,
  );
}

async function groupRow(text: string) {
  const table = await screen.findByRole("table", { name: "Error groups" });
  return (await within(table).findByText((content) => content.startsWith(text))).closest("tr") as HTMLElement;
}

beforeEach(() => {
  sessionStorage.clear();
  localStorage.clear();
  writeCache("operations.entities.overview", undefined);
  Object.values(apiMock).forEach((fn) => fn.mockReset());
  Object.values(toast).forEach((fn) => fn.mockReset());
  apiMock.getProjectsOverview.mockResolvedValue(opsOverview);
  apiMock.listIncidents.mockResolvedValue({ incidents: [], count: 0 });
  apiMock.listAllReleases.mockResolvedValue({ releases: [] });
  apiMock.listOpsErrors.mockImplementation(async (query: { includeIgnored?: boolean }) =>
    query.includeIgnored ? { groups: [defined, undefinedGroup, ignoredGroup], unavailable: [], truncated: false } : answer,
  );
});

describe("Errors inbox and error definitions", () => {
  it("shows the definition a group matches, linked to it, and the way to the Definitions view", async () => {
    renderAt("/operations/errors");
    const row = await groupRow("context deadline exceeded calling stripe");
    const chip = within(row).getByRole("link", { name: /Dependency\s*Stripe timeouts/ });
    expect(chip).toHaveAttribute("href", "/operations/errors/definitions?repository=r-shop&definition=d-1");
    expect(within(row).getByRole("link", { name: "Open its definition" })).toBeInTheDocument();
    expect(within(row).queryByRole("button", { name: "Define this error" })).not.toBeInTheDocument();
    const nav = screen.getByRole("navigation", { name: "Errors" });
    expect(within(nav).getByRole("link", { name: "Inbox" })).toHaveAttribute("aria-current", "page");
  });

  it("brings back the groups an ignore definition hides", async () => {
    renderAt("/operations/errors");
    await groupRow("order 4521 not found");
    expect(apiMock.listOpsErrors.mock.calls[0][0].includeIgnored).toBeUndefined();
    const toggle = screen.getByRole("switch", { name: "Show ignored (4)" });
    fireEvent.click(toggle);
    await waitFor(() => expect(apiMock.listOpsErrors).toHaveBeenLastCalledWith(expect.objectContaining({ includeIgnored: true })));
    expect(screen.getByTestId("where").textContent).toContain("ignored=1");
    const row = await groupRow("CartNotFound: cart 7");
    expect(within(row).getByText("Hidden from the inbox by its definition")).toBeInTheDocument();
    expect(screen.getByRole("switch", { name: "Show ignored" })).toBeChecked();
  });

  it("offers Show ignored when every group in the range is hidden", async () => {
    apiMock.listOpsErrors.mockResolvedValue({ groups: [], unavailable: [], truncated: false, ignored_count: 2 });
    renderAt("/operations/errors");
    fireEvent.click(await screen.findByRole("button", { name: "Show ignored (2)" }));
    await waitFor(() => expect(screen.getByTestId("where").textContent).toContain("ignored=1"));
  });

  it("defines a group from the inbox, prefilled with its fingerprint and a pattern for its message", async () => {
    const saved: ErrorDefinition = {
      id: "d-9",
      repository_id: "r-shop",
      component_id: "c-api",
      name: "order 4521 not found",
      match: { fingerprint: "fp-order", message_regex: "order \\d+ not found" },
      category: "user_error",
      severity: "medium",
      action: "none",
      alert_threshold: null,
      source: "human",
      status: "active",
      rationale: "",
      evidence: [],
      task_id: null,
      firing_since: null,
      created_at: "",
      updated_at: "",
    };
    apiMock.createErrorDefinition.mockResolvedValue(saved);
    renderAt("/operations/errors");
    const row = await groupRow("order 4521 not found");
    fireEvent.click(within(row).getByRole("button", { name: "Define this error" }));
    const dialog = await screen.findByRole("dialog", { name: "New error definition" });
    expect(within(dialog).getByLabelText("Fingerprint")).toHaveValue("fp-order");
    expect(within(dialog).getByLabelText("Message pattern")).toHaveValue("order \\d+ not found");
    expect(within(dialog).getByLabelText("Name")).toHaveValue("order 4521 not found");
    expect(within(dialog).getByText("Matches")).toBeInTheDocument();

    fireEvent.click(within(dialog).getByLabelText("Category"));
    fireEvent.click(screen.getByRole("option", { name: "User error" }));
    const before = apiMock.listOpsErrors.mock.calls.length;
    fireEvent.click(within(dialog).getByRole("button", { name: "Save" }));
    await waitFor(() =>
      expect(apiMock.createErrorDefinition).toHaveBeenCalledWith(
        expect.objectContaining({
          repository_id: "r-shop",
          component_id: "c-api",
          match: { fingerprint: "fp-order", message_regex: "order \\d+ not found" },
          category: "user_error",
        }),
      ),
    );
    await waitFor(() => expect(toast.success).toHaveBeenCalledWith("Definition saved", expect.anything()));
    await waitFor(() => expect(apiMock.listOpsErrors.mock.calls.length).toBeGreaterThan(before));
  });

  it("puts Define in the group's side panel too", async () => {
    renderAt("/operations/errors?group=e-api.fp-order");
    const panel = await screen.findByRole("region", { name: "Error group" });
    fireEvent.click(within(panel).getByRole("button", { name: "Define" }));
    expect(await screen.findByRole("dialog", { name: "New error definition" })).toBeInTheDocument();
  });
});
