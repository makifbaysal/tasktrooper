import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError, type AIJob, type ErrorDefinition } from "@/api";
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
  listErrorDefinitions: vi.fn(),
  createErrorDefinition: vi.fn(),
  updateErrorDefinition: vi.fn(),
  deleteErrorDefinition: vi.fn(),
  acceptErrorDefinition: vi.fn(),
  dismissErrorDefinition: vi.fn(),
  suggestErrorDefinitions: vi.fn(),
  getAIJob: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, ...apiMock } };
});

import { OpsShell } from "@/components/operations/shell/OpsShell";
import OpsErrorDefinitionsPage from "@/pages/ops/OpsErrorDefinitionsPage";

function definition(overrides: Partial<ErrorDefinition>): ErrorDefinition {
  return {
    id: "d-1",
    repository_id: "r-shop",
    component_id: null,
    name: "Stripe timeouts",
    match: { message_regex: "context deadline exceeded.*stripe" },
    category: "dependency",
    severity: "high",
    action: "alert",
    alert_threshold: null,
    source: "human",
    status: "active",
    rationale: "",
    evidence: [],
    task_id: null,
    firing_since: new Date(Date.now() - 5 * 60_000).toISOString(),
    created_at: "2026-10-17T10:00:00Z",
    updated_at: "2026-10-17T10:00:00Z",
    ...overrides,
  };
}

const active = definition({});
const fromErrors = definition({
  id: "d-ai",
  name: "Cart not found",
  component_id: "c-api",
  match: { exception_type: "CartNotFound" },
  category: "user_error",
  severity: "low",
  action: "ignore",
  source: "ai",
  status: "suggested",
  firing_since: null,
  rationale: "Clients ask for carts that expired.",
  evidence: ["CartNotFound: cart 42 (312 times in 24 h)"],
});
const fromCode = definition({
  id: "d-code",
  name: "Card declined",
  match: { status: 402 },
  category: "expected",
  severity: "low",
  action: "none",
  source: "code",
  status: "suggested",
  firing_since: null,
});
const dismissed = definition({ id: "d-gone", name: "Old noise", status: "dismissed", action: "none", firing_since: null });

function job(overrides: Partial<AIJob> = {}): AIJob {
  return {
    id: "j-1",
    kind: "define_errors",
    repository_id: "r-shop",
    component_id: null,
    environment: "production",
    status: "running",
    session_id: "s-1",
    result_summary: "",
    error: "",
    created: [],
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
    finished_at: null,
    ...overrides,
  };
}

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
            <Route path="errors" element={<p>inbox</p>} />
            <Route path="errors/definitions" element={<OpsErrorDefinitionsPage />} />
          </Route>
        </Routes>
        <Where />
      </I18nProvider>
    </MemoryRouter>,
  );
}

async function rows() {
  const table = await screen.findByRole("table", { name: "Error definitions" });
  return within(table).getAllByRole("row").slice(1);
}

beforeEach(() => {
  sessionStorage.clear();
  localStorage.clear();
  for (const key of ["operations.entities.overview", "operations.errorDefinitions.all.all", "operations.errorDefinitions.r-shop.all", "operations.aiJob.defineErrors"]) {
    writeCache(key, undefined);
  }
  Object.values(apiMock).forEach((fn) => fn.mockReset());
  Object.values(toast).forEach((fn) => fn.mockReset());
  apiMock.getProjectsOverview.mockResolvedValue(opsOverview);
  apiMock.listIncidents.mockResolvedValue({ incidents: [], count: 0 });
  apiMock.listAllReleases.mockResolvedValue({ releases: [] });
  apiMock.listErrorDefinitions.mockResolvedValue({ definitions: [active, fromErrors, fromCode, dismissed] });
});

describe("OpsErrorDefinitionsPage", () => {
  it("lists the active definitions with their match in words, under the Errors area's tabs", async () => {
    renderAt("/operations/errors/definitions");
    expect(await screen.findByRole("heading", { level: 1, name: "Error definitions" })).toBeInTheDocument();
    const nav = screen.getByRole("navigation", { name: "Errors" });
    expect(within(nav).getByRole("link", { name: "Definitions" })).toHaveAttribute("aria-current", "page");
    expect(within(nav).getByRole("link", { name: "Inbox" })).toHaveAttribute("href", "/operations/errors");

    const [row] = await rows();
    expect(within(row).getByText("Stripe timeouts")).toBeInTheDocument();
    expect(within(row).getByText("message matches /context deadline exceeded.*stripe/")).toBeInTheDocument();
    expect(within(row).getByText("Dependency")).toBeInTheDocument();
    expect(within(row).getByText("Alert")).toBeInTheDocument();
    expect(within(row).getByText("10 in 1 h (default)")).toBeInTheDocument();
    expect(within(row).getByText("Human")).toBeInTheDocument();
    expect(within(row).getByText("Every component · shop")).toBeInTheDocument();

    const tabs = screen.getByRole("tablist", { name: "Definition status" });
    expect(within(tabs).getByRole("tab", { name: /Active/ })).toHaveAttribute("aria-selected", "true");
    expect(within(within(tabs).getByRole("tab", { name: /Suggested/ })).getByText("2")).toBeInTheDocument();
    expect(apiMock.listErrorDefinitions).toHaveBeenCalledWith({ repositoryId: undefined, componentId: undefined });
  });

  it("accepts a suggestion, and reloads when a dismissal finds it is no suggestion any more", async () => {
    apiMock.acceptErrorDefinition.mockResolvedValue({ ...fromErrors, status: "active" });
    apiMock.dismissErrorDefinition.mockRejectedValue(new ApiError("not a suggestion", 409, "not_suggested"));
    renderAt("/operations/errors/definitions?status=suggested");
    const list = await rows();
    expect(list).toHaveLength(2);
    expect(within(list[0]).getByText("AI")).toBeInTheDocument();
    expect(within(list[1]).getByText("Code")).toBeInTheDocument();
    expect(within(list[0]).getByText("exception type is CartNotFound")).toBeInTheDocument();
    expect(within(list[0]).getByText("api · shop")).toBeInTheDocument();

    fireEvent.click(within(list[0]).getByRole("button", { name: "Accept Cart not found" }));
    await waitFor(() => expect(apiMock.acceptErrorDefinition).toHaveBeenCalledWith("d-ai"));
    await waitFor(() => expect(toast.success).toHaveBeenCalledWith("Cart not found is active now"));
    expect(await rows()).toHaveLength(1);

    const calls = apiMock.listErrorDefinitions.mock.calls.length;
    fireEvent.click(screen.getByRole("button", { name: "Dismiss Card declined" }));
    await waitFor(() => expect(toast.info).toHaveBeenCalledWith("This isn't a suggestion any more, so the list was refreshed."));
    await waitFor(() => expect(apiMock.listErrorDefinitions.mock.calls.length).toBeGreaterThan(calls));
  });

  it("opens a definition from ?definition= on its own status tab, with its reason and evidence", async () => {
    renderAt("/operations/errors/definitions?repository=r-shop&definition=d-ai");
    const panel = await screen.findByRole("dialog");
    expect(within(panel).getByRole("heading", { name: "Cart not found" })).toBeInTheDocument();
    expect(within(panel).getByText("Clients ask for carts that expired.")).toBeInTheDocument();
    expect(within(panel).getByText("CartNotFound: cart 42 (312 times in 24 h)")).toBeInTheDocument();
    expect(within(panel).getByText("Hide from the inbox", { selector: "span" })).toBeInTheDocument();
    expect(within(panel).getByRole("button", { name: "Accept" })).toBeInTheDocument();
    expect(apiMock.listErrorDefinitions).toHaveBeenCalledWith({ repositoryId: "r-shop", componentId: undefined });
    expect(screen.getByRole("tab", { name: /Suggested/, hidden: true })).toHaveAttribute("aria-selected", "true");
  });

  it("writes a definition: checks locally first, then shows the server's field errors in place", async () => {
    const created = definition({ id: "d-new", name: "Payments timeout", category: "bug", action: "none", firing_since: null, match: { message_regex: "payments.*timeout" } });
    apiMock.createErrorDefinition
      .mockRejectedValueOnce(new ApiError("match.message_regex: invalid", 400, "", { "match.message_regex": "invalid" }))
      .mockResolvedValueOnce(created);
    renderAt("/operations/errors/definitions");
    await rows();
    fireEvent.click(screen.getByRole("button", { name: "New definition" }));
    const dialog = await screen.findByRole("dialog", { name: "New error definition" });

    fireEvent.click(within(dialog).getByRole("button", { name: "Save" }));
    expect(within(dialog).getAllByText("Required.").length).toBeGreaterThan(0);
    expect(within(dialog).getByText("Fill in at least one field to match on.")).toBeInTheDocument();
    expect(within(dialog).getByText("Pick a category.")).toBeInTheDocument();
    expect(apiMock.createErrorDefinition).not.toHaveBeenCalled();

    fireEvent.click(within(dialog).getByLabelText("Repository"));
    fireEvent.click(screen.getByRole("option", { name: "shop" }));
    fireEvent.change(within(dialog).getByLabelText("Name"), { target: { value: "Payments timeout" } });
    fireEvent.change(within(dialog).getByLabelText("Message pattern"), { target: { value: "payments.*timeout" } });
    fireEvent.change(within(dialog).getByLabelText("Test it on a sample"), { target: { value: "POST /pay: payments call timeout after 30s" } });
    expect(within(dialog).getByText("Matches")).toBeInTheDocument();
    expect(within(dialog).getByText("payments call timeout", { selector: "mark" })).toBeInTheDocument();
    fireEvent.click(within(dialog).getByLabelText("Category"));
    fireEvent.click(screen.getByRole("option", { name: "Bug" }));

    fireEvent.click(within(dialog).getByRole("button", { name: "Save" }));
    expect(await within(dialog).findByText("RE2 can't compile this pattern.")).toBeInTheDocument();
    expect(within(dialog).getByLabelText("Message pattern")).toHaveAttribute("aria-invalid", "true");
    expect(apiMock.createErrorDefinition).toHaveBeenLastCalledWith({
      repository_id: "r-shop",
      name: "Payments timeout",
      match: { message_regex: "payments.*timeout" },
      category: "bug",
      severity: "medium",
      action: "none",
      rationale: "",
    });

    fireEvent.click(within(dialog).getByRole("button", { name: "Save" }));
    await waitFor(() => expect(screen.queryByRole("dialog", { name: "New error definition" })).not.toBeInTheDocument());
    await waitFor(() => expect(screen.getByTestId("where").textContent).toContain("definition=d-new"));
  });

  it("says a full repository takes no more definitions, in words", async () => {
    apiMock.createErrorDefinition
      .mockRejectedValueOnce(new ApiError("the repository already has as many error definitions as it may hold", 409, "too_many_definitions"))
      .mockRejectedValueOnce(new ApiError("bad", 400, "", { definitions: "too_long" }));
    renderAt("/operations/errors/definitions");
    await rows();
    fireEvent.click(screen.getByRole("button", { name: "New definition" }));
    const dialog = await screen.findByRole("dialog", { name: "New error definition" });
    fireEvent.click(within(dialog).getByLabelText("Repository"));
    fireEvent.click(screen.getByRole("option", { name: "shop" }));
    fireEvent.change(within(dialog).getByLabelText("Name"), { target: { value: "Payments timeout" } });
    fireEvent.change(within(dialog).getByLabelText("Message pattern"), { target: { value: "payments.*timeout" } });
    fireEvent.click(within(dialog).getByLabelText("Category"));
    fireEvent.click(screen.getByRole("option", { name: "Bug" }));
    fireEvent.click(within(dialog).getByRole("button", { name: "Save" }));
    expect(await within(dialog).findByText(/already holds 200 definitions, active and suggested together. Delete or dismiss some/)).toBeInTheDocument();
    fireEvent.click(within(dialog).getByRole("button", { name: "Save" }));
    expect(await within(dialog).findByText("This repository already holds 200 definitions, active and suggested together.")).toBeInTheDocument();
  });

  it("asks the AI, follows the job and links what it proposed", async () => {
    apiMock.suggestErrorDefinitions.mockResolvedValue({ job_id: "j-1", job: job() });
    apiMock.getAIJob.mockResolvedValue(
      job({
        status: "done",
        result_summary: "Two groups are expected **user errors**.",
        created: [{ kind: "error_definition", id: "d-ai", name: "Cart not found", rationale: "Expired carts" }],
      }),
    );
    renderAt("/operations/errors/definitions?repository=r-shop");
    await rows();
    fireEvent.click(screen.getByRole("button", { name: "Ask AI to define errors" }));
    const dialog = await screen.findByRole("dialog", { name: "Ask AI to define errors" });
    fireEvent.click(within(dialog).getByRole("radio", { name: /The code's error types/ }));
    fireEvent.click(within(dialog).getByRole("button", { name: "Ask" }));

    await waitFor(() =>
      expect(apiMock.suggestErrorDefinitions).toHaveBeenCalledWith({ repository_id: "r-shop", component_id: undefined, environment: "production", mode: "code" }),
    );
    const link = await screen.findByRole("link", { name: "Cart not found" });
    expect(link).toHaveAttribute("href", "/operations/errors/definitions?repository=r-shop&definition=d-ai");
    expect(screen.getByText("Expired carts")).toBeInTheDocument();
    expect(screen.getByText("user errors")).toBeInTheDocument();
    expect(apiMock.getAIJob).toHaveBeenCalledWith("j-1");

    fireEvent.click(screen.getByRole("button", { name: "Review 1 suggestions" }));
    await waitFor(() => expect(screen.getByTestId("where").textContent).toContain("status=suggested"));
  });

  it("says how to turn the release engineer on when there is none", async () => {
    apiMock.suggestErrorDefinitions.mockRejectedValue(new ApiError("no release engineer", 409, "no_release_engineer"));
    renderAt("/operations/errors/definitions?repository=r-shop");
    await rows();
    fireEvent.click(screen.getByRole("button", { name: "Ask AI to define errors" }));
    const dialog = await screen.findByRole("dialog", { name: "Ask AI to define errors" });
    fireEvent.click(within(dialog).getByRole("button", { name: "Ask" }));
    expect(await screen.findByText("No release engineer is on for this component")).toBeInTheDocument();
    expect(screen.getByText(/Turn it on with Edit team/)).toBeInTheDocument();
    expect(apiMock.getAIJob).not.toHaveBeenCalled();
  });
});
