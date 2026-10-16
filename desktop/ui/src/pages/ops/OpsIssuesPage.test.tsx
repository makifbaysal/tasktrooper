import "@testing-library/jest-dom/vitest";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError, type Incident } from "@/api";
import { I18nProvider } from "@/hooks/useI18n";
import { writeCache } from "@/lib/uiCache";
import { incident, opsOverview } from "@/test/opsConsoleFixtures";

vi.mock("uplot", () => ({
  default: class {
    static pxRatio = 1;
    static rangeNum = (min: number, max: number) => [min, max];
    setData() {}
    setSize() {}
    redraw() {}
    destroy() {}
  },
}));

const mocks = vi.hoisted(() => ({
  getProjectsOverview: vi.fn(),
  listIncidents: vi.fn(),
  listAllReleases: vi.fn(),
  getIncident: vi.fn(),
  triageIncident: vi.fn(),
  resolveIncident: vi.fn(),
  ignoreIncident: vi.fn(),
  proposeIncidentRemedy: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, ...mocks } };
});

import { OpsShell } from "@/components/operations/shell/OpsShell";
import OpsEntityPage from "@/pages/ops/OpsEntityPage";
import OpsIssuesPage from "@/pages/ops/OpsIssuesPage";
import EntityIssues from "@/pages/ops/entity/EntityIssues";
import { EntitySubRoute } from "@/pages/ops/entity/EntitySubRoute";

const LIVE = "open,triaging,proposed,fixing";
const FROM = Date.parse("2026-10-15T00:00:00Z");
const TO = Date.parse("2026-10-16T00:00:00Z");
const RANGE = `range=custom&from=${FROM}&to=${TO}`;

const INCIDENTS: Incident[] = [
  incident("i-low", "r-tools", "low", { title: "Jobs are slow" }),
  incident("i-crit", "r-shop", "critical", {
    title: "Checkout is down",
    occurrences: 12,
    source: "webhook",
    remedy: "Roll back the last deploy",
    remedy_kind: "rollback",
    remedy_author: "auto_triage",
    confidence: 70,
    task_id: "t-9",
  }),
  incident("i-stage", "r-shop", "high", { env: "stage", title: "Staging API errors" }),
  incident("i-medium", "r-site", "medium", { status: "triaging", title: "Site is slow" }),
  incident("i-resolved", "r-site", "critical", {
    status: "resolved",
    title: "Site was down",
    source: "probe",
    first_seen_at: "2026-10-15T08:00:00Z",
    resolved_at: "2026-10-15T08:30:00Z",
    payload: { alertname: "SiteDown", instance: "site:443" },
  }),
];

function withEvents(i: Incident): Incident {
  return {
    ...i,
    events: [
      { id: `${i.id}-1`, incident_id: i.id, kind: "detected", message: "two failed probes", created_at: i.first_seen_at },
      { id: `${i.id}-2`, incident_id: i.id, kind: "triaged", message: "rollback suggested", created_at: i.last_seen_at },
    ],
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
            <Route path="issues" element={<OpsIssuesPage />} />
            <Route path="entity/:componentId" element={<OpsEntityPage />}>
              <Route path="issues" element={<EntitySubRoute tab="issues" page={EntityIssues} />} />
            </Route>
          </Route>
        </Routes>
        <Where />
      </I18nProvider>
    </MemoryRouter>,
  );
}

const CACHE_KEYS = [
  "operations.entities.overview",
  "operations.console.openIncidents",
  "operations.console.shippedReleases",
  "operations.entity.incidents.r-shop",
  "operations.entity.incidents.r-shop|live",
  "operations.entity.releases.c:c-api",
  ...["*", "r-shop", "r-site", "r-tools"].flatMap((r) => [`operations.issues.${r}|live`, `operations.issues.${r}|all`]),
];

beforeEach(() => {
  sessionStorage.clear();
  localStorage.clear();
  for (const key of CACHE_KEYS) writeCache(key, undefined);
  mocks.getProjectsOverview.mockReset().mockResolvedValue(opsOverview);
  mocks.listAllReleases.mockReset().mockResolvedValue({ releases: [] });
  mocks.listIncidents.mockReset().mockImplementation((params: { repositoryId?: string; status?: string } = {}) => {
    const statuses = params.status?.split(",");
    const list = INCIDENTS.filter(
      (i) => (!params.repositoryId || i.repository_id === params.repositoryId) && (!statuses || statuses.includes(i.status)),
    );
    return Promise.resolve({ incidents: list, count: list.length });
  });
  mocks.getIncident.mockReset().mockImplementation((id: string) => {
    const found = INCIDENTS.find((i) => i.id === id);
    return found ? Promise.resolve(withEvents(found)) : Promise.reject(new ApiError("incident not found", 404));
  });
  mocks.triageIncident.mockReset();
  mocks.resolveIncident.mockReset();
  mocks.ignoreIncident.mockReset();
  mocks.proposeIncidentRemedy.mockReset();
});

async function issueTitles() {
  const table = await screen.findByRole("table", { name: "Issues" });
  await waitFor(() => expect(within(table).getAllByRole("link", { name: /^Open issue/ }).length).toBeGreaterThan(0));
  return within(table)
    .getAllByRole("link", { name: /^Open issue/ })
    .map((a) => a.textContent);
}

const rowOf = (title: string) => screen.getByRole("link", { name: `Open issue: ${title}` }).closest("tr")!;

describe("OpsIssuesPage", () => {
  it("lists the environment's live issues, the worst first", async () => {
    renderAt("/operations/issues");
    expect(await issueTitles()).toEqual(["Checkout is down", "Site is slow", "Jobs are slow"]);
    expect(mocks.listIncidents).toHaveBeenCalledWith({ repositoryId: undefined, status: LIVE, limit: 500 });
    expect(screen.getByText("Showing every issue open now")).toBeInTheDocument();

    const crit = rowOf("Checkout is down");
    expect(within(crit).getByText("Critical")).toBeInTheDocument();
    expect(within(crit).getByText("Open")).toBeInTheDocument();
    expect(within(crit).getByText("Production")).toBeInTheDocument();
    expect(within(crit).getByText("12")).toBeInTheDocument();
    expect(within(crit).getByRole("link", { name: "api" })).toHaveAttribute("href", "/operations/entity/c-api/issues");
    expect(within(crit).getByText("+1")).toBeInTheDocument();
  });

  it("adds resolved and ignored issues of the window with the switch", async () => {
    renderAt(`/operations/issues?${RANGE}`);
    await issueTitles();
    fireEvent.click(screen.getByRole("switch", { name: "Include resolved and ignored" }));
    await waitFor(() => expect(screen.getByTestId("where")).toHaveTextContent("closed=1"));
    await waitFor(async () => expect(await issueTitles()).toEqual(["Checkout is down", "Site is slow", "Jobs are slow", "Site was down"]));
    expect(mocks.listIncidents).toHaveBeenCalledWith({ repositoryId: undefined, status: undefined, limit: 500 });
  });

  it("filters by severity and search through the URL", async () => {
    renderAt("/operations/issues");
    await issueTitles();
    fireEvent.click(screen.getByRole("button", { name: /^Medium/ }));
    await waitFor(() => expect(screen.getByTestId("where")).toHaveTextContent("severity=medium"));
    expect(await issueTitles()).toEqual(["Site is slow"]);
    fireEvent.click(screen.getByRole("button", { name: /^All/ }));
    fireEvent.change(screen.getByRole("searchbox", { name: "Search issues" }), { target: { value: "jobs" } });
    await waitFor(async () => expect(await issueTitles()).toEqual(["Jobs are slow"]));
  });

  it("opens ?id= straight from a notification, even a closed issue the list leaves out", async () => {
    renderAt("/operations/issues?id=i-resolved");
    const panel = await screen.findByRole("dialog", { name: "Site was down" });
    expect(mocks.getIncident).toHaveBeenCalledWith("i-resolved");
    expect(within(panel).getByText("Resolved and ignored issues are final. If it happens again, a new issue opens.")).toBeInTheDocument();
    expect(within(panel).queryByRole("button", { name: "Ignore" })).not.toBeInTheDocument();
    expect(within(panel).getByText("Health probe")).toBeInTheDocument();
    expect(within(panel).getByText("Detected")).toBeInTheDocument();
    expect(within(panel).getByText("two failed probes")).toBeInTheDocument();
    fireEvent.click(within(panel).getByRole("button", { name: "Show the alert as received" }));
    expect(within(panel).getByText(/"alertname": "SiteDown"/)).toBeInTheDocument();
  });

  it("shows the remedy, its confidence and who wrote it", async () => {
    renderAt("/operations/issues?id=i-crit");
    const panel = await screen.findByRole("dialog", { name: "Checkout is down" });
    const remedy = within(panel).getByRole("region", { name: "Suggested remedy" });
    expect(within(remedy).getByText("Roll back")).toBeInTheDocument();
    expect(within(remedy).getByText("70%")).toBeInTheDocument();
    expect(within(remedy).getByText("Rules engine (first pass)")).toBeInTheDocument();
    expect(within(remedy).getByText("Roll back the last deploy")).toBeInTheDocument();
    expect(within(panel).getByRole("link", { name: "Open the task" })).toHaveAttribute("href", "/board?task=t-9");
  });

  it("scopes ?entity= to the entity's repository and environment", async () => {
    renderAt("/operations/issues?entity=c-api");
    expect(await issueTitles()).toEqual(["Checkout is down"]);
    expect(mocks.listIncidents).toHaveBeenCalledWith({ repositoryId: "r-shop", status: LIVE, limit: 500 });
    expect(within(screen.getByRole("table", { name: "Issues" })).queryByRole("columnheader", { name: "Entity" })).not.toBeInTheDocument();
  });

  it("follows the environment picked in the top bar", async () => {
    renderAt("/operations/issues?entity=c-api&env=staging");
    expect(await issueTitles()).toEqual(["Staging API errors"]);
  });

  it("narrows to ?repository= and lets the filter go", async () => {
    renderAt("/operations/issues?repository=r-site");
    expect(await issueTitles()).toEqual(["Site is slow"]);
    expect(mocks.listIncidents).toHaveBeenCalledWith({ repositoryId: "r-site", status: LIVE, limit: 500 });
    fireEvent.click(screen.getByRole("button", { name: "Show issues of every repository" }));
    await waitFor(() => expect(screen.getByTestId("where")).not.toHaveTextContent("repository="));
    await waitFor(async () => expect(await issueTitles()).toHaveLength(3));
  });

  it("asks before ignoring an issue, then reads the list again", async () => {
    mocks.ignoreIncident.mockImplementation((id: string) =>
      Promise.resolve({ ...INCIDENTS.find((i) => i.id === id)!, status: "ignored" }),
    );
    renderAt("/operations/issues?id=i-crit");
    const panel = await screen.findByRole("dialog", { name: "Checkout is down" });
    const calls = mocks.listIncidents.mock.calls.length;
    fireEvent.click(within(panel).getByRole("button", { name: "Ignore" }));
    const confirm = await screen.findByRole("dialog", { name: "Ignore this issue?" });
    fireEvent.change(within(confirm).getByLabelText("Note (optional)"), { target: { value: "known flake" } });
    await act(async () => {
      fireEvent.click(within(confirm).getByRole("button", { name: "Ignore" }));
    });
    expect(mocks.ignoreIncident).toHaveBeenCalledWith("i-crit", "known flake");
    await waitFor(() => expect(mocks.listIncidents.mock.calls.length).toBeGreaterThan(calls));
  });

  it("shows a refused triage as the server said it", async () => {
    mocks.triageIncident.mockRejectedValue(new ApiError("incident is resolved", 400));
    renderAt("/operations/issues?id=i-crit");
    const panel = await screen.findByRole("dialog", { name: "Checkout is down" });
    await act(async () => {
      fireEvent.click(within(panel).getByRole("button", { name: "Triage again" }));
    });
    expect(await within(panel).findByText("The server refused this")).toBeInTheDocument();
    expect(within(panel).getByText("incident is resolved")).toBeInTheDocument();
  });

  it("saves a hand-written remedy", async () => {
    mocks.proposeIncidentRemedy.mockImplementation((id: string) =>
      Promise.resolve({ ...INCIDENTS.find((i) => i.id === id)!, status: "proposed", remedy_author: "human" }),
    );
    renderAt("/operations/issues?id=i-crit");
    const panel = await screen.findByRole("dialog", { name: "Checkout is down" });
    fireEvent.click(within(panel).getByRole("button", { name: "Write a remedy" }));
    const dialog = await screen.findByRole("dialog", { name: "Write a remedy" });
    fireEvent.click(within(dialog).getByRole("button", { name: "Save remedy" }));
    expect(within(dialog).getByText("Say what to do.")).toBeInTheDocument();
    expect(mocks.proposeIncidentRemedy).not.toHaveBeenCalled();
    fireEvent.change(within(dialog).getByLabelText("What to do"), { target: { value: "Raise the pool size" } });
    fireEvent.change(within(dialog).getByLabelText("Steps, one per line (optional)"), { target: { value: "edit env\n\nredeploy" } });
    fireEvent.change(within(dialog).getByLabelText("Confidence (0–100)"), { target: { value: "80" } });
    await act(async () => {
      fireEvent.click(within(dialog).getByRole("button", { name: "Save remedy" }));
    });
    expect(mocks.proposeIncidentRemedy).toHaveBeenCalledWith("i-crit", {
      kind: "rollback",
      summary: "Raise the pool size",
      confidence: 80,
      steps: ["edit env", "redeploy"],
      rollback: true,
    });
    await waitFor(() => expect(screen.queryByRole("dialog", { name: "Write a remedy" })).not.toBeInTheDocument());
  });

  it("lists issues whose environment label it does not know apart, whatever environment is picked", async () => {
    const canary = incident("i-canary", "r-shop", "high", { env: "eu-canary", title: "Canary is failing" });
    mocks.listIncidents.mockResolvedValue({ incidents: [...INCIDENTS, canary], count: INCIDENTS.length + 1 });
    renderAt("/operations/issues?env=staging");
    expect(await issueTitles()).toEqual(["Staging API errors"]);
    const other = await screen.findByRole("table", { name: "Issues in other environments" });
    expect(screen.getByRole("heading", { name: "Other environments (1)" })).toBeInTheDocument();
    expect(within(other).getByRole("link", { name: "Open issue: Canary is failing" })).toBeInTheDocument();
    expect(within(other).getByText("eu-canary")).toBeInTheDocument();
    expect(screen.getByText("2 issues")).toBeInTheDocument();
  });

  it("says when the read came back full", async () => {
    const many = Array.from({ length: 500 }, (_, n) => incident(`i-${n}`, "r-site", "low", { title: `Slow page ${n}` }));
    mocks.listIncidents.mockResolvedValue({ incidents: many, count: many.length });
    renderAt("/operations/issues");
    expect(await screen.findByText("Only the 500 most recently seen issues are listed")).toBeInTheDocument();
  });

  it("follows a notification's ?id= to the issue's own environment", async () => {
    renderAt("/operations/issues?id=i-stage");
    expect(await screen.findByRole("dialog", { name: "Staging API errors" })).toBeInTheDocument();
    await waitFor(() => expect(screen.getByTestId("where")).toHaveTextContent("env=staging"));
    expect(screen.getByTestId("where")).toHaveTextContent("id=i-stage");
  });

  it("asks before Triage again replaces a remedy a person wrote", async () => {
    const handWritten = { ...INCIDENTS[1], remedy_author: "human" };
    mocks.getIncident.mockImplementation((id: string) =>
      Promise.resolve(withEvents(id === handWritten.id ? handWritten : INCIDENTS.find((i) => i.id === id)!)),
    );
    mocks.triageIncident.mockResolvedValue({ ...handWritten, remedy_author: "auto_triage" });
    renderAt("/operations/issues?id=i-crit");
    const panel = await screen.findByRole("dialog", { name: "Checkout is down" });
    await waitFor(() => expect(within(panel).getByText("A person")).toBeInTheDocument());
    fireEvent.click(within(panel).getByRole("button", { name: "Triage again" }));
    const confirm = await screen.findByRole("dialog", { name: "Replace the current remedy?" });
    expect(mocks.triageIncident).not.toHaveBeenCalled();
    expect(within(confirm).getByText(/A person wrote the current proposal/)).toBeInTheDocument();
    await act(async () => {
      fireEvent.click(within(confirm).getByRole("button", { name: "Replace and triage" }));
    });
    expect(mocks.triageIncident).toHaveBeenCalledWith("i-crit");
  });
});

describe("EntityIssues", () => {
  it("shows the entity's attributed live issues", async () => {
    renderAt("/operations/entity/c-api/issues");
    expect(await issueTitles()).toEqual(["Checkout is down"]);
    expect(mocks.listIncidents).toHaveBeenCalledWith({ repositoryId: "r-shop", status: LIVE, limit: 500 });
  });

  it("opens the same panel from the Activity column's ?id= link", async () => {
    renderAt("/operations/entity/c-api/issues?id=i-stage");
    expect(await screen.findByRole("dialog", { name: "Staging API errors" })).toBeInTheDocument();
    expect(mocks.getIncident).toHaveBeenCalledWith("i-stage");
  });
});
